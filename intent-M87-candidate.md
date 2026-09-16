# M87-candidate — G-5 已签发 JWT 不查库 主动失效收口 (active invalidation, OMH ulw-loop 第 17 cycle)

> **Loop cycle**: 17 of `itmanager-grit-2026q3`
> **Loop mode**: B (watchdog Mode B auto-dispatched M87-candidate after M86-candidate cycle 16 ship, 10-min commit-age gate 沿用 M79 D3)
> **Dispatch origin**: `~/.hermes/scripts/pm-loop-watchdog.sh` Mode B at 2026-09-16T??:??:??+08:00 (PM_QUEUE M87-candidate = `G-5 已签发 JWT 不查库`, derived from TODO.md L62 by `pm-loop-derive-candidates.py` M84 ship)
> **Scope**: `backend/internal/service/user_service.go` + `backend/internal/middleware/auth_status_cache.go` + tests + db_smoke + docs, ≤3h estimated (auto-derived)
> **Prerequisite**: M86-candidate (`3f3562d`, 2026-09-16) cycle 16 ship; M40 (`6172977`, 2026-09-13) JWT 路径 DB lookup + 30s cache 已 ship; M61 (`893e214`, 2026-09-15) `PATCH /users/:id/status` / `PATCH /users/:id/role` 账号处置写端点已 ship; `InvalidateAuthStatusCacheForUser` 函数已 ship 但仅用于测试 (M40 §edges 显式标「保留给将来 hooks」)

## Goal

PM_QUEUE M87-candidate = **`G-5 已签发 JWT 不查库`** (TODO.md L62). 这条登记有一个**反转史**:

- **2026-09-09 登记原状**: JWT 路径**不查库** (`middleware/auth.go` 原先只 `VerifyToken` 不查 DB), `status=inactive` 对已签发会话**最长 24h 才生效** (`auth.jwt.expire=86400`) — 「禁用账号」对会话失窃**不是即时止血**.
- **2026-09-13 M40 ship** (`M40-completion-report.md`, `6172977`): 方向反转 — JWT 路径**加上** `lookupUserStatus`, 30s TTL 进程内 cache, `TestDBSmoke_M40_JWTDisableTakesEffect` 5 场景真 PG 全过. **「≤30s 全副本生效」(cache per-process) 已记 trade-off**.
- **2026-09-13 M40 注释** (`auth_status_cache.go:88-93`): `InvalidateAuthStatusCacheForUser` 函数已 ship 但**仅用于测试** — 注释原文:「生产代码不应调用此函数；运维封禁场景的 cache 滞后 30s 是设计取舍」. 同时**保留了 hook 位**:「本函数保留给将来 hooks（如 user_service 在 Update 时同步调 invalidate）使用；当前 round 不引入这条链路以控制爆炸半径」.
- **2026-09-15 M61 ship** (`893e214`): 补上 `user_service.Update` / `UpdateStatus` / `UpdateRole` 与 `PUT /users/:id` / `PATCH /users/:id/status` / `PATCH /users/:id/role` 三个写端点 — **但 M61 没有把 active invalidation 接进 user_service.Update**, 与 M40 注释里承诺的「保留 hook 位」还有一步之差.

M87 = **把 M40 预留的 hook 接上 M61 的写端点**: `user_service.applyUserUpdate` 事务 commit 后调 `middleware.InvalidateAuthStatusCacheForUser(id)`, 把「≤30s 全副本生效」收口为「**该副本 ≈0s 生效**」(其他副本仍 ≤30s, 沿用 M40 trade-off).

**核心收紧面**:
- TODO.md L62:「已签发 JWT 不查库」 — **M40 ship 后该 framing 已 obsolete** (M40 反向加了 DB 查), TODO 条目本身从未标 done, 行号引用 (`middleware/auth.go:112-124 只用 claims`) 与 M40 ship 后实情不符 (现 `middleware/auth.go:130-140` 是 `lookupUserStatus` 调用).
- `middleware/auth_status_cache.go:88-93` 注释: 显式承诺 hook 给将来 round 用, M61 没接上, M87 接上.
- M61 (`M61-completion-report.md`): `user_service.Update/UpdateStatus/UpdateRole` 写后**没有 invalidation**, 用户被 `PATCH /users/:id/status` 禁掉后, 该副本最长 30s 才感知. 同进程同副本的近 0s 失效**未实现**.

**目标交付**:
1. **`user_service.applyUserUpdate` 在事务 commit 成功后**调 `middleware.InvalidateAuthStatusCacheForUser(id)` (`user_service.go:144-178` 是当前 `applyUserUpdate` 实现, 在 `err == nil` 的 happy path 加一行). Update / UpdateStatus / UpdateRole 三条路径共用 `applyUserUpdate`, 一处加全部覆盖. 「空 updates 不发 UPDATE」分支 (`user_service.go:122-124`) **不**调 invalidate — 没改东西就不必清 cache.
2. **`InvalidateAuthStatusCacheForUser` 文档翻新** (`auth_status_cache.go:88-93`): 从「生产代码不应调用此函数」改为「**生产路径 hook** (user_service.applyUserUpdate); 仍可被测试用」; 解释「多副本部署下, 主动失效只对处理 update 请求的副本生效, 其他副本仍走 30s TTL — 这与 M40 trade-off 一致」.
3. **真 PG db_smoke 新增 5 场景**: `TestDBSmoke_M87_ActiveInvalidation` — 验证 PATCH `/users/:id/status` 后该副本**立即**感知 inactive, 不等 30s. 场景表 (镜像 `TestDBSmoke_M40_JWTDisableTakesEffect` 但改走 service):
   - S1: 预热 cache (active) → PATCH inactive → 下一个 JWT 请求 401
   - S2: PATCH inactive (cache 空) → 下一个 JWT 请求 401 (cache miss → DB 读 → inactive)
   - S3: PATCH inactive → 等 50ms → 下一个 JWT 请求 401 (cache 已主动清, 不是等 TTL 到期)
   - S4: PATCH inactive 后 PATCH 翻回 active → 下一个 JWT 请求 200
   - S5: PATCH role (不改 status) → cache.get(status) **不**被清 (role 改不影响 status cache 内容, 仅失效该 user_id 条目, 证明 invalidate 是 user_id 粒度不是 status 字段粒度)
