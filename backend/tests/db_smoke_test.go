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
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	network_monitor_platform "network-monitor-platform"
	"network-monitor-platform/internal/config"
	"network-monitor-platform/internal/database"
	"network-monitor-platform/internal/integration"
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
// 在空库上建库, 断言全部迁移应用成功。
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

	// 应用数必须等于 embed 里的 *.up.sql 数量 —— 否则「迁移文件被删/没进 embed」
	// 会让后面所有按 version 判断的用例静默 Skip（审计 F-A：最危险的假绿）。
	want, err := fs.Glob(network_monitor_platform.MigrationsFS, "migrations/*.up.sql")
	if err != nil || len(want) == 0 {
		t.Fatalf("读取 embed 内迁移文件失败(数量=%d):\n%v", len(want), err)
	}
	if int64(len(want)) != applied {
		t.Fatalf("embed 里有 %d 个 *.up.sql，schema_migrations 只记录了 %d 个 —— 迁移未全部应用或版本表被绕过", len(want), applied)
	}
	// 000015 是本轮守门的核心：它缺失时后续真库用例必须红，不是 Skip。
	var has15 bool
	if err := db.Raw(`SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = 15)`).
		Scan(&has15).Error; err != nil {
		t.Fatalf("探测 schema_migrations.version=15 失败:\n%v", err)
	}
	if !has15 {
		t.Fatalf("schema_migrations 里没有 version=15（000015_asset_netbox_unique）—— 本轮唯一索引迁移没进 embed 或没被应用")
	}

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

// assertJSONB 按列名断言一条资产的 tags/custom_fields。
//
// 比较走 `col = $x::jsonb` 的**语义相等**，不是逐字节比 text：PG 把 jsonb 渲染成 text 时
// 会规范化（`{"k":"v"}` → `{"k": "v"}`，冒号后补空格），键顺序也不保证。比 text 会假红。
// 失败信息里带上 ::text 便于定位；COALESCE 把 NULL 折成 'NULL' 而不是扫描报错。
func assertJSONB(t *testing.T, db *gorm.DB, whereVal, wantTags, wantCF string) {
	t.Helper()
	var tagsOK, cfOK bool
	var tags, cf string
	require.NoError(t, db.Raw(
		`SELECT COALESCE(tags = ?::jsonb, false),
		        COALESCE(custom_fields = ?::jsonb, false),
		        COALESCE(tags::text, 'NULL'),
		        COALESCE(custom_fields::text, 'NULL')
		   FROM assets WHERE name = ?`, wantTags, wantCF, whereVal,
	).Row().Scan(&tagsOK, &cfOK, &tags, &cf), "读取 %s 的 jsonb 列失败", whereVal)
	assert.True(t, tagsOK, "%s.tags: want jsonb %s, got %s", whereVal, wantTags, tags)
	assert.True(t, cfOK, "%s.custom_fields: want jsonb %s, got %s", whereVal, wantCF, cf)
}

// TestDBSmoke_AssetJSONBBackfill 守 000014 的**回填正确性**（升级路径）。
//
// 为什么必须预置数据：两条路径在 000014 执行时表里本来都没有资产，
// `UPDATE ... WHERE tags IS NULL` 恒命中 0 行 —— 用例会变成「删掉回填也是绿」的假绿。
// 脚本因此在升级库里预插两行：一行两列为 NULL（必须被回填），一行是合法 JSON（必须原样保留）。
func TestDBSmoke_AssetJSONBBackfill(t *testing.T) {
	db := openSmokeDB(t)

	const legacyNull, legacyJSON = "legacy-null-jsonb", "legacy-json-jsonb"
	var n int64
	require.NoError(t, db.Raw(
		`SELECT count(*) FROM assets WHERE name IN (?, ?)`, legacyNull, legacyJSON).Scan(&n).Error)
	if n == 0 {
		if os.Getenv("SMOKE_EXPECT_UPGRADE") == "1" {
			t.Fatalf("SMOKE_EXPECT_UPGRADE=1 但库里没有预置的存量资产 —— scripts/db_smoke.sh 的 seed 没生效")
		}
		t.Skip("非升级路径库（无预置存量资产），跳过")
	}

	migrate.FS = network_monitor_platform.MigrationsFS
	require.NoError(t, migrate.Up(db), "存量库上 migrate.Up 失败")

	assertJSONB(t, db, legacyNull, "[]", "{}")           // NULL → 回填
	assertJSONB(t, db, legacyJSON, `["x"]`, `{"k":"v"}`) // 合法值 → 不被改写
}

