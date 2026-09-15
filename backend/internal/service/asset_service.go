// Package service 业务逻辑层。handler 只做参数解析和响应拼装，DB 访问与业务规则下沉到 service。
// service 通过 interface 暴露，便于在 handler 中 mock 测试。
package service

import (
	"context"
	"errors"
	"net"
	"strings"
	"time"

	"network-monitor-platform/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// 业务错误，handler 根据错误类型决定 HTTP 状态码
var (
	ErrNotFound      = errors.New("resource not found")
	ErrAlreadyExists = errors.New("resource already exists")
	ErrInvalidInput  = errors.New("invalid input")
	ErrTooManyItems  = errors.New("too many items in batch request") // 🐛 BUG#17
	// ErrInvalidState M19：请求本身合法，但与资源**当前状态**冲突（如对已解决的告警再确认）。
	// 与 ErrInvalidInput 分开：那是「请求写错了」（400），这是「来晚了/状态已经走了」（409）。
	// 混成一个会让调用方分不清「改参数重试」和「刷新后别再试」。
	ErrInvalidState = errors.New("invalid state transition")
	// ErrForbidden M61：请求合法、调用方也有权限，但**服务端策略**不允许（403）。
	// 与 ErrInvalidInput 分开的理由同上：自我禁用 / 降级最后一名管理员改参数重试无用，
	// 报 400 会让调用方去改请求体。目前只有账号处置守卫（user_service.go）用它。
	ErrForbidden = errors.New("forbidden")
	// ErrIPConflict M68：写入网卡 IP 时发现该 IP 已被**其他资产**占用。
	// 与 ErrAlreadyExists 区分：那是「asset.name 唯一冲突」（资产创建层），
	// 这是「asset_networks.ipv4_address 业务冲突」（网卡写入层）。两层都用 409 但语义不同,
	// 调用方看 message 区分。两个 error 不复用 — 未来 audit 视图要列「历史重复 IP」时,
	// 不能把 name 冲突和 IP 冲突混在一起报。
	ErrIPConflict = errors.New("ip address already in use by another asset")
)

// AssetFilter 资产列表查询条件
type AssetFilter struct {
	Keyword   string
	Status    string
	AssetType string
	Page      int
	PageSize  int
}

// AssetService 资产业务接口
type AssetService interface {
	List(ctx context.Context, f AssetFilter) (items []models.Asset, total int64, err error)
	// ListAll 返回全部资产（导出专用，不分页、不计数）。语义与 List 不同，见实现处注释。
	ListAll(ctx context.Context) (items []models.Asset, err error)
	Get(ctx context.Context, id string) (*models.Asset, []models.AssetNetwork, error)
	// Create 建资产。ipAddress 非空（trim 后）时在**同一事务**里补一张网卡：
	// IPv4 落 ipv4_address、IPv6 落 ipv6_address（M64 / G-Asset-NetworksPersist）。
	Create(ctx context.Context, asset *models.Asset, ipAddress *string) error
	// Update 部分更新。ipAddress 非 nil 且 trim 后非空时，在**同一事务**里把第一张网卡的 IP
	// 改成这个值（v4 落 ipv4_address、v6 落 ipv6_address）；nil/空串 = 不改（M66 / G-Asset-UpdateIpPersist）。
	Update(ctx context.Context, id string, updates map[string]interface{}, ipAddress *string) (*models.Asset, error)
	Delete(ctx context.Context, id string) error
	// B4: 软退役 — 把 AssetNetwork.IP* 清空, 存档到 Asset.LastKnownIP*, 释放 IP 给新设备用
	Retire(ctx context.Context, id string, reason string, userID uuid.UUID) (*models.Asset, []models.AssetNetwork, error)
	// M58: 批量退役 — 单请求替代前端原本的 N 次串行 Retire（100 项 100 RTT → 1 RTT）。
	// 每条 id 走与 Retire 相同的内核；部分失败进 failed，不影响已成功的条目。
	BulkRetire(ctx context.Context, ids []string, reason string, userID uuid.UUID) (succeeded []string, failed map[string]string, err error)
	// B4: 恢复 — 反向: last_known_ip* 写回 AssetNetwork.IP*, 清空 retired_*
	Restore(ctx context.Context, id string) (*models.Asset, []models.AssetNetwork, error)
}

type assetService struct {
	db *gorm.DB
}

// NewAssetService 创建 AssetService
func NewAssetService(db *gorm.DB) AssetService {
	return &assetService{db: db}
}

func (s *assetService) List(ctx context.Context, f AssetFilter) ([]models.Asset, int64, error) {
	q := s.db.WithContext(ctx).Model(&models.Asset{})

	if f.Keyword != "" {
		kw := "%" + strings.TrimSpace(f.Keyword) + "%"
		q = q.Where("name ILIKE ? OR asset_tag ILIKE ? OR sn ILIKE ?", kw, kw, kw)
	}
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	if f.AssetType != "" {
		q = q.Where("asset_type = ?", f.AssetType)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	page := f.Page
	if page < 1 {
		page = 1
	}
	pageSize := f.PageSize
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 500 {
		pageSize = 500 // 防止一次性拉过大
	}

	var items []models.Asset
	if err := q.Offset((page - 1) * pageSize).Limit(pageSize).Order("created_at DESC").Find(&items).Error; err != nil {
		return nil, 0, err
	}
	// M63: 投影主 IP（虚拟字段，不是列；见 injectPrimaryIPs 的取舍说明）。
	if err := s.injectPrimaryIPs(ctx, items); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// ListAll 返回全部资产（导出专用，不分页、不计数）。
//
// 为什么不让导出复用 List：List 的语义是「给我第 N 页」，pageSize 有 500 硬顶
// （防止交互式分页被一次拉爆）。导出要的是「给我全部」—— 把 PageSize 调大只会
// 被那个硬顶接管，导出行为一个字不变（docs/FIX-PLAN-EXPORT-FIDELITY.md §2.1）。
//
// 与 List 的等价性契约：本轮两者都没有过滤参数，故「ListAll 的集合 == List 无过滤
// 时逐页拼起来的集合」。将来给 List 加过滤/软删除/scope 必须同步改这里，
// 否则导出会多返回行，推翻需求 §3.4 的「导出不构成提权」论证。
//
// 排序加 id 兜底：created_at 同刻时（批量导入是常态）单靠 created_at 顺序不稳定，
// 导出产物就没法做 diff —— 对账场景要的就是可 diff（需求 §0）。
func (s *assetService) ListAll(ctx context.Context) ([]models.Asset, error) {
	var items []models.Asset
	if err := s.db.WithContext(ctx).Model(&models.Asset{}).
		Order("created_at DESC, id DESC").
		Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

func (s *assetService) Get(ctx context.Context, id string) (*models.Asset, []models.AssetNetwork, error) {
	var asset models.Asset
	if err := s.db.WithContext(ctx).First(&asset, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, ErrNotFound
		}
		return nil, nil, err
	}

	networks, err := s.listNetworks(ctx, s.db, asset.ID)
	if err != nil {
		return nil, nil, err
	}
	// M63: 网卡已经在手，直接投影主 IP（不再查一次库）。
	asset.IpAddress = primaryIP(networks)
	return &asset, networks, nil
}

// listNetworks 取某资产的全部网卡，按**稳定顺序**（最早建的在前）。
//
// 为什么要显式 ORDER BY：`Retire` 把「第一张有 IP 的网卡」的地址存进 `last_known_ip*`，
// `Restore` 再把它写回「第一张网卡」—— 两处都依赖「第一张」的含义，而 Postgres 对不带
// ORDER BY 的查询**不保证行序**：走 asset_id 索引时按 (asset_id, ctid) 排，而 Retire 把
// N 张网卡全 UPDATE 了一遍，ctid 全变 → 退役前后两次 SELECT 的「第一行」可能不是同一张卡，
// 恢复时 IP 就落到别的网卡上。排序把「第一张」钉成「最早创建的那张」，两次调用说的是同一张。
//
// `id` 只是决胜列（同一批插入的 created_at 可能相同），与 alert/ticket 的
// `created_at DESC, id DESC` 二元组排序同一套约定。
// db 由调用方指定：Get/Restore 传 s.db，Retire/BulkRetire 传事务句柄 ——
// 网卡快照的读必须和后续清空 IP 的写落在同一事务/快照里。
func (s *assetService) listNetworks(ctx context.Context, db *gorm.DB, assetID uuid.UUID) ([]models.AssetNetwork, error) {
	var networks []models.AssetNetwork
	err := db.WithContext(ctx).
		Where("asset_id = ?", assetID).
		Order("created_at ASC, id ASC").
		Find(&networks).Error
	return networks, err
}

// ==================== M63: IP 投影 (G-UI-AssetIpPersistence) ====================
//
// 判据（pickPrimaryIP / primaryIP）在 asset_ip.go —— 它是这些投影与复盘报告头
// （postmortem_service.fetchIP）共用的唯一一份实现，本文件只负责**取数**。

// injectPrimaryIPs 给**一页**资产填 `IpAddress`：一条 IN 查询取回这些资产的全部网卡，
// 按 asset_id 分组后逐资产取主 IP。入参是切片，元素就地改（调用方拿到同一份 backing array）。
//
// 为什么不用 GORM `Joins` 一步到位：assets 与 asset_networks 是 1:N，join 会把有 N 张
// 网卡的资产复制成 N 行 —— 「第 N 页」的页大小和 `total` 的含义当场改变（一条资产多行），
// 前端 AssetTable 也会出现重复行。多一条 IN 查询换来行数语义不变，代价与一页
// （≤500 条）同阶。
//
// 为什么不逐条查：N+1。一条 IN + 内存分组，DB 往返恒为 1。
//
// 排序与 listNetworks 同口径（created_at ASC, id ASC）：IN 结果里**每个资产的子序列**
// 保持这个相对顺序，故「第一张网卡」的含义与详情页/退役恢复路径一致（T-45：没有
// ORDER BY 的「第一张」是没有定义的）。
func (s *assetService) injectPrimaryIPs(ctx context.Context, items []models.Asset) error {
	if len(items) == 0 {
		return nil // 不给空切片发 `IN ()`（空页多一次往返，且部分方言不接受空 IN）
	}
	ids := make([]uuid.UUID, 0, len(items))
	for i := range items {
		ids = append(ids, items[i].ID)
	}

	var networks []models.AssetNetwork
	if err := s.db.WithContext(ctx).
		Where("asset_id IN ?", ids).
		Order("created_at ASC, id ASC").
		Find(&networks).Error; err != nil {
		return err
	}

	byAsset := make(map[uuid.UUID][]models.AssetNetwork, len(ids))
	for _, n := range networks {
		byAsset[n.AssetID] = append(byAsset[n.AssetID], n)
	}
	for i := range items {
		// 没有网卡的资产：map 里没有键 → nil 切片 → primaryIP 返回 nil（`ip_address: null`）。
		items[i].IpAddress = primaryIP(byAsset[items[i].ID])
	}
	return nil
}

// Create 建资产（M64 / G-Asset-NetworksPersist：表单的 `ip_address` 落第一张网卡）。
//
// 为什么 IP 是**独立入参**而不是 `models.Asset` 上的字段：`assets` 表没有 `ip_address` 列
// —— IP 属于网卡，两份存储必然漂移（退役改的是 `asset_networks`，见 B4）；
// `models.Asset.IpAddress` 是 `gorm:"-"` 的只读投影字段。绑定进模型 = 被 GORM 静默丢掉
// （M63 之前的现状：前端表单里的 IP 写完没影）。
//
// 为什么 v4/v6 分列写：两列并存**正是为区分族**。把 v6 塞进 `ipv4_address` 会让
// `pickPrimaryIP` 的判据（先第一个非空 v4，否则第一个非空 v6）读出错误结果 ——
// 列表/详情显示 v6 时被当成 v4。
//
// 为什么包事务：资产行与网卡行必须同生。分两次写时第二条失败会留下「资产建了、IP 丢了」
// 的半落状态，而调用方拿到 500 会当整条失败去重试 → 撞上 name 唯一约束（409）。
func (s *assetService) Create(ctx context.Context, asset *models.Asset, ipAddress *string) error {
	if asset == nil || strings.TrimSpace(asset.Name) == "" {
		return ErrInvalidInput
	}
	// 与 handler 同款兜底：handler 已用 invalidIPAddress 422 挡在入口；这里再判一次是给
	// **直接调用方**（tests/db_smoke_test.go、将来的导入器）—— 宁可报错，也不要把脏 IP 写进网卡表。
	if ipAddress != nil {
		ip := strings.TrimSpace(*ipAddress)
		if ip != "" && net.ParseIP(ip) == nil {
			return ErrInvalidInput
		}
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(asset).Error; err != nil {
			if isUniqueViolation(err) {
				return ErrAlreadyExists
			}
			return err
		}
		return s.updateFirstNetworkIP(tx, asset.ID, ipAddress)
	})
}

// updateFirstNetworkIP 在 tx 内更新（或创建）指定资产的第一张网卡的 IP。
//
//   - ipAddress == nil 或空字符串：不改网卡（未提供/明确不改）。
//   - ipAddress 非空但 net.ParseIP 失败：返 ErrInvalidInput（handler 映 422）。
//   - IP 已被**其他资产**占用：返 ErrIPConflict（handler 映 409）。M68 加的守卫 ——
//     业务规则而非 DB unique 约束，留「退役释放后能否复用」的语义灵活性。
//   - 资产无网卡：创建 eth0（与 Create 对齐）。
//   - 有网卡：v4/v6 分流落 IPv4Address/IPv6Address，另一列清空（避免「v4 字段残留 v6 历史」）。
//
// 第一张网卡的判据沿用 M45 T-45：`created_at ASC, id ASC`，与 listNetworks 一致，
// 否则「更新的是这张、读取的是那张」的漂移面又会出现。
//
// M66：从 Create 抽出供 Update 复用 —— POST/PUT 的 IP 写入路径走同一函数，
// 同一判据不再有两份实现。
//
// M68：在 SELECT 网卡**之前**先做 IP 占用检查（排除自己），原因有两个：
//   1. INSERT 分支（isNew=true）下当前 SELECT 还没拿到 network.ID，self-exclude 必须用
//      assetID 而不是 network.ID —— 全表查更稳。
//   2. 提前 fail 比拿到网卡后再发现冲突少一步往返；tx 里 SELECT 多查一次不增 commit 数。
func (s *assetService) updateFirstNetworkIP(tx *gorm.DB, assetID uuid.UUID, ipAddress *string) error {
	if ipAddress == nil {
		return nil
	}
	ip := strings.TrimSpace(*ipAddress)
	if ip == "" {
		return nil
	}
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return ErrInvalidInput
	}
	// M68: v4 业务冲突守卫（v6 留 future，理由见 intent-M68.md Decision 3）。
	// parsed.To4() 非 nil ⇒ IPv4/4-in-6 映射形式；后者落 ipv4_address 时已归一为 v4 文本，
	// 所以这里直接拿归一后的字符串查。self-exclude 必须放在 INSERT 之前，因为 INSERT 分支
	// 这时还没网络行 —— 用 assetID 排除。
	if v4 := parsed.To4(); v4 != nil {
		v4Str := v4.String()
		var taken struct {
			ID uuid.UUID
		}
		err := tx.Raw(
			`SELECT id FROM asset_networks WHERE ipv4_address = ? AND asset_id <> ? AND ipv4_address <> '' LIMIT 1`,
			v4Str, assetID,
		).Scan(&taken).Error
		// 0 行 = 没人占 = OK；tx.Raw + Scan 在 0 行时返 ErrRecordNotFound，与「占用了」
		// 分歧清楚，不算异常。ErrRecordNotFound 之外的错误才返。
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if taken.ID != uuid.Nil {
			return ErrIPConflict
		}
	} else {
		// M69：v6 业务冲突守卫，复用 M68 v4 SELECT pattern。
		// 业务上同 IPv6 跨资产也是真摩擦（link-local / 唯一本地 / 全局单播都可能撞）。
		v6Str := parsed.String()
		var taken struct {
			ID uuid.UUID
		}
		err := tx.Raw(
			`SELECT id FROM asset_networks WHERE ipv6_address = ? AND asset_id <> ? AND ipv6_address <> '' LIMIT 1`,
			v6Str, assetID,
		).Scan(&taken).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if taken.ID != uuid.Nil {
			return ErrIPConflict
		}
	}
	var network models.AssetNetwork
	err := tx.Where("asset_id = ?", assetID).
		Order("created_at ASC, id ASC").
		First(&network).Error
	isNew := false
	if errors.Is(err, gorm.ErrRecordNotFound) {
		network = models.AssetNetwork{
			AssetID:       assetID,
			InterfaceName: "eth0",
			InterfaceType: "ethernet",
			Status:        "unknown",
		}
		isNew = true
	} else if err != nil {
		return err
	}
	// To4() 对 **4-in-6 映射形式**（`::ffff:1.2.3.4`）也非 nil —— 那种地址本来就是
	// 一个 IPv4，落 ipv4_address 并归一成 `1.2.3.4` 是正确读法。
	if v4 := parsed.To4(); v4 != nil {
		network.IPv4Address = v4.String()
		network.IPv6Address = ""
	} else {
		network.IPv4Address = ""
		network.IPv6Address = parsed.String()
	}
	if isNew {
		return tx.Create(&network).Error
	}
	return tx.Model(&models.AssetNetwork{}).
		Where("id = ?", network.ID).
		Updates(map[string]interface{}{
			"ipv4_address": network.IPv4Address,
			"ipv6_address": network.IPv6Address,
		}).Error
}

func (s *assetService) Update(ctx context.Context, id string, updates map[string]interface{}, ipAddress *string) (*models.Asset, error) {
	if len(updates) == 0 && ipAddress == nil {
		asset, _, err := s.Get(ctx, id)
		return asset, err
	}
	assetUUID, err := uuid.Parse(id)
	if err != nil {
		return nil, ErrInvalidInput
	}
	var result *models.Asset
	txErr := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var asset models.Asset
		if err := tx.First(&asset, "id = ?", assetUUID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		if len(updates) > 0 {
			if err := tx.Model(&asset).Updates(updates).Error; err != nil {
				// net_box_id 上的唯一索引（migrations/000015）让 PATCH 也能撞 23505：
				// 与 Create 一致映射成 409，别让客户端看到 500（审计 F-8）。
				if isUniqueViolation(err) {
					return ErrAlreadyExists
				}
				return err
			}
		}
		if err := s.updateFirstNetworkIP(tx, asset.ID, ipAddress); err != nil {
			return err
		}
		result = &asset
		return nil
	})
	if txErr != nil {
		return nil, txErr
	}
	return result, nil
}

