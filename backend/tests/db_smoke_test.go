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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	network_monitor_platform "network-monitor-platform"
	"network-monitor-platform/internal/config"
	"network-monitor-platform/internal/database"
	"network-monitor-platform/internal/integration"
	"network-monitor-platform/internal/middleware"
	"network-monitor-platform/internal/migrate"
	"network-monitor-platform/internal/models"
	"network-monitor-platform/internal/service"

	"github.com/gin-gonic/gin"
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

// TestDBSmoke_MigrationNoSessionGUCLeak 钉住 000014 的 lock_timeout 作用域（TODO G-26）：
// 迁移必须用 SET LOCAL，绝不能把会话级 GUC 泄漏到连接池。
//
// 为什么不复用 migrate.Up 复现：GUC 落在「执行迁移的那条物理连接」上，而 migrate.Up
// 期间至少占两条连接（acquireLock 的会话级 advisory lock 单独持一条，见 migrate.go:54-79），
// 归还顺序是 LIFO —— 迁移结束后裸查 SHOW lock_timeout 大概率拿到 acquireLock 那条干净连接，
// 从而**假绿**。所以这里显式独占一条连接，在同一事务里跑完迁移文件，再在同一条连接上验证。
func TestDBSmoke_MigrationNoSessionGUCLeak(t *testing.T) {
	db := openSmokeDB(t)
	sqlDB, err := db.DB()
	require.NoError(t, err)

	ctx := context.Background()
	conn, err := sqlDB.Conn(ctx)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	// 前置：连接必须处于 PG 默认值，否则断言无从谈起（也挡住「跑在脏连接上」的假绿）
	var before string
	require.NoError(t, conn.QueryRowContext(ctx, "SHOW lock_timeout").Scan(&before))
	require.Equal(t, "0", before, "前置不满足：本连接不是默认 lock_timeout（实为 %q）", before)

	sqlText, err := fs.ReadFile(network_monitor_platform.MigrationsFS,
		"migrations/000014_asset_jsonb_defaults.up.sql")
	require.NoError(t, err, "读不到 embed 里的 000014 —— 迁移文件改名或没进 embed？")

	// 复刻 execInTx（internal/migrate/migrate.go:298-325）：整文件一个事务
	tx, err := conn.BeginTx(ctx, nil)
	require.NoError(t, err)
	for _, stmt := range splitSimpleStatements(string(sqlText)) {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			_ = tx.Rollback()
			t.Fatalf("执行 000014 语句失败 (%s):\n%v", firstSQLLine(stmt), err)
		}
	}
	require.NoError(t, tx.Commit(),
		"000014 必须可重入（重复 SET DEFAULT / 第二次回填 0 行）")

	var after string
	require.NoError(t, conn.QueryRowContext(ctx, "SHOW lock_timeout").Scan(&after))
	assert.NotEqual(t, "5s", after,
		"000014 把会话级 lock_timeout 泄漏到了连接上（TODO G-26）——须用 SET LOCAL；"+
			"泄漏后连接归还池，后续业务写入会莫名 55P03")
}

