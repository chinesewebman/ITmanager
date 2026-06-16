package models

import (
	"time"

	"github.com/google/uuid"
)

// TimelineEventType 时间线事件类型
type TimelineEventType string

const (
	// TimelineEventAlert 告警事件（触发 / 确认 / 解决）
	TimelineEventAlert TimelineEventType = "alert"
	// TimelineEventTicket 工单事件（创建 / 解决 / 关闭）
	TimelineEventTicket TimelineEventType = "ticket"
	// TimelineEventStatus 资产状态变更（online / offline / maintenance）
	TimelineEventStatus TimelineEventType = "status_change"
	// TimelineEventLink 网卡状态变更（up / down）
	TimelineEventLink TimelineEventType = "link_change"
)

// TimelineEvent 时间线事件（聚合自 alerts / tickets / assets / asset_networks 四张表）
type TimelineEvent struct {
	TS          time.Time         `json:"ts"`
	Kind        TimelineEventType `json:"kind"`
	SubKind     string            `json:"sub_kind,omitempty"` // triggered/acknowledged/resolved/created/closed/online/offline/up/down
	Severity    int               `json:"severity,omitempty"` // 0-5，告警才有意义
	Title       string            `json:"title"`
	Description string            `json:"description,omitempty"`
	RefID       *uuid.UUID        `json:"ref_id,omitempty"`
	RefTable    string            `json:"ref_table,omitempty"`
}

// DiagnosticSummary 资产诊断摘要
type DiagnosticSummary struct {
	AlertCount    int64      `json:"alert_count"`
	TicketCount   int64      `json:"ticket_count"`
	OpenAlerts    int64      `json:"open_alerts"`
	OpenTickets   int64      `json:"open_tickets"`
	LastOffline   *time.Time `json:"last_offline,omitempty"`
	LastOnline    *time.Time `json:"last_online,omitempty"`
	MTTRSeconds   *int64     `json:"mttr_seconds,omitempty"` // 告警平均恢复时间（秒），nil=无数据
	LinkDownCount int64      `json:"link_down_count"`
	WindowDays    int        `json:"window_days"`
	WindowStart   time.Time  `json:"window_start"`
	WindowEnd     time.Time  `json:"window_end"`
	AssetID       uuid.UUID  `json:"asset_id"`
}

// DiagnosticAsset 资产概要（嵌在 timeline 响应里）
type DiagnosticAsset struct {
	ID        uuid.UUID  `json:"id"`
	Name      string     `json:"name"`
	AssetTag  string     `json:"asset_tag"`
	AssetType string     `json:"asset_type"`
	Brand     string     `json:"brand"`
	Model     string     `json:"model"`
	Status    string     `json:"status"`
	SiteID    *uuid.UUID `json:"site_id,omitempty"`
	SiteName  string     `json:"site_name"`
	RackID    *uuid.UUID `json:"rack_id,omitempty"`
	RackName  string     `json:"rack_name"`
}

// DiagnosticTimeline 时间线响应
type DiagnosticTimeline struct {
	Asset   *DiagnosticAsset   `json:"asset"`
	Events  []TimelineEvent    `json:"events"`
	Summary *DiagnosticSummary `json:"summary"`
}
