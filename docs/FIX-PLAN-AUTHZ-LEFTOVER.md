# 修复计划：AUTHZ 遗留安全缺陷（强改密绕过 + API Key 自我复制）

- **状态**：已实现（2026-09-09）
- **前置**：`docs/FIX-PLAN-AUTHZ.md`（能力矩阵 + API Key scope，已实现并推送）
- **来源**：该文档 §4.3 / TODO.md「AUTHZ 已知缺口」中遗留的安全项
- **实现记录与偏差**：见 §8

---

## §1 现状实测

### S-1 首次强改密可被绕过（服务端判定错 + 前端参数名错，两处叠加）

#### S-1a 服务端：只看客户端自报的 `reason` 字面量，不看 DB 状态

`backend/internal/api/handlers/auth_handler.go:278-331`（`SkipPasswordChange`）：

```go
// body 可空 (老 API 兼容: 不传 reason 视为 optional)
_ = c.ShouldBindJSON(&req)
...
if req.Reason == "first_login" {                     // :296  唯一的一道拦截
    apierr.BadRequest(c, "首次登录必须修改默认密码,不允许跳过")
    return
}
now := time.Now()
// 幂等: 已是 FALSE 不重复写 audit
wasFlagged := user.MustChangePassword                // :303  此时必然为 true（否则无意义）
user.MustChangePassword = false                      // :304  直接清 flag
user.PasswordSetAt = &now                            // :305
```

判定依据是**请求体里的字符串**，而用户是否处于「强改密待办」状态在数据库里（`users.must_change_password`）。
两者不同源 → 同一状态换一个 reason 就得到相反结果：

| 请求 | `must_change_password` | 现状响应 | flag 结果 | 应为 |
| --- | --- | --- | --- | --- |
| `{"reason":"first_login"}` | true | 400 | 不动 | 400 ✓ |
| `{"reason":"optional"}` | true | **200** | **被清** ✗ | 400 |
| `{}`（空 body / 不传） | true | **200** | **被清** ✗ | 400 |
| 任意 body | false | 200 | 无变化 | 200（幂等，无副作用） |

即：`curl -XPOST /api/auth/skip-password-change -H 'Authorization: Bearer <token>'` 空 body 即绕过
主人 7/02 决策「首次登录强改密不可跳」。前端隐藏按钮只是 UX，不构成控制。

注：`users.must_change_password` 的列默认值是 `TRUE`（`migrations/000012_first_login_force_change.up.sql:12`），
所以**每个新用户/seed 用户默认处于待办态**，上表第 2、3 行是常态而非边角。

#### S-1b 前端：`reason` 参数名两端不一致 → 隐藏按钮的双保险恒失效

```ts
// frontend/src/pages/Login.tsx:53
navigate('/change-password?reason=first-login', { replace: true })   // 连字符

// frontend/src/pages/ChangePassword.tsx:32-33
const reason = searchParams.get("reason") || "optional"
const isFirstLogin = reason === "first_login"                        // 下划线
```

`isFirstLogin` **恒为 false** → `ChangePassword.tsx:203` 的 `{!isFirstLogin && <跳过按钮>}` 恒渲染
→ 首次登录用户看到「本次跳过」，点一下走 `authApi.skipPasswordChange("optional")`（`ChangePassword.tsx:73`），
配合 S-1a 现状返回 200 → **纯 UI 操作即可跳过强改密**。这不是"双保险失效"，是绕过的主路径之一。

#### S-1c 副作用：`password_set_at` 被写成假记录

`password_set_at` 全仓只有 3 处写入（`auth_handler.go:253` 改密、`:305` 跳过、`cmd/admin-bootstrap/main.go:94` 初始化），
**没有任何读取方**（无密码过期策略在用）。跳过改密却写 `password_set_at = NOW()` 语义为假，
一旦将来接入「密码 90 天过期」就会变成真实的策略绕过。本次一并去掉。

### S-2 write scope 的 API Key 可以铸造新 API Key（持久化后门）

```go
// backend/internal/api/routes.go:209-212（修复前）
protected.POST("/auth/api-keys", canIdentity, handlers.CreateAPIKey)
protected.GET("/auth/api-keys", canIdentity, handlers.ListAPIKeys)
protected.DELETE("/auth/api-keys/:id", canIdentity, handlers.DeleteAPIKey)
protected.PUT("/auth/api-keys/:id/revoke", canIdentity, handlers.RevokeAPIKey)
```

