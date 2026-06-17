package service

import (
	"context"
	"errors"

	"network-monitor-platform/internal/models"
	"network-monitor-platform/internal/notification"

	"gorm.io/gorm"
)

// ChannelService 通知渠道业务
type ChannelService interface {
	List(ctx context.Context) ([]models.NotificationChannel, error)
	Get(ctx context.Context, id string) (*models.NotificationChannel, error)
	Create(ctx context.Context, ch *models.NotificationChannel) error
	Update(ctx context.Context, id string, updates map[string]interface{}) (*models.NotificationChannel, error)
	Delete(ctx context.Context, id string) error
	// Test 真实调 Sender 试发, 失败返 error
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
	if err := s.db.WithContext(ctx).Create(ch).Error; err != nil {
		if isUniqueViolation(err) {
			return ErrAlreadyExists
		}
		return err
	}
	return nil
}

func (s *channelService) Update(ctx context.Context, id string, updates map[string]interface{}) (*models.NotificationChannel, error) {
	// 🐛 BUG#25: 原版 len==0 走 Get + 主路径 First 重复，统一 1 次
	var ch models.NotificationChannel
	if err := s.db.WithContext(ctx).First(&ch, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if len(updates) == 0 {
		return &ch, nil
	}
	if err := s.db.WithContext(ctx).Model(&ch).Updates(updates).Error; err != nil {
		return nil, err
	}
	return &ch, nil
}

func (s *channelService) Delete(ctx context.Context, id string) error {
	return s.db.WithContext(ctx).Delete(&models.NotificationChannel{}, "id = ?", id).Error
}

// Test v1.4: 真实发测试消息 (走 notification.Sender)
func (s *channelService) Test(ctx context.Context, id string) error {
	ch, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	sender, err := notification.Resolver(ch)
	if err != nil {
		return err
	}
	return sender.Send(ctx, "", "[ITmanager Test] 渠道连通性测试")
}
