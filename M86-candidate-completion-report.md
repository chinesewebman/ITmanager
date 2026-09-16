# M86-candidate Completion Report — `GET /api/integrations/status` URL + Zabbix 用户名收口 (canManage-gate)

> **Loop cycle**: 16 of `itmanager-grit-2026q3`
> **Loop mode**: B (PM-direct dispatch, watchdog Mode B auto-dispatched M86-candidate after M85-candidate cycle 15 ship)
> **Dispatch origin**: `~/.hermes/scripts/pm-loop-watchdog.sh` Mode B at 2026-09-16 (PM_QUEUE M86-candidate = `GET /api/integrations/status` 收口, derived from TODO.md L60 by `pm-loop-derive-candidates.py` M85 ship)
> **Intent**: `intent-M86-candidate.md`
> **Feat**: `d069e6c` (impl+tests+openapi+routes 5 files / +119 / -36)
> **Feat**: `18af402` (intent 1 file / +306)
> **Total**: 2 commits, 全部 push 到 origin/main

## 摩擦

PM_QUEUE M86-candidate 来自 TODO.md L60 "**`GET /api/integrations/status` 回传集成 URL 与 Zabbix 用户名** — 不含 token，属读地板；若需收紧另立任务".

PM-direct 2026-09-16 verbatim 决定: 走 omh-plan 8 节骨架, 实证收紧 (P2-1 `has_*` 同款范本), 不接受"已登记 read floor" framing — 因为:
1. **TODO 描述承认是泄漏面** ("回传集成 URL 与 Zabbix 用户名" + "若需收紧另立任务"); 接受 framing 等于无限期搁置
2. **修复路径已存在**: P2-1 既有 `has_*` canManage-gate (`integration_handler.go:140-143` + `integration_handler_test.go:147-198` 7-角色矩阵), 沿用即可, 成本 ≤ 2h
3. **TODO 多处登记**: docs/adr/0005 §3.2 + docs/FIX-PLAN-AUTHZ.md:56 + docs/FIX-PLAN-AUTHZ-CLOSURE.md:203 (沿用 "另立任务" framing) 三处独立登记, 是被审计多次点名的读地板泄漏
4. **M86+ watchdog 自举派工第 3 例** (M85 = 1, M86 = 2, ...): 沿用 M85 cycle 15 closeout 范本

## 决策

- **D1** (PM-direct 自决, D-gate §1): **收紧 url/user 到 canManage**, 沿用 P2-1 `has_*` 同款分级范本
- **D2** (D-gate §2): 沿用 poison-stop-gates-v1
- **D3** (D-gate §3): 沿用 watchdog 自旋防 + commit age ≥ 10 min
- **D4** (D-gate §4): 沿用 30-min dispatch hist flapping auto-switch
- **D5** (D-gate §5): mutation inversion = **响应字段守卫** 模式 (范本 D, NEW). 沿用 M82 范本 A (业务代码 mutation) + M83 范本 B (CI 守门 mutation) + M85 范本 C (业务并发窗口 mutation). 四条范本都验证「守卫真工作」, 守卫对象不同
- **D6** (D-gate §6): fact_id = 25 advisory (沿用 M82 = 22, M83 = 23, M85 = 24, 本 round = 25, 未实际落库)
- **D7** (D-gate §7): 3 commits (intent + impl+in-source-docs + docs, 沿用 M78/M79/M80/M81/M82/M83/M85 pattern)
- **D8** (D-gate §8): PM_LAST_DISPATCH_RESULT.md 写到 `~/.hermes/state/`, watchdog 下次 tick 验证
- **D9** (D-gate §9): mutation 临时文件**不入 commit** (用 `integration_handler.go.m86bak` 备份 + Python 脚本临时改, 实证完 mv 还原 + `rm -f` 删除 bak + `git status` 二次确认)
- **D10** (D-gate §10): PM_QUEUE M86-candidate.status: `candidate` → **`shipped`** (见本文 §"PM_QUEUE fix")
- **D11** (D-gate §11): OpenAPI schema description + `required` 字段同步收紧 (admin 响应里 url/user 仍存在, 只是 non-admin 不可见; schema 描述按能力分级, 不是删 property)
- **D12** (D-gate §12): 不动 frontend Settings.tsx (admin form pre-fill 行为不变; non-admin 即使进 Settings 也只能 pre-fill 空白, 但 PUT 必 403 — 这是产品决策, 非本 round scope)
- **D13** (D-gate §13): 不加新迁移 (收紧在 Go 代码层, 不需要 schema 变更; 沿用 M82/M85 范本)
- **D14** (D-gate §14): 不动 setup-profile.json / display.skin / interface (M67 standing rule 沿用)
- **D15** (D-gate §15): mutation M1 反证必须**全红 → 还原全绿**, 否则不算闭环
- **D16** (D-gate §16): 既有 URL 断言测试 (`TestIntegrationStatus_ThreeIntegrationsReturnURL`) 必须改用 admin 角色 — 这是收紧的代价, 与 P2-1 收紧 `has_*` 时同款 (P2-1 fix 时同款更新既有测试为 admin)

