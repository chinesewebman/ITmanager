# intent-M41: CI 加 -race flag + 修 baseline 测试 fixture + 修 eventbus race

## Context

CI 当前 `go test ./...` 没带 `-race`。本地跑 `-race` 发现：

1. **eventbus 真 race** — `internal/eventbus/eventbus.go:newID` 用
   `atomic.AddUint64(&idCounter, 1)` + 后续 `%d` 读 `idCounter` 两步
   不是原子的，race detector 在 `TestPublish_并发安全` 抓出 DATA RACE
   （两个 goroutine 各自的 Add + Read 交错）。**这是 race detector
   上线后第一个被它抓的真 bug**——证明守门有活要干，不是空设。

2. **8 个 baseline test FAIL**（无 race 也 FAIL，非 race detector
   引入）—— M40 引入的 `lookupUserStatus` 在 JWT 路径下对**用户不
   存在**默认作 `inactive` 处理（防止物理删除用户的旧 JWT 仍可用）。
   这是 fail-closed 安全设计，**生产正确**；但测试 fixture 普遍
   没为 `genValidToken` 创 user，导致所有 JWT-鉴权的 test 全打到
   401 而不是 200/403/404。具体 8 个：

   | Test | 当前 | 期望 | 真因 |
   |------|------|------|------|
   | TestMiddleware_Auth_带Token_正常进入Handler | 401 | ≠401 | user 不存在 → inactive |
   | TestRoutes_Protected_带有效token能进handler | 401 | ≠401 | 同上 |
   | TestRoutes_CF4_assets_export不被id吞 | 401 | ≠401 | 同上 |
   | TestRoutes_CF4_alerts_stats不被id吞 | 401 | ≠401 | 同上 |
   | TestRoutes_DiagnosticTimeline_无效UUID返400 | 401 | 400 | 同上（无效 UUID → user 不存在） |
   | TestRoutes_能力矩阵_无权限被拒 | 401 | 403 | 同上（403 在 capability 检查前就 401 了） |
   | TestRoutes_能力矩阵_有权限放行 | 401 | ≠403 | 同上 |

   加上 `TestPublish_并发安全`（eventbus race 触发），共 8 个。

3. **额外问题**：cache 跨 test 污染 —— `defaultAuthStatusCache` 是
   package singleton，30s TTL。中间件包的 TestMain 已加 reset，
   但 `internal/api` 包的 test 没 reset，跨 test 复用同一 userID
   会拿到上次 cache 的状态。

## Goal

让 `go test -race ./...` 在 CI 上 PASS，覆盖 eventbus race fix +
fixture 修复 + CI 加 `-race`。

## Non-goals

- **不改 M40 product code**（`lookupUserStatus` 保持 fail-closed +
  missing user = inactive 设计；这是 M40 安全设计，测试 fixture
  错误就该修 fixture，不该开 production 后门）。
- 不加新 helper（`seedUserWithRole` 已存在，复用它）。
- 不改 capability 矩阵或权限检查逻辑。
- 不动 `models.User` schema 或 user 创建流程。
- 不加新 CI gate（仅在 `go test` step 加 `-race`，不动 lint /
  vet / coverage）。

## Approach

### Step 1: 修 `genValidToken` fixture (≤1h)

`internal/api/routes_integration_test.go:225-232` —— 在 token 生成前
调 `seedUserWithRole(t, "admin")` 拿到真实 userID，再用它生成 token。
这样所有 12 处 `genValidToken(t)` 调用自动获得 fixture user，零
diff 到 caller。

### Step 2: 修 `genTokenWithRole` fixture (≤30min)

`internal/api/routes_integration_test.go:657-663` —— 同样在生成 token
前 seed 对应 role 的 user。覆盖 `能力矩阵_*` 两个 test 的所有
`genTokenWithRole` 调用（通过 `requestAs` helper，间接 N 次）。

### Step 3: 加 cache reset 到 `setupTestRouter` (≤30min)

`internal/api/routes_integration_test.go:107` —— cleanup 里调
`middleware.resetAuthStatusCache()`，避免 package singleton 跨 test
污染（30s TTL + 上 test 留下的 inactive 缓存）。

### Step 4: 修 eventbus race (≤15min)

`backend/internal/eventbus/eventbus.go:354` —— 改 `newID()` 用
`AddUint64` 返回新值一次性读改（避免「先 Add 再读」两步 race）。

