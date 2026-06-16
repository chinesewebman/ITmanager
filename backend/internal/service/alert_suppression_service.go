package service

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"
	"sync"
	"time"

	"network-monitor-platform/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// AlertSuppressionService 告警抑制规则服务（P0-2）
//
// 双层去重：
//  1. 规则匹配：severity ≤ severity_max + host glob 匹配 + enabled=true
//  2. 时间窗口：同 (rule_id, host_id) 在 time_window_seconds 内只放行 1 次
//
// in-memory state 用 sync.RWMutex 保护：读路径多写路径少
// 进程重启不丢规则（DB 持久化），只丢热缓存（5min TTL 内可能短暂"过漏"）
type AlertSuppressionService struct {
	db *gorm.DB

	// lastFired: key = rule_id + "|" + host_id → 最后触发时间
	// 缓存策略：清理过去 1h 前的条目（lazy GC，每次 Evaluate 触发）
	mu        sync.RWMutex
	lastFired map[string]time.Time
}

func NewAlertSuppressionService(db *gorm.DB) *AlertSuppressionService {
	return &AlertSuppressionService{
		db:        db,
		lastFired: make(map[string]time.Time),
	}
}

// Create 创建抑制规则
func (s *AlertSuppressionService) Create(ctx context.Context, rule *models.AlertSuppression) error {
	if rule.Name == "" {
		return errors.New("name 不能为空")
	}
	if rule.HostPattern == "" {
		return errors.New("host_pattern 不能为空")
	}
	if rule.SeverityMax < 0 || rule.SeverityMax > 5 {
		return fmt.Errorf("severity_max 必须在 0-5 之间，当前 %d", rule.SeverityMax)
	}
	if rule.TimeWindowSeconds <= 0 {
		return errors.New("time_window_seconds 必须 > 0")
	}
	// 显式生成 ID（gorm 的 default:gen_random_uuid() 在 Create 时不回填到 struct）
	if rule.ID == uuid.Nil {
		rule.ID = uuid.New()
	}
	if err := s.db.WithContext(ctx).Create(rule).Error; err != nil {
		return err
	}
	return nil
}

// List 列出所有抑制规则
func (s *AlertSuppressionService) List(ctx context.Context) ([]models.AlertSuppression, error) {
	var rules []models.AlertSuppression
	if err := s.db.WithContext(ctx).Order("created_at DESC").Find(&rules).Error; err != nil {
		return nil, err
	}
	return rules, nil
}

