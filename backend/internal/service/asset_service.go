// Package service 业务逻辑层。handler 只做参数解析和响应拼装，DB 访问与业务规则下沉到 service。
// service 通过 interface 暴露，便于在 handler 中 mock 测试。
package service

import (
	"context"
	"errors"
	"strings"

	"network-monitor-platform/internal/models"

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
	return s.db.WithContext(ctx).Create(asset).Error
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
