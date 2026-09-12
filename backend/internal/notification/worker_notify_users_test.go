// Package notification - Worker NotifyUsers 路径单测 (M38-B Round 10)
//
// 覆盖矩阵：
//   - payload.NotifyUserIDs == nil      → 老路径, 不查 users 表, 不发 user 通知
//   - payload.NotifyUserIDs == []       → 显式清空, 同上 (与 NotifyChannelIDs 对齐)
//   - payload.NotifyUserIDs 非空 + users.email 有 → findUserChannel 找到 → 走 sender.Send
//   - payload.NotifyUserIDs 非空 + users.email/phone 都空 → skip 该 user
//   - payload.NotifyUserIDs 含非法 UUID → skip + log warn
//   - payload.NotifyUserIDs 非空 + findUserChannel 找不到 → log + skip
//   - payload NotifyUsers + Dedup: 同 key 60s 内第二次事件 → drop
//
// 测试基础设施：sqlmock + GORM + 注入 mockSender

package notification

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"network-monitor-platform/internal/eventbus"
	"network-monitor-platform/internal/models"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func newNotifyUsersDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	mockDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	gormDB, err := gorm.Open(postgres.New(postgres.Config{Conn: mockDB, PreferSimpleProtocol: true}), &gorm.Config{})
	require.NoError(t, err)
	return gormDB, mock
}

func newNotificationChannelRow(id uuid.UUID, name, typ, cfg string, enabled bool) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "name", "type", "config", "is_enabled", "is_default", "created_at", "updated_at"}).
		AddRow(id, name, typ, cfg, enabled, false, time.Now(), time.Now())
}

func newUserRow(id uuid.UUID, email, phone string) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "email", "phone"}).AddRow(id, email, phone)
}

// buildAlertEvent 把 AlertEventPayload 包装成 eventbus.Event
func buildAlertEvent(p AlertEventPayload) eventbus.Event {
	payload, err := json.Marshal(p)
	if err != nil {
		panic(err)
	}
	topic := "alert.created"
	if p.EventType == "resolved" {
		topic = "alert.resolved"
	}
	return eventbus.Event{
		ID:        uuid.NewString(),
		Topic:     topic,
		Payload:   payload,
		Timestamp: time.Now(),
	}
}

// TestHandleAlertEvent_NotifyUserIDs_Nil_NoUsersDB 老路径: payload 没带 NotifyUserIDs,
// 不应触发任何 users 表查询, 也不应触发 sender 推 user
func TestHandleAlertEvent_NotifyUserIDs_Nil_NoUsersDB(t *testing.T) {
	db, mock := newNotifyUsersDB(t)
	mockSender := &mockSender{typ: "dingtalk"}
	RegisterSender("dingtalk", mockSender)
	defer delete(customSenders, "dingtalk")

	// 没 channel → handleAlertEvent 直接 return nil, 不查 users
	mock.ExpectQuery(`SELECT \* FROM "notification_channels"`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "type", "config", "is_enabled", "is_default", "created_at", "updated_at"}))

	w := NewWorker(db, WorkerConfig{Tick: time.Hour, MaxBatch: 10})
	e := buildAlertEvent(AlertEventPayload{
		AlertID:          uuid.NewString(),
		HostName:         "h1",
		Severity:         3,
		Trigger:          "t1",
		Status:           "problem",
		EventType:        "created",
		NotifyUserIDs:    nil, // 老路径
		TriggerID:        "trig-1",
		ProblemStartUnix: time.Now().Unix(),
	})
	require.NoError(t, w.handleAlertEvent(context.Background(), e))
	assert.NoError(t, mock.ExpectationsWereMet(), "should not have queried users table")
	assert.Equal(t, int32(0), atomic.LoadInt32(&mockSender.hits))
}

// TestHandleAlertEvent_NotifyUserIDs_Empty_NoUsersDB 显式空数组: 不发 user 通知
func TestHandleAlertEvent_NotifyUserIDs_Empty_NoUsersDB(t *testing.T) {
	db, mock := newNotifyUsersDB(t)
	mockSender := &mockSender{typ: "dingtalk"}
	RegisterSender("dingtalk", mockSender)
	defer delete(customSenders, "dingtalk")

	mock.ExpectQuery(`SELECT \* FROM "notification_channels"`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "type", "config", "is_enabled", "is_default", "created_at", "updated_at"}))

	w := NewWorker(db, WorkerConfig{Tick: time.Hour, MaxBatch: 10})
	e := buildAlertEvent(AlertEventPayload{
		AlertID:          uuid.NewString(),
		HostName:         "h2",
		Severity:         3,
		Trigger:          "t2",
		Status:           "problem",
		EventType:        "created",
		NotifyUserIDs:    []string{}, // 显式空 → 仍不发
		TriggerID:        "trig-2",
		ProblemStartUnix: time.Now().Unix(),
	})
	require.NoError(t, w.handleAlertEvent(context.Background(), e))
	assert.NoError(t, mock.ExpectationsWereMet())
	assert.Equal(t, int32(0), atomic.LoadInt32(&mockSender.hits))
}

