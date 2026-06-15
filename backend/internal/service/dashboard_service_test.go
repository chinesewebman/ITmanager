package service

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ==================== Dashboard Service 测试 ====================

func TestDashboardService_Stats_5个count聚合(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewDashboardService(gormDB)
	ctx := context.Background()

	// 5 个 Count：assets / alerts(problem) / sites / tickets(!closed)
	// machines/networks 占位 = assets，不发额外 SQL
	mock.ExpectQuery(`SELECT count\(\*\) FROM "assets"`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(100))

	mock.ExpectQuery(`SELECT count\(\*\) FROM "alerts" WHERE status =`).
		WithArgs("problem").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(5))

	mock.ExpectQuery(`SELECT count\(\*\) FROM "sites"`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(3))

	mock.ExpectQuery(`SELECT count\(\*\) FROM "tickets" WHERE`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(20))

	stats, err := svc.Stats(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(100), stats.Assets)
	assert.Equal(t, int64(5), stats.Alerts)
	assert.Equal(t, int64(3), stats.Sites)
	assert.Equal(t, int64(20), stats.Tickets)
	assert.Equal(t, int64(100), stats.Machines) // 占位
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
