package database

import (
	"context"
	"io"
	"log"
	"strings"
	"time"

	gormlogger "gorm.io/gorm/logger"
)

// gormLogLevel 是 gorm SQL 日志的级别，由各 main 在 database.Init 之前通过
// SetGormLogLevel 注入（对齐 SetMigrationsFS 的注入方式，不改 Init 签名）。
//
// 必须显式初始化成 Warn：gorm 的 LogLevel 零值比 Silent 还小，而 logger.Trace
// 首行就是 `if l.LogLevel <= Silent { return }` —— 零值会一条都不打，连错误和
// 慢查询都静默，比预期的 Warn 更糟。
var gormLogLevel = gormlogger.Warn

// SetGormLogLevel 注入 gorm 日志级别（传 cfg.Log.Level 原样即可）。
func SetGormLogLevel(level string) {
	gormLogLevel = mapGormLogLevel(level)
}

// mapGormLogLevel 把项目日志级别映射到 gorm 级别。
//
// 只有 debug 才逐条打印 SQL；info/warn/空串/非法值一律 Warn（只打慢查询与错误），
// error 只打错误。**非法值不降级成 Info** —— 配置写错不该变成「把每条 SQL 都打出来」。
// 空串落到 Warn 是必要的：viper 没给 log.level 设默认值（config.go 只给
// server/auth/database/redis 设了），自定义 config.yaml 缺该键时就是空串。
func mapGormLogLevel(level string) gormlogger.LogLevel {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return gormlogger.Info
	case "error":
		return gormlogger.Error
	default: // info / warn / "" / 拼写错误
		return gormlogger.Warn
	}
}

// gormLoggerConfig 构造 gorm logger 配置。
//
// 抽成纯函数是因为 gormlogger.New 返回接口、具体类型未导出，测试拿不到 Config
// 字段 —— 只有这样才能断言「参数化恒开、颜色恒关」，而不是靠注释承诺。
func gormLoggerConfig(level gormlogger.LogLevel) gormlogger.Config {
	return gormlogger.Config{
		SlowThreshold:             time.Second,
		LogLevel:                  level,
		IgnoreRecordNotFoundError: true,
		// 恒定 true，不做成配置项：gorm 默认会把参数展开进 SQL 文本（Dialector.Explain），
		// 展开值包含 bcrypt 哈希、API Key 哈希（实证见 docs/FIX-PLAN-LOG-HYGIENE.md）。
		// 任何「能把它翻成泄漏」的开关都是定时炸弹。
		ParameterizedQueries: true,
		Colorful:             false, // ANSI 转义会污染容器/文件日志
	}
}

// newGormLogger 构造 gorm logger，并顺带收口记录器的参数过滤。
//
// 为什么不能只靠 Config.ParameterizedQueries：(*DB).Scan 会把 logger 换成
// traceRecorder（finisher_api.go:527-533），它的 ParamsFilter 走的是**包级**
// RecorderParamsFilter（logger.go:220-225），默认恒等 no-op（logger.go:85-88）——
// 于是 Scan 路径的参数照样被展开落日志。下面这行覆盖那个钩子。
//
// w 只用于测试注入（生产传 log.Writer()，即标准库 log 的 stderr）。
func newGormLogger(w io.Writer) gormlogger.Interface {
	gormlogger.RecorderParamsFilter = dropRecorderParams
	return gormlogger.New(log.New(w, "\r\n", log.LstdFlags), gormLoggerConfig(gormLogLevel))
}

// dropRecorderParams 丢弃参数，只留占位符骨架。
func dropRecorderParams(_ context.Context, sql string, _ ...interface{}) (string, []interface{}) {
	return sql, nil
}