// TestDBSmoke_AssetJSONBDefaults 是 G-20 的真库回归（全新安装路径）：
// assets.tags / custom_fields 是 jsonb、模型字段是 Go string，零值 "" 被写进 INSERT
// → PG `invalid input syntax for type json`（22P02）。sqlite 不校验 JSON，单测全绿也挡不住。
// 本用例覆盖两条独立保证：① 应用层钩子 BeforeSave（gorm 写入路径）；② 列默认值（非 gorm 写入方）。
func TestDBSmoke_AssetJSONBDefaults(t *testing.T) {
	db := openSmokeDB(t)

	// ① gorm 建零值资产 —— 就是 cmd/seed 与 POST /api/assets 的路径
	asset := models.Asset{Name: "jsonb-zero-" + uuid.NewString()[:8], AssetType: "server"}
	require.NoError(t, db.Create(&asset).Error,
		"gorm 建零值资产失败 —— BeforeSave 钩子没生效（G-20 回归：PG 报 22P02）")
	assertJSONB(t, db, asset.Name, "[]", "{}")

	// ② 裸插入省略两列 —— 列默认值必须补上（删掉 000014 的 SET DEFAULT 即红）
	raw := "jsonb-raw-" + uuid.NewString()[:8]
	require.NoError(t, db.Exec(
		`INSERT INTO assets (asset_type, name) VALUES ('server', ?)`, raw).Error,
		"裸插入省略 jsonb 列失败 —— 000014 的列默认值没生效")
	assertJSONB(t, db, raw, "[]", "{}")

	// ③ 列默认值本身。只断言「插入不报错」会漏掉「默认值没建」——
	// 拿掉 SET DEFAULT 后裸插入会落 NULL 而不报错（这也是 down 后的组合状态）。
	for col, want := range map[string]string{"tags": `'[]'::jsonb`, "custom_fields": `'{}'::jsonb`} {
		var def *string
		require.NoError(t, db.Raw(
			`SELECT column_default FROM information_schema.columns
			  WHERE table_name = 'assets' AND column_name = ?`, col).Scan(&def).Error)
		require.NotNil(t, def, "assets.%s 没有列默认值 —— 000014 没跑或 SET DEFAULT 被删", col)
		assert.Equal(t, want, *def, "assets.%s 的列默认值", col)
	}
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

// assertNetBoxIDIndexUnique 断言 assets.net_box_id 上索引的唯一性形态。
//
// 用 pg_indexes.indexdef 而不是 pg_constraint：000015 建的是**唯一索引**，不是 UNIQUE 约束
// （约束会额外进 pg_constraint，索引不会）。gorm 的 uniqueIndex 标签同样只建索引。
func assertNetBoxIDIndexUnique(t *testing.T, db *gorm.DB, wantUnique bool) {
	t.Helper()
	var def string
	require.NoError(t, db.Raw(
		`SELECT indexdef FROM pg_indexes WHERE schemaname = 'public' AND indexname = 'idx_assets_net_box_id'`).
		Scan(&def).Error)
	require.NotEmpty(t, def, "idx_assets_net_box_id 不存在 —— 000015 没跑？")
	if wantUnique {
		assert.Contains(t, def, "CREATE UNIQUE INDEX",
			"net_box_id 必须是唯一索引，否则 ON CONFLICT 报 42P10：%s", def)
		return
	}
	assert.NotContains(t, def, "UNIQUE", "此刻应是非唯一索引（模拟 000015 之前的库）：%s", def)
}

// smokeNetBoxDevice 拼 NetBox /api/dcim/devices 的单台设备（只给 SyncDevices 会读的字段）。
func smokeNetBoxDevice(id int, name, roleSlug, model, sn, site string) map[string]any {
	return map[string]any{
		"id":            id,
		"name":          name,
		"device_type":   map[string]any{"id": 1, "slug": "cisco", "model": model},
		"device_role":   map[string]any{"id": 1, "slug": roleSlug, "name": roleSlug},
		"site":          map[string]any{"id": 1, "slug": "dc", "name": site},
		"serial_number": sn,
	}
}

// TestDBSmoke_NetBoxUpsert 是 G-22 的真库回归（docs/FIX-PLAN-NETBOX-UPSERT.md §4 V-5）。
//
// 为什么必须在真 PG 上测：ON CONFLICT 的仲裁者（唯一索引）是**迁移产物**，sqlite 单测用的是手写
// DDL，测不到 000015 是否真的跑到了生产库上；而 42P10 只存在于 PG。覆盖五件事：
// ① 索引形态是 UNIQUE；② 重复 net_box_id 被拒；③ 多个 NULL 共存（手工资产）；④ 真调用点端到端
// （更新 + 保留 id + 不覆盖本地 status）；⑤ 脏数据挡住迁移时的原子失败 + 清理后重跑成功。
func TestDBSmoke_NetBoxUpsert(t *testing.T) {
	db := openSmokeDB(t)

	// 前置不满足必须**红**，不是跳过：本用例是本轮唯一索引迁移的唯一真库守门，
	// 静默 Skip 等于「删掉 000015 也全绿」（审计 F-A）。
	var applied int64
	if err := db.Raw(`SELECT count(*) FROM schema_migrations WHERE version = 15`).
		Scan(&applied).Error; err != nil {
		t.Fatalf("读取 schema_migrations 失败:\n%v", err)
	}
	if applied == 0 {
		t.Fatalf("库未应用到 000015（assets.net_box_id 唯一索引）—— 本用例的前置不满足")
	}

	// ① 索引形态
	assertNetBoxIDIndexUnique(t, db, true)

	// ② 重复 net_box_id 必须被拒 —— 这正是 upsert 走 DO UPDATE 的前提
	const dupID = 990015
	require.NoError(t, db.Create(&models.Asset{
		Name: "smoke-nb-first", AssetType: "server", Source: "netbox", NetBoxID: intPtr(dupID),
	}).Error)
	err := db.Create(&models.Asset{
		Name: "smoke-nb-dup", AssetType: "server", Source: "netbox", NetBoxID: intPtr(dupID),
	}).Error
	require.Error(t, err, "重复 net_box_id 必须被唯一索引拒绝")
	assert.Contains(t, strings.ToLower(err.Error()), "duplicate key", "应是唯一冲突: %v", err)

	// ③ 多个 NULL 共存（手工资产 net_box_id 为 NULL，PG 唯一索引允许多个 NULL）
	require.NoError(t, db.Create(&models.Asset{Name: "smoke-nb-manual-1", AssetType: "server", Source: "manual"}).Error)
	require.NoError(t, db.Create(&models.Asset{Name: "smoke-nb-manual-2", AssetType: "server", Source: "manual"}).Error,
		"多个 NULL net_box_id 必须能共存（唯一索引不该约束 NULL）")

	// ④ 真调用点端到端：httptest 假 NetBox + 真 SyncFromNetBox + 真 PG
	payload := mustJSON(t, map[string]any{"count": 2, "results": []map[string]any{
		smokeNetBoxDevice(990101, "smoke-nb-sw01", "switch", "Catalyst 9300", "SN-101", "DC1"),
		smokeNetBoxDevice(990102, "smoke-nb-srv01", "server", "PowerEdge R750", "SN-102", "DC1"),
	}})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(payload))
	}))
	defer srv.Close()

	oldDB := database.GetDB()
	database.SetDBForTest(db)
	defer database.SetDBForTest(oldDB)

	svc := integration.NewIntegrationService(&config.Config{
		Integrations: config.IntegrationsConfig{
			Netbox: config.NetboxConfig{URL: srv.URL, Token: "t"},
		},
	}, nil)

	n, err := svc.SyncFromNetBox(context.Background())
	require.NoError(t, err, "真 PG 上 SyncFromNetBox 失败 —— 000015 的唯一索引没生效（42P10）？")
	require.Equal(t, 2, n)

	var created models.Asset
	require.NoError(t, db.First(&created, "net_box_id = ?", 990101).Error)
	firstID := created.ID
	assert.Equal(t, "smoke-nb-sw01", created.Name)
	assert.Equal(t, "network", created.AssetType, "device_role.slug=switch → asset_type=network")

	// 本地先退役（NetBox 侧不跟踪退役）+ 写人工标签/机柜，再同步 → name 更新，
	// status/tags/custom_fields/rack_name 都不被覆盖（更新列里刻意不含它们）。
	// 不预置这几列的话，「误加回更新列」的改动照样绿（审计 F-2）。
	require.NoError(t, db.Model(&models.Asset{}).Where("net_box_id = ?", 990101).
		Updates(map[string]any{
			"status":        "retired",
			"tags":          `["prod"]`,
			"custom_fields": `{"owner":"ops"}`,
			"rack_name":     "R-A1",
		}).Error)
	payload = mustJSON(t, map[string]any{"count": 2, "results": []map[string]any{
		smokeNetBoxDevice(990101, "smoke-nb-sw01-renamed", "switch", "Catalyst 9300", "SN-999", "DC2"),
		smokeNetBoxDevice(990102, "smoke-nb-srv01", "server", "PowerEdge R750", "SN-102", "DC1"),
	}})
	n, err = svc.SyncFromNetBox(context.Background())
	require.NoError(t, err, "二次同步（冲突更新）失败")
	require.Equal(t, 2, n)

	var updated models.Asset
	require.NoError(t, db.First(&updated, "net_box_id = ?", 990101).Error)
	assert.Equal(t, "smoke-nb-sw01-renamed", updated.Name, "冲突行必须被更新")
	assert.Equal(t, "SN-999", updated.SN)
	assert.Equal(t, "DC2", updated.SiteName, "site_name 必须跟着 NetBox 走")
	assert.Equal(t, firstID, updated.ID, "upsert 不得改写已有行的 id")
	assert.Equal(t, "retired", updated.Status, "同步不得覆盖本地 status")
	assert.Equal(t, `["prod"]`, updated.Tags, "同步不得覆盖人工标签 tags")
	assert.Equal(t, `{"owner": "ops"}`, updated.CustomFields, "同步不得覆盖人工自定义字段")
	assert.Equal(t, "R-A1", updated.RackName, "同步不得清空 rack_name（ConvertToAsset 不映射机柜）")

	// ⑤ 负循环：重复数据 + 版本记录被抹 → Up 必须整体失败（不留半应用状态）
	require.NoError(t, db.Exec(`DROP INDEX IF EXISTS idx_assets_net_box_id`).Error)
	require.NoError(t, db.Exec(`CREATE INDEX idx_assets_net_box_id ON assets(net_box_id)`).Error)
	require.NoError(t, db.Exec(`DELETE FROM schema_migrations WHERE version = 15`).Error)

	const dirtyID = 990016
	// 中途失败也要把库恢复成「唯一索引 + version 15」，否则同库后续用例
	// 会因前置缺失而 Skip（审计 F-7）。用例成功时它是无害的空转。
	t.Cleanup(func() {
		_ = db.Exec(`DELETE FROM assets WHERE net_box_id = ?`, dirtyID).Error
		if err := migrate.Up(db); err != nil {
			t.Logf("恢复库到 000015 失败（同库后续用例可能前置不满足）: %v", err)
		}
	})

	require.NoError(t, db.Create(&models.Asset{
		Name: "smoke-nb-dirty-1", AssetType: "server", NetBoxID: intPtr(dirtyID),
	}).Error)
	require.NoError(t, db.Create(&models.Asset{
		Name: "smoke-nb-dirty-2", AssetType: "server", NetBoxID: intPtr(dirtyID),
	}).Error, "非唯一索引下重复行必须能插进去（否则本用例的前提不成立）")

	migrate.FS = network_monitor_platform.MigrationsFS
	err = migrate.Up(db)
	require.Error(t, err, "存在重复 net_box_id 时 000015 必须失败")
	// 断言 PG 原文 + SQLSTATE。注意日志里**没有 DETAIL（哪个键重复）**：驱动的 PgError.Error()
	// 只拼 Severity/Message/SQLSTATE（本仓库未开 gorm TranslateError）→ 定位重复值只能靠
	// 000015 里给的自检 SQL，不能指望日志。
	assert.Contains(t, strings.ToLower(err.Error()), "could not create unique index",
		"应是唯一索引创建失败: %v", err)
	assert.Contains(t, err.Error(), "23505", "应带 SQLSTATE 23505: %v", err)

	var recorded int64
	require.NoError(t, db.Raw(`SELECT count(*) FROM schema_migrations WHERE version = 15`).
		Scan(&recorded).Error)
	assert.Zero(t, recorded, "失败的迁移不得留下版本记录（DDL 与版本号在同一事务）")
	assertNetBoxIDIndexUnique(t, db, false) // 失败后仍是普通索引，没有半应用状态

	// 清掉重复 → 重跑成功
	require.NoError(t, db.Exec(`DELETE FROM assets WHERE net_box_id = ?`, dirtyID).Error)
	require.NoError(t, migrate.Up(db), "清掉重复后 000015 必须成功（可重入）")
	require.NoError(t, db.Raw(`SELECT count(*) FROM schema_migrations WHERE version = 15`).
		Scan(&recorded).Error)
	assert.Equal(t, int64(1), recorded, "成功后才记录版本")
	assertNetBoxIDIndexUnique(t, db, true)
}

