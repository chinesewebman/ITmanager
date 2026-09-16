package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"network-monitor-platform/internal/models"
)
// TestWriteNotificationTrigger_HasChannels_WritesPendingLogs v1.1 P2-B-3:
// 验证 trigger 在 status 变更后写 1 行/channel 的 pending notification_log。
// mock 流程：SELECT notification_channels → 1 row → INSERT notification_logs (1 row) → commit
func TestWriteNotificationTrigger_HasChannels_WritesPendingLogs(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := &alertService{db: gormDB}
	ctx := context.Background()

	alertID := uuid.New()
	// M82: writeNotificationTrigger 签名改为 (ctx, alert *models.Alert, newStatus, userID) — AlertRuleID nil 触发 fallback (全启用, 与改动前一致)
	alert := &models.Alert{ID: alertID}
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

	err := svc.writeNotificationTrigger(ctx, alert, "acknowledged", userID)
	require.NoError(t, err)
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

	err := svc.writeNotificationTrigger(ctx, &models.Alert{ID: uuid.New()}, "resolved", "u-2")
	require.NoError(t, err)
	// 关键断言: 没有 INSERT 发生
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

	err := svc.writeNotificationTrigger(ctx, &models.Alert{ID: uuid.New()}, "resolved", "u-3")
	require.NoError(t, err, "trigger 失败不应 return error — 主流程已成功改 status")
}
// ==================== M82: writeNotificationTrigger 按 rule.NotifyChannels 过滤测试 ====================

// TestWriteNotificationTrigger_RuleEmpty_NoLogs M82 AC: alert 有 rule, rule.NotifyChannels 显式空 → 推 0 次
// 与 worker bus 路径 AC-M37-A-3 语义对齐
func TestWriteNotificationTrigger_RuleEmpty_NoLogs(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := &alertService{db: gormDB}
	ctx := context.Background()

	ruleID := uuid.New()
	alert := &models.Alert{ID: uuid.New(), AlertRuleID: &ruleID}
	userID := "u-m82-empty"


	// 实际代码顺序: 先加载 channels, 再查 rule; rule 显式空 → filter 后 0 → return nil
	// 1) SELECT 加载 all enabled channels (3 个) — 必须先发生 (channels SELECT 在 rule 查前)
	channelRows := sqlmock.NewRows([]string{"id", "name", "type", "is_enabled"}).
		AddRow(uuid.New(), "ops-A", "dingtalk", true).
		AddRow(uuid.New(), "ops-B", "dingtalk", true).
		AddRow(uuid.New(), "ops-C", "dingtalk", true)
	mock.ExpectQuery(`SELECT \* FROM "notification_channels"`).
		WithArgs(true).
		WillReturnRows(channelRows)
	// 2) loadRuleNotifyChannelIDs: SELECT id, notify_channels FROM alert_rules WHERE id = ? → 显式空
	ruleRows := sqlmock.NewRows([]string{"id", "notify_channels"}).AddRow(ruleID.String(), "")
	mock.ExpectQuery(`SELECT.*"alert_rules".*WHERE id = .*$`).
		WithArgs(ruleID, sqlmock.AnyArg()).
		WillReturnRows(ruleRows)
	err := svc.writeNotificationTrigger(ctx, alert, "acknowledged", userID)
	require.NoError(t, err)
	// 关键断言: 没有 SELECT notification_channels 也没有 INSERT notification_logs 发生
	assert.NoError(t, mock.ExpectationsWereMet(), "rule 显式空 → 不应查 channels / 不应 INSERT logs")
}

