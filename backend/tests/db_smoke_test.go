//go:build dbsmoke

// 真 Postgres 冒烟测试 —— 用「迁移建的库」跑核心链路。
//
// 由 scripts/db_smoke.sh 驱动(它会起临时容器、跑 migrations/*.up.sql、再跑本文件)。
// 手动运行: 先备好一个用迁移建库的 PG, 再
//
//	cd backend && TEST_DATABASE_URL='postgres://user:pass@host:port/db?sslmode=disable' \
//	  go test -tags dbsmoke -v -run TestDBSmoke ./tests/
//
// 没设 TEST_DATABASE_URL 时全部 skip, 所以不会影响普通 `go test`。
//
// 设计意图: 失败就是信号 —— 每个断言直接暴露 models/*.go 与 backend/migrations/*.up.sql
// 的 schema 漂移(docs/v3-架构优化需求.md §9 D-1/D-4/D-5), 并原样打印 Postgres 报错。
package tests

import (
	"os"
	"strings"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	network_monitor_platform "network-monitor-platform"
	"network-monitor-platform/internal/migrate"
	"network-monitor-platform/internal/models"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// openSmokeDB 连接 TEST_DATABASE_URL 指向的库; 未设置则 skip。
func openSmokeDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL 未设置 —— 跳过真 Postgres 冒烟测试(由 scripts/db_smoke.sh 驱动)")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("连接 Postgres 失败 [%s]:\n%v", maskDSN(dsn), err)
	}
	return db
}

// maskDSN 打日志时隐藏密码。
func maskDSN(dsn string) string {
	if at := strings.LastIndex(dsn, "@"); at >= 0 {
		if colon := strings.Index(dsn, "://"); colon >= 0 {
			return dsn[:colon+3] + "***" + dsn[at:]
		}
	}
	return dsn
}

// TestDBSmoke_MigrateRunner 用生产迁移执行器(migrate.Up + embed 的真实 migrations)
// 在空库上建库, 断言 13 个迁移全部应用成功。
//
// 为什么单独立一个用例: 早先的冒烟脚本用 psql 逐文件喂 SQL, 绕过了 internal/migrate
// 的语句切分(execInTx → splitStatements), 于是 `DO $$ ... $$` 块在真生产路径上会失败
// 而冒烟仍是绿的(假绿)。这里走与 cmd/server 完全相同的代码路径。
func TestDBSmoke_MigrateRunner(t *testing.T) {
	db := openSmokeDB(t)

	// 必须空库, 否则无法证明 schema 是本次 migrate.Up 建出来的
	var existing int64
	if err := db.Raw(
		`SELECT count(*) FROM information_schema.tables WHERE table_schema = 'public'`,
	).Scan(&existing).Error; err != nil {
		t.Fatalf("探测空库失败:\n%v", err)
	}
	if existing > 0 {
		t.Fatalf("public schema 非空(%d 张表) —— 该用例要求空库; scripts/db_smoke.sh 每次起新容器", existing)
	}

	migrate.FS = network_monitor_platform.MigrationsFS
	if err := migrate.Up(db); err != nil {
		t.Fatalf("migrate.Up 失败 —— 生产迁移路径不可用:\n%v", err)
	}

	var applied int64
	if err := db.Raw(`SELECT count(*) FROM schema_migrations`).Scan(&applied).Error; err != nil {
		t.Fatalf("读取 schema_migrations 失败:\n%v", err)
	}
	t.Logf("✅ 迁移执行器跑通: 已应用 %d 个迁移", applied)

	// 抽查漂移修复过的关键列(模型要、旧迁移缺)
	checks := []struct{ table, col string }{
		{"users", "role"}, {"users", "deleted_at"},
		{"tickets", "ticket_number"}, {"tickets", "requester_id"},
		{"audit_logs", "created_at"}, {"audit_logs", "method"},
		{"alerts", "ticket_id"}, {"assets", "site_id"},
		{"notification_channels", "is_enabled"}, {"api_keys", "permissions"},
	}
	for _, c := range checks {
		var exists bool
		if err := db.Raw(
			`SELECT EXISTS (SELECT 1 FROM information_schema.columns
			 WHERE table_name = ? AND column_name = ?)`, c.table, c.col,
		).Scan(&exists).Error; err != nil {
			t.Fatalf("探测列 %s.%s 失败:\n%v", c.table, c.col, err)
		}
		if !exists {
			t.Errorf("迁移执行后仍缺列 %s.%s —— 模型与迁移漂移", c.table, c.col)
		}
	}
}

