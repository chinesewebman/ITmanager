# 修复需求文档：角色词表归一 + 权限矩阵（AUTHZ）

> 状态: **已实现（2026-09-09，三路审计后迭代完成）** — 决策见 [ADR-0005](adr/0005-角色词表与权限矩阵.md)
> 日期: 2026-09-09
> 基线: `main` @ 1099e67
> 依据: `docs/v3-架构优化需求.md` §9、`docs/FIX-PLAN-D1-D7.md` §7 R-2、`06-用户权限.md` §6.0.2
> 关联: [ADR-0003](adr/0003-grpc-作废与三层定位.md)

---

## 0. 一句话

`users.role` 在仓库里存在**五处互不相同的词表**，而唯一的授权中间件 `RequireRole("admin")` 只认字面量 `admin` —— 于是 `ops_admin` 被自己的门禁挡在门外（D-6 自锁）、`readonly` 能删资产（越权）、`auditor` 读不了审计日志（角色形同虚设）。本轮把词表收敛成一套、把权限写成矩阵、把每条写/删/凭据路由按能力挂载。

---

## 1. 现状实测（2026-09-09，非推测）

### 1.1 五处词表

| # | 出处 | 取值 | 问题 |
|---|------|------|------|
| V1 | `backend/migrations/000001_init.up.sql:1151-1155`（`roles.code`） | `admin` `ops_admin` `ops_user` `readonly` `auditor` | **权威**（000013 的 `users.role` 回填就取自这里） |
| V2 | `backend/cmd/seed/main.go:49,68,81` | `admin` **`operator`** `readonly` | `operator` 不在 V1 |
| V3 | `backend/internal/models/user.go:23`（注释） | `admin` **`operator`** `readonly` | 同上 |
| V4 | `backend/internal/api/openapi.yaml:2395` + `frontend/src/types/index.ts:7` | `admin` **`operator`** **`viewer`** | 两个都不在 V1 |
| V5 | `backend/migrations/000013_schema_align.up.sql:301-303`（兜底） | **`user`** | 不在 V1；仅作最小权限兜底 |

**权威口径**：V1（`roles.code`）。理由：迁移 000013 从 `user_roles`/`roles` 回填 `users.role`（`000013:292-303`），生产库的值必然来自 V1；写 `users.role` 的代码路径只有三处 —— `cmd/seed`、`cmd/admin-bootstrap:89`（取 `roles.code='admin'`）、迁移 000013，**没有任何 HTTP 接口能改角色**。dev 库经 `cmd/seed` 可写入 `operator`，由 §3.1 别名兼容。

### 1.2 门禁现状

`RequireRole` 在 `internal/api/routes.go` 被调用 **17 处**（覆盖 18 条路由注册，`users.Use()` 一处覆盖 2 条），**全部是 `RequireRole("admin")`**；其余路由对 **JWT 认证的用户**没有角色写门禁（API Key 路径另有方法级 scope 门禁，见 `middleware/auth.go:206-225`）。

| 类别 | 现状 | 后果 |
|------|------|------|
| 已挂 admin 的 17 条（用户列表 / API Key / 集成写测 / 审计日志 / 通知渠道写） | `RequireRole("admin")` | **`ops_admin` 403**（D-6 自锁，`FIX-PLAN-D1-D7.md` §7 R-2 已记录） |
| 未挂门禁的写/删路由（约 31 条） | 无 | **任何已登录用户（含 `readonly`）可执行** |

未挂门禁的破坏性路由（实测清单）：

| 路由 | 方法 | 危害 |
|------|------|------|
| `/api/assets/:id` | DELETE | 删资产 |
| `/api/assets/:id/retire`、`/restore` | POST | 软退役 / 恢复资产 |
| `/api/alert-rules`、`/:id` | POST/PUT/DELETE | 改删告警规则 |
| `/api/alerts/bulk-delete` | POST | 批量删告警 |
| `/api/tickets`、`/:id` | POST/PUT | 建改工单 |
| `/api/alert-suppressions`、`/:id` | POST/PUT/DELETE | 改删抑制规则 |
| `/api/oncall/schedules`、`/api/oncall/schedules/:id/shifts`、`/api/oncall/policies` | POST | 改值班与升级策略 |
| `/api/oncall/schedules/:id`、`/shifts/:shift_id`、`/policies/:id` | DELETE | 删值班与升级策略 |
| `/api/runbooks`、`/:id` | POST/PUT/DELETE | 改删 Runbook |
| `/api/metric-snapshots` | POST | 注入伪造指标 |
| `/api/diagnostics/ping`、`/traceroute` | GET | 服务端主动外连（内网可达性探测） |

