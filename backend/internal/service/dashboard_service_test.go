package service

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ==================== Dashboard Service 测试 ====================

func TestDashboardService_Stats_单条聚合SQL(t *testing.T) {
	// 🐛 BUG#27+#28: 6 次 count 串行 → 1 条聚合 SQL
	gormDB, mock := newMockDB(t)
	svc := NewDashboardService(gormDB)
	ctx := context.Background()

	// 单条 SQL 返 6 列
	mock.ExpectQuery(`SELECT`).
		WillReturnRows(sqlmock.NewRows([]string{
			"assets", "machines", "networks", "alerts", "tickets", "sites",
		}).AddRow(100, 60, 25, 5, 20, 3))

	stats, err := svc.Stats(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(100), stats.Assets)
	assert.Equal(t, int64(60), stats.Machines) // 不再是 assets 占位
	assert.Equal(t, int64(25), stats.Networks) // 不再是 assets 占位
	assert.Equal(t, int64(5), stats.Alerts)
	assert.Equal(t, int64(20), stats.Tickets)
	assert.Equal(t, int64(3), stats.Sites)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestDashboardService_AlertTrends_默认7天(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewDashboardService(gormDB)
	ctx := context.Background()

	// days=0 走 default 7
	rows := sqlmock.NewRows([]string{"date", "count"}).
		AddRow("2026-06-09", 3).
		AddRow("2026-06-10", 5)

	mock.ExpectQuery(`SELECT to_char`).
		WithArgs(7).
		WillReturnRows(rows)

	trends, err := svc.AlertTrends(ctx, 0)
	require.NoError(t, err)
	assert.Len(t, trends, 2)
	assert.Equal(t, "2026-06-09", trends[0].Date)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestDashboardService_AlertTrends_上限90天(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewDashboardService(gormDB)
	ctx := context.Background()

	// days=200 截到 90
	mock.ExpectQuery(`SELECT to_char`).
		WithArgs(90).
		WillReturnRows(sqlmock.NewRows([]string{"date", "count"}))

	trends, err := svc.AlertTrends(ctx, 200)
	require.NoError(t, err)
	assert.Empty(t, trends)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestDashboardService_AlertTrends_负数走默认(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewDashboardService(gormDB)
	ctx := context.Background()

	mock.ExpectQuery(`SELECT to_char`).
		WithArgs(7).
		WillReturnRows(sqlmock.NewRows([]string{"date", "count"}))

	_, err := svc.AlertTrends(ctx, -1)
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// ==================== DashboardService.KPIs 测试 ====================
// KPIs 涉及 5 条 SQL：MTTR / MTTD / AlertDensity / Counts / SLA
// 用 sqlmock mock 而非 testcontainers（轻量、毫秒级）

func TestDashboardService_KPIs_完整数据(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewDashboardService(gormDB)
	ctx := context.Background()

	// 1) MTTR: 3600s
	mock.ExpectQuery(`SELECT AVG\(EXTRACT\(EPOCH FROM \(resolve_time`).
		WillReturnRows(sqlmock.NewRows([]string{"avg"}).AddRow(3600.0))

	// 2) MTTD: 300s
	mock.ExpectQuery(`SELECT AVG\(EXTRACT\(EPOCH FROM \(ack_time`).
		WillReturnRows(sqlmock.NewRows([]string{"avg"}).AddRow(300.0))

	// 3) AlertDensity: 35 alerts in 7d
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM alerts WHERE problem_start`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(35))

	// 4) Resolved + Acked counts
	mock.ExpectQuery(`SELECT`).
		WillReturnRows(sqlmock.NewRows([]string{"resolved_alerts", "acked_alerts"}).AddRow(10, 28))

	// 5) SLA: 20 closed, 18 on-time → 90%
	mock.ExpectQuery(`SELECT`).
		WillReturnRows(sqlmock.NewRows([]string{"total", "on_time"}).AddRow(20, 18))

	kpi, err := svc.KPIs(ctx, 7)
	require.NoError(t, err)
	require.NotNil(t, kpi)
	assert.Equal(t, 7, kpi.WindowDays)
	require.NotNil(t, kpi.MTTRSeconds)
	assert.Equal(t, int64(3600), *kpi.MTTRSeconds)
	require.NotNil(t, kpi.MTTDSeconds)
	assert.Equal(t, int64(300), *kpi.MTTDSeconds)
	assert.InDelta(t, 5.0, kpi.AlertDensity, 0.01) // 35/7
	assert.Equal(t, int64(10), kpi.ResolvedAlerts)
	assert.Equal(t, int64(28), kpi.AckedAlerts)
	require.NotNil(t, kpi.SLAClosedRate)
	assert.InDelta(t, 0.9, *kpi.SLAClosedRate, 0.01)
	assert.Equal(t, int64(20), kpi.ClosedTickets)
	assert.Equal(t, int64(18), kpi.OnTimeTickets)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestDashboardService_KPIs_无数据字段全nil(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewDashboardService(gormDB)
	ctx := context.Background()

	// MTTR: NULL (无 resolved alerts)
	mock.ExpectQuery(`SELECT AVG\(EXTRACT\(EPOCH FROM \(resolve_time`).
		WillReturnRows(sqlmock.NewRows([]string{"avg"}).AddRow(nil))

	// MTTD: NULL
	mock.ExpectQuery(`SELECT AVG\(EXTRACT\(EPOCH FROM \(ack_time`).
		WillReturnRows(sqlmock.NewRows([]string{"avg"}).AddRow(nil))

	// AlertDensity: 0 alerts
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM alerts WHERE problem_start`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	// Counts: 0 resolved / 0 acked
	mock.ExpectQuery(`SELECT`).
		WillReturnRows(sqlmock.NewRows([]string{"resolved_alerts", "acked_alerts"}).AddRow(0, 0))

	// SLA: tickets 表无数据
	mock.ExpectQuery(`SELECT`).
		WillReturnRows(sqlmock.NewRows([]string{"total", "on_time"}).AddRow(0, 0))

	kpi, err := svc.KPIs(ctx, 7)
	require.NoError(t, err)
	require.NotNil(t, kpi)
	// MTTR/MTTD nil 不是 0（避免假装有数据）
	assert.Nil(t, kpi.MTTRSeconds)
	assert.Nil(t, kpi.MTTDSeconds)
	assert.Equal(t, 0.0, kpi.AlertDensity)
	// SLA 在无数据时也是 nil
	assert.Nil(t, kpi.SLAClosedRate)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestDashboardService_KPIs_SLA查询报错优雅降级(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewDashboardService(gormDB)
	ctx := context.Background()

	// 1) MTTR
	mock.ExpectQuery(`SELECT AVG\(EXTRACT\(EPOCH FROM \(resolve_time`).
		WillReturnRows(sqlmock.NewRows([]string{"avg"}).AddRow(1800.0))
	// 2) MTTD
	mock.ExpectQuery(`SELECT AVG\(EXTRACT\(EPOCH FROM \(ack_time`).
		WillReturnRows(sqlmock.NewRows([]string{"avg"}).AddRow(200.0))
	// 3) AlertDensity
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM alerts WHERE problem_start`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(14))
	// 4) Counts
	mock.ExpectQuery(`SELECT`).
		WillReturnRows(sqlmock.NewRows([]string{"resolved_alerts", "acked_alerts"}).AddRow(5, 12))
	// 5) SLA 查询失败（tickets 表无 in_sla 字段时典型错误）
	mock.ExpectQuery(`SELECT`).
		WillReturnError(assert.AnError)

	// SLA 报错时整体应不失败，SLARate 为 nil
	kpi, err := svc.KPIs(ctx, 7)
	require.NoError(t, err, "SLA 报错时 KPIs 整体仍应成功")
	require.NotNil(t, kpi)
	assert.Nil(t, kpi.SLAClosedRate)
	// 其他字段正常
	require.NotNil(t, kpi.MTTRSeconds)
	assert.Equal(t, int64(1800), *kpi.MTTRSeconds)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestDashboardService_KPIs_days边界(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewDashboardService(gormDB)
	ctx := context.Background()

	// days=200 应截到 90，但 KPI SQL 用 start (time.Time) 不用 days 参数
	// 验证 MTTR / MTTD 路径仍能跑通
	mock.ExpectQuery(`SELECT AVG\(EXTRACT\(EPOCH FROM \(resolve_time`).
		WillReturnRows(sqlmock.NewRows([]string{"avg"}).AddRow(nil))
	mock.ExpectQuery(`SELECT AVG\(EXTRACT\(EPOCH FROM \(ack_time`).
		WillReturnRows(sqlmock.NewRows([]string{"avg"}).AddRow(nil))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM alerts WHERE problem_start`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery(`SELECT`).
		WillReturnRows(sqlmock.NewRows([]string{"resolved_alerts", "acked_alerts"}).AddRow(0, 0))
	mock.ExpectQuery(`SELECT`).
		WillReturnRows(sqlmock.NewRows([]string{"total", "on_time"}).AddRow(0, 0))

	kpi, err := svc.KPIs(ctx, 200)
	require.NoError(t, err)
	require.NotNil(t, kpi)
	assert.Equal(t, 90, kpi.WindowDays) // 截到 90
	assert.NoError(t, mock.ExpectationsWereMet())
}
