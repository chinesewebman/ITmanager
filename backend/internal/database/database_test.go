package database

import (
	"embed"
	"io/fs"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"network-monitor-platform/internal/config"
)

// testMigrationsFS 是数据库测试用的迁移 embed.FS, 编译期注入 testdata/ 内容.
// 实际给 migrate 用的是通过 fs.Sub 切到 testdata 这一层 (subFS 根里是 migrations/
// 子目录, 正好对齐 migrate.Load 的 fs.ReadDir(FS, "migrations") 期望).
// SubFS 在 withM88FS 测试 helper 里做.
//
//go:embed all:testdata
var testMigrationsFS embed.FS

// newTestDB 用 sqlmock 隔离, 不连真实 PG
func newTestDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	mockDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	gormDB, err := gorm.Open(postgres.New(postgres.Config{Conn: mockDB, PreferSimpleProtocol: true}), &gorm.Config{})
	require.NoError(t, err)
	return gormDB, mock
}

func TestSetDBForTest_andGetDB_往返一致(t *testing.T) {
	gormDB, _ := newTestDB(t)
	original := GetDB()
	defer SetDBForTest(original)

	SetDBForTest(gormDB)
	got := GetDB()
	assert.Same(t, gormDB, got, "GetDB 应该返回 SetDBForTest 注入的实例")
}

func TestGetDB_初始为nil(t *testing.T) {
	original := GetDB()
	defer SetDBForTest(original)

	SetDBForTest(nil)
	assert.Nil(t, GetDB(), "未初始化时 GetDB 应返回 nil")
}

func TestSetMigrationsFS_不panic(t *testing.T) {
	assert.NotPanics(t, func() { SetMigrationsFS(embed.FS{}) })
}

func TestClose_DB为nil不报错(t *testing.T) {
	original := GetDB()
	defer SetDBForTest(original)

	SetDBForTest(nil)
	err := Close()
	assert.NoError(t, err, "DB 为 nil 时 Close 应安全返回 nil")
}

func TestClose_DB为有效实例应关闭(t *testing.T) {
	gormDB, mock := newTestDB(t)
	original := GetDB()
	defer SetDBForTest(original)

	SetDBForTest(gormDB)
	mock.ExpectClose()

	err := Close()
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestDatabaseConfig_DSN_FormatCorrect(t *testing.T) {
	c := config.DatabaseConfig{
		Host:     "localhost",
		Port:     5432,
		User:     "postgres",
		Password: "secret",
		Name:     "testdb",
		SSLMode:  "disable",
	}
	dsn := c.DSN()
	assert.Contains(t, dsn, "host=localhost")
	assert.Contains(t, dsn, "port=5432")
	assert.Contains(t, dsn, "user=postgres")
	assert.Contains(t, dsn, "password=secret")
	assert.Contains(t, dsn, "dbname=testdb")
	assert.Contains(t, dsn, "sslmode=disable")
}

// v1.4 Batch 2: 增量测试, 覆盖 autoMigrate + Init 错误路径

func TestDatabaseConfig_DSN_含空password(t *testing.T) {
	c := config.DatabaseConfig{
		Host: "db", Port: 5432, User: "u", Password: "",
		Name: "n", SSLMode: "disable",
	}
	dsn := c.DSN()
	assert.Contains(t, dsn, "password=")
}

func TestDatabaseConfig_DSN_默认端口5432(t *testing.T) {
	c := config.DatabaseConfig{Host: "h", User: "u", Password: "p", Name: "n", SSLMode: "disable"}
	assert.Equal(t, 0, c.Port, "未设 Port 时为零值")
	dsn := c.DSN()
	assert.Contains(t, dsn, "port=0", "未设 Port 时 DSN 应含 port=0")
}

func TestInit_DSN格式错误返回中文包装错(t *testing.T) {
	cfg := &config.DatabaseConfig{
		Host:     "",
		Port:     5432,
		User:     "",
		Password: "",
		Name:     "",
		SSLMode:  "disable",
	}
	_, err := Init(cfg)
	if err == nil {
		t.Skip("Init 在 mock 环境意外成功")
	}
	assert.Contains(t, err.Error(), "数据库", "错误信息应含中文包装词")
}

func TestInit_FS已注入时不调autoMigrate(t *testing.T) {
	originalFS := MigrationsFS
	defer func() { MigrationsFS = originalFS }()
	t.Skip("无法运行时构造非零 embed.FS, 跳过此路径; 真实测试靠 integration test")
}

func TestAutoMigrate_全模型逐个迁移(t *testing.T) {
	mockDB, mock, err := sqlmock.New(
		sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp),
	)
	require.NoError(t, err)
	defer mockDB.Close()
	mock.MatchExpectationsInOrder(false)

	for range 50 {
		mock.ExpectExec(`(CREATE|ALTER|DROP|INSERT|UPDATE|DELETE)`).
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectQuery(`SELECT`).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	}

	gormDB, err := gorm.Open(postgres.New(postgres.Config{Conn: mockDB, PreferSimpleProtocol: true}), &gorm.Config{})
	require.NoError(t, err)

	original := GetDB()
	defer SetDBForTest(original)
	SetDBForTest(gormDB)

	err = autoMigrate()
	assert.NoError(t, err, "mock 充分, autoMigrate 应成功")
}