// intPtr 取 *int（models.Asset.NetBoxID 是指针，NULL 表示手工资产）。
func intPtr(v int) *int { return &v }

// mustJSON 把任意结构编码成 JSON 字符串（测试内的假服务响应体）。
func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return string(b)
}

// TestDBSmoke_DownPreservesLegacyColumns 回滚 000013 不得删掉 000001 就存在的列。
// down.sql 曾无条件 DROP tickets.ticket_type（up 里对它是 no-op），
// 回滚后该列与数据一起消失，且 GORM 枚举 Ticket.TicketType 会直接 500（审计 阻断-2）。
//
// 注意两点：
//  1. migrate.Down 只回滚**最新已应用版本**（internal/migrate/migrate.go:245）——
//     每新增一个迁移就要多回滚一次，否则本用例会静默变成「回滚上一层」的空转。
//     当前最高版本是 000021，故九次 Down = 21 → 20 → 19 → 18 → 17 → 16 → 15 → 14 → 13。
//  2. 本用例会回滚 000013，必须放在依赖 000013 的用例之后运行。
func TestDBSmoke_DownPreservesLegacyColumns(t *testing.T) {
	db := openSmokeDB(t)

	// 前置 1：必须已应用到 000021（本用例回滚 21→20→19→18→17→16→15→14→13）。缺失要**红**不是跳过 —— 审计 F-A。
	var applied int64
	if err := db.Raw(`SELECT count(*) FROM schema_migrations WHERE version = 21`).
		Scan(&applied).Error; err != nil {
		t.Fatalf("读取 schema_migrations 失败:\n%v", err)
	}
	if applied == 0 {
		t.Fatalf("库未应用到 000021 —— 本用例要回滚 21→20→19→18→17→16→15→14→13，前置不满足")
	}

	// 前置 2：必须是**升级路径**库。回滚链里要断言 000014 的回填值仍在（assertJSONB），
	// fresh 库里那两行不存在 → 断言会因 0 行失败；显式跳过比假红更诚实。
	var seeded int64
	require.NoError(t, db.Raw(`SELECT count(*) FROM assets WHERE name = ?`, "legacy-null-jsonb").
		Scan(&seeded).Error)
	if seeded == 0 {
		if os.Getenv("SMOKE_EXPECT_UPGRADE") == "1" {
			t.Fatalf("SMOKE_EXPECT_UPGRADE=1 但库里没有预置的存量资产 —— scripts/db_smoke.sh 的 seed 没生效")
		}
		t.Skip("非升级路径库（无预置存量资产），跳过回滚用例")
	}

	migrate.FS = network_monitor_platform.MigrationsFS

	// 第一次 Down = 回滚 000021：删除 path text_pattern_ops 索引
	require.NoError(t, migrate.Down(db), "回滚 000021 失败")
	var pathIdxExists bool
	require.NoError(t, db.Raw(
		`SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE indexname = 'idx_audit_logs_path')`).
		Scan(&pathIdxExists).Error)
	assert.False(t, pathIdxExists, "down 000021 应 DROP idx_audit_logs_path")

	// 第二次 Down = 回滚 000020：删除 name 索引
	require.NoError(t, migrate.Down(db), "回滚 000020 失败")
	var nameIdxExists bool
	require.NoError(t, db.Raw(
		`SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE indexname = 'idx_assets_name')`).
		Scan(&nameIdxExists).Error)
	assert.False(t, nameIdxExists, "down 000020 应 DROP idx_assets_name")

	// 第三次 Down = 回滚 000019：删除 external_id 索引
	require.NoError(t, migrate.Down(db), "回滚 000019 失败")
	var extIdxExists bool
	require.NoError(t, db.Raw(
		`SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE indexname = 'idx_tickets_external_id')`).
		Scan(&extIdxExists).Error)
	assert.False(t, extIdxExists, "down 000019 应 DROP idx_tickets_external_id")

	// 第四次 Down = 回滚 000018：删除 trigger_id 索引
	require.NoError(t, migrate.Down(db), "回滚 000018 失败")
	var trigIdxExists bool
	require.NoError(t, db.Raw(
		`SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE indexname = 'idx_alerts_trigger_id')`).
		Scan(&trigIdxExists).Error)
	assert.False(t, trigIdxExists, "down 000018 应 DROP idx_alerts_trigger_id")

	// 第五次 Down = 回滚 000017：删除 problem_start 索引
	require.NoError(t, migrate.Down(db), "回滚 000017 失败")
	var psIdxExists bool
	require.NoError(t, db.Raw(
		`SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE indexname = 'idx_alerts_problem_start')`).
		Scan(&psIdxExists).Error)
	assert.False(t, psIdxExists, "down 000017 应 DROP idx_alerts_problem_start")

	// 第六次 Down = 回滚 000016：删除 pending 部分索引
	require.NoError(t, migrate.Down(db), "回滚 000016 失败")
	var pendingIdxExists bool
	require.NoError(t, db.Raw(
		`SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE indexname = 'idx_notification_logs_pending')`).
		Scan(&pendingIdxExists).Error)
	assert.False(t, pendingIdxExists, "down 000016 应 DROP idx_notification_logs_pending")

	// 第七次 Down = 回滚 000015：net_box_id 回到非唯一索引（组合状态下 ON CONFLICT 会 42P10）
	require.NoError(t, migrate.Down(db), "回滚 000015 失败")
	assertNetBoxIDIndexUnique(t, db, false)

	// 第八次 Down = 回滚 000014：只撤列默认值，数据不动
	require.NoError(t, migrate.Down(db), "回滚 000014 失败")
	var def *string
	require.NoError(t, db.Raw(
		`SELECT column_default FROM information_schema.columns
		  WHERE table_name = 'assets' AND column_name = 'tags'`).Scan(&def).Error)
	assert.Nil(t, def, "down 000014 应 DROP DEFAULT assets.tags")
	assertJSONB(t, db, "legacy-null-jsonb", "[]", "{}") // 回填值仍在（down 不动数据）

	// 第九次 Down = 回滚 000013：本用例真正要守的那个
	require.NoError(t, migrate.Down(db), "回滚 000013 失败")

	var exists bool
	require.NoError(t, db.Raw(
		`SELECT EXISTS (SELECT 1 FROM information_schema.columns
		 WHERE table_name = 'tickets' AND column_name = 'ticket_type')`,
	).Scan(&exists).Error)
	assert.True(t, exists, "down 不得 DROP 000001 建的 tickets.ticket_type（丢列丢数据）")
}

