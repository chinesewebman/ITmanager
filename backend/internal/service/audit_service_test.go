package service

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuditService_List_空表返空(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewAuditService(gormDB)

	mock.ExpectQuery(`SELECT \* FROM "audit_logs"`).
		WithArgs(10).
		WillReturnRows(sqlmock.NewRows([]string{"id", "action"}))

	items, err := svc.List(context.Background(), AuditFilter{Limit: 10})
	require.NoError(t, err)
	assert.Empty(t, items)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestAuditService_List_带user过滤(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewAuditService(gormDB)

	userID := uuid.New()
	mock.ExpectQuery(`SELECT \* FROM "audit_logs" WHERE user_id`).
		WithArgs(userID, 10).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	_, err := svc.List(context.Background(), AuditFilter{UserID: &userID, Limit: 10})
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestAuditService_List_cursor分页(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewAuditService(gormDB)

	ts := time.Date(2026, 6, 17, 19, 0, 0, 0, time.UTC)
	cursorID := uuid.New()
	// gorm 翻译 (a, b) < (?, ?) 为 SQL 字符串, 实际 mock 宽松匹配
	mock.ExpectQuery(`SELECT \* FROM "audit_logs" WHERE`).
		WithArgs(ts, cursorID, 10).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	_, err := svc.List(context.Background(), AuditFilter{
		Limit: 10, CursorTS: ts, CursorID: cursorID,
	})
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestAuditService_List_Path前缀匹配(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewAuditService(gormDB)

	mock.ExpectQuery(`SELECT \* FROM "audit_logs" WHERE path LIKE`).
		WithArgs("/api/v1/users%", 10).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	_, err := svc.List(context.Background(), AuditFilter{Path: "/api/v1/users", Limit: 10})
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestAuditService_List_LimitClamp(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewAuditService(gormDB)

	// limit=9999 → clamp 1000
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "audit_logs"`)).
		WithArgs(1000).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	_, err := svc.List(context.Background(), AuditFilter{Limit: 9999})
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}
