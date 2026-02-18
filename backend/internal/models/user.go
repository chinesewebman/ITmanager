package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// User 用户
type User struct {
	ID           uuid.UUID      `json:"id" gorm:"type:uuid;primary_key;default:gen_random_uuid()"`
	Username     string         `json:"username" gorm:"uniqueIndex;size:50;not null"`
	PasswordHash string         `json:"-" gorm:"size:255;not null"`
	Nickname     string         `json:"nickname" gorm:"size:100"`
	Email        string         `json:"email" gorm:"size:255"`
	Phone        string         `json:"phone" gorm:"size:20"`
	Avatar       string         `json:"avatar" gorm:"size:500"`
	Status       string         `json:"status" gorm:"size:20;default:active"` // active, inactive, locked
	DepartmentID *uuid.UUID     `json:"department_id" gorm:"type:uuid"`
	Role        string         `json:"role" gorm:"size:20;default:user"` // admin, operator, readonly
	FailedLogin int            `json:"failed_login" gorm:"default:0"`
	LockedUntil *time.Time    `json:"locked_until"`
	LastLogin   *time.Time    `json:"last_login"`
	LastLoginIP string        `json:"last_login_ip" gorm:"size:50"`
	CreatedAt   time.Time    `json:"created_at"`
	UpdatedAt   time.Time    `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `json:"-" gorm:"index"`
}

func (u *User) TableName() string {
	return "users"
}

// BeforeCreate 创建前
func (u *User) BeforeCreate(tx *gorm.DB) error {
	if u.ID == uuid.Nil {
		u.ID = uuid.New()
	}
	return nil
}

// APIKey API密钥
type APIKey struct {
	ID          uuid.UUID      `json:"id" gorm:"type:uuid;primary_key;default:gen_random_uuid()"`
	UserID      uuid.UUID      `json:"user_id" gorm:"type:uuid;not null"`
	Name        string         `json:"name" gorm:"size:100;not null"`
	KeyHash     string         `json:"-" gorm:"size:255;not null"`
	Prefix      string         `json:"prefix" gorm:"size:20;not null"`
	Permissions []string       `json:"permissions" gorm:"type:text[]"`
	IPWhitelist []string       `json:"ip_whitelist" gorm:"type:text[]"`
	RateLimit   int            `json:"rate_limit" gorm:"default:1000"`
	ExpiresAt   *time.Time    `json:"expires_at"`
	LastUsedAt  *time.Time    `json:"last_used_at"`
	Status      string         `json:"status" gorm:"size:20;default:active"`
	CreatedAt   time.Time    `json:"created_at"`
	UpdatedAt   time.Time    `json:"updated_at"`
}

func (a *APIKey) TableName() string {
	return "api_keys"
}

// AuditLog 审计日志
type AuditLog struct {
	ID         uuid.UUID      `json:"id" gorm:"type:uuid;primary_key;default:gen_random_uuid()"`
	UserID     *uuid.UUID    `json:"user_id" gorm:"type:uuid"`
	Username   string         `json:"username" gorm:"size:100"`
	Action     string         `json:"action" gorm:"size:50;not null"`
	Resource   string         `json:"resource" gorm:"size:100"`
	ResourceID *uuid.UUID    `json:"resource_id" gorm:"type:uuid"`
	Method     string         `json:"method" gorm:"size:10"`
	Path       string         `json:"path" gorm:"size:500"`
	IP         string         `json:"ip" gorm:"size:50"`
	UserAgent  string         `json:"user_agent" gorm:"size:500"`
	Status     int            `json:"status"` // 200, 400, 401, 403, 500
	ErrorMsg   string         `json:"error_msg" gorm:"size:1000"`
	RequestID  string         `json:"request_id" gorm:"size:50"`
	CreatedAt  time.Time     `json:"created_at"`
}

func (a *AuditLog) TableName() string {
	return "audit_logs"
}