4. **服务级契约测试** (sqlite): `TestUserService_Update_成功后清鉴权statusCache` — 预 `defaultAuthStatusCache.set(id, "active")` → 调 `svc.UpdateStatus(ctx, id, "inactive", ...)` → 断言 `defaultAuthStatusCache.get(id) == false`. **同时**断言: `applyUserUpdate` 返回**前**cache 仍 active, 返回**后**cache 已清 (commit + invalidate 顺序钉死).
5. **白盒 mutation inversion M1**: 临时把 `middleware.InvalidateAuthStatusCacheForUser(id)` 从 `applyUserUpdate` 里剥掉 → 跑 `TestDBSmoke_M87_ActiveInvalidation` 期望**红** (S1/S3 期望 401 但 cache 仍 active → 200) → 还原 → 期望绿.
6. **Handler 层契约测试** (`user_handler_test.go`): `TestUpdateUserStatus_主动清鉴权statusCache` — admin JWT PATCH `/users/:id/status` → inactive → 同 user_id JWT (受害者) 调 `GET /me` 或类似**只读端点** (任意 authenticated GET) → 期望 401 (cache 已被 service 层 invalidate, 下一个 JWT 路径 lookupUserStatus 必读 DB, DB 已 inactive).
7. **TODO.md L62 标 `[x]` + 描述更新**: 标注「已 ship M40 + M87 — M40 加 30s TTL cache (≤30s 全副本生效), M87 接 active invalidation hook (该副本 ≈0s 生效) — 详见 `M87-candidate-completion-report.md`」. 行号引用同步更新到 `middleware/auth.go:130-140` (现 `lookupUserStatus` 位置).
8. **`auth_status_cache.go:120` 注释更新** (lookupUserStatus 函数注释):「30s TTL 是 fallback; 写路径 (M87 起) 由 `user_service.applyUserUpdate` 主动 invalidate」.
9. **`FIX-PLAN-AUTHZ-CLOSURE.md:201` 「不改 JWT「不查库」的架构」段**: 加注「2026-09-13 M40 + 2026-09-16 M87 联合 ship: JWT 路径查库 (30s TTL cache) + 写路径主动 invalidate, trade-off 落地, 此条结案」.
10. **`CHANGELOG.md` M87 段 + `M87-candidate-completion-report.md` + `M87-candidate-graph-analysis.md`**.

## Non-goals

- **不动** `lookupUserStatus` / `authStatusCache` / TTL = 30s 这些 M40 ship 的核心参数 — M87 不重写 cache 层, 只接 hook
- **不动** `handleAPIKeyAuth` (`middleware/auth.go:151-213`) — API Key 路径每次都查 DB (无 cache), 没有 lag 问题
- **不动** `middleware/auth_status_cache.go:62-71` `invalidate` 函数本身 — 只改外层函数 `InvalidateAuthStatusCacheForUser` 的文档
- **不动** `cmd/admin-bootstrap` / `cmd/migrate` / `cmd/seed` 等 CLI 命令
- **不动** `cmd/set-role` (M85-candidate 已 ship, 走 `user_service.Update` 同一路径 — 顺带**自然**获得 active invalidation, 不需要单独处理)
- **不动** `setup-profile.json` / `display.skin` / `interface` (M67 standing rule)
- **不动** `sing-box` / `keyring` / `OMH config` (Poison 红线)
- **不动** `go.mod` / `package.json` (本 round 不改依赖)
- **不动** `migrations/` (无 schema 变更, 与 M85 / M86 同款: 收紧在 Go 代码层)
- **不动** 路由前缀 / 端点名 / HTTP method
- **不动** PATCH `/users/:id/status` 既有契约 (含 403 守卫 / 审计 `audit_action="update_user_status"` / 同事务持锁读旧值)
- **不动** 多副本部署下**其他副本**的 ≤30s TTL lag — 这是 M40 trade-off 的本质, M87 只在该副本接 update 请求时缩短; 真要全局近 0s 需要 Redis pub/sub 广播 invalidation, 属独立架构决策 (登记 G-5-2 followup, 见 §Risks)
- **不动** `Role` 字段 cache — `InvalidateAuthStatusCacheForUser` 是 user_id 粒度清 cache, `role` 变化时同条目清掉即可; 鉴权路径仍用 JWT claims 里的 role, 角色变更对**已签发 JWT** 最长 24h 才生效是 JWT 本身的语义, 不在本 round scope (与 M40 ship 时一致, JWT claims 是签发时快照, 真要改必须重发 token — 属独立特性)
- **不动** `models.User.MustChangePassword` 字段 cache — 该字段**不**进 `lookupUserStatus` 缓存 (cache 只存 status), 改了不影响鉴权路径

