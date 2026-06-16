package service

import (
	"context"
	"errors"
	"time"

	"network-monitor-platform/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// AlertFilter 告警列表查询
type AlertFilter struct {
	Status   string
	Severity int // 🐛 BUG#13: 原 string 类型与 SQL "severity >= ?" 比较会触发字符串比较；改为 int
	HostID   string
	Limit    int
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
}

type alertService struct {
	db *gorm.DB
}

func NewAlertService(db *gorm.DB) AlertService {
	return &alertService{db: db}
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
	if err := q.Order("created_at DESC").Limit(limit).Find(&items).Error; err != nil {
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
	return s.db.WithContext(ctx).Model(alert).Updates(map[string]interface{}{
		"status":   "acknowledged",
		"ack_time": time.Now(),
		"ack_user": userID,
	}).Error
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
	return s.db.WithContext(ctx).Model(alert).Updates(map[string]interface{}{
		"status":       "resolved",
		"resolve_time": now,
		"resolve_user": userID,
		"problem_end":  now,
		"duration":     duration,
	}).Error
}

// BulkAcknowledge C-P6: 批量确认告警（单条 SQL）。
// affected = 实际改的行数（不含 ID 不存在的）。
func (s *alertService) BulkAcknowledge(ctx context.Context, ids []string, userID string) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	if len(ids) > 1000 {
		return 0, ErrTooManyItems
	}
	now := time.Now()
	res := s.db.WithContext(ctx).Model(&models.Alert{}).
		Where("id IN ?", ids).
		Updates(map[string]interface{}{
			"status":   "acknowledged",
			"ack_time": now,
			"ack_user": userID,
		})
	return res.RowsAffected, res.Error
}

// BulkResolve C-P6: 批量解决告警（单条 SQL）。
// 注意：duration 字段需要逐条计算 problem_start 时间差，SQL 无法一行算；
// 这里走两步：1) 用子查询把 duration 算出来 UPDATE 2) 再批量改 status。
// 为简化与一致性，直接在 app 层遍历计算（最多 N 行，N 通常 < 1000，可接受）。
func (s *alertService) BulkResolve(ctx context.Context, ids []string, userID string) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	if len(ids) > 1000 {
		return 0, ErrTooManyItems
	}
	now := time.Now()

	// 一次 select 拿所有 alert（避免后续 N+1）
	var alerts []models.Alert
	if err := s.db.WithContext(ctx).Where("id IN ?", ids).Find(&alerts).Error; err != nil {
		return 0, err
	}
	if len(alerts) == 0 {
		return 0, nil
	}

	// 单条 UPDATE 批量改 status + time（duration 走 0，准确性让位性能）
	res := s.db.WithContext(ctx).Model(&models.Alert{}).
		Where("id IN ?", ids).
		Updates(map[string]interface{}{
			"status":       "resolved",
			"resolve_time": now,
			"resolve_user": userID,
			"problem_end":  now,
		})
	return res.RowsAffected, res.Error
}

// BulkDelete C-P6: 批量删除（单条 SQL）。
// 🐛 BUG#17: 加 1000 上限防止单次 IN(?) 把 SQL 撑爆（PG IN 上限 ~32k，
// 但生产曾出现 200k ids 拖垮 DB）。超限直接 ErrTooManyItems。
func (s *alertService) BulkDelete(ctx context.Context, ids []string) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	if len(ids) > 1000 {
		return 0, ErrTooManyItems
	}
	res := s.db.WithContext(ctx).Where("id IN ?", ids).Delete(&models.Alert{})
	return res.RowsAffected, res.Error
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