`canIdentity` = `RequireCapability(CapIdentity)`（`routes.go:181`），判定依据是**关联用户的 role**（`middleware/auth.go:196` 的 `c.Set("role", user.Role)`）。
API Key 自身的 scope 只拦 HTTP 方法（`middleware/auth.go:166` → `apiKeyAllows`，`:206-225`：read 只读 / write 可写）。

攻击链：管理员脚本里泄露了一个 `write` scope 的 Key（最常见泄露形态）→ 攻击者 `POST /auth/api-keys`
铸造一把新 Key（scope 任选）→ 管理员吊销旧 Key 后**新 Key 仍在**，且审计日志里新 Key 的铸造者是管理员本人。

同类影响：`GET`（枚举**其所属账号**的 Key 元数据）、`DELETE`/`PUT .../revoke`（吊销该账号的其它 Key → 拒绝服务 + 抹除痕迹）。
注：这些 handler 都带 `user_id = ?` 过滤（`api_key_handler.go:209/242/264`），**不能跨用户操作**。

相邻路径（同类缺陷）：`PUT /auth/password`（`routes.go:194`）在 `auth` 组，`AuthMiddleware` 同样接受 API Key
→ 泄露的 write Key 可直接改掉所属账号的密码，且**吊销 Key 撤销不了已改的密码**（账号沦陷不可逆）。

「长期凭据不得自我复制」与「Key 继承持有者角色的能力」是两件事：后者是设计（见 FIX-PLAN-AUTHZ.md §3.3），
前者是本次要堵的持久化通道。

### §1.3 既有测试编码了绕过行为 —— 修 bug 必须同步改测试

`backend/internal/api/handlers/auth_handler_test.go`：

| 行 | 测试 | 断言 | 与修复后语义 |
| --- | --- | --- | --- |
| 605 | `TestSkipPasswordChange_清Flag_写Audit` | 先置 flag=1 → 空 body → **200 + flag 被清 + 写 audit** | **冲突**，已重写 |
| 627 | `TestSkipPasswordChange_幂等不重复写Audit` | flag=1 → 两次 skip → 均 200，audit 1 条 | **冲突**，已重写 |
| 650 | `TestSkipPasswordChange_FirstLoginReason_拒绝400` | flag=1 + `reason=first_login` → 400 + flag 不动 | 正确行为，已并入参数化用例 |
| 672 | `TestSkipPasswordChange_OptionalReason_允许跳` | flag=1 + `reason=optional` → **200 + flag 被清** | **冲突**，已重写 |

其中 `:672` 的注释写「非首次（用户在改密页自己点取消）」而 fixture 恰恰 `UPDATE users SET must_change_password = 1`（强制态），
注释与 fixture 直接矛盾——测试是照着实现写的，不是照着需求写的。

前端同样有编码缺陷的测试：`frontend/src/pages/Login.test.tsx:136` 的用例名声称验证
`?reason=first-login`，但断言只检查「目标路由渲染了」，**参数写错也不会红**。

### §1.4 审查发现、本次不修的相关缺口（已记 TODO）

| 编号 | 缺口 | 证据 | 处置 |
| --- | --- | --- | --- |
| G-1 | `must_change_password=true` 在服务端**不阻断**其它端点（登录后拿 JWT 即可调任意 API） | `AuthMiddleware`（`middleware/auth.go:77-126`）不读该 flag；非测试代码读取点：`auth_handler.go:133/251/300`、`api_key_handler.go:126`、`cmd/seed/main.go:52/73`、`cmd/admin-bootstrap/main.go:93` | 见 §6.1（**铸 Key 一条已在 §8.1 F-1 收窄**），其余另立任务 |
| G-2 | API Key 路径不检查 `LockedUntil`（登录路径检查） | `auth_handler.go:58` vs `middleware/auth.go:187-191` | 见 §6.2 |
| G-3 | 前端密钥管理打的是 `/api/api-keys`，后端在 `/api/auth/api-keys` → 落到 NoRoute 返 index.html | `frontend/src/services/api.ts:12` + `:197-202` vs `routes.go:221-228` | 见 §6.4 |

---

## §2 方案

### D-A 服务端：以 DB 的 `must_change_password` 为唯一判据 + 该路由移入 protected 组

