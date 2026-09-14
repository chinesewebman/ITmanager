package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"network-monitor-platform/internal/apierr"
	"network-monitor-platform/internal/middleware"
	"network-monitor-platform/internal/models"
	"network-monitor-platform/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// UserHandler 用户 HTTP handler。读路径（List/Get）给 admin 看账号清单；
// 写路径（M61：Update/UpdateStatus/UpdateRole）是账号处置 —— 启用/禁用、改角色、
// 置强改密。三个写入端点挂 canIdentity（仅 admin），见 routes.go。
type UserHandler struct {
	svc service.UserService
}

func NewUserHandler(svc service.UserService) *UserHandler {
	return &UserHandler{svc: svc}
}

func (h *UserHandler) ListUsers(c *gin.Context) {
	// 🐛 BUG#26: 改成分页，避免全表返撑爆内存
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	users, total, err := h.svc.List(c.Request.Context(), page, pageSize)
	if err != nil {
		apierr.Internal(c, "获取用户列表失败", err)
		return
	}
	// 词表归一：存量库可能存遗留别名 operator/viewer，出站一律折叠（ADR-0005 决策 2）
	for i := range users {
		users[i].Role = middleware.CanonicalRole(users[i].Role)
	}
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"items":     users,
			"total":     total,
			"page":      page,
			"page_size": pageSize,
		},
	})
}

func (h *UserHandler) GetUser(c *gin.Context) {
	u, err := h.svc.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		if errors.Is(err, service.ErrNotFound) {
			apierr.NotFound(c, "用户不存在")
			return
		}
		apierr.Internal(c, "获取用户失败", err)
		return
	}
	u.Role = middleware.CanonicalRole(u.Role) // 词表归一，同 ListUsers
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": u})
}

// ==================== M61 账号处置（admin） ====================
//
// 三个写入端点的分工：
//
//	PUT   /users/:id          局部更新（status / role / must_change_password 任选）
//	PATCH /users/:id/status   只改状态（前端状态 Switch）
//	PATCH /users/:id/role     只改角色（前端角色下拉）
//
// 后两个是**单字段**端点的理由：整对象 PUT 的语义是「用请求体替换资源」，而前端表格里
// 点一下 Switch 只知道自己要改 status —— PUT 整行会把同时刻别人改过的角色一起写回旧值
// （lost update）。窄端点让「这次请求想改哪个字段」无歧义。
// 三个 handler 共用 service.Update 一份实现（守卫/事务/回读不分叉）。
//
// **不做 DELETE**：账号走 status=inactive，不硬删 —— 审计要求保留操作历史
// （audit_logs.user_id 取值来自 users，硬删后历史里的操作人再也查不到是谁）。

// updateUserRequest PUT /users/:id 的请求体。
//
// 三个字段都是指针：nil = 本次不动该字段（`false` / `""` 都是合法取值，必须与
// 「未提供」可区分）。刻意不用 map[string]interface{} —— gorm 的 Updates(map) 会按
// **列名或 Go 字段名**解析键（H-1 教训：{"PasswordHash": …} 能绕过只匹配小写列名的
// 白名单），结构体天然只收这三列。
type updateUserRequest struct {
	Status             *string `json:"status"`
	Role               *string `json:"role"`
	MustChangePassword *bool   `json:"must_change_password"`
}

type updateUserStatusRequest struct {
	Status string `json:"status"`
}

type updateUserRoleRequest struct {
	Role string `json:"role"`
}

// decodeUserBody 严格解码请求体：未知键 / 类型不符 / 尾随内容一律 400。
//
// 为什么不用 c.ShouldBindJSON：它**静默忽略**未知键 —— 调用方 PUT
// {"email": "new@example.com"} 会拿到 200 而邮箱一字未改（「写了不生效」的静默失败，
// T-72 同族）。这里 DisallowUnknownFields 把「请求里带了不可改的列」变成显式 400，
// 与工单 M17「系统维护列不可写」同一口径。
//
// 文案是静态的：json 的解码错误里带调用方可控的字段名，回显进 400 body 就是反射面
// （G-33 M1 的教训）。
func decodeUserBody(c *gin.Context, dst any) bool {
	dec := json.NewDecoder(c.Request.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		apierr.BadRequest(c, "请求体不是合法 JSON，或含本端点不接受的字段")
		return false
	}
	// 尾随内容（`{…}{…}` 或 `{…}garbage`）：只有第一个 JSON 文档是权威的。静默忽略
	// 会让「两个请求体粘在一起」这种客户端/代理 bug 看起来成功了。
	if err := dec.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		apierr.BadRequest(c, "请求体含多余内容")
		return false
	}
	return true
}

