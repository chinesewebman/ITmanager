# M31：OpenAPI 契约完整度（G-37 残余：22 条真实路由未文档化 + 契约门禁）

> 承 M28「通知链路保真」。M28 清单里的 G-39（告警通知路由）卡在产品语义上（「告警命中哪条规则」未定），
> 可自主推进的只剩 G-37 的**残余部分**。本轮只做 G-37 残余，不碰 G-39。

## 0. 规模决策（先定规模，再写内容）

| 项 | 值 |
|---|---|
| 改的文件 | `backend/internal/api/openapi.yaml`（主）、`backend/internal/api/routes_integration_test.go`（守门）、`frontend/src/services/api.types.ts`（生成物）、`frontend/package.json`（1 条 script） |
| 是否动生产 Go 代码 | **否**（零 handler 改动、零路由改动、零迁移、零运行时行为变更） |
| 是否动 DB | **否** |
| YAML 增量 | **21 个 path key / 22 个 operation**（`/auth/api-keys` 一条 key 承载 GET+POST）+ 约 8 个新 schema |
| 拆几步 | 5 步，每步一个 commit（见 §2.3） |

选这个规模的理由：本轮只碰**文档 + 门禁断言 + 一条 npm script**，爆炸半径低（无运行时行为变更），
但收益是结构性的 —— 把「OpenAPI 是契约单一事实来源」从口号变成**可执行的断言**。
反过来说，只要门禁是单向的（现状，见 §1.5），漂移就一定会重新长出来：G-37 自己就是漂移的产物。

## 1. 事实与实测（先量，不引用 TODO 的旧数字）

### 1.1 量化差集（脚本从 `routes.go` 提取分组前缀 + 方法，与 `openapi.yaml` 的 `paths` 对照）

```
真实 /api 路由（方法,path 去重）：93
openapi.yaml 记录（方法,path）：71
有路由、无文档：22        ← 本轮要清掉的
有文档、无路由：0         ← rev2 的幻影断言在守，方向是好的
```

**非 `/api` 的路由（不在 spec 的 `servers.url` 前缀内，也不参与比对）**：

| 路由 | 注册点 | 性质 |
|---|---|---|
| `GET /healthz`、`GET /readyz` | `routes.go:155-156` | 探针（K8s/manifest 用） |
| `GET /metrics` | `routes.go:157-163` | **条件注册**：`if cfg.Server.MetricsEnabled`，默认 `false` |
| `GET /openapi.yaml`、`GET /swagger/*any` | `swagger.go:24,30` | 契约自服务（安全性见 §1.7） |
| `GET|HEAD /static/*filepath` | gin 静态资源 | 框架路由 |

> rev1 本文档写「非 /api 3 条」，是把 `routes.go` 的字面量当成了运行时事实（漏了 swagger/static，
> 且把默认关闭的 `/metrics` 算成常驻）。**审查①实测更正**：以 `SetupRouter(t).Routes()` 为准。
> 这不影响 93/71/22/0 四个数字（它们只统计 `/api` 前缀内）。

### 1.2 对 G-37 原文的事实更正（**两处已过时 + 一处算术错，必须写下来，否则后人会去修不存在的问题**）

| G-37 / TODO 原文（2026-09-09） | 当前实测 | 结论 |
|---|---|---|
| spec 写的是 `/notification/channels`（真实 `/notification-channels`） | `openapi.yaml:887/916/955` 就是 `/notification-channels…` | **已由 rev2（`3988375`）修掉** |
| 渠道 test 是 `post`（真实 `put`） | `openapi.yaml:955` 是 `put` | **已修** |
| 「**29 条**真实路由仍未文档化」 | `TODO.md:312` 表头写 29，但它**自己列的清单只有 27 条**（表头与正文互相矛盾）；rev2 前实测 **28** 条 | 表头数字不可用 |
| （rev2 修了什么） | rev2 收口 **6** 条幻影：5 条 notification-channels **+ `/alerts/{id}/acknowledge` → `/alerts/{id}/ack`** | 净减 6 → 22 |

即：**G-37 标题描述的问题（「路径与实际路由不一致」）已经不存在了**。剩下的是 rev2 自己划出的残余
——「文档完整度，非契约正确性」。本轮做且只做这个残余。

### 1.3 22 条未文档化路由（按组，逐条对应 §1.1 差集，无漏无多）

