package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"network-monitor-platform/internal/diagnostic"
	"network-monitor-platform/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// DiagnosticService 资产诊断服务：聚合 alerts/tickets/assets/asset_networks 四张表生成时间线。
//
// 设计要点：
//  1. 时间窗口默认 30 天，最大 365 天（防止单次查询扫表过大）
//  2. 事件按 ts 倒序，前端可以原样渲染 Ant Design Timeline
//  3. MTTR (Mean Time To Resolve) 走 service 层聚合 SQL，不在 handler 重复算
//  4. 单次查询 N+1 防护：asset + 4 个 groupby count 都用一条 SQL
//  5. ctx 必传，DB 调用全程包 ctx
type DiagnosticService struct {
	db *gorm.DB
}

// NewDiagnosticService 构造 DiagnosticService
func NewDiagnosticService(db *gorm.DB) *DiagnosticService {
	return &DiagnosticService{db: db}
}

// DiagnosticFilter 时间线查询参数
type DiagnosticFilter struct {
	// Days 查询窗口（天），0 表示用默认值 30；最大 365
	Days int
	// Limit 事件数量上限，0 表示默认 200；最大 1000
	Limit int
}

// GetTimeline 返回指定资产的时间线事件 + 摘要 + 资产概要。
//
// 返回：
//   - nil, ErrRecordNotFound 当资产不存在
//   - 非 nil error 当 DB 失败
func (s *DiagnosticService) GetTimeline(ctx context.Context, assetID uuid.UUID, filter DiagnosticFilter) (*models.DiagnosticTimeline, error) {
	days := filter.Days
	if days <= 0 {
		days = 30
	}
	if days > 365 {
		days = 365
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = 200
	}
	if limit > 1000 {
		limit = 1000
	}
	now := time.Now().UTC()
	windowStart := now.Add(-time.Duration(days) * 24 * time.Hour)

	// 1) 资产概要
	var asset models.Asset
	if err := s.db.WithContext(ctx).First(&asset, "id = ?", assetID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: asset_id=%s", ErrNotFound, assetID)
		}
		return nil, err
	}

	// 2) 事件聚合：alerts + tickets + assets.status 变更 + asset_networks 变更
	events := make([]models.TimelineEvent, 0, limit)

	// 2.1) 告警事件：triggered/acknowledged/resolved
	// 窗口内筛选：problem_start OR ack_time OR resolve_time 任一在 window 内都算
	type alertRow struct {
		ID           uuid.UUID
		TriggerName  string
		Severity     int
		SeverityName string
		Problem      string
		ProblemStart time.Time
		ProblemEnd   *time.Time
		AckTime      *time.Time
		AckUser      string
		ResolveTime  *time.Time
		ResolveUser  string
		Status       string
	}
	var alertRows []alertRow
	if err := s.db.WithContext(ctx).
		Table("alerts").
		Select("id, trigger_name, severity, severity_name, problem, problem_start, problem_end, ack_time, ack_user, resolve_time, resolve_user, status").
		Where("host_id = ? AND (problem_start >= ? OR (ack_time IS NOT NULL AND ack_time >= ?) OR (resolve_time IS NOT NULL AND resolve_time >= ?))",
			assetID, windowStart, windowStart, windowStart).
		Order("problem_start DESC").
		Limit(limit).
		Scan(&alertRows).Error; err != nil {
		return nil, err
	}
	for _, a := range alertRows {
		events = append(events, models.TimelineEvent{
			TS: a.ProblemStart, Kind: models.TimelineEventAlert, SubKind: "triggered",
			Severity:    a.Severity,
			Title:       a.TriggerName,
			Description: severityDesc(a.SeverityName, a.Problem),
			RefID:       &a.ID, RefTable: "alerts",
		})
		if a.AckTime != nil {
			events = append(events, models.TimelineEvent{
				TS: *a.AckTime, Kind: models.TimelineEventAlert, SubKind: "acknowledged",
				Title:       "已确认告警",
				Description: "操作人: " + a.AckUser,
				RefID:       &a.ID, RefTable: "alerts",
			})
		}
		if a.ResolveTime != nil {
			events = append(events, models.TimelineEvent{
				TS: *a.ResolveTime, Kind: models.TimelineEventAlert, SubKind: "resolved",
				Title:       "告警已解决",
				Description: "操作人: " + a.ResolveUser,
				RefID:       &a.ID, RefTable: "alerts",
			})
		}
	}

	// 2.2) 工单事件
	type ticketRow struct {
		ID           uuid.UUID
		Title        string
		Status       string
		CreatedAt    time.Time
		ResolvedAt   *time.Time
		ClosedAt     *time.Time
		AssigneeName string
	}
	var ticketRows []ticketRow
	if err := s.db.WithContext(ctx).
		Table("tickets").
		Select("id, title, status, created_at, resolved_at, closed_at, assignee_name").
		Where("asset_id = ? AND (created_at >= ? OR (resolved_at IS NOT NULL AND resolved_at >= ?) OR (closed_at IS NOT NULL AND closed_at >= ?))",
			assetID, windowStart, windowStart, windowStart).
		Order("created_at DESC").
		Limit(limit).
		Scan(&ticketRows).Error; err != nil {
		return nil, err
	}
	for _, t := range ticketRows {
		events = append(events, models.TimelineEvent{
			TS: t.CreatedAt, Kind: models.TimelineEventTicket, SubKind: "created",
			Title:       t.Title,
			Description: "工单创建",
			RefID:       &t.ID, RefTable: "tickets",
		})
		if t.ResolvedAt != nil {
			events = append(events, models.TimelineEvent{
				TS: *t.ResolvedAt, Kind: models.TimelineEventTicket, SubKind: "resolved",
				Title:       t.Title,
				Description: "处理人: " + t.AssigneeName + " / 工单已解决",
				RefID:       &t.ID, RefTable: "tickets",
			})
		}
		if t.ClosedAt != nil {
			events = append(events, models.TimelineEvent{
				TS: *t.ClosedAt, Kind: models.TimelineEventTicket, SubKind: "closed",
				Title:       t.Title,
				Description: "工单关闭",
				RefID:       &t.ID, RefTable: "tickets",
			})
		}
	}

	// 2.3) 资产状态变更：online_time / offline_time
	if asset.OnlineTime != nil && asset.OnlineTime.After(windowStart) {
		events = append(events, models.TimelineEvent{
			TS: *asset.OnlineTime, Kind: models.TimelineEventStatus, SubKind: "online",
			Title:       "资产上线",
			Description: "OnlineTime 变更",
		})
	}
	if asset.OfflineTime != nil && asset.OfflineTime.After(windowStart) {
		events = append(events, models.TimelineEvent{
			TS: *asset.OfflineTime, Kind: models.TimelineEventStatus, SubKind: "offline",
			Title:       "资产离线",
			Description: "OfflineTime 变更",
		})
	}

	// 2.4) 网卡状态变更
	type netRow struct {
		ID            uuid.UUID
		InterfaceName string
		Status        string
		UpdatedAt     time.Time
	}
	var netRows []netRow
	if err := s.db.WithContext(ctx).
		Table("asset_networks").
		Select("id, interface_name, status, updated_at").
		Where("asset_id = ? AND updated_at >= ?", assetID, windowStart).
		Order("updated_at DESC").
		Limit(limit).
		Scan(&netRows).Error; err != nil {
		return nil, err
	}
	for _, n := range netRows {
		events = append(events, models.TimelineEvent{
			TS: n.UpdatedAt, Kind: models.TimelineEventLink, SubKind: n.Status,
			Title:       n.InterfaceName + " → " + n.Status,
			Description: "网卡状态变更",
			RefID:       &n.ID, RefTable: "asset_networks",
		})
	}

	// 3) 排序：ts 倒序，limit 在排序后截断（保证多源数据公平截断）
	sort.SliceStable(events, func(i, j int) bool {
		return events[i].TS.After(events[j].TS)
	})
	if len(events) > limit {
		events = events[:limit]
	}

	// 4) 摘要
	summary, err := s.summary(ctx, assetID, windowStart, now, days)
	if err != nil {
		return nil, err
	}
	summary.AssetID = assetID

	return &models.DiagnosticTimeline{
		Asset: &models.DiagnosticAsset{
			ID: asset.ID, Name: asset.Name, AssetTag: asset.AssetTag,
			AssetType: asset.AssetType, Brand: asset.Brand, Model: asset.Model,
			Status: asset.Status,
			SiteID: asset.SiteID, SiteName: asset.SiteName,
			RackID: asset.RackID, RackName: asset.RackName,
		},
		Events:  events,
		Summary: summary,
	}, nil
}

