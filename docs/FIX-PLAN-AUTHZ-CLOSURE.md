# 修复计划：AUTHZ 遗留收口（G-2 / G-3 / F-3 / F-5）

**状态**：v2（已按两路审查修订，见 §8）
**日期**：2026-09-09
**关联**：[FIX-PLAN-AUTHZ](FIX-PLAN-AUTHZ.md)、[FIX-PLAN-AUTHZ-LEFTOVER](FIX-PLAN-AUTHZ-LEFTOVER.md)、[ADR-0005](adr/0005-角色词表与权限矩阵.md)、[TODO.md](../TODO.md)

本轮处理上一轮两个审计留下的遗留项，全部集中在「API Key 这条长期凭据通道」上。

**核心原则**（与上一轮 `S-2` 同源，本轮补齐第二个面）：

> 长期凭据（API Key）不得**自我复制**，也不得**读取或改写其它凭据**。

**两个需要重点审查的判定**：
- **G-2 判定为「不修」**（§2 D-A）——不把 API Key 耦合到 `LockedUntil`，改为补检测（§2 D-E）。
- **范围扩大**：审查实测出 `PUT /integrations/*` 可用 write Key 把**已存凭据外泄**（§1 S-4），本轮一并收口（§2 D-C）。

---

## §1 现状实测

### S-1（G-2）API Key 路径不查 `LockedUntil`

| 事实 | 证据 |
| --- | --- |
| 登录路径检查锁定（在密码校验**之前**） | `backend/internal/api/handlers/auth_handler.go:88-89` → 403 |
| API Key 路径**不**检查锁定 | `backend/internal/middleware/auth.go:186-188` 只判 `user.Status == "inactive"` |
| 锁定只由「登录密码连续失败」触发 | `auth_handler.go:33` `maxFailedLoginAttempts = 5`；`:104-109` 达阈值 → `locked_until = now+30min` |
| **锁定是一次性的**：首次锁定后 `locked_until` 不再为 nil，到期后继续错密只累加 `failed_login`，**不会重新锁定**；唯一清空点是登录成功（`:120`） | 审查实测：锁定后继续错密 → `locked_until` 保持旧值；成功登录一次 → 清空，再错 5 次才再次锁定 |
| 全仓无「手动禁用/锁定账号」入口 | `routes.go:317-321` 的 `/users` 只有 `GET`/`GET /:id`；`cmd/` 只有 `admin-bootstrap`/`migrate`/`seed`/`server`/`set-role` |
| 登录失败**零审计**：`/auth/login` 不在 `protected` 组（无 `AuditLog`），handler 内无审计写入 | `routes.go:198`；`grep models.AuditLog internal/api/handlers/auth_handler.go` 零命中 |

### S-2（G-3）前端密钥管理打的是不存在的路径，且字段名、格式提示也不对

| # | 事实 | 证据 |
| --- | --- | --- |
| S-2a | `baseURL = "/api"` + `"/api-keys"` → 实际请求 `/api/api-keys`；真实路径 `/api/auth/api-keys` → 落到 NoRoute 返回 `index.html` | `frontend/src/services/api.ts:12`、`:199-206`；`routes.go:231-236` |
| S-2b | 读 `res.data.data.key`，后端返回 `api_key` → **一次性明文 Key 永不显示** | `Settings.tsx:313`；`api_key_handler.go:200` |
| S-2c | 该区块对所有角色可见；`/auth/api-keys` 是 `identity`（仅 admin）→ 非 admin 静默空白（拦截器已弹「没有权限访问」，组件 catch 再置空数组） | `routes.go:231-236`；`Settings.tsx:282-298`；`api.ts:48-50` |
| S-2d | 零测试覆盖：当时 26 个前端测试文件里没有 `Settings.test.tsx`（本轮已补，现 27 个） | `ls frontend/src/pages/*.test.tsx` |
| S-2e | 过期时间输入框提示「RFC3339，如 2027-01-01T00:00:00Z」，后端只接受 `YYYY-MM-DD` → 按提示输入必 400 | `Settings.tsx:941`；`api_key_handler.go:148` `time.Parse("2006-01-02", …)` |

### S-3（F-3）`GET /notification-channels` 的明文凭据可被 API Key 读取

