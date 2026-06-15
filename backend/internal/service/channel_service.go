package service

import (
	"context"
	"errors"

	"network-monitor-platform/internal/models"

	"gorm.io/gorm"
)

// ChannelService 通知渠道业务
type ChannelService interface {
	List(ctx context.Context) ([]models.NotificationChannel, error)
	Get(ctx context.Context, id string) (*models.NotificationChannel, error)
	Create(ctx context.Context, ch *models.NotificationChannel) error
	Update(ctx context.Context, id string, updates map[string]interface{}) (*models.NotificationChannel, error)
	Delete(ctx context.Context, id string) error
	Test(ctx context.Context, id string) error
}

type channelService struct {
	db *gorm.DB
}

func NewChannelService(db *gorm.DB) ChannelService {
	return &channelService{db: db}
}

func (s *channelService) List(ctx context.Context) ([]models.NotificationChannel, error) {
	var chs []models.NotificationChannel
	if err := s.db.WithContext(ctx).Find(&chs).Error; err != nil {
		return nil, err
	}
	return chs, nil
}

func (s *channelService) Get(ctx context.Context, id string) (*models.NotificationChannel, error) {
	var ch models.NotificationChannel
	if err := s.db.WithContext(ctx).First(&ch, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &ch, nil
}

func (s *channelService) Create(ctx context.Context, ch *models.NotificationChannel) error {
	if ch == nil || ch.Name == "" {
		return ErrInvalidInput
	}
	return s.db.WithContext(ctx).Create(ch).Error
}

func (s *channelService) Update(ctx context.Context, id string, updates map[string]interface{}) (*models.NotificationChannel, error) {
	if len(updates) == 0 {
		return s.Get(ctx, id)
	}
	var ch models.NotificationChannel
	if err := s.db.WithContext(ctx).First(&ch, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if err := s.db.WithContext(ctx).Model(&ch).Updates(updates).Error; err != nil {
		return nil, err
	}
	return &ch, nil
}

func (s *channelService) Delete(ctx context.Context, id string) error {
	return s.db.WithContext(ctx).Delete(&models.NotificationChannel{}, "id = ?", id).Error
}

func (s *channelService) Test(ctx context.Context, id string) error {
	// 真实实现：按 channel.Type 调对应 webhook / SMTP
	// 这里只验证存在
	if _, err := s.Get(ctx, id); err != nil {
		return err
	}
	return nil
}