**Before**（`auth_handler.go:296-325`）：只拒绝 `reason == "first_login"`；否则清 flag、写 `password_set_at`、条件写 audit。

**After**：

```go
if user.MustChangePassword {
    apierr.BadRequest(c, "当前账号处于强制改密状态,不允许跳过")
    return
}
// 无待办：幂等成功，不写任何状态
c.JSON(200, gin.H{"code": 0, "message": "当前无需改密"})
```

- `reason` 字段**保留**（请求结构体与 openapi 不动，向后兼容），但服务端**不再读取**；注释写明已废弃。
- 拒绝路径**不动** user 表。
- 去掉的写入均为死代码/假记录：`wasFlagged` 恒为 false（flag=true 已提前 return）→ 原 audit 分支不可达；
  `password_set_at` 是假记录（S-1c）。
- **审计**：handler 内手写 audit 删除后，该端点必须另有留痕来源。修复前该路由挂在 `auth` 组
  （`routes.go:196`），**没有 AuditLog 中间件**（只在 `protected` 组，`routes.go:205`）→ 移入 `protected` 组，
  顺带获得 RateLimit(100/min) 与 AuditLog（拒绝与放行都留痕）。这与 API Key 路由移组的既有理由一致。

### D-B 前端：参数名对齐，让隐藏按钮真正生效

- `Login.tsx:53`：`reason=first-login` → `reason=first_login`（与 openapi enum、`ChangePassword.tsx:33` 一致）。
- `ChangePassword.tsx`：判定逻辑不变，补注释说明三处必须同名（`:1-5`、`:31`）。
- `Login.test.tsx`：加 `useLocation` 探针，把「跳转参数」变成真断言（修复前该用例只断言目标路由渲染）。

### D-C API Key / 会话凭据边界：`RejectAPIKeyAuth` 中间件

`backend/internal/middleware/auth.go` 新增：

```go
// RejectAPIKeyAuth 拒绝以 API Key 身份访问本端点（凭据铸造 / 凭据变更类操作专用）。
func RejectAPIKeyAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.GetString("api_key_id") != "" {
			apierr.Forbidden(c, "API Key 不能执行该操作,请使用登录会话")
			c.Abort()
			return
		}
		c.Next()
	}
}
```

挂载点：

```go
// routes.go — 整组挂载，未来新增 /auth/api-keys/* 自动继承
apiKeys := protected.Group("/auth/api-keys")
apiKeys.Use(middleware.RejectAPIKeyAuth())
{
    apiKeys.POST("", canIdentity, handlers.CreateAPIKey)
    apiKeys.GET("", canIdentity, handlers.ListAPIKeys)
    apiKeys.DELETE("/:id", canIdentity, handlers.DeleteAPIKey)
    apiKeys.PUT("/:id/revoke", canIdentity, handlers.RevokeAPIKey)
}

// 改密同理（泄露的 write Key 可直接改掉账号密码）
// 注：该路由**已移入 protected 组**（拿 AuditLog），路径由 /password 变 /auth/password
protected.PUT("/auth/password", middleware.RejectAPIKeyAuth(), middleware.RateLimit(...), handlers.ChangePassword)
```

**为什么比 TODO 记录的方案更严**：TODO.md:53 记的方向是「identity 路由额外要求 Key 权限含 `admin`」。
但 `admin` scope 的 Key 本身就是「一把能自我复制的长期凭据」，只要 `permissions` 里写 admin 就照样能铸新 Key；
read 是地板、write 是唯一实用的写权限，把门槛提到 admin 只是把后门挪了个位置。故改为**任何 API Key 一律不得管理凭据**。

### D-D 契约同步

- `backend/internal/api/openapi.yaml`：更新 `/auth/skip-password-change`（判据、`reason` 标 deprecated、200 语义），
  并把该 200 响应的 schema 从 `$ref: Error` 改为实际结构（修复既有笔误）。
- `frontend/src/services/api.types.ts`：`npm run gen:api` 重新生成。
- **`/auth/api-keys*` 四条路径不在 openapi 里**（`grep api-keys openapi.yaml` 零命中），本次不补
  （补 4 条 path + schema 属独立工作项，见 §6.5）；403 行为由代码注释与本文档承载。

---

## §3 Where