// TestDBSmoke_LoginQuery 复刻登录查询: 按 models.User 查一条用户。
// 漂移点: 迁移 users 表缺 role / deleted_at, 而 GORM 会枚举模型全部列并附加
// `deleted_at IS NULL` → 列不存在 (D-4)。
func TestDBSmoke_LoginQuery(t *testing.T) {
	db := openSmokeDB(t)

	const username = "smoke_login_probe"
	// 用迁移里真实存在的列插一条最小用户(不碰 role/deleted_at —— 迁移里就没有)。
	if err := db.Exec(
		`INSERT INTO users (username, password_hash) VALUES (?, ?) ON CONFLICT (username) DO NOTHING`,
		username, "not-a-real-hash",
	).Error; err != nil {
		t.Fatalf("准备用户失败(迁移 DDL 本身跑不通?):\n%v", err)
	}

	var u models.User
	// 生产登录路径同款: First(&user, "username = ?")。
	err := db.Where("username = ?", username).First(&u).Error
	if err != nil {
		t.Fatalf("登录查询失败 —— models.User 与迁移 users DDL 漂移 (D-4):\n%v", err)
	}
	t.Logf("✅ 登录查询通过: id=%s username=%s role=%q deleted_at=%v", u.ID, u.Username, u.Role, u.DeletedAt)
}

// TestDBSmoke_AuditInsert 复刻审计写入: 插入一条 models.AuditLog。
// 漂移点: 模型 method/path/ip/status/error_msg/request_id/created_at
// vs 迁移 event_type/ip_address/result/error_message/timestamp (D-5)。
func TestDBSmoke_AuditInsert(t *testing.T) {
	db := openSmokeDB(t)

	entry := models.AuditLog{
		Action:   "dbsmoke",
		Resource: "smoke",
		Method:   "GET",
		Path:     "/api/v1/smoke",
		IP:       "127.0.0.1",
		Status:   200,
	}
	if err := db.Create(&entry).Error; err != nil {
		t.Fatalf("审计写入失败 —— models.AuditLog 与迁移 audit_logs DDL 列名漂移 (D-5):\n%v", err)
	}
	t.Logf("✅ 审计写入通过: id=%s action=%s", entry.ID, entry.Action)
}

// TestDBSmoke_TicketInsert 复刻建工单: 插入一条 models.Ticket。
// 漂移点: 迁移 tickets 24 列(含 ticket_no / creator_id NOT NULL / alert_id ...)
// vs 模型 24 列(含 ticket_number / requester_* / source ...), 交集仅 10 列 (D-1)。
func TestDBSmoke_TicketInsert(t *testing.T) {
	db := openSmokeDB(t)

	tk := models.Ticket{
		Title:      "dbsmoke ticket",
		TicketType: "incident",
		Priority:   "low",
		Status:     "open",
	}
	if err := db.Create(&tk).Error; err != nil {
		t.Fatalf("建工单失败 —— models.Ticket 与迁移 tickets DDL 三套 schema 漂移 (D-1):\n%v", err)
	}
	t.Logf("✅ 建工单通过: id=%s ticket_number=%s", tk.ID, tk.TicketNumber)
}