| # | 组 | method path |
|---|---|---|
| 1-3 | `/alerts` 批量 | `POST /alerts/bulk-ack`、`POST /alerts/bulk-resolve`、`POST /alerts/bulk-delete` |
| 4-6 | `/assets` | `GET /assets/export`、`POST /assets/{id}/retire`、`POST /assets/{id}/restore` |
| 7 | `/audit-logs` | `GET /audit-logs` |
| 8-13 | `/auth` 身份与密钥 | `GET /auth/me`、`PUT /auth/password`、`GET /auth/api-keys`、`POST /auth/api-keys`、`DELETE /auth/api-keys/{id}`、`PUT /auth/api-keys/{id}/revoke` |
| 14 | `/health` | `GET /health`（在 `/api` 组内，与组外 `/healthz` **不是**同一条） |
| 15-22 | `/integrations` | `GET /integrations/status`、`PUT /integrations/{zabbix,glpi,netbox}`、`POST /integrations/{zabbix,glpi,netbox}/test`、`POST /integrations/sync` |

### 1.4 与既有 TODO 的重叠（避免重复立项；rev1 只列了前两条，审查②补全）

| 既有条目 | 与本轮的关系 | 处置 |
|---|---|---|
| `TODO.md:58`「**`/auth/me` 不在 `openapi.yaml`** — `capabilities` 进不了 `gen:api` 生成类型」 | 正是 1.3 第 8 条 | **本轮关闭**，该行改为指向本文档 |
| `TODO.md:59`「`GET /integrations/status` 回传集成 URL 与 Zabbix 用户名」 | 是**响应内容**的安全议题，与「有没有被文档化」无关 | 本轮只补文档，**不**改响应字段（§6.2） |
| `TODO.md:315` **G-41**（`zabbix_truncated` 裸字符串约定，「走 openapi/`gen:api` 让类型系统兜住」是它的候选路②） | 本轮要新增 **`POST /integrations/sync`** 这条 path，两者必然交叉 | 本轮该 path 用**宽松 schema + description**（§1.2 的诚实边界），**明确不关闭 G-41** |
| `TODO.md:141-143,196`「type-safe 推进：3/13 → 10/13 服务方法仍 `data: any`」 | 本轮重生成 `api.types.ts` 会牵动它 | 不属本轮范围；但 §2.4-D-2 要求生成物随每个 commit 提交，**不引入**新的 `any` 收口 |

### 1.5 现状守门测试的能力边界（`routes_integration_test.go`）

`TestRoutes_OpenAPI无幻影路径`（`:406`）：

- 方向 **spec ⊆ 路由**（只挡幻影端点），注释（`:404`）明写「不要求每条路由都已文档化」。
- 真实路由从 `SetupRouter(t).Routes()` 取，`:id` 归一成 `{id}`（`:414`）；spec 走 **HTTP 端点** `/openapi.yaml` 取
  （与 `/swagger` 消费同一份 embed 产物，不是磁盘上的另一个副本）。
- 哨兵：`assert.GreaterOrEqual(t, checked, 60)`（`:452`）—— 只防「spec 被整体清空 → subset 断言空转」，
  **不覆盖单条 path 被删**。

**审查①实测的两个关键前提**（决定了 §5.3 变异表怎么写）：

| 变异 | 现有测试的反应 | 含义 |
|---|---|---|
| 从 spec **删掉**一条 path（实测删整段 `/tickets:` 726–773 行） | **绿**（`ok ... 0.115s`） | 反向是真空的 → M31-M1 是**真·新方向的守卫** |
| 改一条 spec 里 path 的**动词**（`/tickets/{id}` 的 `put`→`post`） | **红**（`:447`「声明了 POST /tickets/{id}，但未注册」） | 改动词会先造出一条**幻影**，被**旧**断言抓住 → **M31-M2 不能单独证明新方向** |

> rev1 本文档把 M1/M2 并列称为「新增方向的守卫」，对 M2 不成立 —— 已按审查①更正（§5.3）。

### 1.6 openapi.yaml 的真实消费方，以及「今天谁保证它合法」

| 消费方 | 位置 | 吃错契约的后果 |
|---|---|---|
| 前端类型生成 | `frontend/package.json:17` `gen:api` → `api.types.ts` | 生成错类型 → 要么 `tsc` 红（好），要么生成出**更宽的类型**而静默放行（坏） |
| **CI 生成物漂移门禁** | `.github/workflows/ci.yml:193-198`：重跑 `gen:api` 后 `git diff --exit-code -- src/services/api.types.ts` | **改了 spec 忘记重生成 → CI 直接红**（§2.4-D-2 的根据） |
| 前端手写类型对齐 | `frontend/src/types/index.ts:147-149`（`TicketHistoryDriftOK` / `…Rev` 双向可赋值断言；`:115-122` 是解释性**注释**，不是断言） | 契约与手写类型漂移 → `tsc` 红。**注意覆盖范围只有 `TicketHistory` 一组**，不是「手写类型」全体 |
| Go 测试 | `backend/internal/integration/glpi_e2e_test.go:205` `os.ReadFile("../api/openapi.yaml")` + `:198-228` 解析 `Ticket.priority` 的 `enum` | 删/改 enum → 测试红（M26 建立，**运行时解析**，不是抄一份） |
| Swagger UI | `swagger.go:13` `//go:embed openapi.yaml`，`/swagger/index.html` + `/openapi.yaml` | 人看错文档 → 按错的调 |

