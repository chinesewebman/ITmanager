// runbook service：复用 service 包已有的 ErrNotFound / ErrInvalidInput
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"network-monitor-platform/internal/models"
)

// RunbookService 标准化操作手册服务
type RunbookService struct {
	db *gorm.DB
}

func NewRunbookService(db *gorm.DB) *RunbookService {
	return &RunbookService{db: db}
}

// Create 新建 Runbook
func (s *RunbookService) Create(ctx context.Context, rb *models.Runbook) error {
	if strings.TrimSpace(rb.Title) == "" {
		return fmt.Errorf("title 不能为空: %w", ErrInvalidInput)
	}
	if strings.TrimSpace(rb.AssetType) == "" {
		return fmt.Errorf("asset_type 不能为空: %w", ErrInvalidInput)
	}
	if err := s.validateStepsJSON(rb.Steps); err != nil {
		return fmt.Errorf("steps JSON 非法: %w: %w", ErrInvalidInput, err)
	}
	now := time.Now()
	if rb.ID == uuid.Nil {
		rb.ID = uuid.New()
	}
	rb.CreatedAt = now
	rb.UpdatedAt = now
	if err := s.db.WithContext(ctx).Create(rb).Error; err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("create runbook: %w", ErrAlreadyExists)
		}
		return fmt.Errorf("create runbook: %w", err)
	}
	return nil
}

// Update 更新 Runbook
func (s *RunbookService) Update(ctx context.Context, rb *models.Runbook) error {
	if strings.TrimSpace(rb.Title) == "" {
		return fmt.Errorf("title 不能为空: %w", ErrInvalidInput)
	}
	if err := s.validateStepsJSON(rb.Steps); err != nil {
		return fmt.Errorf("steps JSON 非法: %w: %w", ErrInvalidInput, err)
	}
	rb.UpdatedAt = time.Now()
	res := s.db.WithContext(ctx).
		Model(&models.Runbook{}).
		Where("id = ?", rb.ID).
		Updates(map[string]any{
			"title":      rb.Title,
			"asset_type": rb.AssetType,
			"summary":    rb.Summary,
			"content_md": rb.ContentMD,
			"steps":      rb.Steps,
			"tags":       rb.Tags,
			"severity":   rb.Severity,
			"enabled":    rb.Enabled,
			"updated_at": rb.UpdatedAt,
		})
	if res.Error != nil {
		return fmt.Errorf("update runbook: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("runbook %s: %w", rb.ID, ErrNotFound)
	}
	return nil
}

// Delete 删除
func (s *RunbookService) Delete(ctx context.Context, id string) error {
	res := s.db.WithContext(ctx).
		Where("id = ?", id).
		Delete(&models.Runbook{})
	if res.Error != nil {
		return fmt.Errorf("delete runbook: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// Get 查询单个
func (s *RunbookService) Get(ctx context.Context, id string) (*models.Runbook, error) {
	var rb models.Runbook
	err := s.db.WithContext(ctx).First(&rb, "id = ?", id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get runbook: %w", err)
	}
	return &rb, nil
}

// ListOptions 列表过滤选项
type RunbookListOptions struct {
	AssetType string
	Severity  int
	Enabled   *bool
	Keyword   string
	Limit     int
	Offset    int
}

// List 列表查询
func (s *RunbookService) List(ctx context.Context, opt RunbookListOptions) ([]models.Runbook, int64, error) {
	q := s.db.WithContext(ctx).Model(&models.Runbook{})
	if opt.AssetType != "" {
		q = q.Where("asset_type = ?", opt.AssetType)
	}
	if opt.Severity > 0 {
		q = q.Where("severity = ? OR severity = 0", opt.Severity)
	}
	if opt.Enabled != nil {
		q = q.Where("enabled = ?", *opt.Enabled)
	}
	if opt.Keyword != "" {
		like := "%" + opt.Keyword + "%"
		q = q.Where("title LIKE ? OR summary LIKE ? OR tags LIKE ?", like, like, like)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count runbook: %w", err)
	}

	limit := clampRunbookLimit(opt.Limit, 50)
	offset := clampRunbookOffset(opt.Offset, 0)
	var items []models.Runbook
	if err := q.Order("updated_at DESC").Limit(limit).Offset(offset).Find(&items).Error; err != nil {
		return nil, 0, fmt.Errorf("list runbook: %w", err)
	}
	return items, total, nil
}

// ListForAssetTypeAndSeverity 推荐 Runbook（按 asset_type + severity 匹配）
// severity<=0 不按 severity 过滤
func (s *RunbookService) ListForAssetTypeAndSeverity(ctx context.Context, assetType string, severity int) ([]models.Runbook, error) {
	q := s.db.WithContext(ctx).Where("enabled = ?", true)
	if assetType != "" {
		q = q.Where("asset_type = ?", assetType)
	}
	if severity > 0 {
		q = q.Where("severity = ? OR severity = 0", severity)
	}
	var items []models.Runbook
	if err := q.Order("severity DESC, updated_at DESC").Limit(10).Find(&items).Error; err != nil {
		return nil, fmt.Errorf("recommend runbook: %w", err)
	}
	return items, nil
}

// parseStepsJSON 解析 steps JSON（前端用）
func (s *RunbookService) validateStepsJSON(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return err
	}
	return nil
}

// 防 OOM 上限
func clampRunbookLimit(n, def int) int {
	if n <= 0 {
		return def
	}
	if n > 500 {
		return 500
	}
	return n
}

func clampRunbookOffset(n, def int) int {
	if n < 0 {
		return def
	}
	return n
}

// 显式调一次 clause 包，保证 import 完整
var _ = clause.OnConflict{}
