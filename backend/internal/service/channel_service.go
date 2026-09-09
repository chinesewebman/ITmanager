package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"network-monitor-platform/internal/models"
	"network-monitor-platform/internal/notification"
	"network-monitor-platform/internal/redact"

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

// validateChannelConfig 用 notification.NewSender 试构造（构造器不发网络 I/O），
// 校验范围只做「必填存在性」：不解析 URL、不校验 scheme（见 FIX-PLAN-NOTIFY-CHANNEL §2.2）。
//
// 失败原因必须在这里过 redact.Text（源头收口）：handler 的 400 body 会原样回显
// err.Error()，而 apierr.BadRequest 的 internalErr 恒为 nil、Respond 只在 status>=500
// 时脱敏（apierr.go:37-45,52-55）→ 400 出口没有任何兜底。
func validateChannelConfig(chType, cfg string) error {
	_, err := notification.NewSender(&models.NotificationChannel{Type: chType, Config: cfg}) //nolint:exhaustruct
	if err != nil {
		return fmt.Errorf("%w: %s", ErrInvalidInput, redact.Text(err.Error()))
	}
	return nil
}

func (s *channelService) Create(ctx context.Context, ch *models.NotificationChannel) error {
	if ch == nil || ch.Name == "" {
		return fmt.Errorf("%w: 渠道名称不能为空", ErrInvalidInput)
	}
	if err := validateChannelConfig(ch.Type, ch.Config); err != nil {
		return err
	}
	if err := s.db.WithContext(ctx).Create(ch).Error; err != nil {
		if isUniqueViolation(err) {
			return ErrAlreadyExists
		}
		return err
	}
	return nil
}

// allowedChannelUpdateFields Update 允许修改的列。键先归一化到小写再按此白名单放行。
//
// 不能只靠「精确匹配小写键」：gorm 的 Updates(map) 对每个键走 Schema.LookUpField(k)
// （先按列名、再按 Go 字段名解析，均大小写敏感），于是 {"Config": …} / {"Type": …}
// 完全跳过校验照样写列，{"id": …} 还能改主键（安全审计 H-1 / 正确性审计 H-1，真 PG 实测）。
var allowedChannelUpdateFields = map[string]bool{
	"name": true, "type": true, "config": true, "is_enabled": true, "is_default": true,
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
	// 键归一化 + 白名单：保证「校验的键 == 落库的键」，并挡掉 id/created_at 等不该被改的列。
	norm := make(map[string]interface{}, len(updates))
	for k, v := range updates {
		lk := strings.ToLower(k)
		if !allowedChannelUpdateFields[lk] {
			// 不回显键名：键是调用方可控字符串，同样会进 400 body
			return nil, fmt.Errorf("%w: 不支持的更新字段（允许: name/type/config/is_enabled/is_default）", ErrInvalidInput)
		}
		norm[lk] = v
	}
	updates = norm
	// G-33 M1：局部更新校验「生效后的 (type, config)」。只改 name/is_enabled 时跳过；
	// 改了 type 或 config 时用合并值校验，否则会造出 type=dingtalk + 存量 {"url":…} 的坏组合。
	effType, effConfig, touched := ch.Type, ch.Config, false
	if v, ok := updates["type"]; ok {
		str, isStr := v.(string)
		if !isStr {
			return nil, fmt.Errorf("%w: type 必须是字符串", ErrInvalidInput)
		}
		effType, touched = str, true
	}
	if v, ok := updates["config"]; ok {
		// fail-closed：JSON 对象/数组/数字/布尔一律拒绝，不能写成
		// `if str, ok := v.(string); ok { 校验 }` —— 那样非字符串会绕过校验静默落库
		// （gorm Updates(map) 会把 float64/bool 写成 "12345.0"/"1"）。
		str, isStr := v.(string)
		if !isStr {
			return nil, fmt.Errorf("%w: config 必须是 JSON 字符串", ErrInvalidInput)
		}
		effConfig, touched = str, true
	}
	if touched {
		if err := validateChannelConfig(effType, effConfig); err != nil {
			return nil, err
		}
		// 写入值 == 校验值：两个并发局部更新交错时（各自读到旧行）也不会写出
		// 「A 的 type + B 的 config」这种谁都没校验过的组合（正确性审计 M-3）。
		updates["type"], updates["config"] = effType, effConfig
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