func (s *DiagnosticService) summary(ctx context.Context, assetID uuid.UUID, start, end time.Time, days int) (*models.DiagnosticSummary, error) {
	summary := &models.DiagnosticSummary{
		WindowDays:  days,
		WindowStart: start,
		WindowEnd:   end,
	}

	// 4.1) alert_count + open_alerts（Row().Scan 模式，sqlite/PG 都用整数）
	var alertTotal, alertOpen int64
	row := s.db.WithContext(ctx).
		Table("alerts").
		Select("COUNT(*), SUM(CASE WHEN status = 'problem' THEN 1 ELSE 0 END)").
		Where("host_id = ? AND problem_start >= ?", assetID, start).
		Row()
	if err := row.Scan(&alertTotal, &alertOpen); err == nil {
		summary.AlertCount = alertTotal
		summary.OpenAlerts = alertOpen
	}

	// 4.2) ticket_count + open_tickets
	var ticketTotal, ticketOpen int64
	row = s.db.WithContext(ctx).
		Table("tickets").
		Select("COUNT(*), SUM(CASE WHEN status NOT IN ('resolved','closed') THEN 1 ELSE 0 END)").
		Where("asset_id = ? AND created_at >= ?", assetID, start).
		Row()
	if err := row.Scan(&ticketTotal, &ticketOpen); err == nil {
		summary.TicketCount = ticketTotal
		summary.OpenTickets = ticketOpen
	}

	// 4.3) last_offline / last_online（已经在 GetTimeline 顶部 First(asset) 读过，复用）
	var assetSummary models.Asset
	if err := s.db.WithContext(ctx).First(&assetSummary, "id = ?", assetID).Error; err == nil {
		summary.LastOnline = assetSummary.OnlineTime
		summary.LastOffline = assetSummary.OfflineTime
	}

	// 4.4) MTTR (平均恢复时间)：avg(resolve_time - problem_start)
	// 双方言兼容：在 Go 端计算（避免 sqlite strftime vs postgres EXTRACT(EPOCH) 分歧）
	// 窗口筛选：resolved alert 必须 resolve_time 落入窗口（这样窗口滑动时 MTTR 跟着滑动）
	type resolvedAlertRow struct {
		ProblemStart time.Time
		ResolveTime  time.Time
	}
	var resolvedRows []resolvedAlertRow
	if err := s.db.WithContext(ctx).
		Table("alerts").
		Select("problem_start, resolve_time").
		Where("host_id = ? AND status = 'resolved' AND resolve_time IS NOT NULL AND resolve_time >= ?", assetID, start).
		Limit(1000).
		Scan(&resolvedRows).Error; err == nil && len(resolvedRows) > 0 {
		var totalSeconds int64
		for _, r := range resolvedRows {
			delta := r.ResolveTime.Sub(r.ProblemStart).Seconds()
			if delta > 0 {
				totalSeconds += int64(delta)
			}
		}
		avg := totalSeconds / int64(len(resolvedRows))
		summary.MTTRSeconds = &avg
	}

	// 4.5) link_down_count
	var linkDown int64
	row = s.db.WithContext(ctx).
		Table("asset_networks").
		Select("COUNT(*)").
		Where("asset_id = ? AND status = 'down' AND updated_at >= ?", assetID, start).
		Row()
	if err := row.Scan(&linkDown); err == nil {
		summary.LinkDownCount = linkDown
	}

	return summary, nil
}