// explainSeqScanOff 在**单连接**上禁掉顺序扫描后跑 EXPLAIN，返回完整计划文本。
//
// 为什么用单连接：`SET enable_seqscan` 是会话级，走连接池会在 EXPLAIN 时换连接、
// 设置失效。为什么禁 seqscan：冒烟库几乎为空，优化器对 0 行表默认选 Seq Scan
// （无数据时索引反而慢），EXPLAIN 断言会假红；禁掉后强制走索引，验证「索引存在
// 且其谓词/列与查询匹配」。
func explainSeqScanOff(t *testing.T, db *gorm.DB, query string) string {
	t.Helper()
	sqlDB, err := db.DB()
	require.NoError(t, err)
	conn, err := sqlDB.Conn(context.Background())
	require.NoError(t, err)
	defer conn.Close()

	_, err = conn.ExecContext(context.Background(), "SET enable_seqscan = off")
	require.NoError(t, err, "SET enable_seqscan 失败")

	rows, err := conn.QueryContext(context.Background(), query)
	require.NoError(t, err, "EXPLAIN 失败")
	defer rows.Close()

	var lines []string
	for rows.Next() {
		var line string
		require.NoError(t, rows.Scan(&line))
		lines = append(lines, line)
	}
	require.NoError(t, rows.Err())
	return strings.Join(lines, "\n")
}

