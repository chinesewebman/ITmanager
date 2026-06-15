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

// ==================== Channel Service 测试 ====================

func TestChannelService_List_返回列表(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewChannelService(gormDB)
	ctx := context.Background()

	rows := sqlmock.NewRows([]string{"id", "name", "type", "config"}).
		AddRow(uuid.NewString(), "email-1", "email", "{}").
		AddRow(uuid.NewString(), "webhook-1", "webhook", "{}")

	mock.ExpectQuery(`SELECT \* FROM "notification_channels"`).
		WillReturnRows(rows)

	chs, err := svc.List(ctx)
	require.NoError(t, err)
	assert.Len(t, chs, 2)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestChannelService_Get_存在返回(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewChannelService(gormDB)
	ctx := context.Background()

	id := uuid.NewString()
	rows := sqlmock.NewRows([]string{"id", "name", "type"}).
		AddRow(id, "test-channel", "email")

	mock.ExpectQuery(`SELECT \* FROM "notification_channels" WHERE id = \$1`).
		WithArgs(id, 1).
		WillReturnRows(rows)

	got, err := svc.Get(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, "test-channel", got.Name)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestChannelService_Get_不存在返回ErrNotFound(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewChannelService(gormDB)
	ctx := context.Background()

	mock.ExpectQuery(`SELECT \* FROM "notification_channels" WHERE id = \$1`).
		WithArgs("nonexistent", 1).
		WillReturnError(gorm.ErrRecordNotFound)

	got, err := svc.Get(ctx, "nonexistent")
	assert.Nil(t, got)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestChannelService_Create_空name返回ErrInvalidInput(t *testing.T) {
	gormDB, _ := newMockDB(t)
	svc := NewChannelService(gormDB)
	ctx := context.Background()

	err := svc.Create(ctx, &models.NotificationChannel{Name: ""}) //nolint:exhaustruct
	assert.ErrorIs(t, err, ErrInvalidInput)

	err = svc.Create(ctx, nil)
	assert.ErrorIs(t, err, ErrInvalidInput)
}

func TestChannelService_Delete_成功(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewChannelService(gormDB)
	ctx := context.Background()

	id := uuid.NewString()

	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM "notification_channels"`).
		WithArgs(id).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err := svc.Delete(ctx, id)
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestChannelService_Update_空updates走Get(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewChannelService(gormDB)
	ctx := context.Background()

	id := uuid.NewString()
	rows := sqlmock.NewRows([]string{"id", "name"}).
		AddRow(id, "unchanged")

	// 空 updates：Update 内部直接走 Get，不发 UPDATE
	mock.ExpectQuery(`SELECT \* FROM "notification_channels" WHERE id = \$1`).
		WithArgs(id, 1).
		WillReturnRows(rows)

	got, err := svc.Update(ctx, id, map[string]interface{}{})
	require.NoError(t, err)
	assert.Equal(t, "unchanged", got.Name)
	assert.NoError(t, mock.ExpectationsWereMet())
}