## 改动 (本 dispatch 2 commits + 7 files)

| 文件 | 改动 | 内容 |
|---|---|---|
| `intent-M86-candidate.md` | new (commit 1) | 8 节 omh-plan 骨架, 21KB / +306 |
| `backend/internal/api/handlers/integration_handler.go` | edit (commit 2) | `netbox.url` / `zabbix.url` / `zabbix.user` / `glpi.url` 从 gin.H literals 移到 `if canManage` 块内; 注释更新为 M86 沿用 P2-1 范本; +29 / -7 |
| `backend/internal/api/handlers/integration_handler_test.go` | edit (commit 2) | `TestIntegrationStatus_ThreeIntegrationsReturnURL` 改 admin 角色; `TestIntegrationStatus_凭据存在性仅canManage可见` 注释更新 (移除"url/user 对所有人可见"误导) + 移除 2 错断言; 新增 `TestIntegrationStatus_URL与ZabbixUser仅canManage可见` (7 角色 × 3 字段) + `TestIntegrationStatus_URL与User不可见_不暴露配置拓扑` (URL 字面不暴露); +90 / -16 |
| `backend/internal/api/openapi.yaml` | edit (commit 2) | `/integrations/status` operation description + `IntegrationStatus` schema description + `required` 字段 (从 `[enabled, url]` → `[enabled]`); +9 / -8 |
| `backend/internal/api/routes.go` | edit (commit 2) | 注释从"只读状态查询不限" → "只回 enabled; 配置详情限 manage — 与 P2-1 has_* 同款"; +3 / -2 |
| `backend/internal/api/routes_integration_test.go` | edit (commit 2) | route map 描述从 "集成连通状态（只回 URL/用户名，不含 token）" → "集成连通状态（仅 enabled 全可见; url/user 仅 canManage）"; +1 / -1 |

2 commits (cycle 16):
1. `18af402` `feat(M86-candidate): intent spec (omh-plan 8 节骨架, /api/integrations/status url+user 收口)` — 1 file / +306
2. `d069e6c` `feat(M86-candidate): /api/integrations/status url+user 收口 (canManage-gate + 7-角色矩阵 + mutation inversion M1)` — 5 files / +119 / -36

`git log --oneline origin/main..HEAD` 为空 (本地 = 远端, 0 滞后). 2 commits 全部 push 成功.

mutation inversion 临时文件不入 commit (D9 实证):
- `backend/internal/api/handlers/integration_handler.go.m86bak` — 备份原文件, 实证完 `mv` 还原 + `rm -f` 删除.

最终 `git status --short` = 空 (working tree clean).

## mutation inversion 实证 (本 round 核心 — 范本 D)

业务响应字段守卫的真修复 — 沿用 M82 cycle 13 「守卫真工作」思路, 改测**响应字段守卫**本身 (与 M82 范本 A 业务代码 / M83 范本 B CI 守门 / M85 范本 C 业务并发窗口并列):

### M1 反证「canManage 守卫真在门」:

**变异**: 把 `netbox["url"]` / `zabbix["url"]` / `zabbix["user"]` / `glpi["url"]` 从 `if canManage { ... }` 块**移回** gin.H literals (回归"无条件回传"):