// userPathID 取并校验路径上的 :id（与 alertPathID 同形，那份注释解释了为什么必须校验：
// 裸字符串进 gorm 的 UUID 列比较 → PG 22P02 → 经 apierr.Internal 报成 500，把
// 「调用方 id 写错」报成服务端故障）。各 handler 保留自己的文案，与
// oncall / runbook / alert_suppression 的既有做法一致。
func userPathID(c *gin.Context) (string, bool) {
	id := c.Param("id")
	if _, err := uuid.Parse(id); err != nil {
		apierr.BadRequest(c, "ID 格式错误")
		return "", false
	}
	return id, true
}

// UpdateUser PUT /api/users/:id
func (h *UserHandler) UpdateUser(c *gin.Context) {
	id, ok := userPathID(c)
	if !ok {
		return
	}
	// 审计动作名：默认 ActionFunc 取 HTTP method，而三个端点都是 PUT/PATCH，
	// 审计页的「动作」列要能读出改的是状态还是角色。失败路径也带上（取证价值同成功）。
	c.Set("audit_action", "update_user")
	var req updateUserRequest
	if !decodeUserBody(c, &req) {
		return
	}
	if req.Status == nil && req.Role == nil && req.MustChangePassword == nil {
		// 空的 `{}` 不是「改了零个字段」，而是「这次请求没有意义」：放行会回一个 200 加
		// 当前值，调用方以为改成功了（同「写了不生效」的静默失败）。
		apierr.BadRequest(c, "请求体至少要包含 status / role / must_change_password 之一")
		return
	}
	u, err := h.svc.Update(c.Request.Context(), id, service.UpdateUserInput{
		Status:             req.Status,
		Role:               req.Role,
		MustChangePassword: req.MustChangePassword,
	}, actorFromContext(c))
	h.writeUser(c, u, err)
}

// UpdateUserStatus PATCH /api/users/:id/status
func (h *UserHandler) UpdateUserStatus(c *gin.Context) {
	id, ok := userPathID(c)
	if !ok {
		return
	}
	c.Set("audit_action", "update_user_status")
	var req updateUserStatusRequest
	if !decodeUserBody(c, &req) {
		return
	}
	if req.Status == "" {
		apierr.BadRequest(c, "status 必填")
		return
	}
	u, err := h.svc.UpdateStatus(c.Request.Context(), id, req.Status, actorFromContext(c))
	h.writeUser(c, u, err)
}

// UpdateUserRole PATCH /api/users/:id/role
func (h *UserHandler) UpdateUserRole(c *gin.Context) {
	id, ok := userPathID(c)
	if !ok {
		return
	}
	c.Set("audit_action", "update_user_role")
	var req updateUserRoleRequest
	if !decodeUserBody(c, &req) {
		return
	}
	if req.Role == "" {
		apierr.BadRequest(c, "role 必填")
		return
	}
	u, err := h.svc.UpdateRole(c.Request.Context(), id, req.Role, actorFromContext(c))
	h.writeUser(c, u, err)
}

// writeUser 三个写入 handler 共用的收口：错误映射 + 出站词表归一 + 响应。
//
// 错误分类的判据是**调用方该做什么**（不是服务端内部怎么实现）：
//   - ErrNotFound     404：目标用户不存在
//   - ErrInvalidInput 400：请求写错了（枚举/词表越界），改参数重试有用
//   - ErrForbidden    403：策略拒绝（自我禁用 / 降级最后一名管理员），改参数无用
//   - 其他             500：真故障
func (h *UserHandler) writeUser(c *gin.Context, u *models.User, err error) {
	if err != nil {
		switch {
		case errors.Is(err, service.ErrNotFound):
			apierr.NotFound(c, "用户不存在")
		case errors.Is(err, service.ErrInvalidInput):
			apierr.BadRequest(c, err.Error()) // 原因由 service 给出（枚举/词表越界）
		case errors.Is(err, service.ErrForbidden):
			apierr.Forbidden(c, err.Error())
		default:
			apierr.Internal(c, "更新用户失败", err)
		}
		return
	}
	u.Role = middleware.CanonicalRole(u.Role) // 出站归一，同 List/Get
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": u})
}
