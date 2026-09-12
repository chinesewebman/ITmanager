package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"time"

	"network-monitor-platform/internal/eventbus"
	"network-monitor-platform/internal/models"
	"network-monitor-platform/internal/notification"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// AlertFilter 告警列表查询
type AlertFilter struct {
	Status          string
	Severity        int // 🐛 BUG#13: 原 string 类型与 SQL "severity >= ?" 比较会触发字符串比较；改为 int
	HostID          string
	IsFalsePositive *bool // 小改进 #2：nil=全部 true=仅误报 false=仅非误报
	Limit           int
	// v2.0 cursor 分页: 非空时, 返回 (created_at, id) < (CursorTS, CursorID) 的记录
	// 为空时走 v1.x 行为 (Limit only, no offset)
	CursorTS time.Time
	CursorID uuid.UUID
	// 新增：offset 分页（Page > 0 时走 offset 路径；PageSize 每页条数，默认 20 上限 500，对齐 asset/ticket）
	Page     int
	PageSize int
	// P2: 是否需要 stats 全表聚合 (默认 false, 减少分页路径多 1 次 DB roundtrip)
	// HTTP 列表页设 true; gRPC 已弃用 stats 字段不设
	IncludeStats bool
}

// AlertStats 告警统计聚合结果
type AlertStats struct {
	Total        int64 `json:"total"`
	Problem      int64 `json:"problem"`
	Acknowledged int64 `json:"acknowledged"`
	Resolved     int64 `json:"resolved"`
}

// SeverityStat 按严重级别分组
type SeverityStat struct {
	Severity     int    `json:"severity"`
	SeverityName string `json:"severity_name"`
	Count        int64  `json:"count"`
}

// HourlyStat 按小时分组
type HourlyStat struct {
	Hour  time.Time `json:"hour"`
	Count int64     `json:"count"`
}

// AlertService 告警业务接口
type AlertService interface {
	List(ctx context.Context, f AlertFilter) (items []models.Alert, stats AlertStats, total int64, err error)
	Get(ctx context.Context, id string) (*models.Alert, error)
	Acknowledge(ctx context.Context, id, userID string) error
	Resolve(ctx context.Context, id, userID string) error
	// C-P6 批量：单次 SQL 更新多记录，N 次 N+1 → 1 次
	BulkAcknowledge(ctx context.Context, ids []string, userID string) (affected int64, err error)
	BulkResolve(ctx context.Context, ids []string, userID string) (affected int64, err error)
	BulkDelete(ctx context.Context, ids []string) (affected int64, err error)
	Stats(ctx context.Context) (bySeverity []SeverityStat, byHour []HourlyStat, err error)
	ListRules(ctx context.Context) ([]models.AlertRule, error)
	CreateRule(ctx context.Context, rule *models.AlertRule) error
	UpdateRule(ctx context.Context, id string, updates map[string]interface{}) (*models.AlertRule, error)
	DeleteRule(ctx context.Context, id string) error
	// 小改进 #2：标记误报 + ML 训练集
	// isFP=true 标记为误报（写 marked_by/marked_at/note）；isFP=false 反标记
	MarkFalsePositive(ctx context.Context, id, userID, note string, isFP bool) (*models.Alert, error)
	// 列出所有被标记为误报的告警（给 ML 训练集导出用）
	ListFalsePositives(ctx context.Context, since *time.Time) ([]models.Alert, error)
	// M38-B: triggerid → rule_id 映射管理（运维 CRUD）
	//   ruleID = alert_rules.id；triggerid 唯一（迁移 000038 已建 PK）
	//   同 triggerid 多次 Create = 后写覆盖前写（应用层 UPSERT 语义，因为 PK 是 triggerid）
	ListMappings(ctx context.Context, ruleID string) ([]models.AlertRuleTriggerMap, error)
	CreateMapping(ctx context.Context, ruleID, triggerID string) (*models.AlertRuleTriggerMap, error)
	DeleteMapping(ctx context.Context, ruleID, triggerID string) error
}

type alertService struct {
	db  *gorm.DB
	bus eventbus.Bus // v2.0: 可选, nil 时跳过 Publish (兼容老测试)
}