**关于生成物覆盖率（审查①实测更正 rev1）**：`api.types.ts` 与 spec 的 path 集合**完全相等**
（51 个 path key，双向差集为空）。所以缺口**不在生成器**，而在 **spec 本身只覆盖 71/93 条真实路由**。

**关于「合法性谁保证」（审查②发现的整篇缺口）**：

- 现有 Go 测试只做 `yaml.Unmarshal`（**语法级**，`routes_integration_test.go:430`）。
- `@apidevtools/swagger-cli@^4.0.4` **已经在 `frontend/package.json:33`（devDependency）**，`package-lock.json` 有锁。
- 但实测：**仓库没有 `.pre-commit-config.yaml`**，`.git/hooks` 只有 `*.sample`，`ci.yml` 里没有 swagger 步骤，
  `package.json` 的 `scripts` 里没有 validate 脚本 —— **装了，从未被任何自动化调用**。
- `README.md:114` 称 openapi.yaml「swagger-cli validate 通过」、`TODO.md:141,143` 称 CI + pre-commit hook 在跑它 ——
  **都是陈旧声明**。

→ 处置见 §2.4-D-3：恢复它（零新依赖，本来就在），而不是新引入校验器。

### 1.7 公开可达性（安全前置事实，决定 §2.4-D-6）

`/openapi.yaml` 与 `/swagger/*any` 注册在**根引擎**（`swagger.go:24,30`；`routes.go:459` 的 `RegisterSwagger(r)` 在 `api` 组**之外**）：
无 `AuthMiddleware`、无 IP 白名单、无限流、无审计。仓库自带测试亲自钉死匿名可达
（`routes_integration_test.go:381-392` 不带 token 请求 `/openapi.yaml`，断言 200）。

**生产拓扑碰巧掩盖了它**：`docker-compose.yml` 里 `api` 只绑 `127.0.0.1:8080`，唯一对外入口 `web` 的 nginx 只 `proxy_pass /api/`。
这是部署巧合，不是控制 —— 任何裸机反代 / 把 `/` 全转发 / 直连后端端口都会匿名暴露。

**本轮往 spec 里补 22 条 path 的边际新增暴露（诚实核算，不夸大）**：
22 条里 **18 条已经在公开的 JS bundle 里**（`frontend/src/services/api.ts` 明文含 `/auth/password`、`/auth/api-keys*`、
`/alerts/bulk-*`、`/integrations/*`、`/assets/{id}/retire|restore`）。
真正新增的只有 4 条：`/audit-logs`、`/auth/me`、`/assets/export`、`/health`。
未认证攻击者得到的是「一份 93 条端点+动词+参数名的枚举表」（价值：收敛扫描面），
**拿不到**调用权（全部 401）、**拿不到**任何明文值（spec 无真值）。
**结论：不构成本轮的高危项**，但它引出一个必须**显式**做的决策 → §2.4-D-6。

## 2. 修法

### 2.1 三个候选

| 方案 | 做法 | 权衡 | 结论 |
|---|---|---|---|
| **A. 补齐 + 集合相等门禁** | 按组补 22 条 YAML（复用既有 schema，缺口新建），把守门断言从 `spec ⊆ 路由` 升为**集合相等** | 工作量集中在 YAML（机械）；门禁是「加路由不写文档就红」的**硬约束** | **推荐** |
| B. 由路由反向生成 spec | 用代码生成器从 `routes.go` 产出 paths | 丢掉手写的 schema/描述/示例；引入生成器依赖（违反「不加新依赖」）；生成物不可读、review 无意义 | 否决 |
| C. 只加白名单断言，不补 YAML | 断言「未文档化集合 == 已知白名单」 | 白名单会长久腐烂（今天 22 条，明天 30 条），不产生任何可读文档；且理由字符串是软控制 | 否决 |

### 2.2 判据：为什么是「集合相等」而**不**留 allowlist（D-1）

rev1 本文档设计过 `undocumentedRoutes map[string]string`（路由→理由）的豁免名单。**审查②与审查③各自独立指出它坏了**，
现否决，理由四条：

1. **先天为空即死代码**：本轮就把 22 条补完，allowlist 从第一天起是空 map；而配套的「理由必须非空」用例
   在空 map 上恒绿（**vacuous**）—— 正是本项目一直在防的假绿形态。
