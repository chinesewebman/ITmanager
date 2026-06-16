package service

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"network-monitor-platform/internal/models"

	"github.com/google/uuid"
	"github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// init 注册带 gen_random_uuid() 的 sqlite3 driver
// 跟其他包 test 同步约定
func init() {
	sql.Register("sqlite3_uuid", &sqlite3.SQLiteDriver{
		ConnectHook: func(conn *sqlite3.SQLiteConn) error {
			return conn.RegisterFunc("gen_random_uuid", func() string {
				return uuid.New().String()
			}, true)
		},
	})
}

// newDiagTestDB 创建一个 4 张表的 sqlite 内存 DB
// 覆盖 diagnostic 端点用到的 4 张表：assets / alerts / tickets / asset_networks
// schema 手写跟 production 一致，uuid 用 text
func newDiagTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	stmts := []string{
		`CREATE TABLE assets (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			asset_tag TEXT,
			sn TEXT,
			asset_type TEXT,
			brand TEXT,
			model TEXT,
			site_id TEXT,
			site_name TEXT,
			rack_id TEXT,
			rack_name TEXT,
			rack_position TEXT,
			purchase_date DATETIME,
			warranty_end DATETIME,
			vendor TEXT,
			vendor_contact TEXT,
			status TEXT DEFAULT 'active',
			online_time DATETIME,
			offline_time DATETIME,
			business_unit TEXT,
			service_name TEXT,
			tags TEXT,
			custom_fields TEXT,
			net_box_id INTEGER,
			source TEXT,
			created_at DATETIME,
			updated_at DATETIME
		)`,
		`CREATE TABLE alerts (
			id TEXT PRIMARY KEY,
			alert_id TEXT,
			host_id TEXT,
			host_name TEXT,
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
			ticket_id TEXT,
			asset_id TEXT,
			source TEXT,
			repeat_count INTEGER,
			created_at DATETIME,
			updated_at DATETIME
		)`,
		`CREATE TABLE tickets (
			id TEXT PRIMARY KEY,
			ticket_number TEXT,
			title TEXT,
			description TEXT,
			ticket_type TEXT,
			priority TEXT,
			status TEXT DEFAULT 'open',
			requester_id TEXT,
			requester_name TEXT,
			requester_email TEXT,
			assignee_id TEXT,
			assignee_name TEXT,
			category TEXT,
			tags TEXT,
			asset_id TEXT,
			asset_name TEXT,
			external_id TEXT,
			source TEXT,
			resolution TEXT,
			resolved_at DATETIME,
			closed_at DATETIME,
			due_date DATETIME,
			created_at DATETIME,
			updated_at DATETIME
		)`,
		`CREATE TABLE asset_networks (
			id TEXT PRIMARY KEY,
			asset_id TEXT,
			interface_name TEXT,
			interface_type TEXT,
			mac_address TEXT,
			ipv4_address TEXT,
			ipv4_netmask TEXT,
			ipv_address TEXT,
			speed INTEGER,
			duplex TEXT,
			status TEXT,
			connected_to TEXT,
			connected_port TEXT,
			purpose TEXT,
			created_at DATETIME,
			updated_at DATETIME
		)`,
	}
	for _, s := range stmts {
		require.NoError(t, db.Exec(s).Error)
	}
	return db
}

