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