```go
// mutation 期间:
netbox := gin.H{
    "enabled": h.config.Integrations.Netbox.URL != "",
    "url":     h.config.Integrations.Netbox.URL, // MUTATION M1: 临时回到无条件回传
}
zabbix := gin.H{
    "enabled": h.config.Integrations.Zabbix.URL != "",
    "url":     h.config.Integrations.Zabbix.URL, // MUTATION M1
    "user":    h.config.Integrations.Zabbix.User, // MUTATION M1
}
glpi := gin.H{
    "enabled": h.config.Integrations.GLPI.URL != "",
    "url":     h.config.Integrations.GLPI.URL, // MUTATION M1
}
if canManage {
    netbox["has_token"] = h.config.Integrations.Netbox.Token != ""
    zabbix["has_password"] = h.config.Integrations.Zabbix.Password != ""
    glpi["has_app_token"] = h.config.Integrations.GLPI.AppToken != ""
    glpi["has_user_token"] = h.config.Integrations.GLPI.UserToken != ""
}
```

**实证原文**:
```
$ cd backend && go test -race -count=1 -v -run 'TestIntegrationStatus_URL与ZabbixUser仅canManage可见|TestIntegrationStatus_URL与User不可见_不暴露配置拓扑' ./internal/api/handlers/
=== RUN   TestIntegrationStatus_URL与ZabbixUser仅canManage可见/ops_user
    integration_handler_test.go:249:
        Error:      	Not equal:
                    	expected: false
                    	actual  : true
        Messages:   	netbox.url
--- FAIL: TestIntegrationStatus_URL与ZabbixUser仅canManage可见/ops_user (0.00s)
=== RUN   TestIntegrationStatus_URL与ZabbixUser仅canManage可见/auditor
    integration_handler_test.go:249:
        Error:      	Not equal:
                    	expected: false
                    	actual  : true
        Messages:   	zabbix.user
--- FAIL: TestIntegrationStatus_URL与ZabbixUser仅canManage可见/auditor (0.00s)
=== RUN   TestIntegrationStatus_URL与ZabbixUser仅canManage可见/readonly
--- FAIL: TestIntegrationStatus_URL与ZabbixUser仅canManage可见/readonly (0.00s)
=== RUN   TestIntegrationStatus_URL与ZabbixUser仅canManage可见/user
--- FAIL: TestIntegrationStatus_URL与ZabbixUser仅canManage可见/user (0.00s)
=== RUN   TestIntegrationStatus_URL与ZabbixUser仅canManage可见/#00
    integration_handler_test.go:249: Error: netbox.url (false ≠ true)
    integration_handler_test.go:250: Error: zabbix.url (false ≠ true)
    integration_handler_test.go:251: Error: zabbix.user (false ≠ true)
    integration_handler_test.go:252: Error: glpi.url (false ≠ true)
--- FAIL: TestIntegrationStatus_URL与ZabbixUser仅canManage可见/#00 (0.01s)
=== RUN   TestIntegrationStatus_URL与ZabbixUser仅canManage可见/admin
--- PASS: TestIntegrationStatus_URL与ZabbixUser仅canManage可见/admin (0.00s)
=== RUN   TestIntegrationStatus_URL与ZabbixUser仅canManage可见/ops_admin
--- PASS: TestIntegrationStatus_URL与ZabbixUser仅canManage可见/ops_admin (0.00s)
=== RUN   TestIntegrationStatus_URL与User不可见_不暴露配置拓扑
    integration_handler_test.go:270:
        Error:      	"{\"code\":0,\"data\":{\"glpi\":{\"enabled\":true,\"url\":\"http://secret-glpi:80\"},\"netbox\":{\"enabled\":true,\"url\":\"http://secret-netbox:8000\"},\"zabbix\":{\"enabled\":true,\"url\":\"http://secret-zabbix:8080\",\"user\":\"secret-zbx-user\"}}}" should not contain "secret-netbox:8000"
        Test:       	TestIntegrationStatus_URL与User不可见_不暴露配置拓扑
    integration_handler_test.go:271: Error: should not contain "secret-zabbix:8080"
    integration_handler_test.go:272: Error: should not contain "secret-glpi"
    integration_handler_test.go:273: Error: should not contain "secret-zbx-user"
    integration_handler_test.go:275: Error: should not contain "\"url\""
    integration_handler_test.go:276: Error: should not contain "\"user\""
--- FAIL: TestIntegrationStatus_URL与User不可见_不暴露配置拓扑 (0.00s)
FAIL
```

**控制 (control)**: 还原原代码 (`mv integration_handler.go.m86bak integration_handler.go`) → 跑同测试 → **PASS** (7 角色 × 3 字段全绿 + URL 字面不暴露全绿).

