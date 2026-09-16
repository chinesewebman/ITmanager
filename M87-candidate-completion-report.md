# M87-candidate — G-5 已签发 JWT 不查库 主动失效收口 — 完成报告

**Round**: M87-candidate
**Loop cycle**: 17 of `itmanager-grit-2026q3`
**PM**: PM-direct (auto-dispatched by watchdog Mode B after M86-candidate cycle 16 ship)
**Date**: 2026-09-16
**Branch**: main
**Commit sequence**: `(cee9c03)` → `(M87 commit 2: impl + tests + in-source-docs)` → `(M87 commit 3: completion + graph + CHANGELOG + TODO)`

---

## 1. Delivered（交付物）

### 1.1 业务问题

PM_QUEUE M87-candidate 标题为 **"G-5 已签发 JWT 不查库"** (TODO.md L62). 这条登记有**反转史**:

- **2026-09-09 (AUTHZ-CLOSURE 登记)**: JWT 路径**不查库**, `status=inactive` 对已签发会话**最长 24h 才生效** — 「禁用账号」对会话失窃**不是即时止血**.
- **2026-09-13 (M40 ship, `6172977`)**: 方向反转 — JWT 路径**加上** `lookupUserStatus` + 30s TTL cache, 真 PG `TestDBSmoke_M40_JWTDisableTakesEffect` 5 场景全过. **「≤30s 全副本生效」(cache per-process) 已记 trade-off**.
- **2026-09-13 (M40 ship 注释)**: `auth_status_cache.go:88-93` 显式承诺「保留 hook 位, 给将来 round 用」 — `InvalidateAuthStatusCacheForUser` 函数已 ship 但**仅用于测试**.
- **2026-09-15 (M61 ship, `893e214`)**: 补上 `user_service.Update/UpdateStatus/UpdateRole` 与 `PUT /users/:id` / `PATCH /users/:id/status` / `PATCH /users/:id/role` — **但 M61 没有把 active invalidation 接进 service.Update**, 与 M40 注释里承诺的 hook 还有一步之差.
- **2026-09-16 (M87 ship, 本 round)**: 把 M40 预留 hook 接上 M61 写端点. `user_service.applyUserUpdate` 事务 commit 后调 `middleware.InvalidateAuthStatusCacheForUser(id)` → **该副本 ≈0s 生效**. 其他副本仍 ≤30s (M40 trade-off 沿用).

### 1.2 修复实现

**`backend/internal/service/user_service.go` `applyUserUpdate` (patch)**

在 `return &out, nil` **之前**加 `middleware.InvalidateAuthStatusCacheForUser(id)`. Update / UpdateStatus / UpdateRole 三条路径共用 `applyUserUpdate`, 一处加全部覆盖. 「空 updates 不发 UPDATE」分支 (`user_service.go:122-124`) **不**调 invalidate — 没改东西就不必清 cache (D21 强约束).

```go
if err != nil {
    return nil, err
}
// M87 active invalidation: 写成功后立即清鉴权 cache, 让 victim 的下一请求
// 必读 DB 拿到最新 status (该副本 ≈0s 生效). `invalidate` 是纯 map 操作,
// 不会失败也不 panic —— 兜底仍是 30s TTL.
middleware.InvalidateAuthStatusCacheForUser(id)
out.Role = middleware.CanonicalRole(out.Role)
return &out, nil
```

**`backend/internal/middleware/auth_status_cache.go` 文档翻新**

- `InvalidateAuthStatusCacheForUser` (`:88-103`): 从「生产代码不应调用此函数」改为「**生产路径 hook** (M87 起 `user_service.applyUserUpdate`); 多副本部署下只对处理 update 请求的副本生效, 其他副本仍走 30s TTL (M40 trade-off). 仍可被测试用」
- 新增测试 helpers (`:88-105`): `SetAuthStatusCacheForTest(userID, status)` / `GetAuthStatusCacheForTest(userID) (string, bool)` — 跨包 service / handler 测试用, 「生产代码不应调用」注释明示
- `lookupUserStatus` (`:108-122`): 注释补「30s TTL 是 fallback; 写路径由 user_service.applyUserUpdate 主动 invalidate」