2. **它是把「未文档化」洗成合法态的后门**（审查③ R-5）：新加一条敏感路由（甚至误挂在 `api` 组而非 `protected` 组
   → 无鉴权）的开发者，断言红时只需塞一行 `"GET /foo/secret": "调试端点"`，断言即绿，
   而这条路由**永不进契约、永不被 review、Swagger 上永不出现**。
3. **理由字符串是软控制**：无格式要求、无 TODO 引用、无有效期、无上限，挡不住上面那条。
4. **摩擦是刻意要的**：唯一出路「补 spec」正是期望行为。若将来真出现「不该进公开契约的端点」，
   那时的正确做法是带具体场景重新讨论（可能是在 spec 里用 `x-internal: true` 标注并让前端生成器过滤），
   **而不是**在一条测试里开豁免口子。

→ **D-1：断言 = 集合相等（真实路由 == spec 声明的 (method,path)），不设 allowlist。**

### 2.3 分步（每步一个 commit，每步门禁全绿）

| 步 | 内容 | 每步的验收命令 |
|---|---|---|
| 1 | 本文档（需求 + 三路审查 + 处置） | —（纯文档） |
| 2 | 补 YAML：`/integrations`（8）+ `/health`（1）；新建 `IntegrationStatus`/`Health`/`SyncResult` schema（**宽松形态**，见 D-4） | `go test ./internal/api/ -run OpenAPI -count=1`（既有单向断言会**逐条**核新 path，写错即红）+ `gen:api` + CI diff |
| 3 | 补 YAML：`/auth`（6）+ `/audit-logs`（1）+ `/alerts` 批量（3）+ `/assets`（3）；**本步新建** `APIKey`/`AuditLog`/`BulkResult` 等（不复用步 2 的，步 2 只产出 integration/health 族） | 同上 |
| 4 | 断言升级为集合相等（新增用例，**保留** `TestRoutes_OpenAPI无幻影路径`：它挡的是另一个方向，不可替代）+ `gen:api` | 见 §5.1 全量 + §5.3 变异 |
| 5 | 契约的**鉴权语义**补全：顶层 `security: [{BearerAuth: []}]` + 公开操作显式 `security: []`（D-5） | 同上 |

**每一步都要跑 `gen:api` 并提交生成物**（D-2）：CI 已有硬的漂移门禁（`.github/workflows/ci.yml:193-198`），
步 2/3 改了 YAML 而不重生成，**CI 会红**。rev1 把重生成排到第 4 步，是错的。

**关于「第 2/3 步只加 YAML，既有断言不受影响」**：这句话 rev1 写错了。现有单向断言遍历 `spec.Paths` 的**每一条**
并断言真实路由存在（`routes_integration_test.go:441-450`），所以步 2/3 新写的 path 只要拼错（多一个不存在的方法/子路径）
**立刻红** —— 这是**好事**（免费的即时校验），但方向的预期要写对。

### 2.4 同轮必须一并做的四件事（否则门禁装上了、地基是歪的）

**D-2 生成物提交粒度**：见 §2.3。

**D-3 恢复 `swagger-cli validate`（零新依赖）**：在 `frontend/package.json` 加一条
`"validate:api": "swagger-cli validate ../backend/internal/api/openapi.yaml"`，并写进 §5.1 门禁。
它回答的是 §1.6 那个整篇没说的问题：**今天没有任何自动化在保证 openapi.yaml 是合法 OpenAPI 文档**
（Go 测试只做 YAML 语法解析）。依赖早就在，本轮的 YAML 增量（~400 行手写）正是最需要它的时候。
顺带订正 `README.md:114` 的陈旧声明（它已声称「validate 通过」）。

**D-4 「响应形态拿不准」的诚实写法**：本轮的 schema **必须来自 handler 实读**
（打开 `c.JSON(http.StatusOK, gin.H{...})` 逐个字段抄）。拿不准的**不编造字段** —— 用
`type: object` + `additionalProperties: true`（或对自由 map 如此），并在 `description` 里写明
「形态未收口，见 TODO G-41」。**example 一律占位符**（`"http://example.invalid"`、`"<token>"`），
**禁止**真实主机名/用户名/token —— 见 R-1 与审查③ R-7。

**D-5 契约的鉴权语义**：spec 有 `securitySchemes.BearerAuth`（`openapi.yaml:1638`）但**顶层无 `security:`**，
71 个 operation 里只有 2 个写了 `security:` —— 按 OpenAPI 语义，这等于声明「其余 69 个端点无需鉴权」。
把 spec 升为「单一事实来源 + 双向门禁」之后，这个错误就**载重**了（机器读 spec 会得出 API 是开放的）。
修法机械且廉价：顶层 `security: [{BearerAuth: []}]`，对三个真正公开的操作显式 `security: []` ——
`GET /health`（`routes.go:210`，`api` 组内、无 AuthMiddleware）、`POST /auth/login`、`POST /auth/logout`
（后两者在 `auth` 组，仅 login 挂限流+审计）。
 **鉴权状态从 `routes.go` 的组归属推导，不猜测**；`protected` 组下的 90 条一律需要 BearerAuth。

