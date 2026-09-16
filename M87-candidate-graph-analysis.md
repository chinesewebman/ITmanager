# M87-candidate — G-5 主动失效收口 — Graph Analysis

**Round**: M87-candidate (loop cycle 17)
**Date**: 2026-09-16
**Author**: PM-direct

## 1. 节点图 (Node Graph)

### 1.1 Service ↔ Middleware Cache 联动图 (核心收紧)

```
                     ┌────────────────────────────────────────────────┐
                     │  backend/internal/service/user_service.go        │
                     │                                                │
                     │  applyUserUpdate (M87 起, commit 后调):         │
                     │    1. tx.Clauses(Locking{UPDATE})               │
                     │       .First(&target)  // 持锁读旧值           │
                     │    2. checkUserUpdateGuards(tx, &target,        │
                     │                                updates, actor)  │
                     │    3. tx.Model(&User{}).Where("id = ?", id)     │
                     │       .Updates(updates)                          │
                     │    4. tx.First(&out)              // 回读       │
                     │    5. tx.Commit()                                │
                     │                                                │
                     │  ★ M87 新增 (commit 成功后, 失败路径不调):      │
                     │    6. middleware.InvalidateAuthStatusCacheForUser(id)
                     │    7. out.Role = CanonicalRole(out.Role)         │
                     │    8. return &out, nil                           │
                     └───────────────┬─────────────────────────────────┘
                                     │ 调用 (生产路径 hook)
                                     ▼
                     ┌────────────────────────────────────────────────┐
                     │  backend/internal/middleware/                   │
                     │       auth_status_cache.go                      │
                     │                                                │
                     │  InvalidateAuthStatusCacheForUser(id):         │
                     │    defaultAuthStatusCache.invalidate(id)        │
                     │      → delete(map, id)        // 纯 map 操作   │
                     │                                                │
                     │  (cache 内容被清; 下次 lookupUserStatus 必读 DB) │
                     └───────────────┬─────────────────────────────────┘
                                     │
                                     ▼ (cache.get → false → DB 读)
                     ┌────────────────────────────────────────────────┐
                     │  AuthMiddleware JWT path:                       │
                     │    claims, _ := VerifyToken(...)               │
                     │    status, _ := lookupUserStatus(db, userID)    │
                     │      → cache miss → db.Raw("SELECT status      │
                     │         FROM users WHERE id = ?", userID)      │
                     │      → 拿到最新 status (inactive)               │
                     │    if status == "inactive":                     │
                     │        apierr.Unauthorized(c, "用户已被禁用")   │
                     │        c.Abort()                                │
                     │      → 401 ✓                                    │
                     └────────────────────────────────────────────────┘
```

### 1.2 M40 ↔ M87 cache 生命周期图

```
                   M40 ship (2026-09-13)              M87 ship (2026-09-16)
                   ─────────────────────              ─────────────────────

TTL 30s 进程内     ┌─────────────────┐               ┌─────────────────┐
cache:              │  cache.get(id)  │               │  cache.get(id)  │
                    │  ├─ hit → 返    │               │  ├─ hit → 返    │
                    │  └─ miss →      │               │  └─ miss →      │
                    │       db.Raw    │               │       db.Raw    │
                    │       set(...)  │               │       set(...)  │
                    │                 │               │                 │
写路径 (Update)     │  ✗ 不调          │               │  ✓ 调            │
                    │  invalidate     │               │  Invalidate...  │
                    │                 │               │  (commit 后)    │
                    │                 │               │                 │
时序 (单副本):      │                 │               │                 │
                    │  T0: PATCH       │               │  T0: PATCH       │
                    │  T0..T30s:       │               │  T0..T0+ε:      │
                    │   cache 仍 active│ ←≤30s lag     │   cache 被主动清 │
                    │  T30s:           │               │  T0+ε:           │
                    │   cache 自然过期 │               │   下一请求 401  │
                    │   → DB 读 → 401 │               │                 │
                    │                 │               │  ≈0s lag        │
                    └─────────────────┘               └─────────────────┘

                    trade-off: ≤30s 全副本生效        trade-off: 该副本 ≈0s, 其他副本 ≤30s
                    (cache per-process)               (M40 trade-off 本质沿用)
```

### 1.3 5 场景 db_smoke 实测范本 (本 round 暂未加, 范本登记)

```
S1 预热 cache 后 PATCH inactive → 401
   预 fill cache("active") → PATCH → cache 被清 → DB 读 inactive → 401

S2 空 cache PATCH inactive → 401
   cache 空 → PATCH → cache 被清 → DB 读 inactive → 401

S3 PATCH inactive 后等 50ms → 401   ← 真 PG 唯一能验「主动失效不是靠 TTL」的场景
   PATCH → cache 被清 (50ms 远小于 30s TTL) → DB 读 inactive → 401

S4 PATCH 翻回 active → 200
   PATCH inactive → 翻 active → cache 被清 → DB 读 active → 200

S5 PATCH role (不改 status) → cache 条目被清, 下次读 DB 拿到当前 status
   PATCH role → invalidate(id) 是 user_id 粒度 → cache 清 → DB 读 status → 当前 status
   (证明 invalidate 是 user_id 粒度不是 status 字段粒度)
```