**另有凭据泄露（读路径）**：`GET /api/notification-channels` 无门禁且**不脱敏**（`channel_handler.go:23-30` 直接返回模型，`Config` 含 `smtp_password`/`webhook_url`，见 `internal/notification/sender.go:77,84`）→ `readonly` 可读出渠道明文凭据。对比：`GET /integrations/status` **不含 token**（`integration_handler.go:91-113` 只回 `has_token` 布尔），但**仍回传集成 URL 与 Zabbix 用户名**（内网拓扑信息）——本轮保留在读地板，未收紧。

### 1.3 `auditor` 角色失效

`roles` 表给 `auditor` 授了 `p.action='read'`（`000001_init.up.sql:1189-1190`），但 `GET /api/audit-logs` 挂的是 `RequireRole("admin")`（`routes.go:215`）→ 审计员读不到审计日志，角色设计落空。

### 1.4 矩阵角色在生产不可达（审查新增）

`users.role` 无任何 HTTP 写接口（`routes.go:273-278` 只有 `GET /users`、`GET /users/:id`），`admin-bootstrap` 恒写 `admin`，`seed` 只写 `admin`/`operator`/`readonly` → **部署后没有任何途径把用户设成 `ops_admin`/`ops_user`/`auditor`**，只能手写 SQL。本轮必须补一条最小分配路径，否则矩阵有一半角色是纸面的。

---

## 2. 目标

1. **词表归一**：Go 里只有一套角色常量（= V1 + 兜底 `user`）；`seed` / `openapi` / 前端类型向它对齐；V2/V4 的遗留值按别名读入并记录为 deprecated。
2. **权限矩阵**：把「谁能做什么」写成一个**可读、可测、单点**的表，而不是散落在 17 个调用点的字面量。
3. **按能力挂载**：写/删/凭据路由全部挂上矩阵对应的能力；只读端点**默认不额外收紧**（防过度收紧导致可用性下降），凭据类与主动外连类端点列为**显式例外**。
4. **可分配**：补 `cmd/set-role`（直连 DB，不经 HTTP），让矩阵的每个角色在生产可达。

---

## 3. 权限矩阵（决策）

### 3.1 角色词表（权威）

| 角色 | 含义 | 来源 |
|------|------|------|
| `admin` | 超级管理员，完全控制 | V1 |
| `ops_admin` | 运维管理员：业务写 + 破坏性删除 + 集成/渠道配置 | V1 |
| `ops_user` | 运维人员：业务写，无删除、无配置 | V1 |
| `auditor` | 审计员：只读 + 审计日志 | V1 |
| `readonly` | 只读用户 | V1 |
| `user` | 兜底最小权限（000013 引入，等同 `readonly`） | V5 |

**遗留别名（deprecated，只读入不写出）**：`operator` → `ops_user`，`viewer` → `readonly`。保留是为了「存量 dev 库 + 未过期 JWT（24h）不静默降权」，v4 清理。

### 3.2 能力矩阵

| 能力 | admin | ops_admin | ops_user | auditor | readonly | user（含未知/空） |
|------|:-----:|:---------:|:--------:|:-------:|:--------:|:----------------:|
| `read`（未被更高能力覆盖的端点默认放行） | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| `write`（业务写：资产增改、告警确认、工单、抑制、排班、Runbook、规则、探活、指标写入） | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ |
| `manage`（破坏性删除 + 集成凭据 + 通知渠道 + 资产硬删） | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ |
| `audit`（审计日志读取） | ✅ | ✅ | ❌ | ✅ | ❌ | ❌ |
| `identity`（用户列表 / API Key 签发吊销） | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ |

**设计说明**：