// NewAlertService 创建 AlertService
func NewAlertService(db *gorm.DB) AlertService {
	return &alertService{db: db}
}

// WithBus 注入事件总线 (v2.0, main.go 启动时调用)
// bus=nil 时 service 不发事件 (单元测试不依赖 bus)
func (s *alertService) WithBus(bus eventbus.Bus) {
	s.bus = bus
}

// publish 内部辅助: bus 为 nil 时静默跳过
// P2: 失败 log warn (旧版 _ = err 静默吞掉)
func (s *alertService) publish(topic string, payload any) {
	if s.bus == nil {
		return
	}
	if err := s.bus.Publish(topic, payload); err != nil {
		slog.Warn("alertService: publish failed", slog.String("topic", topic), slog.String("err", err.Error()))
	}
}

func (s *alertService) List(ctx context.Context, f AlertFilter) ([]models.Alert, AlertStats, int64, error) {
	q := s.db.WithContext(ctx).Model(&models.Alert{})

	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	if f.Severity > 0 {
		q = q.Where("severity >= ?", f.Severity)
	}
	if f.HostID != "" {
		q = q.Where("host_id = ?", f.HostID)
	}
	if f.IsFalsePositive != nil {
		q = q.Where("is_false_positive = ?", *f.IsFalsePositive)
	}

	limit := f.Limit
	// 🐛 BUG#14: 原 "limit <= 0 || limit > 1000" 命中 0 时改 100，但 0 也是合法
	// 客户端"想要 0 条"时（探测/分页 size=0）会被悄悄改 100。明确：
	//   - 0 / 负数 → 100（默认）
	//   - > 1000 → 1000（上限）
	//   - 1..1000 → 原值
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}

	q = q.Order("created_at DESC, id DESC") // v2.0 cursor: 二元组排序

	var items []models.Alert
	var total int64

	switch {
	case !f.CursorTS.IsZero() && f.CursorID != uuid.Nil:
		// v2.0 cursor 路径（gRPC）：二元组 < 走联合索引, O(log N)；不跑 Count，total 留 0
		q = q.Where("(created_at, id) < (?, ?)", f.CursorTS, f.CursorID)
		if err := q.Limit(limit).Find(&items).Error; err != nil {
			return nil, AlertStats{}, 0, err
		}
	case f.Page > 0:
		// offset 路径（HTTP 列表页）：过滤后 Count + Offset/Limit，对齐 asset/ticket service
		pageSize := f.PageSize
		if pageSize < 1 {
			pageSize = 20
		}
		if pageSize > 500 {
			pageSize = 500
		}
		if err := q.Count(&total).Error; err != nil {
			return nil, AlertStats{}, 0, err
		}
		if err := q.Offset((f.Page - 1) * pageSize).Limit(pageSize).Find(&items).Error; err != nil {
			return nil, AlertStats{}, 0, err
		}
	default:
		// limit 上限路径（dashboard 最近告警 limit=5）
		if err := q.Limit(limit).Find(&items).Error; err != nil {
			return nil, AlertStats{}, 0, err
		}
	}

	// P2: statsInternal 仅在 IncludeStats=true 时调用 (gRPC 翻页场景可省 1 次 DB roundtrip)
	if !f.IncludeStats {
		return items, AlertStats{}, total, nil
	}
	stats, err := s.statsInternal(ctx)
	if err != nil {
		return nil, AlertStats{}, 0, err
	}
	return items, stats, total, nil
}

