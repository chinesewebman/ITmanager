package service

import (
	"context"
	"time"

	"network-monitor-platform/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// AuditFilter 审计日志查询
type AuditFilter struct {
	UserID   *uuid.UUID // 按用户过滤 (nil = 全部)
	Action   string     // 按 action 过滤 (空 = 全部)
	Method   string     // GET/POST/PUT/DELETE (空 = 全部)
	Path     string     // 按 path 前缀匹配 (空 = 全部)
	Limit    int
	// v2.0 cursor 分页: 非空时走 (created_at, id) 二元组 < 翻页
	CursorTS time.Time
	CursorID uuid.UUID
}

// AuditService 审计日志查询接口
type AuditService interface {
	List(ctx context.Context, f AuditFilter) ([]models.AuditLog, error)
}

type auditService struct {
	db *gorm.DB
}

func NewAuditService(db *gorm.DB) AuditService {
	return &auditService{db: db}
}

func (s *auditService) List(ctx context.Context, f AuditFilter) ([]models.AuditLog, error) {
	q := s.db.WithContext(ctx).Model(&models.AuditLog{})

	if f.UserID != nil {
		q = q.Where("user_id = ?", *f.UserID)
	}
	if f.Action != "" {
		q = q.Where("action = ?", f.Action)
	}
	if f.Method != "" {
		q = q.Where("method = ?", f.Method)
	}
	if f.Path != "" {
		// path 前缀匹配 (LIKE 'path%'), 客户端可传 /api/v1/users
		q = q.Where("path LIKE ?", f.Path+"%")
	}

	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}

	q = q.Order("created_at DESC, id DESC")
	// v2.0 cursor 分页
	if !f.CursorTS.IsZero() && f.CursorID != uuid.Nil {
		q = q.Where("(created_at, id) < (?, ?)", f.CursorTS, f.CursorID)
	}

	var items []models.AuditLog
	if err := q.Limit(limit).Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}
