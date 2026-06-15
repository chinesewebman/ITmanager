package service

import (
	"context"
	"errors"

	"network-monitor-platform/internal/models"

	"gorm.io/gorm"
)

// RackService 机柜与机房业务接口
type RackService interface {
	ListRacks(ctx context.Context, siteID string) ([]RackDTO, error)
	GetRack(ctx context.Context, id string) (*RackDTO, error)
	GetRackDevices(ctx context.Context, rackID string) ([]RackDevice, error)

	ListSites(ctx context.Context) ([]models.Site, error)
	GetSite(ctx context.Context, id string) (*SiteDetail, error)
}

// RackDTO 面向前端的机柜 DTO，total_units / used_units 命名规范化（C-F12）
type RackDTO struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	SiteID     string `json:"site_id"`
	TotalUnits int    `json:"total_units"` // 模型字段 TotalU 的 snake_case 友好别名
	UsedUnits  int    `json:"used_units"`  // 聚合自机柜内设备 rack_position 占位
}

// RackDevice 机柜内设备 + 告警状态聚合
type RackDevice struct {
	models.Asset
	HealthStatus string `json:"health_status"` // green/yellow/red
	AlertCount   int    `json:"alert_count"`
}

// SiteDetail 机房详情（含机柜/设备计数）
type SiteDetail struct {
	Site       models.Site `json:"site"`
	RackCount  int64       `json:"rack_count"`
	AssetCount int64       `json:"asset_count"`
}

type rackService struct {
	db *gorm.DB
}

func NewRackService(db *gorm.DB) RackService {
	return &rackService{db: db}
}

func (s *rackService) ListRacks(ctx context.Context, siteID string) ([]RackDTO, error) {
	q := s.db.WithContext(ctx).Model(&models.Rack{})
	if siteID != "" {
		q = q.Where("site_id = ?", siteID)
	}
	var racks []models.Rack
	if err := q.Order("name ASC").Find(&racks).Error; err != nil {
		return nil, err
	}
	return toRackDTOs(ctx, s.db, racks), nil
}

func (s *rackService) GetRack(ctx context.Context, id string) (*RackDTO, error) {
	var rack models.Rack
	if err := s.db.WithContext(ctx).First(&rack, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	dtos := toRackDTOs(ctx, s.db, []models.Rack{rack})
	return &dtos[0], nil
}

// toRackDTOs 把 models.Rack 转换为 RackDTO，并聚合 used_units（机柜内设备 rack_position 总数）
// C-P3: 改单条 GROUP BY 替代 N+1 Count（100 个机柜从 100+1 次 query → 1 次）
func toRackDTOs(ctx context.Context, db *gorm.DB, racks []models.Rack) []RackDTO {
	out := make([]RackDTO, 0, len(racks))
	if len(racks) == 0 {
		return out
	}

	// 1) 收集所有 rack ID
	rackIDs := make([]string, len(racks))
	idToIndex := make(map[string]int, len(racks))
	for i, r := range racks {
		rackIDs[i] = r.ID.String()
		idToIndex[r.ID.String()] = i
	}

	// 2) 一次 GROUP BY 查回所有 rack 的 used count
	type countRow struct {
		RackID string
		Used   int64
	}
	var rows []countRow
	if err := db.WithContext(ctx).Model(&models.Asset{}).
		Select("rack_id, COUNT(*) AS used").
		Where("rack_id IN ? AND rack_position IS NOT NULL", rackIDs).
		Group("rack_id").
		Scan(&rows).Error; err == nil {
		for _, cr := range rows {
			if idx, ok := idToIndex[cr.RackID]; ok {
				_ = idx
			}
		}
	}

	// 3) 用 map 复用聚合结果
	usedMap := make(map[string]int, len(rows))
	for _, cr := range rows {
		usedMap[cr.RackID] = int(cr.Used)
	}

	for _, r := range racks {
		rid := r.ID.String()
		out = append(out, RackDTO{
			ID:         rid,
			Name:       r.Name,
			SiteID:     r.SiteID.String(),
			TotalUnits: r.TotalU,
			UsedUnits:  usedMap[rid], // 0 if not in result
		})
	}
	return out
}

func (s *rackService) GetRackDevices(ctx context.Context, rackID string) ([]RackDevice, error) {
	var assets []models.Asset
	if err := s.db.WithContext(ctx).Where("rack_id = ?", rackID).
		Order("rack_position ASC").Find(&assets).Error; err != nil {
		return nil, err
	}
	devices := make([]RackDevice, len(assets))

	// C-P3: 单条 GROUP BY 拿所有设备的告警计数（替代 N 次 Count）
	alertCountMap := make(map[string]int, len(assets))
	if len(assets) > 0 {
		assetIDs := make([]string, len(assets))
		for i, a := range assets {
			assetIDs[i] = a.ID.String()
		}
		type alertCountRow struct {
			AssetID string
			Cnt     int64
		}
		var rows []alertCountRow
		if err := s.db.WithContext(ctx).Model(&models.Alert{}).
			Select("asset_id, COUNT(*) AS cnt").
			Where("asset_id IN ? AND status = ?", assetIDs, "problem").
			Group("asset_id").
			Scan(&rows).Error; err == nil {
			for _, r := range rows {
				alertCountMap[r.AssetID] = int(r.Cnt)
			}
		}
	}

	for i, a := range assets {
		alertCount := alertCountMap[a.ID.String()]
		health := "green"
		if alertCount > 0 {
			health = "red"
		}
		devices[i] = RackDevice{
			Asset:        a,
			HealthStatus: health,
			AlertCount:   alertCount,
		}
	}
	return devices, nil
}

func (s *rackService) ListSites(ctx context.Context) ([]models.Site, error) {
	var sites []models.Site
	if err := s.db.WithContext(ctx).Where("is_active = ?", true).
		Order("name ASC").Find(&sites).Error; err != nil {
		return nil, err
	}
	return sites, nil
}

func (s *rackService) GetSite(ctx context.Context, id string) (*SiteDetail, error) {
	var site models.Site
	if err := s.db.WithContext(ctx).First(&site, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var rackCount, assetCount int64
	s.db.WithContext(ctx).Model(&models.Rack{}).Where("site_id = ?", id).Count(&rackCount)
	s.db.WithContext(ctx).Model(&models.Asset{}).Where("site_id = ?", id).Count(&assetCount)
	return &SiteDetail{Site: site, RackCount: rackCount, AssetCount: assetCount}, nil
}