// TestDBSmoke_UpgradePath 模拟「存量部署升级」这条最危险的路径。
//
// 前置(由 scripts/db_smoke.sh 准备): 库已应用 000001~000012, schema_migrations
// 记录 1..12, 且存在一个升级前就创建的 admin(当时 users 表还没有 role 列,
// 其角色只在 user_roles 里)。本用例只跑 migrate.Up(即 000013), 断言回填生效。
//
// 为什么必须有: PG 11+ 的 `ADD COLUMN role VARCHAR(20) DEFAULT 'user'` 会让既有行
// 直接读出 'user', 使「只回填 NULL/空」的 UPDATE 恒不命中 —— 存量 admin 被降级成
// user 后, D-6 的 admin 门禁会把他锁在门外, 且仓库内没有提升角色的接口/CLI。
func TestDBSmoke_UpgradePath(t *testing.T) {
	db := openSmokeDB(t)

	var legacy int64
	if err := db.Raw(`SELECT count(*) FROM users WHERE username = 'legacy_admin'`).
		Scan(&legacy).Error; err != nil {
		t.Skipf("非升级路径库(无 users.legacy_admin), 跳过: %v", err)
	}
	if legacy == 0 {
		// 脚本声明了「这个库是升级库」时 seed 却缺失 → 必须 fail。
		// 否则 seed 的 INSERT 静默失败会变成「跳过 = 绿」（审计 中-6）。
		if os.Getenv("SMOKE_EXPECT_UPGRADE") == "1" {
			t.Fatal("SMOKE_EXPECT_UPGRADE=1 但库里没有 legacy_admin —— scripts/db_smoke.sh 的 seed 没生效")
		}
		t.Skip("非升级路径库(未 seed legacy_admin), 跳过")
	}

	var before int64
	if err := db.Raw(`SELECT count(*) FROM schema_migrations`).Scan(&before).Error; err != nil {
		t.Fatalf("读取 schema_migrations 失败:\n%v", err)
	}

	migrate.FS = network_monitor_platform.MigrationsFS
	if err := migrate.Up(db); err != nil {
		t.Fatalf("migrate.Up 在存量库上失败:\n%v", err)
	}

	var after int64
	if err := db.Raw(`SELECT count(*) FROM schema_migrations`).Scan(&after).Error; err != nil {
		t.Fatalf("读取 schema_migrations 失败:\n%v", err)
	}
	if after <= before {
		t.Errorf("升级后未新增迁移记录: before=%d after=%d", before, after)
	}

	var adminRole string
	if err := db.Raw(`SELECT role FROM users WHERE username = 'legacy_admin'`).
		Scan(&adminRole).Error; err != nil {
		t.Fatalf("查询 legacy_admin.role 失败:\n%v", err)
	}
	if adminRole != "admin" {
		t.Errorf("存量 admin 的 role 回填失败: got %q want %q —— 上线后该用户会被 D-6 门禁锁在门外",
			adminRole, "admin")
	}

	var plainRole string
	if err := db.Raw(`SELECT role FROM users WHERE username = 'legacy_plain'`).
		Scan(&plainRole).Error; err != nil {
		t.Fatalf("查询 legacy_plain.role 失败:\n%v", err)
	}
	if plainRole != "user" {
		t.Errorf("无 user_roles 记录的存量用户应兜底 user: got %q", plainRole)
	}
	t.Logf("✅ 存量升级通过: 迁移 %d→%d, legacy_admin.role=%s, legacy_plain.role=%s",
		before, after, adminRole, plainRole)
}

// newSmokeAsset 用迁移后的列名（000013 把 asset_name 改名为 name）插一条最小资产并返回其 id。
// 返回值走 string 再 uuid.Parse —— 直接 Scan 到 uuid.UUID 会因为驱动返回 string 而报错。
func newSmokeAsset(t *testing.T, db *gorm.DB, name string) uuid.UUID {
	t.Helper()
	var idStr string
	require.NoError(t, db.Raw(
		`INSERT INTO assets (asset_type, name) VALUES ('server', ?) RETURNING id`, name,
	).Scan(&idStr).Error, "造资产失败")
	id, err := uuid.Parse(idStr)
	require.NoError(t, err)
	return id
}