// splitSimpleStatements 只服务「无 DO 块、无字符串内分号」的迁移文件（当前即 000014）。
// 生产执行器用的是 internal/migrate.splitStatements（要处理 $$ 块与引号转义），
// 此处刻意不重复实现它 —— 本用例要验证的是 GUC 作用域，不是语句切分。
func splitSimpleStatements(sqlText string) []string {
	var out []string
	for _, raw := range strings.Split(sqlText, ";") {
		var kept []string
		for _, ln := range strings.Split(raw, "\n") {
			if strings.HasPrefix(strings.TrimSpace(ln), "--") {
				continue // 丢弃纯注释行，使「只剩注释」的碎片自然变空
			}
			kept = append(kept, ln)
		}
		if s := strings.TrimSpace(strings.Join(kept, "\n")); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// firstSQLLine 取语句首行，用于失败信息里定位是 000014 的哪一条。
func firstSQLLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i > 0 {
		return s[:i]
	}
	return s
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

// TestDBSmoke_TicketPriorityNormalize 守 000023：存量 priority='medium' 必须被归一为 'normal'（M16）。
//
// 为什么必须预置数据：升级库里本来一行 priority='medium' 都没有，
// `UPDATE ... WHERE priority='medium'` 恒命中 0 行 —— 用例会变成「把迁移删掉也是绿」的
// 假绿，与 000014 的回填同一个坑（见 TestDBSmoke_AssetJSONBBackfill 的注释）。
//
// 必须排在 migrate.Up 之后、TestDBSmoke_DownPreservesLegacyColumns 之前：
// 后者会把库一路 Down 回 000013，跑在它后面的话前置 3 会直接 Fatal。
// （这里刻意不写「第一次 Down 滚的是 0000NN」—— 那个号每加一个迁移就过期一次，
// 历史上就写歪过两回；「一路 Down 到 000013」才是不变量。）
func TestDBSmoke_TicketPriorityNormalize(t *testing.T) {
	db := openSmokeDB(t)

	var applied int64
	require.NoError(t, db.Raw(`SELECT count(*) FROM schema_migrations WHERE version = 23`).
		Scan(&applied).Error)
	if applied == 0 {
		if os.Getenv("SMOKE_EXPECT_UPGRADE") == "1" {
			t.Fatalf("SMOKE_EXPECT_UPGRADE=1 但 000023 未应用 —— 迁移没跑或本用例排在 migrate.Up 之前")
		}
		t.Skip("非升级路径库(000023 未应用), 跳过")
	}

	// 列名用 000013 RENAME 之后的 ticket_number —— pre-000013 的库上是 ticket_no。
	readPriority := func(t *testing.T, no string) string {
		t.Helper()
		var n int64
		require.NoError(t, db.Raw(`SELECT count(*) FROM tickets WHERE ticket_number = ?`, no).
			Scan(&n).Error)
		require.Equal(t, int64(1), n,
			"脚本的存量工单 %s 没进库 —— scripts/db_smoke.sh 的 INSERT 没生效(没有它, 本用例恒绿)", no)
		var prio string
		require.NoError(t, db.Raw(`SELECT priority FROM tickets WHERE ticket_number = ?`, no).
			Scan(&prio).Error)
		return prio
	}

	assert.Equal(t, "normal", readPriority(t, "LEGACY-M16-1"),
		"000023 应把存量 medium 归一为 normal —— 否则 /tickets 按「普通」筛选查不到这张票")

	// 对照组：非 medium 的存量行必须**一字不动**。
	// 少了这条，「把 up 的 WHERE 去掉、全表一律改成 normal」也会全绿 —— 迁移的实际语义
	// 是「只归一这一个已知同义词」，这条断言才是它的守门人。
	assert.Equal(t, "high", readPriority(t, "LEGACY-M16-2"),
		"000023 只该动 medium 行 —— high 行被改说明 WHERE 丢了或写宽了")

	// 全表兜底：迁移跑完后不该再有契约词表外的值。种子/夹具漏改一处就会在这里现形，
	// 不必等运维报「筛选少了几张票」。
	// 非空前提：tickets 一行都没有时，下面两条 `NOT IN` 兜底恒真 —— 空库上的假绿
	// （同本文件 TestDBSmoke_AssetJSONBBackfill 的「把迁移删掉也是绿」那类坑）。
	var total int64
	require.NoError(t, db.Raw(`SELECT count(*) FROM tickets`).Scan(&total).Error)
	require.NotZero(t, total, "tickets 表为空 —— 词表兜底断言会退化成恒真, 不再守任何东西")

	var off int64
	require.NoError(t, db.Raw(
		`SELECT count(*) FROM tickets WHERE priority NOT IN ('low','normal','high','critical')`).
		Scan(&off).Error)
	assert.Zero(t, off, "tickets.priority 出现 openapi Ticket.priority enum 之外的值")

	// M18 扩展：status 是同一类缺陷的另一半 —— openapi Ticket.status enum 五个值，
	// DB 里同样没有 CHECK 约束。词表外的状态在工单页也是**静默消失**：筛选器只有这
	// 五档选不中它、TicketStatsCards 的 `if (t.status in acc)` 不计数。
	// 这里守的是**存量**（种子/夹具/迁移写歪了会现形）；新增写入由 TicketService
	// 的 validateTicketEnumValues 拦（service 单测覆盖），两条一起才是完整防线。
	require.NoError(t, db.Raw(
		`SELECT count(*) FROM tickets WHERE status NOT IN ('open','in_progress','pending','resolved','closed')`).
		Scan(&off).Error)
	assert.Zero(t, off, "tickets.status 出现 openapi Ticket.status enum 之外的值")
}

// alertSmokeState 读回一行 alerts 的状态列。
func alertSmokeState(t *testing.T, db *gorm.DB, id uuid.UUID) (status, ackUser, resolveUser string, ackTime, resolveTime *time.Time) {
	t.Helper()
	var row struct {
		Status      string
		AckUser     string
		ResolveUser string
		AckTime     *time.Time
		ResolveTime *time.Time
	}
	require.NoError(t, db.Raw(
		`SELECT status, ack_user, resolve_user, ack_time, resolve_time FROM alerts WHERE id = ?`, id).
		Scan(&row).Error, "读回 alerts 行失败")
	return row.Status, row.AckUser, row.ResolveUser, row.AckTime, row.ResolveTime
}

// TestDBSmoke_AlertBulkTransitionGuards 守 M19：批量确认/解决只能动**合法源状态**的行。
//
// 单测里那条 `WHERE id IN (...) AND status IN (...)` 只是正则匹配 SQL 文本，它证明不了
// Postgres 真按这个条件筛行 —— 语义只有真库能证明。三条断言各对应一个真实后果：
//
//	① 已 resolved 的行不能被批量「确认」拉回 acknowledged（会掉出 dashboard 的
//	   ResolvedAlerts 计数、重新落进待处理桶，还会多发一条「已确认」通知）；
//	② 已 resolved 的行不能被批量「解决」重写 resolve_time（MTTR = AVG(resolve_time −
//	   problem_start) 随之虚高）；已 acknowledged 的行也不能被重写 ack_time（MTTD 虚高）；
//	③ affected 必须如实报数，而不是「传了几个 id 就报几」—— UI 拿它显示「成功 N 条」。
//
// 放在 TestDBSmoke_DownPreservesLegacyColumns 之前：Go 按源码顺序跑用例，那个用例会
// 回滚 000013。本用例自建自清（t.Cleanup 删掉插入的行），不扰动后面的索引 EXPLAIN 断言。
func TestDBSmoke_AlertBulkTransitionGuards(t *testing.T) {
	db := openSmokeDB(t)
	svc := service.NewAlertService(db)
	ctx := context.Background()

	t0 := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	var inserted []uuid.UUID
	t.Cleanup(func() {
		if len(inserted) > 0 {
			db.Where("id IN ?", inserted).Delete(&models.Alert{})
		}
	})

	mk := func(status string) uuid.UUID {
		a := &models.Alert{
			AlertID:      "m19-" + uuid.NewString()[:8],
			HostName:     "smoke-m19",
			TriggerName:  "M19 状态守卫",
			Severity:     3,
			Problem:      "M19 dbsmoke",
			ProblemStart: t0,
			Status:       status,
		}
		require.NoError(t, db.Create(a).Error, "插入 alerts 夹具失败")
		inserted = append(inserted, a.ID)
		return a.ID
	}

	problemID := mk("problem")
	ackedID := mk("acknowledged")
	resolvedID := mk("resolved")

	// 给「已经处理过」的两行各钉一个旧时间戳：幂等路径若不挡，这两个值会被推到 now。
	require.NoError(t, db.Model(&models.Alert{}).Where("id = ?", ackedID).
		Updates(map[string]interface{}{"ack_time": t0, "ack_user": "orig-ack"}).Error)
	require.NoError(t, db.Model(&models.Alert{}).Where("id = ?", resolvedID).
		Updates(map[string]interface{}{"resolve_time": t0, "resolve_user": "orig-resolve"}).Error)

	ids := []string{problemID.String(), ackedID.String(), resolvedID.String()}

	// ---- ① 批量确认：只有 problem 那行该被改 ----
	affected, err := svc.BulkAcknowledge(ctx, ids, "m19-user")
	require.NoError(t, err)
	assert.Equal(t, int64(1), affected,
		"affected 必须如实报数（3 个 id 里只有 1 行合法），UI 直接拿它显示「成功 N 条」")

	st, _, _, _, _ := alertSmokeState(t, db, problemID)
	assert.Equal(t, "acknowledged", st)
	st, ackUser, _, ackTime, _ := alertSmokeState(t, db, ackedID)
	assert.Equal(t, "acknowledged", st)
	assert.Equal(t, "orig-ack", ackUser, "批量确认不得改写已确认行的认领人")
	require.NotNil(t, ackTime, "ack_time 被清空了")
	assert.True(t, ackTime.Equal(t0), "重复确认把 ack_time 推到 now → MTTD 虚高，实际 %v", ackTime)

	st, _, _, _, resolveTime := alertSmokeState(t, db, resolvedID)
	assert.Equal(t, "resolved", st,
		"已解决的告警被批量确认拉回 acknowledged —— 会掉出 ResolvedAlerts 计数并重新落进待处理桶")
	require.NotNil(t, resolveTime, "resolve_time 被清空了")

	// ---- ② 批量解决：problem/acknowledged 两行该被改，resolved 那行不许动 ----
	affected, err = svc.BulkResolve(ctx, ids, "m19-user")
	require.NoError(t, err)
	assert.Equal(t, int64(2), affected, "3 个 id 里只有 2 行处于可解决状态")

	var resolveUser string
	st, _, resolveUser, _, resolveTime = alertSmokeState(t, db, resolvedID)
	assert.Equal(t, "resolved", st)
	assert.Equal(t, "orig-resolve", resolveUser, "重复解决不得改写解决人")
	assert.True(t, resolveTime.Equal(t0),
		"重复解决把 resolve_time 推到 now → MTTR(AVG(resolve_time − problem_start)) 虚高，实际 %v", resolveTime)

	for _, id := range []uuid.UUID{problemID, ackedID} {
		st, _, _, _, resolveTime = alertSmokeState(t, db, id)
		assert.Equal(t, "resolved", st)
		require.NotNil(t, resolveTime, "合法解决路径必须写上 resolve_time")
	}
}

// TestDBSmoke_AlertStatusDefault 守 M20：alerts.status 的**库默认值**必须是契约里的初始态。
//
// 为什么只有真库能守：sqlmock 完全没有「列默认值」这回事，库默认值与模型 tag 之间的
// 漂移它永远看不见。**实测出的机理**（见迁移 000024 的注释）：GORM 对带 `default:` tag 的
// 零值字段，是 Create 时用它自己解析的 tag 值替代，**不省略该列、也不吃库默认值**。
// 于是两条路各自独立：GORM 路靠 tag 兜，裸 SQL 路靠库默认值兜 —— 本用例分别钉住它们。
//
// 三条断言，一条比一条靠近真实路径：
//
//	① information_schema 里列的默认值就是 problem（迁移真的改了它，且没退回 firing）；
//	② 裸 SQL 漏设 status 插入 → 落 problem（**库默认值本身可达**，这是 000024 的靶心）；
//	③ 走 GORM `db.Create` 漏设 Status → 落 problem（活路径：模型 tag 与契约一致）。
//	   ③ 不依赖 000024：把 up 换成 SELECT 1 它照样绿（已实测）。留着是因为它钉住
//	   「tag 声明 == 落库值 == 契约初始态」这三者不脱节，且未来 GORM 若改成送空串，
//	   红的是这里 —— 那正是本用例想提前知道的。
//
// 放在 TestDBSmoke_DownPreservesLegacyColumns 之前：那个用例一路回滚到 000013，
// 本用例依赖 000024 设下的默认值。自建自清，不扰动后面的索引 EXPLAIN 断言。
func TestDBSmoke_AlertStatusDefault(t *testing.T) {
	db := openSmokeDB(t)

	var applied int64
	require.NoError(t, db.Raw(`SELECT count(*) FROM schema_migrations WHERE version = 24`).
		Scan(&applied).Error)
	if applied == 0 {
		t.Fatalf("库未应用到 000024（alerts.status 默认值对齐）—— 本用例前置不满足")
	}

	// ① 列默认值
	var def *string
	require.NoError(t, db.Raw(
		`SELECT column_default FROM information_schema.columns
		  WHERE table_name = 'alerts' AND column_name = 'status'`).Scan(&def).Error)
	require.NotNil(t, def, "alerts.status 没有默认值了 —— 000024 没生效？")
	assert.Contains(t, *def, "problem",
		"库默认值不是 problem —— 漏设 status 的写入方会落一个前端不认的状态：%s", *def)
	assert.NotContains(t, *def, "firing",
		"库默认值仍是 000001 的 firing —— 前端 getAlertActions 对它一个按钮都不给")

	var ids []uuid.UUID
	t.Cleanup(func() {
		if len(ids) > 0 {
			db.Where("id IN ?", ids).Delete(&models.Alert{})
		}
	})

	// ② 裸 SQL 漏设 status（写入方最原始的那条路）
	rawID := uuid.New()
	require.NoError(t, db.Exec(
		`INSERT INTO alerts (id, alert_id, severity) VALUES (?, ?, ?)`,
		rawID, "m20-raw", 3).Error)
	ids = append(ids, rawID)
	st, _, _, _, _ := alertSmokeState(t, db, rawID)
	assert.Equal(t, "problem", st, "裸 SQL 漏设 status 应落库默认值 problem")

	// ③ GORM 活路径漏设 Status
	viaORM := &models.Alert{AlertID: "m20-orm", Severity: 3, Problem: "M20 dbsmoke"}
	require.NoError(t, db.Create(viaORM).Error)
	ids = append(ids, viaORM.ID)
	st, _, _, _, _ = alertSmokeState(t, db, viaORM.ID)
	assert.Equal(t, "problem", st,
		"GORM 零值路径漏设 Status 落成 %q —— 模型 tag 声明的是 problem，两边必须一致", st)
}

// TestDBSmoke_TicketHistory 000025 建出来的表在**真 Postgres** 上确实是我们要的形状。
//
// 为什么必须上真库：`ticket_history` 的几条关键属性**在 sqlite 单测基座里根本不存在**
//
//	· `gen_random_uuid()` 默认值 —— sqlite 没有这个函数（单测库是手写 CREATE TABLE，不走 DDL）；
//	· `ON DELETE CASCADE` —— sqlite 要 PRAGMA foreign_keys=ON 才生效；
//	· `actor_id` 上**没有**外键 —— 「没有」这种否定性事实只有查 pg 的系统表才说得清；
//	· 索引是否真建出来 —— drift 测试只比对列，不看索引。
//
// 这几条又都直接支撑 M25 的设计决策（见 docs/FIX-PLAN-TICKET-HISTORY.md §2.1/§2.4），
// 全靠「迁移文件里写了」当保证是不合格的。
//
// 放在 TestDBSmoke_DownPreservesLegacyColumns 之前：那个用例一路回滚到 000013，会拆掉本表。
func TestDBSmoke_TicketHistory(t *testing.T) {
	db := openSmokeDB(t)

	var applied int64
	require.NoError(t, db.Raw(`SELECT count(*) FROM schema_migrations WHERE version = 25`).
		Scan(&applied).Error)
	if applied == 0 {
		t.Fatalf("库未应用到 000025（ticket_history）—— 本用例前置不满足")
	}

	// ① 列集合与模型一致（模型侧由 tests/schema_drift_test.go 比对，两边合成一个闭环）
	var cols []string
	require.NoError(t, db.Raw(
		`SELECT column_name FROM information_schema.columns
		  WHERE table_name = 'ticket_history'`).Scan(&cols).Error)
	assert.ElementsMatch(t, []string{
		"id", "ticket_id", "batch_id", "kind", "field_name", "old_value", "new_value",
		"actor_id", "actor_name", "source", "request_id", "created_at",
	}, cols, "ticket_history 的实际列与模型/契约不符")

	// ② 读路径的索引必须真建出来（drift 只比对列，不看索引）
	var idx int64
	require.NoError(t, db.Raw(
		`SELECT count(*) FROM pg_indexes
		  WHERE tablename = 'ticket_history' AND indexname = 'idx_ticket_history_ticket'`).
		Scan(&idx).Error)
	assert.Equal(t, int64(1), idx, "读路径固定是「某张票的最新在前」，缺索引就是全表扫")

	// ③ actor_id **不得**有外键 —— 这是 M25「历史插入失败即整单回滚」策略敢成立的前提。
	// 带 FK 的话，带外删号后未过期 token 仍带 user_id（middleware/auth.go 只信 claims、
	// 不复查用户存在），该用户此后每次改工单都会 500。
	var actorFK int64
	require.NoError(t, db.Raw(`
		SELECT count(*) FROM information_schema.key_column_usage kcu
		JOIN information_schema.table_constraints tc
		  ON tc.constraint_name = kcu.constraint_name AND tc.table_name = kcu.table_name
		WHERE kcu.table_name = 'ticket_history' AND kcu.column_name = 'actor_id'
		  AND tc.constraint_type = 'FOREIGN KEY'`).Scan(&actorFK).Error)
	assert.Zero(t, actorFK,
		"actor_id 上出现了外键 —— 会把「记不上历史」变成「用户改不了工单」，见 FIX-PLAN §2.1")

	// ④ ticket_id 的删除规则是 CASCADE（**已知取舍**，不是疏漏）：当前无工单删除端点故不可达，
	// 将来新增删除端点时这条断言会提醒「历史会跟着一起没」。
	var delRule string
	require.NoError(t, db.Raw(`
		SELECT rc.delete_rule FROM information_schema.referential_constraints rc
		JOIN information_schema.key_column_usage kcu
		  ON kcu.constraint_name = rc.constraint_name AND kcu.table_name = 'ticket_history'
		WHERE kcu.column_name = 'ticket_id'`).Scan(&delRule).Error)
	assert.Equal(t, "CASCADE", delRule,
		"ticket_id 的 ON DELETE 规则变了 —— 历史与工单的生命周期绑定是有意选的，改动要同步文档")
}

// TestDBSmoke_TicketResolvedAt resolved_at 的三态转移表在**真 Postgres** 上确实成立。
//
// 为什么必须上真库：这条 CASE 把「调用方没给值」表达成 **nil 参数**
// （`COALESCE($2, resolved_at, $3)`）。sqlite 基座对 nil 参数照单全收，真库要先做
// 参数类型推断 —— 推不出来就是 42P18 `could not determine data type of parameter $2`，
// 单测永远看不见。同理「CASE 把它写成 NULL」之后读回来到底是 NULL 还是零值时间、
// 时间列走一遍 COALESCE 之后精度剩多少，也只有真驱动说了算。
//
// 四条断言各对应设计文档 §2.5 的一格：
//  1. 进入 resolved → now（COALESCE 第三个参数真被用上）
//  2. resolved → closed 保留（走 `COALESCE($2, resolved_at)` 且 $2 为 nil 那一支）
//  3. 重开 → NULL（`ELSE NULL` 落在一张时间列上）
//  4. 被清掉的值进历史（old_value 有值 / new_value 为 NULL）
func TestDBSmoke_TicketResolvedAt(t *testing.T) {
	db := openSmokeDB(t)
	svc := service.NewTicketService(db)
	ctx := context.Background()
	actor := service.Actor{Name: "dbsmoke"}

	tk := &models.Ticket{ //nolint:exhaustruct
		Title: "dbsmoke resolved_at", TicketType: "incident", Priority: "low", Status: "open",
	}
	require.NoError(t, svc.Create(ctx, tk, actor), "建单失败 —— Create 在真库上没走通")

	// ① open → resolved
	got, err := svc.Update(ctx, tk.ID.String(), map[string]interface{}{"status": "resolved"}, actor)
	require.NoError(t, err, "status→resolved 的 CASE 在真库上求值失败（多半是 nil 参数推不出类型）")
	require.NotNil(t, got.ResolvedAt, "进入 resolved 必须落下解决时刻")
	resolvedAt := *got.ResolvedAt

	// ② resolved → closed：保留解决时刻（已关闭的票 MTTR 靠它）
	got, err = svc.Update(ctx, tk.ID.String(), map[string]interface{}{"status": "closed"}, actor)
	require.NoError(t, err, "status→closed 的 CASE 在真库上求值失败")
	require.NotNil(t, got.ResolvedAt, "resolved→closed 丢了解决时刻，已关闭的票 MTTR 会归零")
	assert.True(t, resolvedAt.Equal(*got.ResolvedAt), "解决时刻被改写: %v → %v", resolvedAt, *got.ResolvedAt)

	// ③ 重开 → NULL。判据走 SQL 而不是 Go 侧指针：指针为 nil 只说明「没读出来」，
	// 分不出「列是 NULL」和「驱动把零值时间读成了 nil」。
	_, err = svc.Update(ctx, tk.ID.String(), map[string]interface{}{"status": "open"}, actor)
	require.NoError(t, err, "重开的 CASE 在真库上求值失败")
	var isNull bool
	require.NoError(t, db.Raw(`SELECT resolved_at IS NULL FROM tickets WHERE id = ?`, tk.ID).
		Scan(&isNull).Error)
	assert.True(t, isNull, "重开后 resolved_at 必须真的是 NULL，否则时间线上永久挂着一条「已解决」")

	// ④ 被清掉的值进历史。不按下标取行：同一秒内的两行没有可靠顺序（T-45），
	// 按「new_value 为 NULL 的那一行」定位，顺带把「清空」与「写入」两种行都覆盖到。
	var hist []struct{ OldValue, NewValue *string }
	require.NoError(t, db.Raw(`
		SELECT old_value, new_value FROM ticket_history
		 WHERE ticket_id = ? AND field_name = 'resolved_at'`, tk.ID).Scan(&hist).Error)
	require.NotEmpty(t, hist, "resolved_at 的两次变更在真库上没有留下历史行")

	var cleared bool
	for _, h := range hist {
		if h.NewValue != nil {
			continue
		}
		cleared = true
		require.NotNil(t, h.OldValue, "清空那一行的 old_value 不能为空 —— 否则「谁在何时解决的」永久消失")
		assert.Equal(t, resolvedAt.UTC().Format(time.RFC3339Nano), *h.OldValue,
			"被清掉的时间必须原样进 old_value")
	}
	assert.True(t, cleared, "清空 resolved_at 没有留下 new_value 为 NULL 的历史行（空串不算 NULL）")
}

// TestDBSmoke_TicketsGLPIExternalIDUnique 守 000026 —— M26/D-4 的 ON CONFLICT 仲裁者。
//
// 为什么必须上真库：部分唯一索引是**迁移产物**，sqlite 单测用的是手写 DDL；而
// `ON CONFLICT (external_id) WHERE ...` 能否仲裁完全取决于生产库上索引的谓词是否逐字一致，
// 差一个字符就是 42P10「there is no unique or exclusion constraint matching the
// ON CONFLICT specification」—— 每一次 GLPI 同步都 500，而单测全绿。
//
// 覆盖三件事：
//
//	① 索引存在、是 UNIQUE、且**带 WHERE**（少了 WHERE 就退化成全表唯一，
//	   人工建单那些 external_id='' 的票会互相撞，第二张票直接插不进去）；
//	② 两条同 external_id 的 glpi 票 → 第二条 23505；
//	③ 两条同 external_id 的 manual 票 → **不冲突**（谓词把非 glpi 排除在外）。
func TestDBSmoke_TicketsGLPIExternalIDUnique(t *testing.T) {
	db := openSmokeDB(t)

	var applied int64
	if err := db.Raw(`SELECT count(*) FROM schema_migrations WHERE version = 26`).
		Scan(&applied).Error; err != nil {
		t.Fatalf("读取 schema_migrations 失败:\n%v", err)
	}
	if applied == 0 {
		t.Fatalf("库未应用到 000026（tickets glpi external_id 部分唯一索引）—— 本用例前置不满足")
	}

	// ① 索引形态
	var indexdef string
	require.NoError(t, db.Raw(
		`SELECT indexdef FROM pg_indexes WHERE indexname = 'uq_tickets_glpi_external_id'`).
		Scan(&indexdef).Error, "uq_tickets_glpi_external_id 不存在 —— 000026 没生效")
	assert.Contains(t, indexdef, "UNIQUE", "必须是唯一索引，否则 ON CONFLICT 直接 42P10: %s", indexdef)
	assert.Contains(t, indexdef, "WHERE", "必须是**部分**索引 —— 全表唯一会让所有 external_id='' 的人工票互相冲突: %s", indexdef)

	const dupExt = "990016-m26-smoke"
	t.Cleanup(func() {
		// 自建自清：本用例插的行不能留给后面的用例（尤其 DownPreservesLegacyColumns
		// 要回滚 000026，库里留着重复行会让那次 Up 失败）。
		_ = db.Exec(`DELETE FROM tickets WHERE external_id = ?`, dupExt).Error
	})

	// ② glpi 重复必须被拒
	// 走 models.Ticket + gorm.Create（而不是手写 INSERT）：真库的 tickets 有
	// ticket_no/ticket_type/creator_id 等 NOT NULL 列，手抄列清单就是给自己埋一次漂移。
	// 这条路径同时也是生产建单路径，一举两得。
	insertTicket := func(source string) error {
		return db.Create(&models.Ticket{
			Title: "m26 smoke", TicketType: "incident", Priority: "normal",
			Status: "open", Source: source, ExternalID: dupExt,
		}).Error
	}
	require.NoError(t, insertTicket("glpi"))
	err := insertTicket("glpi")
	require.Error(t, err, "同 external_id 的第二条 glpi 票必须被唯一索引拒绝 —— 否则 ON CONFLICT 无仲裁者")
	assert.Contains(t, strings.ToLower(err.Error()), "duplicate key", "应是唯一冲突: %v", err)

	// ③ 非 glpi 来源不受约束（谓词 source='glpi' 的边界）
	require.NoError(t, insertTicket("manual"),
		"manual 来源的 external_id 必须不受此索引约束 —— 谓词写宽了会把人工建单一起管住")
	require.NoError(t, insertTicket("manual"),
		"第二条同 external_id 的 manual 票也必须能插 —— 部分索引的谓词没生效？")
}

// TestDBSmoke_Migration026BlockedByDuplicates 守 000026 的**前置自检**（D-5 fail-closed）。
//
// 库里有重复的 glpi external_id 时，CREATE UNIQUE INDEX 必然失败 —— 但 PG 原生 23505
// 的 DETAIL 只有一行「Key (external_id)=(...) already exists」，不含「还有哪几行重了」，
// 而迁移失败会让版本记录不落、每次重启重放、服务持续不可用。所以 up 里加了 DO 自检，
// 把重复的 external_id **样本**写进异常文本。
//
// 本用例走的是「升级到一个脏库」这条真实路径：先滚掉索引 → 造重复行 → Up 必须失败。
// 三步缺一不可 —— 少了第①步（不滚索引）第②步就插不进去，整个用例变成空转。
func TestDBSmoke_Migration026BlockedByDuplicates(t *testing.T) {
	db := openSmokeDB(t)

	var applied int64
	if err := db.Raw(`SELECT count(*) FROM schema_migrations WHERE version = 26`).
		Scan(&applied).Error; err != nil {
		t.Fatalf("读取 schema_migrations 失败:\n%v", err)
	}
	if applied == 0 {
		t.Fatalf("库未应用到 000026 —— 本用例要「回滚索引 → 造重复 → Up 必须失败」，前置不满足")
	}

	migrate.FS = network_monitor_platform.MigrationsFS

	// ① 滚掉 000026（以及它之上的一切），制造出「可以插重复行」的窗口。
	// 先把 26 以上的层循环滚光，而不是写死「多 Down 一次」：migrate.Down 只滚**最新已应用**
	// 那一层，每新增一个迁移本用例就要再改一次，而漏改的症状是下面 require.True(idxGone)
	// 变红、错误信息指向「down 000026 没删掉索引」这个**错误方向**（M27 加 000027 时实测）。
	for {
		var above int64
		require.NoError(t, db.Raw(`SELECT count(*) FROM schema_migrations WHERE version > 26`).
			Scan(&above).Error, "读取 26 以上已应用层数失败")
		if above == 0 {
			break
		}
		require.NoError(t, migrate.Down(db), "回滚 26 以上的迁移失败（剩 %d 层）", above)
	}
	require.NoError(t, migrate.Down(db), "回滚 000026 失败")
	var idxGone bool
	require.NoError(t, db.Raw(
		`SELECT NOT EXISTS (SELECT 1 FROM pg_indexes WHERE indexname = 'uq_tickets_glpi_external_id')`).
		Scan(&idxGone).Error)
	require.True(t, idxGone, "down 000026 没删掉索引 —— 下一步插重复行会直接失败，用例变空转")

	const dupExt = "990026-m26-dup"
	// 收尾必须把库恢复成「000026 已应用 + 无重复行」。不做的话：
	//   · 同进程后面的 TestDBSmoke_DownPreservesLegacyColumns 前置 3 直接 Fatal（一个红变一片红）；
	//   · 更糟的是库留在「索引没了」的状态，而那个状态在运行时就是 GLPI 同步全 500。
	// 用 defer 而不是 t.Cleanup：t.Cleanup 在 Fatal 时也跑，defer 同样跑，但这里
	// 关键顺序（先删重复行再 Up）用 defer 更直白。
	defer func() {
		if err := db.Exec(`DELETE FROM tickets WHERE external_id = ?`, dupExt).Error; err != nil {
			t.Errorf("清理重复行失败，库已留在脏状态: %v", err)
			return
		}
		if err := migrate.Up(db); err != nil {
			t.Errorf("复原 000026 失败，库已留在「索引缺失」状态（运行时=GLPI 同步全 500）: %v", err)
		}
	}()

	// ② 插入两行同 external_id 的 glpi 票（此时没有索引，插得进去）
	for i := 0; i < 2; i++ {
		require.NoError(t, db.Create(&models.Ticket{
			Title: "m26 dup", TicketType: "incident", Priority: "normal",
			Status: "open", Source: "glpi", ExternalID: dupExt,
		}).Error, "第 %d 条重复行插入失败 —— 索引不是真被删掉了？", i+1)
	}

	// ③ Up 必须失败，且异常文本必须点名重复的 external_id（这正是 DO 自检存在的理由）
	err := migrate.Up(db)
	require.Error(t, err, "库里有重复 glpi external_id 时 000026 必须失败（fail-closed），绝不能静默跳过建索引")
	assert.Contains(t, err.Error(), "存在重复的 glpi external_id",
		"异常文本必须是 DO 自检抛的那条 —— 只有 PG 原生的 23505 说明自检被删了，"+
			"运维拿不到「哪几个 ID 重了」: %v", err)
	assert.Contains(t, err.Error(), dupExt, "自检必须把重复的 external_id 样本带进异常文本")
}

// TestDBSmoke_GLPITimeZoneWallClock 守 M26 的**时区转换**在真驱动上确实生效（§7 R1）。
//
// 为什么 sqlite 用例守不住它（实测，见 IMPL §4 基座注意）：pgx 对 TIMESTAMP（无时区）列
// 写入 time.Time 时**丢弃 Location、只写挂钟数字**；sqlite 反过来，把偏移一起写进字符串
// 再原样还原。于是「有没有调 .UTC()」在 sqlite 上两种写法都读回同一个值 ——
// 纯函数层的 TestParseGLPITime_时区 也只能钉到 `Location()==UTC`，钉不到落库结果。
// 只有真 PG 能回答「库里的挂钟数字到底是不是 02:00」。
//
// 走的是**真调用点**（真 GLPIClient → 真 SyncFromGLPI → 真 PG），不是直接调解析函数：
// 直接调会绕开「解析结果有没有一路传到 created_at」这段管道，那只剩半条防线。
//
// 变异反证：去掉 parseGLPITime 里的 .UTC() → 落库变 10:00:00，本用例必红。
func TestDBSmoke_GLPITimeZoneWallClock(t *testing.T) {
	db := openSmokeDB(t)

	var applied int64
	if err := db.Raw(`SELECT count(*) FROM schema_migrations WHERE version = 26`).
		Scan(&applied).Error; err != nil {
		t.Fatalf("读取 schema_migrations 失败:\n%v", err)
	}
	if applied == 0 {
		t.Fatalf("库未应用到 000026 —— SyncFromGLPI 的 ON CONFLICT 会 42P10，本用例前置不满足")
	}

	// GLPI 的 id 是整数，ConvertToTicket 用 fmt.Sprintf("%d") 转成 external_id，
	// 所以常量必须与下面 fixture 里的 "id" 逐字一致（不一致时查询落空 → 断言红，
	// 但错误信息会指向「时区算错」这个错误方向）。
	const extID = "990018"
	t.Cleanup(func() {
		_ = db.Exec(`DELETE FROM tickets WHERE source = 'glpi' AND external_id = ?`, extID).Error
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/initSession"):
			_, _ = w.Write([]byte(`{"session_token":"smoke-sess"}`))
		case strings.Contains(r.URL.Path, "/Ticket"):
			// GLPI 返回的是**挂钟**（无偏移），10:00 Asia/Shanghai == 02:00 UTC
			_, _ = w.Write([]byte(`[{"id":990018,"name":"m26 tz","content":"x","status":1,"priority":3,"date":"2026-06-15 10:00"}]`))
		default:
			t.Errorf("未预期的 GLPI 请求: %s", r.URL.Path)
			w.WriteHeader(400)
		}
	}))
	defer srv.Close()

	oldDB := database.GetDB()
	database.SetDBForTest(db)
	defer database.SetDBForTest(oldDB)

	svc := integration.NewIntegrationService(&config.Config{
		Integrations: config.IntegrationsConfig{
			GLPI: config.GLPIConfig{URL: srv.URL, AppToken: "a", UserToken: "u"},
		},
	}, nil)

	n, skipped, err := svc.SyncFromGLPI(context.Background())
	require.NoError(t, err, "真 PG 上 SyncFromGLPI 失败 —— 000026 的部分唯一索引没生效（42P10）？")
	require.Equal(t, 1, n)
	require.Equal(t, 0, skipped)

	// 逐字断言落库文本：SQLite 基座下这里会是 "2026-06-15 10:00:00"（偏移被保留又还原），
	// 真 PG 下若少了 .UTC() 也会是 10:00:00 —— 那正好是我们要抓的 8 小时偏移。
	var got string
	require.NoError(t, db.Raw(
		`SELECT created_at::text FROM tickets WHERE source = 'glpi' AND external_id = ?`, extID).
		Scan(&got).Error)
	assert.Equal(t, "2026-06-15 02:00:00", got,
		"GLPI 的 10:00(Asia/Shanghai) 落到 TIMESTAMP 列必须是 02:00 UTC —— "+
			"10:00 说明 .UTC() 没了（全库时间偏 8 小时）；别的值说明时区名写错了")
}