- `read` 是**地板**，语义是「**未被 §3.3 更高能力覆盖的端点**默认放行」，不是「所有 GET」。因此 `GET /api/users`（`identity`）、`GET /api/audit-logs`（`audit`）、`GET /api/notification-channels`（`manage`）、`GET /api/diagnostics/ping|traceroute`（`write`）都是**显式例外**。
- `manage` 与 `write` 的分界 = **不可逆或涉及凭据**。软退役（`retire`）可逆 → `write`；硬删 → `manage`。
- `identity` 只给 `admin`：签发 API Key / 看用户列表是身份与凭据操作，`ops_admin` 不持有（这也是 `admin` 与 `ops_admin` 的唯一分界，矩阵因此有实际含义而非同义词）。
- `audit` 给 `auditor`：修复 §1.3 的角色失效；同时保留 `admin`/`ops_admin` 访问。
- **已知例外（凭据）**：`GET /api/notification-channels` 归 `manage`，因为响应体含渠道明文凭据（§1.2）。**不做** config 脱敏（会改变前端 Settings 的编辑契约，属独立任务，见 §7）。

### 3.3 路由挂载清单

| 端点 | 方法 | 能力 |
|------|------|------|
| `/api/auth/api-keys`、`/:id`、`/:id/revoke` | GET/POST/DELETE/PUT | `identity` |
| `/api/users`、`/:id` | GET | `identity` |
| `/api/integrations/sync` | POST | `manage` |
| `/api/integrations/{zabbix,netbox,glpi}` | PUT | `manage` |
| `/api/integrations/{zabbix,netbox,glpi}/test` | POST | `manage` |
| `/api/integrations/status` | GET | （不挂，读地板） |
| `/api/audit-logs` | GET | `audit` |
| `/api/notification-channels` | GET/POST | `manage` |
| `/api/notification-channels/:id` | PUT/DELETE | `manage` |
| `/api/notification-channels/:id/test` | PUT | `manage` |
| `/api/assets` | POST | `write` |
| `/api/assets/:id` | PUT | `write` |
| `/api/assets/:id` | DELETE | `manage` |
| `/api/assets/:id/retire`、`/restore` | POST | `write` |
| `/api/alert-rules`、`/:id` | POST/PUT | `write` |
| `/api/alert-rules/:id` | DELETE | `manage` |
| `/api/alerts/bulk-ack`、`bulk-resolve` | POST | `write` |
| `/api/alerts/bulk-delete` | POST | `manage` |
| `/api/alerts/:id/ack`、`/:id/resolve`、`/:id/mark-fp` | PUT/POST | `write` |
| `/api/tickets`、`/:id` | POST/PUT | `write` |
| `/api/alert-suppressions`、`/:id` | POST/PUT | `write` |
| `/api/alert-suppressions/:id` | DELETE | `manage` |
| `/api/alert-suppressions/preview` | POST | （不挂：纯计算无副作用，`alert_suppression_handler.go:162-183` 仅 DB 读 + 计算） |
| `/api/oncall/schedules`、`/api/oncall/schedules/:id/shifts`、`/api/oncall/policies` | POST | `write` |
| `/api/oncall/schedules/:id`、`/shifts/:shift_id`、`/policies/:id` | DELETE | `manage` |
| `/api/runbooks`、`/:id` | POST/PUT | `write` |
| `/api/runbooks/:id` | DELETE | `manage` |
| `/api/metric-snapshots` | POST | `write` |
| `/api/diagnostics/ping`、`/traceroute` | GET | `write`（服务端外连，只读身份不应触发） |

**后续修订（2026-09-09，见 `docs/FIX-PLAN-AUTHZ-CLOSURE.md` §2 D-C）**：本表的 `manage` 只解决「哪个**角色**能碰」，
不解决「哪种**身份**能碰」——API Key 的 role 取自关联用户，故 admin 名下的 write Key 同样过 `manage`。
以下端点因此**额外**挂 `middleware.RejectAPIKeyAuth`（只拒 API Key，不拒会话；能力要求不变）：
`/api/notification-channels*`（整组，响应体含明文凭据）、`PUT /api/integrations/{zabbix,netbox,glpi}`（可改出站地址外泄已存凭据）。
`POST /api/integrations/*/test` 与 `/sync` **不拦**——堵住 PUT 后它们只能打管理员配置过的地址，是自动化该用的能力。

