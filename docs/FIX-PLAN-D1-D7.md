# 缺陷修复需求文档：D-1 ~ D-7（schema 漂移 + 鉴权缺口）

> 状态: **draft（待审查）**
> 日期: 2026-09-09
> 基线: `main` @ 7e3295b（文档轮已推送）
> 依据: `docs/v3-架构优化需求.md` §9 缺陷清单
> 关联: [ADR-0003](adr/0003-grpc-作废与三层定位.md)、[ADR-0004](adr/0004-工单SoT决策.md)

---

## 0. 一句话

**迁移 DDL 与 GORM 模型是两个平行宇宙**：模型是代码实际执行的 schema，迁移是生产建库的 schema，两边各自演化、交集之外互不覆盖，而 CI 没有 Postgres，所以漂移长期不可见。本轮用**一个补齐式迁移 + 少量代码修复**把它们对齐，并**补上真库冒烟测试**防止再次漂移。

---

## 1. 问题范围（实测）

| # | 缺陷 | 严重度 | 本质 |
|---|------|--------|------|
| D-1 | `tickets` 三套 schema 不一致（迁移 24 列 / 模型 24 字段，交集 10） | 阻断级 | 列漂移 |
| D-4 | `users` 缺 `role` / `deleted_at` | 阻断级 | 列漂移 |
| D-5 | `audit_logs` 列名漂移（`method`/`path`/`ip`/`status` vs `event_type`/`ip_address`/`result`） | 高 | 列漂移 |
| D-2 | `generateTicketNumber` 用全表 `Count()%26` → 撞号 | 高 | 代码逻辑 |
| D-6 | `RequireRole` 定义后无路由调用 | 高 | 鉴权未挂载 |
| D-7 | API Key 的 `permissions` 不校验 | 中 | 鉴权未执行 |
| D-3 | `alerts.ticket_id` 悬空（无写入方） | 中 | 功能缺口 |

> 全量漂移清单由审计任务产出，见 §3.1；**本文档的迁移清单以审计结果为准**。

---

## 2. 修复方向决策

### 2.1 方案对比

| 方案 | 做法 | 优点 | 缺点 | 结论 |
|------|------|------|------|------|
| **A. 补齐式迁移** | 新增 `000013_schema_align.up.sql`：`ADD COLUMN` 补模型列、`DROP NOT NULL` 放宽模型不写的列、补唯一/普通索引；旧列保留 | 非破坏；对空库与有数据的库都安全；不动已应用的迁移 | 表里会同时存在旧列与新列（文档标注 deprecated） | ✅ **采纳** |
| B. 重写 `000001_init.up.sql` | 让初始迁移直接等于模型 | 干净 | 已应用的库不会重跑；runner 无 checksum 不报错 → **静默不一致**；破坏可追溯性 | ❌ |
| C. 生产改用 `AutoMigrate` | 删掉迁移路径 | 与模型天然一致 | 无版本控制、无回滚、无审计；线上 DDL 不可预期 | ❌ |

**选 A 的理由**：`internal/migrate` 用 `schema_migrations(version, applied_at)` 记录，**没有 checksum**（`backend/internal/migrate/migrate.go:3-4`）。这意味着改老迁移**不会被发现**，只会让已部署的库与新库悄悄分叉——正是本轮要消灭的东西。

### 2.2 为什么不是「改模型去迁就迁移」

- 迁移里的 `tickets.ticket_no`、`alerts.level/title/message`、`audit_logs.event_type` 等列**没有任何 Go 代码写入**，而模型里的列是**全链路在用**的。
- 改模型 = 改 API 契约（JSON 字段名、前端消费），爆炸半径远大于加列。
- 因此方向固定为：**迁移向模型对齐**。

---

## 3. 逐项修复方案

### 3.1 D-1 / D-4 / D-5：补齐式迁移

**迁移文件**：`backend/migrations/000013_schema_align.up.sql` + `.down.sql`（13 张表，2026-09-09 审计后二次修订）