| 事实 | 证据 |
| --- | --- |
| `config` 是明文 JSON（webhook token / SMTP 凭据），响应体原样回传 | `models/alert.go:109`；`channel_handler.go:23-30` |
| 该组门禁是 `manage`（admin / ops_admin） | `routes.go:336-343` |
| API Key 的 `role` 取自**关联用户** | `middleware/auth.go:196` |
| → admin 账号名下的 `write` Key 可 `GET` 到全部渠道明文凭据（审查已实测：响应体含 `access_token=SUPERSECRET`） | `apiKeyAllows` 只按方法判（`auth.go:206-225`） |
| `RejectAPIKeyAuth` 目前只覆盖 `/auth/api-keys*` 与 `PUT /auth/password` | `routes.go:232`、`:231-236` |

### S-4（F-5）`PUT /integrations/*` 可被 write Key 用来**外泄已存凭据**（审查实测复现）

| 事实 | 证据 |
| --- | --- |
| integrations 组只挂 `canManage`，**无** `RejectAPIKeyAuth` | `routes.go:241-254` |
| 更新时空 token/password = **保留旧值**，只改 URL | `integration_handler.go:145`（NetBox `:194`、GLPI `:233-234` 同） |
| `/test` 用**已存凭据**向配置的 URL 发起出站请求 | `zabbix.go:38/49`、`netbox.go:32-34`（`Authorization: Token <已存 token>`） |

**复现链**（审查用 overlay 实测）：`PUT /integrations/zabbix`（改 URL 指向攻击者、password 留空）→ `POST /integrations/zabbix/test` → 攻击者服务器收到含已存凭据的请求体。
比 S-3 更重：拿到的口令可复用、可横向。

### S-5 已核实、无需重复核对的事实

- `GET /integrations/status` 只回 `has_token`/`has_password` 布尔 + URL/用户名，**不含凭据**（`integration_handler.go:91-113`）。
- 通知渠道凭据**无第二出口**：告警投递 worker 直读 DB 不走 HTTP；无 `/notification-logs` 端点；审计中间件不记 body。
- 前端「路径」类断言必须走 `services/api.test.ts` 的 `api.defaults.adapter` 手法（`api.test.ts:53-70`）；`Assets.test.tsx:13` 的 `vi.mock('../services/api')` 会把 URL 一起 mock 掉，无法观测。
- D-C 不新增/删除路由 → `TestRoutes_所有路由都已分类`、能力矩阵正/反例测试均不受影响。

---

## §2 方案

### D-A（G-2）判定为**不修**：不耦合 `LockedUntil`，改为补检测

**结论**：API Key 路径**保持**只看 `status`，不看 `LockedUntil`。

**理由**（三条，均已按审查修正）：

1. **因果错位，安全收益为零**：`LockedUntil` 是「密码被猜」的信号，与「Key 是否被盗」无因果关系。
   持有有效 Key 的攻击者不会因为别人试错密码被拦住；而唯一能真正止血的账号级开关是 `status=inactive`
   （登录与 Key 两条路径都立即失效，`auth_handler.go:52` / `middleware/auth.go:187`）。
2. **耦合的代价是真实存在的、且方向是「削弱处置能力」**：
   - 任何知道用户名的人都能触发一次 30 分钟锁定（一次性，见 S-1 第 4 行；要再次武装需目标账号成功登录一次）。
   - **锁定会阻断受害者自救**：锁定判定在密码校验之前（`auth_handler.go:57`），而吊销 Key / 改密必须走登录会话
     （`RejectAPIKeyAuth`，`routes.go:214`/`:222`）→ 被锁期间受害者无法登录去撤销那把 Key，而 Key 在此期间
     照常可用。若把 Key 也耦合进去，这个「锁死自救窗口」的后果只会更重。
3. **正确的补充是「看得见」，不是「再上一个开关」**：当前登录失败与触发锁定**完全无留痕**（S-1 末行），
   运维既分不清爆破还是忘密码，也无法判断谁在制造锁定。故新增 **D-E：登录尝试入审计**。

**不修不等于不记录**：G-2 结案为「不修（决策已记 ADR）」，并新增两个缺陷条目：
- **G-4 账号处置无产品化入口**：`status`/`locked_until` 只能改库；建议下一轮做用户管理最小集
  （`cmd/disable-user` + `PUT /users/:id/status`），并覆盖「禁用即失效」的 JWT 侧缺口。