// TestDBSmoke_AlertsZabbixIdentityUnique 守 000027 在**真 Postgres** 上建出来的索引形态与谓词边界。
//
// 为什么必须上真库：sqlite 单测基座是手写 DDL（upsertTestSchema），它与 000027 是两份
// 独立文本 —— 那边绿只证明「我以为的索引」可用，证明不了**迁移真建出来的那份**能用。
// 而 PG 是 ON CONFLICT 的唯一判据（谓词差一个字符 → 42P10，每一次 Zabbix 同步 500，
// 而全部单测照样绿）。
//
// 覆盖三件事：
//
//	① 索引存在、是 UNIQUE、且**带 WHERE 谓词**（谓词写宽成全表唯一，人工/其它来源的
//	   同 trigger 行会互相撞，第二条直接插不进去）；
//	② 两条同身份（trigger_id + problem_start）的 zabbix 行 → 第二条 23505
//	   —— 这正是 ON CONFLICT 的仲裁者，没有它 service.go 的 TargetWhere 就是 42P10；
//	③ 谓词两处收窄各有一条反向用例：source='manual' 同键不冲突、trigger_id='' 同键不冲突。
//	   再加 problem_start IS NULL 的多行共存（PG 唯一索引视 NULL 互不相等）。
func TestDBSmoke_AlertsZabbixIdentityUnique(t *testing.T) {
	db := openSmokeDB(t)

	var applied int64
	require.NoError(t, db.Raw(`SELECT count(*) FROM schema_migrations WHERE version = 27`).
		Scan(&applied).Error)
	if applied == 0 {
		t.Fatalf("库未应用到 000027 —— 本用例前置不满足")
	}

	// ① 索引形态。断言的是 pg_indexes.indexdef 的**实测文本**，不是迁移文件里的源码字面：
	// PG 会把谓词规范化成 `(source)::text = 'zabbix'::text`、`(trigger_id)::text <> ''::text`，
	// 照抄源码字面会永远红（红在断言写法上，不是红在漂移上）。
	var indexdef string
	require.NoError(t, db.Raw(
		`SELECT indexdef FROM pg_indexes WHERE indexname = 'uq_alerts_zabbix_identity'`).
		Scan(&indexdef).Error, "uq_alerts_zabbix_identity 不存在 —— 000027 没生效")
	assert.Contains(t, indexdef, "UNIQUE", "必须是唯一索引，否则 ON CONFLICT 无仲裁者: %s", indexdef)
	assert.Contains(t, indexdef, "WHERE", "必须是**部分**索引 —— 全表唯一会让非 zabbix 的同键行互相冲突: %s", indexdef)
	assert.Contains(t, indexdef, "(trigger_id, problem_start)", "仲裁列必须是这两列: %s", indexdef)
	assert.Contains(t, indexdef, "trigger_id IS NOT NULL", "谓词少了 IS NOT NULL（与 service.go 的 TargetWhere 会分叉）: %s", indexdef)
	assert.Contains(t, indexdef, "<> ''::text", "谓词少了 trigger_id <> '': %s", indexdef)
	assert.Contains(t, indexdef, "= 'zabbix'::text", "谓词少了 source = 'zabbix': %s", indexdef)

	// 自建自清：本用例插的行不能留给后面的用例（尤其 DownPreservesLegacyColumns 会回滚
	// 000013 拆表，以及 Migration027BlockedByDuplicates 要造重复行 —— 库里留着额外行
	// 会让那次自检在**不该报**的时候报出来）。按 alert_id 前缀删，不按 trigger_id：
	// ③ 里有一行的 trigger_id 就是空串，按它删会波及别的用例的行。
	t.Cleanup(func() {
		_ = db.Exec(`DELETE FROM alerts WHERE alert_id LIKE 'm27smoke%'`).Error
	})

	const key = "990027-m27-smoke"
	const start = int64(1756728000)
	mk := func(alertID, source, triggerID string) *models.Alert {
		return &models.Alert{
			AlertID: alertID, Source: source, TriggerID: triggerID,
			TriggerName: "m27 smoke", HostName: "web-01", Severity: 5, Status: "problem",
			ProblemStart: time.Unix(start, 0).UTC(),
		}
	}

	// ② zabbix 同身份必须被拒
	require.NoError(t, db.Create(mk("m27smoke-z1", "zabbix", key)).Error)
	err := db.Create(mk("m27smoke-z2", "zabbix", key)).Error
	require.Error(t, err, "同身份的第二个 zabbix 告警必须被唯一索引拒绝 —— 否则 ON CONFLICT 无仲裁者")
	assert.Contains(t, err.Error(), "23505", "应带 SQLSTATE 23505: %v", err)

	// ③a 非 zabbix 来源不受约束（谓词 source='zabbix' 的边界）
	require.NoError(t, db.Create(mk("m27smoke-m1", "manual", key)).Error,
		"manual 来源的同键行必须不受此索引约束 —— 谓词写宽了会把人工告警一起管住")

	// ③b trigger_id='' 不受约束（谓词 trigger_id <> '' 的边界）
	require.NoError(t, db.Create(mk("m27smoke-e1", "zabbix", "")).Error)
	require.NoError(t, db.Create(mk("m27smoke-e2", "zabbix", "")).Error,
		"空 trigger_id 的第二行也必须能插 —— 少了 <> '' 会在这里红")

	// ④ problem_start IS NULL 的行不参与唯一性（PG 视 NULL 互不相等）。
	// 走裸 SQL 是本文件「不手写 INSERT」惯例的**唯一例外**：models.Alert.ProblemStart 是
	// 非指针 time.Time，GORM 写不出 NULL（M26 §1.6 真 PG 实测）；这条行为又必须钉住 ——
	// 自检专门放过了 NULL 行，若哪天索引把 NULL 也算成冲突，迁移会变成「永远无法满足」。
	for i := 1; i <= 2; i++ {
		require.NoError(t, db.Exec(
			`INSERT INTO alerts (id, alert_id, source, trigger_id, severity, status)
			 VALUES (?, ?, 'zabbix', ?, 5, 'problem')`,
			uuid.New(), fmt.Sprintf("m27smoke-n%d", i), key).Error,
			"problem_start IS NULL 的第 %d 行必须能共存", i)
	}
}