五类操作，全部幂等：

| 操作 | 用途 | 实例 |
|------|------|------|
| `RENAME` 表/列（`DO` + `to_regclass`/`information_schema` 守卫） | 同一概念、叫法不同 → 保数据、保外键 | `idc→sites`、`asset_network→asset_networks`、`tickets.ticket_no→ticket_number`、`tickets.creator_id→requester_id`、`assets.asset_name→name`、`assets/racks.idc_id→site_id`、`alert_rules.enabled→is_enabled`、`notification_channels.channel_type→type`、`audit_logs.timestamp→created_at` |
| `ADD COLUMN IF NOT EXISTS` | 补模型需要、迁移缺的列 | `users.role`、`users.deleted_at`、`tickets.tags/source/due_date` 等 |
| `ALTER COLUMN ... DROP NOT NULL` | 放宽「迁移有 NOT NULL、模型不写」的列 | `tickets.requester_id`、`alerts.level/title`、`alert_rules.metric_name/level`、`audit_logs.event_type`；**并补**「旧列与新列并存」时旧列的 DROP NOT NULL（审计 中-4） |
| 类型转换（`DO` 按当前类型守卫） | 模型按字符串/JSON 写，原列是数组/jsonb/inet | `api_keys.permissions/ip_whitelist`、`alert_rules.notify_users/notify_channels`、`notification_channels.config`、`users.last_login_ip`、`asset_networks` 的 3 个 inet 列 + `connected_to` |
| `CREATE [UNIQUE] INDEX IF NOT EXISTS` | 补模型声明的索引 | `tickets(ticket_number)` UNIQUE、`users(role)`、`users(deleted_at)` 等 |

**数据回填**：

```sql
-- users.role ← user_roles/roles（cmd/admin-bootstrap/main.go:105 写这两张表）
-- 顺序必须是：无 DEFAULT 加列 → 回填 → 兜底 'user' → 最后才 SET DEFAULT。
-- 见下方「B-1」——先带 DEFAULT 加列会让既有行直接读出 'user'，回填恒不命中。
UPDATE users u SET role = r.code
FROM user_roles ur JOIN roles r ON r.id = ur.role_id
WHERE ur.user_id = u.id AND u.role IS DISTINCT FROM r.code;
UPDATE users SET role = 'user' WHERE role IS NULL OR role = '';
```

> `tickets.ticket_number` **不需要回填**：000013 走 `RENAME COLUMN ticket_no`，数据随列名一起迁移。

**不做**：不 DROP 任何旧列（破坏性；留待 v4 大版本，且需先确认无代码引用）。

**Risk（schema 变更，高风险 → 3 个失败模式 + 缓解）**：

| # | 失败模式 | 缓解（已落地） |
|---|----------|------|
| 1 | **存量 admin 被降级锁在门外**（B-1，实测复现过）：`ADD COLUMN role ... DEFAULT 'user'` 在 PG 11+ 是快默认，既有行直接读出 `'user'`，`WHERE role IS NULL` 恒不命中 | 改为「无 DEFAULT 加列 → 回填 → 兜底 → 再 SET DEFAULT」；`TestDBSmoke_UpgradePath` 守住（已做反证：用旧写法跑该用例会 FAIL，`legacy_admin.role=user`） |
| 2 | **迁移不可重入，重放静默损坏数据**：`USING to_json(x)::text` 在列已是 TEXT 时再套一层引号（`["read"]` → `"[""read""]"`），API Key 认证全挂；触发窗口是「DDL 已提交、版本未记录」 | 两处修复：① 类型转换全部按 `information_schema.data_type` 守卫；② `migrate.Up` 的版本 INSERT 挪进**同一个事务**（`migrate.go`）；`TestDBSmoke_MigrationReapply` 守住（已做反证：去掉守卫该用例 FAIL） |
| 3 | **锁表时间**：`ALTER COLUMN DROP NOT NULL` / 类型转换需全表扫描，大表长时间持 `ACCESS EXCLUSIVE` | 本轮 13 张表规模有限；迁移在启动时执行且有 `pg_advisory_lock` 互斥。上线前在真库副本计时，>5s 就拆多步 |

