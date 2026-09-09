package service

import (
	"context"
	"errors"
	"time"

	"network-monitor-platform/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// TicketFilter 工单列表筛选
type TicketFilter struct {
	Status   string
	Priority string
	Page     int
	PageSize int
	// v2.0 cursor 分页: 非空时走 (created_at, id) 二元组 < 翻页, O(log N)
	// 为空时走 v1.x 行为 (Page/PageSize offset 翻页)
	CursorTS time.Time
	CursorID uuid.UUID
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
	q = q.Order("created_at DESC, id DESC") // v2.0 cursor: 二元组排序
	// v2.0 cursor 分页: 二元组 < 走联合索引, O(log N)
	if !f.CursorTS.IsZero() && f.CursorID != uuid.Nil {
		q = q.Where("(created_at, id) < (?, ?)", f.CursorTS, f.CursorID)
		// cursor 模式不跑 Count (cursor 翻页用 hasMore 检测)
		if err := q.Limit(pageSize).Find(&items).Error; err != nil {
			return nil, 0, err
		}
		return items, 0, nil
	}
	// v1.x 兼容: page/size offset 翻页 —— 只有这条路径需要 total（cursor 模式已提前 return）
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := q.Offset((page - 1) * pageSize).Limit(pageSize).Find(&items).Error; err != nil {
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
	// 工单号在 BeforeCreate 里按「当天已建数量」生成，并发下两个请求可能算出同一个号。
	// 唯一索引拒绝后重新生成并重试（最多 5 次），彻底消除竞态（缺陷 D-2）。
	//
	// 例外：客户端显式传了工单号（外部系统对接）时不重试 —— 悄悄换一个号会让调用方
	// 拿到的号与它请求的不一致，语义上是「这个号已被占用」，应原样返回 409。
	clientSuppliedNumber := t.TicketNumber != ""
	const maxCreateAttempts = 5
	for attempt := 1; ; attempt++ {
		err := s.db.WithContext(ctx).Create(t).Error
		if err == nil {
			return nil
		}
		if !isUniqueViolation(err) {
			return err
		}
		if clientSuppliedNumber || attempt >= maxCreateAttempts {
			return ErrAlreadyExists
		}
		// 清掉自动生成的主键与工单号，让 BeforeCreate 重新生成
		t.ID = uuid.Nil
		t.TicketNumber = ""
	}
}

func (s *ticketService) Update(ctx context.Context, id string, updates map[string]interface{}) (*models.Ticket, error) {
	// 🐛 BUG#24: 原版 len==0 走 Get + 主路径 First 重复，统一为 1 次 First
	var t models.Ticket
	if err := s.db.WithContext(ctx).First(&t, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	// 空 updates 直接返当前记录（不写库）
	if len(updates) == 0 {
		return &t, nil
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
