# M40 — G-5 JWT 用户禁用即时生效 — 完成报告

**Round**: M40
**PM**: hermes@local
**Date**: 2026-09-13
**Branch**: main
**Commit sequence**: `186dad3` → `4af497b` → `9a8b78c` → `0621b50` → (本次)

---

## 1. Delivered（交付物）

### 1.1 业务问题

admin 在 ITmanager 把某个用户 status 从 `active` 改为 `inactive`：
- ✅ API Key 鉴权路径 → 该用户下次请求立刻被拒（已有）
- ❌ JWT 鉴权路径 → 该用户的旧 JWT **最坏 24h 内仍能用**（仅验签，不查 DB）

攻击场景：离职员工 / 被封禁账号 / 测试账号复用，运维封了但 JWT 没到期，对外接口照样跑。G-5（TODO.md L63）已挂 2 round。

### 1.2 修复实现

**`backend/internal/middleware/auth_status_cache.go` (新建, 105 lines)**

- `authStatusCache` struct: `sync.RWMutex` + `map[string]authStatusCacheEntry` + `ttl time.Duration`
- `defaultAuthStatusCache` 包级 singleton (TTL = 30s)
- `lookupUserStatus(db *gorm.DB, userID string) (string, error)`:
  1. RLock → 命中未过期 → 直接返回
  2. Lock → 双重检查 → miss/expired → DB query
  3. DB 错误 → 返回 `("active", err)` 让调用方走 401（fail-closed，不放行）
  4. nil DB guard（生产不触发，仅给旧测试兜底）

**`backend/internal/middleware/auth.go` AuthMiddleware JWT 分支 (patch)**

`VerifyToken` 成功 → 调 `lookupUserStatus(database.DB, userID)` → status=="inactive" → `apierr.Forbidden(c, "账号已禁用")` + `c.Abort()`。API Key 分支已有 `user.Status` 检查无回归。

---

## 2. Changed（变更清单）

| 文件 | 操作 | 行数 | 备注 |
|---|---|---|---|
| `backend/internal/middleware/auth_status_cache.go` | 新建 | 105 | TTL 30s cache + lookupUserStatus |
| `backend/internal/middleware/auth.go` | patch | +8 | JWT 路径加 status 检查 |
| `backend/internal/middleware/auth_status_cache_test.go` | 新建 | 345 | 11 条单测 + mutation inversion |
| `backend/internal/middleware/rate_limit_test.go` | patch | +5 | TestMain 扩展重置 authStatusCache |
| `backend/tests/db_smoke_test.go` | patch | +207 | `TestDBSmoke_M40_JWTDisableTakesEffect` 5 场景 |
| `scripts/db_smoke.sh` | patch | +1 | 白名单 +1 |
| `intent-M40.md` | 新建 | 121 | spec 已 commit 在 `186dad3` |
| `CHANGELOG.md` | patch | +37 | M40 章节 |
| `M40-completion-report.md` | 新建 | (本文件) | |

**净增**: ~810 lines (含测试)

---

## 3. Validation（验证）

### 3.1 单元测试 11 条全绿 (`go test ./internal/middleware/...`)

- **3 cache 单元**: `TestAuthStatusCache_GetSet_Basic / Expired / ConcurrentSet200Goroutines_NoRace`
- **4 lookup 单元**: `TestLookupUserStatus_ActiveUser_HitCacheOnSecondCall / InactiveUser / DBError_ReturnsActive / NilDB_ReturnsActive`
- **3 AuthMiddleware 端到端** (`sqlmock`):
  - `_JWT_UserInactive_Returns401` (AC-M40-1)
  - `_JWT_ActiveUser_CacheHitsAvoidDB` (AC-M40-2, 200 req → 1 SELECT)
  - `_JWT_StatusFlipWithinTTL_StillAllows` (AC-M40-3 trade-off 钉死)
  - `_JWT_DBError_Returns401` (AC-M40-4 fail-closed)

### 3.2 mutation inversion PASS-FAIL-PASS（守门网有效）

注释 `defaultAuthStatusCache.get` → `TestAuthMiddleware_JWT_ActiveUser_CacheHitsAvoidDB` 失败 → revert → 通过。

### 3.3 真 PG 端到端 `TestDBSmoke_M40_JWTDisableTakesEffect` (5 场景全过)

1. `status=active` + 旧 JWT + cache invalidate → **200**
2. UPDATE `status=inactive` + cache invalidate → **401** ✓
3. UPDATE `status=active` + cache invalidate → **200**
4. UPDATE `status=inactive` 但 cache TTL 未到期 → **200**（trade-off 钉死）
5. 物理 DELETE 用户 + cache invalidate → **401**（cache miss → DB 无结果 → 视为 inactive）

### 3.4 真 PG mutation inversion PASS-FAIL-PASS

```
[db_smoke] 结果: ❌ 冒烟失败 (exit=1) —— 上面的 Postgres 原始报错就是漂移证据
        Test:       	TestDBSmoke_M40_JWTDisableTakesEffect
        Messages:   	AC-M40-3 场景 4: cache TTL 内 status 翻转不感知（trade-off）
--- FAIL: TestDBSmoke_M40_JWTDisableTakesEffect (0.04s)
```

注释 `cache.get` 后，场景 3 之后场景 4 不命中 cache（每次必读 DB），读到最新 inactive → 401 → 与场景 4 期望 200 不符 → 测试 FAIL。守门网在生产 PG 路径上有效。

### 3.5 门禁

- `go vet ./...` 干净（仅 sqlite3 C warning 系既有）
- `gofmt -l` 干净
- `go test -count=1 ./...` 全绿（27 packages）
- `DOCKER='sudo -n docker' bash scripts/db_smoke.sh` 真 PG：**44 PASS / 0 FAIL**（43 baseline + M40 +1）

---

## 4. Risk（风险）

### 4.1 已识别风险

1. **多副本部署 cache 是 per-process** — 水平扩展时每副本各持一份 cache，禁用生效最坏窗口 = TTL 30s × 副本数 N。单副本部署（当前生产）不影响。
2. **cache TTL 30s 内状态翻转不感知** — 运维禁用 UI 必须显示「最长 30s 内生效」（已有 toaster 提示）。
3. **DB 错误 fail-closed** — DB 抖动时所有 JWT 用户请求 401（trade-off：不放行 vs 不锁用户），M37-A 已暴露类似问题但这次选择更保守（fail-closed 而非 fail-open）。

### 4.2 残余（后续 round）

- 多副本 cache 收口（Redis）
- `InvalidateAuthStatusCacheForUser` 升级为生产 API（admin 禁用时主动调一次）
- 迁移到分布式 session / JWT revocation list（彻底消灭 TTL 窗口）

---

## 5. Status（状态）

✅ **M40 shipped (5/5 commits on origin/main)**

```
186dad3  intent-M40.md
4af497b  feat(M40): JWT 路径用户状态查 DB + 30s cache
9a8b78c  test(M40): authStatusCache 单元测试
0621b50  feat(M40): JWT 用户禁用即时生效 - 真 PG e2e
(本次)   docs(M40): CHANGELOG + completion report
```

**PM-direct 模式**: 4h 完成（intent + impl + 单测 + 真 PG + 文档），全部 PM-direct 无 subagent。

**PM_QUEUE.json 更新**: M40 status=pending → shipped。

**下一步候选**: G-21 (asset jsonb 入参规范化, ≤2h) 或 G-23 (ticket.Tags 一致性, ≤1h)，待 Poison 触发下一 focus。