## Assumptions

- ITmanager repo HEAD = `3f3562d` (M86-candidate cycle 16 ship — `GET /api/integrations/status` URL + Zabbix 用户名收口), working tree clean, branch `main` up-to-date with `origin/main`
- M40 ship (`6172977`, 2026-09-13) JWT 路径 `lookupUserStatus` + 30s cache + `InvalidateAuthStatusCacheForUser` (test-only) + `ResetAuthStatusCacheForTest` 已 ship
- M61 ship (`893e214`, 2026-09-15) `user_service.Update` / `UpdateStatus` / `UpdateRole` + `PUT /users/:id` / `PATCH /users/:id/status` / `PATCH /users/:id/role` 已 ship; 写路径走 `applyUserUpdate` (事务 + 持锁读旧值 + 守卫 + UPDATE + 回读)
- M61 ship 时 `user_service.applyUserUpdate` 末尾 (`user_service.go:170-178`) 是 `tx.First(&out, ...)` 回读后 commit, 然后 `out.Role = middleware.CanonicalRole(out.Role)` — **这是接 invalidation 的天然锚点** (commit 已成功, 改 DB 已落库)
- 既有 `TestDBSmoke_M40_JWTDisableTakesEffect` (`tests/db_smoke_test.go:3345-3411`) 是真 PG 上 5 场景模板, M87 新增 `TestDBSmoke_M87_ActiveInvalidation` 沿用同款 `smokeSetupMinConfig` / `smokeSetDB` / `smokeDoRequest` 辅助
- 既有 `smokeSetDB` (`db_smoke_test.go:3434-3442`) 每轮 `middleware.InvalidateAuthStatusCacheForUser("")` 清空 cache — M87 新测试无需额外 reset, 沿用
- 既有 `TestUserService_Update_持锁读旧值` (`user_service_test.go:421-448`) 是 sqlmock 模式, 沿用同款 mock + 期望; M87 新增 sqlite 路径测试 (验证 cache 副作用需真 cache, sqlmock 测不到)
- `InvalidateAuthStatusCacheForUser(userID)` 接受 `userID == ""` 表示清空全部 (`invalidate` 内部 `delete(map, "")`, 单测 `auth_status_cache_test.go:56` 已验证「未 set 的 key 不 panic」) — 既有 `smokeSetDB` 走 "" 清空, 沿用
- 临时 mutation 文件用 `m87_` 前缀 (与 M86 `m86bak` 同款, 不撞既有命名); 实证完 `mv main.go.m87bak main.go` 还原 + `rm -f` 删除 bak
- Go 1.25.14 (本地) 与 CI runner Go 1.25 一致, 测试基座稳定
- fact_store fact_id = 26 advisory (沿用 M82 cycle 13 = 22, M83 cycle 14 = 23, M85 cycle 15 = 24, M86 cycle 16 = 25, 本 round = 26, 未实际落库)
- watchdog tick 时段: M87-candidate 由 Mode B 自动 dispatch, commit-age ≥ 10 min 才起下一 round (M79 D3 沿用)
- `~/.hermes/state/PM_LAST_DISPATCH_RESULT.md` 写 M87 closeout (Poison 看 + watchdog 下次 tick 验证)

## Acceptance criteria

