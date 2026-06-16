package main

import (
	"database/sql"
	"embed"
	"io/fs"
	"os"
	"strings"
	"testing"

	"network-monitor-platform/internal/migrate"

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

//go:embed all:cmd-migrate-testdata/migrations/*.sql
var migrateTestFS embed.FS

// migrationsFS 包装 embed.FS 把 cmd-migrate-testdata/migrations/ 暴露成 "migrations"
type migrationsFS2 struct{ inner embed.FS }

func (m migrationsFS2) Open(name string) (fs.File, error) {
	return m.inner.Open("cmd-migrate-testdata/" + name)
}
func (m migrationsFS2) ReadDir(name string) ([]fs.DirEntry, error) {
	return m.inner.ReadDir("cmd-migrate-testdata/" + name)
}
func (m migrationsFS2) ReadFile(name string) ([]byte, error) {
	return m.inner.ReadFile("cmd-migrate-testdata/" + name)
}

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	return db
}

// ==================== runWithDeps 测试 ====================

func TestRunWithDeps_up_成功应用所有migration(t *testing.T) {
	db := newTestDB(t)
	migrate.FS = migrationsFS2{inner: migrateTestFS}
	t.Cleanup(func() { migrate.FS = nil })

	err := runWithDeps(db, "up")
	require.NoError(t, err)

	// schema_migrations 应有 1 条
	var count int64
	db.Raw("SELECT COUNT(*) FROM schema_migrations").Scan(&count)
	assert.Equal(t, int64(1), count, "应有 1 条 applied migration")

	// users 表应存在
	var tableCount int64
	db.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='users'").Scan(&tableCount)
	assert.Equal(t, int64(1), tableCount, "users 表应被创建")
}

func TestRunWithDeps_up_幂等_跑两次不重复应用(t *testing.T) {
	db := newTestDB(t)
	migrate.FS = migrationsFS2{inner: migrateTestFS}
	t.Cleanup(func() { migrate.FS = nil })

	require.NoError(t, runWithDeps(db, "up"))
	require.NoError(t, runWithDeps(db, "up"))

	var count int64
	db.Raw("SELECT COUNT(*) FROM schema_migrations").Scan(&count)
	assert.Equal(t, int64(1), count, "第二次 up 不应重复插入")
}

func TestRunWithDeps_down_回滚最后一条(t *testing.T) {
	db := newTestDB(t)
	migrate.FS = migrationsFS2{inner: migrateTestFS}
	t.Cleanup(func() { migrate.FS = nil })

	require.NoError(t, runWithDeps(db, "up"))
	require.NoError(t, runWithDeps(db, "down"))

	var count int64
	db.Raw("SELECT COUNT(*) FROM schema_migrations").Scan(&count)
	assert.Equal(t, int64(0), count, "down 后应清空")

	// users 表应被 drop
	var tableCount int64
	db.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='users'").Scan(&tableCount)
	assert.Equal(t, int64(0), tableCount, "users 表应被删除")
}

func TestRunWithDeps_down_无migration时返nil不panic(t *testing.T) {
	db := newTestDB(t)
	migrate.FS = migrationsFS2{inner: migrateTestFS}
	t.Cleanup(func() { migrate.FS = nil })

	// 不先 up，直接 down
	err := runWithDeps(db, "down")
	assert.NoError(t, err, "无 migration 时 down 应返 nil")
}

func TestRunWithDeps_status_不返错(t *testing.T) {
	db := newTestDB(t)
	migrate.FS = migrationsFS2{inner: migrateTestFS}
	t.Cleanup(func() { migrate.FS = nil })

	require.NoError(t, runWithDeps(db, "up"))
	require.NoError(t, runWithDeps(db, "status"))
}

func TestRunWithDeps_status_空DB不panic(t *testing.T) {
	db := newTestDB(t)
	migrate.FS = migrationsFS2{inner: migrateTestFS}
	t.Cleanup(func() { migrate.FS = nil })

	require.NoError(t, runWithDeps(db, "status"))
}

func TestRunWithDeps_unknown_command_返错(t *testing.T) {
	db := newTestDB(t)
	err := runWithDeps(db, "bogus")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown command")
	assert.Contains(t, err.Error(), "bogus")
}

func TestRunWithDeps_up_无FS_返错(t *testing.T) {
	db := newTestDB(t)
	migrate.FS = embed.FS{} // 空 FS
	t.Cleanup(func() { migrate.FS = nil })

	err := runWithDeps(db, "up")
	require.Error(t, err)
	assert.True(t,
		strings.Contains(err.Error(), "read migrations dir") || strings.Contains(err.Error(), "no such file"),
		"应返 FS 错误，实际: %v", err)
}

// ==================== migrateReset 测试（输入 yes）====================

func TestMigrateReset_确认yes_回滚所有(t *testing.T) {
	db := newTestDB(t)
	migrate.FS = migrationsFS2{inner: migrateTestFS}
	t.Cleanup(func() { migrate.FS = nil })

	require.NoError(t, runWithDeps(db, "up"))
	var before int64
	db.Raw("SELECT COUNT(*) FROM schema_migrations").Scan(&before)
	require.Equal(t, int64(1), before)

	// 模拟 stdin 输入 "yes\n"
	mockStdin(t, "yes\n")
	err := migrateReset(db)
	require.NoError(t, err)

	var after int64
	db.Raw("SELECT COUNT(*) FROM schema_migrations").Scan(&after)
	assert.Equal(t, int64(0), after, "yes 确认后应全部回滚")
}

func TestMigrateReset_输入非yes_中断不报错(t *testing.T) {
	db := newTestDB(t)
	migrate.FS = migrationsFS2{inner: migrateTestFS}
	t.Cleanup(func() { migrate.FS = nil })

	require.NoError(t, runWithDeps(db, "up"))

	mockStdin(t, "no\n")
	err := migrateReset(db)
	require.NoError(t, err, "输入 no 应 abort，不返错")

	// migration 应还在
	var count int64
	db.Raw("SELECT COUNT(*) FROM schema_migrations").Scan(&count)
	assert.Equal(t, int64(1), count, "abort 后 migration 仍在")
}

// mockStdin 把给定字符串作为 stdin 注入（自动 cleanup 恢复）
func mockStdin(t *testing.T, input string) {
	t.Helper()
	orig := os.Stdin
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = orig })

	go func() {
		_, _ = w.Write([]byte(input))
		_ = w.Close()
	}()
}