// TestDBSmoke_ZabbixSyncOnConflict 是 ON CONFLICT（D-4）在**真 PG** 上的唯一防线。
//
// 为什么不能靠「同步两遍、第二遍 synced==0」：第二遍预过滤已把同一身份全剔除 →
// toInsert 为空 → 事务根本不进 → 删掉 TargetWhere 它**照样绿**（假绿）。
// 必须构造「预查之后、插入之前出现冲突行」的交错 —— 那正是 D-4 存在的唯一场景。
// 用一个一次性 Query 钩子在预过滤 Find 返回后注入该行（同 M26 的 GLPI 用例）。
//
// 断言设计成**混合批**（1 冲突 + 1 新），这是必需的而不是「多测一条」：
// 只有 1 条冲突行时，删掉 ON CONFLICT 会 23505（红），但若把 ON CONFLICT 换成
// 「RowsAffected 计数」之类就不会红；有了一条真正要插的新行，synced 才同时钉住
// 「冲突的不算新增」与「该插的确实插了」。
func TestDBSmoke_ZabbixSyncOnConflict(t *testing.T) {
	db := openSmokeDB(t)

	var applied int64
	require.NoError(t, db.Raw(`SELECT count(*) FROM schema_migrations WHERE version = 27`).
		Scan(&applied).Error)
	if applied == 0 {
		t.Fatalf("库未应用到 000027 —— SyncFromZabbix 的 ON CONFLICT 会 42P10，本用例前置不满足")
	}

	const conflictSec = 1756728000
	const newSec = 1756728060
	t.Cleanup(func() {
		_ = db.Exec(`DELETE FROM alerts WHERE alert_id LIKE 'm27smoke-sync%'`).Error
	})

	oldDB := database.GetDB()
	database.SetDBForTest(db)
	defer database.SetDBForTest(oldDB)

	// 预过滤（Find）返回后注入冲突行 —— TOCTOU 窗口的唯一入口。
	var once sync.Once
	require.NoError(t, db.Callback().Query().After("gorm:query").
		Register("m27:inject-conflict", func(tx *gorm.DB) {
			// 只在**第一次**查 alerts 时注入；之后的查询（事务里的 COUNT）不再注入，
			// 否则计数被污染，synced 断言红在夹具上而不是代码上。
			once.Do(func() {
				require.NoError(t, db.Create(&models.Alert{
					AlertID: "m27smoke-sync-injected", Source: "zabbix",
					TriggerID: "m27smoke-sync-1", TriggerName: "CPU > 90%", HostName: "web-01",
					Severity: 5, Status: "problem",
					// 必须 .UTC()：pgx 对 TIMESTAMP 列只写**挂钟数字**（T-48），而同步路径
					// 写的是 time.Unix(sec,0).UTC()。用本地时区会差 8 小时 → 两行不撞 →
					// ON CONFLICT 根本没被触发 → 假绿。
					ProblemStart: time.Unix(conflictSec, 0).UTC(),
				}).Error, "注入冲突行失败")
			})
		}))

	// 假 Zabbix：登录固定成功，trigger.get 吐两条 —— 第一条与注入行同身份（冲突），
	// 第二条全新。两条的 triggerid 都带 m27smoke-sync 前缀，收尾按前缀清得干净。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case bytes.Contains(raw, []byte(`"user.login"`)):
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":"smoke-tok","id":1}`))
		case bytes.Contains(raw, []byte(`"trigger.get"`)):
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":[` +
				`{"triggerid":"m27smoke-sync-1","description":"CPU > 90%","priority":5,` +
				`"hosts":[{"hostid":"1","host":"web-01"}],"value":"1","lastchange":"1756728000"},` +
				`{"triggerid":"m27smoke-sync-2","description":"磁盘满","priority":4,` +
				`"hosts":[{"hostid":"1","host":"web-01"}],"value":"1","lastchange":"1756728060"}` +
				`],"id":2}`))
		default:
			t.Errorf("未预期的 Zabbix 请求: %s", raw)
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	svc := integration.NewIntegrationService(&config.Config{
		Integrations: config.IntegrationsConfig{
			Zabbix: config.ZabbixConfig{URL: srv.URL, User: "admin", Password: "p"},
		},
	}, nil)

	n, truncated, err := svc.SyncFromZabbix(context.Background())
	require.NoError(t, err,
		"预查后漏进的冲突行必须被 ON CONFLICT 幂等跳过；报 23505 说明 TargetWhere/ON CONFLICT 没了")
	assert.Equal(t, 0, truncated, "两条不构成截断")
	assert.Equal(t, 1, n, "只应新增 1 条 —— 冲突那条被跳过；报 2 说明计数把跳过的那条也算进去了")

	var rows []models.Alert
	require.NoError(t, db.Where("source = 'zabbix' AND trigger_id LIKE 'm27smoke-sync%'").
		Order("trigger_id").Find(&rows).Error)
	require.Len(t, rows, 2, "注入行 + 新增 1 条 = 2 行；多出来说明 ON CONFLICT 没生效（插了重复）")
	assert.Equal(t, "m27smoke-sync-2", rows[1].TriggerID)
	// 新行必须落在源侧给的故障时刻上，而不是同步时刻 —— 顺带钉住这条管道没被改动带偏。
	assert.Equal(t, int64(newSec), rows[1].ProblemStart.Unix())
}