| 标准 | 实证 |
|---|---|
| `intent-M87-candidate.md` 8 节 omh-plan 骨架 (Goal / Non-goals / Assumptions / Acceptance / Verification / Risks / Plan / Decision gate) | 本文件 (commit 1) |
| `user_service.go` `applyUserUpdate` 末尾 `return &out, nil` **之前**加 `middleware.InvalidateAuthStatusCacheForUser(id)` 调用 | verify (grep) |
| `user_service.go` `applyUserUpdate` 注释更新: 「事务 commit 成功后立即清 JWT 路径 status cache (M87 active invalidation), 让该副本的下一请求不走 cache, 把 M40 30s TTL 收口到 ≈0s」 | verify |
| `user_service.go` `Update` 入口的「空 updates 不发 UPDATE」分支 (`user_service.go:122-124`) **不**调 invalidate — 没改就不必清 cache, 与「空 updates 等价于 Get」语义对齐 | verify |
| `middleware/auth_status_cache.go:88-93` `InvalidateAuthStatusCacheForUser` 文档翻新: 从「生产代码不应调用此函数」改为「生产路径 hook (M87 起 user_service.applyUserUpdate) + 仍可被测试用; 多副本部署下只对处理 update 请求的副本生效, 其他副本仍走 30s TTL (M40 trade-off 沿用)」 | verify |
| `middleware/auth_status_cache.go:120` `lookupUserStatus` 注释补「30s TTL 是 fallback; 写路径由 user_service.applyUserUpdate 主动 invalidate」 | verify |
| `user_service_test.go` 新增 `TestUserService_Update_成功后清鉴权statusCache`: sqlite, 预 `defaultAuthStatusCache.set(id, "active")` → 调 `svc.UpdateStatus(ctx, id, "inactive", ...)` → 断言 `defaultAuthStatusCache.get(id) == false` | verify |
| `user_service_test.go` 新增 `TestUserService_Update_空输入不调invalidate`: `UpdateUserInput{}` (零字段) → 走「空 updates 只回读」分支 → 断言 cache 没被动 (`set("never-touched", "inactive")` 仍能 get 到, 因为根本没调 invalidate) | verify |
| `user_handler_test.go` (或 `routes_integration_test.go`) 新增 `TestUpdateUserStatus_主动清鉴权statusCache_下次JWT请求立即401`: admin JWT PATCH `/users/:id/status` → inactive → 同 user_id JWT (受害者) 调任意 authenticated GET → 期望 401 | verify |
| `tests/db_smoke_test.go` 新增 `TestDBSmoke_M87_ActiveInvalidation` 真 PG 5 场景: S1 (预热 cache 后 PATCH inactive → 401) / S2 (空 cache PATCH inactive → 401) / S3 (PATCH inactive 后等 50ms → 401) / S4 (PATCH 翻回 active → 200) / S5 (PATCH role 不改 status → cache 条目被清, 下次 JWT 请求重读 DB 拿到当前 status) | verify (真 PG) |
| `scripts/db_smoke.sh` 白名单 +1: `TestDBSmoke_M87_ActiveInvalidation` | verify |
| **mutation inversion 实证 — active invalidation hook 真在门** | see Verification §3 |
| &nbsp;&nbsp; M1: 临时把 `applyUserUpdate` 里 `middleware.InvalidateAuthStatusCacheForUser(id)` 这一行注释掉 → 跑 `TestDBSmoke_M87_ActiveInvalidation` 期望**红** (S1/S3 cache 仍 active → 期望 401 实得 200) → 还原 → 绿 | verify |
| mutation 临时文件实证完**全部 rm** (不入 commit) | verify (git status) |
| 27 packages `go test -race -count=1 -timeout=180s ./...` 全绿 (含 M87 新增测试, 0 退化) | verify |
| 真 PG `db_smoke.sh` 全过 (M87 +1 场景, 既有 44 PASS / 0 FAIL 不退化) | verify |
| `TODO.md` L62 `- [ ]` → `- [x]`, 描述更新: 「已 ship M40 (30s TTL cache) + M87 (active invalidation hook) — 该副本 ≈0s 生效, 其他副本仍 ≤30s (M40 trade-off 沿用)」, 行号引用 `middleware/auth.go:130-140` | verify |
| `FIX-PLAN-AUTHZ-CLOSURE.md:201` 「不改 JWT「不查库」的架构」段加注: 「2026-09-13 M40 + 2026-09-16 M87 联合 ship, 此条结案」 | verify |
| `M87-candidate-completion-report.md` + `M87-candidate-graph-analysis.md` 写完 | docs commit 3 |
| `CHANGELOG.md` M87 段加 "G-5 active invalidation hook (该副本 ≈0s 生效)" 条目 | docs commit 3 |
| git log 3 commits, 全部 push 到 origin/main | verify |
| `~/.hermes/state/PM_QUEUE.json` M87-candidate.status: `candidate` → **`shipped`** | state fixup |
| `~/.hermes/state/PM_LAST_DISPATCH_RESULT.md` 写完 | verify |

## Verification

### 1. 收紧锚点定位 (grep verify)

```bash
$ grep -n 'InvalidateAuthStatusCacheForUser' backend/internal/service/user_service.go
180:	middleware.InvalidateAuthStatusCacheForUser(id) // M87: 主动清鉴权 status cache
```

只有一处调用, 在 `applyUserUpdate` 末尾. 既有测试 (24 个 user_service_test.go + 6 个 user_handler 测试) 不退化.

### 2. 既有契约测试 (Green: 既有测试 + 4 新测试)

**既有 (不破坏)**:
```bash
$ cd backend && go test -race -count=1 ./internal/service/ ./internal/middleware/
ok  	network-monitor-platform/internal/middleware  [... PASS, 0 FAIL]
ok  	network-monitor-platform/internal/service  	[24 PASS + 2 new PASS, 0 FAIL]
```

**新测试 (M87)**:
- `TestUserService_Update_成功后清鉴权statusCache` (sqlite, 钉住「commit 成功后 cache.get(id) == false」)
- `TestUserService_Update_空输入不调invalidate` (sqlite, 钉住「空 updates 走 Get 分支不副作用 cache」)
- `TestUpdateUserStatus_主动清鉴权statusCache_下次JWT请求立即401` (handler, 端到端验证: victim JWT 在 PATCH 后立即 401)
- `TestDBSmoke_M87_ActiveInvalidation` (真 PG, 5 场景)

**全 backend 不退化**:
```bash
$ cd backend && go test -race -count=1 -timeout=180s ./...
ok  	network-monitor-platform/cmd/admin-bootstrap  	~7s
ok  	network-monitor-platform/cmd/migrate           	~1s
ok  	network-monitor-platform/cmd/seed              	~30s
ok  	network-monitor-platform/cmd/set-role          	~1s (M85 测试不退化; set-role 走 user_service.Update 同一路径, 自动获 invalidation)
ok  	network-monitor-platform/internal/api          	~23s (含 1 M87 新测试)
ok  	network-monitor-platform/internal/api/handlers 	~13s
ok  	network-monitor-platform/internal/service      	~7s (含 2 M87 新测试)
ok  	network-monitor-platform/internal/middleware   	~5s
... (27 packages, all ok, 0 FAIL)
```

### 3. mutation inversion 实证

**M1 反证「active invalidation hook 真在门」**:

```bash
# 临时把 applyUserUpdate 末尾的 InvalidateAuthStatusCacheForUser 注释掉
$ sed -i.bak 's|^\tmiddleware\.InvalidateAuthStatusCacheForUser(id) // M87.*|\t_ = id // M87 MUTATION M1: 主动失效被剥|' backend/internal/service/user_service.go
# (或: 用 Python 脚本精准替换, 避免 sed 转义陷阱 — M85 用过同款)

$ DOCKER='sudo -n docker' bash scripts/db_smoke.sh -run TestDBSmoke_M87_ActiveInvalidation
=== RUN   TestDBSmoke_M87_ActiveInvalidation/S1_预热cache后PATCH_inactive_期望401
    Error: 期望 S1 victim JWT 401, 但响应 200 (cache 仍 active, 没被 invalidate)
--- FAIL: TestDBSmoke_M87_ActiveInvalidation/S1_预热cache后PATCH_inactive_期望401 (0.05s)
=== RUN   TestDBSmoke_M87_ActiveInvalidation/S3_PATCH后等50ms_期望401
    Error: 期望 S3 victim JWT 401, 但响应 200 (cache 没被 invalidate, 等 50ms 不够等 30s TTL)
--- FAIL: TestDBSmoke_M87_ActiveInvalidation/S3_PATCH后等50ms_期望401 (0.05s)
PASS  (S2/S4/S5 因不走 cache 或 role 不动 status, 不依赖 invalidate, 仍 PASS)
FAIL
```

**还原 (control)**:
```bash
$ mv backend/internal/service/user_service.go.m87bak backend/internal/service/user_service.go

$ DOCKER='sudo -n docker' bash scripts/db_smoke.sh -run TestDBSmoke_M87_ActiveInvalidation
ok  [5 PASS, 0 FAIL]
```

**关键设计要点**:
- **白盒 sqlite 测试 + 黑盒 handler 测试 + 真 PG db_smoke 三层** — 与 M86 (handler 测试 + grep 收紧 + openapi) + M85 (sqlmock SQL 契约 + 并发等价 + 真 PG) 范本同形不同层. 三层覆盖 = 「收紧在哪都钉死」.
- **S3 等 50ms 不等 30s**: 这是真 PG 上**唯一**能验证「主动失效不是靠 TTL」的场景. 若 hook 被剥, cache 不会因为 50ms 短窗而过期 → 必须靠 invalidate 才能在 50ms 内失效. S3 必红 = 主动失效被剥的真证据.
- **S5 验「invalidate 是 user_id 粒度」**: PATCH role (不改 status) → cache 条目被清 (因为 invalidate(id) 不分字段). 下次 lookupUserStatus 必读 DB, 拿到**当前** status. 这证明 invalidate 是 user_id 粒度, 不是 status 字段粒度 — 与 `InvalidateAuthStatusCacheForUser(id string)` 签名一致, 没意外副作用.
- **mutation M2 不写**: 改 `InvalidateAuthStatusCacheForUser` 函数体 (e.g. `defaultAuthStatusCache.invalidate` → 空函数) 是无意义 mutation — 该函数本身就是 cache 失效的**唯一定义点**, 失效它 = cache 不失效, 与 M1 逻辑等价. M2 与 M1 同形, 重复.
- **handler 测试不验 mutation 反证** (`TestUpdateUserStatus_主动清鉴权statusCache_下次JWT请求立即401`): mutation M1 已经通过真 PG db_smoke 反证到位, handler 测试只验「正常路径契约」. 重复反证 = 噪音, 沿用 M86 (mutation 只在最高真实度一处) + M85 (mutation 在 sqlmock + 并发测试两处同质反证, 但都是 sqlmock / 真 PG) 范本.

### 4. cmd/set-role 自动获 invalidation (顺带 coverage)

`cmd/set-role/main.go` 调 `user_service.UpdateRole` (M85 ship 时已 ship), 本 round 加 invalidation 后, **该命令路径自动获得 ≈0s 生效** (同进程同副本). 真 PG 测试 `TestCmd_SetRole_DisabledUserOldJWTTakesEffect` (M40 ship 后无此测试, M87 可顺带加, 但**不在本 round scope** — 留给后续 round). M85 ship 时 `TestRunWithDeps_*` 既有断言不退化 = cmd/set-role 路径覆盖到位.

### 5. TODO.md L62 切 [x]

```bash
$ git diff TODO.md | grep "G-5 已签发"
-- [ ] **G-5 已签发 JWT 不查库**（2026-09-09 新增）— `status=inactive` 对 API Key 立即生效（`middleware/auth.go:187`），但对**已签发会话 JWT 最长 24h 才生效**（`middleware/auth.go:112-124` 只用 claims；`auth.jwt.expire=86400`）。「禁用账号」对会话失窃不是即时止血。属独立架构决策（每请求查库 vs 短 TTL + 刷新）
++ [x] **G-5 已签发 JWT 不查库 → 已 ship M40 + M87 联合**（2026-09-09 登记，2026-09-16 M87 结案）— 原状：JWT 路径不查库，已签发会话最长 24h 才失效。**M40 (2026-09-13, `6172977`) ship**：JWT 路径加 `lookupUserStatus` + 30s TTL cache (≤30s 全副本生效, cache per-process). **M87 (2026-09-16) ship**：`user_service.applyUserUpdate` commit 后调 `middleware.InvalidateAuthStatusCacheForUser(id)` → 该副本 ≈0s 生效；其他副本仍 ≤30s (M40 trade-off 沿用). 行号引用 `middleware/auth.go:130-140` (现 lookupUserStatus 位置). 真要全局近 0s 需 Redis pub/sub 广播 invalidation, 属独立架构决策 (登记 G-5-2 followup).
```

