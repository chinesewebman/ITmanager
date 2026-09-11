// Package service 业务逻辑层。handler 只做参数解析和响应拼装，DB 访问与业务规则下沉到 service。
// service 通过 interface 暴露，便于在 handler 中 mock 测试。
package service

import (
	"context"
	"errors"
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
	Create(ctx context.Context, asset *models.Asset) error
	Update(ctx context.Context, id string, updates map[string]interface{}) (*models.Asset, error)
	Delete(ctx context.Context, id string) error
	// B4: 软退役 — 把 AssetNetwork.IP* 清空, 存档到 Asset.LastKnownIP*, 释放 IP 给新设备用
	Retire(ctx context.Context, id string, reason string, userID uuid.UUID) (*models.Asset, []models.AssetNetwork, error)
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

	networks, err := s.listNetworks(ctx, asset.ID)
	if err != nil {
		return nil, nil, err
	}
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
func (s *assetService) listNetworks(ctx context.Context, assetID uuid.UUID) ([]models.AssetNetwork, error) {
	var networks []models.AssetNetwork
	err := s.db.WithContext(ctx).
		Where("asset_id = ?", assetID).
		Order("created_at ASC, id ASC").
		Find(&networks).Error
	return networks, err
}

func (s *assetService) Create(ctx context.Context, asset *models.Asset) error {
	if asset == nil || strings.TrimSpace(asset.Name) == "" {
		return ErrInvalidInput
	}
	if err := s.db.WithContext(ctx).Create(asset).Error; err != nil {
		if isUniqueViolation(err) {
			return ErrAlreadyExists
		}
		return err
	}
	return nil
}

func (s *assetService) Update(ctx context.Context, id string, updates map[string]interface{}) (*models.Asset, error) {
	if len(updates) == 0 {
		asset, _, err := s.Get(ctx, id)
		return asset, err
	}
	var asset models.Asset
	if err := s.db.WithContext(ctx).First(&asset, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if err := s.db.WithContext(ctx).Model(&asset).Updates(updates).Error; err != nil {
		// net_box_id 上的唯一索引（migrations/000015）让 PATCH 也能撞 23505：
		// 与 Create 一致映射成 409，别让客户端看到 500（审计 F-8）。
		if isUniqueViolation(err) {
			return nil, ErrAlreadyExists
		}
		return nil, err
	}
	return &asset, nil
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
	uid, err := uuid.Parse(id)
	if err != nil {
		return nil, nil, ErrInvalidInput
	}

	var asset models.Asset
	// First(uid) 走纯 PK, 不带 "id = ?" 条件避免 gorm 重复 bind (uuid PK 字段)
	if err := s.db.WithContext(ctx).First(&asset, uid).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, ErrNotFound
		}
		return nil, nil, err
	}

	// 已退役 → 拒绝重复退役 (idempotent-friendly: 返 ErrInvalidInput 让 handler 转 400)
	if asset.Status == "retired" {
		return nil, nil, ErrInvalidInput
	}

	networks, err := s.listNetworks(ctx, uid)
	if err != nil {
		return nil, nil, err
	}

	// 取"主 IP"作为 last_known (第一张有 IPv4 的网卡; 都没就空)
	var lastIP4, lastIP6 *string
	for _, n := range networks {
		if lastIP4 == nil && n.IPv4Address != "" {
			ip := n.IPv4Address
			lastIP4 = &ip
		}
		if lastIP6 == nil && n.IPv6Address != "" {
			ip := n.IPv6Address
			lastIP6 = &ip
		}
		if lastIP4 != nil && lastIP6 != nil {
			break
		}
	}

	now := time.Now()
	trimmedReason := strings.TrimSpace(reason)
	asset.Status = "retired"
	asset.LastKnownIP4 = lastIP4
	asset.LastKnownIP6 = lastIP6
	asset.RetiredAt = &now
	asset.RetiredBy = &userID
	asset.RetiredReason = &trimmedReason

	// 事务: 1) 写 asset 更新 2) 清空所有 AssetNetwork.IP*
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&asset).Updates(map[string]interface{}{
			"status":         asset.Status,
			"last_known_ip4": asset.LastKnownIP4,
			"last_known_ip6": asset.LastKnownIP6,
			"retired_at":     asset.RetiredAt,
			"retired_by":     asset.RetiredBy,
			"retired_reason": asset.RetiredReason,
		}).Error; err != nil {
			return err
		}
		// 清空 networks 的 IP 字段 (其他字段保留: mac / interface_name / connected_to 等)
		if err := tx.Model(&models.AssetNetwork{}).
			Where("asset_id = ?", uid).
			Updates(map[string]interface{}{
				"ipv4_address": "",
				"ipv6_address": "", // 列名由字段名 IPv6Address 推导: ipv6_address（`ipv_address` 只是 JSON tag，不是列）
			}).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	// 重读 networks 返给 handler (gorm.Model.Updates 不会刷新内存 struct)
	if networks, err = s.listNetworks(ctx, uid); err != nil {
		return nil, nil, err
	}
	return &asset, networks, nil
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

	networks, err := s.listNetworks(ctx, uid)
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
	if networks, err = s.listNetworks(ctx, uid); err != nil {
		return nil, nil, err
	}
	// 重新读 asset 拿最终状态 — 用新 struct 实例, 避免事务 Model.Updates 把 asset.ID 写回后再 First 触发重复 bind
	var freshAsset models.Asset
	if err := s.db.WithContext(ctx).First(&freshAsset, uid).Error; err != nil {
		return nil, nil, err
	}
	return &freshAsset, networks, nil
}