**不挂**（保持现状）：

- 所有未列入上表的 `GET` 列表/详情、`/api/dashboard/*`、`/api/topology`、`/api/postmortem/*`、`/api/diagnostics/assets/:id/timeline`、`/api/health`、`/healthz`、`/readyz`、`/metrics`。
- **auth 组自助端点**：`POST /api/auth/login`、`logout`、`PUT /api/auth/password`、`POST /api/auth/skip-password-change` —— 只需认证，不挂能力（否则 `readonly` 连自己的密码都改不了）。
  - 后续修订（2026-09-09，见 `docs/FIX-PLAN-AUTHZ-LEFTOVER.md`）：`POST /auth/skip-password-change` 已移入 `protected` 组（补 AuditLog 留痕）；`PUT /auth/password` 追加 `RejectAPIKeyAuth`（API Key 不得改密）。二者仍**不挂能力**，readonly 自助改密不受影响。
- **导出/下载**：`GET /api/assets/export`、`GET /api/alerts/false-positives/export`、`GET /api/postmortem/assets/:id/report` 属读地板（已核实：导出限 500 行、文件名经 `sanitizeFilename`，无路径穿越）。

---

## 4. 实施

### 4.1 新增 `internal/middleware/roles.go`（单点）

```go
const ( RoleAdmin = "admin"; RoleOpsAdmin = "ops_admin"; RoleOpsUser = "ops_user"
        RoleAuditor = "auditor"; RoleReadonly = "readonly"; RoleUser = "user" )

type Capability string
const ( CapRead Capability = "read"; CapWrite = "write"; CapManage = "manage"
        CapAudit = "audit"; CapIdentity = "identity" )

func CanonicalRole(role string) string      // operator→ops_user, viewer→readonly, 其余原样
func Can(role string, cap Capability) bool  // 内部先 CanonicalRole；矩阵的唯一实现
func Capabilities(role string) []Capability // 给 /auth/me 用，避免前端复制矩阵
func RequireCapability(cap Capability) gin.HandlerFunc
```

**硬约束**：`Can(role, cap) ≡ Can(CanonicalRole(role), cap)`，且 `RequireCapability` 必须走 `Can`（不得自行比较字面量）——否则别名失效时既有测试仍全绿（§6 V-1 专门守这一点）。

`RequireRole` **保留**（仅测试在用：`auth_scope_test.go`、`backend/tests/auth_middleware_test.go`）；已加 `Deprecated` 标记，`routes.go` 不再调用它。

### 4.2 `internal/api/routes.go`

- **17 处** `RequireRole("admin")` → 对应 `RequireCapability(...)`。
- 按 §3.3 **新增约 31 条**路由的能力挂载。
- `GET /notification-channels` 由「不挂」改为 `manage`（§3.2 例外）。

### 4.3 API Key 路由移入审计组

`/api/auth/api-keys` 四条路由当前在 `auth` 组（`routes.go:180-195`），**没有 AuditLog 中间件** → 铸造/吊销长期凭据不留痕。移入 `protected` 组（已挂 AuthMiddleware + RateLimit + AuditLog），路径不变。属于本轮「凭据能力」改动的配套修复。

### 4.4 `/auth/me` 暴露能力集

`GET /api/auth/me` 响应增加 `capabilities: ["read","write",...]`。

**数据源必须与鉴权同源**：用 `c.GetString("role")`（JWT claim / API Key 关联用户角色），**不是** handler 里回查 DB 的 `user.Role`（`auth_handler.go:157-172`）——否则角色变更后 24h 内会出现「按钮隐藏但接口放行」的不一致。前端 UI 改造本轮不做（§7）。

### 4.5 新增 `cmd/set-role`（让矩阵可达）

仿 `cmd/admin-bootstrap` 模式：直连 DB，env 驱动（`SET_ROLE_USERNAME` / `SET_ROLE_ROLE`），校验角色 ∈ §3.1 词表（含 `user`），更新 `users.role` 并同步 `user_roles`（若角色存在于 `roles` 表），全程单事务 + 防自锁。**不经 HTTP**，不新增攻击面。用户角色管理 UI 另立任务（§7）。

### 4.6 词表对齐

