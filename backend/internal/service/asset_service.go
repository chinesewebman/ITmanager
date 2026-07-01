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

func (s *assetService) Get(ctx context.Context, id string) (*models.Asset, []models.AssetNetwork, error) {
	var asset models.Asset
	if err := s.db.WithContext(ctx).First(&asset, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, ErrNotFound
		}
		return nil, nil, err
	}

	var networks []models.AssetNetwork
	if err := s.db.WithContext(ctx).Where("asset_id = ?", asset.ID).Find(&networks).Error; err != nil {
		return nil, nil, err
	}
	return &asset, networks, nil
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

	var networks []models.AssetNetwork
	if err := s.db.WithContext(ctx).Where("asset_id = ?", uid).Find(&networks).Error; err != nil {
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
				"ipv_address":  "", // 注意: IPv6Address gorm tag 是 `ipv_address` (T-6/Trap 11 已知)
			}).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	// 重读 networks 返给 handler (gorm.Model.Updates 不会刷新内存 struct)
	if err := s.db.WithContext(ctx).Where("asset_id = ?", uid).Find(&networks).Error; err != nil {
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

	var networks []models.AssetNetwork
	if err := s.db.WithContext(ctx).Where("asset_id = ?", uid).Find(&networks).Error; err != nil {
		return nil, nil, err
	}

	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 写回 IP: 把 LastKnownIP4/6 写回第一张网卡 (顺序: 之前有 IPv4 的那张优先)
		// 简单策略: 第一张网卡接收 IPv4, 第二张接收 IPv6 (如果多网卡需要精细化, 后续扩展)
		for i := range networks {
			if i == 0 && asset.LastKnownIP4 != nil {
				networks[i].IPv4Address = *asset.LastKnownIP4
			}
			if asset.LastKnownIP6 != nil {
				networks[i].IPv6Address = *asset.LastKnownIP6
			}
			if err := tx.Model(&models.AssetNetwork{}).
				Where("id = ?", networks[i].ID).
				Updates(map[string]interface{}{
					"ipv4_address": networks[i].IPv4Address,
					"ipv_address":  networks[i].IPv6Address,
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
	if err := s.db.WithContext(ctx).Where("asset_id = ?", uid).Find(&networks).Error; err != nil {
		return nil, nil, err
	}
	// 重新读 asset 拿最终状态 — 用新 struct 实例, 避免事务 Model.Updates 把 asset.ID 写回后再 First 触发重复 bind
	var freshAsset models.Asset
	if err := s.db.WithContext(ctx).First(&freshAsset, uid).Error; err != nil {
		return nil, nil, err
	}
	return &freshAsset, networks, nil
}
