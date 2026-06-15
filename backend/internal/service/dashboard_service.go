package service

import (
	"context"

	"network-monitor-platform/internal/models"

	"gorm.io/gorm"
)

// DashboardStats 仪表盘首页数字
type DashboardStats struct {
	Assets   int64 `json:"assets"`
	Alerts   int64 `json:"alerts"`
	Tickets  int64 `json:"tickets"`
	Sites    int64 `json:"sites"`
	Machines int64 `json:"machines"` // 服务器
	Networks int64 `json:"networks"` // 网络设备
}

// TrendPoint 时间序列点
type TrendPoint struct {
	Date  string `json:"date"`
	Count int64  `json:"count"`
}

// DashboardService 仪表盘聚合业务
type DashboardService interface {
	Stats(ctx context.Context) (*DashboardStats, error)
	AlertTrends(ctx context.Context, days int) ([]TrendPoint, error)
}

type dashboardService struct {
	db *gorm.DB
}

func NewDashboardService(db *gorm.DB) DashboardService {
	return &dashboardService{db: db}
}

func (s *dashboardService) Stats(ctx context.Context) (*DashboardStats, error) {
	var stats DashboardStats
	db := s.db.WithContext(ctx)
	if err := db.Model(&models.Asset{}).Count(&stats.Assets).Error; err != nil {
		return nil, err
	}
	if err := db.Model(&models.Alert{}).Where("status = ?", "problem").Count(&stats.Alerts).Error; err != nil {
		return nil, err
	}
	if err := db.Model(&models.Site{}).Count(&stats.Sites).Error; err != nil {
		return nil, err
	}
	if err := db.Model(&models.Ticket{}).Where("status != ?", "closed").Count(&stats.Tickets).Error; err != nil {
		return nil, err
	}
	// 简化：machines/networks 占位 — 真实场景需 asset_type 区分
	stats.Machines = stats.Assets
	stats.Networks = stats.Assets
	return &stats, nil
}

func (s *dashboardService) AlertTrends(ctx context.Context, days int) ([]TrendPoint, error) {
	if days <= 0 {
		days = 7
	}
	if days > 90 {
		days = 90
	}
	// 真实实现需按天 group；目前缺数据时返回空数组（前端兜底）
	var points []TrendPoint
	err := s.db.WithContext(ctx).Raw(`
		SELECT to_char(date_trunc('day', created_at), 'YYYY-MM-DD') AS date,
		       COUNT(*) AS count
		FROM alerts
		WHERE created_at > NOW() - (? || ' days')::interval
		GROUP BY 1
		ORDER BY 1
	`, days).Scan(&points).Error
	if err != nil {
		return nil, err
	}
	return points, nil
}