func (s *alertService) statsInternal(ctx context.Context) (AlertStats, error) {
	var stats AlertStats
	db := s.db.WithContext(ctx).Model(&models.Alert{})

	// C-P4: 单条条件聚合（替代 4 次全表 Count）
	// SUM(CASE WHEN ...) 是 PG/MySQL 通用写法，gorm 用 Raw + Scan
	type countRow struct {
		Total        int64
		Problem      int64
		Acknowledged int64
		Resolved     int64
	}
	var row countRow
	err := db.Raw(`
		SELECT
			COUNT(*) AS total,
			COUNT(*) FILTER (WHERE status = 'problem')       AS problem,
			COUNT(*) FILTER (WHERE status = 'acknowledged') AS acknowledged,
			COUNT(*) FILTER (WHERE status = 'resolved')     AS resolved
		FROM alerts
	`).Scan(&row).Error
	if err == nil {
		stats = AlertStats(row)
	} else {
		// SQLite / 不支持 FILTER 的 DB 退化用 SUM CASE
		err = db.Raw(`
			SELECT
				COUNT(*) AS total,
				SUM(CASE WHEN status = 'problem'       THEN 1 ELSE 0 END) AS problem,
				SUM(CASE WHEN status = 'acknowledged' THEN 1 ELSE 0 END) AS acknowledged,
				SUM(CASE WHEN status = 'resolved'     THEN 1 ELSE 0 END) AS resolved
			FROM alerts
		`).Scan(&row).Error
		if err != nil {
			return stats, err
		}
		stats = AlertStats(row)
	}
	return stats, nil
}

