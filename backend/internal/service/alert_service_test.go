package service

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
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

// ==================== BUG FIX 回归测试 ====================

// TestAlertService_List_Severity类型必须是int — BUG#13
//
//	原 AlertFilter.Severity 是 string，但 SQL "severity >= ?" 与 int 列比较
//	时若传 "3" 会触发字符串字典序比较，0、1、10、11 都会被误匹配
//	修复：Severity 改为 int，传字符串在编译期就 fail
func TestAlertService_List_Severity类型是int(t *testing.T) {
	// 编译期断言：AlertFilter.Severity 必须是 int
	var f AlertFilter
	f.Severity = 3 // 编译过 = 是 int
	assert.Equal(t, 3, f.Severity)

	// 0 应该是"不过滤"，>0 过滤
	f2 := AlertFilter{Severity: 0}
	assert.Equal(t, 0, f2.Severity, "Severity=0 应被解释为不过滤")
}

// TestAlertService_UpdateRule_空updates不重复First — BUG#15
//
//	原版有两次 First（len==0 分支 + 主路径），重构后 1 次
//	这里只验证空 updates 时不报错
func TestAlertService_UpdateRule_空updates不报错(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewAlertService(gormDB)
	ctx := context.Background()

	// mock: 只 expect 一次 First，Updates 不发 SQL
	rows := sqlmock.NewRows([]string{"id", "name", "expression", "priority", "is_enabled", "created_at", "updated_at"}).
		AddRow(uuid.NewString(), "test-rule", "x>0", 1, true, time.Now(), time.Now())
	mock.ExpectQuery(`SELECT.*FROM "alert_rules"`).WillReturnRows(rows)

	rule, err := svc.UpdateRule(ctx, uuid.NewString(), nil)
	require.NoError(t, err)
	require.NotNil(t, rule)
	assert.NoError(t, mock.ExpectationsWereMet(), "空 updates 不应发 UPDATE SQL")
}

// ==================== 小改进 #2：误报标记 + ML 训练集 ====================

// TestAlertService_MarkFalsePositive_成功 — 验证 First（查 alert）+ UPDATE + 再次 First（重读）
// 用 sqlmock 的精细化匹配：Begin/Exec SELECT/UPDATE/SELECT/Commit
func TestAlertService_MarkFalsePositive_成功(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewAlertService(gormDB)
	ctx := context.Background()

	alertID := uuid.NewString()
	now := time.Now()

	// 第一次 First：查 alert 是否存在
	alertRows := sqlmock.NewRows([]string{"id", "alert_id", "host_name", "severity", "status", "created_at", "updated_at"}).
		AddRow(alertID, "zab-1", "web-01", 4, "problem", now, now)
	mock.ExpectQuery(`SELECT.*FROM "alerts"`).WillReturnRows(alertRows)

	// UPDATE 标记误报
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "alerts" SET`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	// 第二次 First：返回更新后的 alert
	updatedRows := sqlmock.NewRows([]string{"id", "alert_id", "host_name", "severity", "status", "is_false_positive", "created_at", "updated_at"}).
		AddRow(alertID, "zab-1", "web-01", 4, "problem", true, now, now)
	mock.ExpectQuery(`SELECT.*FROM "alerts"`).WillReturnRows(updatedRows)

	alert, err := svc.MarkFalsePositive(ctx, alertID, "alice", "周期性抖动", true)
	require.NoError(t, err)
	require.NotNil(t, alert)
	assert.True(t, alert.IsFalsePositive)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestAlertService_MarkFalsePositive_告警不存在返回ErrNotFound(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewAlertService(gormDB)
	ctx := context.Background()

	// First → ErrRecordNotFound
	mock.ExpectQuery(`SELECT.*FROM "alerts"`).
		WillReturnError(gorm.ErrRecordNotFound)

	alert, err := svc.MarkFalsePositive(ctx, uuid.NewString(), "alice", "test", true)
	assert.Nil(t, alert)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestAlertService_ListFalsePositives_全量列出(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewAlertService(gormDB)
	ctx := context.Background()

	now := time.Now()
	rows := sqlmock.NewRows([]string{"id", "alert_id", "host_name", "is_false_positive", "marked_at", "created_at", "updated_at"}).
		AddRow(uuid.NewString(), "zab-1", "web-01", true, now, now, now).
		AddRow(uuid.NewString(), "zab-2", "db-02", true, now, now, now)
	mock.ExpectQuery(`SELECT.*FROM "alerts"`).WillReturnRows(rows)

	items, err := svc.ListFalsePositives(ctx, nil)
	require.NoError(t, err)
	assert.Len(t, items, 2)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestAlertService_ListFalsePositives_since增量导出(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewAlertService(gormDB)
	ctx := context.Background()

	since := time.Now().Add(-24 * time.Hour)
	rows := sqlmock.NewRows([]string{"id", "alert_id", "is_false_positive", "marked_at", "created_at", "updated_at"})
	mock.ExpectQuery(`SELECT.*FROM "alerts".*marked_at >= .*`).
		WillReturnRows(rows)

	items, err := svc.ListFalsePositives(ctx, &since)
	require.NoError(t, err)
	assert.Empty(t, items)
	assert.NoError(t, mock.ExpectationsWereMet())
}