### 3.2 D-2：工单号按当日序号 + 冲突重试

**修复前现状**（`backend/internal/models/ticket.go:54-58`，现已改为 `generateTicketNumber`）：

```go
db.Model(&Ticket{}).Count(&count)              // 全表计数，与日期无关
return "TICKET-" + time.Now().Format("20060102") + "-" + string(rune('A'+count%26))
```

→ 同一天第 27 张与第 1 张同号；`ticket_number` 有唯一索引 → 插入失败。

**修复**：
1. 计数限定当日：`WHERE ticket_number LIKE 'TICKET-<今日>-%'`。
2. 后缀支持进位：A…Z → AA…（`seqLabel(n)`）。
3. `ticketService.Create` 对唯一冲突**重试**（最多 5 次，每次重新生成号），彻底消除竞态。

**Risk**：

| 失败模式 | 缓解 |
|---|---|
| 并发下两个请求算出同一序号 | 唯一索引兜底 + 服务层重试。**注意**：序号来自「当天已存在行数」，若撞号来自被硬删除的行，重试会重算出同一个号，5 次后返回 `ErrAlreadyExists` —— 这是「重试缓解竞态」而非「保证成功」 |
| 重试掩盖了真实错误（非唯一冲突也重试） | 只在 `isUniqueViolation(err)` 时重试，其余立即返回 |
| **静默改写调用方指定的工单号**（审计 中-11）：客户端显式传 `ticket_number` 时，原实现会把冲突号悄悄换成自动号并返回成功 | `Create` 区分「客户端自带号」与「自动生成号」：前者冲突直接返回 `ErrAlreadyExists`(409)，不重试 |

### 3.3 D-6：挂载 `RequireRole`

**现状**：`RequireRole` 只被测试引用。

**修复（最小可用）**：只挂 **admin 专属**路由，不做 operator/readonly 细分。**实际挂载 17 条**（`internal/api/routes.go`，逐条由 `TestRoutes_D6_*` 覆盖）：

| 挂载点 | 方法 | 理由 |
|--------|------|------|
| `/users` | GET | 越权可直接改他人密码/角色 |
| `/auth/api-keys`、`/:id`、`/:id/revoke` | GET/POST/DELETE/PUT | 签发凭据 |
| `/integrations/sync`、`/{zabbix,netbox,glpi}`、`/{...}/test` | POST/PUT | 含 token 且服务端主动外连（可被用作 SSRF 探测） |
| `/audit-logs` | GET | 含用户名/IP/操作轨迹，只读用户也能拉全量 |
| `/notification-channels`（写/删/测试） | POST/PUT/DELETE | 含 webhook token / SMTP 凭据，测试端点会外连 |
| **只读列表不限**：`/integrations/status`、`/notification-channels` GET | GET | 防过度收紧 |

> **未挂载**：其余 7 条资源 `DELETE`（assets / alert rules / suppressions / oncall / runbooks）。它们确属破坏性操作，但当前角色词表是 `admin/ops_admin/ops_user/readonly/auditor`（DB 种子），而 `RequireRole("admin")` 只认 `admin` —— 直接挂会把 `ops_admin` 一起挡在门外。需先做角色词表归一 + 权限矩阵，另立任务（见 §7）。

**前置条件**：`users.role` 必须已存在且**已回填**（§3.1）——否则 admin 自己也是 `user`，会被自己的门禁挡住。

**Risk（鉴权变更，高风险 → 2 个失败模式 + 缓解）**：