```go
// before
func newID() string {
    atomic.AddUint64(&idCounter, 1)
    return fmt.Sprintf("%d-%d", time.Now().UTC().UnixNano(), idCounter)
}
// after
func newID() string {
    n := atomic.AddUint64(&idCounter, 1)
    return fmt.Sprintf("%d-%d", time.Now().UTC().UnixNano(), n)
}
```

### Step 5: 加单测捕 eventbus race regression (≤30min)

复用现有 `TestPublish_并发安全`（已在跑）—— `-race` flag 上线后
这测试自动守门。新增一行注释到 test file 标 "// M41: race detector
守住，不删"。

### Step 6: CI 加 `-race` flag (≤10min)

`.github/workflows/ci.yml:50` —— `go test ./...` → `go test -race ./...`。

### Step 7: docs + commit (≤30min)

- `CHANGELOG.md` M41 section
- `M41-completion-report.md` (5 sections)
- 6 commit, 6 push per round cadence

## Acceptance Criteria

AC-M41-1: `go test -race -count=1 ./...` 0 FAIL（baseline 7 FAIL +
eventbus 1 FAIL 共 8 全修）
AC-M41-2: `go test -count=1 ./...` 0 FAIL（race 模式挂、normal 模式
也不挂——双重验证）
AC-M41-3: CI workflow `ci.yml` line 50 含 `-race`
AC-M41-4: 无 M40 product code 改动（`git diff da68dc5...M41_commit_hash
backend/internal/middleware/auth.go backend/internal/middleware/auth_status_cache.go`
应为空 / 仅注释微调）
AC-M41-5: `go vet ./...` clean（除 sqlite3 C warning pre-existing）
AC-M41-6: `gofmt` clean

## Trade-offs

- **Test runtime**：`-race` 加内存开销 + 重排，可能让 CI 时间 +30~60%。
  实测 baseline `go test -race ./...` 跑 65s（race detector 额外开销
  + 中间件 panic recovery 测试延后），是可接受的代价。
- **M40 fail-closed 不动**：测试 fixture 完整 = 8 FAIL 修复 + 不
  开 production 后门。如果用户**生产中**遇到 user 不存在场景
  （应该不会，user 删除走软删除 `status=inactive`），会返 401，
  这是对的。
- **Cache reset 频率**：每个 `setupTestRouter` cleanup 都 reset =
  每 test 都冷启动 cache。生产代码路径不受影响（生产 cache 不
  reset，仅 30s TTL 自然过期）。

## Validation plan

1. `go test -race -count=1 ./...` → 0 FAIL
2. `go test -count=1 ./...` → 0 FAIL（race 关闭也过）
3. `go vet ./...` → 仅 sqlite3 pre-existing warning
4. `gofmt -l backend/` → 无 diff
5. mutation inversion：临时把 `genValidToken` 改回不 seed user →
   AC-M41-1 FAIL → revert → PASS
6. mutation inversion：临时把 `newID` 改回两步原子 → `TestPublish_并发安全`
   FAIL → revert → PASS

## Risks

- **scope 扩张**：原 M41 只 "CI 加 -race + 修 eventbus race"，扩到
  包含 fixture 修复（必要）。总工时从 ≤1h 增到 ~2-3h。
- **fixture 影响面**：12 + N 处 `genValidToken/genTokenWithRole` 调用
  通过 helper 集中修，但若某个 test 对 user 字段有特殊要求（比
  如 username 唯一约束），seed 后会冲突。需要跑一遍验证。
- **cache reset 兼容性**：中间件包已用 `resetAuthStatusCache()`，
  api 包首次引入；若 reset 函数签名变化需要同步。

## Commits (planned, per commit + push)

1. `docs(M41): intent-M41.md`
2. `test(M41): genValidToken/genTokenWithRole 自动 seed user fixture`
3. `test(M41): setupTestRouter cleanup reset authStatusCache`
4. `fix(M41): eventbus.newID 合并 AddUint64 + 读为单次原子`
5. `ci(M41): go test 加 -race flag`
6. `docs(M41): CHANGELOG + M41-completion-report.md`

## Status

- Scope: PM-direct (≤3h)
- Author: hermes@local
- Branch: main
- Pre-flight: 已跑过 baseline `go test -race ./...` 确认 8 FAIL 真因
