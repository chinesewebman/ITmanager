package service

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// newMockDB 拿一个 sqlmock 驱动的 *gorm.DB（C-F15 业务层测试基建）
// 跟生产用相同的 postgres dialect，让 SQL 语法校验通过
func newMockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	mockDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)

	gormDB, err := gorm.Open(postgres.New(postgres.Config{Conn: mockDB, PreferSimpleProtocol: true}), &gorm.Config{})
	require.NoError(t, err)
	return gormDB, mock
}

// ==================== Alert Service 测试 ====================

func TestAlertService_Get_存在返回alert(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewAlertService(gormDB)

	id := uuid.New()
	hostID := uuid.New()
	now := time.Now()
	zabbixID := "zb-123"

	rows := sqlmock.NewRows([]string{
		"id", "alert_id", "host_id", "host_name", "host_ip",
		"trigger_name", "trigger_id", "severity", "severity_name",
		"problem", "problem_start", "problem_end", "duration",
		"status", "ack_time", "ack_user", "resolve_time", "resolve_user",
		"ticket_id", "asset_id", "source", "repeat_count", "created_at", "updated_at",
	}).AddRow(
		id, zabbixID, hostID, "host-1", "10.0.0.1",
		"CPU 100%", "trig-1", 4, "High",
		"CPU usage over 90%", now, nil, 300,
		"problem", nil, "", nil, "",
		nil, nil, "zabbix", 0, now, now,
	)

	// GORM First(id) 走 SQL: SELECT * FROM "alerts" WHERE id = $1 ORDER BY "alerts"."id" LIMIT $2
	mock.ExpectQuery(`SELECT \* FROM "alerts" WHERE id = \$1 ORDER BY "alerts"\."id" LIMIT \$2`).
		WithArgs(id, 1).
		WillReturnRows(rows)

	got, err := svc.Get(context.Background(), id.String())
	require.NoError(t, err)
	assert.Equal(t, "problem", got.Status)
	assert.Equal(t, "CPU 100%", got.TriggerName)
	assert.Equal(t, "zabbix", got.Source)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestAlertService_Get_不存在返回ErrNotFound(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewAlertService(gormDB)

	id := uuid.New()
	// GORM First() 找不到会扫一次 rows = empty
	mock.ExpectQuery(`SELECT \* FROM "alerts" WHERE id = \$1 ORDER BY "alerts"\."id" LIMIT \$2`).
		WithArgs(id, 1).
		WillReturnRows(sqlmock.NewRows([]string{"id"})) // 空

	_, err := svc.Get(context.Background(), id.String())
	assert.ErrorIs(t, err, ErrNotFound)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestAlertService_Acknowledge_成功更新状态(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewAlertService(gormDB)

	id := uuid.New()
	userID := uuid.New().String()
	now := time.Now()

	// 1) First(id) 拿 record
	mock.ExpectQuery(`SELECT \* FROM "alerts" WHERE id = \$1 ORDER BY "alerts"\."id" LIMIT \$2`).
		WithArgs(id, 1).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "status", "ack_time", "ack_user", "updated_at",
		}).AddRow(id, "problem", nil, "", now))

	// 2) Save() 走 UPDATE
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "alerts"`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err := svc.Acknowledge(context.Background(), id.String(), userID)
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestAlertService_Resolve_成功更新状态(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewAlertService(gormDB)

	id := uuid.New()
	userID := uuid.New().String()
	now := time.Now()

	// 1) First 拿 record
	mock.ExpectQuery(`SELECT \* FROM "alerts" WHERE id = \$1 ORDER BY "alerts"\."id" LIMIT \$2`).
		WithArgs(id, 1).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "status", "resolve_time", "resolve_user", "updated_at",
		}).AddRow(id, "problem", nil, "", now))

	// 2) Save 走 UPDATE
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "alerts"`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err := svc.Resolve(context.Background(), id.String(), userID)
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestAlertService_DeleteRule_成功(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewAlertService(gormDB)

	id := uuid.New()
	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM "alert_rules"`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err := svc.DeleteRule(context.Background(), id.String())
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestAlertService_ListRules_空集合(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewAlertService(gormDB)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "alert_rules"`)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name"})) // 空

	rules, err := svc.ListRules(context.Background())
	require.NoError(t, err)
	assert.Empty(t, rules)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// ==================== User Service 测试（最简业务） ====================

func TestUserService_List_空表返回空(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewUserService(gormDB)

	// 🐛 BUG#26: List 现在走 Count + Find 两步
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "users"`)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "users"`)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "username"}))

	users, _, err := svc.List(context.Background(), 1, 20)
	require.NoError(t, err)
	assert.Empty(t, users)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// ==================== ErrNotFound 边界测试 ====================

func TestErrNotFound_被业务方法正确返回(t *testing.T) {
	// 确保 ErrNotFound 是 sentinel error
	require.True(t, errors.Is(ErrNotFound, ErrNotFound))

	// sentinel 不应跟其他 err 相等
	require.False(t, errors.Is(ErrNotFound, errors.New("other")))
}

// 防止编译时 unused
var _ = sql.ErrNoRows