// TestDBSmoke_NotificationPendingIndex 守 000016 的 pending 部分索引（W6 P13）。
//
// 为什么必须真 PG：sqlite 单测用本地 GORM AutoMigrate 的 schema，测不到迁移产物
// 是否真的建了部分索引；而 worker 的轮询查询 `WHERE status='pending' ORDER BY sent_at`
// 在 000009 只有 failed 部分索引时全表扫（审计实测 286.6ms）。两层断言：
// ① 索引形态是部分索引（WHERE status='pending'，列 sent_at）；② EXPLAIN 走该索引。
func TestDBSmoke_NotificationPendingIndex(t *testing.T) {
	db := openSmokeDB(t)

	// 前置不满足必须**红**，不是跳过：本用例是 000016 的唯一真库守门。
	var applied int64
	require.NoError(t, db.Raw(`SELECT count(*) FROM schema_migrations WHERE version = 16`).
		Scan(&applied).Error)
	if applied == 0 {
		t.Fatalf("库未应用到 000016（idx_notification_logs_pending）—— 本用例前置不满足")
	}

	// ① 索引形态：部分索引 WHERE status='pending'，列 sent_at
	var def string
	require.NoError(t, db.Raw(
		`SELECT indexdef FROM pg_indexes WHERE schemaname = 'public' AND indexname = 'idx_notification_logs_pending'`).
		Scan(&def).Error)
	require.NotEmpty(t, def, "idx_notification_logs_pending 不存在 —— 000016 没跑？")
	assert.Contains(t, def, "sent_at", "索引应建在 sent_at 上：%s", def)
	assert.Contains(t, def, "status", "部分索引谓词应含 status 列：%s", def)
	assert.Contains(t, def, "'pending'", "部分索引谓词应是 status='pending'：%s", def)

	// ② EXPLAIN 断言：worker 的 pending 轮询查询走该索引（worker.go:198-206 同款）
	plan := explainSeqScanOff(t, db,
		`EXPLAIN (COSTS OFF) SELECT * FROM notification_logs WHERE status = 'pending' ORDER BY sent_at ASC LIMIT 100`)
	assert.Contains(t, plan, "idx_notification_logs_pending",
		"pending 轮询查询应走部分索引：\n%s", plan)
}