// rollbackTo27 把库回滚到「000027 未应用」，供 000027 的两条升级路径用例共用。
//
// 先循环滚掉 27 以上的一切，再滚 000027：migrate.Down 只滚**最新已应用**那一层，
// 写死次数的话每新增一个迁移本用例就要再改一次，而漏改的症状是下面 require.True(idxGone)
// 变红、错误信息却指向「down 000027 没删掉索引」这个**错误方向**（M27 加 000027 时在
// 000026 的同类用例上实测过）。
func rollbackTo27(t *testing.T, db *gorm.DB) {
	t.Helper()
	migrate.FS = network_monitor_platform.MigrationsFS
	for {
		var above int64
		require.NoError(t, db.Raw(`SELECT count(*) FROM schema_migrations WHERE version > 27`).
			Scan(&above).Error, "读取 27 以上已应用层数失败")
		if above == 0 {
			break
		}
		require.NoError(t, migrate.Down(db), "回滚 27 以上的迁移失败（剩 %d 层）", above)
	}
	require.NoError(t, migrate.Down(db), "回滚 000027 失败")

	var idxGone bool
	require.NoError(t, db.Raw(
		`SELECT NOT EXISTS (SELECT 1 FROM pg_indexes WHERE indexname = 'uq_alerts_zabbix_identity')`).
		Scan(&idxGone).Error)
	require.True(t, idxGone, "down 000027 没删掉索引 —— 后续步骤会空转、或红在错误方向")
}