| 文件 | 改动 |
| --- | --- |
| `backend/internal/api/handlers/auth_handler.go` | `SkipPasswordChange` 判定与写入（S-1a/S-1c）；删 `uuid` 导入 |
| `backend/internal/middleware/auth.go` | 新增 `RejectAPIKeyAuth`（S-2） |
| `backend/internal/api/routes.go` | skip 移入 protected；api-keys 整组挂中间件；`PUT /auth/password` 挂中间件 |
| `backend/internal/api/handlers/auth_handler_test.go` | 重写 3 条 skip 测试 + 新增 1 条（§1.3） |
| `backend/internal/middleware/auth_scope_test.go` | 新增 `RejectAPIKeyAuth` 单测 |
| `backend/internal/api/handlers/api_key_handler.go` | `CreateAPIKey` 加强改密守卫（§8.1 F-1） |
| `backend/internal/api/handlers/testschema_test.go` | 新增：`testUsersDDL` 唯一 users 测试 DDL（消除两文件重复 DDL 的顺序依赖） |
| `backend/internal/api/handlers/api_key_handler_test.go` | 补 `users` 表 + `seedAPIKeyTestUser`；新增强改密守卫用例 |
| `backend/internal/api/routes_integration_test.go` | 新增 `TestRoutes_APIKey不能管理APIKey`（端到端，含 GET 判别用例）、`TestRoutes_强改密窗口内不能铸造APIKey`；修 skip 路由注解 |
| `backend/internal/api/openapi.yaml` | 契约描述 |
| `frontend/src/services/api.types.ts` | `gen:api` 重新生成 |
| `frontend/src/pages/Login.tsx` | `reason` 参数名（S-1b） |
| `frontend/src/pages/Login.test.tsx` | 参数探针 + 用例名 |
| `frontend/src/pages/ChangePassword.tsx` | 注释对齐（判定逻辑不变） |
| `docs/FIX-PLAN-AUTHZ.md` / `docs/adr/0005-角色词表与权限矩阵.md` / `TODO.md` | 状态同步 |

---

## §4 验收标准

| ID | 验收点 | 载体 |
| --- | --- | --- |
| AV-1 | flag=true 时，空 body / `{}` / `reason=optional` / `reason=first_login` / 未知 reason 五种 body 一律 400，且 flag 与 `password_set_at` 不变、不写 skip audit | `TestSkipPasswordChange_强制态一律拒绝`（5 子用例） |
| AV-2 | flag=false 时返回 200（含 `reason=optional` 与**真·空 body**），**不写** `password_set_at`、不写 audit；重复调用仍 200 | `TestSkipPasswordChange_无待办幂等且无副作用` |
| AV-3 | **write scope + admin 账号**的 API Key 调 `GET/POST/DELETE/PUT` 四条 `/auth/api-keys*` → 403，且文案含「登录会话」（区别于 `apiKeyAllows` 的「API Key 权限不足」）；`GET` 是唯一判别用例 | `TestRoutes_APIKey不能管理APIKey` |
| AV-4 | 同一把 write Key 调 `PUT /auth/password` → 403 | 同上 |
| AV-5 | 会话（JWT/cookie）访问同样路由行为不变：admin 可铸造（201），能力矩阵断言仍绿 | `TestRoutes_APIKey不能管理APIKey`（铸造步骤）+ `TestRoutes_能力矩阵_有权限放行` |
| AV-6 | 中间件本体：API Key 身份 403 且 handler 不执行；会话身份放行 | `TestRejectAPIKeyAuth` |
| AV-7 | 非 api-keys 端点不受影响：API Key 读写资产/告警的既有用例全绿 | `TestAuthMiddleware_APIKey*`、`TestAPIKeyAllows` |
| AV-8 | 前端 `Login.tsx` 跳转参数为 `first_login`（写成 `first-login` 该用例必须红） | `Login.test.tsx` 探针 |
| AV-9 | `go build ./...` + `go vet ./...` + `go test ./...` 全绿；前端 `tsc --noEmit` + `lint --max-warnings 0` + `vitest run` 全绿 | CI |
| AV-10 | 变异反证：改回 `req.Reason=="first_login"` → skip 测试红；卸掉 `RejectAPIKeyAuth` → 集成测试红；改回 `first-login` → 前端用例红；删掉 `CreateAPIKey` 的强改密守卫 → 守卫用例红 | §8 记录 |
| AV-11 | 强改密态（flag=true）下 `POST /auth/api-keys` → 403 且 `api_keys` 零行；flag=false → 201；用户不存在 → 404 | `TestCreateAPIKey_强改密态_拒铸造长期凭据`（3 子用例） |
| AV-12 | 路由层同一攻击路径：seed admin（flag=true）会话有效（GET 200）但铸造 403 | `TestRoutes_强改密窗口内不能铸造APIKey` |