func (s *alertService) Get(ctx context.Context, id string) (*models.Alert, error) {
	var alert models.Alert
	if err := s.db.WithContext(ctx).First(&alert, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &alert, nil
}

// ==================== M19：告警状态迁移收口 ====================
//
// 合法迁移表**本来就存在**，写在前端 getAlertActions 里：只给 problem 出「确认」、
// 只给 problem/acknowledged 出「解决」，resolved 两个按钮都不给。也就是说
//
//	problem      --ack-->     acknowledged
//	problem      --resolve--> resolved
//	acknowledged --resolve--> resolved
//
// ——服务端此前一条都不校验，只按 id 更新。于是公开端点（openapi 有 PUT
// /alerts/{id}/ack）可以把**已解决**的告警「确认」回去，status 从 resolved 退回
// acknowledged，后果全是静默的：
//   - dashboard 的 ResolvedAlerts 是 COUNT(*) FILTER (WHERE status='resolved')，当场掉数；
//   - 按状态过滤时它重新落回「未处理」，值班会重复处理一条已经处理完的告警；
//   - writeNotificationTrigger 补发一条「已确认」的假通知。
//
// 前端的按钮判断挡不住：它看到的是 5s 轮询前的世界，两个值班同时处理同一条告警时，
// 第二个人的列表**必然**过期。可见性判断只配用来省一次请求，正确性必须由服务端兜底
// ——这是 D-3 已经确立的原则，这里是它的反面。
//
// 重复请求分两类，不能合并：
//   - 已是**目标状态**（ack 一个 acknowledged、resolve 一个 resolved）→ 幂等成功，
//     且**不重写时间戳**。重写会污染指标：ack_time 后移拉长 MTTD、resolve_time 后移
//     拉长 MTTR（dashboard_service.go 那两个 AVG 直接吃这两列）。
//   - 处于**更后的终态**（ack 一个 resolved）→ 拒绝。静默 no-op 会让调用方以为生效，
//     正是本仓反复出现的「静默」反模式。
//
// 并发：合法源状态写进 UPDATE 的 WHERE，而不是「读出来在 Go 里判断再写」。两个值班
// 同时点「确认」和「解决」，读-判-写会让后落地的那个把状态改回去（正是要修的缺陷）；
// 条件 UPDATE 让最终结果与到达顺序无关。
var (
	alertAckSources     = []string{"problem"}
	alertResolveSources = []string{"problem", "acknowledged"}
)

// alertTerminalState 判断状态是否已是该操作的目标（→ 幂等成功，不写库不通知）。
func alertTerminalState(cur, target string) bool { return cur == target }

// classifyAlertNoRows 解释「条件 UPDATE 影响了 0 行」。
//
// 返回 nil = 幂等成功（并发下别人已经推进到 target）；否则返回给调用方的错误。
// 只在这条冷路径上多查一次：happy path 不付这个代价。
func (s *alertService) classifyAlertNoRows(ctx context.Context, id, target string) error {
	var cur models.Alert
	if err := s.db.WithContext(ctx).Select("status").First(&cur, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		return err
	}
	if alertTerminalState(cur.Status, target) {
		return nil
	}
	// 不回显 cur.Status：它虽出自本库，但历史行可能带着任意字符串，
	// 而这条 message 会原样进 409 body。
	return fmt.Errorf("%w: 告警当前状态不允许该操作", ErrInvalidState)
}

func (s *alertService) Acknowledge(ctx context.Context, id, userID string) error {
	alert, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	// 幂等 / 拒绝的快速判定，省掉一次必然 0 行的 UPDATE
	if alertTerminalState(alert.Status, "acknowledged") {
		return nil
	}
	if alert.Status != "problem" {
		return fmt.Errorf("%w: 告警当前状态不允许该操作", ErrInvalidState)
	}
	res := s.db.WithContext(ctx).Model(&models.Alert{}).
		Where("id = ? AND status IN ?", id, alertAckSources).
		Updates(map[string]interface{}{
			"status":   "acknowledged",
			"ack_time": time.Now(),
			"ack_user": userID,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		// 读与写之间被别人改了
		return s.classifyAlertNoRows(ctx, id, "acknowledged")
	}
	// v1.1: 状态变更触发通知 trigger — 落 notification_logs (pending)
	// 实际发送 (dingtalk/email) 由 v1.2 异步 worker 消费
	return s.writeNotificationTrigger(ctx, alert.ID, "acknowledged", userID)
}

func (s *alertService) Resolve(ctx context.Context, id, userID string) error {
	alert, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	// 已是终态 → 幂等成功。重复解决会把 resolve_time 改成 now，MTTR 随之虚高
	if alertTerminalState(alert.Status, "resolved") {
		return nil
	}
	if alert.Status != "problem" && alert.Status != "acknowledged" {
		return fmt.Errorf("%w: 告警当前状态不允许该操作", ErrInvalidState)
	}
	now := time.Now()
	var duration int
	if !alert.ProblemStart.IsZero() {
		duration = int(now.Sub(alert.ProblemStart).Seconds())
	}
	res := s.db.WithContext(ctx).Model(&models.Alert{}).
		Where("id = ? AND status IN ?", id, alertResolveSources).
		Updates(map[string]interface{}{
			"status":       "resolved",
			"resolve_time": now,
			"resolve_user": userID,
			"problem_end":  now,
			"duration":     duration,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return s.classifyAlertNoRows(ctx, id, "resolved")
	}
	// v2.0: 发 alert.resolved 事件给 event bus (通知 worker subscriber)
	// M37-A：snapshot 携带 RuleID + NotifyChannels 给 worker，避免 worker 二次 DB 读
	//   - alert.AlertRuleID nil（历史 alert）→ 传 nil NotifyChannelIDs → worker fallback 全启用（兼容）
	//   - 加载 rule 失败 → log + 同样传 nil（不漏告警）
	//   - NotifyChannels 解析失败 → 同样传 nil（worker fallback 全启用）
	//   - 解析成功但为空数组 → 传 []string{}（worker 推 0 次，运维明确清空语义）
	payload := notification.AlertEventPayload{
		AlertID:   alert.ID.String(),
		HostName:  alert.HostName,
		Severity:  alert.Severity,
		Trigger:   alert.TriggerName,
		Status:    "resolved",
		EventType: "resolved",
	}
	if alert.AlertRuleID != nil {
		payload.RuleID = alert.AlertRuleID.String()
		payload.NotifyChannelIDs = s.loadRuleNotifyChannelIDs(ctx, *alert.AlertRuleID)
	}
	s.publish(eventbus.TopicAlertResolved, payload)
	return s.writeNotificationTrigger(ctx, alert.ID, "resolved", userID)
}

// BulkAcknowledge C-P6: 批量确认告警（单条 SQL）。
// affected = 实际改的行数（不含 ID 不存在的）。
// v1.1: 包在事务里；Updates 单 SQL 本身原子，事务主要是为审计/notification trigger 留扩展点
// (v1.1 batch 3 会加 notify trigger，会需要和 DB 写入同事务)。
func (s *alertService) BulkAcknowledge(ctx context.Context, ids []string, userID string) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	if len(ids) > 1000 {
		return 0, ErrTooManyItems
	}
	now := time.Now()
	var affected int64
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&models.Alert{}).
			// M19: 源状态守卫 —— 已经 resolved 的不能被批量「确认」回去（单条路径的同一个洞）。
			// 批量**不**因为个别 id 状态不合法而整批失败：运维勾了 20 条、其中 3 条已被同事
			// 处理完，拒掉整批是最糟的选择。让它们自然落空，affected 如实报数即可。
			Where("id IN ? AND status IN ?", ids, alertAckSources).
			Updates(map[string]interface{}{
				"status":   "acknowledged",
				"ack_time": now,
				"ack_user": userID,
			})
		if res.Error != nil {
			return res.Error
		}
		affected = res.RowsAffected
		return nil
	})
	return affected, err
}

