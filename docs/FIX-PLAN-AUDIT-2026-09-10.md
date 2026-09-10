# FIX-PLAN: 2026-09-10 独立代码审查遗留问题

**来源**:本会话 4 angle 主代理 + 1 个 leaf subagent(angle 5)的审计报告
**审查范围**:9/9-9/10 期间 90 commit / 545 file / 24179+ / 3987-(HEAD `1609d4a`,origin/main)
**方法论**:`independent-code-review` v1.2.0 的 5 angle(API 契约 / 错误路径 / 安全 / 升级风险 / 测试质量)+ `codegraph_explore` + `git blame` 验证

**主审计 verdict**:**REQUEST CHANGES** — 0 P0 阻断 / 4 P1 必修 / 5 P2 推荐 / 3 Pre-existing

**12 项 Confirmed-correct 未列入本文**(已通过验证,不属于遗留问题);完整审计报告见本会话历史。

---

## P1 — Fix before merge(4 项)

### P1-1 ⚠️ `diagnostics` GET ping/traceroute 误挂 `canWrite` 守护 — 9/9 AUTHZ 引入的回归

- **位置**:`backend/internal/api/routes.go:380-381`
- **证据**:`git blame` 确认 `b5c46fda` (Yan 9/9 07:01:38) 引入。`DiagnosticHandler.PingAsset`(`backend/internal/api/handlers/diagnostic_handler.go:96-128`) 内部纯读 — `host` + `count` 参数,调 `h.svc.PingAsset` 走 ICMP,**不写 DB**。
- **影响**:`readonly` / `auditor` 角色(主人 9/9 刻意保留的"只读"和"审计"角色)**完全无法 ping 自己的网络**。违反"GET 不改状态应给 CapRead"原则。
- **修复**(2 行):改为不加 cap(`CapRead` 默认放行),或显式 `middleware.RequireCapability(middleware.CapRead)`。**建议不加**,语义更清晰。
- **验证**:改后跑 `TestRoutes_能力矩阵_无权限被拒` — 改前 `RoleReadonly` 应仍能 ping;改后仍能。两者差异应在 commit body 注释清楚(避免下一次又加错 cap)。
- **预估**:5 分钟

### P1-2 ⚠️ `internal/api/testdata/migrations/` 缺 14 个迁移(含 9/9-9/10 全部 9 个新索引) — 路由集成测试跑的是陈旧 schema

- **位置**:`backend/internal/api/testdata/migrations/` 目录(实有 7 个:`000001/2/3/4/8/9/12`)vs `backend/migrations/`(实有 21 个)
- **缺失清单**:`000005/6/7/10/11/13/14/15/16/17/18/19/20/21`(14 个 — 不只是 9 个新索引,还包括 5 个之前就缺的)
- **证据**:
  - `backend/internal/api/routes_integration_test.go:86` `//go:embed testdata/migrations/*.sql` 只 embed 7 个老迁移
  - `routes_integration_test.go:1143` 注释自承"testdata 的 assets 表没有 brand 列" — 已知的 schema drift(000013 加的列)
  - `backend/tests/db_smoke_test.go` 真库冒烟(build tag `dbsmoke`)有 000013/000014/000015 守门,**但没有 000016-000021 的 6 个新索引冒烟**
- **影响**:
  - 9/9-9/10 期间 7 个 W6 后端索引 commit(000016-000021)+ 2 个 schema 迁移(000013/000015)的 **CREATE INDEX 语法、column 引用、partial where 子句、Concurrent 语法**在 CI 里**完全没被验证**
  - `routes_integration_test.go` 跑出来的 9 角色 × ~51 受控路由矩阵(`TestRoutes_能力矩阵_无权限被拒` lines 723-741)是**在陈旧 schema 上**测的
- **修复**(2 处):
  1. 把 `backend/migrations/000013-000021` 复制/软链到 `internal/api/testdata/migrations/`
  2. `backend/tests/db_smoke_test.go` 加 000016-000021 的 6 个新索引守门(对照 000015 的写法,加 `pg_indexes` assertion)
- **预估**:1-2 小时

### P1-3 `user` 兜底角色(000013 引入)无 capabilities 约定文档

- **位置**:`backend/internal/middleware/roles.go:11-22` 注释,`backend/cmd/set-role/main.go:125-128` 实现
- **证据**:`Capabilities(RoleUser)` 走 `Can("user", CapRead)` 返 true,其他 cap 返 false — **实际是只读地板**。但 `cmd/set-role` 允许把用户设成 `user` 角色。**两套角色定义来源**(roles 表 vs users.role)开始漂移。
- **影响**:新写的代码如果调 `Can(RoleUser, CapWrite)` 直接 false,可能误判"user 角色不存在"或写成 `if role == admin` 的硬编码(注释自承无自动测试兜底)。
- **修复**:在 `roles.go:11` 注释加"**所有引用角色必须走 `Can()` / `IsKnownRole()` / `CanonicalRole()` 这三个函数,禁止直接比较字面量**"。
- **预估**:15 分钟(纯文档)