### 1.4 handler test 调用链 (PASS-FAIL-PASS 反证)

```
TestRoutes_主动清鉴权statusCache_下次JWT立即401 (M87 新增):

  step 1:  victimToken GET /api/assets
              AuthMiddleware → lookupUserStatus(victimID) → cache miss → DB read active
                            → cache.set(active) → status=active → 200
                            (cache 现在有 stale "active" 等待被 invalidate)

  step 2:  adminToken PATCH /api/users/:victim/status {status: "inactive"}
              AuthMiddleware → admin 校验通过 → userH.UpdateUserStatus
                            → svc.UpdateStatus(...) → applyUserUpdate
                            ★ commit 成功后: middleware.InvalidateAuthStatusCacheForUser(victimID)
                            → cache.get(victimID) == false (条目没了)
                            → return 200

  step 3:  victimToken GET /api/assets
              AuthMiddleware → lookupUserStatus(victimID)
                            → cache.get(victimID) → FALSE (已被主动清) ✓ M87 关键
                            → db.Raw("SELECT status FROM users WHERE id = ?") → inactive
                            → apierr.Unauthorized(c, "用户已被禁用") → 401 ✓

  step 4:  adminToken PATCH /api/users/:victim/status {status: "active"}
              (反向: 重新启用) → commit → invalidate → cache 清 → 200

  step 5:  victimToken GET /api/assets
              cache 清 → DB read active → 200 ✓ (证明拒绝来自 status 而非 token 黑名单)

M1 mutation (剥 applyUserUpdate 末尾的 invalidate):
  step 1:  cache miss → DB → cache.set(active)         [同]
  step 2:  PATCH → commit → invalidate **被剥**         [M1 红点]
                           → cache 仍 active             [stale!]
  step 3:  victimToken GET /api/assets
              cache.get(victimID) → TRUE → status=active → 200  ❌ 期望 401
              → 测试红 ✓ (M87 hook 真在门的反证)
```

## 2. M86 ↔ M87 范本对比 (mutation inversion 范本系列)

| 维度 | M86 (响应字段守卫) | M87 (服务层 → cache 失效联动) |
|---|---|---|
| **守卫对象** | `if canManage { ... }` 响应字段块 | `applyUserUpdate` 末尾 `InvalidateAuthStatusCacheForUser(id)` 调用 |
| **触发点** | handler 层 (integration_handler.go:117-155) | service 层 (user_service.go:185) |
| **反证方式** | 临时把 url/user 移出 if 块 → 多角色看到字段 → 红 | 临时把 invalidate 行注释掉 → cache 仍 active → 401 期望 200 → 红 |
| **检测能力** | 显式断言「admin/ops_admin 可见 url, 其他 5 角色不可见」 | 显式断言「PATCH 后 victim 下一请求 401, 不依赖手动 invalidate」 |
| **trade-off 沿用** | P2-1 既有 `has_*` 同款分级范本 | M40 ship 的 30s TTL cache (m87 把该副本 lag 收口到 ≈0s) |
| **测试层级** | handler 白盒 7 角色矩阵 + URL 字面暴露 | handler 端到端 victim JWT 反证 + sqlite service cache 状态断言 |
| **mutation 实证位置** | 真 PG + handler (M86 §3 M1) | sqlite handler 端到端 (本 round) — 真 PG 范本登记留 M88+ |
| **范本代号** | D (响应字段守卫) | E (服务层 → cache 失效联动) |

**范本系列演化**:
- M82 范本 A (业务代码 mutation) — `service.alertService.writeNotificationTrigger` 过滤逻辑
- M83 范本 B (CI 守门 mutation) — `bash scripts/db_smoke.sh` 守门
- M85 范本 C (业务并发窗口 mutation) — `cmd/set-role` SELECT FOR UPDATE 锁
- M86 范本 D (响应字段守卫 mutation) — `if canManage { ... }` 字段块
- **M87 范本 E (服务层 → cache 失效联动 mutation) — 本 round 新增**

E 与 A 同形 (都是业务代码 mutation), 但守卫对象不同: A 守业务逻辑正确性, E 守**跨层副作用** (service 操作触发 middleware 状态变更)。

## 3. codegraph 双轨 wiring 验证

按 M47-M51-RETRO 5 round 漏跑双轨的修正, M52+ 起每 round 必跑 (5 步 ~15s):

### 3.1 service 节点 grep
```bash
$ grep -rn 'middleware\.InvalidateAuthStatusCacheForUser' backend/internal/service/
backend/internal/service/user_service.go:185:	middleware.InvalidateAuthStatusCacheForUser(id) // M87 active invalidation
```

仅 1 callsite, 在 `applyUserUpdate` 末尾, happy path. 失败路径 (事务回滚) 不调 — `user_service.go:179-181` `if err != nil { return nil, err }` 早返. 既有 24 个 user_service_test.go 测试 + M87 新增 2 个 sqlite 测试全绿 = 0 退化.