// seedAsset 创建一个资产并返回 ID
func seedAsset(t *testing.T, db *gorm.DB, name string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	now := time.Now().UTC()
	err := db.Exec(`INSERT INTO assets
		(id, name, asset_type, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		id, name, "server", "active", now, now).Error
	require.NoError(t, err)
	return id
}

// seedAlert 创建一个告警
func seedAlert(t *testing.T, db *gorm.DB, assetID uuid.UUID, severity int, status string, problemStart, ackTime, resolveTime *time.Time) uuid.UUID {
	t.Helper()
	id := uuid.New()
	now := time.Now().UTC()
	err := db.Exec(`INSERT INTO alerts
		(id, host_id, host_name, trigger_name, severity, severity_name, problem, problem_start,
		 status, ack_time, ack_user, resolve_time, resolve_user, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, assetID, "host-1", "CPU 100%", severity, "High", "CPU over 90%",
		problemStart, status, ackTime, "ops", resolveTime, "ops", now, now).Error
	require.NoError(t, err)
	return id
}

// seedTicket 创建一个工单
func seedTicket(t *testing.T, db *gorm.DB, assetID uuid.UUID, status string, createdAt, resolvedAt, closedAt *time.Time) uuid.UUID {
	t.Helper()
	id := uuid.New()
	err := db.Exec(`INSERT INTO tickets
		(id, ticket_number, title, status, asset_id, assignee_name, created_at, updated_at, resolved_at, closed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, "T-001", "服务异常", status, assetID, "ops", createdAt, createdAt, resolvedAt, closedAt).Error
	require.NoError(t, err)
	return id
}

// seedLink 创建一个网卡
func seedLink(t *testing.T, db *gorm.DB, assetID uuid.UUID, status string, updatedAt time.Time) uuid.UUID {
	t.Helper()
	id := uuid.New()
	err := db.Exec(`INSERT INTO asset_networks
		(id, asset_id, interface_name, status, updated_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		id, assetID, "eth0", status, updatedAt, updatedAt).Error
	require.NoError(t, err)
	return id
}

// ==================== DiagnosticService 测试 ====================

func TestDiagnosticService_GetTimeline_资产不存在返回ErrNotFound(t *testing.T) {
	db := newDiagTestDB(t)
	svc := NewDiagnosticService(db)

	_, err := svc.GetTimeline(context.Background(), uuid.New(), DiagnosticFilter{})
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestDiagnosticService_GetTimeline_空数据返回空事件流(t *testing.T) {
	db := newDiagTestDB(t)
	assetID := seedAsset(t, db, "test-server")
	svc := NewDiagnosticService(db)

	got, err := svc.GetTimeline(context.Background(), assetID, DiagnosticFilter{})
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, assetID, got.Asset.ID)
	assert.Equal(t, "test-server", got.Asset.Name)
	assert.Equal(t, "server", got.Asset.AssetType)
	assert.Empty(t, got.Events, "无事件时 events 应为空")
	assert.Equal(t, int64(0), got.Summary.AlertCount)
	assert.Equal(t, int64(0), got.Summary.TicketCount)
	assert.Equal(t, int64(0), got.Summary.OpenAlerts)
	assert.Nil(t, got.Summary.MTTRSeconds, "无 resolved alert 时 MTTR 应为 nil")
}

func TestDiagnosticService_GetTimeline_告警三态事件triggered_ack_resolved(t *testing.T) {
	db := newDiagTestDB(t)
	assetID := seedAsset(t, db, "alert-server")
	now := time.Now().UTC()
	problemStart := now.Add(-1 * time.Hour)
	ackTime := now.Add(-50 * time.Minute)
	resolveTime := now.Add(-30 * time.Minute)
	seedAlert(t, db, assetID, 4, "resolved", &problemStart, &ackTime, &resolveTime)
	svc := NewDiagnosticService(db)

	got, err := svc.GetTimeline(context.Background(), assetID, DiagnosticFilter{})
	require.NoError(t, err)
	require.Len(t, got.Events, 3, "应有 3 个事件：triggered + acknowledged + resolved")

	// 顺序：ts 倒序
	assert.Equal(t, "resolved", got.Events[0].SubKind)
	assert.Equal(t, "acknowledged", got.Events[1].SubKind)
	assert.Equal(t, "triggered", got.Events[2].SubKind)
	assert.Equal(t, models.TimelineEventAlert, got.Events[0].Kind)

	// triggered 有 severity
	assert.Equal(t, 4, got.Events[2].Severity)
	assert.Equal(t, "CPU 100%", got.Events[2].Title)
}

func TestDiagnosticService_GetTimeline_MTTR计算正确(t *testing.T) {
	db := newDiagTestDB(t)
	assetID := seedAsset(t, db, "mttr-server")
	now := time.Now().UTC()

	// 3 个 resolved alert，分别耗时 1h / 2h / 3h
	for _, mins := range []int{60, 120, 180} {
		problemStart := now.Add(-24 * time.Hour) // 落入窗口
		resolveTime := problemStart.Add(time.Duration(mins) * time.Minute)
		ackTime := resolveTime.Add(-time.Minute)
		seedAlert(t, db, assetID, 3, "resolved", &problemStart, &ackTime, &resolveTime)
	}
	svc := NewDiagnosticService(db)

	got, err := svc.GetTimeline(context.Background(), assetID, DiagnosticFilter{})
	require.NoError(t, err)
	require.NotNil(t, got.Summary.MTTRSeconds)
	// (3600 + 7200 + 10800) / 3 = 7200 秒 = 2 小时
	assert.Equal(t, int64(7200), *got.Summary.MTTRSeconds, "MTTR 应为 7200 秒 (2 小时)")
	assert.Equal(t, int64(3), got.Summary.AlertCount)
	assert.Equal(t, int64(0), got.Summary.OpenAlerts, "全部 resolved，open 应为 0")
}

func TestDiagnosticService_GetTimeline_工单三事件created_resolved_closed(t *testing.T) {
	db := newDiagTestDB(t)
	assetID := seedAsset(t, db, "ticket-server")
	now := time.Now().UTC()
	createdAt := now.Add(-2 * time.Hour)
	resolvedAt := now.Add(-1 * time.Hour)
	closedAt := now.Add(-30 * time.Minute)
	seedTicket(t, db, assetID, "closed", &createdAt, &resolvedAt, &closedAt)
	svc := NewDiagnosticService(db)

	got, err := svc.GetTimeline(context.Background(), assetID, DiagnosticFilter{})
	require.NoError(t, err)
	require.Len(t, got.Events, 3, "应有 3 个工单事件")
	assert.Equal(t, "closed", got.Events[0].SubKind)
	assert.Equal(t, "resolved", got.Events[1].SubKind)
	assert.Equal(t, "created", got.Events[2].SubKind)
	assert.Equal(t, models.TimelineEventTicket, got.Events[0].Kind)
	assert.Equal(t, int64(1), got.Summary.TicketCount)
	assert.Equal(t, int64(0), got.Summary.OpenTickets)
}

func TestDiagnosticService_GetTimeline_网卡状态变更事件(t *testing.T) {
	db := newDiagTestDB(t)
	assetID := seedAsset(t, db, "link-server")
	now := time.Now().UTC()
	seedLink(t, db, assetID, "down", now.Add(-15*time.Minute))
	seedLink(t, db, assetID, "up", now.Add(-5*time.Minute))
	svc := NewDiagnosticService(db)

	got, err := svc.GetTimeline(context.Background(), assetID, DiagnosticFilter{})
	require.NoError(t, err)
	require.Len(t, got.Events, 2)
	assert.Equal(t, models.TimelineEventLink, got.Events[0].Kind)
	assert.Equal(t, "up", got.Events[0].SubKind)
	assert.Equal(t, int64(1), got.Summary.LinkDownCount, "1 个 link down")
}

func TestDiagnosticService_GetTimeline_事件按ts倒序(t *testing.T) {
	db := newDiagTestDB(t)
	assetID := seedAsset(t, db, "sort-server")
	now := time.Now().UTC()
	// 故意打乱时间
	seedAlert(t, db, assetID, 3, "problem",
		ptrTime(now.Add(-3*time.Hour)), nil, nil)
	seedAlert(t, db, assetID, 3, "problem",
		ptrTime(now.Add(-1*time.Hour)), nil, nil)
	seedAlert(t, db, assetID, 3, "problem",
		ptrTime(now.Add(-2*time.Hour)), nil, nil)
	svc := NewDiagnosticService(db)

	got, err := svc.GetTimeline(context.Background(), assetID, DiagnosticFilter{})
	require.NoError(t, err)
	require.Len(t, got.Events, 3)
	// 倒序：-1h, -2h, -3h
	assert.True(t, got.Events[0].TS.After(got.Events[1].TS))
	assert.True(t, got.Events[1].TS.After(got.Events[2].TS))
}

func TestDiagnosticService_GetTimeline_DaysLimit边界(t *testing.T) {
	db := newDiagTestDB(t)
	assetID := seedAsset(t, db, "limit-server")
	now := time.Now().UTC()

	// 在窗口外的告警（45 天前）应该被滤掉
	outside := now.Add(-45 * 24 * time.Hour)
	seedAlert(t, db, assetID, 3, "problem", &outside, nil, nil)
	// 窗口内（10 天前）应该保留
	inside := now.Add(-10 * 24 * time.Hour)
	seedAlert(t, db, assetID, 3, "problem", &inside, nil, nil)
	svc := NewDiagnosticService(db)

	// 默认 30 天窗口
	got, err := svc.GetTimeline(context.Background(), assetID, DiagnosticFilter{})
	require.NoError(t, err)
	assert.Len(t, got.Events, 1, "45 天前应被 30 天窗口滤掉")
	assert.Equal(t, int64(1), got.Summary.AlertCount)

	// 60 天窗口 → 两条都看到
	got, err = svc.GetTimeline(context.Background(), assetID, DiagnosticFilter{Days: 60})
	require.NoError(t, err)
	assert.Len(t, got.Events, 2, "60 天窗口应包含 45 天前告警")
}

func TestDiagnosticService_GetTimeline_Limit截断事件(t *testing.T) {
	db := newDiagTestDB(t)
	assetID := seedAsset(t, db, "trunc-server")
	now := time.Now().UTC()
	// 创建 10 个 ack 告警 → 20 个事件
	for i := 0; i < 10; i++ {
		problemStart := now.Add(-time.Duration(10+i) * time.Minute)
		ackTime := now.Add(-time.Duration(5+i) * time.Minute)
		seedAlert(t, db, assetID, 3, "acknowledged", &problemStart, &ackTime, nil)
	}
	svc := NewDiagnosticService(db)

	got, err := svc.GetTimeline(context.Background(), assetID, DiagnosticFilter{Limit: 5})
	require.NoError(t, err)
	assert.Len(t, got.Events, 5, "limit=5 应截断到 5 条")
}

func TestDiagnosticService_GetTimeline_参数clamp_最大365天1000事件(t *testing.T) {
	db := newDiagTestDB(t)
	assetID := seedAsset(t, db, "clamp-server")
	svc := NewDiagnosticService(db)

	// 传 days=99999, limit=99999 → 内部 clamp 到 365/1000
	got, err := svc.GetTimeline(context.Background(), assetID, DiagnosticFilter{Days: 99999, Limit: 99999})
	require.NoError(t, err)
	assert.Equal(t, 365, got.Summary.WindowDays, "days 应 clamp 到 365")
}

func ptrTime(t time.Time) *time.Time { return &t }