- **G-5 已签发 JWT 不查库**：`status=inactive` 对 Key 立即生效，但对**已签发的会话 JWT 最长 24h 才生效**
  （`middleware/auth.go:112-124` 只用 claims；`config.yaml:48` `expire: 86400`）——「禁用账号」对会话失窃
  不是即时止血。属独立架构决策（每请求查库 vs 短 TTL + 刷新）。

### D-B（G-3）修前端：路径 + 字段名 + 格式提示 + 403 呈现

- `frontend/src/services/api.ts:197-202`：四条 `/api-keys` → `/auth/api-keys`。
- `Settings.tsx:300`：`res?.data?.data?.key` → `api_key`。
- `Settings.tsx:916`：占位提示改为 `YYYY-MM-DD`（与后端 `2006-01-02` 对齐）。
- `Settings.tsx:281-289`：403 时**不再二次 toast**（拦截器已弹「没有权限访问」），改为置
  `apiKeysForbidden` 状态并在区块内渲染 `Alert`「当前账号无密钥管理权限」，避免静默空白。

### D-C（F-3 + F-5）凭据类端点整组拒绝 API Key

```go
// routes.go
channels := protected.Group("/notification-channels")
channels.Use(canManage)
channels.Use(middleware.RejectAPIKeyAuth())   // F-3：渠道 config 含明文凭据

// integrations：只拦「写凭据」，不拦只读状态与同步/测试
protected.PUT("/integrations/zabbix", middleware.RejectAPIKeyAuth(), canManage, integrationH.UpdateZabbix)
protected.PUT("/integrations/netbox", middleware.RejectAPIKeyAuth(), canManage, integrationH.UpdateNetBox)
protected.PUT("/integrations/glpi",   middleware.RejectAPIKeyAuth(), canManage, integrationH.UpdateGLPI)
```

**为什么 `PUT` 拦、`test`/`sync` 不拦**：原则是「不得**读取或改写**凭据」。`PUT` 改写凭据（且可只改 URL 保留旧 token），
是 S-4 外泄链的**唯一入口**；堵掉它之后，`/test` 只能打管理员配置过的地址，`/sync` 只是触发数据同步——
这两条正是自动化该用的能力，拦了没有安全收益（会误伤合法自动化）。

### D-E（新增）登录尝试写审计

`routes.go` 给 `/auth/login` 挂 `AuditLog`（其余 protected 路由已有）；`Login` handler 在校验前
`c.Set("username", sanitizeAuditUsername(req.Username))`，让审计行带上**尝试的用户名**（否则未认证请求的审计只有 IP）。
- **净化**（审查 F3）：审计行是行式消费的（SIEM / CSV 导出），请求体里的 `\n` / `\0` 能把一行伪造成多条记录。
  `sanitizeAuditUsername` 剥掉控制字符并按 **96 字节 / rune 边界**截断（小于 `audit.go` 的 100 字节硬截，避免中文/emoji 被切出非法 UTF-8）；只影响审计展示值，不参与鉴权判定。

- 价值：爆破/锁定可被追溯（G-2 的「看得见」替代「再上一个开关」）。
- 成本：每次登录多一次同步 INSERT（`AuditConfig.Async` 默认 false）；登录本身已限流 5/min。
- 审计行不含密码（`ShouldBindJSON` 的 body 不进审计；`AuditLog` 只记 method/path/status/IP/UA）。

### D-F 契约

不改 `openapi.yaml`（`/auth/api-keys*`、`/notification-channels`、`/integrations/*` 的 403 语义由代码注释
与本文档承载；补 spec 是独立工作项，见 §6）。

---

## §3 Where