| 文件 | 改动 |
|------|------|
| `backend/cmd/seed/main.go:68` | `operator` → `ops_user`（用户名 `operator` 不变，只改角色值） |
| `backend/internal/models/user.go:23` | 注释改为权威词表 |
| `backend/internal/api/openapi.yaml:2395` | `enum: [admin, ops_admin, ops_user, auditor, readonly, user]` |
| `frontend/src/services/api.types.ts:1163` | `npm run gen:api` 生成（不要手改） |
| `frontend/src/types/index.ts:7` | **手写** union，手工同步（`gen:api` 不覆盖此文件） |
| `06-用户权限.md` §6.0.2（`roles` 行取值列表）+ §6.2.2（默认角色表） | 换成 §3.1 + §3.2 |

### 4.7 新增 ADR-0005

记录 §3 的矩阵决策、「read 是地板」「identity 只给 admin」「凭据类端点例外」四条边界，供后续扩展引用。

---

## 5. Risk

**高风险（鉴权变更）→ 失败模式 + 缓解**

| # | 失败模式 | 缓解 |
|---|----------|------|
| 1 | **把自己锁在门外**：`users.role` 为空/未知（未跑 000013、或 JWT 签发早于 role 列）→ 无人具备 `identity`，进不去 `/users`、签不了 API Key | ① `Can` 对未知角色 fail-safe 到只读地板（不会 500）；② `cmd/admin-bootstrap` 直连 DB 可**新建**管理员（注意：同名用户会拒绝，只能换用户名新建）；③ 应用内无用户写接口，修复既有账号角色需直连 DB（`cmd/set-role` 或 SQL）；④ JWT 24h 过期，重新登录即从 DB 取新角色 |
| 2 | **存量 `user` 角色用户被静默降权**：000013 对无 `user_roles` 行的用户兜底为 `'user'`（`000013:301-303`），本轮把 `user` 定义为只读 → 这类用户从「能写一切」变成「只能读」，且无任何提示 | ① 发布前盘点：`SELECT username, role FROM users WHERE role IN ('user','operator','viewer')`；② 需写权限的账号用 `cmd/set-role` 提为 `ops_user`/`ops_admin`；③ 发布说明写明这是**行为变更**（`user` 从「未定义」变为「=readonly」），不是纯归一 |
| 3 | **服务账号被连带收紧**：API Key 的 `role` 取自关联用户（`auth.go:196`），本轮后写操作会被「Key scope（方法级）+ 用户角色能力」**双重拦截** | ① 现状已有该交集（17 条 admin 门禁同样按用户角色判定），本轮只是扩大范围；② 需要写的服务账号请用 `cmd/set-role` 设为 `ops_user`/`ops_admin`；③ 仓库内已核实无 API-Key 调用方（grep `X-API-Key`/`curl` 仅命中设计文档） |
| 4 | **API Key scope 无法表达「GET 但不是读」**：`apiKeyAllows` 只按 HTTP 方法判定（`auth.go:206-225`），`permissions:["read"]` 的 Key 挂在 `ops_user` 账号上仍可触发 `GET /diagnostics/ping` | 本轮如实记录该限制（`write` 门禁**仅对 JWT 身份生效**）；Key scope 能力化另立任务（§7） |
| 5 | **前端 403 无降级**：`readonly` 用户界面上的删除/新建按钮仍可见，点击后 403 弹错（前端只 toast，不白屏，`services/api.ts:39-41`） | 本轮后端只做正确拒绝；`/auth/me` 下发 `capabilities` 已为前端铺路，UI 改造另立任务（§7） |
| 6 | **角色变更生效延迟 ≤ JWT TTL(24h)**：JWT 内嵌 role 且不回查 DB（`auth.go:102-124`） | 与现状一致（D-6 门禁同源）；发布说明写明；JWT 吊销/refresh 属 R-3 独立任务 |
| 7 | **矩阵与路由脱节**：新增路由忘了挂能力 → 静默裸奔 | §6 V-11：用 `r.Routes()` 枚举全部注册路由，与「受控路由 → 能力」声明表做**双向 diff**，未分类的非 GET 路由即失败 |
| 8 | **别名让词表重新分叉**：`CanonicalRole` 悄悄接受任意值 | 只映射 2 个已知别名，其余原样返回；矩阵单测枚举**全部**已知角色 + 未知值，别名行为显式断言 |