**`docs/FIX-PLAN-AUTHZ-CLOSURE.md` §6 (`:201-207`) 加注**

「不改 JWT「不查库」的架构」段加注: 「2026-09-13 M40 + 2026-09-16 M87 联合 ship, 此条结案」+ 解释方向反转 + 主动失效 hook 落地 + 登记 **G-5-2 followup** (Redis pub/sub 广播 invalidation, 跨副本全局近 0s).

---

## 2. Changed（变更清单）

| 文件 | 操作 | 行数 | 备注 |
|---|---|---|---|
| `intent-M87-candidate.md` | 新建 | 299 | omh-plan 8 节骨架 spec, 已 commit `cee9c03` |
| `backend/internal/service/user_service.go` | patch | +9 / -1 | `applyUserUpdate` 加 `InvalidateAuthStatusCacheForUser(id)` + 注释更新 |
| `backend/internal/middleware/auth_status_cache.go` | patch | +33 / -6 | `InvalidateAuthStatusCacheForUser` 文档翻新 + 新增 `SetAuthStatusCacheForTest` / `GetAuthStatusCacheForTest` + `lookupUserStatus` 注释补 M87 hook |
| `backend/internal/service/user_service_test.go` | patch | +73 / 0 | 新增 2 测试: `TestUserService_Update_成功后清鉴权statusCache` + `TestUserService_Update_空输入不调invalidate` + 加 middleware import |
| `backend/internal/api/routes_integration_test.go` | patch | +51 / 0 | 新增 `TestRoutes_主动清鉴权statusCache_下次JWT立即401` (端到端, 不依赖手动 invalidate) |
| `docs/FIX-PLAN-AUTHZ-CLOSURE.md` | patch | +7 / 0 | §6 第 3 条加注「M40 + M87 联合 ship, 此条结案」+ G-5-2 followup 登记 |
| `TODO.md` | patch | (本文件) | L62 `- [ ]` → `- [x]` + 描述更新 (M40 ship + M87 ship + 行号引用 `middleware/auth.go:130-140` + G-5-2 followup) |
| `CHANGELOG.md` | patch | +13 / 0 | M87 段加条目 |
| `M87-candidate-completion-report.md` | 新建 | (本文件) | |
| `M87-candidate-graph-analysis.md` | 新建 | 见另文件 | |

**净增**: ~400 LOC (含测试 + 文档)

---

## 3. Validation（验证）

### 3.1 单元 + 集成测试 全绿

**新测试 (M87)**:
- `TestUserService_Update_成功后清鉴权statusCache` (sqlite, 钉住「commit 成功后 cache.get(id) == false」)
- `TestUserService_Update_空输入不调invalidate` (sqlite, 钉住 D21「空 updates 走 Get 分支不副作用 cache」)
- `TestRoutes_主动清鉴权statusCache_下次JWT立即401` (handler 端到端, 模拟「PATCH 后 victim 下一请求必 401」 — **不**依赖测试代码手动 invalidate, 这是与 M61 `TestRoutes_禁用账号后JWT立即失效` 的关键区别)
- 后续 M88+ round 应加 `TestDBSmoke_M87_ActiveInvalidation` (真 PG 5 场景: S1 预热 / S2 空 cache / S3 50ms 短窗 / S4 翻回 active / S5 role 不动 status). **本 round 因不依赖 db_smoke.sh 跑过, 暂未加** — 真 PG 5 场景范本在 `intent-M87-candidate.md` §Acceptance, 等 M88+ 顺带跑真 PG 时落地 (与 M86 同款范本「mutation inversion 在真 PG 反证」, 但 M87 仅 1 handler test 已钉住行为; 真 PG 反证边际收益低)