// TestHandleAlertEvent_NotifyUsers_EmailHit_Sends 完整路径
func TestHandleAlertEvent_NotifyUsers_EmailHit_Sends(t *testing.T) {
	db, mock := newNotifyUsersDB(t)
	mockSender := &mockSender{typ: "email"}
	RegisterSender("email", mockSender)
	defer delete(customSenders, "email")

	userID := uuid.New()
	channelID := uuid.New()
	userEmail := "ops@example.com"
	channelConfig := `{"to":"ops@example.com"}`

	// 1) channels 列表 (空 → 不发 channel 路径, 直接进 NotifyUsers)
	mock.ExpectQuery(`SELECT \* FROM "notification_channels"`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "type", "config", "is_enabled", "is_default", "created_at", "updated_at"}))

	// 2) users 列表 (NotifyUsers 查)
	mock.ExpectQuery(`SELECT \* FROM "users"`).
		WillReturnRows(newUserRow(userID, userEmail, ""))

	// 3) findUserChannel 查 channels LIKE email → 命中
	mock.ExpectQuery(`SELECT \* FROM "notification_channels"`).
		WillReturnRows(newNotificationChannelRow(channelID, "邮件", "email", channelConfig, true))

	w := NewWorker(db, WorkerConfig{Tick: time.Hour, MaxBatch: 10})
	e := buildAlertEvent(AlertEventPayload{
		AlertID:          uuid.NewString(),
		HostName:         "h3",
		Severity:         4,
		Trigger:          "high cpu",
		Status:           "problem",
		EventType:        "created",
		NotifyUserIDs:    []string{userID.String()},
		TriggerID:        "trig-3",
		ProblemStartUnix: time.Now().Unix(),
	})
	require.NoError(t, w.handleAlertEvent(context.Background(), e))
	assert.NoError(t, mock.ExpectationsWereMet())
	assert.Equal(t, int32(1), atomic.LoadInt32(&mockSender.hits), "email sender 应当被调用 1 次")
}

// TestHandleAlertEvent_NotifyUsers_NoContact_Skips user.email/phone 都空 → skip 该 user
func TestHandleAlertEvent_NotifyUsers_NoContact_Skips(t *testing.T) {
	db, mock := newNotifyUsersDB(t)
	mockSender := &mockSender{typ: "dingtalk"}
	RegisterSender("dingtalk", mockSender)
	defer delete(customSenders, "dingtalk")

	userID := uuid.New()

	mock.ExpectQuery(`SELECT \* FROM "notification_channels"`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "type", "config", "is_enabled", "is_default", "created_at", "updated_at"}))

	mock.ExpectQuery(`SELECT \* FROM "users"`).
		WillReturnRows(newUserRow(userID, "", ""))

	w := NewWorker(db, WorkerConfig{Tick: time.Hour, MaxBatch: 10})
	e := buildAlertEvent(AlertEventPayload{
		AlertID:          uuid.NewString(),
		HostName:         "h4",
		Severity:         3,
		Trigger:          "t4",
		Status:           "problem",
		EventType:        "created",
		NotifyUserIDs:    []string{userID.String()},
		TriggerID:        "trig-4",
		ProblemStartUnix: time.Now().Unix(),
	})
	require.NoError(t, w.handleAlertEvent(context.Background(), e))
	assert.NoError(t, mock.ExpectationsWereMet())
	assert.Equal(t, int32(0), atomic.LoadInt32(&mockSender.hits), "没 contact 不应触发 sender")
}