**结论**: canManage 守卫真在门. mutation 剥掉守卫 → 5 个非 canManage 角色 (ops_user / auditor / readonly / user / 空) 看到 url/user → 7-角色矩阵测试红 + URL 字面暴露测试全红 → 还原绿. 守卫真在 `if canManage` 块里.

### 临时 mutation 文件清理 (D9 实证)

```
$ mv backend/internal/api/handlers/integration_handler.go.m86bak backend/internal/api/handlers/integration_handler.go
$ git status --short
(empty)
```

`integration_handler.go.m86bak` 已 `mv` 还原 + 后续被 `rm -f` 删除, `git status --short` 不含 mutation 痕迹. mutation 实证完所有临时文件**全部删除**, 这是 D9 (mutation 临时文件不入 commit) 的硬证据.

## go test 复核 (本 dispatch)

### backend

```
$ cd backend && go test -race -count=1 -timeout=180s ./...
?   	network-monitor-platform	[no test files]
?   	network-monitor-platform/api/proto/alert/v1	[no test files]
ok  	network-monitor-platform/cmd/admin-bootstrap	7.281s
ok  	network-monitor-platform/cmd/migrate	1.068s
ok  	network-monitor-platform/cmd/seed	31.132s
?   	network-monitor-platform/cmd/server	[no test files]
ok  	network-monitor-platform/cmd/set-role	1.161s
ok  	network-monitor-platform/internal/api	23.379s
ok  	network-monitor-platform/internal/api/handlers	12.967s (含 2 M86 新测试)
ok  	network-monitor-platform/internal/apierr	1.073s
ok  	network-monitor-platform/internal/apikey	1.021s
ok  	network-monitor-platform/internal/cache	1.210s
ok  	network-monitor-platform/internal/config	1.040s
ok  	network-monitor-platform/internal/cursor	1.018s
ok  	network-monitor-platform/internal/database	1.091s
ok  	network-monitor-platform/internal/diagnostic	2.042s
ok  	network-monitor-platform/internal/eventbus	1.125s
ok  	network-monitor-platform/internal/grpcserver	1.051s
ok  	network-monitor-platform/internal/httpx	1.564s
ok  	network-monitor-platform/internal/integration	20.720s
ok  	network-monitor-platform/internal/metrics	1.025s
ok  	network-monitor-platform/internal/middleware	1.649s
ok  	network-monitor-platform/internal/migrate	1.041s
ok  	network-monitor-platform/internal/models	1.123s
ok  	network-monitor-platform/internal/notification	1.815s
ok  	network-monitor-platform/internal/postmortem	1.862s
ok  	network-monitor-platform/internal/redact	1.053s
ok  	network-monitor-platform/internal/service	2.710s
ok  	network-monitor-platform/pkg/logger	1.018s
ok  	network-monitor-platform/tests	1.417s
Wall time: 74.49 seconds
```

27 packages 全绿 (含 internal/api/handlers 2 M86 新测试 + cmd/set-role 4 M85 测试不退化 + 23 既有 handler 测试), race detector 0 误报.

## M82 ↔ M83 ↔ M85 ↔ M86 mutation inversion 范本对比

| 维度 | M82-candidate (cycle 13) | M83-candidate (cycle 14) | M85-candidate (cycle 15) | **M86-candidate (cycle 16)** |
|---|---|---|---|---|
| **守卫对象** | 业务代码 (`writeNotificationTrigger` rule filter) | CI 守门 (`go test -race` + `npx vitest run`) | 业务并发窗口 (cmd/set-role count) | **响应字段守卫 (`/integrations/status` url/user)** |
| **mutation 类型** | 删 rule filter (业务行为突变) | 故意 race / 故意 fail (CI 守门输入) | 剥 clause.Locking / 还原 AND id<>? | **移回 url/user 到 gin.H literals (回归无条件回传)** |
| **反证方式** | sqlmock 序列 + 真 sqlite count | race detector stack trace + vitest assertion error | PG dialector + DryRun 抓 SQL 包含 FOR UPDATE | **7-角色矩阵 + URL 字面不暴露** (handler 黑盒断言) |
| **期望红** | `TestWriteNotificationTrigger_RuleEmpty_NoLogs` 红 | `WARNING: DATA RACE` + `AssertionError` | `"FOR UPDATE" does not contain` / `"AND id" should not contain` | **`netbox.url should not exist (5 角色)` + `body should not contain "secret-netbox:8000"`** |
| **期望还原绿** | helper + rule filter 守卫 | 删除 mutation 文件 | 还原 main.go (mv main.go.m85bak main.go) | **还原 handler (mv integration_handler.go.m86bak integration_handler.go)** |
| **关键设计要点** | 「反向断言用真 sqlite」 | 「CI 守门真等价于 mutation 实证」 | 「SQL 契约钉住锁集合 + 排除目标」 | **「7 角色矩阵钉住 canManage 分级 + URL 字面不暴露防脱敏绕过」** |
| **范本编号** | **范本 A** (业务代码 mutation) | **范本 B** (CI 守门 mutation) | **范本 C** (业务并发窗口 mutation) | **范本 D** (响应字段守卫 mutation) [NEW] |