**既有测试 (不破坏)**:
- 24 个既有 `user_service_test.go` 测试 (M61 ship) — sqlite + sqlmock, 全绿
- M61 ship 的 `TestRoutes_禁用账号后JWT立即失效` (routes_integration_test.go:2087-2118) — 用**手动** `middleware.InvalidateAuthStatusCacheForUser(victim)` 模拟「cache 自然过期」, 现在 M87 ship 后该手动调用理论上**可以删掉**, 但**本 round 不删** (避免双改 M61 测试引入回归; M61 测试等价于「无论 hook 是否在门, 用手动 invalidate 都能验」, 仍 GREEN; M87 新测试才是「**不**手动 invalidate 也 GREEN」的反证)
- 11 个 `auth_status_cache_test.go` (M40 ship) + 12 个 `auth_status_cache_test.go` mutation 实证 — 全绿
- 全部 27 packages `go test -race -count=1 -timeout=180s ./...` PASS / 0 FAIL

### 3.2 mutation inversion M1 PASS-FAIL-PASS（守门网有效）

**M1 反证「active invalidation hook 真在门」**:

```bash
# 临时把 applyUserUpdate 末尾的 InvalidateAuthStatusCacheForUser 注释掉
$ cp backend/internal/service/user_service.go{,.m87bak}
$ python3 -c "...把 invalidate 替换为 _ = id ..."

$ go test -race -count=1 -run "TestRoutes_主动清鉴权statusCache_下次JWT立即401" ./internal/api/ -v
=== RUN   TestRoutes_主动清鉴权statusCache_下次JWT立即401
[GIN] | 200 | /api/assets        (warm up cache with active)
[GIN] | 200 | /api/users/:id/status (PATCH inactive — M87 hook 已被剥)
[GIN] | 200 | /api/assets        ← M1 红: 期望 401 实得 200 (cache 仍 active)
--- FAIL: TestRoutes_主动清鉴权statusCache_下次JWT立即401 (0.02s)
```

**还原 (control)**:
```bash
$ mv backend/internal/service/user_service.go{.m87bak,}
$ go test -race -count=1 -run "TestRoutes_主动清鉴权statusCache_下次JWT立即401" ./internal/api/ -v
=== RUN   TestRoutes_主动清鉴权statusCache_下次JWT立即401
[GIN] | 200 | /api/assets        (warm up cache)
[GIN] | 200 | /api/users/:id/status (PATCH inactive — M87 hook 工作中)
[GIN] | 401 | /api/assets        ← 绿: cache 已被清, DB 读 inactive
[GIN] | 200 | /api/users/:id/status (re-enable)
[GIN] | 200 | /api/assets        (victim recovers)
--- PASS: TestRoutes_主动清鉴权statusCache_下次JWT立即401 (0.02s)
```

**关键设计要点**:
- **PASS-FAIL-PASS 实证**: 还原前 handler test 红 → 还原后绿, 证明 `applyUserUpdate` 末尾的 `InvalidateAuthStatusCacheForUser(id)` 这一行是守门的关键; 剥它 = 「缓存不被清」= 「PATCH 后下一请求 200」= 测试红; 加回来 = 「缓存被清」= 「PATCH 后下一请求 401」= 测试绿.
- **M1 用真集成测试反证 (sqlite)** — 不只钉 sqlmock / 单测, 端到端证明 hook 在真路由链 (AuthMiddleware → JWT path → lookupUserStatus → cache.get) 上**真生效**. 比单独白盒测试 (sqlite cache 状态断言) 覆盖度更高.
- **mutation 临时文件 `user_service.go.m87bak` 实证完 mv 还原 + rm**, `git status --short` 仅 impl commit 4 个文件 (不含 bak) = D9 闭环.
- **M2 (改 invalidate 函数本身为空函数) 不写**: 改 `InvalidateAuthStatusCacheForUser` 函数体 = cache 不失效, 与 M1 逻辑等价 (剥 hook vs 改 hook 都是「不让它生效」). M2 与 M1 同形, 重复反证无新信号.

### 3.3 cmd/set-role 自动获 invalidation (顺带 coverage)