| # | 失败模式 | 缓解 |
|---|----------|------|
| 1 | **把自己锁在门外**：回填失败/角色为空 → admin 访问 `/users` 403，无法自救 | 迁移里 `role` 有 `DEFAULT 'user'` 不会为 NULL；回填从 `user_roles` 取 `admin`；**上线顺序**：先发迁移，确认 `SELECT count(*) FROM users WHERE role='admin'` > 0，再挂门禁（同一次发布内由迁移先跑保证） |
| 2 | **前端页面报错**：非 admin 用户打开 Settings 触发 `/users` 403 | 前端对 403 做静默降级（本项归前端任务，本轮先记录）；后端只做正确拒绝 |

### 3.4 D-7：校验 API Key 的 `permissions`

**现状**：`AuthMiddleware` 的 API Key 分支只按关联用户的 role 放行（修复前 `middleware/auth.go:160-183`，现为 `handleAPIKeyAuth`），`key.Permissions` 从不读取。前端可选值固定为 `read` / `write` / `admin`（`frontend/src/pages/Settings.tsx:907-909`），默认 `['read']`。

**修复（方法级 scope）**：

| 请求 | 要求 |
|------|------|
| `GET` / `HEAD` / `OPTIONS` | `permissions` 含 `read`、`write` 或 `admin` |
| 其它方法 | 含 `write` 或 `admin` |
| `permissions` 为空 | 视为 `read`（fail-safe，不放行写） |

**兼容性影响**：现有 `['read']` 的 Key 将**不能再写**——这正是 R1 想要的效果（HolmesGPT 只读）。需在发布说明里写明。

**Risk**：

| 失败模式 | 缓解 |
|---|---|
| 现有集成用 read Key 做写操作 → 发布后 403 | 发布前 `SELECT name, permissions FROM api_keys` 盘点；有写需求的重签 `write` Key |
| 自建前端/脚本绕过（直接用 JWT） | JWT 分支不受影响（用户级权限），scope 只管 API Key；符合设计 |

### 3.5 D-3：`alerts.ticket_id` 指向本系统工单

**现状**：字段存在、注释写「GLPI 工单」、**无写入方**。

**修复**：随「告警 → 一键建单」实现，`ticket_id` 指向 `tickets.id`；注释同步改为「ITmanager 工单」。

**本轮范围**：只做**字段语义修正 + 注释**；一键建单功能另立任务（避免把缺陷修复轮变成功能开发轮）。

---

### 3.6 审计轮修复（R2，2026-09-09）

对 000013 + 本轮代码做迁移正确性 / 鉴权安全 / 测试与一致性三路审计，结论与处置：

| # | 级别 | 发现 | 处置 |
|---|------|------|------|
| B-1 | 阻断 | `role` 带 DEFAULT 加列 → 存量 admin 被降级 | 已修（§3.1 Risk#1），冒烟 + 反证 |
| B-2 | 阻断 | 类型转换不可重入（重放二次编码，数据损坏） | 已修（§3.1 Risk#2），冒烟 + 反证 |
| B-3 | 阻断 | `down.sql` 无条件 `DROP tickets.ticket_type`（000001 就有该列） | 已删该行；`TestDBSmoke_DownPreservesLegacyColumns` 守住 |
| M-1 | 中 | `inet::text` 走 `network_show` → `10.1.2.3/32`（同文件里 `to_json(inet[])` 却是裸地址） | 改用 `host()`；`TestDBSmoke_TypeConvertedModels` 断言往返无损 |
| M-2 | 中 | 版本记录在迁移事务之外 → 重放窗口 | 版本 INSERT/DELETE 移入 `runInTx` 的同一事务 |
| M-3 | 中 | `pg_try_advisory_lock` 与 `pg_advisory_unlock` 可能落在池里不同连接 → 锁泄漏 | 改为独占 `*sql.Conn` 加解锁 |
| M-4 | 中 | 旧列与新列并存时 RENAME 被跳过，旧列 NOT NULL 仍在 → INSERT 仍失败 | 补「两列都存在则 DROP NOT NULL」的守卫 |
| M-5 | 中 | TimescaleDB 判据只看 `pg_available_extensions` → 没预加载时 `migrate.Up` 必挂、服务起不来 | 追加 `shared_preload_libraries` 判据 |
| M-6 | 中 | `postmortem_service` / `asset_service` 用列名 `ipv_address`，真实列是 `ipv6_address`（GORM 由字段名推导；`ipv_address` 只是 JSON tag）→ 退役流程与复盘查询运行时必失败，sqlmock 把列名一起伪造了所以测试没暴露 | 已改为 `ipv6_address`；`TestDBSmoke_TypeConvertedModels` 用真库覆盖 |
| M-7 | 中 | 测试假绿：重试用例只断言 `NoError`；D-6 admin 侧只抽检 2 条路由；弱断言 `!=403` 放行 500 | 已加强（断言重算后的号、admin 侧覆盖全部 17 条、只读端点断言 200） |
| M-8 | 中 | 漂移守门测试不解析 `DROP COLUMN` | 已补 + 新增解析器自测 |

