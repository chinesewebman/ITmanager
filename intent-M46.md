# intent-M46: G-32 gorm logger Error/Trace err 出口过 redact (防 `%#v` 编码错误带值)

## Context

`TODO.md` G-32:
> pgx v5 在类型编码失败时会把**值本身**用 `%#v` 拼进错误 (pgtype/pgtype.go:1905
> 附近), 而 gorm 的 logger 只参数化了 SQL 文本、不处理错误文本.
> 当前无「凭据 → 非字符串列」的调用点, 故非活缺陷;
> 若将来把 token/口令写进非 text 列 (如 `jsonb`、`inet`、`uuid`)
> 并触发编码错误, 凭据会随错误文本落进 G-16 已接级别的 gorm 日志.
> 待办: 评估自定义 gorm logger 只打 SQLSTATE + 表名,
> 或对 `Error()` 出口统一过 `redact.Text`.

**scope 评估**: ≤3h, 1 文件 `backend/internal/database/gorm_logger.go` 加
`redactGormLogger` wrapper (override Error/Trace 把 err 过 redact) +
3 个单测覆盖 Error/Trace/Info 路径 + 27 packages + 47 真 PG PASS.

**为什么 wrapper 而不是改所有 caller**: G-32 真正的「非活缺陷」= 出口未脱敏.
所有调用点写 `svc.Update(...) if err != nil` 走 gorm logger.Trace → 落日志.
一处 wrapper 覆盖所有路径, 等于把 G-16 ship 过的「参数过滤」扩展到「错误文本过滤」.

## Approach

### Step 1: redactGormLogger wrapper (≤30min)

`backend/internal/database/gorm_logger.go` 加 wrapper:

```go
// redactGormLogger 包一层 gormlogger.Interface, 在 Error/Trace 路径
// 把 err 走 redact.Text + StripControl (M46 / G-32).
// 其他方法直接转发 — G-16 ship 的 ParameterizedQueries=true 已经覆盖
// SQL 文本里的参数, 不需再加.
type redactGormLogger struct {
    gormlogger.Interface
}

func (l *redactGormLogger) Error(ctx context.Context, msg string, args ...interface{}) {
    // args 里如果含 error, 过 redact + StripControl
    safe := redactArgs(args)
    l.Interface.Error(ctx, redact.Text(redact.StripControl(msg)), safe...)
}

func (l *redactGormLogger) Trace(ctx context.Context, begin time.Time,
    fc func() (sql string, rowsAffected int64), err error) {
    // err 走 redact + StripControl (Trace 是错误文本的主要出口)
    if err != nil {
        err = redactErr(err)
    }
    l.Interface.Trace(ctx, begin, fc, err)
}

// redactArgs / redactErr 纯函数 (跨包用)
func redactArgs(args []interface{}) []interface{} { ... }
func redactErr(err error) error { ... }  // 包装成 redactedError, 保 Unwrap
```

注: 必须保 `Unwrap()` 让 errors.Is / errors.As 仍能识别 ErrRecordNotFound.

### Step 2: 接线 (≤10min)

`newGormLogger` 改:
```go
func newGormLogger(w io.Writer) gormlogger.Interface {
    gormlogger.RecorderParamsFilter = dropRecorderParams
    return &redactGormLogger{Interface: gormlogger.New(...)}
}
```

### Step 3: 单测 (≤30min)

`backend/internal/database/gorm_logger_test.go` (新):
- `TestRedactGormLogger_Error`: Error(ctx, "...", errors.New("?token=abc")) → 不含明文
- `TestRedactGormLogger_Trace_ErrRedact`: Trace(ctx, t, fc, err) → err.Error() 过 redact
- `TestRedactGormLogger_Trace_ErrUnwrap`: Trace 含 ErrRecordNotFound → errors.Is 仍识别
- `TestRedactGormLogger_Trace_StripControl`: Trace err 含 `\n` → 落日志无控制字符
- `TestRedactGormLogger_Info_PassThrough`: Info/Warn 直接转发 (无 error)

### Step 4: 全包 + 真 PG (≤10min)

- `go test -race -count=1 ./...` → 27 packages 0 FAIL
- `db_smoke.sh` → 47 PASS / 0 FAIL

### Step 5: docs (≤10min)

- `M46-completion-report.md`
- `CHANGELOG.md` M46 section
- `TODO.md` G-32 标 done

## Acceptance Criteria

AC-M46-1: Error(ctx, msg, err) err.Error() 过 redact + StripControl
AC-M46-2: Trace err 过 redact, errors.Is(err, ErrRecordNotFound) 仍识别 (保错误链)
AC-M46-3: 27 packages 0 FAIL + 47 真 PG db_smoke 0 FAIL
AC-M46-4: 现有 logger 配置不变 (SlowThreshold/LogLevel/IgnoreRecordNotFoundError/ParameterizedQueries)
AC-M46-5: 不动其他 caller (gormlogger.Interface API 不变)

## Trade-offs

- **wrapper 而不是改所有 caller**: 一处覆盖所有 gorm logger 出口, 未来加 caller 自动受益
- **保错误链 (Unwrap)**: errors.Is/As 仍识别哨兵 (ErrRecordNotFound, ErrInvalidTransaction)
- **不动 Info/Warn/LogMode**: G-16 已 ship 参数过滤, 这三个路径无 err 文本
- **不只打 SQLSTATE + 表名**: TODO.md 提案的「极简方案」— 但会丢错误细节, 排障退化, 不取

## Validation plan

1. Unit: 5 用例 PASS
2. 全包 + 真 PG
3. mutation inversion: 注释 redact → 3 用例 FAIL → revert → PASS

## Commits (planned)

1. `docs(M46): intent-M46.md`
2. `fix(M46): redactGormLogger wrapper - Error/Trace err 出口过 redact (G-32)`
3. `test(M46): 5 个 redactGormLogger 单测`
4. `docs(M46): CHANGELOG + completion report + TODO G-32 标 done`

## Status

- Scope: PM-direct ≤4h (Poison 2026-09-13 扩 scope 授权)
- Author: hermes@local
- Branch: main
- pm-tick 已 surface (side-path ready: G-32)
- 自主起 (Poison 扩 scope 后不请示)