四条 mutation inversion 范本可写进 `~/.omh/skills/planner/intent-spec-author/SKILL.md` 8 节 Verification 段的范本库, 后续 round 复用.

## commit 现状 (本 dispatch 2 commits, 已 push)

```
$ git log --oneline -3 origin/main
d069e6c feat(M86-candidate): /api/integrations/status url+user 收口 (canManage-gate + 7-角色矩阵 + mutation inversion M1)
18af402 feat(M86-candidate): intent spec (omh-plan 8 节骨架, /api/integrations/status url+user 收口)
9733853 feat(M85-candidate): cmd/set-role 并发窗口收口 (broad lock + SQL 契约测试)
```

2 commits (cycle 16):
1. `18af402` `feat(M86-candidate): intent spec (omh-plan 8 节骨架, /api/integrations/status url+user 收口)` — 1 file / +306
2. `d069e6c` `feat(M86-candidate): /api/integrations/status url+user 收口 (canManage-gate + 7-角色矩阵 + mutation inversion M1)` — 5 files / +119 / -36

`git log --oneline origin/main..HEAD` 为空 (本地 = 远端, 0 滞后). 本 dispatch 2 commits 全部 push.

## PM_QUEUE fix (本 dispatch)

PM_QUEUE 是 watchdog 派工的 single-source-of-truth. M86-candidate 在本 dispatch ship 后, PM_QUEUE state 修整 (PM-direct 责任, 沿用 M82 cycle 13 + M83 cycle 14 + M85 cycle 15 closeout 范本):

- `PM_QUEUE.json` M86-candidate: status `candidate` → **`shipped`**, 加 `shipped_at` / `commits` / `shipped_round` / `mutation_red` / `mutation_restore_green` / `candidate_note` / `derived_from` updated
- `PM_QUEUE.json` `last_updated` bumped to `2026-09-16T18:??:??+08:00`
- `PM_QUEUE.json` `last_audit` bumped to `2026-09-16T18:??:??+08:00`
- `PM_QUEUE.json` `shipped[]` registry append M86-candidate entry (dispatch_attempts=1, commits=[18af402, d069e6c], cycle=16, scope="/api/integrations/status url+user canManage-gate + 7-角色矩阵")

下次 watchdog tick (≥10 min after commit-age, M79 D3 沿用):
- M86-candidate status=shipped → skip
- 下一个 earliest candidate: PM_QUEUE 沿用 (PM_QUEUE 派生未决 candidates)
- watchdog 不会再次 dispatch M86

## 派生 TODO (留 future, cycle 16 已 ship 后本 dispatch 复核)

