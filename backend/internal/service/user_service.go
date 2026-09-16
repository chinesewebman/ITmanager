package service

import (
	"context"
	"errors"
	"fmt"

	"network-monitor-platform/internal/middleware"
	"network-monitor-platform/internal/models"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// UserService 用户业务接口。读路径（List/Get）把账号清单给 admin；
// 写路径（M61）是账号处置：启用/禁用、改角色、置强改密标志。
type UserService interface {
	// 🐛 BUG#26: 原 List() 无分页，10k users 全表返会内存爆
	List(ctx context.Context, page, pageSize int) (items []models.User, total int64, err error)
	Get(ctx context.Context, id string) (*models.User, error)
	// Update 局部更新（只写给出的字段）。actor 是发起人 —— 自我降级/自我禁用
	// 与「最后一名管理员」两道守卫都需要它，见 checkUserUpdateGuards。
	Update(ctx context.Context, id string, in UpdateUserInput, actor Actor) (*models.User, error)
	// UpdateStatus 只改 status（Update 的窄入口，守卫/事务/回读与 Update 同一份实现）。
	UpdateStatus(ctx context.Context, id, status string, actor Actor) (*models.User, error)
	// UpdateRole 只改 role（同上）。role 先过 middleware.CanonicalRole 折叠遗留别名，
	// 再按权威词表校验 —— 直接存原始字符串会让 ` Admin ` / `operator` 这类值绕过
	// 能力矩阵的字面量比较（S-1a 同族）。
	UpdateRole(ctx context.Context, id, role string, actor Actor) (*models.User, error)
}

// UpdateUserInput 局部更新入参。nil = 本次不动该字段
// （故不能用值类型：`false` / `""` 都是合法取值，与「未提供」必须可区分）。
type UpdateUserInput struct {
	Status             *string
	Role               *string
	MustChangePassword *bool
}

// userStatusValues 可写的账户状态词表。
//
// 只收 active / inactive —— 因为**只有这两个值在鉴权侧被消费**：
// middleware/auth.go 的 JWT 与 API Key 两条路径都只拦 `status == "inactive"`，
// 登录 handler（auth_handler.go:90）也只拦 inactive。`locked` 虽在 models.User 的
// 注释里，写进去不会拦住任何一次请求（真正的锁定走 `locked_until`），
// 收它等于给管理员一个静默无效的开关（T-72 同族：写了不生效的字段）。
var userStatusValues = map[string]bool{
	"active":   true,
	"inactive": true,
}

type userService struct {
	db *gorm.DB
}

func NewUserService(db *gorm.DB) UserService {
	return &userService{db: db}
}

func (s *userService) List(ctx context.Context, page, pageSize int) ([]models.User, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 500 {
		pageSize = 500
	}
	var total int64
	if err := s.db.WithContext(ctx).Model(&models.User{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var users []models.User
	if err := s.db.WithContext(ctx).
		Order("created_at DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).
		Find(&users).Error; err != nil {
		return nil, 0, err
	}
	return users, total, nil
}

func (s *userService) Get(ctx context.Context, id string) (*models.User, error) {
	var u models.User
	if err := s.db.WithContext(ctx).First(&u, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &u, nil
}

// Update 局部更新一个账号。校验在事务**之前**（请求写错就直接 400，不占连接），
// 守卫与写入在同一个事务里（守卫依赖读到的旧值，见 applyUserUpdate）。
func (s *userService) Update(ctx context.Context, id string, in UpdateUserInput, actor Actor) (*models.User, error) {
	updates := make(map[string]interface{}, 3)
	if in.Status != nil {
		if !userStatusValues[*in.Status] {
			return nil, fmt.Errorf("%w: status 只能是 active / inactive", ErrInvalidInput)
		}
		updates["status"] = *in.Status
	}
	if in.Role != nil {
		// 折叠遗留别名（operator→ops_user / viewer→readonly）再校验：否则等价的两个
		// 字符串在能力矩阵里一个放行一个降权（S-1a 的病灶形态）。
		canonical := middleware.CanonicalRole(*in.Role)
		if !middleware.IsKnownRole(canonical) {
			return nil, fmt.Errorf(
				"%w: 未知角色 %q，可用值：admin / ops_admin / ops_user / auditor / readonly",
				ErrInvalidInput, *in.Role)
		}
		updates["role"] = canonical
	}
	if in.MustChangePassword != nil {
		updates["must_change_password"] = *in.MustChangePassword
	}

	// 空 updates：不发 UPDATE（gorm 的 Updates(空 map) 会报 ErrEmptySlice 之类的错），
	// 等价于「没要求改任何东西」→ 回读当前值，与 channelService.Update 同口径。
	if len(updates) == 0 {
		return s.Get(ctx, id)
	}
	return s.applyUserUpdate(ctx, id, updates, actor)
}

func (s *userService) UpdateStatus(ctx context.Context, id, status string, actor Actor) (*models.User, error) {
	return s.Update(ctx, id, UpdateUserInput{Status: &status}, actor)
}

func (s *userService) UpdateRole(ctx context.Context, id, role string, actor Actor) (*models.User, error) {
	return s.Update(ctx, id, UpdateUserInput{Role: &role}, actor)
}

// applyUserUpdate 事务内核：持锁读旧值 → 守卫 → UPDATE → 回读 → 主动清鉴权 cache。
//
// 为什么守卫必须和写入同事务、且旧值要加锁读：两道守卫（自我禁用/降级、
// 最后一名管理员）都是**读-判-写**，不加锁时两个并发请求可以各自读到
// 「还有另一个 admin」而双双把管理员降掉 → 系统一个 admin 不剩。
// 真 PG 上是行锁；sqlite 基座不渲染 FOR UPDATE（driver 明说不支持行级锁），
// 故单测只能验路径，锁本身靠 postgres dialector + sqlmock 的 SQL 文本钉住
// （同 ticket_service.Update 的口径）。
//
// M87：事务 commit 成功后立即清 JWT 路径 status cache，让该副本的下一请求不走
// cache，把 M40 ship 的 ≤30s 全副本生效 trade-off 收口为「该副本 ≈0s 生效」。
// 其他副本仍走 30s TTL（M40 trade-off 本质；真要全局近 0s 需 Redis pub/sub 广播，
// 登记 G-5-2 followup，**不**在本 round scope）。失败路径（事务回滚）不调 ——
// DB 没改，cache 不必清。
func (s *userService) applyUserUpdate(
	ctx context.Context, id string, updates map[string]interface{}, actor Actor,
) (*models.User, error) {
	var out models.User
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var target models.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&target, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		if err := checkUserUpdateGuards(tx, &target, updates, actor); err != nil {
			return err
		}
		// 只更新给出的列：handler 收的是白名单结构体，这里再限定列名，
		// 不存在「把整个对象 PUT 上来顺带覆盖 password_hash」的面。
		if err := tx.Model(&models.User{}).Where("id = ?", target.ID).
			Updates(updates).Error; err != nil {
			return err
		}
		// 回读：Updates(map) 不把新值写回 target，响应体必须是**落库后**的值
		// （否则前端乐观更新拿到的回执是旧值）。
		if err := tx.First(&out, "id = ?", target.ID).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	// M87 active invalidation: 写成功后立即清鉴权 cache，让 victim 的下一请求
	// 必读 DB 拿到最新 status（该副本 ≈0s 生效）。`invalidate` 是纯 map 操作，
	// 不会失败也不 panic —— 兜底仍是 30s TTL。
	middleware.InvalidateAuthStatusCacheForUser(id)
	out.Role = middleware.CanonicalRole(out.Role)
	return &out, nil
}

// checkUserUpdateGuards 账号处置的两道守卫。都返回 ErrForbidden（403）——
// 这不是「请求写错了」（400），而是「服务端策略不允许」，改参数重试无用。
//
//	v-1 自我禁用/自我降级：admin 点错一次就把自己锁在门外，而 identity 路由
//	    只有 admin 进得来 —— 自助恢复的路径不存在（只能进库改）。
//	v-2 最后一名可登录的管理员：禁用/降级它之后，系统**没有任何账号**能进
//	    identity 路由，只能靠 cmd/set-role 直连 DB 自救。
//
// 判据是「更新后的角色/状态」，不是「请求里出现的键」：把 admin 设成 admin
// 不算降级，把已禁用的账号再禁一次也不算（幂等请求不该被拒）。
func checkUserUpdateGuards(tx *gorm.DB, target *models.User, updates map[string]interface{}, actor Actor) error {
	resultRole := middleware.CanonicalRole(target.Role)
	if r, ok := updates["role"].(string); ok {
		resultRole = r // Update 已折叠 + 校验过
	}
	resultStatus := target.Status
	if st, ok := updates["status"].(string); ok {
		resultStatus = st
	}

	self := actor.ID != nil && *actor.ID == target.ID
	if self {
		if resultStatus != "active" {
			return fmt.Errorf("%w: 不能禁用自己的账号", ErrForbidden)
		}
		if middleware.CanonicalRole(target.Role) == middleware.RoleAdmin &&
			resultRole != middleware.RoleAdmin {
			return fmt.Errorf("%w: 不能降级自己的 admin 角色", ErrForbidden)
		}
	}

	// 目标当前是**可登录的管理员**才有「少一个管理员」的风险。
	targetCounts := middleware.CanonicalRole(target.Role) == middleware.RoleAdmin &&
		target.Status == "active"
	if targetCounts && (resultRole != middleware.RoleAdmin || resultStatus != "active") {
		var others int64
		// LOWER(TRIM(...)) 兜住存量脏值（' Admin '），与 cmd/set-role 的判据同形；
		// 只算 active 的：被禁用的管理员登不进来，不算「能自救的那个人」。
		if err := tx.Model(&models.User{}).
			Where("LOWER(TRIM(role)) = ? AND id <> ? AND status = ?",
				middleware.RoleAdmin, target.ID, "active").
			Count(&others).Error; err != nil {
			return err
		}
		if others == 0 {
			return fmt.Errorf(
				"%w: 这是最后一名可登录的管理员，禁用/降级后系统将无人能管理用户",
				ErrForbidden)
		}
	}
	return nil
}
