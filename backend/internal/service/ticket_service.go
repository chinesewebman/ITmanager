package service

import (
	"context"
	"errors"
	"time"

	"network-monitor-platform/internal/models"

	"gorm.io/gorm"
)

// TicketFilter 工单列表筛选
type TicketFilter struct {
	Status   string
	Priority string
	Page     int
	PageSize int
}

// TicketService 工单业务接口
type TicketService interface {
	List(ctx context.Context, f TicketFilter) (items []models.Ticket, total int64, err error)
	Get(ctx context.Context, id string) (*models.Ticket, error)
	Create(ctx context.Context, t *models.Ticket) error
	Update(ctx context.Context, id string, updates map[string]interface{}) (*models.Ticket, error)
}

type ticketService struct {
	db *gorm.DB
}

func NewTicketService(db *gorm.DB) TicketService {
	return &ticketService{db: db}
}

func (s *ticketService) List(ctx context.Context, f TicketFilter) ([]models.Ticket, int64, error) {
	q := s.db.WithContext(ctx).Model(&models.Ticket{})
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	if f.Priority != "" {
		q = q.Where("priority = ?", f.Priority)
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
		pageSize = 500
	}
	var items []models.Ticket
	if err := q.Offset((page - 1) * pageSize).Limit(pageSize).
		Order("created_at DESC").Find(&items).Error; err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (s *ticketService) Get(ctx context.Context, id string) (*models.Ticket, error) {
	var t models.Ticket
	if err := s.db.WithContext(ctx).First(&t, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &t, nil
}

func (s *ticketService) Create(ctx context.Context, t *models.Ticket) error {
	if t == nil || t.Title == "" {
		return ErrInvalidInput
	}
	if t.Status == "" {
		t.Status = "open"
	}
	if t.Source == "" {
		t.Source = "manual"
	}
	if t.Tags == "" {
		t.Tags = "[]"
	}
	return s.db.WithContext(ctx).Create(t).Error
}

func (s *ticketService) Update(ctx context.Context, id string, updates map[string]interface{}) (*models.Ticket, error) {
	if len(updates) == 0 {
		return s.Get(ctx, id)
	}
	var t models.Ticket
	if err := s.db.WithContext(ctx).First(&t, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	// 关闭工单时自动写入 closed_at
	if status, ok := updates["status"].(string); ok && status == "closed" {
		now := time.Now()
		updates["closed_at"] = &now
	}
	if err := s.db.WithContext(ctx).Model(&t).Updates(updates).Error; err != nil {
		return nil, err
	}
	return &t, nil
}