**D-6 Swagger / `/openapi.yaml` 的匿名可达 → 本轮不修，登记 G-47（显式决策，非默认）**：
- **改什么**（若做）：`RegisterSwagger(r)` 外层加 `if cfg.Server.Mode != "release"`，release 模式不注册
  Swagger UI 与 `/openapi.yaml`（~3 行）。测试跑在 `gin.TestMode` 下，两个读 `/openapi.yaml` 的用例不受影响。
- **为什么本轮不做**：① 当前部署拓扑下**不可达**（§1.7：api 只绑环回 + nginx 只转 `/api/`）；
  ② 它改的是**运行时行为**（release 部署下 Swagger 消失），需要独立的需求文档 + 行为突变告知，
  塞进一个「纯文档」轮会污染改动面；③ 超出 G-37 范围。
- **但它必须是一个决策，不是一个默认** —— 已按此登记为 **G-47**（TODO），并在 `08-部署运维.md`
  写明「不得直接暴露后端端口；spec/Swagger 有意公开」。
- **本轮已核实的边际风险**：§1.7 的诚实核算 —— 22 条里 18 条早在公开 JS bundle 中，
  真正新增 4 条，且无值、无调用权。**不构成本轮阻塞项**。

### 2.5 明确不改的边界（防止「顺手扩大范围」）

1. **不改任何 handler / 路由 / 响应结构**。发现响应形态难写进 OpenAPI 就用宽松 schema（D-4），
   **不为「好写文档」而改生产代码**。
2. **不碰 `/api/integrations/status` 的响应内容**（`TODO.md:59`）—— 独立的安全议题。
3. **不引入任何新的 OpenAPI 校验器 / 代码生成器依赖**（`swagger-cli` 是**已存在的** devDependency，见 D-3）。
4. **不补非 `/api` 的端点**（§1.1 表：探针 / metrics / swagger / static）—— 不在 spec 的 `servers.url` 前缀内，
   现有测试也已显式跳过，属运维探针而非 API 契约。
5. **不做「由路由生成 spec」**（方案 B，§2.1 已否决）。
6. **不设 allowlist**（D-1）。
7. **不顺手修** G-39/G-40/G-41/G-42/G-45/G-46 —— 各有独立立项理由，见 TODO。

## 3. Risk

**R-1（最可能）：把响应形态写错 —— 比不写更糟。**
补 YAML 时凭印象写 schema，spec 记的响应与 handler 实际 `c.JSON` 的结构不一致。
失败模式具体长这样：`gen:api` 据此生成 `api.types.ts`，前端按错误的字段名取值 →
TS 不报错（字段存在但语义不同）或报错在**无关**位置，排查方向被带偏；
Swagger 上写着「返回 X」，运维照 X 写脚本，实际是 Y。
**加剧因素（审查③ R-7）**：`openapi.yaml` 现存 5 处 `example:`（如 `:120`、`:131`），说明作者有写 example 的习惯；
而本轮要新增的正是 `IntegrationStatus`（含 `url`/`user`）、`UpdateZabbixRequest`（URL/user/password）、
`UpdateNetBoxRequest`（token）、`UpdateGLPIRequest`（app_token/user_token）这些「照抄配置就变成泄漏」的 schema。
**缓解**：① schema 必须来自 handler 实读，拿不准用宽松对象 + description（D-4）；
② **example 一律占位符**，review 时 `grep -n 'example:'` 逐条过；
③ 步 4 的 `tsc` + `vitest` 是第二道网 —— 但它只挡**类型层**不一致，不挡语义错。

**R-2：集合相等卡住未来开发，被用「关掉测试」绕过。**
开发加一条路由，`go test ./...` 立刻红（这是设计意图），但若失败消息只说「路由未文档化」，
作者的合理反应可能是删断言而不是补文档。
**缓解**：断言消息给出**可直接复制的补法**（`在 openapi.yaml 的 paths 下补 "<METHOD> <path>"，再跑 npm run gen:api`），
并说明「若不打算进公开契约，请在 TODO 登记并说明理由」——把绕过的成本抬到高于补文档。