**未采纳 / 另立任务**：

| # | 发现 | 理由 |
|---|------|------|
| B-2（鉴权） | gRPC :50051 无鉴权 | 按 ADR-0003「保留但冻结」，改动需先定 gRPC 暴露面，**需用户决策** |
| M-9 | `schema_drift_test` 不校验类型/NULL/唯一约束、`liveModels()` 是手工清单 | 真库类型漂移已由 `db_smoke` 覆盖；解析器升级另立任务（§7） |
| M-10 | `internal/api/testdata/migrations/` 与生产迁移已分叉（编号错位、缺 000005-000013） | 集成测试用 sqlite 兼容 schema，结构性改造另立任务（§7） |

---

## 4. 验收标准与实测结果

| # | 验收项 | 判定方法 | 结果 |
|---|--------|----------|------|
| V-1 | 用迁移建的库能登录 | `scripts/db_smoke.sh` 登录断言 | ✅ 通过 |
| V-2 | 用迁移建的库能写审计日志 | 同上 | ✅ 通过 |
| V-3 | 用迁移建的库能建工单 | 同上 | ✅ 通过 |
| V-4 | 工单号不再撞号 | 单测连续 30 张，号码互不相同 | ✅ 通过 |
| V-5 | 非 admin 访问 admin 路由 403 | `TestRoutes_D6_非admin被拒`（17 条） | ✅ 通过 |
| V-6 | read Key 写操作 403、write Key 放行 | middleware 单测 | ✅ 通过 |
| V-7 | 现有测试全绿 | `go test ./...` | ✅ 通过 |
| V-8 | 新增代码覆盖 ≥80% | `go test -coverprofile` + `go tool cover -func` | ✅ 新增函数均 100% 语句覆盖；`middleware` 包 89.2%、`service` 83.3%。**口径说明**：Go 只有语句/块覆盖（`-covermode=set/count/atomic`），无分支覆盖工具，故关键分支另用表驱动用例显式枚举 |
| V-9 | 迁移执行器在空库跑通 | `TestDBSmoke_MigrateRunner`（走 `migrate.Up`，非 psql） | ✅ 13 个迁移全应用 |
| V-10 | 存量升级路径 role 回填 | `TestDBSmoke_UpgradePath` + 反证 | ✅ `legacy_admin=admin`；旧写法跑必 FAIL |
| V-11 | 迁移可重入（重放不损坏数据） | `TestDBSmoke_MigrationReapply` + 反证 | ✅ 通过；去掉类型守卫必 FAIL |
| V-12 | 类型转换往返无损（无 `/32`、无二次编码） | `TestDBSmoke_TypeConvertedModels` | ✅ 通过 |
| V-13 | `ticket_number` 唯一约束在位 | `TestDBSmoke_TicketNumberUnique` | ✅ 通过 |
| V-14 | 回滚不丢 000001 的列 | `TestDBSmoke_DownPreservesLegacyColumns` | ✅ 通过 |

