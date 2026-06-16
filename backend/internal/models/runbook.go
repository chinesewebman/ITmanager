package models

import (
	"time"

	"github.com/google/uuid"
)

// Runbook 标准操作手册（SOP）
// 关联告警处理：按告警的 asset_type + severity 推荐 Runbook
type Runbook struct {
	ID        uuid.UUID `json:"id" gorm:"type:uuid;primary_key;default:gen_random_uuid()"`
	Title     string    `json:"title" gorm:"size:255;not null;index"`
	AssetType string    `json:"asset_type" gorm:"size:50;index"` // server, switch, router, firewall, storage
	Summary   string    `json:"summary" gorm:"size:500"`         // 简短摘要
	ContentMD string    `json:"content_md" gorm:"type:text"`     // 详细步骤（markdown）
	Steps     string    `json:"steps" gorm:"type:text"`          // JSON 步骤数组（前端可解析）
	Tags      string    `json:"tags" gorm:"size:255"`            // 逗号分隔
	Severity  int       `json:"severity" gorm:"default:0;index"` // 0=不限制；1-5 对应告警严重度
	Enabled   bool      `json:"enabled" gorm:"default:true;index"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName override
func (Runbook) TableName() string {
	return "runbooks"
}