**R-3：生成物 `api.types.ts` 的 diff 夹带意外改动。**
`openapi-typescript` 版本差异或 spec 里既有的松散定义，可能让重生成顺带改动**本轮没打算碰**的类型，
甚至与手写类型（`types/index.ts:147-149`）冲突。
**缓解**：重生成后 `git diff --stat` 逐块看，只保留与新增 path/schema 相关的 hunk；出现无关 hunk 先查
是不是 `openapi-typescript` 版本漂移（`package-lock.json` 未动则大概率不是），是则**停下报告，不硬塞**。
跑 `tsc --noEmit` + `vitest` 验收。

**R-4（审查②新发现）：本地门禁与 CI 脱节 —— 本地全绿、CI 红。**
CI 有两条本地 §5.1 没列的步骤：`gen:api` 漂移检查（`ci.yml:193-198`）与……**其实只有这一条**
（`swagger-cli` 尚未接线，正是 D-3 要补的）。
**缓解**：§5.1 把 CI 的两条都写进本地清单，今后凡动 `openapi.yaml` 必跑。

R-1 与 R-2 是本轮两个**方向相反**的风险（一个怕写错、一个怕写不够），缓解措施不共享，故分开列。

## 4. Where

| 文件 | 动作 |
|---|---|
| `backend/internal/api/openapi.yaml` | 加 21 个 path key / 22 个 operation（按 §2.3 分步）+ 约 8 个新 schema + 顶层 `security`（D-5） |
| `backend/internal/api/routes_integration_test.go` | 新增「集合相等」用例（**保留**现有幻影用例） |
| `frontend/package.json` | 加 `validate:api` script（D-3） |
| `frontend/src/services/api.types.ts` | `npm run gen:api` 重生成（**每个动 spec 的 commit 都带**，D-2） |
| `README.md` | 订正 `:114` 的陈旧声明（「swagger-cli validate 通过」→ 指向 `npm run validate:api`） |
| `TODO.md` | G-37 残余结案；`:58` `/auth/me` 结案；新增 **G-47**（Swagger 暴露面，D-6） |
| `CHANGELOG.md` | 记「新增 22 条 path 文档 + 集合相等门禁 + swagger-cli 接线」 |
| `08-部署运维.md` | 写明「不得直接暴露后端端口；spec/Swagger 有意公开」（D-6 的落地部分） |
| `docs/TRAPS.md` | 若过程中踩到新 trap 则登记（不预设编号） |

## 5. 验证清单

### 5.1 门禁（含 CI 的两条，R-4）

```bash
cd backend && gofmt -l ./internal ./cmd ./tests && go vet ./... && go build ./... && go test ./... -count=1
cd .. && ./scripts/db_smoke.sh                     # 本轮无 schema 变更，仍跑（回归面）
cd frontend && npm run validate:api                # D-3 新增（= CI 缺失的那条）
cd frontend && npm run gen:api && git diff --exit-code -- src/services/api.types.ts   # = ci.yml:193-198
cd frontend && npx tsc --noEmit && npx eslint src --ext .ts,.tsx && npx vitest run
```

### 5.2 守门用例

| 用例 | 断言 | 为什么不是空转 |
|---|---|---|
| `TestRoutes_OpenAPI契约集合相等`（步 4 新增） | 真实路由集合 **==** spec 声明的 (method,path) 集合，**两个方向都断言** | 两侧都从**运行时**取（`Routes()` 与 `/openapi.yaml` HTTP 端点）；`require.NotEmpty` 两侧都加，任一侧被清空先被拦 |
| `TestRoutes_OpenAPI无幻影路径`（**保留**） | spec ⊆ 路由 | 与上一条方向不同、不可替代：它挡的是「spec 写了不存在的端点」（按 spec 生成的客户端 404） |

**不新增**「白名单理由必须非空」用例 —— allowlist 已被 D-1 否决。

### 5.3 变异反证（T-31：每条先 `go build`，编译失败判 INVALID 不算数）

> 下表是**第 4 步实现后**的状态，不是现状（§1.5 已实测当前只有 M2 会红，且红在**旧**断言上）。

| 编号 | 变异 | 必须红的断言 | 能否单独证明新方向 |
|---|---|---|---|
| M31-M1 | 从 `openapi.yaml` **删掉**一条已文档化 path（如 `/tickets`） | 集合相等（路由侧有、spec 侧无） | **能**（现状实测绿，§1.5） |
| M31-M2 | 改一条 spec 里 path 的**动词**（`put`→`post`） | 集合相等 **+ 旧幻影断言 `:447`** | **不能** —— 会先造出幻影被旧断言抓住；保留它只为证明「两个方向都在守」，不作新方向的证据 |
| M31-M3 | 从 `routes.go` **删掉**一条已文档化路由的注册 | 集合相等（spec 侧有、路由侧无） | **能**（这是旧断言**完全**覆盖不到的方向：删路由后旧断言遍历 spec 仍全绿） |
| M31-M4 | 把某条 spec path 的方法改成大写/加非法键 | `yaml.Unmarshal` 或集合比对红 | 见 §1.6 合法性（配合 `validate:api`） |