### 6. CHANGELOG M87 段

```markdown
- **M87-candidate G-5 已签发 JWT 不查库 → 主动失效收口** (backend service + middleware + tests + db_smoke, ≤3h)
  — TODO.md L62 "已签发 JWT 不查库" 的收口. 路径: `user_service.applyUserUpdate` 事务 commit 成功后调 `middleware.InvalidateAuthStatusCacheForUser(id)`,
  把 M40 ship 的 ≤30s 全副本生效 trade-off 收口为「该副本 ≈0s 生效」(处理 update 请求的副本; 其他副本仍 ≤30s TTL, M40 trade-off 沿用).
  新增 4 测试: `TestUserService_Update_成功后清鉴权statusCache` (sqlite, 钉住 cache 副作用契约) +
  `TestUserService_Update_空输入不调invalidate` (sqlite, 钉住「空 updates 走 Get 分支不副作用 cache」) +
  `TestUpdateUserStatus_主动清鉴权statusCache_下次JWT请求立即401` (handler, 端到端验证) +
  `TestDBSmoke_M87_ActiveInvalidation` (真 PG, 5 场景: S1 预热 / S2 空 cache / S3 50ms 短窗 / S4 翻回 active / S5 role 不动 status).
  mutation inversion M1 反证: 临时把 `InvalidateAuthStatusCacheForUser(id)` 注释掉 → S1/S3 真 PG 期望 401 实得 200 (cache 仍 active) → 还原 → 全绿.
  文档翻新: `InvalidateAuthStatusCacheForUser` 从「生产不应调用」改为「生产路径 hook (M87 起) + 测试仍可用」; `lookupUserStatus` 注释补「30s TTL 是 fallback, 写路径主动 invalidate」.
  见 `M87-candidate-completion-report.md`.
```

## Risks

- **多副本部署下其他副本仍 ≤30s lag**: M87 只对**处理 update 请求的副本**生效, 其他副本必须等 30s TTL 自然过期. 这是 M40 trade-off 的本质 — 真要全局近 0s 需要 Redis pub/sub 广播 invalidation (M97+ 候选, 登记 G-5-2 followup, **不**在本 round scope). 测试用例 S1 验证**该副本**行为, 不跨副本 — 多副本部署是运维配置, 不在本仓单元测试覆盖范围.
- **`InvalidateAuthStatusCacheForUser` 函数 export 但原本仅测试用**: M40 注释显式承诺「保留 hook 位」, M87 是兑现承诺 — 但**这意味着 service 层与 middleware 层耦合加了一根针**. 既有 service 层已经 import middleware (`user_service.go:8`), 不引入新 import. **缓解**: 注释明确「service 层只调这一个函数, 不读 cache 内容」, 依赖单向; 真要解耦可后续注入 `StatusCacheInvalidator` 接口 (M98+ 候选).
- **`applyUserUpdate` 失败路径不调 invalidate**: 事务回滚 (守卫返回 ErrForbidden / DB 错误 / UPDATE 0 rows) 不调 invalidate. 这是**正确**的 — DB 没改, cache 不必清. 但要警惕: 「DB 写入成功但 invalidate 失败」的情况 — `defaultAuthStatusCache.invalidate` 是纯 map 操作, 不会失败 (无 IO, 无 panic). 真要加防御可 `defer recover()`, 但属于 over-engineering.
- **`user_service_test.go` 既有 sqlmock 测试不被新 cache 副作用污染**: 既有 `TestUserService_Update_持锁读旧值` 用 sqlmock, 不依赖真 cache. `applyUserUpdate` 加 invalidate 调用是 map 操作 (默认空 cache 也安全), 既有测试不退化. **验证**: 跑 `go test -race -count=1 ./internal/service/...` 全绿.
- **mutation M1 sed 模板脆弱**: sed 依赖精确行内容匹配, 与 M86 同款风险. **缓解**: 用 Python 脚本做精准 replace (M85 用过同款), 或 `cp` 备份后整文件 revert.
- **handler 测试 `TestUpdateUserStatus_*` 的受害者 victim JWT 复用 setup**: 既有测试 router 复用同一 JWT 时 cache 已写, M87 新测试必须**先**让 victim JWT 走过一次 cache miss (让 DB 读并 cache active), 然后 PATCH inactive, 再 victim JWT 第二次请求 → 401. 顺序钉死在测试 setup 里 (t.Cleanup resetAuthStatusCache). **缓解**: 测试名前置 `cacheWarmup` helper 显式把 victim 走一次 warmup.
- **真 PG S5「role 不动 status」场景的微妙性**: PATCH role 走 `applyUserUpdate`, invalidate(id) 会清 cache. 下次 lookupUserStatus 必读 DB 拿 status. 这是**正确**行为 (invalidate 是 user_id 粒度不是 status 字段粒度). 但若 PATCH role 后续用户被禁 (跨请求), S5 不能复现那种场景 — S5 只测「同请求里 role 改后 cache 被清」. **缓解**: S5 命名明确「role 不动 status → cache 清, 下次读 DB 拿到当前 status」, 不混淆.
- **`Role` 字段在 JWT claims 里签发时快照**: 即使 M87 让 status cache 主动失效, **role 字段**仍受 JWT 自身语义约束 (claims 是签发时快照, 改 role 必须重发 token). 这是已知 trade-off, JWT 模式本质. 本 round 不动 claims, 不动 token 续签 / 强制重发逻辑 (属独立特性).
- **PM_QUEUE state 同步漏**: 沿用 M82 + M83 + M85 + M86 closeout 模式, 必须把 status 切 `shipped` + append `shipped[]` registry, 否则 watchdog 会反复 dispatch M87.
- **既有 mutation 文件清理**: 实证完 `rm -f user_service.go.m87bak`, 不留到下一 round. `git status --short` 二次确认.
- **`FIX-PLAN-AUTHZ-CLOSURE.md:201` 改注**: 该 doc 是 2026-09-09 AUTHZ-CLOSURE 收口的「不改 JWT 不查库架构」段, M40 ship 后就该改但没改 (M40 §docs 没刷这条), M87 联合结案时一并改. **关键**: 不删原文, 加「2026-09-13 M40 + 2026-09-16 M87 联合 ship, 此条结案」注, 保留历史.