func (s *assetService) Delete(ctx context.Context, id string) error {
	return s.db.WithContext(ctx).Delete(&models.Asset{}, "id = ?", id).Error
}

// B4: Retire 软退役
// - 把第一张网卡的 IPv4/IPv6 复制到 asset.LastKnownIP4/6 (作为"历史 IP"快照)
// - 清空该资产所有 AssetNetwork 的 IPv4/IPv6
// - asset.status = 'retired', retired_at = now, retired_by = userID, retired_reason
// - 整段包事务: 任一步失败回滚 (避免半退役状态)
func (s *assetService) Retire(ctx context.Context, id string, reason string, userID uuid.UUID) (*models.Asset, []models.AssetNetwork, error) {
	var asset *models.Asset
	var networks []models.AssetNetwork
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		a, n, e := s.retireCore(ctx, tx, id, reason, userID)
		if e != nil {
			return e
		}
		asset, networks = a, n
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return asset, networks, nil
}

// retireCore 单条退役的**事务内核**：全部读写都走传入的 db。
//
// Retire 传外层 tx；BulkRetire 传每个 id 各自的 SAVEPOINT。两条路径共用同一实现，
// 退役语义（last_known_ip* 快照、清空网卡 IP、拒绝重复退役）不会分叉。
//
// 读也进事务是刻意的：网卡快照与随后的清空 IP 必须在同一快照里，否则并发改网卡会丢 last_known。
func (s *assetService) retireCore(ctx context.Context, db *gorm.DB, id string, reason string, userID uuid.UUID) (*models.Asset, []models.AssetNetwork, error) {
	uid, err := uuid.Parse(id)
	if err != nil {
		return nil, nil, ErrInvalidInput
	}

	var asset models.Asset
	// First(uid) 走纯 PK, 不带 "id = ?" 条件避免 gorm 重复 bind (uuid PK 字段)
	if err := db.WithContext(ctx).First(&asset, uid).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, ErrNotFound
		}
		return nil, nil, err
	}

	// 已退役 → 拒绝重复退役 (idempotent-friendly: 返 ErrInvalidInput 让 handler 转 400)
	if asset.Status == "retired" {
		return nil, nil, ErrInvalidInput
	}

	networks, err := s.listNetworks(ctx, db, uid)
	if err != nil {
		return nil, nil, err
	}

	// 取"主 IP"作为 last_known: v4 / v6 各自第一张非空。
	// M63: 复用 asset_ip.pickPrimaryIP —— 这段循环曾在这里第三遍实现同一条判据
	// （asset_ip.go 的注释里那份「唯一出口」在当时并不成立）。Retire 存下的地址与
	// 列表/详情显示的主 IP 现在说的是同一个口径。
	// 返回的指针指向 networks 的元素：下面只把它们当**只读**快照绑定进 UPDATE，
	// 不在写库后回读内存（末尾那次 listNetworks 换的是新切片，不动这里的内存）。
	lastIP4, lastIP6 := pickPrimaryIP(networks)

	now := time.Now()
	trimmedReason := strings.TrimSpace(reason)
	asset.Status = "retired"
	asset.LastKnownIP4 = lastIP4
	asset.LastKnownIP6 = lastIP6
	asset.RetiredAt = &now
	asset.RetiredBy = &userID
	asset.RetiredReason = &trimmedReason

	// 写 asset 更新 (事务由调用方持有)
	if err := db.WithContext(ctx).Model(&asset).Updates(map[string]interface{}{
		"status":         asset.Status,
		"last_known_ip4": asset.LastKnownIP4,
		"last_known_ip6": asset.LastKnownIP6,
		"retired_at":     asset.RetiredAt,
		"retired_by":     asset.RetiredBy,
		"retired_reason": asset.RetiredReason,
	}).Error; err != nil {
		return nil, nil, err
	}
	// 清空 networks 的 IP 字段 (其他字段保留: mac / interface_name / connected_to 等)
	if err := db.WithContext(ctx).Model(&models.AssetNetwork{}).
		Where("asset_id = ?", uid).
		Updates(map[string]interface{}{
			"ipv4_address": "",
			"ipv6_address": "", // 列名由字段名 IPv6Address 推导: ipv6_address（`ip_v address` 只是 JSON tag，不是列）
		}).Error; err != nil {
		return nil, nil, err
	}

	// 重读 networks 返给 handler (gorm.Model.Updates 不会刷新内存 struct)
	if networks, err = s.listNetworks(ctx, db, uid); err != nil {
		return nil, nil, err
	}
	return &asset, networks, nil
}

