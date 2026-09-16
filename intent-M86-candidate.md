# M86-candidate — `GET /api/integrations/status` 回传集成 URL 与 Zabbix 用户名收口 (canManage-gate, OMH ulw-loop 第 16 cycle)

> **Loop cycle**: 16 of `itmanager-grit-2026q3`
> **Loop mode**: B (watchdog Mode B auto-dispatched M86-candidate after M85-candidate cycle 15 ship, 10-min commit-age gate 沿用 M79 D3)
> **Dispatch origin**: `~/.hermes/scripts/pm-loop-watchdog.sh` Mode B at 2026-09-16T18:??:??+08:00 (PM_QUEUE M86-candidate = `GET /api/integrations/status` 收口, derived from TODO.md L60 by `pm-loop-derive-candidates.py` M85 ship)
> **Scope**: `backend/internal/api/handlers/integration_handler.go` + tests + openapi.yaml + routes.go 注释, ≤2h estimated (auto-derived)
> **Prerequisite**: M85-candidate (`9733853`, 2026-09-16) cycle 15 ship; P2-1/P2-1-fix 既有 `has_*` canManage-gate 范本 (`integration_handler_test.go:147-198`); settings 页 form pre-fill 范本 (`Settings.tsx:72-90`)

## Goal

PM_QUEUE M86-candidate = **"`GET /api/integrations/status` 回传集成 URL 与 Zabbix 用户名"** 的**收紧** (不是修复 bug, 是补已登记的读地板泄漏):

- **现状**: `integration_handler.go:117-151` `GetIntegrationStatus` 对**所有**已认证角色回传 `netbox.url` / `zabbix.url` / `zabbix.user` / `glpi.url` — 内网拓扑信息 + Zabbix 用户名 (属敏感信息, 文档/审计/ADR 多处登记)
- **核心泄漏面**:
  - TODO.md L60: "`GET /api/integrations/status` 回传集成 URL 与 Zabbix 用户名 — 不含 token，属读地板；若需收紧另立任务"
  - `docs/adr/0005-角色词表与权限矩阵.md:88`: "`GET /api/integrations/status` 仍回传集成 URL 与 Zabbix 用户名（不含 token），属读地板。内网拓扑信息，若需收紧另立任务。"
  - `docs/FIX-PLAN-AUTHZ.md:56`: "对比：`GET /integrations/status` **不含 token**（`integration_handler.go:91-113` 只回 `has_token` 布尔），但**仍回传集成 URL 与 Zabbix 用户名**（内网拓扑信息）"
  - `docs/FIX-PLAN-AUTHZ-CLOSURE.md:203`: "不改 `GET /integrations/status` 回传 URL + Zabbix 用户名（不含 token，见 TODO）" — **M86 立项即收口此条**
- **P2-1 既有范本**: 同样端点的 `has_token` / `has_password` / `has_app_token` / `has_user_token` **已经**按 `canManage` 分级 (admin/ops_admin 可见, 其他角色不可见, `integration_handler_test.go:147-198` 7-角色矩阵钉死). URL + Zabbix 用户名应**沿用同款分级**, 不是新设计 — 是把 P2-1 范本扩展到尚未分级的字段.
- **目标交付**:
  1. **`integration_handler.go` 收紧**: 把 `netbox.url` / `zabbix.url` / `zabbix.user` / `glpi.url` 从「无条件回传」移到 `if canManage { ... }` 块. 非 canManage 角色只看到 `enabled` 布尔 — 这是最小读地板.
  2. **白盒 7-角色矩阵测试**: `TestIntegrationStatus_URL与ZabbixUser仅canManage可见` — 沿用既有 `TestIntegrationStatus_凭据存在性仅canManage可见` 范本, 7 角色 × 3 字段 (netbox.url / zabbix.user / glpi.url), 断言 admin/ops_admin 可见, 其他 5 角色 (ops_user/auditor/readonly/user/空) **不**可见.
  3. **既有契约测试不退化**: 23 个现有 integration_handler_test.go 测试全绿 (更新 3 个用默认角色断言 url 可见的测试 → 加 `WithRole(cfg, "admin")`).
  4. **mutation inversion 实证**: 临时把 `if canManage` 守卫剥掉 (回归「无条件回传」), 跑 7-角色矩阵 → **期望红** (5 个非 canManage 角色看到 url/user) → 还原 → 期望绿.
  5. **Settings.tsx form pre-fill 不退化**: admin 仍能 pre-fill (`data.zabbix.url` / `data.zabbix.user` / `data.netbox.url` / `data.glpi.url` 在 admin 响应里仍在, `Settings.tsx:76-89`).
  6. **OpenAPI 契约 + 路由注释**: `IntegrationStatus` schema description 去掉"已登记的读地板"豁免语, 改为按能力分级描述; `routes.go:267` 注释从「只读状态查询不限」收紧到「只回 `enabled`; 配置详情限 manage」.
  7. **TODO.md L60 切 `[x]`** + CHANGELOG M86 段 + `M86-candidate-completion-report.md` + `M86-candidate-graph-analysis.md`.

