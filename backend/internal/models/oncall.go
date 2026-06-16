package models

import (
	"time"

	"github.com/google/uuid"
)

// OncallSchedule 值班计划（定义一组轮值规则）
//
// 一个 schedule 包含多个 shift（一段时间归属一个 owner）
// 例如：dev-team 每周一至周五 09:00-18:00
type OncallSchedule struct {
	ID          uuid.UUID `json:"id" gorm:"type:uuid;primary_key;default:gen_random_uuid()"`
	Name        string    `json:"name" gorm:"size:100;not null"`
	Description string    `json:"description" gorm:"type:text"`
	Timezone    string    `json:"timezone" gorm:"size:50;default:'Asia/Shanghai'"`
	Enabled     bool      `json:"enabled" gorm:"default:true"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (o *OncallSchedule) TableName() string { return "oncall_schedules" }

// OncallShift 值班班次（一段时间归属一个 user）
type OncallShift struct {
	ID         uuid.UUID `json:"id" gorm:"type:uuid;primary_key;default:gen_random_uuid()"`
	ScheduleID uuid.UUID `json:"schedule_id" gorm:"type:uuid;not null;index"`
	UserID     uuid.UUID `json:"user_id" gorm:"type:uuid;not null;index"`
	UserName   string    `json:"user_name" gorm:"size:100"`
	StartsAt   time.Time `json:"starts_at" gorm:"not null;index"`
	EndsAt     time.Time `json:"ends_at" gorm:"not null;index"`
	CreatedAt  time.Time `json:"created_at"`
}

func (o *OncallShift) TableName() string { return "oncall_shifts" }

// EscalationPolicy 升级策略
//
// 告警触发 → 通知 level 1 user
// 5 分钟未 ack → 通知 level 2
// 5 分钟仍未 ack → 通知 level 3 + 全频道广播
//
// Levels 不直接嵌入（gorm 不支持嵌 struct），通过 service 层 ListLevels() 单独 attach
type EscalationPolicy struct {
	ID        uuid.UUID `json:"id" gorm:"type:uuid;primary_key;default:gen_random_uuid()"`
	Name      string    `json:"name" gorm:"size:100;not null"`
	Enabled   bool      `json:"enabled" gorm:"default:true"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	// 列表响应时由 service 填充
	Levels []EscalationLevel `json:"levels" gorm:"-"`
}

func (e *EscalationPolicy) TableName() string { return "escalation_policies" }

// EscalationLevel 单个升级层级
type EscalationLevel struct {
	ID            uuid.UUID `json:"id" gorm:"type:uuid;primary_key;default:gen_random_uuid()"`
	PolicyID      uuid.UUID `json:"policy_id" gorm:"type:uuid;not null;index"`
	Level         int       `json:"level" gorm:"not null"`      // 1, 2, 3
	TargetType    string    `json:"target_type" gorm:"size:20"` // user, schedule, channel
	TargetID      string    `json:"target_id" gorm:"size:100"`  // UUID 或 schedule name
	WaitMinutes   int       `json:"wait_minutes" gorm:"default:5"`
	NotifyMethods string    `json:"notify_methods" gorm:"size:255"` // comma: email,sms,webhook
}

func (e *EscalationLevel) TableName() string { return "escalation_levels" }

// OncallCurrent 当前值班（查询结果）
type OncallCurrent struct {
	ScheduleID   uuid.UUID `json:"schedule_id"`
	ScheduleName string    `json:"schedule_name"`
	UserID       uuid.UUID `json:"user_id"`
	UserName     string    `json:"user_name"`
	StartsAt     time.Time `json:"starts_at"`
	EndsAt       time.Time `json:"ends_at"`
	ShiftID      uuid.UUID `json:"shift_id"`
}
