// metric_snapshots service：批量插入（兜底采集）+ 时序查询
package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"network-monitor-platform/internal/apierr"
	"network-monitor-platform/internal/models"
)

// MetricSnapshotService 指标快照服务
type MetricSnapshotService struct {
	db *gorm.DB
}

func NewMetricSnapshotService(db *gorm.DB) *MetricSnapshotService {
	return &MetricSnapshotService{db: db}
}

// maxBatchSize 单次批量插入上限（防 DB 撑爆，跟 P0-2 alert_suppression 一致）
const maxBatchSize = 1000

// BulkInsert 批量插入（Zabbix 拉取 / 探针兜底）
func (s *MetricSnapshotService) BulkInsert(ctx context.Context, snaps []models.MetricSnapshot) error {
	if len(snaps) == 0 {
		return fmt.Errorf("snaps empty: %w", ErrInvalidInput)
	}
	if len(snaps) > maxBatchSize {
		return fmt.Errorf("batch too large %d > %d: %w", len(snaps), maxBatchSize, ErrTooManyItems)
	}
	now := time.Now()
	for i := range snaps {
		if snaps[i].ID == uuid.Nil {
			snaps[i].ID = uuid.New()
		}
		if snaps[i].CreatedAt.IsZero() {
			snaps[i].CreatedAt = now
		}
	}
	if err := s.db.WithContext(ctx).Create(&snaps).Error; err != nil {
		return fmt.Errorf("bulk insert snapshots: %w", err)
	}
	return nil
}

// QueryFilter 时序查询过滤
type QueryFilter struct {
	AssetID string
	Key     string
	From    time.Time
	To      time.Time
	Limit   int
}

// Query 时序查询
func (s *MetricSnapshotService) Query(ctx context.Context, f QueryFilter) ([]models.MetricSnapshot, error) {
	q := s.db.WithContext(ctx).Model(&models.MetricSnapshot{})
	if f.AssetID != "" {
		q = q.Where("asset_id = ?", f.AssetID)
	}
	if f.Key != "" {
		q = q.Where("key = ?", f.Key)
	}
	if !f.From.IsZero() {
		q = q.Where("ts >= ?", f.From)
	}
	if !f.To.IsZero() {
		q = q.Where("ts <= ?", f.To)
	}
	limit := f.Limit
	if limit <= 0 || limit > 5000 {
		limit = 1000
	}
	var items []models.MetricSnapshot
	if err := q.Order("ts DESC").Limit(limit).Find(&items).Error; err != nil {
		return nil, fmt.Errorf("query snapshots: %w", err)
	}
	return items, nil
}

// LatestByAssetAndKey 取某 asset+key 的最新 N 个点
func (s *MetricSnapshotService) LatestByAssetAndKey(ctx context.Context, assetID, key string, n int) ([]models.MetricSnapshot, error) {
	if assetID == "" || key == "" {
		return nil, fmt.Errorf("asset_id 和 key 必填: %w", ErrInvalidInput)
	}
	if n <= 0 || n > 500 {
		n = 60
	}
	var items []models.MetricSnapshot
	err := s.db.WithContext(ctx).
		Where("asset_id = ? AND key = ?", assetID, key).
		Order("ts DESC").Limit(n).
		Find(&items).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("latest snapshots: %w", err)
	}
	return items, nil
}

// 隐式依赖 apierr 防止 import 被认为未用
var _ = apierr.NotFound