// TestDBSmoke_TypeConvertedModels 覆盖 000013 第 4 节做类型转换的模型：
// api_keys.permissions/ip_whitelist（TEXT[] → TEXT）、notification_channels.config（jsonb → TEXT）、
// alert_rules.notify_users/notify_channels、asset_networks 的 inet 列 → varchar。
//
// 为什么必须单独测：这些转换**转错了不会报错**。inet→text 走 network_show 会把
// '10.1.2.3' 变成 '10.1.2.3/32'；TEXT 上再套一层 to_json 会变成 '"[""read""]"'。
// 只有真库往返断言才能发现（审计 阻断-1 / 中-3）。
func TestDBSmoke_TypeConvertedModels(t *testing.T) {
	db := openSmokeDB(t)

	// api_keys.user_id 有 FK，先造一个真用户
	u := models.User{Username: "smoke_typecast_" + uuid.NewString()[:8], PasswordHash: "x", Status: "active"}
	require.NoError(t, db.Create(&u).Error, "造用户失败")

	key := models.APIKey{
		UserID:      u.ID,
		Name:        "smoke-key",
		KeyHash:     "smoke-hash",
		Prefix:      "sk-smoke",
		Permissions: models.StringList{"read", "write"},
		IPWhitelist: models.StringList{"10.0.0.1"},
	}
	require.NoError(t, db.Create(&key).Error, "api_keys 写入失败 —— permissions/ip_whitelist 类型漂移")
	var gotKey models.APIKey
	require.NoError(t, db.First(&gotKey, "id = ?", key.ID).Error)
	assert.Equal(t, models.StringList{"read", "write"}, gotKey.Permissions, "permissions 往返必须无损")
	assert.Equal(t, models.StringList{"10.0.0.1"}, gotKey.IPWhitelist, "ip_whitelist 往返必须无损")

	ch := models.NotificationChannel{Name: "smoke-ch", Type: "webhook", Config: `{"url":"http://example.invalid"}`}
	require.NoError(t, db.Create(&ch).Error, "notification_channels 写入失败 —— config 类型漂移")

	rule := models.AlertRule{Name: "smoke-rule", Metric: "cpu", NotifyChannels: `["webhook"]`, NotifyUsers: `["alice"]`}
	require.NoError(t, db.Create(&rule).Error, "alert_rules 写入失败 —— notify_* 类型漂移")

	// asset_networks.asset_id 有 FK，用迁移后的列名造一条最小资产
	assetID := newSmokeAsset(t, db, "smoke-asset")

	net := models.AssetNetwork{
		AssetID:       assetID,
		InterfaceName: "eth0",
		IPv4Address:   "10.1.2.3",
		IPv6Address:   "2001:db8::1",
	}
	require.NoError(t, db.Create(&net).Error, "asset_networks 写入失败 —— inet→varchar 漂移")
	var gotNet models.AssetNetwork
	require.NoError(t, db.First(&gotNet, "id = ?", net.ID).Error)
	assert.Equal(t, "10.1.2.3", gotNet.IPv4Address, "inet→varchar 必须用 host()，不能带 /32")
	assert.Equal(t, "2001:db8::1", gotNet.IPv6Address, "IPv6 同样不能带 /128")
}

// TestDBSmoke_TicketNumberUnique 断言 ticket_number 上有唯一约束 ——
// 这是 D-2「撞号后重试」的兜底前提。少了它，service 的重试逻辑永远不会触发，
// 并发下两张工单会拿到同一个号（审计 中-5）。
func TestDBSmoke_TicketNumberUnique(t *testing.T) {
	db := openSmokeDB(t)

	first := models.Ticket{
		Title: "smoke-unique-1", TicketType: "incident", Priority: "low", Status: "open",
		TicketNumber: "SMOKE-DUP-0001",
	}
	require.NoError(t, db.Create(&first).Error)

	dup := models.Ticket{
		Title: "smoke-unique-2", TicketType: "incident", Priority: "low", Status: "open",
		TicketNumber: "SMOKE-DUP-0001",
	}
	err := db.Create(&dup).Error
	require.Error(t, err, "重复 ticket_number 必须被唯一索引拒绝（D-2 重试的前提）")
	assert.Contains(t, strings.ToLower(err.Error()), "duplicate key", "应是唯一冲突而非别的错误")
}