// Get 拿单条规则
func (s *AlertSuppressionService) Get(ctx context.Context, id uuid.UUID) (*models.AlertSuppression, error) {
	var rule models.AlertSuppression
	if err := s.db.WithContext(ctx).First(&rule, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &rule, nil
}

// Update 更新规则
func (s *AlertSuppressionService) Update(ctx context.Context, id uuid.UUID, updates map[string]interface{}) (*models.AlertSuppression, error) {
	if len(updates) == 0 {
		return s.Get(ctx, id)
	}
	// 简单校验：severity_max 范围（gorm 序列化的数字通常是 int64）
	if v, ok := updates["severity_max"]; ok {
		var n int
		switch x := v.(type) {
		case int:
			n = x
		case int64:
			n = int(x)
		case float64:
			n = int(x)
		}
		if n < 0 || n > 5 {
			return nil, fmt.Errorf("severity_max 必须在 0-5 之间")
		}
		updates["severity_max"] = n // 标准化为 int
	}
	if v, ok := updates["time_window_seconds"]; ok {
		var n int
		switch x := v.(type) {
		case int:
			n = x
		case int64:
			n = int(x)
		case float64:
			n = int(x)
		}
		if n <= 0 {
			return nil, errors.New("time_window_seconds 必须 > 0")
		}
		updates["time_window_seconds"] = n
	}
	if err := s.db.WithContext(ctx).
		Model(&models.AlertSuppression{}).
		Where("id = ?", id).
		Updates(updates).Error; err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

// Delete 软/硬删除：当前用硬删（规则配置数据，无审计需求）
func (s *AlertSuppressionService) Delete(ctx context.Context, id uuid.UUID) error {
	res := s.db.WithContext(ctx).Where("id = ?", id).Delete(&models.AlertSuppression{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	// 清缓存：删所有以 "rule_id|" 开头的 key
	prefix := id.String() + "|"
	s.mu.Lock()
	for k := range s.lastFired {
		if len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			delete(s.lastFired, k)
		}
	}
	s.mu.Unlock()
	return nil
}

// Evaluate 评估一条新告警是否被抑制
//
// 入参：
//   - severity: 告警严重级别 (1-5)
//   - hostID: 告警主机 UUID
//   - hostName: 告警主机名（用于 glob 匹配）
//
// 返回 SuppressionMatchResult。Suppressed=true 时新告警应被静默。
func (s *AlertSuppressionService) Evaluate(ctx context.Context, severity int, hostID uuid.UUID, hostName string) (*models.SuppressionMatchResult, error) {
	var rules []models.AlertSuppression
	if err := s.db.WithContext(ctx).Where("enabled = ?", true).Find(&rules).Error; err != nil {
		return nil, err
	}

	for i := range rules {
		rule := &rules[i]

		// 1) severity 匹配：告警严重级别 ≤ 规则最大才被抑制
		if severity > rule.SeverityMax {
			continue
		}
		// 2) host glob 匹配
		if !matchHost(rule.HostPattern, hostName) {
			continue
		}
		// 3) TTL 过期检查
		if rule.TTLSeconds > 0 && time.Since(rule.UpdatedAt) > time.Duration(rule.TTLSeconds)*time.Second {
			continue
		}
		// 4) 时间窗口检查：同 rule+host 在窗口内已放行过 → 抑制
		key := rule.ID.String() + "|" + hostID.String()
		now := time.Now()
		s.mu.RLock()
		lastFired, seen := s.lastFired[key]
		s.mu.RUnlock()
		if seen && now.Sub(lastFired) < time.Duration(rule.TimeWindowSeconds)*time.Second {
			expiresAt := lastFired.Add(time.Duration(rule.TimeWindowSeconds) * time.Second)
			return &models.SuppressionMatchResult{
				Suppressed:      true,
				MatchedRule:     &rule.ID,
				Reason:          "窗口期内已触发过同 host 告警",
				LastFiredAt:     &lastFired,
				WindowExpiresAt: &expiresAt,
			}, nil
		}
		// 5) 通过评估 → 记录本次触发时间
		s.mu.Lock()
		s.lastFired[key] = now
		s.mu.Unlock()
		// lazy GC：清理 1h 前的过期条目
		s.gcLocked(now, time.Hour)
		return &models.SuppressionMatchResult{
			Suppressed:  false,
			MatchedRule: &rule.ID,
			Reason:      "通过抑制评估，记录触发时间",
		}, nil
	}
	return &models.SuppressionMatchResult{Suppressed: false, Reason: "无规则匹配"}, nil
}

// gcLocked 清理过期条目（调用方须持写锁）
func (s *AlertSuppressionService) gcLocked(now time.Time, maxAge time.Duration) {
	cutoff := now.Add(-maxAge)
	for k, t := range s.lastFired {
		if t.Before(cutoff) {
			delete(s.lastFired, k)
		}
	}
}

// ResetWindow 重置窗口缓存（用于测试 / 手动清空）
func (s *AlertSuppressionService) ResetWindow() {
	s.mu.Lock()
	s.lastFired = make(map[string]time.Time)
	s.mu.Unlock()
}

// matchHost 简单 glob 匹配：* 匹配任意字符（不含 /），? 匹配单字符
// 用 path.Match 实现，保证轻量 + 标准
func matchHost(pattern, host string) bool {
	if pattern == "" {
		return false
	}
	if pattern == host {
		return true
	}
	ok, err := path.Match(pattern, host)
	if err == nil {
		return ok
	}
	// path.Match 失败（语法错）退化为子串匹配
	return strings.Contains(host, pattern)
}