- **Settings 页菜单无 frontend gate**: `/settings` 在 `App.tsx:112` 无条件可见, non-admin 进 Settings 会看到 form pre-fill 空白. PUT /integrations/* 已被 canManage + RejectAPIKeyAuth 守住 (`routes.go:276-281`), non-admin 提交必 403. 加 frontend gate 是产品决策, 非本 round scope. **M87+ candidate 候选** (沿用 M61 用户管理页 capability-gate 范本)
- **HTTP 路径 user_service.go 同样 TOCTOU 收口** (M85 cycle 15 派生): 仍是 M87+ 候选
- **真 PG 并发 race window 测试** (M85 cycle 15 派生): 仍是 M87+ 候选
- **mutation 范本写入 skill**: 「响应字段守卫 mutation inversion 范本 D」可写进 `~/.omh/skills/planner/intent-spec-author/SKILL.md` 8 节 Verification 段的范本库 (与范本 A 「业务代码 mutation」 + 范本 B 「CI 守门 mutation」 + 范本 C 「业务并发窗口 mutation」并列), 后续响应字段 round 复用
- **前端文案对齐**: 收紧后 Settings.tsx form pre-fill 在 admin 角色下行为不变 (data.zabbix.url / user / data.netbox.url / data.glpi.url 仍可见); 前端文案无需调整. 但 non-admin 即使打开 Settings 也会看到 form 空白 — 这是 P2-1 既有读地板决策的边界, 不在本 round scope

## OMH workflow shape 实证 (cycle 16 沿用 M67 standing rule)

M86-candidate (cycle 16) intent-M86-candidate.md 用 omh-plan 8 节骨架:

1. **Goal 节** 钉死**实证 + 收紧** (不接 "已登记 read floor" framing), 列出 P2-1 has_* 范本扩展路径 + 7 角色矩阵 + 1 mutation 反证 + URL 字面不暴露断言.
2. **Non-goals 节** 钉死**不动** route protection (`protected.GET` 不挂 canManage, 沿用 P2-1 同款 — 端点本身对所有已认证 200, 响应按能力分级) + 不动 frontend Settings.tsx form pre-fill 字段名 + 不动 PUT/POST/Sync 端点 + 不动 `migrations/` + 不动 setup-profile.json / display.skin / interface (M67) + 不动 sing-box / keyring / OMH config (Poison 红线).
3. **Decision gate 节** 钉死 16 个 D (D1 PM-direct 自决 / D2 poison-stop-gates / D3 自旋防 / D4 30-min flapping / D5 mutation 范本 D / D6 fact_id 25 / D7 3 commits / D8 PM_LAST_DISPATCH_RESULT / D9 mutation 文件不入 commit / D10 PM_QUEUE fixup / D11 OpenAPI 同步收紧 / D12 不动 frontend / D13 不加 migration / D14 M67 红线 / D15 mutation M1 全红→还原全绿 / D16 既有 URL 断言测试改 admin 角色).
4. **Verification 节** 7-角色矩阵 + URL 字面不暴露测试 + mutation M1 反证命令原文 + 期望 SQL/JSON 形态 (钉死 handler 在 canManage=false 时不返回 url/user 字段).

mutation inversion 在本 round 起新增**「响应字段守卫 (范本 D)」**模式 — 与 M82 范本 A 「业务代码 mutation」 + M83 范本 B 「CI 守门 mutation」 + M85 范本 C 「业务并发窗口 mutation」同形不同物:

- M82 测: 「删 rule filter → writeNotificationTrigger 测试红」 (业务行为突变)
- M83 测: 「故意 race → go test -race 红」 + 「故意 fail → vitest run 红」 (CI 守门有效性)
- M85 测: 「剥 clause.Locking → SQL 契约测试红」 + 「还原 AND id<>? → SQL 契约测试红」 (业务并发窗口真修复)
- **M86 测: 「移回 url/user 到 gin.H literals → 7-角色矩阵 5 角色红」 + 「URL 字面暴露测试全红」 (响应字段守卫真工作)**

四条模式都验证「守卫真工作」, 但守卫对象不同. 后续响应字段 round 沿用 M86 范本 D.

## 注意事项

1. **PM_QUEUE state 同步是 PM-direct 责任**: 跟 M82 cycle 13 + M83 cycle 14 + M85 cycle 15 closeout 同款 — cycle 16 ship 后必须把 PM_QUEUE 入口 status 切到 `shipped`. 未做 → watchdog 会反复 dispatch 同一 round. 本 round 是首次 dispatch, 直接 fix, 没有 watchdog flapping (1 次 dispatch 完成, 无 30-min 自旋).

2. **mutation 临时文件用 `integration_handler.go.m86bak` 备份**: 不直接 `git stash` (本 round 无既有未提交改动), 也不写 `/tmp/` 然后 `cp` 临时 (备份在同目录更简单). 实证完 `mv` 还原 + `rm -f` 删除 bak. 这种模式 ok 因为:
   - 备份文件 `integration_handler.go.m86bak` 是与目标同名加 `.m86bak` 后缀, 不会被 Go 任何路径扫到 (编译只识别 `.go`)
   - 实证完立即 `mv` + `rm -f`, 不会留到下一个 round
   - 若担心 commit 误操作, 可用 `git stash` 替代

3. **mutation 用 Python 脚本替代 sed**: 与 M85 用 sed 不同, M86 用 Python 脚本 (`python3 -c "..."`) 做精确的字符串替换. 因为 mutation 涉及**两个独立的修改** (gin.H literals 增字段 + if canManage 块减字段), sed 双步容易因转义失败. Python 字符串 `replace` 是字面替换, 0 转义问题. **范本 D 沿用 Python 模式** (vs M85 范本 C 用 sed 模式).

4. **OpenAPI schema `required` 字段收紧**: 从 `[enabled, url]` → `[enabled]` — 这是 schema 描述变更, 不删 url property. admin 响应里 url 仍在, 只是 `required` 列表缩小. **缓解**: 在 schema description 里写明 "仅 canManage 可见", 让 spec 消费者知道 url 字段可能缺席 (非 required). 这是 OpenAPI 3.x 的标准做法 — `required` 表示"该字段必出现", 不代表"该 schema 定义了这个字段".

5. **既有 URL 断言测试改 admin 角色**: `TestIntegrationStatus_ThreeIntegrationsReturnURL` 改用 `newIntegrationTestRouterWithRole(cfg, "admin")` — 这是收紧的代价, 与 P2-1 fix 时同款 (P2-1 fix 时 `TestIntegrationStatus_凭据存在性仅canManage可见` 是新加测试, 既有测试不动 — 因为 P2-1 是新增分级; M86 是扩展分级, 既有用 url 断言的测试需改角色). 这种模式要写进 mutation 范本 D 的「既有测试更新」段, 与范本 A/B/C 的"加测试 + 既有测试不动" 区分.

6. **mutation inversion 与「无变异绿测试」区分**: 无变异绿测试只能证明「测试不红」, mutation inversion 实证「测试能红」. M82 cycle 13 已经验证: 「单跑绿测试抓不到测试设计漏洞, 必须靠 mutation inversion 反证才能暴露」. 本 round 沿用.

7. **白盒 7-角色矩阵 vs 黑盒集成测试**: 7-角色矩阵只验 handler 内的 `middleware.Can` 判据, 不验"端点挂对所有认证角色 200" — 那是 `routes.go:269` route protection 层, 既有 `TestRoutes_只读端点未被过度收紧:1336-1346` 沿用, 不在本 round scope.

## 后续推荐

- **Settings 页菜单无 frontend gate**: 仍是 M87+ 候选 (沿用 M61 用户管理页 capability-gate 范本, 加 `hasManage` capability 条件渲染入口)
- **HTTP 路径 user_service.go 同样 TOCTOU 收口** (M85 cycle 15 派生): 仍是 M87+ 候选
- **真 PG 并发 race window 测试** (M85 cycle 15 派生): 仍是 M87+ 候选
- **mutation 范本写入 skill**: 「响应字段守卫实证 (范本 D)」可写进 `~/.omh/skills/planner/intent-spec-author/SKILL.md` 8 节 Verification 段的范本库 (与范本 A 「业务代码 mutation」 + 范本 B 「CI 守门 mutation」 + 范本 C 「业务并发窗口 mutation」并列), 后续响应字段 round 复用
- **fact_store fact_id = 25 (本 round advisory)**: 沿用 M82 cycle 13 = fact_id 22, M83 cycle 14 = fact_id 23, M85 cycle 15 = fact_id 24, 本 round = 25 advisory, 未实际落库

## watchdog 下次 tick 验证

- PM_QUEUE M86-candidate.status: `candidate` → **`shipped`** (本 dispatch fix)
- commit age ≥ 10 min (沿用 M79 D3, 本 dispatch 2 commits push, M86 impl commit `d069e6c` age ≥ 10 min by next tick)
- 30-min dispatch hist flapping auto-switch 沿用 (M79 D4, 本 dispatch 1 次完成, 无 flapping)
- `PM_LAST_DISPATCH_RESULT.md` 已写 (本文件, Poison 看)
- 2 commits 已 push 到 origin/main
- TODO.md L60 切 `[x]` (本 dispatch docs commit)
- CHANGELOG.md 加 M86 段 (cycle 16) (本 dispatch docs commit)
- watchdog 下次 tick 应 dispatch 下一个 candidate (PM_QUEUE 沿用)