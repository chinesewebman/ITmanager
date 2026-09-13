# intent-M45: G-31 apierr 4xx 路径也走 redact + StripControl

## Context

`TODO.md` G-31:
> `apierr.BadRequest(c, "...:"+err.Error())` 全仓约 30 处, 形态相同 —
> 将来任何一处接上第三方客户端错误就会复现 G-28.

**实际评估**: apierr.Respond 已经给 **5xx 路径**加了 `redact.Text + StripControl`
(`apierr.go:53-58`). 但 **4xx 路径** (`BadRequest/Unauthorized/Forbidden/NotFound/Conflict`)
直接拼 message, **不进 redact**. `BadRequest(c, "...:"+err.Error())` 调用点
(38 处) 全是 4xx 路径, 因此 G-28 留下的 30 处同类形态**都没过 redact**.

**scope**: ≤1h, 单文件 `internal/apierr/apierr.go` + 1 unit test.

## Approach

### Step 1: apierr 4xx 路径加 redact + StripControl (≤15min)

`apierr.go` 5 个 4xx helper 改内部统一过 redact:
```go
// BadRequest 400 — message 已脱敏 + 去控制字符
func BadRequest(c *gin.Context, message string) {
    Respond(c, http.StatusBadRequest, CodeBadRequest,
        redact.Text(redact.StripControl(message)), nil)
}
// Unauthorized/Forbidden/NotFound/Conflict 同款
```

`Respond` 内部 4xx 路径**不**过 redact (保留 4xx 原始 message 给 caller) —
因为 caller 传的可能是"工单不存在"等中文静态文案. 但**所有 4xx helper 入口**都过.

### Step 2: 单测 (≤15min)

`apierr_test.go` 加 3 个用例:
- `BadRequest(c, "abc\ndef")` → message 不含 `\n` (StripControl)
- `BadRequest(c, "?token=abc123")` → message 不含 `token=abc123` (redact)
- `BadRequest(c, "中文静态")` → message 含"中文静态" (no-op 验证)

### Step 3: 真 PG db_smoke (≤10min)

不动 db_smoke (无关), 跑全包 + 27 packages 0 FAIL 验证.

### Step 4: docs (≤5min)

- `M45-completion-report.md`
- `CHANGELOG.md` M45 section
- `TODO.md` G-31 标 done

## Acceptance Criteria

AC-M45-1: `apierr.BadRequest(c, msg)` msg 含 `\n` / `token=` 时, response body 已脱敏
AC-M45-2: `apierr.BadRequest(c, "中文静态文案")` no-op 不变
AC-M45-3: 27 packages 0 FAIL
AC-M45-4: 不动 38 个 caller (apierr API 不变)

## Trade-offs

- **是否改 Respond 内部 4xx 路径**: 否. `Respond` 是通用入口, 5xx 必须 redact (内部错),
  4xx 保留 caller 决定 (有些 caller 写静态文案, 不该 redact 掉"中"字等).
- **5xx 不变**: 已 ship, 不动.
- **3 处 lint/测试钉住**: 不做, scope 太小. 留 G-31 backlog.

## Validation plan

1. Unit: `apierr_test.go` 3 用例 PASS
2. 全包: `go test -race -count=1 ./...` 27 packages 0 FAIL
3. mutation inversion: 注释 StripControl → 含 `\n` 用例 FAIL → revert → PASS

## Commits (planned)

1. `docs(M45): intent-M45.md`
2. `fix(M45): apierr 4xx 路径加 redact + StripControl (G-31 收口)`
3. `test(M45): 3 个 apierr 4xx 脱敏单测`
4. `docs(M45): CHANGELOG + completion report + TODO G-31 标 done`

## Status

- Scope: PM-direct (≤1h)
- Author: hermes@local
- Branch: main
- pm-tick 已 surface (side-path ready: G-31)
- 自主起 (Poison 2026-09-13 批评"重复停顿 + 自主解决")
