package models

import (
	"time"

	"github.com/google/uuid"
)

// AlertSuppression 告警抑制规则
//
// 设计：
//   - host_pattern 用 glob 风格（"*", "db-*-prod", "switch-core-01"）
//   - severity_max 表示 "严重级别 ≤ 此值才抑制"（数字越大越严重）
//   - time_window_seconds 在窗口内同 key 只产生 1 条 alert
//   - ttl_seconds 抑制规则自动过期时间，0 = 不过期
//   - enabled=false 时该规则不参与抑制
type AlertSuppression struct {
	ID                uuid.UUID `json:"id" gorm:"type:uuid;primary_key;default:gen_random_uuid()"`
	Name              string    `json:"name" gorm:"size:100;not null"`
	SeverityMax       int       `json:"severity_max" gorm:"default:3"` // 1-5，5=critical，3=warning
	HostPattern       string    `json:"host_pattern" gorm:"size:255"`  // glob
	TimeWindowSeconds int       `json:"time_window_seconds" gorm:"default:300"`
	TTLSeconds        int       `json:"ttl_seconds" gorm:"default:0"` // 0=不过期
	Enabled           bool      `json:"enabled" gorm:"default:true"`
	Description       string    `json:"description" gorm:"type:text"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

func (a *AlertSuppression) TableName() string { return "alert_suppressions" }

// SuppressionMatchResult 抑制评估结果
type SuppressionMatchResult struct {
	Suppressed      bool       `json:"suppressed"`
	MatchedRule     *uuid.UUID `json:"matched_rule,omitempty"`
	Reason          string     `json:"reason,omitempty"`
	LastFiredAt     *time.Time `json:"last_fired_at,omitempty"`
	WindowExpiresAt *time.Time `json:"window_expires_at,omitempty"`
}