| 文件 | 改动 |
| --- | --- |
| `backend/internal/api/routes.go` | D-C（channels 组 + 三条 integrations PUT）、D-E（login 挂 AuditLog） |
| `backend/internal/api/handlers/auth_handler.go` | D-E：`sanitizeAuditUsername(req.Username)` 净化后入 context；审查 F3 |
| `backend/internal/api/routes_integration_test.go` | 新增：API Key 打 channels / integrations PUT → 403；登录成功/失败各留一条审计；审查 F1（429 不写审计）、F3（用户名净化）、F5（AV-3 断言 200）、F6（集成端点指向本进程假服务）、F7（限流桶按次独立） |
| `frontend/src/services/api.ts` | D-B：四条路径 |
| `frontend/src/services/api.test.ts` | AV-4：用 adapter 断言四条 URL + `baseURL`；审查 F8（reject 保留 `.response`） |
| `frontend/src/pages/Settings.tsx` | D-B：字段名、占位提示、403 Alert |
| `frontend/src/pages/Settings.test.tsx` | 新增：AV-5（一次性明文展示）、AV-6（403 Alert）、审查 F5（关闭后重开不显示旧明文）、审查 F3（403→500 撤下 Alert） |
| `backend/internal/middleware/audit.go` | 注释修正：`Async` 零值 = 同步写（原注释写「默认 true」，与代码相反） |
| `frontend/vite.config.ts` | `testTimeout` 5s→20s：antd 页面级用例在 jsdom 下挂载 Tabs+Table+Modal 需 ~7-10s（本机 2 核与 CI 同档） |
| `TODO.md` | G-2 结案（不修+理由）、G-3 结案、F-3/F-5 结案；新增 G-4、G-5、G-6（TLS 最低版本）、G-7（受信代理）、G-8（RejectAPIKeyAuth 未挂载时静默失效） |
| `docs/FIX-PLAN-AUTHZ-LEFTOVER.md` | §8.1「未采纳 3 条」中 F-3 结案；§8.2 表内 G-2/G-3 状态更新 |
| `docs/FIX-PLAN-AUTHZ.md` | §3.3 路由挂载清单加「后续修订（2026-09-09，见本文档）」注记（沿用上一轮模式） |
| `docs/adr/0005-角色词表与权限矩阵.md` | **纠正 `:84` 的既有错误论断**（原文称 API Key 访问 `GET /notification-channels` 会被 `manage` 挡住——实测不成立，role 取自关联用户）；补 F-3/F-5 决策行 |

---

## §4 验收标准

| ID | 验收点 | 载体 |
| --- | --- | --- |
| AV-1 | admin 会话铸造的 **write scope** Key 调 `GET/POST/PUT/DELETE /api/notification-channels*` 一律 403，文案含「登录会话」（区别于 `apiKeyAllows` 的「API Key 权限不足」） | 集成测试 |
| AV-2 | 同一把 write Key 调 `PUT /api/integrations/{zabbix,netbox,glpi}` → 403 且文案含「登录会话」；`POST /integrations/*/test` 与 `/sync` **不**被新中间件拦（防过度收紧） | 集成测试 |
| AV-3 | 会话（JWT）访问上述端点行为不变：admin 访问 `GET /notification-channels`、`GET /integrations/status` **返 200**（不是「非 403」——401/404 也会通过） | 集成测试 + `TestRoutes_能力矩阵_有权限放行` |
| AV-4 | 前端 `apiKeyApi` 四条请求的 `config.url` 为 `/auth/api-keys…`、`baseURL` 为 `/api` | `services/api.test.ts`（`api.defaults.adapter`） |
| AV-5 | 创建成功后页面**渲染出后端返回的 `api_key` 明文**（`getByDisplayValue`，非文案匹配） | `Settings.test.tsx` |
| AV-6 | `apiKeyApi.list` 返 403 时区块渲染「无密钥管理权限」Alert，且不静默空白 | `Settings.test.tsx` |
| AV-7 | 登录成功与失败各写一条审计（path=`/api/auth/login`、status 200/401、username=尝试的用户名）；密码不出现在审计行 | 集成测试 |
| AV-8 | `go build/vet/test` + 前端 `tsc`/`eslint --max-warnings 0`/`vitest` 全绿 | CI |
| AV-9 | 变异反证（九处，逐条实测「变红 → 还原后变绿」） | 见 §9.2 |
| AV-10 | 被限流（429）的登录请求**不**写审计行：7 次请求 = 5×401 + 2×429，审计只 +5 | `TestRoutes_限流的登录请求不写审计` |
| AV-11 | 登录尝试的用户名入库前净化：无控制字符、≤96 字节、合法 UTF-8、可打印内容保留 | `TestRoutes_登录审计用户名被净化` |
| AV-12 | 关闭生成密钥 Modal 后重新打开**不再显示**上一把明文 Key | `Settings.test.tsx`（审查 F5） |

