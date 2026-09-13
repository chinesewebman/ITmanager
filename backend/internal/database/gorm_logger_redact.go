package database

import (
	"context"
	"time"

	"network-monitor-platform/internal/redact"

	gormlogger "gorm.io/gorm/logger"
)

// redactGormLogger 包一层 gormlogger.Interface, 在 Error / Trace 路径
// 把 err 走 redact.Text + StripControl (M46 / G-32).
//
// 设计动机: gormlogger 默认的 Trace 实现把 err 直接 Printf 进日志
// (logger.go:170-173), err.Error() 含 pgx 编码失败的 `%#v` 值
// (pgtype/pgtype.go:1905 附近). 当前无「凭据 → 非字符串列」的活路径,
// 但出口必须先脱敏, 防未来 caller 引入.
//
// 同时实现 ParamsFilter 接口 — 在 callbacks.go:136 的 `db.Dialector.Explain
// (sql, vars...)` 之前, 把 sql + vars 里的参数替换为占位符 (vars=nil),
// 让 Explain 拿到的是骨架 sql + nil vars. 这是 G-16 ship 过的
// `ParameterizedQueries=true` 在 sqlite 下失效的补漏.
//
// 保留行为:
//   - 其他方法 (LogMode/Info/Warn) 直接转发, 不改参数过滤.
//   - errors.Is(err, ErrRecordNotFound) 等错误链必须保, 所以包一层
//     redactedError, Unwrap 回原 err.
//
// 不要做成「只打 SQLSTATE + 表名」的极简方案: 那是 TODO.md 备选,
// 排障时丢太多信息, 不取.
type redactGormLogger struct {
	gormlogger.Interface
}

// Error 覆盖 — args 含 error 走 redact.
func (l *redactGormLogger) Error(ctx context.Context, msg string, args ...interface{}) {
	l.Interface.Error(ctx,
		redact.Text(redact.StripControl(msg)),
		redactArgs(args)...)
}

// Trace 覆盖 — 错误文本过 redact (M46 / G-32 scope).
//
// 为什么不直接转发到 l.Interface.Trace: 那条路 Trace 把 fc() 返回的 sql
// (含参数展开值) 拼进 %v 写出, 错误文本也裸拼. 这里只覆盖错误路径的 err
// 文本脱敏, SQL 文本仍走 gorm 默认 — 但**前置**用 ParamsFilter 把
// vars 替换为占位符 (see below), 让 fc() 拿到的 sql 是骨架形式.
func (l *redactGormLogger) Trace(ctx context.Context, begin time.Time,
	fc func() (sql string, rowsAffected int64), err error) {
	if err != nil {
		err = redactErr(err)
	}
	l.Interface.Trace(ctx, begin, fc, err)
}

// ParamsFilter 接口实现 — callbacks.go:136 在 Explain 之前调这个.
// 我们把 vars 全部丢弃 (vars=nil) + sql 不动 (已经是骨架: `?` 占位符形式),
// 这样 Dialector.Explain 拿到的 sql 是骨架, 不会展开参数值进日志.
//
// 这是 G-16 ship 过的 `ParameterizedQueries=true` 的补漏:
// ParameterizedQueries=true 在 sqlite 下不生效 (sqlite driver
// 不返回 $N 占位符形式), 所以 Query/Exec 路径的 SQL 参数仍落日志.
// ParamsFilter 是 gorm 的真正 hook, 把 vars 替换为 nil 让 Explain
// 拿到无值 sql.
func (l *redactGormLogger) ParamsFilter(ctx context.Context, sql string, params ...interface{}) (string, []interface{}) {
	return sql, nil
}

// redactArgs 把 args 里的 error 元素过 redact + StripControl.
// 非 error 元素 (string/int) 透传 (它们通常是 SQL 参数, G-16 已 ship 过滤).
func redactArgs(args []interface{}) []interface{} {
	out := make([]interface{}, len(args))
	for i, a := range args {
		if e, ok := a.(error); ok {
			out[i] = redactErr(e)
		} else {
			out[i] = a
		}
	}
	return out
}

// redactErr 对 err.Error() 走 redact.Text + StripControl.
// 返回 redactedError, Error() 输出脱敏文本, Unwrap() 回原 err 保错误链
// (errors.Is(err, gorm.ErrRecordNotFound) 仍识别).
func redactErr(err error) error {
	return redactedError{
		original: err,
		text:     redact.Text(redact.StripControl(err.Error())),
	}
}

// redactedError 包装原 err, 让 Error() 输出脱敏文本,
// 同时保 Unwrap() 让 errors.Is/As 识别原 err 的哨兵 (ErrRecordNotFound 等).
type redactedError struct {
	original error // 原 err, errors.Is/As 走它
	text     string // 脱敏后文本, Error() 输出
}

func (e redactedError) Error() string {
	return e.text
}

func (e redactedError) Unwrap() error {
	return e.original
}