// TestDBSmoke_MigrationReapply 模拟「DDL 已提交、版本未记录」的崩溃窗口：
// 删掉 schema_migrations 的 000013 记录后重跑 migrate.Up。迁移必须可重入，
// 且不得把已转换的数据再转一次（审计 阻断-1 / 中-8 的回归测试）。
func TestDBSmoke_MigrationReapply(t *testing.T) {
	db := openSmokeDB(t)

	var applied int64
	if err := db.Raw(`SELECT count(*) FROM schema_migrations WHERE version = 13`).
		Scan(&applied).Error; err != nil || applied == 0 {
		t.Skipf("库尚未应用到 000013，跳过重放用例: %v", err)
	}

	u := models.User{Username: "smoke_reapply_" + uuid.NewString()[:8], PasswordHash: "x", Status: "active"}
	require.NoError(t, db.Create(&u).Error)
	key := models.APIKey{
		UserID: u.ID, Name: "smoke-reapply", KeyHash: "h", Prefix: "sk-r",
		Permissions: models.StringList{"read"}, IPWhitelist: models.StringList{"10.0.0.1"},
	}
	require.NoError(t, db.Create(&key).Error)

	assetID := newSmokeAsset(t, db, "smoke-reapply-asset")
	net := models.AssetNetwork{AssetID: assetID, InterfaceName: "eth0", IPv4Address: "10.9.9.9"}
	require.NoError(t, db.Create(&net).Error)

	// 抹掉版本记录 → 下次 Up 会重放 000013
	require.NoError(t, db.Exec(`DELETE FROM schema_migrations WHERE version = 13`).Error)
	migrate.FS = network_monitor_platform.MigrationsFS
	require.NoError(t, migrate.Up(db), "000013 必须可重入（审计 阻断-1）")

	var gotKey models.APIKey
	require.NoError(t, db.First(&gotKey, "id = ?", key.ID).Error)
	assert.Equal(t, models.StringList{"read"}, gotKey.Permissions,
		"重放不得二次 JSON 编码（'[\"read\"]' → '\"[\\\"read\\\"]\"'）")

	var gotNet models.AssetNetwork
	require.NoError(t, db.First(&gotNet, "id = ?", net.ID).Error)
	assert.Equal(t, "10.9.9.9", gotNet.IPv4Address, "重放不得把地址变成 10.9.9.9/32")
}

// TestDBSmoke_DownPreservesLegacyColumns 回滚 000013 不得删掉 000001 就存在的列。
// down.sql 曾无条件 DROP tickets.ticket_type（up 里对它是 no-op），
// 回滚后该列与数据一起消失，且 GORM 枚举 Ticket.TicketType 会直接 500（审计 阻断-2）。
//
// 注意：本用例会回滚 000013，必须放在依赖 000013 的用例之后运行。
func TestDBSmoke_DownPreservesLegacyColumns(t *testing.T) {
	db := openSmokeDB(t)

	var applied int64
	if err := db.Raw(`SELECT count(*) FROM schema_migrations WHERE version = 13`).
		Scan(&applied).Error; err != nil || applied == 0 {
		t.Skipf("库未应用到 000013，跳过回滚用例: %v", err)
	}

	migrate.FS = network_monitor_platform.MigrationsFS
	require.NoError(t, migrate.Down(db), "回滚 000013 失败")

	var exists bool
	require.NoError(t, db.Raw(
		`SELECT EXISTS (SELECT 1 FROM information_schema.columns
		 WHERE table_name = 'tickets' AND column_name = 'ticket_type')`,
	).Scan(&exists).Error)
	assert.True(t, exists, "down 不得 DROP 000001 建的 tickets.ticket_type（丢列丢数据）")
}
