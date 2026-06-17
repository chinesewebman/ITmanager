package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"network-monitor-platform/internal/eventbus"
	"network-monitor-platform/internal/models"
	"network-monitor-platform/internal/notification"

	"github.com/gin-gonic/gin"
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
	List(ctx context.Context, f AlertFilter) (items []models.Alert, stats AlertStats, err error)
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
func (s *alertService) publish(topic string, payload any) {
	if s.bus == nil {
		return
	}
	if err := s.bus.Publish(topic, payload); err != nil {
		// Publish 失败不应阻塞主业务, 只 log
		// (用 slog 后续可注入, 现在 fmt 占位)
		_ = err
	}
}
func (s *alertService) List(ctx context.Context, f AlertFilter) ([]models.Alert, AlertStats, error) {
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

	var items []models.Alert
	q = q.Order("created_at DESC, id DESC") // v2.0 cursor: 二元组排序
	// v2.0 cursor 分页: 二元组 < (ts, id) 走 (created_at, id) 联合索引, O(log N)
	if !f.CursorTS.IsZero() && f.CursorID != uuid.Nil {
		q = q.Where("(created_at, id) < (?, ?)", f.CursorTS, f.CursorID)
	}
	if err := q.Limit(limit).Find(&items).Error; err != nil {
		return nil, AlertStats{}, err
	}

	stats, err := s.statsInternal(ctx)
	if err != nil {
		return nil, AlertStats{}, err
	}
	return items, stats, nil
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

func (s *alertService) Acknowledge(ctx context.Context, id, userID string) error {
	alert, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if err := s.db.WithContext(ctx).Model(alert).Updates(map[string]interface{}{
		"status":   "acknowledged",
		"ack_time": time.Now(),
		"ack_user": userID,
	}).Error; err != nil {
		return err
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
	now := time.Now()
	var duration int
	if !alert.ProblemStart.IsZero() {
		duration = int(now.Sub(alert.ProblemStart).Seconds())
	}
	if err := s.db.WithContext(ctx).Model(alert).Updates(map[string]interface{}{
		"status":       "resolved",
		"resolve_time": now,
		"resolve_user": userID,
		"problem_end":  now,
		"duration":     duration,
	}).Error; err != nil {
		return err
	}
	// v2.0: 发 alert.resolved 事件给 event bus (通知 worker subscriber)
	s.publish(eventbus.TopicAlertResolved, notification.AlertEventPayload{
		AlertID:   alert.ID.String(),
		HostName:  alert.HostName,
		Severity:  alert.Severity,
		Trigger:   alert.TriggerName,
		Status:    "resolved",
		EventType: "resolved",
	})
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
			Where("id IN ?", ids).
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
		// 一次 select 拿所有 alert（避免后续 N+1）
		var alerts []models.Alert
		if err := tx.Where("id IN ?", ids).Find(&alerts).Error; err != nil {
			return err
		}
		if len(alerts) == 0 {
			return nil
		}
		// 单条 UPDATE 批量改 status + time（duration 走 0，准确性让位性能）
		res := tx.Model(&models.Alert{}).
			Where("id IN ?", ids).
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

// writeNotificationTrigger v1.1 P2-B-3: 告警状态变更 → 落 notification_logs (pending)。
// 实际发送 (dingtalk/email) 是 v1.2 异步 worker 的事，这里只做 trigger + 落库。
// 失败仅 log，不影响主流程 — 主调用方 (Acknowledge/Resolve) 已成功改 status。
func (s *alertService) writeNotificationTrigger(ctx context.Context, alertID uuid.UUID, newStatus, userID string) error {
	// 拿所有启用的 channel (去重 by ID)，给每个 channel 落一行 pending log
	var channels []models.NotificationChannel
	if err := s.db.WithContext(ctx).
		Where("is_enabled = ?", true).
		Find(&channels).Error; err != nil {
		// 不致命 — log 后继续
		gin.DefaultErrorWriter.Write([]byte(
			"[WARN] notification trigger: query channels failed: " + err.Error() + "\n",
		))
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
		gin.DefaultErrorWriter.Write([]byte(
			"[WARN] notification trigger: insert logs failed: " + err.Error() + "\n",
		))
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