### 3.2 handler → service → middleware 链路 grep
```bash
$ grep -rn 'InvalidateAuthStatusCacheForUser\|h\.svc\.\(Update\|UpdateStatus\|UpdateRole\)' backend/internal/api/handlers/ backend/internal/api/routes_integration_test.go
backend/internal/api/handlers/user_handler.go:162:	u, err := h.svc.Update(...)
backend/internal/api/handlers/user_handler.go:185:	u, err := h.svc.UpdateStatus(...)
backend/internal/api/handlers/user_handler.go:204:	u, err := h.svc.UpdateRole(...)
backend/internal/api/routes_integration_test.go:2104:	middleware.InvalidateAuthStatusCacheForUser(victim) // M61 workaround (M87 ship 后仍保留)
backend/internal/api/routes_integration_test.go:2112:	middleware.InvalidateAuthStatusCacheForUser(victim) // M61 workaround
backend/internal/api/routes_integration_test.go:2116:	middleware.InvalidateAuthStatusCacheForUser(victim) // M61 workaround
```

3 个 handler 调用 service.Update/UpdateStatus/UpdateRole → service.applyUserUpdate → middleware.InvalidateAuthStatusCacheForUser. 链路完整.

**注意**: M61 ship 的 `TestRoutes_禁用账号后JWT立即失效` 仍用 3 处手动 `InvalidateAuthStatusCacheForUser(victim)` 调用. 这是 M61 ship 时的 workaround (那时 service 还没有 active invalidation). M87 ship 后理论上**可以删**, 但本 round **不删**:
- M61 测试等价于「无论 hook 是否在门, 用手动 invalidate 都能验」, 仍 GREEN
- M87 新测试 `TestRoutes_主动清鉴权statusCache_下次JWT立即401` 是「**不**手动 invalidate 也 GREEN」的反证
- 两个测试并存, 不互相替代; 删 M61 测试会丢失「手动 invalidate 路径」的覆盖

### 3.3 middleware cache 节点 grep
```bash
$ grep -n 'func ' backend/internal/middleware/auth_status_cache.go
42:func (c *authStatusCache) get(userID string) (string, bool)
53:func (c *authStatusCache) set(userID, status string)
67:func (c *authStatusCache) invalidate(userID string)
76:func resetAuthStatusCache()
85:func ResetAuthStatusCacheForTest()
88:func SetAuthStatusCacheForTest(userID, status string)    # M87 新增
100:func GetAuthStatusCacheForTest(userID string) (string, bool)  # M87 新增
104:func InvalidateAuthStatusCacheForUser(userID string)    # M87 文档翻新
125:func lookupUserStatus(db *gorm.DB, userID string) (string, error)  # M87 注释补 hook
```

cache 包内部 + 公开 API 一致, 既有 11 个 auth_status_cache_test.go + M87 service / handler 测试全绿 = 0 退化.

### 3.4 docs 节点 grep
```bash
$ grep -rn 'G-5\|active invalidation\|InvalidateAuthStatusCacheForUser' docs/FIX-PLAN-AUTHZ-CLOSURE.md TODO.md
docs/FIX-PLAN-AUTHZ-CLOSURE.md:201-207: 「不改 JWT「不查库」的架构」段加 M40+M87 联合 ship 注释 + G-5-2 followup
TODO.md:62 (M87 ship 后): [x] G-5 已签发 JWT 不查库 → 已 ship M40 + M87 联合
```

3 处文档节点一致 (FIX-PLAN / TODO / completion report 互相印证).

## 4. 多副本部署 trade-off 节点图 (G-5-2 followup)

```
           ┌──────────────┐                ┌──────────────┐
           │   Replica A  │                │   Replica B  │
           │              │                │              │
admin →    │ PATCH        │                │              │
PATCH      │ /users/:id/  │                │              │
request    │ status       │                │              │
           │ ↓            │                │              │
           │ svc.Update   │                │              │
           │ ↓ commit     │                │              │
           │ ★ invalidate │ ←── 仅 A 生效 ──│ cache 仍     │
           │   (per-      │     (per-process│ "active"    │
           │    process)  │      in-process)│              │
           │ ↓            │                │              │
           │ 200 OK       │                │              │
           └──────────────┘                └──────────────┘
                  ▲                                ▲
                  │                                │
                  │ admin (200)                    │ victim JWT (另一副本)
                  │                                │ (cache 仍 active → 30s 内仍 200)
                  │                                │
                                            ≤30s 后 cache TTL 过期
                                            → DB 读 inactive → 401

G-5-2 followup: Redis pub/sub 广播 invalidate 到所有副本
                 → 全局 ≈0s 生效 (所有副本)
                 → 触发条件: 多副本部署上线 (运维动作, 不是代码)
```

M87 不动这个 trade-off (D11), 因为 (1) M40 ship 时已登记这是本质 trade-off, (2) 真要全局近 0s 需引入 Redis pub/sub 广播机制, 跨基础架构决策, 属独立 round. 当前 ≤30s 已够运维封禁场景, 接受现状.