## Plan

1. **写 `intent-M87-candidate.md`** (本文件, 8 节 omh-plan 骨架) — **feat commit 1**: `feat(M87-candidate): intent spec (omh-plan 8 节骨架, G-5 active invalidation 主动失效收口)`

2. **impl 服务层** (`backend/internal/service/user_service.go:170-178` `applyUserUpdate` 末尾):
   - 在 `return &out, nil` **之前**加 `middleware.InvalidateAuthStatusCacheForUser(id)` 调用
   - 注释: 「事务 commit 成功后立即清 JWT 路径 status cache (M87 active invalidation), 让该副本的下一请求不走 cache, 把 M40 30s TTL 收口到 ≈0s」
   - 不在「空 updates 不发 UPDATE」分支 (`user_service.go:122-124`) 加调用 — 没改就不必清 cache

3. **impl middleware 注释翻新**:
   - `middleware/auth_status_cache.go:88-93` `InvalidateAuthStatusCacheForUser` 文档: 从「生产不应调用」改为「生产路径 hook (M87 起 user_service.applyUserUpdate); 多副本部署下只对处理 update 请求的副本生效, 其他副本仍 30s TTL (M40 trade-off). 仍可被测试用」
   - `middleware/auth_status_cache.go:120` `lookupUserStatus` 注释补「30s TTL 是 fallback; 写路径由 user_service.applyUserUpdate 主动 invalidate」

4. **impl 服务级测试** (`backend/internal/service/user_service_test.go`):
   - 新增 `TestUserService_Update_成功后清鉴权statusCache`: sqlite, `defaultAuthStatusCache.set(id, "active")` → `svc.UpdateStatus(ctx, id, "inactive", ...)` → 断言 `defaultAuthStatusCache.get(id) == false`
   - 新增 `TestUserService_Update_空输入不调invalidate`: sqlite, 预 `defaultAuthStatusCache.set(id, "inactive")` → `svc.Update(ctx, id, UpdateUserInput{}, ...)` → 断言 cache 仍 `inactive` (没被动)

5. **impl handler 测试** (`backend/internal/api/handlers/user_handler_test.go` 或 `routes_integration_test.go`):
   - 新增 `TestUpdateUserStatus_主动清鉴权statusCache_下次JWT请求立即401`: admin JWT PATCH `/users/:id/status` → inactive → 同 user_id JWT (受害者) 调任意 authenticated GET → 期望 401

6. **impl db_smoke 真 PG** (`backend/tests/db_smoke_test.go`):
   - 新增 `TestDBSmoke_M87_ActiveInvalidation`: 5 场景 (S1 预热 / S2 空 cache / S3 50ms 短窗 / S4 翻回 active / S5 role 不动 status)
   - `scripts/db_smoke.sh` 白名单 +1

7. **mutation inversion 实证**:
   - **M1**: Python 脚本临时把 `applyUserUpdate` 末尾 `InvalidateAuthStatusCacheForUser(id)` 这一行注释掉 → 跑 `TestDBSmoke_M87_ActiveInvalidation` 期望**红** (S1/S3 cache 仍 active → 期望 401 实得 200) → `mv user_service.go.m87bak user_service.go` 还原
   - mutation bak 文件**rm**, `git status --short` 仅 commit 2 (impl + tests + comments) 才算闭环

8. **impl docs 更新** (commit 2 内含, 与 impl 同 commit):
   - `docs/FIX-PLAN-AUTHZ-CLOSURE.md:201` 「不改 JWT「不查库」的架构」段加注: 「2026-09-13 M40 + 2026-09-16 M87 联合 ship, 此条结案」

9. **写 docs**:
   - `M87-candidate-completion-report.md` (主动失效实证 + mutation inversion 反证 + commit 序列 + 派生 TODO + G-5-2 followup)
   - `M87-candidate-graph-analysis.md` (service ↔ middleware cache 节点图 + M40 ↔ M87 节点图 + 5 场景 db_smoke 节点图 + M86 ↔ M87 范本对比)
   - `TODO.md` L62 `- [ ]` → `- [x]` + 描述更新 + 行号引用 `middleware/auth.go:130-140`
   - `CHANGELOG.md` M87 段加条目 (放在 M86 之后, cycle 17)
   - — **docs commit 3**: `docs(M87-candidate): completion + graph analysis + CHANGELOG + TODO (G-5 active invalidation)`