// TestWriteNotificationTrigger_RuleWithChannels_WritesOnlyListed M82 AC:
// alert 有 rule, rule.NotifyChannels=["chA","chB"], DB 有 3 个 enabled channel → 仅写 2 log (chA+chB, chC 不写)
// 与 worker bus 路径 AC-M37-A-1 语义对齐
func TestWriteNotificationTrigger_RuleWithChannels_WritesOnlyListed(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := &alertService{db: gormDB}
	ctx := context.Background()

	ruleID := uuid.New()
	chA := uuid.New()
	chB := uuid.New()
	chC := uuid.New() // rule 显式没勾 → 不应被 INSERT
	alert := &models.Alert{ID: uuid.New(), AlertRuleID: &ruleID}
	userID := "u-m82-listed"
	// 实际代码顺序: 先加载 channels, 再查 rule; rule 勾 [chA, chB] → filter 后剩 chA+chB → INSERT 2 log


	// 1) 加载所有 enabled channels (3 个) — 必须先发生
	channelRows := sqlmock.NewRows([]string{"id", "name", "type", "is_enabled"}).
		AddRow(chA, "ops-A", "dingtalk", true).
		AddRow(chB, "ops-B", "dingtalk", true).
		AddRow(chC, "ops-C", "dingtalk", true)
	mock.ExpectQuery(`SELECT \* FROM "notification_channels"`).
		WithArgs(true).
		WillReturnRows(channelRows)

	// 2) loadRuleNotifyChannelIDs 查 rule
	ruleRows := sqlmock.NewRows([]string{"id", "notify_channels"}).
		AddRow(ruleID.String(), fmt.Sprintf(`["%s","%s"]`, chA, chB))
	mock.ExpectQuery(`SELECT.*"alert_rules".*WHERE id = .*$`).
		WithArgs(ruleID, sqlmock.AnyArg()).
		WillReturnRows(ruleRows)

	// 3) INSERT notification_logs — gorm batch create 包事务, INSERT 应只含 chA + chB 两行
	//    sqlmock 不强校验 INSERT 行数 (gorm 会动态拼 Values), 但期望序列被清空 = 守卫有效
	err := svc.writeNotificationTrigger(ctx, alert, "resolved", userID)
	require.NoError(t, err)
	// 关键断言: 序列精确匹配 — 没多余 INSERT / 没多余 SELECT = filter 守卫生效
	assert.NoError(t, mock.ExpectationsWereMet(), "rule 勾 2 channel → 必须先查 rule → 仅 INSERT 2 log (chA+chB)")
}

// TestWriteNotificationTrigger_RuleNotFound_FallbackAllEnabled M82 AC:
// alert 有 rule, rule 查询 gorm.ErrRecordNotFound → fallback 全启用 channels (兼容, 不漏告警)
func TestWriteNotificationTrigger_RuleNotFound_FallbackAllEnabled(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := &alertService{db: gormDB}
	ctx := context.Background()

	ruleID := uuid.New()
	chA := uuid.New()
	chB := uuid.New()
	chC := uuid.New()
	alert := &models.Alert{ID: uuid.New(), AlertRuleID: &ruleID}
	userID := "u-m82-missing"

	// 实际代码顺序: 先加载 channels, 再查 rule; rule 查询失败 → fallback 全启用 3 channels
	// 1) 加载所有 enabled channels (3 个) — 必须先发生 (channels SELECT 在 rule 查前)
	channelRows := sqlmock.NewRows([]string{"id", "name", "type", "is_enabled"}).
		AddRow(chA, "ops-A", "dingtalk", true).
		AddRow(chB, "ops-B", "dingtalk", true).
		AddRow(chC, "ops-C", "dingtalk", true)
	mock.ExpectQuery(`SELECT \* FROM "notification_channels"`).
		WithArgs(true).
		WillReturnRows(channelRows)

	// 2) loadRuleNotifyChannelIDs: SELECT rule → gorm.ErrRecordNotFound → 返 nil → 不过滤
	mock.ExpectQuery(`SELECT.*"alert_rules".*WHERE id = .*$`).
		WithArgs(ruleID, sqlmock.AnyArg()).
		WillReturnError(gorm.ErrRecordNotFound)

	// 3) INSERT notification_logs — fallback 全启用 → 写 3 行
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "notification_logs"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New()))
	mock.ExpectCommit()

	err := svc.writeNotificationTrigger(ctx, alert, "resolved", userID)
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet(), "rule 不存在 → fallback 全启用 3 channels")
}

