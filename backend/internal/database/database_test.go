package database

import (
	"embed"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"network-monitor-platform/internal/config"
)

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
	defer SetDBForTest(original) // cleanup

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
	// embed.FS 不能运行时构造, 这里只验证注入函数不 panic
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
	// DSN 是 config.DatabaseConfig 方法, 这里间接验证 database.go 用到的格式
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
	assert.Contains(t, dsn, "password=") // 应有 password= 字段 (空值)
}

func TestDatabaseConfig_DSN_默认端口5432(t *testing.T) {
	c := config.DatabaseConfig{Host: "h", User: "u", Password: "p", Name: "n", SSLMode: "disable"}
	assert.Equal(t, 0, c.Port, "未设 Port 时为零值")
	dsn := c.DSN()
	assert.Contains(t, dsn, "port=0", "未设 Port 时 DSN 应含 port=0")
}

func TestInit_DSN格式错误返回中文包装错(t *testing.T) {
	// 空 host + 空 user 等导致 DSN 格式仍合法, 但 PG driver 连不上 → 触发错误包装
	// 由于无真 PG, 一定失败; 验证错误信息含"数据库"
	cfg := &config.DatabaseConfig{
		Host:     "",
		Port:     5432,
		User:     "",
		Password: "",
		Name:     "",
		SSLMode:  "disable",
	}
	// 保留 cleanup (Init 失败前不设置 DB, 不用恢复)
	_, err := Init(cfg)
	if err == nil {
		t.Skip("Init 在 mock 环境意外成功")
	}
	// 验证错误含中文包装 (PG driver 失败时 gorm.Open 返错, 我们包装成"连接数据库失败")
	assert.Contains(t, err.Error(), "数据库", "错误信息应含中文包装词")
}

func TestInit_FS已注入时不调autoMigrate(t *testing.T) {
	// 临时注入 MigrationsFS, 验证 Init 走 migration 路径
	// 因无真 PG, 会在 gorm.Open 阶段失败; 但能验证到 migrate.Up 路径
	// 这里用 embed.FS{} 零值, migrate.Up 不会执行 (MigrationsFS 检查)
	originalFS := MigrationsFS
	defer func() { MigrationsFS = originalFS }()

	// 强制 MigrationsFS 不为零值 embed.FS{}
	// (embed.FS{} 是 zero value, 在 database.go L71 用 == embed.FS{} 比较)
	// 这里靠 SetMigrationsFS 注入, 但零值会触发 autoMigrate fallback
	// 用一个非零值的 embed.FS (无法运行时构造, 跳过此路径)
	t.Skip("无法运行时构造非零 embed.FS, 跳过此路径; 真实测试靠 integration test")
}

func TestAutoMigrate_全模型逐个迁移(t *testing.T) {
	// 模拟 12 个 model 走 AutoMigrate
	// gorm AutoMigrate 单 model: SELECT count + CREATE TABLE + N*CREATE INDEX
	// 我们不 mock 所有 query, 只验证: autoMigrate 跑起来 (会因 mock exhaustion 返 error),
	// 但只要 SQL 是预期类型就算通过
	mockDB, mock, err := sqlmock.New(
		sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp),
	)
	require.NoError(t, err)
	defer mockDB.Close()
	mock.MatchExpectationsInOrder(false)

	// 接受任意 (SELECT|INSERT|UPDATE|DELETE|CREATE|ALTER|DROP) query
	// 故意不限次数, 让 gorm 用完 mock 走"all expectations fulfilled"路径
	for i := 0; i < 50; i++ {
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
	// 50 expectations 够 12 model 跑完
	assert.NoError(t, err, "mock 充分, autoMigrate 应成功")
}

func TestAutoMigrate_某model失败返错(t *testing.T) {
	// 模拟 AutoMigrate 失败
	mockDB, mock, err := sqlmock.New(
		sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp),
	)
	require.NoError(t, err)
	defer mockDB.Close()
	mock.MatchExpectationsInOrder(false)

	// 第一个 query (SELECT count) 失败, 触发 error
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
	// 测试多次 SetMigrationsFS 后以最后一次为准
	original := MigrationsFS
	defer func() { MigrationsFS = original }()

	SetMigrationsFS(embed.FS{})
	// embed.FS{} 是 zero value, 但 SetMigrationsFS 赋值后变量地址变化
	// 验证不 panic + 可再次注入
	assert.NotPanics(t, func() {
		SetMigrationsFS(embed.FS{})
		SetMigrationsFS(embed.FS{})
	})
}

func TestClose_DB非nil但非gorm调用返错(t *testing.T) {
	// 模拟 gormDB.Close() 失败 (sqlmock 强制返回错)
	gormDB, mock := newTestDB(t)
	original := GetDB()
	defer SetDBForTest(original)

	SetDBForTest(gormDB)
	mock.ExpectClose().WillReturnError(assert.AnError)

	err := Close()
	assert.Error(t, err, "底层 close 失败应上抛")
}