// TestDBSmoke_Migration027AllowsNullProblemStart 守自检里的 `problem_start IS NOT NULL`
// （IMPL §8 的 M9）。
//
// PG 的唯一索引视 NULL 互不相等，所以同 trigger 的多行 NULL problem_start 建索引时
// **并不冲突**。自检若少了 `problem_start IS NOT NULL`，这些行会被算成重复 → RAISE EXCEPTION
// → 迁移被拒，而且**永远无法满足**（唯一出路是删数据 —— 而 NULL problem_start 是合法历史数据）。
// 这条路径真实存在：非指针 time.Time 写不出 NULL，裸 SQL / 早期写入方可以。
//
// 反向对照是 TestDBSmoke_Migration027BlockedByDuplicates（非 NULL 重复必须被拒）——
// 两条合起来才是「自检的粒度刚好」，缺一条就变成「要么误报、要么漏报」只看得到一半。
func TestDBSmoke_Migration027AllowsNullProblemStart(t *testing.T) {
	db := openSmokeDB(t)

	var applied int64
	require.NoError(t, db.Raw(`SELECT count(*) FROM schema_migrations WHERE version = 27`).
		Scan(&applied).Error)
	if applied == 0 {
		t.Fatalf("库未应用到 000027 —— 本用例前置不满足")
	}

	t.Cleanup(func() {
		_ = db.Exec(`DELETE FROM alerts WHERE alert_id LIKE 'm27smoke-null%'`).Error
	})

	rollbackTo27(t, db)

	// 收尾必须把 000027 重新应用上：库留在「索引没了」的状态时，运行时就是
	// 每一次 Zabbix 同步都 500，且后面 TestDBSmoke_DownPreservesLegacyColumns 的前置会 Fatal。
	defer func() {
		if err := migrate.Up(db); err != nil {
			t.Errorf("复原 000027 失败，库已留在「索引缺失」状态（运行时=Zabbix 同步全 500）: %v", err)
		}
	}()

	const nullTrigger = "990027-m27-null"
	// 裸 SQL 是本文件「不手写 INSERT」惯例的**唯一例外**：ProblemStart 是非指针 time.Time，
	// GORM 写不出 NULL。这里要的正是 NULL，所以只能裸 SQL。
	for i := 1; i <= 2; i++ {
		require.NoError(t, db.Exec(
			`INSERT INTO alerts (id, alert_id, source, trigger_id, severity, status)
			 VALUES (?, ?, 'zabbix', ?, 5, 'problem')`,
			uuid.New(), fmt.Sprintf("m27smoke-null-%d", i), nullTrigger).Error,
			"第 %d 条 NULL problem_start 行插入失败 —— 索引不是真被删掉了？", i)
	}

	require.NoError(t, migrate.Up(db),
		"同 trigger 的多行 NULL problem_start 不构成重复（PG 视 NULL 互不相等）—— "+
			"自检少了 problem_start IS NOT NULL 会在这里把合法历史数据判成重复，且迁移从此无法满足")

	var idxBack bool
	require.NoError(t, db.Raw(
		`SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE indexname = 'uq_alerts_zabbix_identity')`).
		Scan(&idxBack).Error)
	assert.True(t, idxBack, "Up 之后索引必须建出来")
}

