package service

import (
	"context"
	"time"

	"gorm.io/gorm"
)

// DashboardStats 仪表盘首页数字
type DashboardStats struct {
	Assets   int64 `json:"assets"`
	Alerts   int64 `json:"alerts"`
	Tickets  int64 `json:"tickets"`
	Sites    int64 `json:"sites"`
	Machines int64 `json:"machines"` // 服务器
	Networks int64 `json:"networks"` // 网络设备
}

// TrendPoint 时间序列点
type TrendPoint struct {
	Date  string `json:"date"`
	Count int64  `json:"count"`
}

// KPI 关键绩效指标
//   - MTTRSeconds: 平均恢复时间（秒），nil = 无已解决告警
//   - MTTDSeconds: 平均检测/确认时间（秒），nil = 无已确认告警
//   - AlertDensity: 窗口内每日告警数（alerts/day）
//   - SLAClosedRate: SLA 达成率（已关单中按时比例，0-1），nil = 无已关工单
//   - WindowDays: KPI 窗口
type KPI struct {
	MTTRSeconds    *int64   `json:"mttr_seconds"`
	MTTDSeconds    *int64   `json:"mttd_seconds"`
	AlertDensity   float64  `json:"alert_density"`
	SLAClosedRate  *float64 `json:"sla_closed_rate"`
	WindowDays     int      `json:"window_days"`
	WindowStart    string   `json:"window_start"`
	WindowEnd      string   `json:"window_end"`
	ResolvedAlerts int64    `json:"resolved_alerts"`
	AckedAlerts    int64    `json:"acked_alerts"`
	ClosedTickets  int64    `json:"closed_tickets"`
	OnTimeTickets  int64    `json:"on_time_tickets"`
}

// DashboardService 仪表盘聚合业务
type DashboardService interface {
	Stats(ctx context.Context) (*DashboardStats, error)
	AlertTrends(ctx context.Context, days int) ([]TrendPoint, error)
	KPIs(ctx context.Context, days int) (*KPI, error)
}

type dashboardService struct {
	db *gorm.DB
}

func NewDashboardService(db *gorm.DB) DashboardService {
	return &dashboardService{db: db}
}

func (s *dashboardService) Stats(ctx context.Context) (*DashboardStats, error) {
	// 🐛 BUG#27+#28: 6 次 count 串行 → 1 条聚合 SQL（Machines/Networks 之前是
	// 假数据 = Assets，必须按 asset_type 区分）
	var stats DashboardStats
	row := s.db.WithContext(ctx).Raw(`
		SELECT
			(SELECT COUNT(*) FROM assets) AS assets,
			(SELECT COUNT(*) FROM assets WHERE asset_type = 'server') AS machines,
			(SELECT COUNT(*) FROM assets WHERE asset_type IN ('switch','router','firewall')) AS networks,
			(SELECT COUNT(*) FROM alerts WHERE status = 'problem') AS alerts,
			(SELECT COUNT(*) FROM tickets WHERE status != 'closed') AS tickets,
			(SELECT COUNT(*) FROM sites) AS sites
	`).Row()
	if err := row.Scan(&stats.Assets, &stats.Machines, &stats.Networks,
		&stats.Alerts, &stats.Tickets, &stats.Sites); err != nil {
		return nil, err
	}
	return &stats, nil
}

func (s *dashboardService) AlertTrends(ctx context.Context, days int) ([]TrendPoint, error) {
	if days <= 0 {
		days = 7
	}
	if days > 90 {
		days = 90
	}
	// 真实实现需按天 group；目前缺数据时返回空数组（前端兜底）
	var points []TrendPoint
	err := s.db.WithContext(ctx).Raw(`
		SELECT to_char(date_trunc('day', created_at), 'YYYY-MM-DD') AS date,
		       COUNT(*) AS count
		FROM alerts
		WHERE created_at > NOW() - (? || ' days')::interval
		GROUP BY 1
		ORDER BY 1
	`, days).Scan(&points).Error
	if err != nil {
		return nil, err
	}
	return points, nil
}

