package database

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// testSecret 模拟落进 SQL 参数里的凭据材料（bcrypt 哈希）。
const testSecret = "$2a$10$SElqtKSJzKq5lGCTg5ULieCcBMQmTlSPMK6gJIKMNkZfBqifZiV9C"

func TestMapGormLogLevel_映射表(t *testing.T) {
	cases := []struct {
		in   string
		want gormlogger.LogLevel
	}{
		{"debug", gormlogger.Info},
		{"DEBUG", gormlogger.Info},
		{" debug ", gormlogger.Info},
		{"info", gormlogger.Warn},
		{"INFO", gormlogger.Warn},
		{" info ", gormlogger.Warn},
		{"warn", gormlogger.Warn},
		{"", gormlogger.Warn},        // 缺 log.level 键
		{"warning", gormlogger.Warn}, // 非法值：安全兜底，不得降级成 Info
		{"verbose", gormlogger.Warn},
		{"error", gormlogger.Error},
		{"ERROR", gormlogger.Error},
	}
	for _, c := range cases {
		if got := mapGormLogLevel(c.in); got != c.want {
			t.Errorf("mapGormLogLevel(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

// TestGormLogLevel_默认不是零值 钉住包级默认：gorm 的 LogLevel 零值比 Silent 还小，
// logger.Trace 首行 `if l.LogLevel <= Silent { return }` 会一条都不打 —— 连错误和
// 慢查询都静默，比预期的 Warn 更糟。写成 `var gormLogLevel gormlogger.LogLevel` 即红。
func TestGormLogLevel_默认不是零值(t *testing.T) {
	assert.Equal(t, gormlogger.Warn, gormLogLevel,
		"包级默认必须显式初始化成 Warn：零值比 Silent 还小，gorm 会一条都不打")
	assert.Equal(t, gormlogger.Warn, mapGormLogLevel(""),
		"缺 log.level 键（空串）必须落到 Warn")
}

// TestGormLoggerConfig_恒定参数化且无色 断言的是配置结构体本身 ——
// gormlogger.New 返回接口、具体类型未导出，只能靠这个纯函数把口径钉住。
func TestGormLoggerConfig_恒定参数化且无色(t *testing.T) {
	c := gormLoggerConfig(gormlogger.Info)
	assert.True(t, c.ParameterizedQueries, "参数化必须恒开：关掉就会把 bcrypt 哈希写进日志")
	assert.False(t, c.Colorful, "ANSI 转义会污染容器/文件日志")
	assert.True(t, c.IgnoreRecordNotFoundError)
	assert.Equal(t, time.Second, c.SlowThreshold)
	assert.Equal(t, gormlogger.Info, c.LogLevel, "级别必须来自入参，不能硬编码")
}

// openDBWithCurrentLevel 按**当前** gormLogLevel 新建一个库（gorm 在 Open 时把级别
// 固化进 logger，改级别必须重新 Open 才看得到效果）。
func openDBWithCurrentLevel(t *testing.T) (*gorm.DB, *bytes.Buffer) {
	t.Helper()
	// newGormLogger 会覆写包级 RecorderParamsFilter，测试结束要还回去，
	// 否则同包后续用例（或将来加 t.Parallel 的用例）会看到上一个用例的钩子。
	oldFilter := gormlogger.RecorderParamsFilter
	t.Cleanup(func() { gormlogger.RecorderParamsFilter = oldFilter })

	var buf bytes.Buffer
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger:                 newGormLogger(&buf),
		SkipDefaultTransaction: true,
	})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE t (id TEXT PRIMARY KEY)`).Error)
	buf.Reset()
	return db, &buf
}

// TestSetGormLogLevel_真的接到logger上 钉住 setter → 包级变量 → newGormLogger 这条链。
// 只改 setter 写入的变量、而 newGormLogger 读的是另一个（或 setter 写成空操作），
// 配置就会**静默失效** —— 这正是 T-32 那一族缺陷的形态，所以必须端到端断言行为。
func TestSetGormLogLevel_真的接到logger上(t *testing.T) {
	old := gormLogLevel
	t.Cleanup(func() { gormLogLevel = old })

	SetGormLogLevel("error")
	db, buf := openDBWithCurrentLevel(t)
	require.NoError(t, db.Exec(`INSERT INTO t (id) VALUES (?)`, "x").Error)
	assert.Empty(t, buf.String(),
		"error 档下成功的快查询不该打任何 SQL —— 有输出说明 setter 没接到 logger 上")

	SetGormLogLevel("debug")
	db2, buf2 := openDBWithCurrentLevel(t)
	require.NoError(t, db2.Exec(`INSERT INTO t (id) VALUES (?)`, "y").Error)
	assert.Contains(t, buf2.String(), "INSERT INTO",
		"debug 档必须逐条打印 —— 空说明级别没有跟着 setter 变")
}

// openLoggingTestDB 起一个把 gorm 日志写进 buffer 的 sqlite 内存库。
// 级别固定 Info（逐条打印）——Warn 档下快查询一条都不打，断言会变成空转。
func openLoggingTestDB(t *testing.T) (*gorm.DB, *bytes.Buffer) {
	t.Helper()
	old := gormLogLevel
	gormLogLevel = gormlogger.Info
	t.Cleanup(func() { gormLogLevel = old })

	var buf bytes.Buffer
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger:                 newGormLogger(&buf),
		SkipDefaultTransaction: true,
	})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE secrets (id TEXT PRIMARY KEY, password_hash TEXT)`).Error)
	return db, &buf
}

// TestGormLogger_普通查询不落参数值 走 callbacks → logger.ParamsFilter 路径。
func TestGormLogger_普通查询不落参数值(t *testing.T) {
	db, buf := openLoggingTestDB(t)

	require.NoError(t, db.Exec(`INSERT INTO secrets (id, password_hash) VALUES (?, ?)`, "s1", testSecret).Error)

	out := buf.String()
	// 先证明日志真的打了语句：只断言「不含」的话，日志被关掉也是绿的（T-31 的假绿）。
	require.Contains(t, out, "INSERT INTO", "语句骨架必须被打出来，否则下面的断言是空转")
	assert.NotContains(t, out, "$2a$", "参数值（bcrypt 哈希）不得出现在 SQL 日志里")
}

// TestGormLogger_Scan路径不落参数值 走 (*DB).Scan → traceRecorder → 包级
// RecorderParamsFilter 路径。只设 ParameterizedQueries 挡不住它（见 gorm_logger.go）。
func TestGormLogger_Scan路径不落参数值(t *testing.T) {
	db, buf := openLoggingTestDB(t)
	require.NoError(t, db.Exec(`INSERT INTO secrets (id, password_hash) VALUES (?, ?)`, "s1", testSecret).Error)
	buf.Reset()

	var got []struct{ PasswordHash string }
	require.NoError(t, db.Raw(`SELECT password_hash FROM secrets WHERE password_hash = ?`, testSecret).Scan(&got).Error)
	require.Len(t, got, 1, "先证明这条 SQL 真执行到了，否则下面的断言空转")

	out := buf.String()
	require.Contains(t, out, "SELECT password_hash FROM secrets", "Scan 路径也必须留下骨架")
	assert.NotContains(t, out, "$2a$", "Scan 路径的参数同样不得落日志")
}