M31-M3 是本轮最关键的变异：它证明**新断言抓的是旧断言结构性看不见的方向**（旧断言遍历 spec、只看幻影）。

### 5.4 台账动作

1. `TODO.md`：G-37 标记结案（附交付摘要 + 变异编号 + 行为突变），`:58` 行结案并指向本文档，新增 **G-47**。
2. `CHANGELOG.md`：新增条目（含「spec 从 71 → 93 条 operation」的数字与 `validate:api` 接线）。
   **行为突变告知**：本轮**无运行时行为变更**（零 Go 代码改动）—— 若采纳 D-6 之外的方案则不适用；D-6 已明确不做。
3. `README.md:114` 订正。
4. `08-部署运维.md`：补「不得直接暴露后端端口」+ spec/Swagger 的有意公开说明。
5. `docs/TRAPS.md`：按实际踩到的登记。

### 5.5 前端

`gen:api` 后 `npx tsc --noEmit` 必须绿（`types/index.ts:147-149` 的双向可赋值断言是现成的第二道网）；
`vitest run` 只跑受影响文件。

## 6. 不做的事（明确排除，避免范围蔓延）

见 §2.5（7 条）。**另加两条与安全相关、必须显式排除而非默认略过的**：

8. **不修 Swagger / `/openapi.yaml` 的匿名可达** —— 显式决策延后，登记 **G-47**，理由见 §2.4-D-6。
9. **不把 `/audit-logs` 的 schema 当作敏感信息处理** —— 审查③已诚实分级：该 spec 只公开「平台记录 IP/UA/path/request_id」
   这一事实（几乎所有后台的默认行为），数据本身要 `canAudit`（admin/ops_admin/auditor）才能读，**不构成风险**。
10. **不为「类型好看」把自由形态收口**（`/integrations/sync` 的 `map[string]int`、`/integrations/status` 的自由 map）——
    那会与 G-41 撞车且需要先改响应结构，属独立改动（§1.4）。

## 7. 审查记录（三路对抗审查 → 逐条复现 → 处置）

三路独立审查（① 正确性 ② 边界与一致性 ③ 安全）全部**独立复现**了文中的量化断言（含把探针塞进
`internal/api` 直接读 `SetupRouter(t).Routes()` 与 `/openapi.yaml` embed 端点做集合比对，跑完删除）。
**93/71/22/0 四个数字、§1.3 的 22 条清单、§1.5 的哨兵边界、以及全部消费方路径句，均实测坐实。**

### 采纳（rev1 的错误，已全部改入上文）

| # | 审查指出 | 位置 | 处置 |
|---|---|---|---|
| 1 | 「非 /api 3 条」是读字面量猜的；实测 6 条注册路由，且 `/metrics` 条件注册（默认关） | §1.1 | 改为表格 + 标注条件注册 + 说明不影响 93/71/22/0 |
| 2 | §1.2 第三行算术与归因都错：rev2 前实测 **28**（不是 29），rev2 收口 **6** 条（5 notification-channels **+ `/alerts/{id}/acknowledge`**），rev1 漏了 ack 那条 | §1.2 | 重写为完整事实链，并记下 `TODO.md:312` 表头 29 与自身清单 27 的自相矛盾 |
| 3 | **M31-M2 不是新方向的守卫**：改动词会先造出幻影，被**旧**断言（`:447`）抓住 —— 实测红了，但红的不是新断言 | §1.5 / §5.3 | 新增「实测两个前提」表；M2 显式标注「不能单独证明新方向」；补 M31-M3（删路由）作为真正的新方向证据 |
| 4 | `types/index.ts:115-122` 是**注释**，断言在 `:147-149`，且只覆盖 `TicketHistory` 一组 | §1.6 | 更正行号与覆盖范围 |
| 5 | 「`gen:api` 不覆盖全量，实际见 §5.3」—— 描述错（实测 51/51 全覆盖）且 §5.3 交叉引用断裂（变异表里没有覆盖率数字） | §1.6 | 重写：缺口在 **spec 本身**（71/93），生成器全覆盖 |
| 6 | **CI 已有 `gen:api` 漂移硬门禁**（`ci.yml:193-198`），rev1 却把重生成排到第 4 步 → 第 2/3 步提交即 CI 红；且 §5.1 本地门禁漏列这条 | §2.3 / §2.4-D-2 / §5.1 | 改为**每个动 spec 的 commit 都重生成**；§5.1 补入 CI 两条 |
| 7 | §2.3「第 2/3 步只加 YAML，既有断言不受影响」不准确：现有断言**逐条**核新 path | §2.3 | 更正为「写错立刻红 —— 这是免费校验，是好事」，并给每步验收命令 |
| 8 | §6.3「不引入 swagger-cli」与事实冲突（已在 `package.json:33`）；且「今天谁保证 openapi.yaml 合法」整篇没答 | §1.6 / §2.4-D-3 | 改为「**恢复**它」（零新依赖），并订正 README/TODO 的陈旧声明 |
| 9 | allowlist 三处自相矛盾：「最终为空」vs「必须带理由」用例（交付态恒绿 vacuous）vs「双向相等」命名（真实判据是 `路由 == spec ∪ allowlist`） | §2.2-D-1 | **整体否决 allowlist**，判据改为纯集合相等，四条理由 |
| 10 | §1.4 重叠清单不全：漏 **G-41**（`/integrations/sync` 必然交叉）与 type-safe 推进条目 | §1.4 | 补全为四行表，G-41 显式「本轮不关闭」 |
| 11 | §2.3 第 3 步括号写错（「复用上一步的 `APIKey`/`AuditLog`」—— 那是第 3 步新建的） | §2.3 | 更正 |
| 12 | §0「22 条 path」不精确：是 22 个 **operation** / **21 个 path key** | §0 | 更正 |
| 13 | 变异表放在 §5.3（计划态）与同目录既有模式（`LOG-INJECTION` §8.2、`REDACT-BOUNDARY` §8.2 放实现后）不一致 | §5.3 / §8 | 表头标注「第 4 步实现后的状态」；§8 预置子节 |