// TestDBSmoke_Migration027BlockedByDuplicates 守 000027 的**前置自检**（D-5 fail-closed）。
//
// 库里有重复的 zabbix 身份时 CREATE UNIQUE INDEX 必然失败 —— 但 PG 原生 23505 的 DETAIL
// 只有一行「Key (trigger_id, problem_start)=(...) already exists」，不含「还有哪几个 trigger
// 重了」，而迁移失败会让版本记录不落、每次重启重放、服务持续不可用。所以 up 里加了 DO 自检，
// 把重复的 trigger_id **样本**写进异常文本。
//
// 本用例走「升级到一个脏库」这条真实路径：先滚掉索引 → 造重复行 → Up 必须失败。
// 三步缺一不可 —— 少了第①步（不滚索引）第②步就插不进去，整个用例变成空转。
func TestDBSmoke_Migration027BlockedByDuplicates(t *testing.T) {
	db := openSmokeDB(t)

	var applied int64
	require.NoError(t, db.Raw(`SELECT count(*) FROM schema_migrations WHERE version = 27`).
		Scan(&applied).Error)
	if applied == 0 {
		t.Fatalf("库未应用到 000027 —— 本用例要「回滚索引 → 造重复 → Up 必须失败」，前置不满足")
	}

	rollbackTo27(t, db)

	const dupTrigger = "990027-m27-dup"
	// 收尾必须把库恢复成「000027 已应用 + 无重复行」。不做的话：同进程后面的
	// TestDBSmoke_DownPreservesLegacyColumns 前置会 Fatal（一个红变一片红）；更糟的是
	// 库留在「索引没了」的状态，而那个状态在运行时就是每一次 Zabbix 同步都 500。
	defer func() {
		if err := db.Exec(`DELETE FROM alerts WHERE trigger_id = ?`, dupTrigger).Error; err != nil {
			t.Errorf("清理重复行失败，库已留在脏状态: %v", err)
			return
		}
		if err := migrate.Up(db); err != nil {
			t.Errorf("复原 000027 失败，库已留在「索引缺失」状态（运行时=Zabbix 同步全 500）: %v", err)
		}
	}()

	// ② 插入两行同身份的 zabbix 告警（此时没有索引，插得进去）
	for i := 1; i <= 2; i++ {
		require.NoError(t, db.Create(&models.Alert{
			AlertID: fmt.Sprintf("m27smoke-dup-%d", i), Source: "zabbix", TriggerID: dupTrigger,
			TriggerName: "m27 dup", HostName: "web-01", Severity: 5, Status: "problem",
			ProblemStart: time.Unix(1756728000, 0).UTC(),
		}).Error, "第 %d 条重复行插入失败 —— 索引不是真被删掉了？", i)
	}

	// ③ Up 必须失败，且异常文本必须点名重复的 trigger_id（这正是 DO 自检存在的理由）
	err := migrate.Up(db)
	require.Error(t, err, "库里有重复 zabbix 身份时 000027 必须失败（fail-closed），绝不能静默跳过建索引")
	assert.Contains(t, err.Error(), "存在重复的 zabbix 告警身份",
		"异常文本必须是 DO 自检抛的那条 —— 只有 PG 原生的 23505 说明自检被删了，"+
			"运维拿不到「哪几个 trigger 重了」: %v", err)
	assert.Contains(t, err.Error(), dupTrigger, "自检必须把重复的 trigger_id 样本带进异常文本")
}

