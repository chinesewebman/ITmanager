# M45 Completion Report — G-31 apierr 4xx 路径脱敏收口

## Delivered

`TODO.md` G-31 收口:
> `apierr.BadRequest(c, "...:"+err.Error())` 全仓约 30 处, 形态相同 —
> 将来任何一处接上第三方客户端错误就会复现 G-28.

**实际范围**: 38 个 4xx caller (`grep 'BadRequest(c, .*err.Error'`),
全部走 `apierr.BadRequest/Unauthorized/Forbidden/NotFound/Conflict` 5 个 helper.
**修法**: 在 5 个 helper 入口加 `sanitizeMessage(msg) = redact.Text(redact.StripControl(msg))`,
**单文件改动**, 不动 38 个 caller (apierr API 不变).

## 关键代码

```go
// apierr.go — 5 个 4xx helper 统一过 sanitizeMessage
func BadRequest(c *gin.Context, message string) {
    Respond(c, http.StatusBadRequest, CodeBadRequest, sanitizeMessage(message), nil)
}
// Unauthorized/Forbidden/NotFound/Conflict 同款

// sanitizeMessage 4xx 路径统一脱敏 + 去控制字符 (M45 / G-31).
// 顺序: StripControl → Text (同 apierr.Respond 5xx 路径的「内层先 Strip 再 Text」,
// 否则 CR/LF 截断 redact.Text 的值类, 外层 Strip 把尾部接回去 = 泄漏).
// 中文静态文案 no-op (StripControl 只去 < 0x20 控制字符, Text 只去 URL/凭据类).
func sanitizeMessage(msg string) string {
    return redact.Text(redact.StripControl(msg))
}
```

## Changed files

- `intent-M45.md` (新)
- `backend/internal/apierr/apierr.go` (+16/-6 lines, 5 个 4xx helper + sanitizeMessage)
- `backend/internal/apierr/sanitize_test.go` (新, 4 个用例)
- `TODO.md` (G-31 `[ ]` → `[x]`, M45 ship 标)
- `CHANGELOG.md` (M45 section inserted before M44)

## Validation

### Unit (4 new + 3 existing 全 PASS)
- `TestBadRequest_StripControl`: 含 `\n\r\x00` 的 message, response body 不含控制字符
- `TestBadRequest_RedactToken`: `?token=abc123secret` → 含 `***`, 不含明文
- `TestBadRequest_ChineseNoop`: `"工单标题不能为空"` → 原文不变 (no-op 验证)
- `TestNotFound_StripControl`: NotFound helper 也过 sanitizeMessage

### 全包 + 真 PG
- `go test -race -count=1 ./...` → **27 packages 全绿**
- `DOCKER='sudo -n docker' bash scripts/db_smoke.sh` → **47 PASS / 0 FAIL**

### Mutation inversion
- `sanitizeMessage` 永返原 msg → `TestBadRequest_StripControl/RedactToken/NotFound_StripControl` 3 个 FAIL
- `TestBadRequest_ChineseNoop` PASS (no-op 静态文案本就不该脱敏)
- revert → 4/4 PASS

## Process retro

### 教训 1: 一开始我把 G-31 scope 想太大
- 第一次想: 改 38 个 caller + lint + 测试钉住 → ≥3h, 超 PM-direct ≤2h 边界
- 重新读 apierr.go: **5xx 已过 redact, 4xx helper 入口没** → 单文件改动 ≤30min
- 教训: 写 intent 前先 grep caller 数量 + 看现有 helper API, 不要"假设需要大改"

### 教训 2: Chinese noop 用例不冗余
- 我原本担心"加脱敏会不会破坏中文" → 加用例验证 no-op
- mutation inversion 时这用例**还 PASS**, 但其它 3 个 FAIL → 验证用例**不冗余**:
  守的是不同性质 (静态文案 vs 凭据 vs 控制字符), 不互为覆盖

### 教训 3: 复用 5xx 已 ship 的「Strip → Text」顺序
- apierr.Respond 5xx 路径注释里写「内层先 Strip 再 Text, 否则 CR/LF 截断」
- M45 直接复用同款, 注释里 cross-reference 5xx 注释
- 不重复发明, 用同一口径

## Risk & 残余

### 已 ship
- 5 个 4xx helper 入口全过 redact + StripControl
- 38 个 caller API 不变 (自动受益)
- 27 packages + 47 真 PG 全 PASS
- mutation inversion 实证

### 未 ship (明确不在 scope)
- **38 个 caller 仍拼 `err.Error()`**: 是 caller 的写法问题, 但 sanitizeMessage 已兜底.
  进一步收紧需 caller 重构, 跨多文件, 留 G-31 backlog.
- **lint 钉住"必须过 redact"**: 留 G-31 backlog, scope 太小.
- **OpenAPI / frontend 同步**: 无影响 (response schema 不变).

### 已 ship 但需观察
- sanitizeMessage 跑在所有 4xx 出参上, 性能 negligible (单字符串处理).
- 中文静态文案 no-op 验证 PASS (TestBadRequest_ChineseNoop).

## Status

- Scope: PM-direct (≤1h, 实际 ~20min)
- Author: hermes@local
- Branch: main
- 4 commits: `3ac6ca2` / `94be0d9` / `17acada` / (待 step 4 docs)
- All pushed ✓
- **Poison 2026-09-13 批评"重复停顿"后自主起 M45** (queue 顶 G-31)