// TestDBSmoke_AlertsProblemStartIndex 守 000017 的 problem_start 索引（W6 P14）。
//
// dashboard/kpis 的 4 条聚合（MTTR/MTTD/密度/计数）都按 `problem_start >= ?`
// 过滤时间窗，无索引时每次扫全表（审计实测合计 ~1.1s）。真 PG 两层断言：
// ① 索引形态（列 problem_start）；② EXPLAIN 走该索引（范围查询）。
func TestDBSmoke_AlertsProblemStartIndex(t *testing.T) {
	db := openSmokeDB(t)

	var applied int64
	require.NoError(t, db.Raw(`SELECT count(*) FROM schema_migrations WHERE version = 17`).
		Scan(&applied).Error)
	if applied == 0 {
		t.Fatalf("库未应用到 000017（idx_alerts_problem_start）—— 本用例前置不满足")
	}

	// ① 索引形态：普通索引，列 problem_start
	var def string
	require.NoError(t, db.Raw(
		`SELECT indexdef FROM pg_indexes WHERE schemaname = 'public' AND indexname = 'idx_alerts_problem_start'`).
		Scan(&def).Error)
	require.NotEmpty(t, def, "idx_alerts_problem_start 不存在 —— 000017 没跑？")
	assert.Contains(t, def, "problem_start", "索引应建在 problem_start 上：%s", def)

	// ② EXPLAIN 断言：KPI 时间窗过滤走该索引（dashboard_service.go:156-207 同款）
	plan := explainSeqScanOff(t, db,
		`EXPLAIN (COSTS OFF) SELECT COUNT(*) FROM alerts WHERE problem_start >= '2026-01-01'::timestamp`)
	assert.Contains(t, plan, "idx_alerts_problem_start",
		"problem_start 范围查询应走索引：\n%s", plan)
}