// BulkResolve C-P6: 批量解决告警（单条 SQL）。
// 注意：duration 字段需要逐条计算 problem_start 时间差，SQL 无法一行算；
// 这里走两步：1) 用子查询把 duration 算出来 UPDATE 2) 再批量改 status。
// 为简化与一致性，直接在 app 层遍历计算（最多 N 行，N 通常 < 1000，可接受）。
// v1.1: 包在事务里 — select 拿 alerts 与 update 改 status 必须原子，否则高并发下
// status 改了但 duration 还是旧值。
func (s *alertService) BulkResolve(ctx context.Context, ids []string, userID string) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	if len(ids) > 1000 {
		return 0, ErrTooManyItems
	}
	now := time.Now()
	var affected int64
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 一次 select 拿所有 alert（避免后续 N+1）。
		// M19: 条件与下面的 UPDATE 保持一致 —— 「算进来的行」必须就是「会写的行」，
		// 否则这个 select 会给出一个偏大的印象（同 M17 的「校验的键 == 落库的键」）。
		var alerts []models.Alert
		if err := tx.Where("id IN ? AND status IN ?", ids, alertResolveSources).
			Find(&alerts).Error; err != nil {
			return err
		}
		if len(alerts) == 0 {
			return nil
		}
		// 单条 UPDATE 批量改 status + time（duration 走 0，准确性让位性能）
		res := tx.Model(&models.Alert{}).
			// M19: 已 resolved 的重复解决会把 resolve_time 推到 now → MTTR 虚高，挡在 WHERE 上
			Where("id IN ? AND status IN ?", ids, alertResolveSources).
			Updates(map[string]interface{}{
				"status":       "resolved",
				"resolve_time": now,
				"resolve_user": userID,
				"problem_end":  now,
			})
		if res.Error != nil {
			return res.Error
		}
		affected = res.RowsAffected
		return nil
	})
	return affected, err
}

// BulkDelete C-P6: 批量删除（单条 SQL）。
// 🐛 BUG#17: 加 1000 上限防止单次 IN(?) 把 SQL 撑爆（PG IN 上限 ~32k，
// 但生产曾出现 200k ids 拖垮 DB）。超限直接 ErrTooManyItems。
// v1.1: 包在事务里 — 与未来的 audit log 写入同事务。
func (s *alertService) BulkDelete(ctx context.Context, ids []string) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	if len(ids) > 1000 {
		return 0, ErrTooManyItems
	}
	var affected int64
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Where("id IN ?", ids).Delete(&models.Alert{})
		if res.Error != nil {
			return res.Error
		}
		affected = res.RowsAffected
		return nil
	})
	return affected, err
}

// writeNotificationTrigger v1.1 P2-B-3: 告警状态变更 → 落 notification_logs (pending).
// 实际发送 (dingtalk/email) 是 v1.2 异步 worker 的事，这里只做 trigger + 落库。
// 失败仅 log，不影响主流程 — 主调用方 (Acknowledge/Resolve) 已成功改 status。
func (s *alertService) writeNotificationTrigger(ctx context.Context, alertID uuid.UUID, newStatus, userID string) error {
	// 拿所有启用的 channel (去重 by ID)，给每个 channel 落一行 pending log
	var channels []models.NotificationChannel
	if err := s.db.WithContext(ctx).
		Where("is_enabled = ?", true).
		Find(&channels).Error; err != nil {
		// P2: 不致命 — slog.Warn 后继续 (旧版用 gin.DefaultErrorWriter 不一致)
		slog.Warn("notification trigger: query channels failed", slog.String("err", err.Error()))
		return nil
	}
	if len(channels) == 0 {
		return nil
	}
	now := time.Now()
	logs := make([]models.NotificationLog, 0, len(channels))
	content := fmt.Sprintf("Alert %s → %s by user %s", alertID, newStatus, userID)
	for _, ch := range channels {
		logs = append(logs, models.NotificationLog{
			AlertID:     alertID,
			ChannelID:   ch.ID,
			ChannelName: ch.Name,
			Content:     content,
			Status:      "pending",
			SentAt:      now,
		})
	}
	if err := s.db.WithContext(ctx).Create(&logs).Error; err != nil {
		slog.Warn("notification trigger: insert logs failed", slog.String("err", err.Error()))
		return nil
	}
	return nil
}