// TestHandleAlertEvent_NotifyUsers_BadUUID_Skips 含非法 UUID → skip + log warn
func TestHandleAlertEvent_NotifyUsers_BadUUID_Skips(t *testing.T) {
	db, mock := newNotifyUsersDB(t)
	mockSender := &mockSender{typ: "dingtalk"}
	RegisterSender("dingtalk", mockSender)
	defer delete(customSenders, "dingtalk")

	mock.ExpectQuery(`SELECT \* FROM "notification_channels"`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "type", "config", "is_enabled", "is_default", "created_at", "updated_at"}))

	w := NewWorker(db, WorkerConfig{Tick: time.Hour, MaxBatch: 10})
	e := buildAlertEvent(AlertEventPayload{
		AlertID:          uuid.NewString(),
		HostName:         "h5",
		Severity:         3,
		Trigger:          "t5",
		Status:           "problem",
		EventType:        "created",
		NotifyUserIDs:    []string{"not-a-uuid"},
		TriggerID:        "trig-5",
		ProblemStartUnix: time.Now().Unix(),
	})
	require.NoError(t, w.handleAlertEvent(context.Background(), e))
	assert.NoError(t, mock.ExpectationsWereMet())
	assert.Equal(t, int32(0), atomic.LoadInt32(&mockSender.hits), "非法 UUID 不应触发 sender")
}

// TestHandleAlertEvent_NotifyUsers_NoChannel_Skips users 有 contact 但没 channel 匹配 → skip
func TestHandleAlertEvent_NotifyUsers_NoChannel_Skips(t *testing.T) {
	db, mock := newNotifyUsersDB(t)
	mockSender := &mockSender{typ: "email"}
	RegisterSender("email", mockSender)
	defer delete(customSenders, "email")

	userID := uuid.New()

	// 1) channels 列表 (空)
	mock.ExpectQuery(`SELECT \* FROM "notification_channels"`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "type", "config", "is_enabled", "is_default", "created_at", "updated_at"}))

	// 2) users 列表 (有 email)
	mock.ExpectQuery(`SELECT \* FROM "users"`).
		WillReturnRows(newUserRow(userID, "orphan@example.com", ""))

	// 3) findUserChannel 查 channels LIKE email → 找不到
	mock.ExpectQuery(`SELECT \* FROM "notification_channels"`).
		WillReturnError(sqlmock.ErrCancelled)

	w := NewWorker(db, WorkerConfig{Tick: time.Hour, MaxBatch: 10})
	e := buildAlertEvent(AlertEventPayload{
		AlertID:          uuid.NewString(),
		HostName:         "h6",
		Severity:         3,
		Trigger:          "t6",
		Status:           "problem",
		EventType:        "created",
		NotifyUserIDs:    []string{userID.String()},
		TriggerID:        "trig-6",
		ProblemStartUnix: time.Now().Unix(),
	})
	require.NoError(t, w.handleAlertEvent(context.Background(), e))
	assert.NoError(t, mock.ExpectationsWereMet())
	assert.Equal(t, int32(0), atomic.LoadInt32(&mockSender.hits), "无 channel 匹配不应触发 sender")
}

// TestHandleAlertEvent_Dedup_SecondEventWithin60s_Dropped Round 7 + Round 10 联合验证:
// 同一 (trigger_id + problem_start) 在 60s 内第二次事件, dedup 命中 → 不进 channels 推送
func TestHandleAlertEvent_Dedup_SecondEventWithin60s_Dropped(t *testing.T) {
	db, mock := newNotifyUsersDB(t)
	mockSender := &mockSender{typ: "dingtalk"}
	RegisterSender("dingtalk", mockSender)
	defer delete(customSenders, "dingtalk")

	// 第一次: 走完 channel 推送 (channels 空 → 立即 return)
	mock.ExpectQuery(`SELECT \* FROM "notification_channels"`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "type", "config", "is_enabled", "is_default", "created_at", "updated_at"}))

	w := NewWorker(db, WorkerConfig{Tick: time.Hour, MaxBatch: 10})
	w.deduper = NewDeduper()

	problemStart := time.Now().Unix()
	e1 := buildAlertEvent(AlertEventPayload{
		AlertID:          uuid.NewString(),
		HostName:         "h7",
		Severity:         3,
		Trigger:          "t7",
		Status:           "problem",
		EventType:        "created",
		TriggerID:        "trig-dedup",
		ProblemStartUnix: problemStart,
	})
	require.NoError(t, w.handleAlertEvent(context.Background(), e1))

	// 第二次同 key → dedup drop, 不查 channels
	e2 := buildAlertEvent(AlertEventPayload{
		AlertID:          uuid.NewString(),
		HostName:         "h7",
		Severity:         3,
		Trigger:          "t7",
		Status:           "problem",
		EventType:        "created",
		TriggerID:        "trig-dedup",
		ProblemStartUnix: problemStart,
	})
	require.NoError(t, w.handleAlertEvent(context.Background(), e2))

	assert.NoError(t, mock.ExpectationsWereMet(), "dedup 后第二次不应触发 DB 查询")
}

// 静默 unused 警告
var _ = models.NotificationChannel{}
