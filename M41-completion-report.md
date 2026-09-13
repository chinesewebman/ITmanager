# M41 Completion Report — CI -race + fixture 修复 + eventbus race fix

## Delivered

1. **CI 加 `-race` flag** (`.github/workflows/ci.yml` line 50) — `go test ./...` → `go test -race ./...`
2. **Fixture 修复** (`internal/api/routes_integration_test.go`) — `genValidToken` + `genTokenWithRole` 自动 `seedUserWithRole`
3. **Cache reset helper** (`internal/middleware/auth_status_cache.go`) — `ResetAuthStatusCacheForTest()` 导出
4. **Eventbus race fix** (`internal/eventbus/eventbus.go`) — `newID` 用 `AddUint64` 返回新值一次性读改
5. **Test marker** (`internal/eventbus/eventbus_test.go`) — `TestPublish_并发安全` 加 race detector 守门注释

## Changed

```
backend/internal/api/routes_integration_test.go        | 13 ++++++++++++-
backend/internal/middleware/auth_status_cache.go      |  7 +++++++
backend/internal/eventbus/eventbus.go                  |  8 ++++++---
backend/internal/eventbus/eventbus_test.go             |  2 ++
.github/workflows/ci.yml                               |  5 ++++-
5 files changed
```

## Validation

### AC-M41-1: `go test -race -count=1 ./...` 0 FAIL
- 27 packages 全 ok ✓
- 实测 65s（含 race detector 开销，可接受）

### AC-M41-2: `go test -count=1 ./...` 0 FAIL (无 race 也过)
- 27 packages 全 ok ✓
- 实测 47s

### AC-M41-3: CI workflow 含 `-race`
- `.github/workflows/ci.yml` line 50: `run: go test -race ./...` ✓

### AC-M41-4: 无 M40 product code 改动
- `git diff da68dc5...HEAD backend/internal/middleware/auth.go` — 仅 M41 commit 240c8de 加 `ResetAuthStatusCacheForTest()` (test helper)
- `auth_status_cache.go` — 加 `ResetAuthStatusCacheForTest()` export wrapper (test helper)
- `auth.go` JWT 路径逻辑 (line 130-140) — **零改动** ✓ (M40 fail-closed 守门)

### AC-M41-5: `go vet ./...` clean
- 除 sqlite3 C warning `zTail = strrchr(zName, '_')` (pre-existing) ✓

### AC-M41-6: `gofmt` clean
- ✓ (`gofmt -l backend/` 无输出)

### Mutation inversion (defensive)

| Step | Mutation | Result |
|------|----------|--------|
| Step 2 (fixture) | 注释 `genValidToken` 的 `seedUserWithRole` 改回 `uuid.NewString()` | `TestRoutes_Protected_带有效token能进handler` FAIL 401 → revert → PASS ✓ |
| Step 4 (race fix) | 改回两步原子 (`atomic.AddUint64` + 单独读 `idCounter`) | `TestPublish_并发安全` FAIL "race detected" → revert → PASS ✓ |

两步 mutation inversion 都按预期反向，证明 fix 是必要的、不是空转。

## Risk

### 残余 1: `TestStats_计数正确` 在 count >= 5 + race 时偶发 FAIL

- **pre-existing**：与本 round race fix 无关
- **根因**：handler dispatch 时序敏感断言（`assert stats.Published >= 3` 在 `wg.Wait()` 之后立刻读 stats，但 dispatcher goroutine 仍可能在并发增减 stats.Published / Dispatched）
- **CI 影响**：零 —— CI 走默认 `count=1`
- **修复路径**（不在 M41 scope）：
  - 用 `assert.Eventually` 或在 `wg.Wait()` 后加小 sleep
  - 或在 `wg.Wait()` 后 sleep 10ms 让 dispatcher 跟上来
  - 或用 atomic 同步点

### 残余 2: M40 fail-closed 设计仍正确

- 测试 fixture 不存在 user → 401 是对的（生产中不会发生，因为 user 删除走软删除）
- M41 没动 M40 product code → 守住了 M40 安全边界
- 但**未来**若有测试想验证 "user 不存在场景"，需要显式 seed 或 mock

### 残余 3: cache reset 频率

- 每个 `setupTestRouter` cleanup 都 reset authStatusCache
- 生产代码路径**不**受影响（生产 cache 不 reset，仅 30s TTL 自然过期）
- `ResetAuthStatusCacheForTest()` 命名明确警告 "生产代码不应调用"

## Status

- **AC-M41-1 ~ AC-M41-6**: 全过
- **CI**: workflow 已切 -race，commits 全部 push 到 origin/main
- **Commits** (6 per round cadence):
  1. `0936e0d` docs(M41): intent-M41.md
  2. `240c8de` test(M41): fixture 修复 + cache reset helper
  3. `78aec0c` fix(M41): eventbus.newID 合并 AddUint64 + 读为单次原子
  4. `793bd1f` test(M41): TestPublish_并发安全 加 race detector 守门注释
  5. `182f621` ci(M41): go test 加 -race flag
  6. (本 commit) docs(M41): CHANGELOG + M41-completion-report.md

## Process retro

**第二次犯同一错**：patch eventbus.go 时把 `var idCounter uint64` 误删
（与 M40 期间的同款错误）。Linter cache 报 "pre-existing" 让我以为
vet 错是误报，**实际是真错**。已落档 pitfall 到
`agentic-pdca-orchestration/SKILL.md`：
> patch 时不要把 "函数 + 紧邻的 package-level 变量" 当成一个 match unit 替换。

下次同类 patch 我会**立刻跑 `go build` 验证**（不是 `go vet`），并把
"函数" 和 "var/const 声明" 分两步 patch。

## Next round candidates

按 PM_QUEUE.json 当前 8 个 pending：
- **G-23** (≤1h) — ticket.Tags 一致性
- **G-30** (≤1h) — smoke 脚本密码泄漏
- **G-32** (≤1h) — gorm/pgx %#v
- **G-21** (≤2h) — asset jsonb 入参规范化
- **G-31** (≤2h) — 同类错误回显点收口
- **G-4** (2-3h) — 账号处置无产品化入口
- **P1-3-MIB** (5-6h subagent) — MIB 浏览器

或开新 round 处理 `TestStats_计数正确` count>=5 偶发 FAIL（≤30min）。
