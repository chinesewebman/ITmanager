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
	Priority       string     `json:"priority" gorm:"size:20"`            // low, normal, high, critical（= openapi Ticket.priority；M16 归一并迁移 000023）
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

// AssignTicketNumbers 给**一批**待插入的工单预分配互不相同的工单号（TODO G-25）。
//
// 为什么批量路径必须显式分配：gorm 的 before_create 回调在 gorm:create 之前执行，
// 且对整片 slice 的**每一行都跑完**才进 INSERT —— 每行各自调 generateTicketNumber
// 查到的「当天条数」是同一个值 → 整批同一个号 → tickets.ticket_number 唯一索引
// 把整条 INSERT 拒掉，一次新增 ≥2 张票的同步全失败。逐条 Create 不受影响
// （每行插完再算下一行，序号自然递增）。
//
// 已在 tickets[i].TicketNumber 里填了号的行走 BeforeCreate 的逃生门
// （TicketNumber != "" 时不覆盖），原样保留、不占用本批的自动序号。
//
// M26/D-9：按「当天已占用的标签集合」分配并跳过空洞，而不是按条数线性推进。
// 条数法只在编号连续时成立 —— 一旦出现空洞，条数会小于最大序号，回绕后撞上已占用的号。
//
// M26 引入了一个新的空洞来源：SyncFromGLPI 的 ON CONFLICT DoNothing 在
// 「预查之后、插入之前」有并发同步插了同一 external_id 时会跳过该行，
// 而它的号已经分配掉了。后果不是丢一张票，而是**当天后续每次 GLPI 同步都 500**
// （撞 ticket_number 唯一索引，跨天自愈，运维无自助恢复手段）。
//
// 边界：本函数**不解决并发**。两个同步各自读到同一份 used 快照仍会撞号，
// 那条路径由 ticket_number 唯一索引 + TicketService.Create 的重试兜底
// （需求 §1.5 已登记为超范围）。本函数只保证「已有空洞不会导致重号」。
func AssignTicketNumbers(db *gorm.DB, tickets []Ticket) {
	prefix := ticketNumberPrefix()
	used := usedTicketLabels(db, prefix)

	// 逃生门行也要占位：否则同一批里「已填号 = TICKET-今天-A」与自动分配会各自拿到 A，
	// 整批被 ticket_number 唯一索引拒绝。旧法同样有此洞，顺手关掉。
	for i := range tickets {
		if n := tickets[i].TicketNumber; n != "" {
			used[n] = struct{}{}
		}
	}

	next := int64(0)
	for i := range tickets {
		if tickets[i].TicketNumber != "" {
			continue
		}
		// used 有限、next 单调增、seqLabel 无界 → 必然终止。
		for {
			label := prefix + seqLabel(next)
			next++
			if _, taken := used[label]; !taken {
				used[label] = struct{}{}
				tickets[i].TicketNumber = label
				break
			}
		}
	}
}

// usedTicketLabels 取当天已占用的工单号集合（含空洞）。
func usedTicketLabels(db *gorm.DB, prefix string) map[string]struct{} {
	var nums []string
	db.Model(&Ticket{}).Where("ticket_number LIKE ?", prefix+"%").Pluck("ticket_number", &nums)
	used := make(map[string]struct{}, len(nums))
	for _, n := range nums {
		used[n] = struct{}{}
	}
	return used
}

// generateTicketNumber 生成工单号 TICKET-YYYYMMDD-<序号>，只用于**逐条** Create。
//
// 序号 = 当天已建工单数，转成 A…Z / AA / AB… 的进位标签（原实现用全表 Count()%26，
// 同一天第 27 张会与第 1 张同号，撞 ticket_number 唯一索引，见缺陷 D-2）。
// 并发下仍可能算出同一个号 —— 由唯一索引兜底 + TicketService.Create 的冲突重试处理。
//
// ⚠️ 批量插入（CreateInBatches）**不能**走这里，必须先用 AssignTicketNumbers 预分配，
// 否则整批同号（见该函数注释）。
func generateTicketNumber(db *gorm.DB) string {
	prefix := ticketNumberPrefix()
	return prefix + seqLabel(nextTicketSeq(db, prefix))
}

// ticketNumberPrefix 当天的工单号前缀：TICKET-YYYYMMDD-。
func ticketNumberPrefix() string {
	return "TICKET-" + time.Now().Format("20060102") + "-"
}

// nextTicketSeq 当天已建工单数 —— 即下一个可用序号的起点。
func nextTicketSeq(db *gorm.DB, prefix string) int64 {
	var count int64
	db.Model(&Ticket{}).Where("ticket_number LIKE ?", prefix+"%").Count(&count)
	return count
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