## Non-goals

- **不动** `has_token` / `has_password` / `has_app_token` / `has_user_token` 既有分级 (P2-1 已 ship, 沿用)
- **不动** route protection (`routes.go:269` `protected.GET("/integrations/status", ...)` 不挂 `canManage` — 端点本身仍对所有已认证角色 200, 只是响应字段按能力分级; 与 P2-1 同款)
- **不动** Settings.tsx form pre-fill 字段名 (`data.zabbix.url` / `user` 等) — admin 仍能看到这些字段, 沿用既有 form 逻辑
- **不动** `internal/integration/*` (集成业务代码), `internal/api/routes_integration_test.go` 既有 22 路由分组的角色矩阵, `cmd/*` 任何 CLI 命令
- **不动** `migrations/` (无 schema 变更, 与 M85 同款: 收紧在 Go 代码层)
- **不动** `setup-profile.json` / `display.skin` / `interface` (M67 standing rule)
- **不动** `sing-box` / `keyring` / `OMH config` (Poison 红线)
- **不动** `go.mod` / `package.json` (本 round 不改依赖)
- **不**新增 `db_smoke.sh` 真 PG 测试 (P2-1 已 ship 既有, 本 round 沿用白盒 + role 矩阵, 无 DB I/O 引入)
- **不**改路由前缀 / 端点名 / HTTP method (与 P2-1 同款: 端点本身不动, 只动响应字段)
- **不**为非 admin 加 Settings 替代入口 (non-admin 无管理集成能力是产品决策, 非本 round scope)

## Assumptions

