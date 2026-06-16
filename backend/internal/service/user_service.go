package service

import (
	"context"
	"errors"

	"network-monitor-platform/internal/models"

	"gorm.io/gorm"
)

// UserService 用户业务接口（只读 — 用户 CRUD 由 auth 模块管）
type UserService interface {
	// 🐛 BUG#26: 原 List() 无分页，10k users 全表返会内存爆
	List(ctx context.Context, page, pageSize int) (items []models.User, total int64, err error)
	Get(ctx context.Context, id string) (*models.User, error)
}

type userService struct {
	db *gorm.DB
}

func NewUserService(db *gorm.DB) UserService {
	return &userService{db: db}
}

func (s *userService) List(ctx context.Context, page, pageSize int) ([]models.User, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 500 {
		pageSize = 500
	}
	var total int64
	if err := s.db.WithContext(ctx).Model(&models.User{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var users []models.User
	if err := s.db.WithContext(ctx).
		Order("created_at DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).
		Find(&users).Error; err != nil {
		return nil, 0, err
	}
	return users, total, nil
}

func (s *userService) Get(ctx context.Context, id string) (*models.User, error) {
	var u models.User
	if err := s.db.WithContext(ctx).First(&u, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &u, nil
}