---

## 6. 验收标准

| # | 验收项 | 判定方法 |
|---|--------|----------|
| V-1 | 矩阵实现与文档一致 | `Can` 表驱动单测：6 角色 + 2 别名 + 未知/空 × 5 能力全枚举；**并断言 `Can(r,c) == Can(CanonicalRole(r),c)`** |
| V-2 | `ops_admin` 不再被门禁挡住 | 集成测试：`ops_admin` 对 `manage`/`audit` 路由**非 403 且 <500** |
| V-3 | `readonly`/`auditor`/`user` 不能写/删 | 同上：对 `write`/`manage` 路由**逐条 403** |
| V-4 | `auditor` 能读审计日志 | `GET /api/audit-logs` 对 `auditor` **非 403 且 <500** |
| V-5 | `ops_user` 不能删、不能配集成 | 对 `manage`/`identity` 路由**逐条 403** |
| V-6 | 只读端点未被过度收紧 | `readonly` 对 `GET /api/assets`、`/integrations/status` 返回 **200**；`GET /notification-channels` **403**（凭据例外） |
| V-7 | 词表单一（Go） | `grep -rnE 'Role:\s*"operator"\|"role".*"operator"\|case "operator"' backend --include=*.go \| grep -v _test` → 仅命中别名映射与用户名/邮箱字面量；**显式排除** `alert_rules.operator`（比较运算符，无关） |
| V-8 | 现有测试全绿 | `go test ./...` + `npm run lint` + `npx tsc --noEmit` |
| V-9 | 新增代码语句覆盖 100% | `go tool cover -func` 逐函数断言 `CanonicalRole`/`Can`/`Capabilities`/`RequireCapability` + `internal/middleware` 包覆盖率 |
| V-10 | `/auth/me` 下发 capabilities 且与执行侧同源 | 集成测试：每个角色登录后 `capabilities` 集合 == `Capabilities(role)` |
| V-11 | 路由清单双向 diff（兜住 Risk#7） | `TestRoutes_所有路由都已分类` 枚举 `r.Routes()` 的**全部**路由（含 GET），既不在 `gatedRoutes` 也不在 `ungatedRoutes` 白名单 → 失败 |
| V-12 | `cmd/set-role` 可用 | 单测：合法角色写入成功、非法角色报错、幂等 |

---

## 7. 明确不做

| 不做 | 理由 |
|------|------|
| 前端按钮级隐藏 / 403 降级 | 后端已下发 `capabilities`，UI 改造是独立前端任务；避免把鉴权修复变成前端重构 |
| **用户角色管理 UI（`PUT /users/:id/role`）** | 需要校验、审计、防自锁设计，是功能开发；本轮用 `cmd/set-role` 保证可达 |
| **通知渠道 `config` 脱敏** | 会改变前端 Settings 的编辑契约（需「留空表示不修改」），属独立任务；本轮用 `manage` 收口 |
| **API Key scope 能力化**（read/write → 细粒度能力） | 另一量级改造；本轮如实记录限制（Risk#4） |
| 用 `permissions` / `role_permissions` 表做运行时授权 | 这两张表仅由迁移 000001 种子写入、`user_roles` 由 `admin-bootstrap` 写入，三者运行时**零读取**（已 grep 核实）；表驱动授权另立任务 |
| 新增迁移清洗 `operator`/`viewer` 存量数据 | 只在 dev 种子数据里出现，别名已保证行为正确；改数据需新迁移，收益不抵风险 |
| 给 `user` 角色补 `roles` 表行 | `user` 是 000013 兜底值，不在 `roles` 表内属已知设计（§3.1 已标注） |
| ~~**前端 6 个页面 token 路径失效**~~ | **已于 2026-09-09 修复**，见 `docs/FIX-PLAN-FRONTEND-TOKEN.md`（收敛为共享 `apiGet`/`apiSend` + 修 `/api/v1` 前缀 + 拦截器 204/blob 容错 + ESLint 守卫） |
| JWT 吊销 / refresh（R-3） | 独立任务 |
| gRPC 鉴权（R-1） | 按 ADR-0003 冻结，需用户决策 |