---

## §5 Risk

- **R-1 语义收紧破坏既有集成**：若外部脚本依赖「空 body 跳过强改密」（现状可用）放行批量创建的用户，
  修复后会开始收到 400。**缓解**：受影响账号改一次密码即恢复——但改密需**旧密码**（`auth_handler.go:225`），
  密码由脚本随机生成且未下发的账号，只能由管理员介入（DB 改 `must_change_password` / 新建账号；
  本仓无管理员重置密码端点，`/users` 只有 GET）。本仓无此类调用方（前端只在改密页按钮调用）。
- **R-2 测试改动可能掩盖回归**：三条冲突测试必须重写，若只把断言从 200 改成 400 而不校验副作用，
  就测不出「拒绝路径不动 user 表」。**缓解**：每条测试断言 flag + `password_set_at` + audit 三处状态（AV-1/AV-2）。
- **R-3 中间件顺序 / 上下文键名依赖**：`RejectAPIKeyAuth` 依赖 `c.Set("api_key_id", ...)`（`auth.go:197`）。
  若某路由没挂 `AuthMiddleware`，该键为空 → 退化为放行。**缓解**：挂载点（`/auth/api-keys*`、`/auth/password`）
  全部在 `protected` 组内、`AuthMiddleware` 之后；单测直接构造 context 覆盖「有 key / 无 key」两种情形。
- **R-4 合法自动化被误伤**：若有轮换脚本用 API Key 管理 Key 或改密，会被 403。**缓解**：轮换应使用登录会话
  或 admin-bootstrap CLI；403 文案明确指引。本仓前端 Settings 页的密钥管理**实际打不到后端**
  （G-3，路径不匹配），故不存在可用的 API Key 调用方。
- **R-5 审计留痕形态变化**：skip 的审计从 handler 手写（action=`skip_password_change`）改为 AuditLog 中间件
  自动写（action=`POST`、path=`/api/auth/skip-password-change`）。**缓解**：AV-1 断言 handler 不再写
  `skip_password_change`；需要按 action 查询的审计脚本应改用 path 过滤——本仓无此类脚本
  （`grep skip_password_change` 仅测试、文档与 `migrations/000012` 注释）。

---

## §6 明确不做

1. **不在服务端全局强制 `must_change_password`**（G-1）：登录后拿到的 JWT 仍可调其它端点。
   彻底方案要在 `AuthMiddleware` 里加「flag=true 时只放行改密/登出/me」的判断，而 JWT 路径目前**不查库**
   （`middleware/auth.go:112-121` 只用 claims），每次请求加一次 DB 查询是独立的性能/架构决策，另立任务。
   **例外（2026-09-09 审计后收窄）**：`CreateAPIKey` 已单独查库拒绝强改密态（见 §8.1 F-1）——
   理由是该端点能把「临时弱口令会话」升级成**永不过期的长期凭据**，是唯一能突破窗口封锁的出口；
   其余端点仍属 G-1 范围。
2. **不给 API Key 路径补 `LockedUntil` 检查**（G-2）：会改变锁定期内自动化的行为，需单独决策。
3. 不改 API Key 继承持有者能力的设计（write Key 仍可写资产/工单/告警）——那是 FIX-PLAN-AUTHZ.md 的既定语义。
4. 不给 API Key 加角色/能力字段（引入新模型，超出本次范围）。
5. 不补 `/auth/api-keys*` 进 openapi（4 条 path + schema，独立工作项）。
6. 不做密码过期策略（`password_set_at` 仍无读取方；本次只停止写假值）。
7. 不动 `GET /users`、`GET /users/:id` 的 API Key 可达性（它们是 **identity** 能力路由，仅 admin；
   本次只限制「凭据铸造/变更」类端点，不收紧只读端点）。