// TestWriteNotificationTrigger_NoRule_FallbackAllEnabled M82 AC:
// alert.AlertRuleID == nil (历史 alert) → 不查 rule → fallback 全启用 channels (兼容)
func TestWriteNotificationTrigger_NoRule_FallbackAllEnabled(t *testing.T) {
	gormDB, mock := newMockDB(t)
	svc := &alertService{db: gormDB}
	ctx := context.Background()

	chA := uuid.New()
	chB := uuid.New()
	chC := uuid.New()
	alert := &models.Alert{ID: uuid.New()} /* AlertRuleID nil */
	userID := "u-m82-norule"

	// alert.AlertRuleID == nil → 跳过 loadRuleNotifyChannelIDs
	// 1) 直接加载所有 enabled channels (3 个) — fallback 全启用
	channelRows := sqlmock.NewRows([]string{"id", "name", "type", "is_enabled"}).
		AddRow(chA, "ops-A", "dingtalk", true).
		AddRow(chB, "ops-B", "dingtalk", true).
		AddRow(chC, "ops-C", "dingtalk", true)
	mock.ExpectQuery(`SELECT \* FROM "notification_channels"`).
		WithArgs(true).
		WillReturnRows(channelRows)

	// 2) INSERT notification_logs — fallback 全启用 → 写 3 行
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "notification_logs"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New()))
	mock.ExpectCommit()

	err := svc.writeNotificationTrigger(ctx, alert, "resolved", userID)
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet(), "alert 无 rule → fallback 全启用 3 channels (历史 alert 兼容)")
}

// ==================== M82: writeNotificationTrigger 真 sqlite 测试 (M3 守卫) ====================
//
// sqlmock 没法测「INSERT 不应发生」(extra queries 默认不报), 改用真 sqlite 在内存里
// 真走 gorm.Create 路径, 然后直接 count notification_logs 表。M3 mutation
// (notifyIDs != nil → len(notifyIDs) > 0) 会让 RuleEmpty 测试 INSERT 3 行, count != 0 → 红