func (s *alertService) Stats(ctx context.Context) ([]SeverityStat, []HourlyStat, error) {
	var bySeverity []SeverityStat
	if err := s.db.WithContext(ctx).Model(&models.Alert{}).
		Select("severity, severity_name, COUNT(*) as count").
		Where("status = ?", "problem").
		Group("severity, severity_name").
		Scan(&bySeverity).Error; err != nil {
		return nil, nil, err
	}

	var byHour []HourlyStat
	if err := s.db.WithContext(ctx).Model(&models.Alert{}).
		Select("date_trunc('hour', created_at) as hour, COUNT(*) as count").
		Where("created_at > ?", time.Now().AddDate(0, 0, -1)).
		Group("hour").
		Order("hour").
		Scan(&byHour).Error; err != nil {
		return nil, nil, err
	}

	return bySeverity, byHour, nil
}

func (s *alertService) ListRules(ctx context.Context) ([]models.AlertRule, error) {
	var rules []models.AlertRule
	if err := s.db.WithContext(ctx).Where("is_enabled = ?", true).Order("priority ASC").Find(&rules).Error; err != nil {
		return nil, err
	}
	return rules, nil
}

func (s *alertService) CreateRule(ctx context.Context, rule *models.AlertRule) error {
	if rule == nil {
		return ErrInvalidInput
	}
	rule.ID = uuid.New()
	return s.db.WithContext(ctx).Create(rule).Error
}