8. 不修 G-3（前端密钥管理路径不匹配）——与本缺陷正交，记 TODO 单独处理。
9. 不改 `reason` 字段的请求结构（保留兼容，只废弃语义）。

---

## §7 实施步骤

1. 写/改测试（先红）：重写 skip 测试（3 改 1 增）+ `RejectAPIKeyAuth` 单测 + 集成测试。
2. 改实现：`auth_handler.go`（D-A）、`middleware/auth.go` + `routes.go`（D-C）。
3. 前端：`Login.tsx` 参数名 + 探针断言（D-B）。
4. 契约：`openapi.yaml` + `npm run gen:api`（D-D）。
5. 验证：AV-1~AV-12，含四处变异反证。
6. 同步 `FIX-PLAN-AUTHZ.md` / `ADR-0005` / `TODO.md` → 提交推送 → CI 三 job 验证。

---

## §8 实现记录（2026-09-09）

### 与本文档 v1 的偏差（审查后修订）

| 项 | v1 写法 | 最终实现 | 原因 |
| --- | --- | --- | --- |
| D-A 审计 | 声称「拒绝会被 AuditLog 中间件记录」 | **该路由原本没有 AuditLog** → 移入 `protected` 组 | 两路审查均判为阻断项：按 v1 实现会让该端点彻底失去审计留痕 |
| D-C 范围 | 只挂 4 条 api-keys 路由 | 追加 `PUT /auth/password` | 审查发现同类路径：write Key 可改掉账号密码，且吊销 Key 撤不回 |
| D-C 挂法 | 逐路由写中间件 | `apiKeys` 子组整组挂载 | 未来新增 `/auth/api-keys/*` 自动继承，防回归 |
| D-D | 「更新 openapi 的 api-keys 403 说明」 | 改为「不补 api-keys（spec 里没有）」 | 事实错误：openapi 无这些 path |
| AV-3 | 未指定 Key scope | 明确 write scope + admin 账号，且必须覆盖 `GET` | read Key 打写方法本来就被 `apiKeyAllows` 挡，会「因错误原因通过」 |
| 文档行号 | `:302`/`:303` | `:303`/`:304`，Before 区间 `:296-325` | ±1 漂移 |

### 验证结果

| 项 | 结果 |
| --- | --- |
| `go build ./...` / `go vet ./...` | 通过 |
| `go test ./...` | 见提交前记录（全绿） |
| 新增/重写 Go 测试 | `TestSkipPasswordChange_强制态一律拒绝`（5 子用例）、`TestSkipPasswordChange_无待办幂等且无副作用`、`TestRejectAPIKeyAuth`（2 子用例）、`TestRoutes_APIKey不能管理APIKey`（5 子用例）、`TestCreateAPIKey_强改密态_拒铸造长期凭据`（3 子用例，`api_key_handler_test.go`）、`TestRoutes_强改密窗口内不能铸造APIKey`、`TestRoutes_跳过强改密留审计` |
| 变异反证 1 | 把 handler 判据改回 `req.Reason == "first_login"` → `TestSkipPasswordChange_强制态一律拒绝` 红 |
| 变异反证 2 | 卸掉 `apiKeys.Use(middleware.RejectAPIKeyAuth())` → `TestRoutes_APIKey不能管理APIKey` 红 |
| 变异反证 3 | `Login.tsx` 改回 `reason=first-login` → `Login.test.tsx` 对应用例红 |
| 变异反证 4 | 删掉 `CreateAPIKey` 的 `owner.MustChangePassword` 守卫 → `TestCreateAPIKey_强改密态_拒铸造长期凭据/flag=true` 红（实测得 201） |
| 前端 `tsc --noEmit` / `lint --max-warnings 0` / `vitest run` | 通过（Login + ChangePassword 12 例） |

### §8.1 安全审计后续修复（同日，审计 agent 回执）

审计对本轮改动判「无阻断项」，但提出 5 条收窄建议。已采纳 2 条：

**F-1（已修）强改密窗口内可用会话铸造永不过期 Key**

- **失败模式**：seed `admin/admin123` 首次登录 → 服务端此刻只拦「跳过改密」这一步，但允许 `POST /auth/api-keys`；
  攻击者（或图省事的运维）铸一把 `expires_at` 为空的 Key → 之后再改密，**Key 依然有效且撤不回**
  （撤销 Key 需要会话，而新会话仍受强改密门禁）。等于把「必须改密」变成可选项。
