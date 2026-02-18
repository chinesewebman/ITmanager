package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Ticket 工单
type Ticket struct {
	ID              uuid.UUID      `json:"id" gorm:"type:uuid;primary_key;default:gen_random_uuid()"`
	TicketNumber    string         `json:"ticket_number" gorm:"size:50;uniqueIndex"`
	Title           string         `json:"title" gorm:"size:255;not null"`
	Description     string         `json:"description" gorm:"type:text"`
	TicketType      string         `json:"ticket_type" gorm:"size:20"` // incident, request, problem, change
	Priority        string         `json:"priority" gorm:"size:20"`     // low, medium, high, critical
	Status          string         `json:"status" gorm:"size:20;default:open"` // open, in_progress, resolved, closed
	RequesterID     *uuid.UUID     `json:"requester_id" gorm:"type:uuid"`
	RequesterName   string         `json:"requester_name" gorm:"size:100"`
	RequesterEmail  string         `json:"requester_email" gorm:"size:255"`
	AssigneeID     *uuid.UUID     `json:"assignee_id" gorm:"type:uuid"`
	AssigneeName   string         `json:"assignee_name" gorm:"size:100"`
	Category        string         `json:"category" gorm:"size:50"`
	Tags            string         `json:"tags" gorm:"type:jsonb;default:'[]'"` // JSON array
	AssetID         *uuid.UUID     `json:"asset_id" gorm:"type:uuid"`
	AssetName       string         `json:"asset_name" gorm:"size:255"`
	ExternalID      string         `json:"external_id" gorm:"size:100"` // GLPI ticket ID
	Source          string         `json:"source" gorm:"size:20;default:manual"` // manual, email, api, glpi
	Resolution      string         `json:"resolution" gorm:"type:text"`
	ResolvedAt      *time.Time     `json:"resolved_at"`
	ClosedAt        *time.Time     `json:"closed_at"`
	DueDate         *time.Time     `json:"due_date"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

func (t *Ticket) TableName() string {
	return "tickets"
}

// BeforeCreate 创建前
func (t *Ticket) BeforeCreate(tx *gorm.DB) error {
	if t.ID == uuid.Nil {
		t.ID = uuid.New()
	}
	// 生成工单号
	if t.TicketNumber == "" {
		t.TicketNumber = generateTicketNumber(tx)
	}
	return nil
}

func generateTicketNumber(db *gorm.DB) string {
	var count int64
	db.Model(&Ticket{}).Count(&count)
	return "TICKET-" + time.Now().Format("20060102") + "-" + string(rune('A'+count%26))
}