func (s *alertService) UpdateRule(ctx context.Context, id string, updates map[string]interface{}) (*models.AlertRule, error) {
	// 🐛 BUG#15: 原版有两次 First（len==0 分支 + 主路径），重构为 1 次
	var rule models.AlertRule
	if err := s.db.WithContext(ctx).First(&rule, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if len(updates) > 0 {
		if err := s.db.WithContext(ctx).Model(&rule).Updates(updates).Error; err != nil {
			return nil, err
		}
	}
	return &rule, nil
}

func (s *alertService) DeleteRule(ctx context.Context, id string) error {
	return s.db.WithContext(ctx).Delete(&models.AlertRule{}, "id = ?", id).Error
}

// MarkFalsePositive 标记/反标记误报（小改进 #2）。
// isFP=true  → 写 is_false_positive=1, marked_by=userID, marked_at=now, note
// isFP=false → 清空 is_false_positive=0, marked_by=nil, marked_at=nil, note=nil
// 返回更新后的 alert（含 FP 元数据）便于前端立即反映。
func (s *alertService) MarkFalsePositive(ctx context.Context, id, userID, note string, isFP bool) (*models.Alert, error) {
	alert, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	updates := map[string]interface{}{
		"is_false_positive": isFP,
	}
	if isFP {
		now := time.Now()
		updates["marked_by"] = userID
		updates["marked_at"] = now
		updates["false_positive_note"] = note
	} else {
		updates["marked_by"] = nil
		updates["marked_at"] = nil
		updates["false_positive_note"] = nil
	}
	if err := s.db.WithContext(ctx).Model(alert).Updates(updates).Error; err != nil {
		return nil, err
	}
	// 重读拿最新值
	return s.Get(ctx, id)
}

// ListFalsePositives 列出所有被标记为误报的告警（ML 训练集导出用）。
// since 非 nil 时只返回 marked_at >= since 的记录（增量导出）。
func (s *alertService) ListFalsePositives(ctx context.Context, since *time.Time) ([]models.Alert, error) {
	q := s.db.WithContext(ctx).Model(&models.Alert{}).
		Where("is_false_positive = ?", true).
		Order("marked_at DESC")
	if since != nil {
		q = q.Where("marked_at >= ?", *since)
	}
	var items []models.Alert
	if err := q.Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

// ListMappings M38-B: 列出某 rule 下的所有 triggerid 映射
//
// ruleID == "" 时返回全局列表（运维总览页用）。
// 排序: created_at DESC — 让"刚加的"排在最前，与 last-write-wins 决策一致。
// 不分页：alert_rule_trigger_map 的体量预期 < 10k 行（一条规则映射 ≤ 几千 trigger），
// 全表 SELECT 在分钟内完成；超大规模再做 cursor 与 M34 tickets 同型。
func (s *alertService) ListMappings(ctx context.Context, ruleID string) ([]models.AlertRuleTriggerMap, error) {
	q := s.db.WithContext(ctx).Model(&models.AlertRuleTriggerMap{}).
		Order("created_at DESC")
	if ruleID != "" {
		if _, err := uuid.Parse(ruleID); err != nil {
			return nil, ErrInvalidInput
		}
		q = q.Where("rule_id = ?", ruleID)
	}
	var items []models.AlertRuleTriggerMap
	if err := q.Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

// CreateMapping M38-B: 创建 triggerid → rule_id 映射 (UPSERT 语义)
//
// 防御：
//   - ruleID / triggerID 必传
//   - rule 必须存在（避免触发 FK 23503）。这一查会带回 gorm.ErrRecordNotFound → 翻译成 ErrNotFound
//     推进 404 而非 500（handler 区分）。
//   - triggerid 已映射到别的 rule → 同 PK 触发 upsert rule_id 改值（last-write-wins）
//
// 返回新写入的行（包括 created_at = NOW()）。同一个 triggerid 多次 POST 视为"改主意"，
// 不抛 409，与「同 triggerid → 多 rule」的设计决策一致。
func (s *alertService) CreateMapping(ctx context.Context, ruleID, triggerID string) (*models.AlertRuleTriggerMap, error) {
	if ruleID == "" || triggerID == "" {
		return nil, ErrInvalidInput
	}
	ruleUUID, err := uuid.Parse(ruleID)
	if err != nil {
		return nil, ErrInvalidInput
	}
	// 校验 rule 存在 (避免 FK 23503 错误污染 500 层)
	var exists int64
	if err := s.db.WithContext(ctx).Model(&models.AlertRule{}).
		Where("id = ?", ruleUUID).Count(&exists).Error; err != nil {
		return nil, err
	}
	if exists == 0 {
		return nil, ErrNotFound
	}
	row := &models.AlertRuleTriggerMap{
		TriggerID: triggerID,
		RuleID:    ruleUUID,
		CreatedAt: time.Now(),
	}
	// 同 triggerid 已存在 → UPDATE rule_id & created_at；否则 INSERT。
	// Save 走 SELECT + INSERT/UPDATE，对单行足够；UPSERT 走 Clauses.OnConflict 更明确，
	// 但 Save 在并发下也安全（PK 冲突必 return ErrDuplicatedKey → 同样改写语义不可得）。
	// 这里走 Save：① 模型小写，② 服务层不需要返回 created_at 旧值。
	if err := s.db.WithContext(ctx).Save(row).Error; err != nil {
		// PG 的 PK 冲突由 Save 转成 ErrDuplicatedKey → 改走 UPDATE 路径。
		// Save 不区分 — 我们直接判字符串序列或重试即可。下面用 UpdateColumns 更稳妥：
		// 取一次是否存在；存在 → UpdateColumns，不存在 → Save 写新行。
		return s.upsertMapping(ctx, row)
	}
	// 重新读以拿到真 created_at（Save 在 INSERT 时可能用 NOW() 但回写依赖 DB）
	return s.getMapping(ctx, triggerID)
}

// upsertMapping 处理 Save 的 PK 冲突：先看 row 是否真存在，存在就 UPDATE，不存在就 INSERT。
// 与 CreateMapping 拆函数仅是为单测可独立 export-测；现在保留私有。
func (s *alertService) upsertMapping(ctx context.Context, row *models.AlertRuleTriggerMap) (*models.AlertRuleTriggerMap, error) {
	var existing models.AlertRuleTriggerMap
	err := s.db.WithContext(ctx).Where("triggerid = ?", row.TriggerID).First(&existing).Error
	if err == nil {
		// 已存在 → UPDATE rule_id + created_at（last-write-wins 翻新）
		if err := s.db.WithContext(ctx).Model(&existing).Updates(map[string]interface{}{
			"rule_id":    row.RuleID,
			"created_at": row.CreatedAt,
		}).Error; err != nil {
			return nil, err
		}
		return s.getMapping(ctx, row.TriggerID)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if err := s.db.WithContext(ctx).Create(row).Error; err != nil {
		return nil, err
	}
	return s.getMapping(ctx, row.TriggerID)
}

// getMapping 私有：拿到最新一行（含 created_at 真值）
func (s *alertService) getMapping(ctx context.Context, triggerID string) (*models.AlertRuleTriggerMap, error) {
	var row models.AlertRuleTriggerMap
	if err := s.db.WithContext(ctx).Where("triggerid = ?", triggerID).First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// DeleteMapping M38-B: 删除某 rule 下的某 triggerid 映射。
// ruleID == "" 时删除全局 mapping（运维 "不想再告警此 trigger" 用法）——
// 按 triggerid 唯一，ruleID 仅做"我只能删我名下"的隔离。
// 不存在 → 返 nil（idempotent；不是 404 — 客户端脚本跑幂等不应被拒）。
func (s *alertService) DeleteMapping(ctx context.Context, ruleID, triggerID string) error {
	if triggerID == "" {
		return ErrInvalidInput
	}
	q := s.db.WithContext(ctx).Where("triggerid = ?", triggerID)
	if ruleID != "" {
		ruleUUID, err := uuid.Parse(ruleID)
		if err != nil {
			return ErrInvalidInput
		}
		q = q.Where("rule_id = ?", ruleUUID)
	}
	if err := q.Delete(&models.AlertRuleTriggerMap{}).Error; err != nil {
		return err
	}
	return nil
}

// loadRuleNotifyChannelIDs M37-A：根据 AlertRule.ID 加载 rule，解析 NotifyChannels JSON 字段为 UUID 字符串数组
//
// 返回约定（与 worker handleAlertEvent 的契约一致）：
//   - rule 加载失败 / NotifyChannels 解析失败 → 返回 nil → worker 走 "推全启用 channels" fallback（不漏告警）
//   - NotifyChannels 字段为 NULL（运维未配）→ 返回 []string{} → worker 推 0 次（明确空语义）
//   - 解析成功但里面 UUID 在 DB 找不到对应 channel → worker 端会按 ID 过滤时自然清空，与设计一致
//
// NotifyChannels 字段语义：JSON 字符串数组（v3 需求 §3.2），元素是 notification_channels.id (UUID)。
// 早期版本可能存成其他格式；解析失败一律走 fallback，不抛错（运维可读警告日志后修配置）。
func (s *alertService) loadRuleNotifyChannelIDs(ctx context.Context, ruleID uuid.UUID) []string {
	var rule models.AlertRule
	if err := s.db.WithContext(ctx).
		Select("id", "notify_channels").
		First(&rule, "id = ?", ruleID).Error; err != nil {
		log.Printf("[alert_service] loadRuleNotifyChannelIDs rule %s: %v → fallback (worker 推全启用)", ruleID, err)
		return nil
	}
	if rule.NotifyChannels == "" {
		// 字段未写过 → 明确空语义（运维主动留空 = 不通知任何人）
		return []string{}
	}
	var ids []string
	if err := json.Unmarshal([]byte(rule.NotifyChannels), &ids); err != nil {
		log.Printf("[alert_service] loadRuleNotifyChannelIDs rule %s: notify_channels JSON parse err %v → fallback", ruleID, err)
		return nil
	}
	return ids
}