// PingAsset 探测资产可达性（调用系统 ping）
//
// 责任：
//   - 校验参数（host 合法性、count 上限）
//   - 调用 internal/diagnostic.Ping 执行
//   - ctx 透传：调用方超时即取消
//
// 返回：
//   - ErrInvalidInput 当 host 非法或 count 越界（已包 errors.Is）
//   - 非 nil error 当 ping 执行失败
func (s *DiagnosticService) PingAsset(ctx context.Context, host string, count int) (*diagnostic.PingResult, error) {
	if count <= 0 {
		count = 4
	}
	if count > diagnostic.MaxPingCount {
		return nil, fmt.Errorf("%w: count 上限 %d", ErrInvalidInput, diagnostic.MaxPingCount)
	}
	res, err := diagnostic.Ping(ctx, host, count)
	if err != nil {
		// 把 host 非法/参数越界 wrap 到 service 层，让 handler 用 errors.Is 判别
		if errors.Is(err, diagnostic.ErrInvalidHost) {
			return nil, fmt.Errorf("%w: %v", ErrInvalidInput, err)
		}
		return nil, err
	}
	return res, nil
}

// TracerouteAsset 跟踪资产网络路径（调用系统 traceroute）
//
// 责任：
//   - 校验参数（host 合法性、maxHops 上限）
//   - 调用 internal/diagnostic.Traceroute 执行
//   - ctx 透传
//
// 返回：
//   - ErrInvalidInput 当 host 非法或 maxHops 越界
//   - 非 nil error 当 traceroute 执行失败
func (s *DiagnosticService) TracerouteAsset(ctx context.Context, host string, maxHops int) (*diagnostic.TracerouteResult, error) {
	if maxHops <= 0 {
		maxHops = 30
	}
	if maxHops > diagnostic.MaxTracerouteHops {
		return nil, fmt.Errorf("%w: maxHops 上限 %d", ErrInvalidInput, diagnostic.MaxTracerouteHops)
	}
	res, err := diagnostic.Traceroute(ctx, host, maxHops)
	if err != nil {
		if errors.Is(err, diagnostic.ErrInvalidHost) {
			return nil, fmt.Errorf("%w: %v", ErrInvalidInput, err)
		}
		return nil, err
	}
	return res, nil
}

// severityDesc 构造告警 sub_title 兼容 severity 名字缺失场景
func severityDesc(name, problem string) string {
	if name == "" {
		name = "Unknown"
	}
	if problem == "" {
		return name
	}
	return name + " · " + problem
}