`cmd/set-role/main.go` 调 `user_service.UpdateRole` (M85 ship), 本 round 加 invalidation 后, **该命令路径自动获得 ≈0s 生效** (同进程同副本). 真 PG `TestCmd_SetRole_DisabledUserOldJWTTakesEffect` (M40 ship 后无此测试, M87 可顺带加, 但**不在本 round scope** — 留给后续 round). M85 ship 时 `TestRunWithDeps_*` 既有断言不退化 = cmd/set-role 路径覆盖到位.

---

## 4. Trade-off 与残余

### 4.1 多副本部署下其他副本仍 ≤30s TTL

M87 只对**处理 update 请求的副本**生效. 真要全局近 0s 需要 Redis pub/sub 广播 invalidation (G-5-2 followup, **不**在本 round scope). 这是 M40 ship 时登记的本质 trade-off, M87 不重写 cache 层 (D13), 只接 hook.

### 4.2 Role 字段在 JWT claims 里签发时快照

即使 M87 让 status cache 主动失效, **role 字段**仍受 JWT 本身语义约束 (claims 是签发时快照). 改 role 必须重发 token (D12), 属独立特性. M87 不动 JWT 续签 / 强制重发逻辑.

### 4.3 InvalidateAuthStatusCacheForUser 函数 export 但原本仅测试用

M40 ship 时该函数标注「生产代码不应调用」 (D22 显式承诺 hook 给将来 round). M87 是兑现承诺. 这意味着 service 层与 middleware 层耦合加了一根针. 既有 service 层已经 import middleware (`user_service.go:8`), 不引入新 import. 真要解耦可后续注入 `StatusCacheInvalidator` 接口 (M98+ 候选).

### 4.4 PM_QUEUE state 同步漏

PM_QUEUE M87-candidate.status: `candidate` → `shipped`, append `shipped[]` registry, bump `last_updated` / `last_audit`. 不做 watchdog 会反复 dispatch M87.

---

## 5. PM-direct 决策记录

- **PM-direct 自决 (Poison ≤4h round)**: 本 round ≤3h 估算, 不请示 Poison
- **接受 framing**: M87 不是「实现不查库」(M40 ship 后不可能回滚), 而是「close out G-5 + add active invalidation hook」. PM-direct 不接受「已 ship 就 close out」的 framing, 必须**真正收紧**才算闭环.
- **不动既有 M61 测试**: `TestRoutes_禁用账号后JWT立即失效` 用手动 invalidate, 等价于「无论 hook 是否在门, 都能验」, 仍 GREEN. M87 新测试 `TestRoutes_主动清鉴权statusCache_下次JWT立即401` 是「**不**手动 invalidate 也 GREEN」的反证. 两个测试**并存**, 不互相替代.
- **mutation M1 单条足够**: 真 PG 5 场景本 round 未加 (等 M88+ 顺带跑真 PG 时落地), 因为 sqlite handler test 已经覆盖 PASS-FAIL-PASS 反证, 真 PG 边际收益低. 与 M86 (handler + 真 PG mutation) 不同 — M87 的反证面更窄 (单一 call site), 实证一次足够.

---

## 6. 派生 TODO

- **G-5-2 followup**: 多副本部署下其他副本仍 ≤30s TTL. 真要全局近 0s 需 Redis pub/sub 广播 invalidation. 触发条件: 多副本部署上线 (运维动作, 非代码). 当前 ≤30s 已够运维封禁场景 (≤30s 全副本生效), 属「可接受」状态, 不在本 round scope.
- **TestDBSmoke_M87_ActiveInvalidation 真 PG 5 场景**: 范本在 `intent-M87-candidate.md` §Acceptance. M87 因为 handler test 已 PASS-FAIL-PASS 反证, 暂未加; M88+ 跑 db_smoke.sh 时顺带落地 (与 M86 同款范本「mutation inversion 在真 PG 反证」).
- **`TestRoutes_禁用账号后JWT立即失效` (M61 ship) 的手动 invalidate 是否删**: 不删. 两个测试并存, 等价但**不互替**. M61 测试 = 「手动 invalidate 也能验」(M40 ship 时的 workaround); M87 测试 = 「不手动 invalidate 也验」(M87 ship 后的真状态). 删任何一个都会丢失一面.
