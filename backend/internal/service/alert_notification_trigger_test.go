package service

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWriteNotificationTrigger_HasChannels_WritesPendingLogs v1.1 P2-B-3:
// 验证 trigger 在 status 变更后写 1 行/channel 的 pending notification_log。
// mock 流程：SELECT notification_channels → 1 row → INSERT notification_logs (1 row) → commit
func TestWriteNotificationTrigger_HasChannels_WritesPendingLogs(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := &alertService{db: gormDB}
	ctx := context.Background()

	alertID := uuid.New()
	channelID := uuid.New()
	channelName := "ops-dingtalk"
	userID := "u-1"

	// 1) SELECT 拿所有 is_enabled=true 的 channel
	channelRows := sqlmock.NewRows([]string{"id", "name", "type", "is_enabled"}).
		AddRow(channelID, channelName, "dingtalk", true)
	mock.ExpectQuery(`SELECT \* FROM "notification_channels"`).
		WithArgs(true).
		WillReturnRows(channelRows)

	// 2) INSERT notification_logs — gorm batch create 包事务
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "notification_logs"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New()))
	mock.ExpectCommit()

	err := svc.writeNotificationTrigger(ctx, alertID, "acknowledged", userID)
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestWriteNotificationTrigger_NoChannels_NoInsert v1.1 P2-B-3:
// 没有启用的 channel → 不写 log
func TestWriteNotificationTrigger_NoChannels_NoInsert(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := &alertService{db: gormDB}
	ctx := context.Background()

	emptyRows := sqlmock.NewRows([]string{"id", "name", "type", "is_enabled"})
	mock.ExpectQuery(`SELECT \* FROM "notification_channels"`).
		WithArgs(true).
		WillReturnRows(emptyRows)

	err := svc.writeNotificationTrigger(ctx, uuid.New(), "resolved", "u-2")
	require.NoError(t, err)
	// 关键断言: 没有 INSERT 发生
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestWriteNotificationTrigger_ChannelQueryFails_DoesNotError v1.1 P2-B-3:
// 主流程已成功改 status，trigger 失败仅 log，不应阻塞主流程
func TestWriteNotificationTrigger_ChannelQueryFails_DoesNotError(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := &alertService{db: gormDB}
	ctx := context.Background()

	mock.ExpectQuery(`SELECT \* FROM "notification_channels"`).
		WithArgs(true).
		WillReturnError(assert.AnError)

	err := svc.writeNotificationTrigger(ctx, uuid.New(), "resolved", "u-3")
	require.NoError(t, err, "trigger 失败不应 return error — 主流程已成功改 status")
	assert.NoError(t, mock.ExpectationsWereMet())
}

// 避免 time 包被 unused warning
var _ = time.Now
