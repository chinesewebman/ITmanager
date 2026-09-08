package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Ticket 工单
type Ticket struct {
	ID             uuid.UUID  `json:"id" gorm:"type:uuid;primary_key;default:gen_random_uuid()"`
	TicketNumber   string     `json:"ticket_number" gorm:"size:50;uniqueIndex"`
	Title          string     `json:"title" gorm:"size:255;not null"`
	Description    string     `json:"description" gorm:"type:text"`
	TicketType     string     `json:"ticket_type" gorm:"size:20"`         // incident, request, problem, change
	Priority       string     `json:"priority" gorm:"size:20"`            // low, medium, high, critical
	Status         string     `json:"status" gorm:"size:20;default:open"` // open, in_progress, resolved, closed
	RequesterID    *uuid.UUID `json:"requester_id" gorm:"type:uuid"`
	RequesterName  string     `json:"requester_name" gorm:"size:100"`
	RequesterEmail string     `json:"requester_email" gorm:"size:255"`
	AssigneeID     *uuid.UUID `json:"assignee_id" gorm:"type:uuid"`
	AssigneeName   string     `json:"assignee_name" gorm:"size:100"`
	Category       string     `json:"category" gorm:"size:50"`
	Tags           string     `json:"tags" gorm:"type:jsonb;default:'[]'"` // JSON array
	AssetID        *uuid.UUID `json:"asset_id" gorm:"type:uuid"`
	AssetName      string     `json:"asset_name" gorm:"size:255"`
	ExternalID     string     `json:"external_id" gorm:"size:100"`          // GLPI ticket ID
	Source         string     `json:"source" gorm:"size:20;default:manual"` // manual, email, api, glpi
	Resolution     string     `json:"resolution" gorm:"type:text"`
	ResolvedAt     *time.Time `json:"resolved_at"`
	ClosedAt       *time.Time `json:"closed_at"`
	DueDate        *time.Time `json:"due_date"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
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

// generateTicketNumber 生成工单号 TICKET-YYYYMMDD-<序号>。
//
// 序号 = 当天已建工单数，转成 A…Z / AA / AB… 的进位标签（原实现用全表 Count()%26，
// 同一天第 27 张会与第 1 张同号，撞 ticket_number 唯一索引，见缺陷 D-2）。
// 并发下仍可能算出同一个号 —— 由唯一索引兜底 + TicketService.Create 的冲突重试处理。
func generateTicketNumber(db *gorm.DB) string {
	prefix := "TICKET-" + time.Now().Format("20060102") + "-"
	var count int64
	db.Model(&Ticket{}).Where("ticket_number LIKE ?", prefix+"%").Count(&count)
	return prefix + seqLabel(count)
}

// seqLabel 把序号转成 Excel 风格字母标签：0→A, 25→Z, 26→AA, 27→AB …
func seqLabel(n int64) string {
	if n < 0 {
		n = 0
	}
	var buf []byte
	for {
		buf = append([]byte{byte('A' + n%26)}, buf...)
		n = n/26 - 1
		if n < 0 {
			break
		}
	}
	return string(buf)
}