**验证方式**：`scripts/db_smoke.sh` 起临时 PG 18 容器，跑两条路径（空库全新安装 / 存量库 000001~000012 后只应用 000013 + 回滚）。CI 已加 `dbsmoke` job（GitHub Actions postgres service，脚本走 `SMOKE_PG_HOST` 外部模式）。

---

## 5. 上线顺序（关键）

```
1) 迁移前快照：SELECT * FROM schema_migrations ORDER BY version;   ← 确认最高版本与预期一致
                  SELECT username, role FROM users WHERE role IN ('admin','ops_admin');  ← 记下管理员
2) 000013 迁移（含 role 回填）   ← 必须先于门禁生效
3) 迁移后校验：SELECT count(*) FROM users WHERE role = 'admin';    ← 必须 > 0，否则回滚
                SELECT count(*) FROM api_keys WHERE permissions NOT LIKE '[%';  ← 无残留
4) 代码修复 D-2 / D-6 / D-7
5) 真库冒烟 V-1~V-3（或直接跑 scripts/db_smoke.sh）
6) 盘点 api_keys.permissions（D-7 影响面：现有 read Key 将不能写）
7) 提交推送
```

> 若第 3 步 `role='admin'` 为 0：**不要**发布门禁代码。先人工 `UPDATE users SET role='admin' WHERE username='<管理员>'`，再继续。仓库内没有提升角色的接口/CLI（这也是 B-1 危险的原因）。

## 6. 明确不做

| 不做 | 理由 |
|------|------|
| DROP 迁移里的旧列 | 破坏性；需先确认零引用，留待 v4 |
| 重写 `000001_init.up.sql` 的既有 DDL | 无 checksum，改了对已部署库静默不一致（只加了 TimescaleDB 预加载判据这一处守卫） |
| operator/readonly 的端点级细分 | 本轮只解决「RBAC 完全没挂」；细分需权限矩阵设计，另立任务 |
| 告警一键建单功能 | D-3 只修字段语义 |
| 前端 403 降级 | 记录，归前端任务 |

---

## 7. 已知风险与后续任务

| # | 风险 / 任务 | 影响 | 状态 |
|---|------------|------|------|
| R-1 | **gRPC :50051 无鉴权**（审计 B-2） | 若该端口对外可达，绕过 HTTP 鉴权 | 按 ADR-0003「保留但冻结」；**需用户决策**（是否暴露 / 是否加 mTLS） |
| R-2 | **角色词表漂移**：DB 种子是 `admin/ops_admin/ops_user/readonly/auditor`，`RequireRole("admin")` 只认 `admin` | `ops_admin` 被 D-6 门禁挡在门外 | 本轮按「只挂 admin」发布，已在 §3.3 写明；归一化 + 权限矩阵另立任务 |
| R-3 | **JWT 有效期 24h**，无 refresh / 无吊销 | 用户被禁用后旧 token 仍可用满 24h（API Key 已按 M-5 即时失效） | 另立任务 |
| R-4 | `schema_drift_test` 不校验类型 / NULL / 唯一约束，`liveModels()` 是手工清单 | 新增模型默认不守门 | 真库类型漂移已由 `db_smoke` 覆盖；解析器升级另立任务 |
| R-5 | `internal/api/testdata/migrations/` 与生产迁移分叉（编号错位、缺 000005~000013） | 集成测试通过 ≠ 生产库可用 | 结构性改造另立任务 |
| R-6 | `verifyAPIKey`（`middleware/auth.go`）无调用者、0% 覆盖 | 死代码 | 按约定只提不删，留待清理 |
| R-7 | `postmortem_service` / `asset_service` 的 `ipv_address` 已修，但同类「代码枚举不存在的列」可能还有 | 运行时 500 | 已加真库往返断言；建议后续用 db_smoke 覆盖更多模型 |
| R-8 | 迁移大表锁（`DROP NOT NULL` / 类型转换需全表扫描） | 上线窗口内 `ACCESS EXCLUSIVE` | 上线前在真库副本计时；>5s 拆多步 |
