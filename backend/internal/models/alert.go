package models

import (
	"time"

	"github.com/google/uuid"
)

// Alert 告警
type Alert struct {
	ID       uuid.UUID  `json:"id" gorm:"type:uuid;primary_key;default:gen_random_uuid()"`
	AlertID  string     `json:"alert_id" gorm:"size:100;index"` // Zabbix 告警ID
	HostID   *uuid.UUID `json:"host_id" gorm:"type:uuid;index"`
	HostName string     `json:"host_name" gorm:"size:255"`

	// M37-A：AlertRule.NotifyChannels 写不读修复 — 让 GORM 能读写 DB 已有但模型缺失的 alert_rule_id 列
	// (migration 000001_init.up.sql:651 已建 UUID FK REFERENCES alert_rules(id))
	// 历史 alert（rule_id NULL）仍走 worker fallback 推全启用 channels，与改动前一致
	AlertRuleID *uuid.UUID `json:"alert_rule_id" gorm:"type:uuid;index"`
	HostIP      string     `json:"host_ip" gorm:"size:45"`

	// 告警信息
	TriggerName  string `json:"trigger_name" gorm:"size:500"`
	TriggerID    string `json:"trigger_id" gorm:"size:100"`
	Severity     int    `json:"severity" gorm:"index"` // 0-5 (Not classified, Info, Warning, Average, High, Disaster)
	SeverityName string `json:"severity_name" gorm:"size:20"`

	// 告警内容
	Problem      string     `json:"problem" gorm:"type:text"`
	ProblemStart time.Time  `json:"problem_start"`
	ProblemEnd   *time.Time `json:"problem_end"`
	Duration     int        `json:"duration"` // 秒

	// 状态
	Status      string     `json:"status" gorm:"size:20;index;default:problem"` // problem, acknowledged, resolved
	AckTime     *time.Time `json:"ack_time"`
	AckUser     string     `json:"ack_user" gorm:"size:100"`
	ResolveTime *time.Time `json:"resolve_time"`
	ResolveUser string     `json:"resolve_user" gorm:"size:100"`

	// 误报标记（小改进 #2：标记误报 + ML 训练集导出）
	IsFalsePositive   bool       `json:"is_false_positive" gorm:"default:false;index"`
	MarkedBy          *string    `json:"marked_by" gorm:"size:100"`
	MarkedAt          *time.Time `json:"marked_at"`
	FalsePositiveNote *string    `json:"false_positive_note" gorm:"type:text"`

	// 关联
	// D-3: 指向本系统 tickets.id（ADR-0004 工单 SoT 归位 ITmanager，GLPI 降级为可选只读参考）
	// 写入方随「告警一键建单」功能落地，当前字段无写入者。
	TicketID *uuid.UUID `json:"ticket_id" gorm:"type:uuid"`
	AssetID  *uuid.UUID `json:"asset_id" gorm:"type:uuid;index"`

	// 来源
	Source string `json:"source" gorm:"size:20;default:zabbix"` // zabbix, netbox, manual

	// 统计
	RepeatCount int `json:"repeat_count" gorm:"default:0"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (a *Alert) TableName() string {
	return "alerts"
}

// AlertRule 告警规则
type AlertRule struct {
	ID          uuid.UUID `json:"id" gorm:"type:uuid;primary_key;default:gen_random_uuid()"`
	Name        string    `json:"name" gorm:"size:100;not null"`
	Description string    `json:"description" gorm:"type:text"`

	// 条件
	Condition string `json:"condition" gorm:"type:text"` // JSON 格式的表达式
	AssetType string `json:"asset_type" gorm:"size:50"`  // server, switch, router
	HostGroup string `json:"host_group" gorm:"size:100"`

	// 阈值
	Metric    string  `json:"metric" gorm:"size:100"`  // cpu, memory, disk
	Operator  string  `json:"operator" gorm:"size:10"` // >, <, =, >=, <=
	Threshold float64 `json:"threshold"`
	Duration  int     `json:"duration"` // 持续时间(秒)

	// 告警级别
	Severity     int    `json:"severity"` // 1-5
	SeverityName string `json:"severity_name" gorm:"size:20"`

	// 通知
	NotifyEnabled  bool   `json:"notify_enabled" gorm:"default:true"`
	NotifyChannels string `json:"notify_channels" gorm:"type:text"` // JSON: ["dingtalk", "email"]
	NotifyUsers    string `json:"notify_users" gorm:"type:text"`    // JSON: ["user1", "user2"]

	// 状态
	IsEnabled bool `json:"is_enabled" gorm:"default:true"`
	Priority  int  `json:"priority" gorm:"default:0"` // 排序

	CreatedBy *uuid.UUID `json:"created_by" gorm:"type:uuid"`
	UpdatedBy *uuid.UUID `json:"updated_by" gorm:"type:uuid"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

func (r *AlertRule) TableName() string {
	return "alert_rules"
}

// AlertRuleTriggerMap M38-B: triggerid (e.g. Zabbix trigger.triggerid) → rule_id 映射
//
// 设计要点（intent-M38-B.md）：
//
//   - triggerid 唯一 → 同一 trigger 只允许一个 rule；last-write-wins 由应用层按
//     created_at DESC 取最新；
//   - ON DELETE CASCADE rule_id（迁移 000038 已建），rule 被删映射也跟着没意义；
//   - 反向（rule_id 改值）由应用层通过 ListMappings + DeleteMapping + CreateMapping
//     走业务流，不在该表的语义内。
//
// 关联：
//   - AlertRule.TriggerMappings（Has Many）：给运维 UI 用，本次 round 不暴露，但模型
//     完整可读；
//   - 该模型本身用 DAOs（service 层 alert_rule_mapper.go）做 CRUD —— 不在 Round 5 范围。
type AlertRuleTriggerMap struct {
	TriggerID string    `json:"triggerid" gorm:"type:varchar(100);primary_key;column:triggerid"`
	RuleID    uuid.UUID `json:"rule_id" gorm:"type:uuid;not null;index;column:rule_id"`
	CreatedAt time.Time `json:"created_at"`

	// 关联 (Preload 用) —— 业务 CRUD 不靠它, 仅给运维 UI 在列表里展示 rule.name 用
	AlertRule *AlertRule `json:"alert_rule,omitempty" gorm:"foreignKey:RuleID;references:ID"`
}

// TableName GORM 自动迁移靠它寻表
func (a *AlertRuleTriggerMap) TableName() string { return "alert_rule_trigger_map" }

// NotificationChannel 通知渠道
type NotificationChannel struct {
	ID   uuid.UUID `json:"id" gorm:"type:uuid;primary_key;default:gen_random_uuid()"`
	Name string    `json:"name" gorm:"size:100;not null"`
	Type string    `json:"type" gorm:"size:20;not null"` // dingtalk, email, wechat, webhook

	// 配置
	Config    string `json:"config" gorm:"type:text"` // JSON 配置
	IsEnabled bool   `json:"is_enabled" gorm:"default:true"`
	IsDefault bool   `json:"is_default" gorm:"default:false"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (n *NotificationChannel) TableName() string {
	return "notification_channels"
}

// NotificationLog 通知日志
type NotificationLog struct {
	ID          uuid.UUID `json:"id" gorm:"type:uuid;primary_key;default:gen_random_uuid()"`
	AlertID     uuid.UUID `json:"alert_id" gorm:"type:uuid;index"`
	ChannelID   uuid.UUID `json:"channel_id" gorm:"type:uuid"`
	ChannelName string    `json:"channel_name" gorm:"size:100"`
	Recipient   string    `json:"recipient" gorm:"size:255"` // 接收人
	Content     string    `json:"content" gorm:"type:text"`
	Status      string    `json:"status" gorm:"size:20"` // success, failed
	ErrorMsg    string    `json:"error_msg" gorm:"size:500"`
	SentAt      time.Time `json:"sent_at"`
	CreatedAt   time.Time `json:"created_at"`
}

func (n *NotificationLog) TableName() string {
	return "notification_logs"
}