// M58: bulkRetireReasonMax 批量退役 reason 的截断上限（runes）。
//
// retired_reason 是 TEXT 不会超长，但批量入参是外部输入：上限对齐审计 path 的 500 口径，
// 免得一条请求把 500KB 的"原因"塞进每一行。
const bulkRetireReasonMax = 500

// M58: BulkRetire 批量软退役 (G-Asset-BulkRetireEndpoint)
//   - 复用 retireCore，每条 id 的语义与单条 Retire 完全一致；
//   - 单次调用 = 一个外层 DB tx，每个 id 再各自套一个 SAVEPOINT → 单条失败（含真实 DB 错误）
//     只回滚该 savepoint，其余 id 照常提交。这是"失败一个不影响其它"在 PG 里唯一成立的做法：
//     没有 savepoint 时，一条语句报错会让整个 tx 进入 aborted 态，后续语句全部 25P02。
//   - 返回 succeeded（按入参顺序）+ failed（id → 文案）。只有外层 tx 自身（Begin/Commit）
//     失败才返回 err != nil —— 那种情况下不能谎报"部分成功"（票面上成功了实际全被回滚）。
//   - 不做 retry / 熔断（YAGNI）：失败如实进 failed 报告。
func (s *assetService) BulkRetire(ctx context.Context, ids []string, reason string, userID uuid.UUID) ([]string, map[string]string, error) {
	reason = truncateRunes(strings.TrimSpace(reason), bulkRetireReasonMax)

	succeeded := make([]string, 0, len(ids))
	failed := make(map[string]string)

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, id := range ids {
			err := tx.Transaction(func(itx *gorm.DB) error {
				_, _, e := s.retireCore(ctx, itx, id, reason, userID)
				return e
			})
			if err != nil {
				failed[id] = bulkRetireErrMsg(err)
				continue
			}
			succeeded = append(succeeded, id)
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return succeeded, failed, nil
}

// bulkRetireErrMsg 把内部错误翻成可直接展示的文案：业务错给中文，未知错保留原文便于排查。
func bulkRetireErrMsg(err error) string {
	switch {
	case errors.Is(err, ErrNotFound):
		return "资产不存在"
	case errors.Is(err, ErrInvalidInput):
		return "无法退役（资产已退役或参数无效）"
	default:
		return err.Error()
	}
}

// B4: Restore 反向
// - 把 asset.LastKnownIP* 写回第一张网卡 (IPv4 → IPv4Address, IPv6 → IPv6Address)
// - 清空 retired_*, status → 'active'
// - 事务包保证一致性
func (s *assetService) Restore(ctx context.Context, id string) (*models.Asset, []models.AssetNetwork, error) {
	uid, err := uuid.Parse(id)
	if err != nil {
		return nil, nil, ErrInvalidInput
	}

	var asset models.Asset
	if err := s.db.WithContext(ctx).First(&asset, "id = ?", uid).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, ErrNotFound
		}
		return nil, nil, err
	}

	if asset.Status != "retired" {
		return nil, nil, ErrInvalidInput // 非退役状态不能恢复
	}

	networks, err := s.listNetworks(ctx, s.db, uid)
	if err != nil {
		return nil, nil, err
	}

	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 写回 IP：IPv4/IPv6 都回到**第一张**网卡（即 listNetworks 排序后最早创建的那张，
		// 也就是 Retire 存 last_known_ip* 时读的那张）。双栈落在同一张卡是常态。
		//
		// 早先这里 IPv6 那支漏了「只有第一张」的守卫：N 张网卡的资产恢复后**每张卡都被写上
		// 同一个 IPv6** —— 一个地址同时挂在 N 个接口上。IPv4 那支有 i == 0，IPv6 没有，
		// 是不对称漏写；两支护在一起才不会再次分叉。
		//
		// 已知局限（如实记录，别当它不存在）：Retire 只记 IP、不记它原来在哪张卡上
		// （last_known_ip* 是**资产级**列），所以 IP 原属 NIC[1] 时恢复会落到 NIC[0]。
		// 要精确还原得给 assets 加来源列，属独立决策（见 docs/FIX-PLAN-UI-PERF.md §8 登记）。
		if len(networks) > 0 {
			if asset.LastKnownIP4 != nil {
				networks[0].IPv4Address = *asset.LastKnownIP4
			}
			if asset.LastKnownIP6 != nil {
				networks[0].IPv6Address = *asset.LastKnownIP6
			}
			if err := tx.Model(&models.AssetNetwork{}).
				Where("id = ?", networks[0].ID).
				Updates(map[string]interface{}{
					"ipv4_address": networks[0].IPv4Address,
					"ipv6_address": networks[0].IPv6Address,
				}).Error; err != nil {
				return err
			}
		}

		// 清 asset 退役字段
		if err := tx.Model(&asset).Updates(map[string]interface{}{
			"status":         "active",
			"last_known_ip4": nil,
			"last_known_ip6": nil,
			"retired_at":     nil,
			"retired_by":     nil,
			"retired_reason": nil,
		}).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	// 重读
	if networks, err = s.listNetworks(ctx, s.db, uid); err != nil {
		return nil, nil, err
	}
	// 重新读 asset 拿最终状态 — 用新 struct 实例, 避免事务 Model.Updates 把 asset.ID 写回后再 First 触发重复 bind
	var freshAsset models.Asset
	if err := s.db.WithContext(ctx).First(&freshAsset, uid).Error; err != nil {
		return nil, nil, err
	}
	return &freshAsset, networks, nil
}
