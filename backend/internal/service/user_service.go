package service

import (
	"context"
	"errors"

	"network-monitor-platform/internal/models"

	"gorm.io/gorm"
)

// UserService 用户业务接口（只读 — 用户 CRUD 由 auth 模块管）
type UserService interface {
	List(ctx context.Context) ([]models.User, error)
	Get(ctx context.Context, id string) (*models.User, error)
}

type userService struct {
	db *gorm.DB
}

func NewUserService(db *gorm.DB) UserService {
	return &userService{db: db}
}

func (s *userService) List(ctx context.Context) ([]models.User, error) {
	var users []models.User
	if err := s.db.WithContext(ctx).Find(&users).Error; err != nil {
		return nil, err
	}
	return users, nil
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
