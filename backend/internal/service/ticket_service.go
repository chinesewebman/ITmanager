package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"network-monitor-platform/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// TicketFilter 工单列表筛选
type TicketFilter struct {
	Status   string
	Priority string
	Page     int
	PageSize int
	// v2.0 cursor 分页: 非空时走 (created_at, id) 二元组 < 翻页, O(log N)
	// 为空时走 v1.x 行为 (Page/PageSize offset 翻页)
	CursorTS time.Time
	CursorID uuid.UUID
}

// TicketService 工单业务接口
type TicketService interface {
	List(ctx context.Context, f TicketFilter) (items []models.Ticket, total int64, err error)
	Get(ctx context.Context, id string) (*models.Ticket, error)
	Create(ctx context.Context, t *models.Ticket) error
	Update(ctx context.Context, id string, updates map[string]interface{}) (*models.Ticket, error)
	// CreateFromAlert 从告警派生一张工单并把 alerts.ticket_id 指回去（TODO D-3）。
	// created=false 表示该告警已有关联工单，直接返回既有那张（幂等）。
	CreateFromAlert(ctx context.Context, alertID, userID string) (ticket *models.Ticket, created bool, err error)
}

type ticketService struct {
	db *gorm.DB
}

func NewTicketService(db *gorm.DB) TicketService {
	return &ticketService{db: db}
}

