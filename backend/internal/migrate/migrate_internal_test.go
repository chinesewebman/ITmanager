package migrate

import (
	"database/sql"
	"embed"
	"io/fs"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

//go:embed testdata/migrations/*.sql
var testFS embed.FS

// setupFS 把测试 sql 注入 migrate.FS
// 包装 fs 让 testFS 根路径下暴露 "migrations" 子目录（匹配 Load 期望）
type migrationsFS struct{ inner embed.FS }

func (m migrationsFS) Open(name string) (fs.File, error) {
	return m.inner.Open("testdata/" + name)
}
func (m migrationsFS) ReadDir(name string) ([]fs.DirEntry, error) {
	return m.inner.ReadDir("testdata/" + name)
}
func (m migrationsFS) ReadFile(name string) ([]byte, error) {
	return m.inner.ReadFile("testdata/" + name)
}

func setupFS(t *testing.T) {
	t.Helper()
	FS = migrationsFS{inner: testFS}
}

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	return db
}

// ==================== Load 测试 ====================

func TestLoad_解析up和downSQL(t *testing.T) {
	setupFS(t)
	migs, err := Load()
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(migs), 1, "应有 1+ up migration")

	sort.Slice(migs, func(i, j int) bool { return migs[i].version < migs[j].version })

	first := migs[0]
	assert.True(t, first.upSQL != "", "upSQL 不应为空")
	assert.Contains(t, first.upSQL, "CREATE TABLE")
}

func TestLoad_版本号从文件名解析(t *testing.T) {
	setupFS(t)
	migs, err := Load()
	require.NoError(t, err)

	for _, m := range migs {
		assert.Greater(t, m.version, int64(0), "version 应 > 0")
		// up 或 down 至少一个有内容
		assert.True(t, m.upSQL != "" || m.downSQL != "", "SQL 不应全空")
	}
}

func TestLoad_空FS返空切片不panic(t *testing.T) {
	FS = emptyFS{}
	migs, err := Load()
	require.NoError(t, err)
	assert.Empty(t, migs)
}

// ==================== Status 测试 ====================

func TestStatus_空DB返nil无panic(t *testing.T) {
	setupFS(t)
	db := newTestDB(t)
	err := Status(db)
	assert.NoError(t, err)
}

// ==================== Up/Down 幂等性 ====================

func TestUp_幂等性_跑两次不重复应用(t *testing.T) {
	setupFS(t)
	db := newTestDB(t)

	// 第一次
	err := Up(db)
	require.NoError(t, err)

	// 验证 schema_migrations 表有数据
	var count int64
	db.Raw("SELECT COUNT(*) FROM schema_migrations").Scan(&count)
	assert.Greater(t, count, int64(0), "至少 1 条 applied")

	// 第二次（应幂等）
	err = Up(db)
	assert.NoError(t, err, "Up 第二次应幂等")

	// schema_migrations 数量不变
	var count2 int64
	db.Raw("SELECT COUNT(*) FROM schema_migrations").Scan(&count2)
	assert.Equal(t, count, count2, "不应重复插入")
}

func TestDown_成功回滚一条(t *testing.T) {
	setupFS(t)
	db := newTestDB(t)

	require.NoError(t, Up(db))
	// users 表应存在
	var tCount int64
	db.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='users'").Scan(&tCount)
	assert.Greater(t, tCount, int64(0), "users 表应已创建")

	err := Down(db)
	assert.NoError(t, err)

	// users 表应被删除
	db.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='users'").Scan(&tCount)
	assert.Equal(t, int64(0), tCount, "users 表应被删除")
}

func TestDown_无migration时返nil不panic(t *testing.T) {
	setupFS(t)
	db := newTestDB(t)
	// 先 up
	require.NoError(t, Up(db))
	// 再 down 一遍
	require.NoError(t, Down(db))
	// 第三次 down（无版本可降）
	err := Down(db)
	assert.NoError(t, err, "无版本可降应返 nil")
}

// emptyFS 模拟无文件 FS（测边界）
type emptyFS struct{}

func (emptyFS) Open(_ string) (fs.File, error) { return nil, fs.ErrNotExist }
func (emptyFS) ReadDir(_ string) ([]fs.DirEntry, error) {
	return nil, nil
}
func (emptyFS) ReadFile(_ string) ([]byte, error) { return nil, fs.ErrNotExist }
func (emptyFS) Glob(_ string) ([]string, error)   { return nil, nil }
func (emptyFS) Sub(_ string) (fs.FS, error)       { return emptyFS{}, nil }

// 兼容 sql import
var _ sql.IsolationLevel = 0