---

## §5 Risk

- **R-1 若审查否决 D-A（决定耦合 `LockedUntil`）**：代价不是「无限重放」而是——① 攻击者每触发一次锁定
  可造成 30 分钟自动化停摆（一次性，需目标账号成功登录后才可再次触发）；② 锁定期间**受害者无法登录自救**
  （吊销 Key / 改密都要会话），耦合只会放大这个窗口。**缓解**：若一定要收，正确形态是「Key 自身连续失败达阈值时
  锁定 Key」（与密码锁定解耦），需新增字段与计数，属独立特性。
- **R-2 前端修好路径后非 admin 会看到 403**：**缓解**：拦截器弹一次提示 + 区块内 Alert 说明，不再二次 toast（D-B 第 4 条）。
- **R-3 若有外部脚本用 API Key 管理渠道或改写集成凭据，会被 403**：**缓解**：403 文案指引改用登录会话；
  本仓无此类调用方（`notification-channels` 仅前端 `api.ts`/`Settings` 与路由、测试、文档；integrations 同理）。
- **R-4 登录审计的写放大**：每次登录 +1 行（已限流 5/min，同步写）。**缓解**：`AuditConfig.SkipPaths` 已含健康检查类路径；
  若后续量级上升，改为 `Async: true` 即可（一处配置）。
- **R-5 D-C 漏了同类端点**：本轮已把审查实测的 integrations 写路径一并收口（S-4）；仍保留 `GET /integrations/status`
  的 URL/用户名（不含 token，§6.4）。**缓解**：审计以「还有哪些端点能读到或改写凭据」为清单逐条过（见 §8 处置表）。

---

## §6 明确不做

1. 不把 `/auth/api-keys*`、`/notification-channels`、`/integrations/*` 补进 `openapi.yaml`（独立工作项）。
2. 不做用户管理 CRUD（**G-4**：禁用/启用/重置密码/`cmd/disable-user`）——本轮只记录。
3. 不改 JWT「不查库」的架构（**G-5**：禁用账号对已签发会话最长 24h 才生效）。
4. 不改 `apiKeyAllows` 的「按 HTTP 方法判定 scope」语义（细粒度 scope 属独立任务）。
5. 不改 `GET /integrations/status` 回传 URL + Zabbix 用户名（不含 token，见 TODO）。
6. 不给 API Key 加角色/能力字段；不做密码过期策略。
7. 不改前端权限选项里的「管理 (admin)」——后端把 `admin` 当 `write`（`apiKeyAllows`），属 scope 能力化范围（§6.4 同源）。
8. 不更新 `CHANGELOG.md`「未发布」节（既存滞后，非本轮引入；与上一轮口径一致）。

---

## §7 实施步骤

1. 写测试（先红）：集成测试 AV-1/AV-2/AV-7、前端 AV-4/AV-5/AV-6。
2. 改实现：`routes.go`（D-C/D-E）、`auth_handler.go`（D-E）、`api.ts` + `Settings.tsx`（D-B）。
3. 验证 AV-1~AV-9（含四处变异反证）。
4. 同步 `TODO.md` / `FIX-PLAN-AUTHZ-LEFTOVER.md` / `FIX-PLAN-AUTHZ.md` / `ADR-0005` → 提交推送 → CI 三 job。

---

## §8 审查处置记录（v1 → v2）

两路审查（安全 / 一致性）共 21 条发现，全部采纳：

