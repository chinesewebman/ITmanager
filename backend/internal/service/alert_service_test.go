package service

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// alert_service_test 只补 alert_service 现有未覆盖的边界场景
// 基础 happy path 已存在 asset_service_test.go (C-F15 一锅炖版)

func TestAlertService_BulkAcknowledge_批量确认(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewAlertService(gormDB)
	ctx := context.Background()

	ids := []string{uuid.NewString(), uuid.NewString(), uuid.NewString()}
	userID := uuid.NewString()

	// 单 SQL UPDATE WHERE id IN (gorm 自动开事务)
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "alerts" SET`).
		WillReturnResult(sqlmock.NewResult(0, 3))
	mock.ExpectCommit()

	affected, err := svc.BulkAcknowledge(ctx, ids, userID)
	require.NoError(t, err)
	assert.Equal(t, int64(3), affected)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestAlertService_BulkResolve_空ids返回0(t *testing.T) {
	gormDB, _ := newMockDB(t)
	svc := NewAlertService(gormDB)
	ctx := context.Background()

	// 空 ids 短路返回 0，不发 SQL
	affected, err := svc.BulkResolve(ctx, []string{}, uuid.NewString())
	require.NoError(t, err)
	assert.Equal(t, int64(0), affected)
}

// BulkResolve 完整 happy path 跳过 —— gorm Find() 拿 24 列 + UPDATE 事务
// 链路在 handler 测里覆盖（实测走 e2e），service 单测边界用例即可。

func TestAlertService_BulkDelete_空ids返回0(t *testing.T) {
	gormDB, _ := newMockDB(t)
	svc := NewAlertService(gormDB)
	ctx := context.Background()

	// 空 ids 不应发 SQL（短路）
	affected, err := svc.BulkDelete(ctx, []string{})
	require.NoError(t, err)
	assert.Equal(t, int64(0), affected)
}

func TestAlertService_BulkDelete_超1000拒绝(t *testing.T) {
	gormDB, _ := newMockDB(t)
	svc := NewAlertService(gormDB)
	ctx := context.Background()

	ids := make([]string, 1001)
	for i := range ids {
		ids[i] = uuid.NewString()
	}

	affected, err := svc.BulkDelete(ctx, ids)
	assert.Error(t, err, "超过 1000 上限应拒绝")
	assert.Equal(t, int64(0), affected)
}

func TestAlertService_ListRules_返回列表(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewAlertService(gormDB)
	ctx := context.Background()

	rows := sqlmock.NewRows([]string{"id", "name", "enabled", "severity", "expression"}).
		AddRow(uuid.NewString(), "rule-1", true, 4, "cpu>90").
		AddRow(uuid.NewString(), "rule-2", false, 3, "mem>80")

	mock.ExpectQuery(`SELECT \* FROM "alert_rules"`).
		WillReturnRows(rows)

	rules, err := svc.ListRules(ctx)
	require.NoError(t, err)
	assert.Len(t, rules, 2)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// 注: statsInternal 是私有方法，Stats() 公共方法在 dashboard_service 测
// alert 测重点是 CRUD + bulk 路径（ack/resolve/list/create 边界）
// public Stats() 由 Stats() 自身实现（空走 statsInternal）—— handler 测覆盖更经济