- **缓解**：`CreateAPIKey` 开头查一次 `users.must_change_password`，true → 403（一次性开销，不碰
  `AuthMiddleware` 热路径）。这是 §6.1 G-1 的**定点例外**，不是全局强制。
- **代价**：测试库必须有 `users` 表 —— `api_key_handler_test.go` 的 schema 原本只有 `api_keys`，
  已补最小 `users` 表 + `seedAPIKeyTestUser` 助手（顺带修掉 `createTestUser` 从不插库的既有问题）。

**F-2（已修）`PUT /auth/password` 移入 `protected` 组补审计**

- 原实现把该路由留在 `auth` 组（只挂 `AuthMiddleware`），改密成功/失败**不留审计**。
  移入 `protected` 后自动获得 `AuditLog`，另挂 `RateLimit(3/min)` + `RejectAPIKeyAuth`。

**未采纳 3 条（记入后续任务，不在本轮）**：F-3 `notification-channels` 凭据可被 write Key 读取、
F-4 `GET /auth/me` 缺 openapi、F-5 `cmd/set-role` TOCTOU。理由：均与本缺陷正交，且 F-3/F-5 需要独立决策。

### §8.2 遗留 G 清单（本轮不修，已记 TODO.md）

| ID | 内容 | 为何不在本轮 |
| --- | --- | --- |
| G-1 | 全局强制强改密（`AuthMiddleware` 查库） | 每请求一次 DB 查询，性能/架构独立决策；本轮只收窄 `CreateAPIKey` |
| G-2 | API Key 路径不查 `LockedUntil` | 会改变锁定期内自动化行为 |
| G-3 | 前端密钥管理路径与后端不匹配（`Settings` 页打不到后端） | 与本缺陷正交；因 R-4 无人受影响 |

### §8.3 测试与一致性审计处置（同日）

审计（独立 agent，只写 /tmp、未改仓库）结论「可以提交」，7 条变异反证全红、无空转断言。其发现全部处置如下：

| 编号 | 发现 | 处置 |
| --- | --- | --- |
| 中-1 | skip 路由移入 `protected` 的**唯一理由**（拿 AuditLog）无测试守护，挪回去全量测试仍绿 | 新增 `TestRoutes_跳过强改密留审计`：flag=true 请求 → 400 且 `audit_logs` 有该 path 一行。**变异反证 5**：挪回 `auth` 组 → 该用例红（实测 `[]` should have 1 item） |
| 中-2 | 两个测试文件在同一共享内存库里各写一份 `users` DDL，`IF NOT EXISTS` 让先建者胜（`-shuffle=on` 曾偶发 `NOT NULL constraint failed`） | DDL 收敛到 `testschema_test.go` 的 `testUsersDDL` 单一常量；`-shuffle=on -count=3` 全绿 |
| 低-1 | §2 D-C 挂载点仍写 `auth.PUT`（实现已移 protected）；§3 Where / R-3 未同步 | 三处已改 |
| 低-2 | §1.4 G-1 证据「全仓仅三处」过时（`api_key_handler.go:126` 也读 flag） | 改为列全非测试读取点，并指向 §8.1 例外 |
| 低-3 | §7/§8 未同步 AV-11/AV-12 与 F-1 载体 | 已同步（AV 表 + 本表） |
| 低-4 | AV-1「`{}` / `{}` 空对象」重复 | 改为「空 body / `{}` / …」 |
| 低-5 | 前端注释与修复后语义相反（`api.ts:90`、`ChangePassword.tsx:72`）；`reason` 联合类型与生成的 `reason?: string` 口径分裂 | 注释改写；`reason?: string` |
| 低-6 | 「空 body」子用例实发 `null`；flag=false 未覆盖真·空 body | 新增 `doNoBodyWithUser`（真不带 body）用于两处。**变异反证 6**：把 `_ = ShouldBindJSON` 改成绑定失败即 400 → 幂等用例红 |
| 低-7 | 能力矩阵对 `POST /auth/api-keys` 的正例断言在 F-1 后空转（随机 user_id → 404） | 就地加注释说明该行只证门禁放行、201 正例由 `TestRoutes_APIKey不能管理APIKey` 承担 |
| 低-8 | R-5「grep 仅测试与文档」不精确（还命中 migration 注释） | 已补 |