| 来源 | 发现 | 处置 |
| --- | --- | --- |
| 安全 F-01 / 一致 F4 | 「锁定可无限重放」为**事实错误**（实测一次性） | §2 D-A 理由重写、R-1 重写 |
| 安全 F-02 | `status=inactive` 对已签发 JWT 最长 24h 才生效 | 新增 **G-5**；D-A 理由 1 改述 |
| 安全 F-03 | `PUT /integrations/*` + `/test` 可外泄已存凭据（实测复现） | 新增 §1 S-4；**本轮一并收口**（D-C） |
| 安全 F-04 / 一致 F1 | AV-3 载体与 R-4 自相矛盾（mock 服务模块无法观测 URL） | AV-4 改用 `services/api.test.ts` 的 adapter 手法 |
| 安全 F-05 | 锁定窗口阻断受害者自救；无 CLI 兜底 | 新增 **G-4**；写入 D-A 理由 2 |
| 安全 F-06 | 登录失败零审计 → G-2 的「第三方案」 | 新增 **D-E**（登录尝试入审计）+ AV-7 |
| 安全 F-07 / 一致 F6 | 双 toast；403 提示无验收点 | D-B 第 4 条 + AV-6 |
| 安全 F-08 / 一致 F2 | AV-4 文案断言会命中 toast（变异反证空转） | AV-5 改用 `getByDisplayValue` |
| 安全 F-09 / 一致 F7 | 行号偏差（`routes.go:318-326`、`auth_handler.go:57`） | 已校正 |
| 一致 F3 | ADR-0005 `:84` 既有论断与 S-3 矛盾 | §3 Where 改为「纠正既有论断」 |
| 一致 F5 | integrations 同类路径未记录 | 已收口（同上） |
| 一致 F8 | R-3 的 grep 证据不精确 | 已改写 |
| 一致 F9 | `FIX-PLAN-AUTHZ.md` §3.3 / LEFTOVER §8.1 未同步 | 补入 §3 Where |
| 一致 F10 | 「静默空白」有条件（dist 存在时才 200 HTML） | S-2c 已限定表述 |
| 一致 F11 | CHANGELOG 滞后 | §6.8 显式记录不做 |
| 自查 | 前端过期时间提示与后端格式不符（S-2e） | 并入 D-B |
| 安全 F-10/F-11、一致 已核实项 | D-C 不误伤、无第二出口、AV 不空转 | 采纳，§1 S-5 记录 |

---

## §9 实测记录（2026-09-09）

### 9.1 验收标准执行结果

| ID | 结果 |
| --- | --- |
| AV-1 / AV-2 / AV-3 | `TestRoutes_APIKey不能读写信道与集成凭据` 通过（含 5 条渠道用例、3 条 integrations PUT、4 条「未拦」反向用例（补状态码断言，实测 400/400/400/500，非 403/404/405）、2 条会话不误伤用例（断言 200）） |
| AV-4 | `api.test.ts`「apiKeyApi 四条请求都打 /api/auth/api-keys*」通过（`baseURL+url` 全量比对 + method 比对 + `baseURL === "/api"` 单独断言） |
| AV-5 / AV-6 | `Settings.test.tsx` 5 条通过（明文展示 / 关闭后重开不显示旧明文 / 403 Alert / 500 不误报权限 / 403→500 撤下 Alert） |
| AV-7 | `TestRoutes_登录尝试留审计` 通过（401 + 200 各一条，username 命中，密码不出现在审计行） |
| AV-8 | `go build ./... && go vet ./... && go test ./... -count=1` 全绿；`tsc --noEmit` / `eslint src --max-warnings 0` / `vitest run`（27 files / 167 tests）全绿 |
| AV-9 | 见 9.2 |

### 9.2 变异反证（九处，全部实测「变红 → 还原后变绿」）

| # | 变异 | 预期红 | 实测 |
| --- | --- | --- | --- |
| ① | 卸掉 `channels.Use(middleware.RejectAPIKeyAuth())` | AV-1 | 5 条渠道用例全红（integrations 用例仍绿，证明断言互相独立） |
| ② | `Settings.tsx` 读回 `res.data.data.key` | AV-5 | 「创建成功后渲染 api_key 明文」红，另 2 条绿 |
| ③ | `api.ts` 路径改回 `/api-keys` | AV-4 | 断言输出 `expected [ '/api/api-keys', … ]`，红 |
| ④ | 去掉 `/auth/login` 的 `AuditLog` 中间件 | AV-7 | 审计行数 0（期望 2），红 |
| ⑤ | 去掉 `Settings.tsx` `onCancel` 里的 `setGeneratedKey(null)` | F5 用例 | 「关闭后重新打开不再显示明文」红（`expect(element).not.toBeInTheDocument()`） |
| ⑥ | `/auth/login` 中间件顺序换回 `AuditLog` 在 `RateLimit` 之前 | F1② 用例 | 审计行数 7（期望 5），红 —— 429 也落库 |
| ⑦ | `c.Set("username", req.Username)`（不经 `sanitizeAuditUsername`） | F3 用例 | 换行 / NUL / 控制字符三条断言全红 |
| ⑧ | `Settings.tsx` catch 改回「只置 true 不复位」 | 审查 F3 用例 | 「先 403 后 500 撤下 Alert」红 |
| ⑨ | 限流用例源 IP 改回固定 `203.0.113.66` | 审查 F7（`-count=2`） | 第二轮 401 计数 0（期望 5），红 |