// TestDBSmoke_AlertsTriggerIDIndex 守 000018 的 trigger_id 索引（W6 P15）。
//
// Zabbix 同步预查 `alerts WHERE trigger_id IN (...) AND status='problem'` 无索引
// 时扫全表（审计实测 294.8ms）。真 PG 两层断言：① 索引形态（列 trigger_id）；
// ② EXPLAIN 走该索引（IN 列表查询）。
func TestDBSmoke_AlertsTriggerIDIndex(t *testing.T) {
	db := openSmokeDB(t)

	var applied int64
	require.NoError(t, db.Raw(`SELECT count(*) FROM schema_migrations WHERE version = 18`).
		Scan(&applied).Error)
	if applied == 0 {
		t.Fatalf("库未应用到 000018（idx_alerts_trigger_id）—— 本用例前置不满足")
	}

	// ① 索引形态：普通索引，列 trigger_id
	var def string
	require.NoError(t, db.Raw(
		`SELECT indexdef FROM pg_indexes WHERE schemaname = 'public' AND indexname = 'idx_alerts_trigger_id'`).
		Scan(&def).Error)
	require.NotEmpty(t, def, "idx_alerts_trigger_id 不存在 —— 000018 没跑？")
	assert.Contains(t, def, "trigger_id", "索引应建在 trigger_id 上：%s", def)

	// ② EXPLAIN 断言：trigger_id 条件走该索引。去掉 status 过滤——真实查询
	// `trigger_id IN (...) AND status='problem'` 在空表上优化器会改选已存在的
	// status 索引（idx_alerts_status_created），EXPLAIN 断言因此不稳定；这里只验证
	// trigger_id 条件本身能命中索引（status 是附加过滤，不影响索引存在性）。
	plan := explainSeqScanOff(t, db,
		`EXPLAIN (COSTS OFF) SELECT * FROM alerts WHERE trigger_id IN ('a','b')`)
	assert.Contains(t, plan, "idx_alerts_trigger_id",
		"trigger_id IN 查询应走索引：\n%s", plan)
}