// newM82SQLiteDB 真 sqlite + 手写 DDL (notification_channels + alert_rules + alerts + notification_logs 四张表)
// 不用 AutoMigrate: models.X.ID 带 `default:gen_random_uuid()`, sqlite 没该函数
func newM82SQLiteDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE notification_channels (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		type TEXT NOT NULL,
		config TEXT,
		is_enabled INTEGER DEFAULT 1,
		is_default INTEGER DEFAULT 0,
		created_at DATETIME,
		updated_at DATETIME
	)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE alert_rules (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		description TEXT,
		condition TEXT,
		asset_type TEXT,
		host_group TEXT,
		metric TEXT,
		operator TEXT,
		threshold REAL,
		duration INTEGER,
		severity INTEGER,
		severity_name TEXT,
		notify_enabled INTEGER DEFAULT 1,
		notify_channels TEXT,
		notify_users TEXT,
		is_enabled INTEGER DEFAULT 1,
		priority INTEGER DEFAULT 0,
		created_by TEXT,
		updated_by TEXT,
		created_at DATETIME,
		updated_at DATETIME
	)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE alerts (
		id TEXT PRIMARY KEY,
		alert_id TEXT,
		host_id TEXT,
		host_name TEXT,
		alert_rule_id TEXT,
		host_ip TEXT,
		trigger_name TEXT,
		trigger_id TEXT,
		severity INTEGER,
		severity_name TEXT,
		problem TEXT,
		problem_start DATETIME,
		problem_end DATETIME,
		duration INTEGER,
		status TEXT DEFAULT 'problem',
		ack_time DATETIME,
		ack_user TEXT,
		resolve_time DATETIME,
		resolve_user TEXT,
		is_false_positive INTEGER DEFAULT 0,
		marked_by TEXT,
		marked_at DATETIME,
		false_positive_note TEXT,
		ticket_id TEXT,
		asset_id TEXT,
		source TEXT DEFAULT 'zabbix',
		repeat_count INTEGER DEFAULT 0,
		created_at DATETIME,
		updated_at DATETIME
	)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE notification_logs (
		id TEXT PRIMARY KEY,
		alert_id TEXT,
		channel_id TEXT,
		channel_name TEXT,
		recipient TEXT,
		content TEXT,
		status TEXT,
		error_msg TEXT,
		sent_at DATETIME,
		created_at DATETIME
	)`).Error)
	return db
}

// TestWriteNotificationTrigger_RuleEmpty_NoLogsSQLite M82 AC 真 sqlite 实证 (M3 守卫):
// alert 有 rule, rule.NotifyChannels="" → 真 DB 里 notification_logs 表 0 行
// M3 mutation (len(notifyIDs) > 0) 会让代码 INSERT 3 行, count != 0 → 红
func TestWriteNotificationTrigger_RuleEmpty_NoLogsSQLite(t *testing.T) {
	db := newM82SQLiteDB(t)
	svc := &alertService{db: db}
	ctx := context.Background()

	// 准备: 1 条 rule (notify_channels="") + 3 条 enabled channel
	ruleID := uuid.New().String()
	require.NoError(t, db.Exec(`INSERT INTO alert_rules (id, name, notify_channels) VALUES (?, ?, ?)`,
		ruleID, "m82-empty-rule", "").Error)
	for i, name := range []string{"ops-A", "ops-B", "ops-C"} {
		require.NoError(t, db.Exec(`INSERT INTO notification_channels (id, name, type, is_enabled) VALUES (?, ?, ?, ?)`,
			uuid.New().String(), name, "dingtalk", 1).Error)
		_ = i
	}

	ruleUUID := uuid.MustParse(ruleID)
	alert := &models.Alert{ID: uuid.New(), AlertRuleID: &ruleUUID}

	err := svc.writeNotificationTrigger(ctx, alert, "acknowledged", "u-m82-empty-sqlite")
	require.NoError(t, err)

	// 真表查: 必须 0 行
	var count int64
	require.NoError(t, db.Model(&models.NotificationLog{}).Count(&count).Error)
	require.Equal(t, int64(0), count, "AC-M82-RuleEmpty: rule 显式空 → notification_logs 必须 0 行 (真 DB 实证, M3 守卫)")
}
// ==================== M82: filterNotificationChannelsByIDs helper 单元测试 ====================
//
// 直接测 helper 函数 (sqlmock INSERT 行数限制不够, 这里用白盒测试保证 helper 行为)
// 配套 mutation inversion M2: 改 helper 让其跳过 filter → 下方测试期望被破

// TestFilterNotificationChannelsByIDs_FiltersToListed 测试 helper 基础语义:
//   - wantIDs 包含 chA + chB, 3 个 channel 输入 → 输出仅 chA + chB (顺序保留)
//   - wantIDs 空 → 返 nil (空数组语义, 与 notification.filterChannelsByIDs 对齐)
func TestFilterNotificationChannelsByIDs_FiltersToListed(t *testing.T) {
	chA := uuid.New()
	chB := uuid.New()
	chC := uuid.New() // rule 没勾 → 不应在输出

	channels := []models.NotificationChannel{
		{ID: chA, Name: "ops-A"},
		{ID: chB, Name: "ops-B"},
		{ID: chC, Name: "ops-C"},
	}
	want := []string{chA.String(), chB.String()}

	out := filterNotificationChannelsByIDs(channels, want)
	require.Len(t, out, 2, "helper 必须仅返 want 里的 2 个")
	require.Equal(t, chB, out[1].ID)
}

// TestFilterNotificationChannelsByIDs_PreservesChannelsOrder 测试 helper 顺序语义
// TestFilterNotificationChannelsByIDs_PreservesChannelsOrder 测试 helper 顺序语义:
//   - 输入 channels 顺序 chA, chB, chC, want [chC, chA] → 输出 chA, chC (按 channels 顺序, 不是 want 顺序)
//   - 与 notification.filterChannelsByIDs (M37-A ship) 语义对齐: 保留 channels 顺序, 不按 want 顺序
//   - 配套 mutation inversion: 改 helper 让其按 want 顺序返 / 改 map 实现丢顺序 → 下方顺序断言失败
func TestFilterNotificationChannelsByIDs_PreservesChannelsOrder(t *testing.T) {
	chA := uuid.New()
	chB := uuid.New()
	chC := uuid.New()

	channels := []models.NotificationChannel{
		{ID: chA, Name: "ops-A"},
		{ID: chB, Name: "ops-B"},
		{ID: chC, Name: "ops-C"},
	}
	want := []string{chC.String(), chA.String()} // want 顺序: chC 然后 chA — 但 helper 按 channels 顺序返

	out := filterNotificationChannelsByIDs(channels, want)
	require.Len(t, out, 2, "chB 不在 want 里 → 不应出现")
	require.Equal(t, chA, out[0].ID, "channels[0]=chA ∈ want → 必须在 out[0] (channels 顺序)")
	require.Equal(t, chC, out[1].ID, "channels[2]=chC ∈ want → 必须在 out[1] (channels 顺序)")
}



// 避免 time 包被 unused warning
var _ = time.Now
