package main

import (
	"database/sql"
	"embed"
	"io/fs"
	"testing"

	"network-monitor-platform/internal/models"

	"github.com/google/uuid"
	"github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// init 注册带 gen_random_uuid() 的 sqlite3 driver
func init() {
	sql.Register("sqlite3_uuid", &sqlite3.SQLiteDriver{
		ConnectHook: func(conn *sqlite3.SQLiteConn) error {
			return conn.RegisterFunc("gen_random_uuid", func() string {
				return uuid.New().String()
			}, true)
		},
	})
}

//go:embed all:seed-testdata/migrations/*.sql
var seedTestMigrations embed.FS

// seedFS 包装 embed.FS 让 .sql 文件直接暴露在根（"migrations/xxx.sql"）
type seedFS struct{ embed.FS }

func (s seedFS) Open(name string) (fs.File, error) {
	return s.FS.Open("seed-testdata/" + name)
}
func (s seedFS) ReadDir(name string) ([]fs.DirEntry, error) {
	return s.FS.ReadDir("seed-testdata/" + name)
}
func (s seedFS) ReadFile(name string) ([]byte, error) {
	return s.FS.ReadFile("seed-testdata/" + name)
}

// newTestDB 开 :memory: sqlite + 跑手写 schema（覆盖 seedData 调用的所有 model）
// 手写跟 gorm AutoMigrate 行为对齐：uuid 用 text，所有 model 字段都覆盖
func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	// 9 张表，跟 model 一一对应
	stmts := []string{
		`CREATE TABLE users (
			id TEXT PRIMARY KEY,
			username TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			nickname TEXT,
			email TEXT,
			phone TEXT,
			avatar TEXT,
			department_id TEXT,
			status TEXT DEFAULT 'active',
			role TEXT DEFAULT 'user',
			failed_login INTEGER DEFAULT 0,
			locked_until DATETIME,
			last_login DATETIME,
			last_login_ip TEXT,
			created_at DATETIME,
			updated_at DATETIME,
			deleted_at DATETIME
		)`,
		`CREATE TABLE sites (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			code TEXT,
			province TEXT,
			city TEXT,
			address TEXT,
			contact TEXT,
			contact_phone TEXT,
			tier TEXT,
			is_active INTEGER DEFAULT 1,
			net_box_id INTEGER,
			created_at DATETIME,
			updated_at DATETIME,
			deleted_at DATETIME
		)`,
		`CREATE TABLE racks (
			id TEXT PRIMARY KEY,
			site_id TEXT,
			site_name TEXT,
			name TEXT NOT NULL,
			total_u INTEGER DEFAULT 42,
			max_weight INTEGER,
			floor TEXT,
			row TEXT,
			column TEXT,
			status TEXT DEFAULT 'active',
			net_box_id INTEGER,
			created_at DATETIME,
			updated_at DATETIME,
			deleted_at DATETIME
		)`,
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
			updated_at DATETIME,
			deleted_at DATETIME
		)`,
		`CREATE TABLE asset_networks (
			id TEXT PRIMARY KEY,
			asset_id TEXT NOT NULL,
			interface_name TEXT,
			interface_type TEXT,
			mac_address TEXT,
			ipv4_address TEXT,
			ipv4_netmask TEXT,
			ipv6_address TEXT,
			speed INTEGER,
			duplex TEXT,
			status TEXT,
			connected_to TEXT,
			connected_port TEXT,
			purpose TEXT,
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
			is_false_positive INTEGER DEFAULT 0,
			marked_by TEXT,
			marked_at DATETIME,
			false_positive_note TEXT,
			ticket_id TEXT,
			asset_id TEXT,
			source TEXT,
			repeat_count INTEGER DEFAULT 0,
			created_at DATETIME,
			updated_at DATETIME
		)`,
		`CREATE TABLE alert_rules (
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
			notify_enabled INTEGER,
			notify_channels TEXT,
			notify_users TEXT,
			is_enabled INTEGER,
			priority INTEGER,
			created_by TEXT,
			updated_by TEXT,
			created_at DATETIME,
			updated_at DATETIME,
			deleted_at DATETIME
		)`,
		`CREATE TABLE tickets (
			id TEXT PRIMARY KEY,
			ticket_number TEXT,
			title TEXT,
			description TEXT,
			ticket_type TEXT,
			priority TEXT,
			status TEXT,
			requester_id TEXT,
			requester_name TEXT,
			requester_email TEXT,
			assignee_id TEXT,
			assignee_name TEXT,
			category TEXT,
			asset_id TEXT,
			asset_name TEXT,
			external_id TEXT,
			source TEXT,
			resolution TEXT,
			resolved_at DATETIME,
			closed_at DATETIME,
			due_date DATETIME,
			tags TEXT,
			created_at DATETIME,
			updated_at DATETIME,
			deleted_at DATETIME
		)`,
		`CREATE TABLE notification_channels (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			type TEXT,
			config TEXT,
			is_enabled INTEGER,
			is_default INTEGER,
			created_at DATETIME,
			updated_at DATETIME,
			deleted_at DATETIME
		)`,
	}
	for _, s := range stmts {
		require.NoError(t, db.Exec(s).Error)
	}
	return db
}

// ==================== seedData 测试 ====================

func TestSeed_空DB_创建默认用户(t *testing.T) {
	db := newTestDB(t)
	seedData(db)

	var users []models.User
	require.NoError(t, db.Find(&users).Error)
	assert.GreaterOrEqual(t, len(users), 3, "应有 admin + operator + viewer")

	names := map[string]bool{}
	for _, u := range users {
		names[u.Username] = true
	}
	assert.True(t, names["admin"], "admin 用户应创建")
	assert.True(t, names["operator"], "operator 用户应创建")
	assert.True(t, names["viewer"], "viewer 用户应创建")
}

func TestSeed_admin密码为admin123_bcrypt加密(t *testing.T) {
	db := newTestDB(t)
	seedData(db)

	var admin models.User
	require.NoError(t, db.Where("username = ?", "admin").First(&admin).Error)
	assert.True(t, len(admin.PasswordHash) > 50, "bcrypt hash 长度应 > 50")
	assert.NotEqual(t, "admin123", admin.PasswordHash, "不应明文存密码")
}

func TestSeed_已有users_跳过userSeed分支(t *testing.T) {
	db := newTestDB(t)
	// 预创建 1 个 user（userCount != 0 → seedData 整个 user 分支跳过）
	existing := models.User{
		ID:           uuid.New(),
		Username:     "pre_existing",
		PasswordHash: "hashed",
		Status:       "active",
		Role:         "user",
	}
	require.NoError(t, db.Create(&existing).Error)

	seedData(db)

	// users 总数应 = 1（只有 pre_existing）—— seed 跳过 user 分支
	var count int64
	db.Model(&models.User{}).Count(&count)
	assert.Equal(t, int64(1), count, "已有 user 时 seed 应跳过 user 分支")
}

func TestSeed_空DB_创建3个site(t *testing.T) {
	db := newTestDB(t)
	seedData(db)

	var sites []models.Site
	require.NoError(t, db.Find(&sites).Error)
	assert.Equal(t, 3, len(sites), "应有 3 个 site")

	names := map[string]bool{}
	for _, s := range sites {
		names[s.Name] = true
	}
	assert.True(t, names["北京数据中心A"])
	assert.True(t, names["上海数据中心B"])
	assert.True(t, names["广州数据中心C"])
}

func TestSeed_空DB_创建告警(t *testing.T) {
	db := newTestDB(t)
	seedData(db)

	var alerts []models.Alert
	require.NoError(t, db.Find(&alerts).Error)
	assert.Equal(t, 6, len(alerts), "应有 6 条告警")

	// severity_name 字段有不同级别
	sevNames := map[string]bool{}
	for _, a := range alerts {
		sevNames[a.SeverityName] = true
	}
	assert.GreaterOrEqual(t, len(sevNames), 4, "至少 4 个不同 severity_name")

	// alert_id/host_name/trigger_name 都填了
	for _, a := range alerts {
		assert.NotEmpty(t, a.AlertID, "AlertID 必填")
		assert.NotEmpty(t, a.HostName, "HostName 必填")
		assert.NotEmpty(t, a.TriggerName, "TriggerName 必填")
		assert.NotEmpty(t, a.Source, "Source 必填")
	}
}

func TestSeed_空DB_创建告警规则(t *testing.T) {
	db := newTestDB(t)
	seedData(db)

	var rules []models.AlertRule
	require.NoError(t, db.Find(&rules).Error)
	assert.Equal(t, 5, len(rules), "应有 5 条告警规则")
}

func TestSeed_空DB_创建通知渠道(t *testing.T) {
	db := newTestDB(t)
	seedData(db)

	var channels []models.NotificationChannel
	require.NoError(t, db.Find(&channels).Error)
	assert.Equal(t, 3, len(channels), "应有 3 个通知渠道 (email/dingtalk/webhook)")

	types := map[string]bool{}
	for _, c := range channels {
		types[c.Type] = true
	}
	assert.True(t, types["email"])
	assert.True(t, types["dingtalk"])
	assert.True(t, types["webhook"])
}

func TestSeed_空DB_创建工单(t *testing.T) {
	db := newTestDB(t)
	seedData(db)

	var tickets []models.Ticket
	require.NoError(t, db.Find(&tickets).Error)
	assert.Equal(t, 4, len(tickets), "应有 4 个工单")
}

func TestSeed_已有site_跳过site_下游asset不创建(t *testing.T) {
	db := newTestDB(t)
	preSite := models.Site{
		Name:     "已存在的机房",
		Code:     "DC-EXIST-01",
		IsActive: true,
	}
	require.NoError(t, db.Create(&preSite).Error)

	seedData(db)

	var count int64
	db.Model(&models.Site{}).Count(&count)
	assert.Equal(t, int64(1), count, "不应重复 seed 已有 site")

	// 既有 site 时不创建 assets
	var assetCount int64
	db.Model(&models.Asset{}).Count(&assetCount)
	assert.Equal(t, int64(0), assetCount, "跳过 site 分支时不应创建任何 asset")
}

func TestSeed_跑两次_幂等性_users数不变(t *testing.T) {
	db := newTestDB(t)
	seedData(db)
	var count1 int64
	db.Model(&models.User{}).Count(&count1)

	seedData(db)

	var count2 int64
	db.Model(&models.User{}).Count(&count2)
	assert.Equal(t, count1, count2, "seed 跑两次 users 数应不变")
}

func TestSeed_不panic_空DB再跑一次(t *testing.T) {
	db := newTestDB(t)
	assert.NotPanics(t, func() { seedData(db) }, "seed 跑一次不应 panic")
	assert.NotPanics(t, func() { seedData(db) }, "seed 跑两次不应 panic")
}

func TestSeed_生成MAC格式正确(t *testing.T) {
	mac := generateMAC()
	// 形如 XX:XX:XX:XX:XX:XX（6 段 16 进制）
	assert.Regexp(t, `^[0-9A-F]{2}(:[0-9A-F]{2}){5}$`, mac)
}