> ⑤ 的断言点选在**重新打开 Modal** 而非「关闭瞬间」：antd Modal 离场由 rc-motion 驱动，jsdom 不触发 `transitionend`，离场节点被保留在 DOM（实测关闭后 `queryByText` 仍命中），断言关闭瞬间会假红。用户实际能看到的时刻是重新打开——那里内容重新渲染，状态没清就会重新显示明文 Key。

### 9.3 后续发现（不在本轮范围）

- **G-6**：Web 界面 TLS 最低版本（PCI DSS 4.2.1）在仓库内**无强制、无验证**——**已于同日单独收口**（见 `TODO.md` G-6 结案）：`08-部署运维.md` §8.2.2 定义终止点与最低版本，`scripts/check-tls.sh` 主动降级断言，`frontend/nginx-tls.conf.example` 提供 TLS 1.2/1.3 + HSTS + 80→443 模板。连带发现 **G-9**（compose 引用的 Dockerfile 不存在）。

- **G-7**：gin 未配受信代理（安全审计 F1①）——默认信任 `0.0.0.0/0`，`ClientIP()` 取 XFF 最左值，登录限流可被逐请求换 XFF 绕过、审计 IP 可伪造。修复需先定部署拓扑（compose 里 nginx 与后端是不同容器，故不能简单 `SetTrustedProxies(nil)`，否则全站共用限流桶）。见 `TODO.md`。
- **G-8**：`RejectAPIKeyAuth` 在未挂 `AuthMiddleware` 的路由上静默放行（安全审计 F6）——当前无实际暴露路径，属潜在陷阱。见 `TODO.md`。

### 9.4 一致性审计（第二轮）处置（2026-09-09）

独立审计员对未提交改动做正确性/一致性审计（非安全视角），结论「需修订后再提交」，8 条全部处置：

| # | 级别 | 问题 | 处置 |
| --- | --- | --- | --- |
| F1 | 中 | 文档 §2/§3/§4 未同步最新硬化（username 净化、vite.config、audit.go 注释、缺 F1/F3/F5 验收条目） | 已同步：§2 D-E 写入净化方案，§3 Where 补 3 行，§4 新增 AV-10/AV-11/AV-12 |
| F2 | 低 | §1 行号失效、§9 计数过期 | 已逐条刷新（16 处行号）+ 计数改为实测值 |
| F3 | 低 | `apiKeysForbidden` 只置不清：先 403 后 500 仍显示「无权限」，故障被伪装成权限问题 | **已修**（`setApiKeysForbidden(status===403)`）+ 用例 + 变异 ⑧ |
| F4 | 低 | AV-4 用 `baseURL+url` 拼串比对，基址漂移可能仍通过 | 已补 `expect(api.defaults.baseURL).toBe("/api")` |
| F5 | 低 | AV-3 只断言「非 403」，401/404/405 也能通过 | 已改为断言 200（实测两条端点均 200） |
| F6 | 中 | 「未拦」4 条用例真实外连 `localhost:8000/8080/443`，若恰好有服务在跑会真的发起登录/同步 | 已改为指向本进程 `httptest` 假服务（返回 401）；用例从 ~9s 降到 0.12s，且无外部副作用 |
| F7 | 低 | 限流用例固定源 IP + 进程级桶，`go test -count=2` 第二轮必红 | 已改为每次运行独立 IP（`probeIP()`），`-count=2` 实测通过；固定 IP 的变异反证见 ⑨ |
| F8 | 低 | 403 分支的 `error.response` 契约无守卫（若拦截器改抛裸 Error，权限 Alert 静默失效） | 已补 `api.test.ts`「reject 保留 error.response」用例 |

审计确认无误的项（正向证据）：`require.Len(logs, 2)` 不受同包用例污染（每个用例独立内存库）；`findByDisplayValue` 取的是后端响应映射值而非组件内部状态；`apiKeyApi` 四条路径与 `api_key_handler.go` 路由组逐条对齐。