### P1-4 frontend `types/index.ts:9` User.role union 手维护,与 openapi.yaml 生成的 `api.types.ts` 必定漂移

- **位置**:`frontend/src/types/index.ts:9` vs `frontend/src/services/api.types.ts`
- **证据**:`types/index.ts:7-11` 注释说"权威词表见 docs/FIX-PLAN-AUTHZ.md §3.1" — 但 union 是手维护的。`apiClient.ts:101-112` 自己也承认"v3.0 大版本迁移目标"。
- **影响**:9/9 加新角色时已经需要**手改 4 处**(`types/index.ts` + `api.types.ts` + `roles.go` + `routes_integration_test.go` matrixRoles) — 4-of-N 错位概率。
- **修复**(短期 P2 也行):
  1. 写 `tools/sync-roles-from-openapi.ts` 跑 CI,union 不匹配时 fail
  2. 或者删 `types/index.ts:9` 的手 union,全部 `import type { User } from '../services/apiClient'`
- **预估**:2-4 小时(含 CI 集成)

---

## P2 — Nice-to-have(5 项,可独立排期)

### P2-1 `/integrations/status` 无任何 cap 守护 + 返回含凭据存在性

- **位置**:`backend/internal/api/routes.go:269` (git blame 6/15, **pre-existing**)+ `backend/internal/api/handlers/integration_handler.go:91-115`
- **影响**:`readonly` 角色能看到 Zabbix/NetBox/GLPI 的 password/token 是否被设置 — token 存在性的**旁路泄露**(虽然密码本身不返回,boolean 足以判断系统启用状态)
- **建议**:`canManage` 守护;或在 status 响应体里彻底不返回 `has_token` / `has_password` / `url` 字段
- **预估**:15 分钟

### P2-2 `metrics.GET` (routes.go:439-440) 无任何 cap 守护

- **位置**:`backend/internal/api/routes.go:439-440`
- **影响**:任何已认证身份(含 `user` 兜底)能查所有指标 — 跨租户可见
- **建议**:显式 `canRead` 守护,语义上更明确
- **预估**:5 分钟

### P2-3 `apierr.ErrorResponse.TraceID` 字段永远空

- **位置**:`backend/internal/apierr/apierr.go:17-21` 字段定义有 `omitempty`,但 `Respond` 函数从未填
- **影响**:frontend `apiClient.ts:84` `trace_id?: string` 类型化但永远拿不到,日志链路断
- **建议**:在 middleware 里 `c.Set("trace_id", uuid.NewString())` 然后 `Respond` 读出来填上;不填就删字段
- **预估**:1 小时(加 request_id middleware)

### P2-4 `b5c46fd` AUTHZ commit body 撒谎

- **证据**:commit 说"17 处 RequireRole 全部替换为 RequireCapability" — 实际 `routes.go:285` 仍有一行 `// 修复前挂 RequireRole("admin")` 注释
- **建议**:不修这个 commit(已合);但加 lint/grep 规则:`rg 'RequireRole\(' backend/internal/api/routes.go` 应该返回 0 行
- **预估**:30 分钟(CI 规则)

### P2-5 `routes_integration_test.go:1118-1120` `TestRoutes_所有路由都已分类` 用 method 跳过 HEAD/OPTIONS

- **位置**:`backend/internal/api/routes_integration_test.go:1118-1120`
- **影响**:未来 HEAD handler 的 admin-only 守护不会被这个测试发现
- **建议**:改成按 source 跳过(gin internals 排除)而非按 method
- **预估**:30 分钟

---

## Pre-existing(不在 9/9-9/10 引入,但被 diff 暴露;3 项)

### Pre-1 `/integrations/status` 凭据存在性泄露(同 P2-1)— 6/15 引入

### Pre-2 `apierr.ErrorResponse.TraceID` 永远空(同 P2-3)— 8 月引入

### Pre-3 `routes_integration_test.go:122-126` Cleanup race

- `database.SetDBForTest(oldDB)` + `sqlDB.Close()` 都在 `t.Cleanup`,顺序无保证。可能让 `database.DB` 指向已关闭的 handle 给下一个测试。
- **建议**:改成显式 defer,顺序 `defer sqlDB.Close()` 在 `t.Cleanup` 之前
- **预估**:15 分钟

---

## 不进本文的 Confirmed-correct(12 项)

完整审计见本会话历史。12 项已主动验证通过的反证/正向测试:

**AUTHZ 大改**: `roles_test.go:51-60` 矩阵全枚举 / `roles_test.go:171-196` 放行与拒绝 / `roles_test.go:199-213` 拒绝时不继续 / `routes_integration_test.go:723-741` 能力矩阵无权限被拒 / `routes_integration_test.go:745-766` 有权限放行 5xx 守门 / `routes_integration_test.go:1108-1128` 所有路由都已分类 / `routes_integration_test.go:406-453` OpenAPI 无幻影路径 / `auth_scope_test.go:422-458` RejectAPIKeyAuth / `auth_scope_test.go:207-234` RequireRole Deprecated 文档化

**通知 sender**: `notification/notification_test.go:431-450` MarkFailed 脱敏按 rune 截断 / `:454-469` 非法 UTF8 清理 / `:474-493` 控制字符剥离 / `sender_sign_resp_test.go:38-82` Python 衍生签名向量 / `sender_wechat_test.go:25-51` body 形状与回执校验 / `notification_test.go:358-377` 发送失败不泄漏 URL 凭据

**审计/限流/路由护城河**: `routes_integration_test.go:991-1036` 登录尝试留审计 / `:1043-1063` 限流不写审计 / `:905-984` APIKey 不能读写凭据

**迁移工程**: `db_smoke_test.go` 真库冒烟(build tag `dbsmoke`)守 000013/000014/000015 schema 演化 + 回填正确性 + 可重入

**路由配置**: `routes.go:261-264` API Key 4 路由 `canIdentity` 守护 / `routes.go:277/279/281` 集成 config update 双重守护 `canManage` + `RejectAPIKeyAuth()`

---

## 跟踪与关闭流程

1. 创建 branch:`fix/audit-2026-09-10`(从 `main` 当前 `1609d4a` 拉)
2. **P1-1 → P1-2 → P1-3 → P1-4** 顺序修(按"对当前生产风险"排序)
3. 每个 P1 一个 commit,P2 可合并到一个 commit 或独立
4. Pre-existing 不必这次修(单独 follow-up issue)
5. 关闭本文档:把所有 `[ ]` 改成 `[x]`,加 commit SHA 引用,最后在本文末尾加 "**Closed 2026-MM-DD**" + "All 4 P1 + 5 P2 fixed in <commit list>"

---

## Closed 2026-09-10 (Pulled & Verified)

主人 9/10 12:25-12:30 间合并 5 个 commit,ff-merge 到 `d1dc8a4` 后验证全过。

### 已修(5/9 + 1 搁置)

- [x] **P1-1** `routes.go:380-381` 误挂 canWrite → `b392b61` + `routes_integration_test.go:1147+` 加只读端点未过度收紧断言
  - **验证**:`TestRoutes_只读端点未被过度收紧` 10/10 subtests PASS,`/api/diagnostics/ping` 返回 400(无 host)**不是 403** —— readonly 角色能 ping
- [x] **P1-3** 角色词表注释 → `98a5fe8` (4 行纯文档)
- [x] **P1-4** User 类型单源 → `d1dc8a4` (apiClient + types + 编译期断言 `UserSourceOK` 钉住同型)
  - **验证**:`tsc --noEmit` 0 error + `eslint --max-warnings 0` 0 警告
- [x] **P2-1** `/integrations/status` 凭据存在性按 cap 区分 → `8aff762` + 7 角色矩阵反证测试
  - **验证**:`TestIntegrationStatus_凭据存在性仅canManage可见` admin/ops_admin/ops_user/auditor/readonly/user/空角色 7/7 PASS
- [x] **P2-3** apierr TraceID 死字段 → `008a122` (前后端各删 1 行,X-Request-ID 由 audit/recovery 已承担)
- [~] **P1-2** testdata/migrations 同步 → **主人决定搁置**,9 个新索引仍由 `db_smoke_test.go` 真库冒烟守 000013-000015(000016-000021 6 个无 CI 守门,**已接受为技术债**)

### 仍 OPEN(4 项)

- [ ] **P2-2** metrics.GET 加 canRead
- [ ] **P2-4** `RequireRole\(` 在 routes.go 应该 0 命中的 lint 规则
- [ ] **P2-5** TestRoutes_所有路由都已分类按 source 而非 method 跳过 gin internals
- [ ] **Pre-3** routes_integration_test.go:122-126 Cleanup race 修复(显式 defer 顺序)

### 验证汇总(本 pull)

- ✅ `go build ./...` exit 0
- ✅ `go test -short ./...` 26 包全 ok,无 FAIL
- ✅ `npx tsc --noEmit` 0 error
- ✅ `npx eslint --max-warnings 0` 0 警告(P1-4 改的文件)
- ✅ `TestRoutes_只读端点未被过度收紧` PASS(含 P1-1 修复断言)
- ✅ `TestIntegrationStatus_凭据存在性仅canManage可见` 7/7 PASS