### 采纳（审查③安全，全部改为显式决策而非默认）

| # | 结论 | 处置 |
|---|---|---|
| S-1 | `/openapi.yaml`/`/swagger` **匿名可达**（根引擎、无鉴权/限流/审计），但生产拓扑碰巧掩盖；本轮补 22 条 path 的**边际新增暴露很小**（18/22 早在公开 JS bundle，真正新增 4 条，无值、无调用权） | §1.7 记事实；**升级为显式决策 D-6** → 登记 G-47 |
| S-2 | 该匿名通道「该不该存在」**从未被决策**，本轮把它从「71 端点偶然泄漏」变成「93 端点制度性公开」 | D-6 三选一里取「本轮不修 + 登记 + 部署文档写死」，**不默认略过** |
| S-3 | allowlist 是「永不进契约、永不 review」的后门，理由字符串是软控制 | D-1 直接否决 allowlist（与审查②的结论合流） |
| S-4 | 新 schema 的 `example:` 可能被填真实主机名/用户名/token（本轮要新建的正是 `UpdateNetBoxRequest.token` 这类） | R-1 缓解②：example 一律占位符 + review 时 `grep 'example:'` |
| S-5 | spec 顶层无 `security:`，71 个 operation 仅 2 个声明鉴权 → 机器读 spec 会得出「69 个端点公开」 | 升为 **D-5**（步 5 做），鉴权状态从 `routes.go` 组归属推导 |
| S-6 | 生成物进浏览器 bundle 算不算泄漏 | 审查③已诚实判定**不构成风险**（`apiClient.ts:13` 是 type-only import，`tsc` 编译期擦除；`api.types.ts` 内 `export const/enum/function/class` 零处）→ §6 第 9/10 条同类，明确排除 |

### 驳回

| # | 提议 | 驳回理由 |
|---|---|---|
| 1 | 给 allowlist 加「上限 ≤5」「理由须带 TODO 编号」「顺带断言已挂 AuthMiddleware」三道加固 | 加固一个**已被否决**的机制没有意义（D-1）。审查③的关切（敏感路由借白名单逃过 review）由「根本没有白名单」更彻底地解决 |
| 2 | 审查③建议的「release 模式不注册 Swagger」直接本轮做（~3 行、零风险） | 改动确实小，但它**改运行时行为**（release 部署下 Swagger 消失），按项目约定需要独立需求文档 + 行为突变告知；且当前拓扑下不可达。**降级为 G-47 显式登记**，不塞进「纯文档」轮（§2.4-D-6） |

## 8. 实现记录

> 待实现完成后填入。预置子节（对齐 `docs/FIX-PLAN-LOG-INJECTION.md` §8 与 `docs/FIX-PLAN-REDACT-BOUNDARY.md` §8）：

### 8.1 逐项 before/after（实测）
### 8.2 变异反证（M31-M1..M4，逐条先 `go build`）
### 8.3 门禁（含 `validate:api` 与 `gen:api` 漂移检查的首次实跑输出）
### 8.4 实现后审计与处置
### 8.5 残余