- ITmanager repo HEAD = `9733853` (M85-candidate cycle 15 ship — cmd/set-role 并发窗口收口), working tree clean, branch `main` up-to-date with `origin/main`
- P2-1 既有 `TestIntegrationStatus_凭据存在性仅canManage可见` (`integration_handler_test.go:147-198`) 是 7-角色矩阵的范本, M86 沿用同款 assert 结构 + 同款角色表 (admin/ops_admin/ops_user/auditor/readonly/user/空)
- `internal/middleware/roles.go:59` `CapManage = {RoleAdmin, RoleOpsAdmin}` — 沿用既有 canManage 判据, 不引入新能力
- Settings 页 menu 项 (`App.tsx:112`) 对所有角色无条件可见; admin 用 form pre-fill, non-admin 即使点进去也无 canManage → PUT /integrations/* 必 403 (G-33/S-4 既有 `RejectAPIKeyAuth` + canManage 守门, `routes.go:276-281`)
- `newIntegrationTestRouterWithRole(cfg, role)` 既有 helper (`integration_handler_test.go:30-49`) — 设 `c.Set("role", role)` 在 route 注册前注入; role 为空 = fail-safe 只读地板
- 临时 mutation 文件用 `m86_` 前缀 (与 M83 同款, 不撞既有测试命名); 实证完 `mv main.go.m86bak main.go` 还原 + `rm -f` 删除 bak
- Go 1.25.14 (本地) 与 CI runner Go 1.25 一致, 测试基座稳定
- fact_store fact_id = 25 advisory (沿用 M82 cycle 13 = 22, M83 cycle 14 = 23, M85 cycle 15 = 24, 本 round = 25, 未实际落库)
- watchdog tick 时段: M86-candidate 由 Mode B 自动 dispatch, commit-age ≥ 10 min 才起下一 round (M79 D3 沿用)
- `~/.hermes/state/PM_LAST_DISPATCH_RESULT.md` 写 M86 closeout (Poison 看 + watchdog 下次 tick 验证)

## Acceptance criteria

| 标准 | 实证 |
|---|---|
| `intent-M86-candidate.md` 8 节 omh-plan 骨架 (Goal / Non-goals / Assumptions / Acceptance / Verification / Risks / Plan / Decision gate) | 本文件 (commit 1) |
| `backend/internal/api/handlers/integration_handler.go` 把 `netbox.url` / `zabbix.url` / `zabbix.user` / `glpi.url` 移到 `if canManage { ... }` 块内 | verify (grep) |
| `integration_handler.go` 注释明确说明"非 canManage 角色只看到 enabled 布尔; url/user 仅 canManage 可见 (P2-1 同款范本)" | verify |
| `integration_handler_test.go` 加 `TestIntegrationStatus_URL与ZabbixUser仅canManage可见`: 7 角色 × 3 字段, 断言 admin/ops_admin 可见 url/user, 其他 5 角色不可见 | verify |
| `integration_handler_test.go` 加 `TestIntegrationStatus_URL与User不可见_不暴露配置拓扑`: 单角色 (readonly) 配 netbox.url="http://secret-netbox:8000", 断言响应**不**含该 URL 字符串 | verify (mutation 反证前置) |
| `integration_handler_test.go` 既有 `TestIntegrationStatus_凭据存在性仅canManage可见` 注释更新: 从 "对所有已认证角色可见" 改为 "canManage 可见 (与 url/user 同款分级)" | verify |
| `integration_handler_test.go` 3 个既有 URL/USER 断言测试 (`TestIntegrationStatus_ThreeIntegrationsReturnURL` / `TestIntegrationStatus_MixedConfig_PartialEnabled` / 等) 改用 `WithRole(cfg, "admin")` — 不退化 | verify |
| **mutation inversion 实证 — canManage 守卫真在门** | see Verification §3 |
| &nbsp;&nbsp; M1: 临时把 url/user 移出 `if canManage` 块 (回归无条件回传) → 7-角色矩阵测试**红** (5 个非 canManage 角色看到 url/user) → 还原 → 绿 | verify |
| mutation 临时文件实证完**全部 rm** (不入 commit) | verify (git status) |
| 23 个既有 integration_handler_test.go 测试全绿 (含本 round 新增) | verify (`go test ./internal/api/handlers/`) |
| `cd backend && go test -race -count=1 -timeout=180s ./...` 全绿 (28 packages, 0 退化) | verify |
| `routes.go:267-269` 注释从"只读状态查询不限"改为"只回 `enabled`; 配置详情限 manage" | verify |
| `openapi.yaml` `IntegrationStatus` schema description 去掉"已登记读地板"豁免语, 改为按能力分级描述 (`url` / `user` 仅 canManage 可见) | verify |
| `routes_integration_test.go:839` route map 描述从"集成连通状态（只回 URL/用户名，不含 token）"改为"集成连通状态（仅 enabled 全可见; url/user 仅 canManage）" | verify |
| `M86-candidate-completion-report.md` + `M86-candidate-graph-analysis.md` 写完 | docs commit 3 |
| `CHANGELOG.md` M86 段加 "URL + Zabbix 用户名收口 (canManage-gate)" 条目 | docs commit 3 |
| `TODO.md` L60 `- [ ]` → `- [x]`, 标注"已 ship M86-candidate (url/user canManage-gate + 7-角色矩阵 + mutation inversion M1)" | docs commit 3 |
| git log 3 commits, 全部 push 到 origin/main | verify |
| `~/.hermes/state/PM_QUEUE.json` M86-candidate.status: `candidate` → **`shipped`** | state fixup |
| `~/.hermes/state/PM_LAST_DISPATCH_RESULT.md` 写完 | verify |

## Verification

### 1. 收紧字段定位 (grep verify)

```bash
$ grep -n '"url"\|"user":' backend/internal/api/handlers/integration_handler.go
127:			"enabled": h.config.Integrations.Netbox.URL != "",
128:			"url":     h.config.Integrations.Netbox.URL,
129:		}
130:		zabbix := gin.H{
131:			"enabled": h.config.Integrations.Zabbix.URL != "",
132:			"url":     h.config.Integrations.Zabbix.URL,
133:			"user":    h.config.Integrations.Zabbix.User,
134:		}
135:		glpi := gin.H{
136:			"enabled": h.config.Integrations.GLPI.URL != "",
137:			"url":     h.config.Integrations.GLPI.URL,
138:		}
139:		if canManage {
140:			netbox["has_token"] = h.config.Integrations.Netbox.Token != ""
141:			zabbix["has_password"] = h.config.Integrations.Zabbix.Password != ""
142:			glpi["has_app_token"] = h.config.Integrations.GLPI.AppToken != ""
143:			glpi["has_user_token"] = h.config.Integrations.GLPI.UserToken != ""
144:		}

# M86 收紧后 (期望):
$ grep -n '"url"\|"user":' backend/internal/api/handlers/integration_handler.go
127:			"enabled": h.config.Integrations.Netbox.URL != "",
128:		}
...
138:		if canManage {
139:			netbox["url"] = h.config.Integrations.Netbox.URL
140:			zabbix["url"] = h.config.Integrations.Zabbix.URL
141:			zabbix["user"] = h.config.Integrations.Zabbix.User
142:			glpi["url"] = h.config.Integrations.GLPI.URL
143:			netbox["has_token"] = h.config.Integrations.Netbox.Token != ""
...
```

任一 FAIL = 收紧未应用, 红测试。

### 2. 既有契约测试 (Green: 23 既有测试 + 2 新测试)

**既有 (收紧后)**:
```bash
$ cd backend && go test -race -count=1 ./internal/api/handlers/
ok  	network-monitor-platform/internal/api/handlers  [23 PASS, 2 new PASS, 0 FAIL]
```

**全 backend 不退化**:
```bash
$ cd backend && go test -race -count=1 -timeout=180s ./...
ok  	network-monitor-platform/cmd/admin-bootstrap  	~7s
ok  	network-monitor-platform/cmd/migrate           	~1s
ok  	network-monitor-platform/cmd/seed              	~30s
ok  	network-monitor-platform/cmd/set-role          	~1s (M85 测试不退化)
ok  	network-monitor-platform/internal/api          	~23s
ok  	network-monitor-platform/internal/api/handlers 	~13s (含 2 M86 新测试)
... (28 packages, all ok, 0 FAIL)
```

### 3. mutation inversion 实证

**M1 反证"canManage 守卫真在门"**:

```bash
# 临时把 url/user 移出 if canManage (回归无条件回传)
$ sed -i.bak 's|^\t\t\tnetbox\["url"\] = h.config.Integrations.Netbox.URL$|\t\tnetbox["url"] = h.config.Integrations.Netbox.URL // MUTATION M1: 临时移到外面|' backend/internal/api/handlers/integration_handler.go
$ sed -i.bak 's|^\t\t\tzabbix\["url"\] = h.config.Integrations.Zabbix.URL$|\t\tzabbix["url"] = h.config.Integrations.Zabbix.URL // MUTATION M1: 临时移到外面|' backend/internal/api/handlers/integration_handler.go
$ sed -i.bak 's|^\t\t\tzabbix\["user"\] = h.config.Integrations.Zabbix.User$|\t\tzabbix["user"] = h.config.Integrations.Zabbix.User // MUTATION M1: 临时移到外面|' backend/internal/api/handlers/integration_handler.go
$ sed -i.bak 's|^\t\t\tglpi\["url"\] = h.config.Integrations.GLPI.URL$|\t\tglpi["url"] = h.config.Integrations.GLPI.URL // MUTATION M1: 临时移到外面|' backend/internal/api/handlers/integration_handler.go

$ cd backend && go test -race -count=1 -v -run 'TestIntegrationStatus_URL与ZabbixUser仅canManage可见|TestIntegrationStatus_URL与User不可见_不暴露配置拓扑' ./internal/api/handlers/
=== RUN   TestIntegrationStatus_URL与ZabbixUser仅canManage可见/ops_user
    Error: 期望 ops_user 不看到 netbox.url, 但响应包含 "url":"http://netbox.local"
--- FAIL: TestIntegrationStatus_URL与ZabbixUser仅canManage可见/ops_user (0.00s)
=== RUN   TestIntegrationStatus_URL与ZabbixUser仅canManage可见/auditor
    Error: 期望 auditor 不看到 zabbix.user, 但响应包含 "user":"Admin"
--- FAIL: TestIntegrationStatus_URL与ZabbixUser仅canManage可见/auditor (0.00s)
=== RUN   TestIntegrationStatus_URL与User不可见_不暴露配置拓扑
    Error: 期望 readonly 响应不含 "http://secret-netbox:8000", 但实际含
--- FAIL: TestIntegrationStatus_URL与User不可见_不暴露配置拓扑 (0.00s)
FAIL
```

**还原 (control)**:
```bash
$ mv backend/internal/api/handlers/integration_handler.go.bak backend/internal/api/handlers/integration_handler.go

$ cd backend && go test -race -count=1 -run 'TestIntegrationStatus_URL与ZabbixUser仅canManage可见|TestIntegrationStatus_URL与User不可见_不暴露配置拓扑' ./internal/api/handlers/
ok  	network-monitor-platform/internal/api/handlers  [7 PASS + 1 PASS, 0 FAIL]
```

**关键设计要点**:
- **白盒 7-角色矩阵**: 沿用 P2-1 既有 `TestIntegrationStatus_凭据存在性仅canManage可见` 范本 (`integration_handler_test.go:147-198`), 同款 assert 结构 + 同款角色表. 一致性 = 可读性, 比单独写新模式容易维护.
- **URL 字符串不可见断言** (`TestIntegrationStatus_URL与User不可见_不暴露配置拓扑`): 不只断 "url 键不存在", 还断 "响应 body 字面不含 URL 字符串" — 防 G-28 同款「脱敏绕过」(URL 出现在 debug log / 错误回显等非响应键路径上, 范本见 `TestTestZabbix_失败回显不泄漏URL凭据:634-643`). 单测冗余一道白盒断言 = 防白盒盲点.
- 两个 mutation 临时文件实证完**全部 rm**, `git status --short` 不含 mutation 痕迹 = D9 实证.
- **不**测 mutation M2 ("剥 if canManage 块"): M1 已覆盖"守卫真在门"语义 — 无论怎么改 if 块内的字段位置, 只要守卫在 = 守卫工作. M2 与 M1 同形, 重复.
- M86 与 M85 范本 C (业务并发窗口) 同形不同物: M85 测 SQL 守卫 (FOR UPDATE), M86 测响应字段守卫 (canManage-gate); 都是"业务代码 + 守卫真工作"的 mutation inversion.

### 4. Settings.tsx form pre-fill 不退化 (前端逻辑)

```bash
$ grep -n 'data.zabbix\|data.netbox\|data.glpi' frontend/src/pages/Settings.tsx
75:      if (data?.zabbix) {
76:        zabbixForm.setFieldsValue({
77:          url: data.zabbix.url || '',
78:          user: data.zabbix.user || '',
79:        })
80:      }
81:      if (data?.netbox) {
82:        netboxForm.setFieldsValue({
83:          url: data.netbox.url || '',
84:        })
85:      }
86:      if (data?.glpi) {
87:        glpiForm.setFieldsValue({
88:          url: data.glpi.url || '',
89:        })
90:      }
```

admin 角色 (有 canManage) 仍能拿到 `data.zabbix.url` / `user` / `data.netbox.url` / `data.glpi.url` — form pre-fill 行为**不变**. non-admin 即使打开 Settings 也只会拿到 `enabled` 布尔, 但表单字段为空 (frontend 无 admin gate, 这是已知 — non-admin 想填也无 canManage, PUT 必 403).

### 5. TODO.md L60 切 [x]

```bash
$ git diff TODO.md | grep "integrations/status"
-- [ ] **`GET /api/integrations/status` 回传集成 URL 与 Zabbix 用户名** — 不含 token，属读地板；若需收紧另立任务
++ [x] **`GET /api/integrations/status` 回传集成 URL 与 Zabbix 用户名** — 已 ship M86-candidate (url/user canManage-gate + 7-角色矩阵 + mutation inversion M1 实证), 见 `M86-candidate-completion-report.md`. url/user 与 P2-1 `has_*` 同款按 canManage 分级, non-admin 仅见 `enabled`
```

### 6. CHANGELOG M86 段

```markdown
- **M86-candidate `GET /api/integrations/status` URL + Zabbix 用户名收口** (backend handler + tests + openapi + docs, ≤2h)
  — TODO.md L60 "`GET /api/integrations/status` 回传集成 URL 与 Zabbix 用户名 — 不含 token，属读地板；若需收紧另立任务" 的收口.
  修复路径: 把 `netbox.url` / `zabbix.url` / `zabbix.user` / `glpi.url` 从无条件回传移到 `if canManage { ... }` 块,
  非 canManage 角色 (ops_user/auditor/readonly/user/空) 仅见 `enabled` 布尔 — 与 P2-1 `has_*` 同款按能力分级.
  新增 2 测试: `TestIntegrationStatus_URL与ZabbixUser仅canManage可见` (7 角色矩阵) + `TestIntegrationStatus_URL与User不可见_不暴露配置拓扑` (URL 字面不暴露).
  mutation inversion M1 反证: 临时把 url/user 移出 if canManage → 7-角色矩阵测试红 (5 角色看到 url) + URL 字面暴露测试红 → 还原 → 全绿.
  见 `M86-candidate-completion-report.md`.
```

## Risks

- **Settings 页 form pre-fill 在 non-admin 角色下空白**: 当前 frontend 无 admin gate (Settings menu 无条件可见, `App.tsx:112`), non-admin 进 Settings 会看到 form 空白. 这是**已知**: PUT /integrations/* 已被 canManage + RejectAPIKeyAuth 守住 (`routes.go:276-281`), non-admin 提交必 403, form 空白是"产品决策"边界 — 本 round 不加 frontend gate. **缓解**: docs 注释说明 (本 round 不动 frontend gate 是 M86+ 候选, 留 TODO 登记).
- **既有 3 个 URL 断言测试需要更新**: `TestIntegrationStatus_ThreeIntegrationsReturnURL` / `TestIntegrationStatus_MixedConfig_PartialEnabled` / `TestIntegrationStatus_Zabbix_HasUserAndHasPassword` 用默认 role (fail-safe only-read) 断言 URL 可见, 收紧后这些断言会反向红. **必须**改用 `WithRole(cfg, "admin")`. 测试失败的红路径就是"真收紧"的证据, 红即修 — 与 P2-1 既有 P2-1 fix 收紧 `has_*` 时同款 (`integration_handler_test.go:147-198` 是新加测试, 既有测试同步更新为 admin 角色).
- **mutation M1 sed 模板脆弱**: sed 命令依赖源文件精确缩进 (`\t\t\tnetbox["url"] = ...`), 若 Go formatter 重排缩进, sed 命令会失败. **缓解**: 用 Python 脚本替代 sed (直接 read → replace → write), 或用 `go fmt` 前先备份 + 还原 (M85 用过同款模式).
- **OpenAPI 契约 schema description 改写**: `IntegrationStatus` schema description 是既有契约的一部分 (`openapi.yaml:3789-3822`), 改 description 不改 property 定义 (url/user 仍声明) — `required` 字段也要从 `netbox`/`zabbix`/`glpi` 三个对象去掉 `url`/`user`, 改为只在 canManage 角色下出现. **关键**: 不能直接删 `url` / `user` property (admin 响应里仍有), 只能改 `required` 字段 + description.
- **routes_integration_test.go:839 route map 描述**: 是 ungatedRoutes 的注释, 改一个字面量字符串无副作用 — 改描述反映收紧后真实形态.
- **PM_QUEUE state 同步漏**: 沿用 M82 cycle 13 + M83 cycle 14 + M85 cycle 15 closeout 模式, 必须把 status 切 `shipped` + append `shipped[]` registry, 否则 watchdog 会反复 dispatch M86.
- **既有 mutation 文件清理**: 实证完 `rm -f integration_handler.go.bak`, 不留到下一 round. `git status --short` 二次确认.
- **白盒 7-角色矩阵 vs 黑盒集成测试**: 7-角色矩阵只验 handler 内的 `middleware.Can` 判据, 不验"端点挂对所有认证角色 200" — 那是 `routes.go:269` route protection 层, 既有 `TestRoutes_只读端点未被过度收紧:1336-1346` 沿用, 不在本 round scope.

## Plan

1. **写 `intent-M86-candidate.md`** (本文件, 8 节 omh-plan 骨架) — **feat commit 1**: `feat(M86-candidate): intent spec (omh-plan 8 节骨架, /api/integrations/status url+user 收口)`

2. **impl 收紧** (`backend/internal/api/handlers/integration_handler.go:117-151`):
   - 把 `netbox["url"]` / `zabbix["url"]` / `zabbix["user"]` / `glpi["url"]` 从无条件赋值移到 `if canManage { ... }` 块内
   - 注释更新: "非 canManage 角色只看到 `enabled` 布尔; url/user 仅 canManage 可见 (P2-1 同款范本, `docs/adr/0005` §3.2)"

3. **impl 测试更新**:
   - 既有 3 测试加 `WithRole(cfg, "admin")`: `TestIntegrationStatus_ThreeIntegrationsReturnURL` / `TestIntegrationStatus_MixedConfig_PartialEnabled` (assertion 含 url 字段), 改 admin 角色注入
   - 既有 `TestIntegrationStatus_凭据存在性仅canManage可见` 注释更新: 从 "对所有已认证角色可见" → "canManage 可见 (与 url/user 同款分级)"
   - 新增 `TestIntegrationStatus_URL与ZabbixUser仅canManage可见`: 7 角色矩阵 × 3 字段 (netbox.url / zabbix.user / glpi.url), 断言 admin/ops_admin 可见, 其他 5 角色不可见
   - 新增 `TestIntegrationStatus_URL与User不可见_不暴露配置拓扑`: readonly 角色, 配置 netbox.url="http://secret-netbox:8000", 断言响应 body **不**含该 URL 字符串

4. **mutation inversion 实证**:
   - **M1**: sed / Python 临时把 url/user 移出 `if canManage` 块 → 跑 7-角色矩阵 + URL 字面暴露测试 → 期望红 (5 角色看到 url/user, URL 字面暴露) → `mv integration_handler.go.m86bak integration_handler.go` 还原
   - mutation bak 文件**rm**, `git status --short` 仅 commit 2 (impl + tests) 才算闭环

5. **既有用例复核**:
   - `cd backend && go test -race -count=1 ./internal/api/handlers/` → 25 个测试全绿 (23 既有 + 2 新)
   - `cd backend && go test -race -count=1 -timeout=180s ./...` → 28 packages 全绿, 0 退化

6. **impl docs 更新** (commit 2 内含, 与 impl 同 commit):
   - `backend/internal/api/routes.go:267-269` 注释更新: "只回 `enabled`; 配置详情 (url / user) 限 manage — 与 P2-1 `has_*` 同款"
   - `backend/internal/api/openapi.yaml:3789-3822` `IntegrationStatus` schema description + `required` 字段: 改为按能力分级描述, `required` 改为 `[enabled]` (url/user 仅 canManage 可见)
   - `backend/internal/api/routes_integration_test.go:839` route map 描述: "集成连通状态（仅 enabled 全可见; url/user 仅 canManage）"

7. **写 docs**:
   - `M86-candidate-completion-report.md` (收紧实证 + mutation inversion 反证 + commit 序列 + 派生 TODO)
   - `M86-candidate-graph-analysis.md` (handler 节点图 + 7-角色矩阵节点图 + mutation inversion 调用链 + P2-1↔M86 范本对比 + Settings form pre-fill 节点图)
   - `CHANGELOG.md` M86 段加条目 (放在 M85 之后, cycle 16)
   - `TODO.md` L60 `- [ ]` → `- [x]`
   - — **docs commit 3**: `docs(M86-candidate): completion + graph analysis + CHANGELOG + TODO (url/user canManage-gate)`

8. **commit + push** 3 commits 到 origin/main

9. **PM_QUEUE state fixup**: M86-candidate.status `candidate` → `shipped`, append `shipped[]` registry, bump `last_updated` / `last_audit`

11. **写 `~/.hermes/state/PM_LAST_DISPATCH_RESULT.md`** (Poison 看 + watchdog 下次 tick 验证)

### Commit 序列

```
9733853 (HEAD, M85-candidate cycle 15)
   ↓
M86 commit 1: feat(M86-candidate): intent spec (omh-plan 8 节骨架, /api/integrations/status url+user 收口)
M86 commit 2: feat(M86-candidate): url+user canManage-gate 收口 (handler 收紧 + 7-角色矩阵 + mutation inversion M1)
M86 commit 3: docs(M86-candidate): completion + graph analysis + CHANGELOG + TODO (url/user 收口)
```

3 commits (沿用 M78/M79/M80/M81/M82/M83/M85 既有 pattern; intent spec 1 + impl+docs-in-source 1 + docs 1). mutation inversion 在 commit 2 之前完成, 不入 commit.

## Decision gate

- **D1**: scope = **`GET /api/integrations/status` 响应字段收紧** (url/user canManage-gate + 7-角色矩阵 + mutation inversion M1), 不动 route protection / 不动 frontend form pre-fill 字段名 / 不动 PUT/POST/Sync 端点
- **D2**: 沿用 poison-stop-gates-v1 (PM_LOOP_MODE = "stop" → freeze)
- **D3**: 沿用 watchdog 自旋防 + commit age ≥ 10 min
- **D4**: 沿用 30-min dispatch hist flapping auto-switch (M79 D4)
- **D5**: mutation inversion = **响应字段守卫** 模式 (范本 D, NEW) — 与 M82 范本 A (业务代码 mutation) + M83 范本 B (CI 守门 mutation) + M85 范本 C (业务并发窗口 mutation) 同形不同物; 都验证「守卫真工作」. 守卫对象 = `if canManage` 分级块, 反证方式 = 剥守卫 → 多角色看到字段 → 红
- **D6**: 不写新 fact_store entry (沿用 M82 = 22, M83 = 23, M85 = 24, 本 round = 25 advisory)
- **D7**: 3 commits (intent + impl+in-source-docs + docs, 沿用 M78/M79/M80/M81/M82/M83/M85 pattern)
- **D8**: PM_LAST_DISPATCH_RESULT.md 写到 `~/.hermes/state/`, watchdog 下次 tick 验证
- **D9**: mutation 临时文件**不入 commit** (用 `integration_handler.go.m86bak` 隔离, 实证完 mv 还原 + `git status --short` 二次确认 + `rm -f` 删除 bak)
- **D10**: PM_QUEUE M86-candidate.status: `candidate` → **`shipped`**, append `shipped[]` registry (沿用 M82 cycle 13 + M83 cycle 14 + M85 cycle 15 closeout 范本)
- **D11**: 接受 OpenAPI schema description + required 字段同步收紧 (admin 响应里 url/user 仍存在, 只是 non-admin 不可见; schema 描述按能力分级, 不是删 property)
- **D12**: 不动 frontend Settings.tsx (admin form pre-fill 行为不变; non-admin 即使进 Settings 也只能 pre-fill 空白, 但 PUT 必 403 — 这是产品决策, 非本 round scope)
- **D13**: 不加新迁移 (收紧在 Go 代码层, 不需要 schema 变更; 沿用 M82/M85 范本)
- **D14**: 不动 setup-profile.json / display.skin / interface (M67 standing rule 沿用)
- **D15**: mutation M1 反证必须**全红 → 还原全绿**, 否则不算闭环
- **D16**: 既有 3 个 URL/USER 断言测试必须同步更新为 admin 角色 (这是收紧的代价, 与 P2-1 既有 `has_*` 收紧时同款)