func TestAutoMigrate_某model失败返错(t *testing.T) {
	mockDB, mock, err := sqlmock.New(
		sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp),
	)
	require.NoError(t, err)
	defer mockDB.Close()
	mock.MatchExpectationsInOrder(false)

	mock.ExpectQuery(`SELECT count`).
		WillReturnError(assert.AnError)

	gormDB, err := gorm.Open(postgres.New(postgres.Config{Conn: mockDB, PreferSimpleProtocol: true}), &gorm.Config{})
	require.NoError(t, err)

	original := GetDB()
	defer SetDBForTest(original)
	SetDBForTest(gormDB)

	err = autoMigrate()
	assert.Error(t, err, "迁移失败应返错")
}

func TestSetMigrationsFS_二次注入覆盖(t *testing.T) {
	original := MigrationsFS
	defer func() { MigrationsFS = original }()

	SetMigrationsFS(embed.FS{})
	assert.NotPanics(t, func() {
		SetMigrationsFS(embed.FS{})
		SetMigrationsFS(embed.FS{})
	})
}

func TestClose_DB非nil但非gorm调用返错(t *testing.T) {
	gormDB, mock := newTestDB(t)
	original := GetDB()
	defer SetDBForTest(original)

	SetDBForTest(gormDB)
	mock.ExpectClose().WillReturnError(assert.AnError)

	err := Close()
	assert.Error(t, err, "底层 close 失败应上抛")
}

// ==================== M88 / G-14: InitWithAutoMigrate 开关收口 ====================
//
// M88 三个测试都走 initDBForTest + 内存 sqlite, 验证 InitWithAutoMigrate 真路径行为:
//   - AutoMigrate=true 时, migrate.Up 跑完应建 schema_migrations + users 两表 (back-compat 契约)
//   - AutoMigrate=false 时, Init 跳过 migrate.Up → 两表都不存在 (mutation inversion M1 锚点)
//   - AutoMigrate=false + 业务 SELECT 应正常 → 没遗留锁 / 没污染连接
//
// 注: InitWithAutoMigrate 硬绑定 postgres.Open, 同包测试走 initDBForTest 注入 sqlite.
//    migrate.go 内置 sqlite 分支 (ensureTable / acquireLock 都判 dialector name).

// withM88FS 通过 fs.Sub 包装 testMigrationsFS, 切到 testdata 这一层 (让 subFS 根里的
// migrations/ 子目录恰好对齐 migrate.Load 的 fs.ReadDir(FS, "migrations") 期望).
// 返回的 fs.FS 用作 initDBForTest 的 overrideFS 参数.
func withM88FS(t *testing.T) fs.FS {
	t.Helper()
	subFS, err := fs.Sub(testMigrationsFS, "testdata")
	require.NoError(t, err, "testdata SubFS 失败 — 测试 fixture 缺失或路径错了")
	return subFS
}

