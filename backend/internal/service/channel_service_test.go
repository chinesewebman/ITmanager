package service

import (
	"context"
	"errors"
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

// ==================== Create 补全 ====================

func TestChannelService_Create_成功(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewChannelService(gormDB)
	ctx := context.Background()

	ch := &models.NotificationChannel{Name: "钉钉告警", Type: "dingtalk", Config: "{}"} //nolint:exhaustruct

	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "notification_channels"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.NewString()))
	mock.ExpectCommit()

	err := svc.Create(ctx, ch)
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestChannelService_Create_nil返回ErrInvalidInput(t *testing.T) {
	gormDB, _ := newMockDB(t)
	svc := NewChannelService(gormDB)
	err := svc.Create(context.Background(), nil)
	assert.ErrorIs(t, err, ErrInvalidInput)
}

func TestChannelService_Create_唯一冲突返回ErrAlreadyExists(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewChannelService(gormDB)
	ctx := context.Background()

	ch := &models.NotificationChannel{Name: "dup"} //nolint:exhaustruct
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "notification_channels"`).
		WillReturnError(errors.New("duplicate key value violates unique constraint"))
	mock.ExpectRollback()

	err := svc.Create(ctx, ch)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrAlreadyExists)
}

// ==================== Update 补全 ====================

func TestChannelService_Update_成功(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewChannelService(gormDB)
	ctx := context.Background()

	id := uuid.NewString()
	rows := sqlmock.NewRows([]string{"id", "name"}).
		AddRow(id, "原名")
	mock.ExpectQuery(`SELECT \* FROM "notification_channels" WHERE id = \$1`).
		WithArgs(id, 1).
		WillReturnRows(rows)

	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "notification_channels"`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	got, err := svc.Update(ctx, id, map[string]interface{}{"name": "新名"})
	require.NoError(t, err)
	assert.Equal(t, "新名", got.Name)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestChannelService_Update_不存在返回ErrNotFound(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewChannelService(gormDB)

	mock.ExpectQuery(`SELECT \* FROM "notification_channels" WHERE id = \$1`).
		WithArgs("nonexistent", 1).
		WillReturnError(gorm.ErrRecordNotFound)

	got, err := svc.Update(context.Background(), "nonexistent", map[string]interface{}{"name": "x"})
	assert.Nil(t, got)
	assert.ErrorIs(t, err, ErrNotFound)
}

// ==================== Test 真发 ====================

func TestChannelService_Test_不存在返ErrNotFound(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewChannelService(gormDB)

	mock.ExpectQuery(`SELECT \* FROM "notification_channels" WHERE id = \$1`).
		WithArgs("nonexistent", 1).
		WillReturnError(gorm.ErrRecordNotFound)

	err := svc.Test(context.Background(), "nonexistent")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestChannelService_Test_未知类型返错(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := NewChannelService(gormDB)
	ctx := context.Background()

	id := uuid.NewString()
	rows := sqlmock.NewRows([]string{"id", "name", "type", "config"}).
		AddRow(id, "weird", "magic", "{}")
	mock.ExpectQuery(`SELECT \* FROM "notification_channels" WHERE id = \$1`).
		WithArgs(id, 1).
		WillReturnRows(rows)

	err := svc.Test(ctx, id)
	require.Error(t, err, "未知 type 应返错")
	assert.NotErrorIs(t, err, ErrNotFound, "不是 NotFound, 是 Resolver 错")
}