// TestDBSmoke_DownPreservesLegacyColumns 回滚 000013 不得删掉 000001 就存在的列。
// down.sql 曾无条件 DROP tickets.ticket_type（up 里对它是 no-op），
// 回滚后该列与数据一起消失，且 GORM 枚举 Ticket.TicketType 会直接 500（审计 阻断-2）。
//
// 注意两点：
//  1. migrate.Down 只回滚**最新已应用版本**（internal/migrate/migrate.go:245）——
//     每新增一个迁移就要多回滚一次，否则本用例会静默变成「回滚上一层」的空转。
//     当前最高版本是 000027（000022 空缺，被 plan 里 P20 的 pg_trgm 预占、尚未落地），
//     故十四次 Down = 27 → 26 → 25 → 24 → 23 → 21 → 20 → 19 → 18 → 17 → 16 → 15 → 14 → 13。
//     **多滚 / 少滚都不会被链上断言发现**：下面全是「索引没了」的 assert.False，晚一步仍为真。
//     故本用例在**首尾各加一条正向断言**：开头钉「头一次 Down 滚的确实是 000027」，
//     结尾钉「000012 必须还在（多滚一层的唯一暴露点）」。
//     下面每一步只写「回滚 0000NN」不写序数：序数本身会随新增迁移整体后移，是这行
//     注释里最容易变成假话的部分（历史上就写重过两个「第三次」，M27 加 000027 时
//     又整体后移一位），版本号才是不变量。
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

	// 前置 3（M16/M20/M25/M26/M27）：最新几个迁移必须已应用，且 000027 必须是**下一次** Down 的对象。
	// 少了这条，第一次 Down 滚掉的会是更早的版本，整条断言链静默后移一位 ——
	// 而末尾断言查的是 000001 建的列，多滚一层照样全绿。
	var has27, has26, has25, has24, has23 int64
	require.NoError(t, db.Raw(`SELECT count(*) FROM schema_migrations WHERE version = 27`).
		Scan(&has27).Error)
	if has27 == 0 {
		t.Fatalf("库未应用到 000027 —— 头一次 Down 会滚掉 000026，整条断言链静默后移")
	}
	require.NoError(t, db.Raw(`SELECT count(*) FROM schema_migrations WHERE version = 26`).
		Scan(&has26).Error)
	if has26 == 0 {
		t.Fatalf("库未应用到 000026 —— 下一个 Down 会滚掉 000025，整条断言链静默后移")
	}
	require.NoError(t, db.Raw(`SELECT count(*) FROM schema_migrations WHERE version = 25`).
		Scan(&has25).Error)
	if has25 == 0 {
		t.Fatalf("库未应用到 000025 —— 下一个 Down 会滚掉 000024，整条断言链静默后移")
	}
	require.NoError(t, db.Raw(`SELECT count(*) FROM schema_migrations WHERE version = 24`).
		Scan(&has24).Error)
	if has24 == 0 {
		t.Fatalf("库未应用到 000024 —— 下一个 Down 会滚掉 000023，整条断言链静默后移")
	}
	require.NoError(t, db.Raw(`SELECT count(*) FROM schema_migrations WHERE version = 23`).
		Scan(&has23).Error)
	if has23 == 0 {
		t.Fatalf("库未应用到 000023 —— 下一个 Down 会滚掉 000021，整条断言链静默后移")
	}

	migrate.FS = network_monitor_platform.MigrationsFS

	// Down = 回滚 000027：只删部分唯一索引，数据不动
	require.NoError(t, migrate.Down(db), "回滚 000027 失败")
	var v27 int64
	require.NoError(t, db.Raw(`SELECT count(*) FROM schema_migrations WHERE version = 27`).
		Scan(&v27).Error)
	assert.Zero(t, v27, "首次 Down 必须滚掉 000027 —— 否则后面每一次 Down 都在滚错的那一层")
	var zabbixIdxExists bool
	require.NoError(t, db.Raw(
		`SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE indexname = 'uq_alerts_zabbix_identity')`).
		Scan(&zabbixIdxExists).Error)
	assert.False(t, zabbixIdxExists, "down 000027 应 DROP uq_alerts_zabbix_identity")
	// 000018 的普通索引不归 000027 管，滚掉它是越界（同步预查靠它，见 up 里的说明）
	var trigIdxStillThere bool
	require.NoError(t, db.Raw(
		`SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE indexname = 'idx_alerts_trigger_id')`).
		Scan(&trigIdxStillThere).Error)
	assert.True(t, trigIdxStillThere, "down 000027 不得顺手删掉 000018 的 idx_alerts_trigger_id")

	// Down = 回滚 000026：只删部分唯一索引，数据不动
	require.NoError(t, migrate.Down(db), "回滚 000026 失败")
	var v26 int64
	require.NoError(t, db.Raw(`SELECT count(*) FROM schema_migrations WHERE version = 26`).
		Scan(&v26).Error)
	assert.Zero(t, v26, "本次 Down 必须滚掉 000026 —— 否则后面每一次 Down 都在滚错的那一层")
	var glpiIdxExists bool
	require.NoError(t, db.Raw(
		`SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE indexname = 'uq_tickets_glpi_external_id')`).
		Scan(&glpiIdxExists).Error)
	assert.False(t, glpiIdxExists, "down 000026 应 DROP uq_tickets_glpi_external_id")
	// 000019 的普通索引不归 000026 管，滚掉它是越界（预查查询靠它，见 up 里的实测）
	var extIdxStillThere bool
	require.NoError(t, db.Raw(
		`SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE indexname = 'idx_tickets_external_id')`).
		Scan(&extIdxStillThere).Error)
	assert.True(t, extIdxStillThere, "down 000026 不得顺手删掉 000019 的 idx_tickets_external_id")

	// Down = 回滚 000025：整表删除（ticket_history 是 000025 新建的，up 里没有存量数据改写）
	require.NoError(t, migrate.Down(db), "回滚 000025 失败")
	var v25 int64
	require.NoError(t, db.Raw(`SELECT count(*) FROM schema_migrations WHERE version = 25`).
		Scan(&v25).Error)
	assert.Zero(t, v25, "本次 Down 必须滚掉 000025 —— 否则后面每一次 Down 都在滚错的那一层")
	var thTables int64
	require.NoError(t, db.Raw(
		`SELECT count(*) FROM information_schema.tables WHERE table_name = 'ticket_history'`).
		Scan(&thTables).Error)
	assert.Zero(t, thTables, "down 000025 应删掉 ticket_history 表")

	// Down = 回滚 000024：只撤 alerts.status 的列默认值，数据不动
	require.NoError(t, migrate.Down(db), "回滚 000024 失败")
	var v24 int64
	require.NoError(t, db.Raw(`SELECT count(*) FROM schema_migrations WHERE version = 24`).
		Scan(&v24).Error)
	assert.Zero(t, v24, "本次 Down 必须滚掉 000024 —— 否则后面每一次 Down 都在滚错的那一层")
	var statusDef *string
	require.NoError(t, db.Raw(
		`SELECT column_default FROM information_schema.columns
		  WHERE table_name = 'alerts' AND column_name = 'status'`).Scan(&statusDef).Error)
	require.NotNil(t, statusDef, "down 000024 后 alerts.status 不该没有默认值")
	assert.Contains(t, *statusDef, "firing",
		"down 000024 应把 alerts.status 默认值退回 000001 声明的 firing：%s", *statusDef)

	// Down = 回滚 000023：工单优先级归一是纯数据迁移，down 是 no-op，
	// 只销版本号（原值已不可分辨，见 000023_*.down.sql 的注释）
	require.NoError(t, migrate.Down(db), "回滚 000023 失败")
	var v23 int64
	require.NoError(t, db.Raw(`SELECT count(*) FROM schema_migrations WHERE version = 23`).
		Scan(&v23).Error)
	assert.Zero(t, v23, "本次 Down 必须滚掉 000023")

	// Down = 回滚 000021：删除 path text_pattern_ops 索引
	require.NoError(t, migrate.Down(db), "回滚 000021 失败")
	var pathIdxExists bool
	require.NoError(t, db.Raw(
		`SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE indexname = 'idx_audit_logs_path')`).
		Scan(&pathIdxExists).Error)
	assert.False(t, pathIdxExists, "down 000021 应 DROP idx_audit_logs_path")

	// Down = 回滚 000020：删除 name 索引
	require.NoError(t, migrate.Down(db), "回滚 000020 失败")
	var nameIdxExists bool
	require.NoError(t, db.Raw(
		`SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE indexname = 'idx_assets_name')`).
		Scan(&nameIdxExists).Error)
	assert.False(t, nameIdxExists, "down 000020 应 DROP idx_assets_name")

	// Down = 回滚 000019：删除 external_id 索引
	require.NoError(t, migrate.Down(db), "回滚 000019 失败")
	var extIdxExists bool
	require.NoError(t, db.Raw(
		`SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE indexname = 'idx_tickets_external_id')`).
		Scan(&extIdxExists).Error)
	assert.False(t, extIdxExists, "down 000019 应 DROP idx_tickets_external_id")

	// Down = 回滚 000018：删除 trigger_id 索引
	require.NoError(t, migrate.Down(db), "回滚 000018 失败")
	var trigIdxExists bool
	require.NoError(t, db.Raw(
		`SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE indexname = 'idx_alerts_trigger_id')`).
		Scan(&trigIdxExists).Error)
	assert.False(t, trigIdxExists, "down 000018 应 DROP idx_alerts_trigger_id")

	// Down = 回滚 000017：删除 problem_start 索引
	require.NoError(t, migrate.Down(db), "回滚 000017 失败")
	var psIdxExists bool
	require.NoError(t, db.Raw(
		`SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE indexname = 'idx_alerts_problem_start')`).
		Scan(&psIdxExists).Error)
	assert.False(t, psIdxExists, "down 000017 应 DROP idx_alerts_problem_start")

	// Down = 回滚 000016：删除 pending 部分索引
	require.NoError(t, migrate.Down(db), "回滚 000016 失败")
	var pendingIdxExists bool
	require.NoError(t, db.Raw(
		`SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE indexname = 'idx_notification_logs_pending')`).
		Scan(&pendingIdxExists).Error)
	assert.False(t, pendingIdxExists, "down 000016 应 DROP idx_notification_logs_pending")

	// Down = 回滚 000015：net_box_id 回到非唯一索引（组合状态下 ON CONFLICT 会 42P10）
	require.NoError(t, migrate.Down(db), "回滚 000015 失败")
	assertNetBoxIDIndexUnique(t, db, false)

	// Down = 回滚 000014：只撤列默认值，数据不动
	require.NoError(t, migrate.Down(db), "回滚 000014 失败")
	var def *string
	require.NoError(t, db.Raw(
		`SELECT column_default FROM information_schema.columns
		  WHERE table_name = 'assets' AND column_name = 'tags'`).Scan(&def).Error)
	assert.Nil(t, def, "down 000014 应 DROP DEFAULT assets.tags")
	assertJSONB(t, db, "legacy-null-jsonb", "[]", "{}") // 回填值仍在（down 不动数据）

	// Down = 回滚 000013：本用例真正要守的那个
	require.NoError(t, migrate.Down(db), "回滚 000013 失败")

	var exists bool
	require.NoError(t, db.Raw(
		`SELECT EXISTS (SELECT 1 FROM information_schema.columns
		 WHERE table_name = 'tickets' AND column_name = 'ticket_type')`,
	).Scan(&exists).Error)
	assert.True(t, exists, "down 不得 DROP 000001 建的 tickets.ticket_type（丢列丢数据）")

	// 收尾正向断言：**多滚一层唯一的暴露点**。
	// 链上其余断言都是「某个索引没了」，晚一步仍然为真 —— 十四次 Down 会一路全绿，
	// 同时把 000012 也滚掉。只有「000012 必须还在」能挡住它。
	var v12 int64
	require.NoError(t, db.Raw(`SELECT count(*) FROM schema_migrations WHERE version = 12`).
		Scan(&v12).Error)
	assert.Equal(t, int64(1), v12,
		"十四次 Down 应止步于 000013 —— 000012 也被滚掉说明本用例多滚了一层（链上其余断言挡不住）")
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

// TestDBSmoke_AuditFieldTruncation 审计字段超长/多字节/控制字符必须能落库（M29-C / G-44）。
//
// 为什么必须真 PG：sqlite **不强制** VARCHAR(n) 长度、也不校验 UTF-8（T-48 同族），
// 修前那条「长路径/超长 request_id → 22001/22021 → 整行静默丢失」在 sqlite 上根本
// 复现不出来 —— 只写 sqlite 用例会**假绿**。
//
// 为什么走 middleware 而不是 db.Create：buildAuditEntry 未导出，直插测的是 GORM 而不是
// 本次改的净化代码，变异门禁恒不成立（见 db_smoke_test.go 的 TestDBSmoke_AuditInsert）。
func TestDBSmoke_AuditFieldTruncation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openSmokeDB(t)

	marker := "trunc-" + uuid.NewString()[:8]

	r := gin.New()
	r.Use(middleware.AuditLog(middleware.AuditConfig{DB: db, Async: false}))
	r.GET("/api/assets/:id", func(c *gin.Context) { c.Status(200) })

	// 三样一起上：超长多字节 path（600 汉字 > varchar(500)）、超长 request_id
	// （60 字符 > varchar(50)）、以及夹在其中的 CR/LF。
	req := httptest.NewRequest("GET", "/api/assets/"+strings.Repeat("中", 600), nil)
	req.Header.Set("X-Request-ID", marker+"\r\n"+strings.Repeat("r", 60))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, 200, w.Code)

	var got models.AuditLog
	require.NoError(t, db.Where("request_id LIKE ?", marker+"%").First(&got).Error,
		"审计行必须落库 —— 修前这里是 22001/22021，整行静默丢失（只留一行 slog.Warn）")

	// path：按**字符**截到 500（按字节截断会切出非法 UTF-8 → 22021 照样丢行）
	require.Equal(t, 500, utf8.RuneCountInString(got.Path), "path 必须截到 varchar(500) 个字符")
	assert.True(t, utf8.ValidString(got.Path), "path 必须是合法 UTF-8（否则 PG 22021 拒收）")

	// request_id：varchar(50)，且 CR/LF 被剥掉（否则落库值会断行，导出 CSV/SIEM 时伪造记录）
	require.Equal(t, 50, utf8.RuneCountInString(got.RequestID),
		"request_id 必须截到 varchar(50) 个字符")
	assert.True(t, utf8.ValidString(got.RequestID))
	assert.NotContains(t, got.RequestID, "\r")
	assert.NotContains(t, got.RequestID, "\n")
	require.True(t, strings.HasPrefix(got.RequestID, marker), "截断不得丢掉前缀（证明是本次这行）")

	t.Logf("✅ 审计字段截断落库: path=%d runes, request_id=%q", utf8.RuneCountInString(got.Path), got.RequestID)
}