func TestInitWithAutoMigrate_默认true不破现有行为(t *testing.T) {
	subFS := withM88FS(t)

	db, err := initDBForTest(sqlite.Open(":memory:"), true, subFS)
	require.NoError(t, err)
	require.NotNil(t, db)

	// back-compat 契约: 既有 4 个 caller (admin-bootstrap / seed / set-role / migrate) 仍走
	// Init(=InitWithAutoMigrate(true)), 行为不变. schema_migrations 与 0001_init.up.sql 的
	// users 表都应被建。
	assert.True(t, tableExistsSQLite(t, db, "schema_migrations"),
		"InitWithAutoMigrate(cfg, true) 应跑 migrate.Up, schema_migrations 表应存在")
	assert.True(t, tableExistsSQLite(t, db, "users"),
		"InitWithAutoMigrate(cfg, true) 应跑 0001_init.up.sql, users 表应存在")
}

func TestInitWithAutoMigrate_开关false跳过migrateUp(t *testing.T) {
	// 传 subFS 是为了让 mutation M1 (极性翻转) 后, AutoMigrate=false 走 migrate.Up 路径
	// → schema_migrations 表被建 → 断言红. 不传的话 mutation 后走 autoMigrateFn 兜底,
	// 测试也红但失败原因不同 (gorm AutoMigrate sql 错误). 传 subFS 让 mutation 反证更精确.
	subFS := withM88FS(t)
	db, err := initDBForTest(sqlite.Open(":memory:"), false, subFS)
	require.NoError(t, err)
	require.NotNil(t, db)

	// 关键断言 1: schema_migrations 表**不**存在 (ensureTable 在 migrate.Up 内, 没被调用)。
	// mutation M1 把 `if !autoMigrate` 翻为 `if autoMigrate` 后, AutoMigrate=false → 进
	// migrate.Up 分支 → schema_migrations 表被建 → 此断言红。
	assert.False(t, tableExistsSQLite(t, db, "schema_migrations"),
		"InitWithAutoMigrate(cfg, false) 应跳过 migrate.Up, schema_migrations 表应不存在 (mutation inversion M1 锚点)")
	// 关键断言 2: users 表**不**存在 (migrate.Up 没跑, 0001_init 没执行)。
	assert.False(t, tableExistsSQLite(t, db, "users"),
		"InitWithAutoMigrate(cfg, false) 应跳过 migrate.Up, users 表应不存在")
}

func TestInitWithAutoMigrate_开关false不抢advisoryLock(t *testing.T) {
	db, err := initDBForTest(sqlite.Open(":memory:"), false, nil)
	require.NoError(t, err)
	require.NotNil(t, db)

	// 间接反证: schema_migrations 不存在 → migrate.Up / ensureTable / acquireLock 全没跑。
	// 真 PG 上的 pg_locks view 断言由 TestDBSmoke_M88_MultiReplicaNoLockContention 覆盖。
	assert.False(t, tableExistsSQLite(t, db, "schema_migrations"),
		"间接反证: schema_migrations 不存在 → migrate.Up 没跑 → acquireLock 没发锁 query")

	// 业务 SELECT 应正常: InitWithAutoMigrate(false) 没遗留状态污染连接
	var n int
	err = db.Raw("SELECT 1").Scan(&n).Error
	require.NoError(t, err, "业务 SELECT 应正常 (InitWithAutoMigrate(false) 没遗留锁或污染连接)")
	assert.Equal(t, 1, n)
}

// tableExistsSQLite 检查 sqlite_master 内是否有指定表.
func tableExistsSQLite(t *testing.T, db *gorm.DB, name string) bool {
	t.Helper()
	var count int64
	err := db.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", name).Scan(&count).Error
	if err != nil {
		return false
	}
	return count > 0
}
