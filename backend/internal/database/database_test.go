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
