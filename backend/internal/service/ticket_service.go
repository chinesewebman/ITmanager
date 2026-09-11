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
	"gorm.io/gorm/clause"
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

// Actor 一次写操作的经手人 —— 进 ticket_history 的 actor_id / actor_name 快照。
//
// 两个字段而不是两个裸 string：需要 **id（稳定）+ name（可读快照）** 两样东西，13 个调用点
// 乘 2 个位置参数既难读又容易传反。ID 用指针：内部调用（seed / GLPI 同步 / 定时任务）
// 没有 HTTP 上下文，用零值 uuid.UUID 表示「无」会和真实零值混淆。
//
// Name 是**快照**不是引用：用户改名或删号后，历史里仍要看得见当时是谁经的手
// （同 audit_logs.username 的取向）。
type Actor struct {
	ID   *uuid.UUID
	Name string
}

// TicketService 工单业务接口
type TicketService interface {
	List(ctx context.Context, f TicketFilter) (items []models.Ticket, total int64, err error)
	Get(ctx context.Context, id string) (*models.Ticket, error)
	// ListHistory 某张工单的经手历史（最新在前，平铺行）。准入与 Get 同级。
	ListHistory(ctx context.Context, ticketID string, page, pageSize int) ([]models.TicketHistory, int64, error)
	Create(ctx context.Context, t *models.Ticket, actor Actor) error
	Update(ctx context.Context, id string, updates map[string]interface{}, actor Actor) (*models.Ticket, error)
	// CreateFromAlert 从告警派生一张工单并把 alerts.ticket_id 指回去（TODO D-3）。
	// created=false 表示该告警已有关联工单，直接返回既有那张（幂等）。
	//
	// 收 Actor 而不是裸 userID（M25 步骤 5c）：旧签名只把 userID 当 requester 名用，
	// handler 里 actorFromContext 算出的 ID 被丢掉 → 这条路径的**出生行没有 actor_id**。
	// 与 Create 收同一个类型，两条建单路径对「谁经手」的表达只有一套。
	CreateFromAlert(ctx context.Context, alertID string, actor Actor) (ticket *models.Ticket, created bool, err error)
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

// ListHistory 某张工单的经手历史（append-only，最新在前）。
//
// 准入与 GET /tickets/:id 同级（燕如 2026-09-11 拍板③「跟工单本身的可见性」）：
// 这里**不**另做归属过滤，判据留在 Get 一处 —— 端点可见性变了两个方法一起变，
// 免得历史端点自己长出一套谁看不见谁的规则。
//
// 先做存在性检查再查历史：不存在的工单返回 ErrNotFound，而不是一个空列表 ——
// 空列表会让调用方把「这张票不存在」读成「这张票还没被改过」，前端会据此渲染出
// 一个并不存在的工单详情页。
func (s *ticketService) ListHistory(ctx context.Context, ticketID string, page, pageSize int) ([]models.TicketHistory, int64, error) {
	if _, err := s.Get(ctx, ticketID); err != nil {
		return nil, 0, err
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	// 上限 500 与 List 同口径（见上）。契约层 page_size 没有 maximum，不能指望
	// schema 兜底 —— 不 clamp 就是可拖库。
	if pageSize > 500 {
		pageSize = 500
	}
	q := s.db.WithContext(ctx).Model(&models.TicketHistory{}).Where("ticket_id = ?", ticketID)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []models.TicketHistory
	// created_at DESC, id DESC 是**全序**（T-45）：同一批次的多行 created_at 完全相同，
	// 只按时间排的话翻页会重复或漏行。
	if err := q.Order("created_at DESC, id DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).Find(&items).Error; err != nil {
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

func (s *ticketService) Create(ctx context.Context, t *models.Ticket, actor Actor) error {
	if t == nil {
		return ErrInvalidInput
	}
	if t.Title == "" {
		// 带原因而不是裸 ErrInvalidInput：handler 直接把 err.Error() 当 400 body，
		// 原来 handler 里写死「工单标题不能为空」，加了枚举校验后那句话会变成假话。
		return fmt.Errorf("%w: title 不能为空", ErrInvalidInput)
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
	if t.Priority == "" {
		t.Priority = "normal"
	}
	// M18：枚举列取值必须落在契约词表内。放在默认值**之后** —— 缺省走的是 "open"/
	// "normal"，本身就是契约值；客户端显式传的词表外取值（"urgent" / "medium" /
	// "Closed"）在这里被拒，不再静默落库后从筛选器和统计卡里消失。
	//
	// 借道同一个校验函数（而不是再写两个 if）：判据只有一处，日后加枚举列不会漏。
	// Create 走结构体绑定，值已是 Go string，不存在 T-36 那种非字符串形态 ——
	// 但这里传的就是 map，形态判据同样适用，不必为「已知是 string」另开一条路径。
	if err := validateTicketEnumValues(map[string]interface{}{
		"priority": t.Priority,
		"status":   t.Status,
	}); err != nil {
		return err
	}
	// M24：建单时就带 status='closed'（补录历史工单 / 对接回灌）也必须落下 closed_at。
	// 不补的话这张票是「已关闭但没有关闭时间」，两个消费者同时看不见它：
	// dashboard 的 SLA 口径是 `status='closed' AND closed_at >= ?`（dashboard_service.go），
	// 统计里它不算已关闭；资产时间线靠 closed_at 派生「工单关闭」事件，它也永远没有。
	// Update 那边管的是「跃迁」，这里管的是「出生即关闭」，同一个不变式（closed_at 与
	// status 一致）的两个入口，两处都要守。
	// 调用方显式给了时间（回灌带原始 ClosedDate）时不覆盖。
	if t.Status == "closed" && t.ClosedAt == nil {
		now := time.Now()
		t.ClosedAt = &now
	}
	// M25：同一个不变式的第二个入口 —— 出生即 resolved 也要落下 resolved_at。
	// 不补的话这张票「已解决但没有解决时刻」：`diagnostic_service` 的资产时间线靠
	// `resolved_at IS NOT NULL` 派生「已解决」事件，它永远没有；MTTR 也算不出。
	// 调用方显式给了时间（回灌带原始 SolvedDate）时不覆盖。
	//
	// status='closed' 出生时**不补** resolved_at：那会凭空发明一个从未发生过的解决时刻。
	// closed 的语义是「保留已有的解决时间」，出生时本来就没有。
	if t.Status == "resolved" && t.ResolvedAt == nil {
		now := time.Now()
		t.ResolvedAt = &now
	}
	// 工单号在 BeforeCreate 里按「当天已建数量」生成，并发下两个请求可能算出同一个号。
	// 唯一索引拒绝后重新生成并重试（最多 5 次），彻底消除竞态（缺陷 D-2）。
	//
	// 例外：客户端显式传了工单号（外部系统对接）时不重试 —— 悄悄换一个号会让调用方
	// 拿到的号与它请求的不一致，语义上是「这个号已被占用」，应原样返回 409。
	clientSuppliedNumber := t.TicketNumber != ""
	const maxCreateAttempts = 5
	for attempt := 1; ; attempt++ {
		// **每次尝试各开一个事务**，出生历史行写在同一事务里。
		//
		// 不能把整个重试循环包进一个长事务：真 PG 下第一次 ticket_number 唯一冲突会让
		// **整个事务**进入 aborted 状态（25P02），后续尝试全部报 "current transaction is
		// aborted" —— 自愈退化成硬 500。而 sqlite 单测基座看不出这个差异（它没有 PG 的
		// 事务中止语义），所以这条只能靠形状守住，见 docs/FIX-PLAN-TICKET-HISTORY.md §2.4。
		//
		// 出生行与工单同事务的意义：冲突回滚时历史一并撤销，不会留下「工单没建成、
		// 却记了一笔出生」的孤儿行，也不会出现「工单建成了、出生事件却缺失」。
		err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := tx.Create(t).Error; err != nil {
				return err
			}
			return insertTicketBirth(tx, t.ID, uuid.New(), actor)
		})
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
func (s *ticketService) CreateFromAlert(ctx context.Context, alertID string, actor Actor) (*models.Ticket, bool, error) {
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
			t := ticketFromAlert(&alert, actor.Name)
			t.ID = newID
			if err := tx.Create(t).Error; err != nil {
				return err // 整个事务回滚，上面的认领一并撤销
			}
			// M25 步骤 5c：出生行与工单同事务 —— 与 Create 同一条不变式（冲突/失败时
			// 一起回滚，不留「票在、出生事件不在」的孤儿）。漏掉这行的后果不是「少个字段」：
			// 值班时一键建单是最常用的建单路径，而它建出来的票**时间线是空的**，
			// 读起来像「这票建了以后没人碰过」—— 那条时间线在说谎。
			//
			// 出生行不填 Source（按 §决策表 D-6）：这条票「从告警来」这一事实由
			// ticketFromAlert 写下的 tickets.source='alert' 承载，读时间线时取那里。
			if err := insertTicketBirth(tx, t.ID, uuid.New(), actor); err != nil {
				return err
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

// immutableTicketUpdateFields Update 禁止修改的列：身份列与审计列（键为小写形态）。
//
// 为什么必须有这道闸：handler 把请求体绑成 map[string]interface{} 直接交给
// gorm 的 Updates(map)，而 gorm 对每个键走 Schema.LookUpField(k) —— 先按列名、
// 再按 Go 字段名解析。于是 {"id": …} 会改写主键，{"TicketNumber": …} 会改工单号。
// 主键被改写后，D-3 建立的 alerts.ticket_id 外键就指向一个不存在的工单（悬空）；
// created_at/updated_at 被改则审计时间线失真。同款缺陷已在 channel_service.go
// 实测并收口（安全审计 H-1），这里沿用同一套判据。
//
// 用「禁改集合」而不是「可改白名单」：tickets 有 20 个业务列，绝大多数本就该可改
// （status / assignee / priority / resolution / due_date …），列白名单既维护不起，
// 也会把外部对接要写的列（external_id / resolved_at）静默丢掉；真正不能碰的只有这 4 个。
//
// 键同时收「列名」与「Go 字段名」两种小写形态（ticket_number / ticketnumber）——
// 只挡列名会让 {"TicketNumber": …} 从 Go 字段名那条路进来。
var immutableTicketUpdateFields = map[string]bool{
	"id":           true,
	"ticketnumber": true, "ticket_number": true,
	"createdat": true, "created_at": true,
	"updatedat": true, "updated_at": true,
}

// ticketPriorityValues / ticketStatusValues —— tickets 两个枚举列的契约词表
// （openapi.yaml:2448-2453 的 Ticket.priority / Ticket.status enum）。
//
// 为什么需要它：这两列在 DB 里是裸 `VARCHAR(20) NOT NULL`，**全仓无 CHECK 约束**
// （grep 过 migrations/），而 POST/PUT 写入口此前只校验「标题非空」，取值随便写。
// 词表外的值不会报错，只会**静默消失**：
//   - 工单页的优先级/状态筛选器只有契约那几档（Tickets.tsx 的 Select），选不中这些票；
//   - TicketStatsCards 用 `if (t.status in acc)` 累加，词表外状态直接不计数；
//   - TicketTable 的 PRIORITY_WEIGHT 查不到键 → 权重 0，排序静默垫底；
//   - 超过 20 字符还会撞 VARCHAR(20) → PG 报错 → 500（本该是调用方的 400）。
//
// 判据以契约为准（同 M16：normal 而非 medium）。**拒绝而不是静默改写**——把它悄悄
// 改成 normal 会把调用方的 bug 藏起来，而 400 + 契约词表是可行动的。
//
// 大小写严格：不接受 "Closed" / "HIGH"。宽容会造出「值合法但下游只认小写」的新裂缝——
// 下面 closed_at 判定是精确比较 `status == "closed"`，收了 "Closed" 就又回到
// M17 修掉的「已关闭但无关闭时间」。契约本身就是小写精确匹配。
//
// 覆盖面止于本 service：GLPI 同步（integration/service.go:270）与 seed 直连
// CreateInBatches 不经过这里，它们各自持有词表（glpi.go ConvertToTicket）。
var (
	ticketPriorityValues = map[string]bool{"critical": true, "high": true, "normal": true, "low": true}
	ticketStatusValues   = map[string]bool{"open": true, "in_progress": true, "pending": true, "resolved": true, "closed": true}
)

// ticketEnumFields Update 的枚举列校验清单。用切片不用 map：map 遍历序随机，
// 两个字段同时越界时报错文案会跳变，用例钉不住。
var ticketEnumFields = []struct {
	name    string
	allowed map[string]bool
}{
	{"priority", ticketPriorityValues},
	{"status", ticketStatusValues},
}

// validateTicketEnumValues 校验 map 里出现的枚举列取值；字段缺省即不校验（PUT 是部分更新）。
//
// fail-closed：值不是字符串（数字/对象/数组/null）一律拒绝。写成
// `if s, ok := v.(string); ok { 校验 }` 会让这些形态静默放行（docs/TRAPS.md T-36）；
// 其中 null 还会撞 tickets.status 的 NOT NULL → 500。
//
// 报错只提**契约列名**（本方常量），不回显调用方的键或值——两者都是调用方可控字符串，
// 会原样进 400 body（同 M17 的禁改列报错）。
func validateTicketEnumValues(updates map[string]interface{}) error {
	for _, f := range ticketEnumFields {
		raw, present := updates[f.name]
		if !present {
			continue
		}
		s, isString := raw.(string)
		if !isString || !f.allowed[s] {
			return fmt.Errorf("%w: %s 取值超出契约词表", ErrInvalidInput, f.name)
		}
	}
	return nil
}

func (s *ticketService) Update(ctx context.Context, id string, updates map[string]interface{}, actor Actor) (*models.Ticket, error) {
	// 空 updates 直接返当前记录（不写库、不留痕）：不进事务 —— 没有要写的行，
	// 也就没有 pre-image 与历史可言。
	// 🐛 BUG#24: 原版 len==0 走 Get + 主路径 First 重复，这里统一为 1 次 First。
	if len(updates) == 0 {
		var t models.Ticket
		if err := s.db.WithContext(ctx).First(&t, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, ErrNotFound
			}
			return nil, err
		}
		return &t, nil
	}
	// 键归一化到小写 + 挡掉禁改列：保证「校验的键 == 落库的键」（同 channel_service.go）。
	// 归一化顺带修掉一处既有不一致：{"Status":"closed"} 走 gorm 能写列，但下面
	// updates["status"] 取不到 → closed_at 不写；小写化后两者一致。
	//
	// 两阶段而不是「另建一张 norm map」：既有用例钉住了「调用方拿到的这张 map 里能看到
	// 注入的 closed_at」（Update 原地改），换成新 map 会悄悄改掉这个约定。
	// 阶段 1 只读不改（避免 range 中增删 map），阶段 2 才落地重命名。
	var renames [][2]string
	for k := range updates {
		lk := strings.ToLower(k)
		if immutableTicketUpdateFields[lk] {
			// 不回显键名：键是调用方可控字符串，会原样进 400 body
			return nil, fmt.Errorf("%w: id / ticket_number / created_at / updated_at 由系统维护，不可修改", ErrInvalidInput)
		}
		if lk != k {
			renames = append(renames, [2]string{k, lk})
		}
	}
	for _, r := range renames {
		updates[r[1]] = updates[r[0]]
		delete(updates, r[0])
	}
	// 枚举列取值校验放在归一化**之后**：Go 字段名写法（{"Status": …}）此刻已折成
	// 小写键，与列名写法走同一条判据。放在 closed_at 判定**之前**：先确认 status
	// 是契约词表里的值（精确比较 "closed"），词表外的值不该进 closed_at 判据。
	if err := validateTicketEnumValues(updates); err != nil {
		return nil, err
	}
	// M24：closed_at 跟着 status 走 —— 但判据必须落在**行自己身上**（SQL 表达式），
	// 不能落在 Go 里读到的 `t.Status` 快照上。三个场景各自的期望：
	//
	//	status 不是 closed         → NULL。工单被重开后必须清掉，否则资产时间线（
	//	                             diagnostic_service 只认 `closed_at IS NOT NULL`，不看
	//	                             status）上永久挂着一条「工单关闭」假事件。
	//	status 是 closed 且原本无值 → now。真正关闭的这一刻记时间。
	//	status 是 closed 且原本有值 → 保留（COALESCE）。原来每 PUT 一次都刷成 now，改个描述
	//	                             就把 SLA 的关闭耗时重置了。
	//
	// 为什么不写成 Go 侧的 `if t.Status == "closed"`：那要先读一次状态再判断，而读到的是
	// **快照**。两个并发 PUT（一个重开、一个关闭）都卡在读之后、写之前时，按快照判断会落成
	// `status='closed'` 且 `closed_at IS NULL` 的自相矛盾行。把判据和写入放进同一条 UPDATE
	// 原子求值就没有这个窗口；COALESCE 顺带让「关掉一张已关闭的工单」天然幂等。
	//
	// 已知边界：只传 closed_at 不传 status 的请求不进这段逻辑（gorm 原样写列）。那种请求能
	// 给已关闭的工单补时间，也能把一张 open 的工单写成「有 closed_at 却不 closed」——
	// 后者会复现本段要治的时间线假事件。没有已知调用方，登记在 §8，不在这里替对接方决定。
	// M25：resolved_at 与 closed_at 同一套语义（燕如 2026-09-11 拍板「当前状态」语义）。
	// 三个场景各自的期望：
	//
	//	status 不是 resolved/closed → NULL。重开/退回必须清掉，否则读端点与资产时间线
	//	                             仍拿它当「已解决」的证据。清掉的值不会丢 ——
	//	                             同事务的字段级 diff 会留下 old_value。
	//	status 是 resolved 且原本无值 → now。这一刻是真正的解决时刻。
	//	status 是 resolved 且原本有值 → 保留（COALESCE）。关掉描述再存一次不该把 MTTR 重置。
	//	status 是 closed             → **保留不动**：resolved→closed 时 MTTR 仍要算得出，
	//	                             清掉会让已关闭的票丢掉解决时刻。
	//
	// 与 closed_at 分开写两条表达式而不是揉成一条：两条列的转移表不同（closed_at 在任何
	// 非 closed 状态下都必须为 NULL，resolved_at 在 closed 下必须存活），揉在一起只会让
	// 「哪条列在哪个状态该是什么」变得读不出来。
	if status, ok := updates["status"].(string); ok {
		var explicitClosed, explicitResolved interface{}
		if v, ok := updates["closed_at"]; ok {
			explicitClosed = v
		}
		if v, ok := updates["resolved_at"]; ok {
			explicitResolved = v
		}
		updates["closed_at"] = gorm.Expr(
			"CASE WHEN ? = 'closed' THEN COALESCE(?, closed_at, ?) ELSE NULL END",
			status, explicitClosed, time.Now())
		// closed 这一支用 COALESCE(?, resolved_at) 而不是裸 resolved_at：默认「保留」，
		// 但调用方显式给了值就**不能悄悄丢掉**（与 resolved 那一支的取向一致；
		// 不给 now 兜底 —— 出生即关闭的票不该被发明一个解决时刻）。
		updates["resolved_at"] = gorm.Expr(
			"CASE WHEN ? = 'resolved' THEN COALESCE(?, resolved_at, ?)"+
				" WHEN ? = 'closed' THEN COALESCE(?, resolved_at) ELSE NULL END",
			status, explicitResolved, time.Now(), status, explicitResolved)
	}
	// 写入与留痕同事务：diff 的 pre-image 必须**属于本次写入**，否则并发 PUT 会让
	// 第二条历史的 from 归错。pre 用原始行 map 而不是 struct —— 见 diffTicketRows 注释。
	var out models.Ticket
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 持锁读 pre。真 PG 上是行锁（同一张票的并发 PUT 串行化）；sqlite 基座不渲染
		// FOR UPDATE（driver 明说不支持行级锁），故单测**验不到**加锁本身，只验路径。
		var pre map[string]interface{}
		if err := tx.Table("tickets").Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", id).Take(&pre).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		if err := tx.Model(&models.Ticket{}).Where("id = ?", id).Updates(updates).Error; err != nil {
			return err
		}
		// post 同样取原始行：map 能看见「库里有、模型里没有」的列（tickets 有 12 个），
		// 用 struct 比会漏掉 `{"alert_id": X}` 这类**真的写进了库**的改动。
		var post map[string]interface{}
		if err := tx.Table("tickets").Where("id = ?", id).Take(&post).Error; err != nil {
			return err
		}
		// 传了键但没有实质变化（例如 title 传的就是原值）→ 0 行历史。
		// 注意 gorm 此时仍会补 updated_at，所以「空 diff 就不写」这条**必须**按列比，
		// 不能拿「有没有发生 UPDATE」当判据。
		if changes := diffTicketRows(pre, post); len(changes) > 0 {
			ticketID, err := uuid.Parse(id)
			if err != nil {
				return fmt.Errorf("工单 id 不是合法 UUID，经手历史无法落库: %w", err)
			}
			if err := insertTicketHistory(tx, ticketID, uuid.New(), actor, changes); err != nil {
				return err
			}
		}
		// 响应体重读一次 struct：map 更新不回写任何 struct 字段，且 gorm 对 map 里的
		// clause.Expr **不回写**（schema 的 fallbackSetter 显式跳过 Expr）。不重读则刚关闭的
		// 工单回 `closed_at: null`、重开的工单回旧的关闭时间（M24 实测结论）。
		// 用零值 struct 接：带主键的 struct 会让 gorm 追加一条主键条件，生成
		// `WHERE id = $1 AND "tickets"."id" = $2`（asset_service.Restore 末尾同款）。
		return tx.First(&out, "id = ?", id).Error
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// insertTicketHistory 把一次操作的字段变更写成 ticket_history 行。
//
// 一字段一行、同批次共享 batch_id（UI 靠它分组）：沿用 asset_history 的既有形状，
// 少发明一套约定；代价是「一次操作 = N 行」，`Create` 一次批量 INSERT 补回来。
//
// source / request_id 暂不填（列可空）：它们的取值语义未定 —— 是「操作来路」还是
// 「工单来路」，以及 API Key 路径算不算 api，都需要先拍板再写死。登记在
// docs/FIX-PLAN-TICKET-HISTORY.md，不在这里替调用方决定。
// insertTicketBirth 写「工单出生」那一行（kind=created）——建单即是一次经手。
//
// **只有一行、FieldName 为 NULL**：出生改的不是某个字段，而是「这张票存在了」。
// 与 updated 行共用 batch_id 机制（出生永远独占一个批次，因为一次请求只会建一张票）。
//
// 必须与工单的 INSERT 同事务（调用方保证）：分开写会留下「工单在、出生事件不在」
// 或「出生事件在、工单不在」两种孤儿状态，而这张表的存在意义就是可信。
//
// field_name/old_value/new_value 三列都留 NULL —— 顺带说明一个**已知缺口**：
// ticket_number 被 diff 排除在系统列之外（它出生后不再变，记进 updated 是噪声），
// 于是它**在整张历史表里不出现**。读端点若要展示「这张票出生时拿到的号」，
// 得从 tickets 表现取，或在此处补一行。语义未定，登记待拍板，不在这里替调用方决定。
func insertTicketBirth(tx *gorm.DB, ticketID, batchID uuid.UUID, actor Actor) error {
	rows := []models.TicketHistory{{ //nolint:exhaustruct
		ID:        uuid.New(),
		TicketID:  ticketID,
		BatchID:   batchID,
		Kind:      models.TicketHistoryKindCreated,
		ActorID:   actor.ID,
		ActorName: actor.Name,
	}}
	return tx.Create(&rows).Error
}

func insertTicketHistory(tx *gorm.DB, ticketID, batchID uuid.UUID, actor Actor, changes []fieldChange) error {
	rows := make([]models.TicketHistory, 0, len(changes))
	for _, c := range changes {
		field := c.Field                          // 取副本地址：range 变量取址在旧 Go 上会让所有行指向同一个字段名
		rows = append(rows, models.TicketHistory{ //nolint:exhaustruct
			ID:        uuid.New(),
			TicketID:  ticketID,
			BatchID:   batchID,
			Kind:      models.TicketHistoryKindUpdated,
			FieldName: &field,
			OldValue:  c.Old,
			NewValue:  c.New,
			ActorID:   actor.ID,
			ActorName: actor.Name,
		})
	}
	return tx.Create(&rows).Error
}
