package service

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"network-monitor-platform/internal/models"
)

// ==================== Ticket Service 测试 ====================

func TestTicketService_Get_存在返回(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewTicketService(gormDB)
	ctx := context.Background()

	id := uuid.NewString()
	rows := sqlmock.NewRows([]string{"id", "title", "status", "priority"}).
		AddRow(id, "Test ticket", "open", "high")

	mock.ExpectQuery(`SELECT \* FROM "tickets" WHERE id = \$1`).
		WithArgs(id, 1).
		WillReturnRows(rows)

	got, err := svc.Get(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, "Test ticket", got.Title)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestTicketService_Get_不存在返回ErrNotFound(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewTicketService(gormDB)
	ctx := context.Background()

	mock.ExpectQuery(`SELECT \* FROM "tickets" WHERE id = \$1`).
		WithArgs("nonexistent", 1).
		WillReturnError(gorm.ErrRecordNotFound)

	got, err := svc.Get(ctx, "nonexistent")
	assert.Nil(t, got)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestTicketService_Create_空title返回ErrInvalidInput(t *testing.T) {
	gormDB, _ := newMockDB(t)
	svc := NewTicketService(gormDB)
	ctx := context.Background()

	tk := &models.Ticket{Title: ""} //nolint:exhaustruct
	err := svc.Create(ctx, tk)
	assert.ErrorIs(t, err, ErrInvalidInput)
}

func TestTicketService_List_空filter返列表(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewTicketService(gormDB)
	ctx := context.Background()

	rows := sqlmock.NewRows([]string{"id", "title", "status"}).
		AddRow(uuid.NewString(), "t-1", "open").
		AddRow(uuid.NewString(), "t-2", "closed")

	// List 走 query + count 两条 SQL
	mock.ExpectQuery(`SELECT count\(\*\) FROM "tickets"`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))

	mock.ExpectQuery(`SELECT \* FROM "tickets"`).
		WillReturnRows(rows)

	list, total, err := svc.List(ctx, TicketFilter{})
	require.NoError(t, err)
	assert.Equal(t, int64(2), total)
	assert.Len(t, list, 2)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestTicketService_Update_空updates返当前(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewTicketService(gormDB)
	ctx := context.Background()

	id := uuid.NewString()
	// 空 updates 走 Get 返回当前（不调 Updates）
	rows := sqlmock.NewRows([]string{"id", "title", "status"}).
		AddRow(id, "unchanged", "open")

	mock.ExpectQuery(`SELECT \* FROM "tickets" WHERE id = \$1`).
		WithArgs(id, 1).
		WillReturnRows(rows)

	got, err := svc.Update(ctx, id, map[string]interface{}{})
	require.NoError(t, err)
	assert.Equal(t, "unchanged", got.Title)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// modelsTicket 测试辅助已用真 models.Ticket，hack helper 删