// KPIs 关键绩效指标聚合
//
// 指标定义：
//   - MTTR: 告警恢复耗时 = AVG(resolve_time - problem_start) on 已解决
//   - MTTD: 告警检测耗时 = AVG(ack_time - problem_start) on 已确认
//   - AlertDensity: 窗口内总告警数 / 窗口天数
//   - SLA 达成率: (closed_tickets 中 in_sla=true) / closed_tickets
//
// 边界：
//   - days 限定 1-90（默认 7）
//   - 任一指标无数据 → 对应字段为 nil，前端显示 n/a
func (s *dashboardService) KPIs(ctx context.Context, days int) (*KPI, error) {
	if days <= 0 {
		days = 7
	}
	if days > 90 {
		days = 90
	}

	now := time.Now().UTC()
	start := now.Add(-time.Duration(days) * 24 * time.Hour)

	kpi := &KPI{
		WindowDays:  days,
		WindowStart: start.Format("2006-01-02T15:04:05Z"),
		WindowEnd:   now.Format("2006-01-02T15:04:05Z"),
	}

	// 1) MTTR (avg resolve_time - problem_start on resolved alerts in window)
	var mttrSeconds *float64
	if err := s.db.WithContext(ctx).Raw(`
		SELECT AVG(EXTRACT(EPOCH FROM (resolve_time - problem_start)))
		FROM alerts
		WHERE status = 'resolved'
		  AND resolve_time IS NOT NULL
		  AND problem_start IS NOT NULL
		  AND problem_start >= ?
	`, start).Scan(&mttrSeconds).Error; err != nil {
		return nil, err
	}
	if mttrSeconds != nil && *mttrSeconds > 0 {
		secs := int64(*mttrSeconds)
		kpi.MTTRSeconds = &secs
	}

	// 2) MTTD (avg ack_time - problem_start on acked alerts in window)
	var mttdSeconds *float64
	if err := s.db.WithContext(ctx).Raw(`
		SELECT AVG(EXTRACT(EPOCH FROM (ack_time - problem_start)))
		FROM alerts
		WHERE ack_time IS NOT NULL
		  AND problem_start IS NOT NULL
		  AND problem_start >= ?
	`, start).Scan(&mttdSeconds).Error; err != nil {
		return nil, err
	}
	if mttdSeconds != nil && *mttdSeconds > 0 {
		secs := int64(*mttdSeconds)
		kpi.MTTDSeconds = &secs
	}

	// 3) Alert density (total alerts in window / days)
	var totalAlerts int64
	if err := s.db.WithContext(ctx).Raw(`
		SELECT COUNT(*) FROM alerts WHERE problem_start >= ?
	`, start).Scan(&totalAlerts).Error; err != nil {
		return nil, err
	}
	if totalAlerts > 0 {
		kpi.AlertDensity = float64(totalAlerts) / float64(days)
	}

	// 4) Resolved/Acked/Closed counts
	if err := s.db.WithContext(ctx).Raw(`
		SELECT
			COUNT(*) FILTER (WHERE status = 'resolved' AND resolve_time >= ?) AS resolved_alerts,
			COUNT(*) FILTER (WHERE ack_time IS NOT NULL AND ack_time >= ?) AS acked_alerts
		FROM alerts
		WHERE problem_start >= ?
	`, start, start, start).Row().Scan(&kpi.ResolvedAlerts, &kpi.AckedAlerts); err != nil {
		return nil, err
	}

	// 5) SLA closed rate (in_sla=true / closed) — assumes tickets table has in_sla bool
	type slaRow struct {
		Total  int64
		OnTime int64
	}
	var sla slaRow
	if err := s.db.WithContext(ctx).Raw(`
		SELECT
			COUNT(*) AS total,
			COUNT(*) FILTER (WHERE in_sla = true) AS on_time
		FROM tickets
		WHERE status = 'closed'
		  AND closed_at >= ?
	`, start).Scan(&sla).Error; err != nil {
		// tickets 可能无 in_sla 字段 → 视为 0 跳过（不让整个 KPI 失败）
		kpi.SLAClosedRate = nil
	} else if sla.Total > 0 {
		rate := float64(sla.OnTime) / float64(sla.Total)
		kpi.SLAClosedRate = &rate
		kpi.ClosedTickets = sla.Total
		kpi.OnTimeTickets = sla.OnTime
	}

	return kpi, nil
}