// TestDBSmoke_TicketsExternalIDIndex 守 000019 的 external_id 索引（W6 P16）。
//
// GLPI 同步预查 `tickets WHERE external_id IN (...)` 无索引时扫全表
// （审计实测 152.2ms）。真 PG 两层断言：① 索引形态（列 external_id）；
// ② EXPLAIN 走该索引（IN 列表查询）。
func TestDBSmoke_TicketsExternalIDIndex(t *testing.T) {
	db := openSmokeDB(t)

	var applied int64
	require.NoError(t, db.Raw(`SELECT count(*) FROM schema_migrations WHERE version = 19`).
		Scan(&applied).Error)
	if applied == 0 {
		t.Fatalf("库未应用到 000019（idx_tickets_external_id）—— 本用例前置不满足")
	}

	// ① 索引形态：普通索引，列 external_id
	var def string
	require.NoError(t, db.Raw(
		`SELECT indexdef FROM pg_indexes WHERE schemaname = 'public' AND indexname = 'idx_tickets_external_id'`).
		Scan(&def).Error)
	require.NotEmpty(t, def, "idx_tickets_external_id 不存在 —— 000019 没跑？")
	assert.Contains(t, def, "external_id", "索引应建在 external_id 上：%s", def)

	// ② EXPLAIN 断言：真实查询 `WHERE external_id IN (...)` 是单条件，
	// 无其它索引竞争，直接走 idx_tickets_external_id。
	plan := explainSeqScanOff(t, db,
		`EXPLAIN (COSTS OFF) SELECT * FROM tickets WHERE external_id IN ('a','b')`)
	assert.Contains(t, plan, "idx_tickets_external_id",
		"external_id IN 查询应走索引：\n%s", plan)
}

// TestDBSmoke_AssetsNameIndex 守 000020 的 name 索引（W6 P17）。
//
// metric sync 每 5min 按 `assets.name IN (...)` 关联无索引时扫全表
// （审计实测 75.3ms）。真 PG 两层断言：① 索引形态（列 name）；
// ② EXPLAIN 走该索引（IN 列表查询）。
func TestDBSmoke_AssetsNameIndex(t *testing.T) {
	db := openSmokeDB(t)

	var applied int64
	require.NoError(t, db.Raw(`SELECT count(*) FROM schema_migrations WHERE version = 20`).
		Scan(&applied).Error)
	if applied == 0 {
		t.Fatalf("库未应用到 000020（idx_assets_name）—— 本用例前置不满足")
	}

	// ① 索引形态：普通索引，列 name
	var def string
	require.NoError(t, db.Raw(
		`SELECT indexdef FROM pg_indexes WHERE schemaname = 'public' AND indexname = 'idx_assets_name'`).
		Scan(&def).Error)
	require.NotEmpty(t, def, "idx_assets_name 不存在 —— 000020 没跑？")
	assert.Contains(t, def, "name", "索引应建在 name 上：%s", def)

	// ② EXPLAIN 断言：真实查询 `WHERE name IN (...)` 是单条件，
	// 无其它索引竞争，直接走 idx_assets_name。
	plan := explainSeqScanOff(t, db,
		`EXPLAIN (COSTS OFF) SELECT * FROM assets WHERE name IN ('a','b')`)
	assert.Contains(t, plan, "idx_assets_name",
		"name IN 查询应走索引：\n%s", plan)
}

// TestDBSmoke_AuditLogsPathIndex 守 000021 的 path text_pattern_ops 索引（W6 P18）。
//
// 审计列表 `path LIKE 'x%'` 前缀匹配无可用索引（审计实测罕见过滤 455ms）。
// text_pattern_ops 专为 LIKE 前缀设计（非 C collation 下默认 btree 不加速 LIKE）。
// 真 PG 两层断言：① 索引形态（列 path + opclass text_pattern_ops）；
// ② EXPLAIN 走该索引（LIKE 前缀查询）。
func TestDBSmoke_AuditLogsPathIndex(t *testing.T) {
	db := openSmokeDB(t)

	var applied int64
	require.NoError(t, db.Raw(`SELECT count(*) FROM schema_migrations WHERE version = 21`).
		Scan(&applied).Error)
	if applied == 0 {
		t.Fatalf("库未应用到 000021（idx_audit_logs_path）—— 本用例前置不满足")
	}

	// ① 索引形态：text_pattern_ops 索引，列 path
	var def string
	require.NoError(t, db.Raw(
		`SELECT indexdef FROM pg_indexes WHERE schemaname = 'public' AND indexname = 'idx_audit_logs_path'`).
		Scan(&def).Error)
	require.NotEmpty(t, def, "idx_audit_logs_path 不存在 —— 000021 没跑？")
	assert.Contains(t, def, "path", "索引应建在 path 上：%s", def)
	assert.Contains(t, def, "text_pattern_ops", "应为 text_pattern_ops 索引（LIKE 前缀）：%s", def)

	// ② EXPLAIN 断言：`path LIKE 'x%'` 前缀查询走该索引（pattern_ops 的 ~>=~ / ~<~）。
	plan := explainSeqScanOff(t, db,
		`EXPLAIN (COSTS OFF) SELECT * FROM audit_logs WHERE path LIKE 'x%'`)
	assert.Contains(t, plan, "idx_audit_logs_path",
		"path LIKE 前缀查询应走索引：\n%s", plan)
}