func (s *ticketService) List(ctx context.Context, f TicketFilter) ([]models.Ticket, int64, error) {
	q := s.db.WithContext(ctx).Model(&models.Ticket{})
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	if f.Priority != "" {
		q = q.Where("priority = ?", f.Priority)
	}
	page := f.Page
	if page < 1 {
		page = 1
	}
	pageSize := f.PageSize
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 500 {
		pageSize = 500
	}
	var items []models.Ticket
	q = q.Order("created_at DESC, id DESC") // v2.0 cursor: 二元组排序
	// v2.0 cursor 分页: 二元组 < 走联合索引, O(log N)
	if !f.CursorTS.IsZero() && f.CursorID != uuid.Nil {
		q = q.Where("(created_at, id) < (?, ?)", f.CursorTS, f.CursorID)
		// cursor 模式不跑 Count (cursor 翻页用 hasMore 检测)
		if err := q.Limit(pageSize).Find(&items).Error; err != nil {
			return nil, 0, err
		}
		return items, 0, nil
	}
	// v1.x 兼容: page/size offset 翻页 —— 只有这条路径需要 total（cursor 模式已提前 return）
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := q.Offset((page - 1) * pageSize).Limit(pageSize).Find(&items).Error; err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (s *ticketService) Get(ctx context.Context, id string) (*models.Ticket, error) {
	var t models.Ticket
	if err := s.db.WithContext(ctx).First(&t, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &t, nil
}

func (s *ticketService) Create(ctx context.Context, t *models.Ticket) error {
	if t == nil || t.Title == "" {
		return ErrInvalidInput
	}
	if t.Status == "" {
		t.Status = "open"
	}
	if t.Source == "" {
		t.Source = "manual"
	}
	if t.Tags == "" {
		t.Tags = "[]"
	}
	// M16：不传 priority 的创建请求（POST /tickets 直接 bind 模型，handler 不校验）
	// 原来会落一行 priority=''——既筛不出也不显示，比落个默认值糟得多。
	// 与上面 Status/Source/Tags 同款兜底；值取契约词表里的 normal。
	// 注：非空但词表外的值（如 "urgent"）仍原样入库，堵它要动契约，不在本轮。
	if t.Priority == "" {
		t.Priority = "normal"
	}
	// 工单号在 BeforeCreate 里按「当天已建数量」生成，并发下两个请求可能算出同一个号。
	// 唯一索引拒绝后重新生成并重试（最多 5 次），彻底消除竞态（缺陷 D-2）。
	//
	// 例外：客户端显式传了工单号（外部系统对接）时不重试 —— 悄悄换一个号会让调用方
	// 拿到的号与它请求的不一致，语义上是「这个号已被占用」，应原样返回 409。
	clientSuppliedNumber := t.TicketNumber != ""
	const maxCreateAttempts = 5
	for attempt := 1; ; attempt++ {
		err := s.db.WithContext(ctx).Create(t).Error
		if err == nil {
			return nil
		}
		if !isUniqueViolation(err) {
			return err
		}
		if clientSuppliedNumber || attempt >= maxCreateAttempts {
			return ErrAlreadyExists
		}
		// 清掉自动生成的主键与工单号，让 BeforeCreate 重新生成
		t.ID = uuid.Nil
		t.TicketNumber = ""
	}
}

// CreateFromAlert 从告警一键建单（TODO D-3，设计见 docs/FIX-PLAN-ALERT-TICKET.md）。
//
// **认领优先**：先做条件 UPDATE「若此告警尚无关联工单，就把它认领给 newID」，抢到才插票。
// 「先查空再建票」的写法在两次点击同时到达时两边都查到空 → 各建一张票，而双击恰恰是最常见
// 的并发。条件 UPDATE 是数据库层面的原子裁决点：N 个并发请求里恰好一个 RowsAffected=1。
//
// 没抢到的请求读出既有 ticket_id 返回那张票（created=false）——重复点击拿到的是同一张票，
// 不是错误。认领与插票同一事务：插票失败（如工单号撞唯一索引）时认领一并回滚，不留悬空指针。
func (s *ticketService) CreateFromAlert(ctx context.Context, alertID, userID string) (*models.Ticket, bool, error) {
	var (
		out     *models.Ticket
		created bool
	)
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 读告警拿派生字段。这次读到的 ticket_id 只用于填工单内容，胜负由下面的条件 UPDATE 定。
		var alert models.Alert
		if err := tx.First(&alert, "id = ?", alertID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}

		newID := uuid.New()
		res := tx.Model(&models.Alert{}).
			Where("id = ? AND ticket_id IS NULL", alertID).
			Update("ticket_id", newID)
		if res.Error != nil {
			return res.Error
		}

		if res.RowsAffected == 1 {
			// 抢到认领：工单 ID 就用刚写进 alerts.ticket_id 的那个，任何时刻 ticket_id
			// 都指向本事务将要插入（或已插入）的那一行。
			t := ticketFromAlert(&alert, userID)
			t.ID = newID
			if err := tx.Create(t).Error; err != nil {
				return err // 整个事务回滚，上面的认领一并撤销
			}
			out, created = t, true
			return nil
		}

		// 没抢到：告警已被关联（并发赢家提交了，或自己重复点）。重读一次——
		// 上面那次 First 可能是并发赢家提交**之前**的快照，其 ticket_id 还是 NULL。
		var cur models.Alert
		if err := tx.First(&cur, "id = ?", alertID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		if cur.TicketID == nil {
			// UPDATE 没命中 ⇔ ticket_id 非 NULL（或行不存在）。重读后仍为空说明关联
			// 在这一瞬间被清掉了，属不该出现的中间态 —— 报错而不是猜。
			return fmt.Errorf("告警 %s 认领失败但 ticket_id 仍为空", alertID)
		}
		var existing models.Ticket
		if err := tx.First(&existing, "id = ?", *cur.TicketID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				// 关联悬空。本系统无删除工单的端点，理论上不可达，属防御性分支：
				// 报 404 而不是静默改写别人写下的关联。
				return ErrNotFound
			}
			return err
		}
		out, created = &existing, false
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return out, created, nil
}

// ticketFromAlert 把告警的现场信息派生成一张待建工单。
func ticketFromAlert(a *models.Alert, userID string) *models.Ticket {
	title := a.TriggerName
	if title == "" {
		title = a.Problem
	}
	if title == "" {
		title = "告警 " + a.AlertID
	}
	if a.HostName != "" {
		title = a.HostName + " " + title
	}

	var b strings.Builder
	b.WriteString("由告警一键建单生成。\n")
	fmt.Fprintf(&b, "告警 ID：%s\n", a.AlertID)
	fmt.Fprintf(&b, "主机：%s", a.HostName)
	if a.HostIP != "" {
		fmt.Fprintf(&b, "（%s）", a.HostIP)
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "触发器：%s\n", a.TriggerName)
	fmt.Fprintf(&b, "严重级别：%s（%d）\n", a.SeverityName, a.Severity)
	if !a.ProblemStart.IsZero() {
		fmt.Fprintf(&b, "开始时间：%s\n", a.ProblemStart.Format(time.RFC3339))
	}
	if a.Problem != "" {
		fmt.Fprintf(&b, "现象：%s\n", a.Problem)
	}

	return &models.Ticket{
		Title:         truncateRunes(title, 255), // tickets.title varchar(255)，触发器名可长 500
		Description:   b.String(),
		TicketType:    "incident",
		Priority:      priorityFromSeverity(a.Severity),
		Source:        "alert",
		AssetID:       a.AssetID,
		AssetName:     a.HostName,
		RequesterName: userID,
	}
}

// priorityFromSeverity Zabbix 0-5 严重级别 → tickets.priority。
//
// 取值限定在 openapi.yaml 的 Ticket.priority enum（low/normal/high/critical），
// 与 GLPI 同步（integration/glpi.go）、手工建单表单（TicketFormModal）一致。
// 原先本函数返回 medium —— 与契约的 normal 是同一个「普通」的两套拼法，后果是
// /tickets 按「普通」筛选查不到这里建出来的票（M16）。迁移 000023 已把存量归一。
func priorityFromSeverity(sev int) string {
	switch {
	case sev >= 5: // Disaster
		return "critical"
	case sev == 4: // High
		return "high"
	case sev >= 2: // Warning / Average
		return "normal"
	default: // 0 Not classified / 1 Information
		return "low"
	}
}

// truncateRunes 按**字符**截断到 max 个 rune。
// 不能用 s[:max] 那种按 byte 的切法 —— 中文被切在两字节之间会留下非法 UTF-8，
// 而 tickets.title 是 PG 的 varchar(255)（按字符计数），rune 数才是对的尺子。
func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}

func (s *ticketService) Update(ctx context.Context, id string, updates map[string]interface{}) (*models.Ticket, error) {
	// 🐛 BUG#24: 原版 len==0 走 Get + 主路径 First 重复，统一为 1 次 First
	var t models.Ticket
	if err := s.db.WithContext(ctx).First(&t, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	// 空 updates 直接返当前记录（不写库）
	if len(updates) == 0 {
		return &t, nil
	}
	// 关闭工单时自动写入 closed_at
	if status, ok := updates["status"].(string); ok && status == "closed" {
		now := time.Now()
		updates["closed_at"] = &now
	}
	if err := s.db.WithContext(ctx).Model(&t).Updates(updates).Error; err != nil {
		return nil, err
	}
	return &t, nil
}