10. **commit + push** 3 commits 到 origin/main

11. **PM_QUEUE state fixup**: M87-candidate.status `candidate` → `shipped`, append `shipped[]` registry, bump `last_updated` / `last_audit`

12. **写 `~/.hermes/state/PM_LAST_DISPATCH_RESULT.md`** (Poison 看 + watchdog 下次 tick 验证)

### Commit 序列

```
3f3562d (HEAD, M86-candidate cycle 16)
   ↓
M87 commit 1: feat(M87-candidate): intent spec (omh-plan 8 节骨架, G-5 active invalidation 主动失效收口)
M87 commit 2: feat(M87-candidate): active invalidation hook 收口 (user_service.applyUserUpdate → InvalidateAuthStatusCacheForUser + 4 测试 + mutation inversion M1)
M87 commit 3: docs(M87-candidate): completion + graph analysis + CHANGELOG + TODO (G-5 active invalidation 联合 M40 结案)
```

3 commits (沿用 M78/M79/M80/M81/M82/M83/M85/M86 既有 pattern; intent spec 1 + impl+in-source-docs 1 + docs 1). mutation inversion 在 commit 2 之前完成, 不入 commit.

## Decision gate

- **D1**: scope = **`user_service.applyUserUpdate` commit 后调 `InvalidateAuthStatusCacheForUser(id)`** + 4 测试 (sqlite 服务级 / handler 端到端 / 真 PG db_smoke 5 场景) + 文档翻新 (InvalidateAuthStatusCacheForUser + lookupUserStatus + FIX-PLAN-AUTHZ-CLOSURE) + TODO.md L62 切 [x] + CHANGELOG + completion report + graph analysis
- **D2**: 沿用 poison-stop-gates-v1 (PM_LOOP_MODE = "stop" → freeze)
- **D3**: 沿用 watchdog 自旋防 + commit age ≥ 10 min
- **D4**: 沿用 30-min dispatch hist flapping auto-switch (M79 D4)
- **D5**: mutation inversion = **服务层 → cache 失效联动** 模式 (范本 E, NEW) — 与 M82 范本 A (业务代码 mutation) + M83 范本 B (CI 守门 mutation) + M85 范本 C (业务并发窗口 mutation) + M86 范本 D (响应字段守卫 mutation) 同形不同物; 都验证「守卫真工作」, 守卫对象 = `applyUserUpdate` commit 后的 cache invalidate 调用
- **D6**: 不写新 fact_store entry (沿用 M82 = 22, M83 = 23, M85 = 24, M86 = 25, 本 round = 26 advisory)
- **D7**: 3 commits (intent + impl+in-source-docs + docs, 沿用 M78/M79/M80/M81/M82/M83/M85/M86 pattern)
- **D8**: PM_LAST_DISPATCH_RESULT.md 写到 `~/.hermes/state/`, watchdog 下次 tick 验证
- **D9**: mutation 临时文件**不入 commit** (用 `user_service.go.m87bak` 隔离, 实证完 mv 还原 + `git status --short` 二次确认 + `rm -f` 删除 bak)
- **D10**: PM_QUEUE M87-candidate.status: `candidate` → **`shipped`**, append `shipped[]` registry (沿用 M82 + M83 + M85 + M86 closeout 范本)
- **D11**: 不动多副本部署下其他副本 ≤30s TTL (M40 trade-off 本质, 真要全局近 0s 需 Redis pub/sub, 登记 G-5-2 followup, **不**在本 round scope)
- **D12**: 不动 `Role` JWT claims 签发时快照语义 (改 role 必须重发 token, 属独立特性)
- **D13**: 不动 JWT 路径 `lookupUserStatus` / `authStatusCache` / TTL = 30s 核心参数 (M87 只接 hook, 不重写 cache 层)
- **D14**: 不动 API Key 路径 `handleAPIKeyAuth` (每次都查 DB 无 cache, 无 lag 问题)
- **D15**: 不动 `cmd/set-role` (M85 已 ship, 走 user_service.Update 同一路径, 自动获 invalidation)
- **D16**: 不动 setup-profile.json / display.skin / interface (M67 standing rule 沿用)
- **D17**: 不动 sing-box / keyring / OMH config (Poison 红线)
- **D18**: 不动 migrations/ (无 schema 变更, 收紧在 Go 代码层)
- **D19**: 不动既有 PATCH `/users/:id/status` 契约 (含 403 守卫 / 审计 / 持锁读旧值)
- **D20**: mutation M1 反证必须**S1+S3 红 → 还原全绿**, 否则不算闭环 (S2/S4/S5 不依赖 invalidate, 仍 PASS 是预期)
- **D21**: 「空 updates 不发 UPDATE」分支**不**调 invalidate — 没改就不必清 cache, 与「空 updates 等价于 Get」语义对齐 (D21 强约束, 与 S5 同「invalidate 是 user_id 粒度不是 status 字段粒度」是两种不同边界)
- **D22**: `FIX-PLAN-AUTHZ-CLOSURE.md:201` 加注**不删原文**, 加注保留历史 (与既有 ADR 类文档口径一致)
- **D23**: TODO.md L62 描述更新保留**所有历史信息** (M40 ship + M87 ship + G-5-2 followup), 不只标 [x] 后就删原文
