# FIX-PLAN：导出保真（M32）

> 立项 2026-09-12 · 对应 TODO **G-49** · 承 M26/M27 的「保真」主题 · 前置 M31（OpenAPI 契约保真）
> 状态：**rev2（需求文档，已过三路对抗审查；审查发现已并入 §9）**

---

## 0. 结论速览

`GET /api/assets/export` 的语义是「导出资产」，实现是「导出**前 500 条**资产」，
且 HTTP 200、CSV 结构完整、末行不是错误行 —— **调用方没有任何办法知道数据被截断了**。

真实流程上它必然出错：用户环境的资产规模是上千 VM + 物理机 + 网络设备，> 500 是常态。
运维拿这个 CSV 去和 CMDB / NetBox 对账，会得出「有 N 台设备在 CMDB 里缺失」的**错误结论**，
并据此发起一轮不存在的补录。

根因不是「500 这个数太小」，而是 —— **500 是分页参数的上限，被当成了导出的数据完整性预算**（§3.1）。

---

## 1. 背景与缺陷定义

| 项 | 内容 |
|---|---|
| 接口 | `GET /api/assets/export?format=csv\|json` |
| 契约现状 | `openapi.yaml:2167-2169` 已如实标注「⚠️ **静默截断**：内部固定取**前 500 条**…已登记为独立缺陷（不在本轮修）」 |
| 缺陷 | **行数不保真**：返回的不是「匹配的全部资产」，而是「前 500 条」，且无信号 |
| 缺陷类 | 与 M27-B（Zabbix/GLPI 同步静默丢数据）同族。**关系要说准**：M27-B 立的口径是「发生截断时必须透出计数」，那是**下限**；本方案让它**真空满足**（不产生截断），标准更高，不冲突 |
| 不在本缺陷内 | 字段不保真（CSV 仅 4 列，G-50）；授权粒度（§3.3 D-6）；另一个导出端点的**无界**问题（G-51） |

---

## 2. 实测事实（取证，非推断）

| # | 事实 | 证据（file:line） |
|---|---|---|
| F1 | handler 硬写 `AssetFilter{Page: 1, PageSize: 500}` | `backend/internal/api/handlers/asset_handler.go:224` |
| F2 | `List` 另有 `if pageSize > 500 { pageSize = 500 }` 硬顶 | `backend/internal/service/asset_service.go:87` |
| F3 | 返回的 `total` 被丢弃（写成 `_`） | `asset_handler.go:224`（`items, _, err :=`） |
| F4 | 导出 handler **只读 `format`**，无 page / page_size / 过滤入参 | `asset_handler.go:221` |
| F5 | CSV 只有 4 列：`ID,Name,Type,Status`（`Asset` 模型约 35 字段） | `asset_handler.go:236-244` |
| F6 | JSON 分支同样只回 500 条，且同样不回 total | `asset_handler.go:250-253` |
| F7 | 路由挂在 `protected` 组、无额外 capability → **read 地板**（任何已认证用户可调） | `backend/internal/api/routes.go:291` |
| F8 | 唯一相关测试只验「不被 `/:id` 吞」，**行数零覆盖** | `backend/internal/api/routes_integration_test.go:293` |
| F9 | 前端无调用方（`assetApi` 无 export 方法）→ 消费方是脚本 / API | `frontend/src/services/api.ts`（`assetApi` 无 export） |
| F10 | **对照面（相反方向）**：`/alerts/false-positives/export` 走 `ListFalsePositives`，**无任何 Limit**，`Find` 全量入内存 | 调用点 `handlers/alert_handler.go:403`；`service/alert_service.go:595-607` |
| F11 | **跨文档矛盾**：`docs/FIX-PLAN-AUTHZ.md:155` 把「导出限 500 行」当作**安全属性**记入授权分析 | `docs/FIX-PLAN-AUTHZ.md:155` |
| F12 | 导出**已有审计**：`AuditLog` 是 `protected` 的**组级**中间件，不是逐路由挂载 | `routes.go:243`（组级）；豁免表 `middleware/audit.go:29-37` 不含 export |
| F13 | 导出**已有限流**：`protected` 组级 `RateLimit(100)`，键 = IP + FullPath → 导出有**独立**的 100/min 桶 | `routes.go:241`；`middleware/rate_limit.go:36-38` |

### 2.1 截断到底发生在哪一层（rev2 新增，rev1 没交代）

**当前线上截断点唯一：`asset_handler.go:224` 的字面量 `500`。**
`asset_service.go:87` 的条件是 `pageSize > 500` —— **500 不 > 500，该分支根本不执行**。

那 F2 的硬顶是什么？它是**「把入参调大」这类修法的第二道闸**：
把 handler 改成 `PageSize: 1000` 后，service 会把它压回 500，导出行为与今天逐字节相同。
所以 §4 R1 说的「改了像没改」成立，但机制是**硬顶接管**，不是两层叠加。

**由此定下修法方向：必须绕开 `List` 的分页语义，而不是改数值。**

### 2.2 为什么 F8/F9 让这个缺陷活得这么久

- **F8**：唯一测试问的是「路由匹配对不对」，不是「导出内容对不对」。行数从未被断言过。
- **F9**：没有前端按钮 → 没有人在 UI 上肉眼发现「怎么只有 500 行」。它的调用方是夜里跑的脚本，
  脚本不会抱怨，只会把 500 行喂给下游。

### 2.3 F10：两个导出端点朝**相反方向**失败

| 端点 | 失败模式 | 后果 |
|---|---|---|
| `/assets/export` | **有界但静默截断**（500） | 数据**少了**，调用方以为完整 |
| `/alerts/false-positives/export` | **无界**（`Find` 全量） | 表大时内存 / 超时 / 连接耗尽 |

一致性角度二者应一起看，但**修法不同**（见 D-7），故本轮只修前者。

---

## 3. 候选与决策

### 3.1 根因：500 是分页上限，不是导出预算

`pageSize > 500 → 500`（`asset_service.go:87`）的正当性来自**交互式列表分页**：防止一个 UI 请求把一页拉爆。
导出复用它，等于把 UI 保护参数当成了数据完整性预算。这是根因，也决定了「保留 500 + 补个截断标记」（候选 C）**不算修好**。

### 3.2 候选对比

| | A1 流式全量 | **A2 全量取回 + 缓冲后原子写出** | B 分页导出 | C 保留 500 + 透出 |
|---|---|---|---|---|
| 数据完整性 | 全 | **全** | 全（需调用方循环） | **仍不全** |
| 内存 | 有界（每批） | 全量入内存（§4 R3） | 有界 | 有界 |
| 调用方改造 | 无 | **无** | **必须改造** | 无 |
| 存量脚本 | 自动修好 | **自动修好** | **继续错**（只拿第一页） | 继续错 |
| 取数阶段出错 | 半截 CSV + 已 200 | **要么完整 200、要么 500 无 body** | 每页原子 | 同现状 |
| 传输阶段截断 | 不可判定 | **由 `Content-Length` + `X-Total-Count` 兜底** | 每页可判定 | — |
| 新代码量 | 大（引入 `c.Stream`/Flusher，仓库无先例） | 小 | 中 | 最小 |
| 判定 | 量级不需要 | ✅ **采用** | ✗ | ✗ |

**B 为什么不合格**：它把「拼完整文件」的责任推给调用方，而调用方**根本不知道要翻页** ——
对「静默截断」这个缺陷而言，B 没有修好它，只是让它更隐蔽。
（B 未来可作为 A2 之上的可选增强：同时接受 `page/page_size`。属 scope creep，§7 登记。）

**C 为什么不合格**：真正的区别不在「自设上限 vs 外部约束」——
M27-B 的 Zabbix `zabbixTriggerLimit = 5000` 也是**自设常量**（`integration/zabbix.go:158`）。
区别在**移除上限的代价**：Zabbix 那侧源数据无界、且真实总数本轮不可知（`docs/FIX-PLAN-ZABBIX-SYNC.md` §3/D-7 明确列为不做），
所以只能「透出计数」；导出这侧的源是**本地表**、上限零成本可移。
M27-B 的原则只在「发生截断」时生效，本方案让它真空满足 —— 不冲突。

**A1 为什么不选**：仓库中**没有任何 `c.Stream` / `http.Flusher` 先例**（已核实），引入流式是发明新模式；
而它的代价是错误语义变差，换来的只是「内存有界」—— 量级（§4 R3）不需要这个交换。

### 3.3 决策

| # | 决策 | 理由 |
|---|---|---|
| **D-1** | 导出走**独立的专用全量取数路径**（service 新增导出用方法，形如既有 `ListFalsePositives`），**不改** `List` 的 500 硬顶 | 硬顶服务的是交互式分页，必须保留（R4）；导出要的是「给我全部」，与「给我第 N 页」语义不同（§2.1）。**既有先例**：`ListFalsePositives`（`alert_service.go:77/595`）就是挂在 service interface 上的导出专用全量方法 |
| **D-2** | 新增 `X-Total-Count = len(items)`（**CSV 与 JSON 两分支都给**），语义 =「本次导出的**数据行数**（不含表头）」 | 机器可读的完整性声明。**刻意取 `len(items)` 而非常量外挂的 `COUNT(*)`** —— 恒等于 body 行数，消除 COUNT/SELECT 竞态（R2 因此不存在） |
| **D-3** | CSV **先写入 `bytes.Buffer`，再一次性 `c.Writer.Write`**，并检查写错误、显式设 `Content-Length` | 兑现 A2 的「要么完整、要么 500」。**这是必需而非洁癖**：现实现的 `csv.NewWriter(c.Writer)` 是边写边发、且 `_ = w.Write` / `defer w.Flush()` **丢弃错误**（`asset_handler.go:237-248`）—— 中途失败会产出**半截 CSV + 已 200**，那本身就是一种静默不完整（§9 审查 F3） |
| **D-4** | JSON 分支同样返回全量，**保留** `{code, data}` 封装形状不变 | 只修行数，不动既有响应形状 |
| **D-5** | 导出路由**加一道更紧的限流** `middleware.RateLimit(DefaultRateLimitConfig(10))` | 去掉 500 后单请求成本从 O(500) 变 O(N)，组级 100/min 允许 1 秒内突发 100 个全表导出 → 可占满连接池。**复用既有模式**（`routes.go:226` login 的 `RateLimit(5)` 同款，一行）。10/min 对「夜间脚本导一次」零影响，对突发滥用收 10 倍。**注意中间件顺序**（`routes.go:222-224` 的既有教训）：路由级限流排在组级 `AuditLog` **之后**，故被 429 拒掉的请求**仍会写审计行** —— 对批量数据端点这是**想要**的行为（留下滥用记录），不会造成登录那样的「未认证即可无限写库」 |
| **D-6** | **不加** capability 门禁；**不新增**审计；但**更正 rev1 的事实错误**：导出**已有**审计（F12）与限流（F13），缺的只是「审计里没有导出**行数**」 | 见 §3.4 |
| **D-7** | `/alerts/false-positives/export` 的**无界**问题**不在本轮** | 失败模式相反（不是少了，是可能撑爆），**修法不同**（A2 对百万级告警表不安全，需流式/分批下载），且它挂在前端按钮上有 UI 影响面 → 登记 **G-51** |
| **D-8** | **不设行数上限**（明确决策，非遗漏）；登记**触发条件** | 见 §3.5 |
| **D-9** | **不扩 CSV 列**（仍为 `ID,Name,Type,Status`） | 扩到哪些列是产品判断（对账必需字段未定），且 IP 在 `AssetNetwork` 需 join（一资产多网卡 → 展开成多行还是拼接？未定）→ 登记 **G-50** |

### 3.4 D-6 的论证（牵涉安全，单独展开）

`docs/FIX-PLAN-AUTHZ.md:155` 把「导出限 500 行」列为读地板导出的安全依据之一。
本方案去掉该上限，等于**削弱了一条被记录在案的安全缓解** —— 必须明说，不能默默改。

判断：**该缓解是名义上的，去掉它不构成提权。**
只读用户本来就能通过 `GET /assets?page_size=500` 反复翻页拿到**同样的**全量数据
（`List` 只有每页上限、无总行数上限，`page/page_size` 参数无上限校验）。
导出改变的是**获取效率**，不是**可访问范围**。因此不去掉它才是自欺：它挡不住任何人，只挡住诚实的使用者。

**且行集完全相同**（审查已逐项核验）：导出直接调同一个 `List`（无过滤参数）；
`Asset` **无** `gorm.DeletedAt`（硬删，`asset_service.go:169-171`）→ 无软删除行差异；
全仓**无 tenant**；JSON 分支返回同一 `models.Asset` 结构 → 无字段差；
`Asset` 无凭据类字段。

但下结论要留痕，所以：
1. 台账步骤更新 `docs/FIX-PLAN-AUTHZ.md:155` 的**两处**过期/错误声明 ——
   「限 500 行」需改；「文件名经 `sanitizeFilename`」**本来就是错的**
   （asset export 用的是静态字面量 `attachment; filename=assets.csv`，`asset_handler.go:234`，
   `sanitizeFilename` 只用于 `postmortem_handler.go:85,104-105`）；
2. 登记观察项 **G-52**：导出端点的能力门禁 + **审计内容**（现审计行只记 method/path/status，
   **不记导出条数**，无法从审计看出外带规模）。不在本轮做的理由：授权粒度是**独立缺陷类**，
   加门禁是行为突变，会打断现有只读运维脚本；
3. §9 请安全角度审查者专条复核 —— 已完成，主张**未被证伪**（§9 F-安全）。

### 3.5 D-8：为什么不设行数上限

审查（§9 F2/F9）提出：去掉 500 后应「补一个新上界」。**不采纳行数上限**，理由：

1. **它会变成死路**。导出端点**没有任何过滤参数**（F4）。若在超过上限时拒绝，
   错误信息只能说「请缩小范围」，而调用方**无从缩小** —— 这是把一个可用的慢接口变成一个不可用的接口。
2. **成本与既有接口同级**。`GET /assets` 每次都要 `Count(&total)`（全表 COUNT 扫描）
   再取一页；导出的增量只是「多取 N 行」。单请求成本没有量级差异，量级差异只在 N 极大时出现。
3. **量级**（R3）在真实规模内安全，且已用 D-5 的限流把并发面收回。

**残余风险显式登记（G-51）**：无并发闸门、无内存上限。
**触发条件**：生产 assets 行数 > 10^5，或出现导出滥用/进程 RSS 异常 → 届时加并发闸门
（带缓冲 channel，容量 1–2）或把导出改流式。**不提前实现**（CLAUDE.md：不为「将来可能」写代码）。

---

## 4. Risk（≥2 具体失败模式 + 缓解）

| # | 失败模式 | 触发条件 | 缓解 |
|---|---|---|---|
| **R1** | **改完看起来一样，实际没改** —— 只改 handler 传参（`PageSize: 500` → 更大值），被 `asset_service.go:87` 硬顶接管，导出行为**逐字节未变**；而测试若只 mock service 就会全绿 | 实现只动 handler | ① D-1 明确绕开 `List` 分页语义；② 守门用例必须 **真 service + 真 DB**（U1–U5），**不得**在行数断言上用 `mockAssetService`；③ 变异 M1/M3 专门红在这一处 |
| **R2** | ~~`X-Total-Count` 与 body 行数不一致~~ | — | **已由 D-2 消解**：头值取 `len(items)`（同一份内存数据），不存在独立 COUNT 的竞态窗口 |
| **R3** | **全量入内存把进程打爆** | assets 行数达 10^5~10^6 | **rev2 更正量级口径**：驻留的是 `[]models.Asset`（约 35 字段），**不是** CSV 的 4 列 —— rev1 按输出字节估算是错的量（差约一个数量级）。按结构体 + 堆字符串估算：10^4 行 ≈ 数 MB，10^5 行 ≈ 数十 MB，JSON 分支再叠一份 marshal 缓冲。**这是量级假设不是证明** → 触发条件与替代方案见 D-8 / G-51 |
| **R4** | **反向破坏**：为去 500 硬顶而把 `asset_service.go:87` 整行删掉 → **交互式列表分页失去保护**，`GET /assets?page_size=100000` 可被打爆 | 实现图省事删硬顶 | ① 硬顶**保留**；② 守门用例 **U5** 钉住「`List` 传 `page_size=600` 仍被压到 500」；③ D-1 要求导出走独立路径 |
| **R5** | 既有脚本**依赖**截断行为 | 存在按 500 行切分的下游 | 无证据（F9：仓库内无调用方）；但**「无证据」≠「不存在」**，外部部署的脚本不可见 → CHANGELOG 必须写「⚠️ 行为突变告知」 |
| **R6** | **新引入的资源耗尽面**（审查 F2） | 100 并发全量导出打满 `MaxOpenConns=100`（`database/database.go:58`）→ 全站排队 | D-5 路由级限流 10/min；残余登记 G-51（无并发闸门） |
| **R7** | **新增 service interface 方法打断 mock 编译** → 按 T-31 判 INVALID，变异表全部作废 | D-1 选了「加方法」而没补 mock | §5 把 `asset_handler_test.go`（补 `mockAssetService` 方法）列为**必改项** |

---

## 5. Where（改动面）

| 文件 | 改什么 |
|---|---|
| `backend/internal/service/asset_service.go` | 新增导出用全量取数方法（形如 `ListFalsePositives`）；**`List` 与 `:87` 硬顶不动** |
| `backend/internal/service/asset_service.go:39-49` | `AssetService` interface 加该方法 |
| `backend/internal/api/handlers/asset_handler.go` | `ExportAssets`：改用全量方法；CSV 缓冲后原子写出 + `Content-Length`；两分支都加 `X-Total-Count`；保留 `safeCSV` / nosniff / Content-Disposition |
| `backend/internal/api/routes.go:291` | 导出路由挂 `middleware.RateLimit(middleware.DefaultRateLimitConfig(10))`（D-5） |
| `backend/internal/middleware/cors.go:36` | `Access-Control-Expose-Headers` 追加 `X-Total-Count`（D-2 的头要能被跨域前端读到；否则契约声明了、运行时读不到） |
| **`backend/internal/api/handlers/asset_handler_test.go`** | **`mockAssetService` 必须补新方法，否则测试包编译失败（T-31 → R7）**；该文件只保留「错误路径 / 响应形状」单测 |
| `backend/internal/api/routes_integration_test.go` | 新增 U1/U1b/U1c/U2/U3/U4/U5；**改 `:772` 的过期理由串**「资产导出（限 500 行 + safeCSV）」 |
| `backend/internal/api/openapi.yaml:2157-2187` | 去掉「⚠️ 静默截断」段（该缺陷本步修掉），改为如实描述：全量导出 + `X-Total-Count` 语义 + `Content-Length` + 列/信封形状不变；在 `responses.'200'.headers` 声明 `X-Total-Count`（**本 spec 首个响应头声明，无先例**；`grep headers: openapi.yaml` = 0） |
| `frontend/src/services/api.types.ts` | `npm run gen:api` 重新生成并一起提交（M31 D-2，CI 有漂移门禁 `ci.yml:193-198`） |
| `TODO.md` / `CHANGELOG.md` / `docs/FIX-PLAN-AUTHZ.md:155` | 结案 G-49；新增 G-50/G-51/G-52；更正 AUTHZ 的两处错误声明；CHANGELOG 加「⚠️ 行为突变告知」 |

**已核查、落地后无需更新的文件**（防止后来者按题面去改正确的东西）：
`network-monitor-design.md:2890/:3394`（只列端点名）、`docs/FIX-PLAN-OPENAPI-CONTRACT.md:61/:131`（同上）、
`CHANGELOG.md:166`（M31 的**历史登记**，不改写，只新增条目）。

---

## 6. 守门用例与变异表

### 6.1 用例

**测试基座**：`setupTestRouter(t)` = 真 `SetupRouter` + 真 service + **sqlite 兼容测试 schema**
（`internal/api/testdata/migrations/`，**不是**生产迁移）。**播种三条硬约束**（审查 §9 F6 实测）：

1. **必须裸 SQL 播种，禁止 `db.Create(&models.Asset{...})`** —— 测试 schema 的 `assets` 表
   （`testdata/migrations/000001_init.up.sql:112-146`）**列集与 `models.Asset` 不一致**（缺 `brand`/`vendor`/
   `warranty_end`/`retired_*`/`business_unit`/`source` 等），GORM 全列 INSERT 会 `no such column: brand`。
   同坑现成证据：`routes_integration_test.go:1301`（`/api/topology` 因缺 `brand` 列 500）。
   可复用既有惯用法：`handlers/diagnostic_handler_test.go:81-84`。
2. **`id` 必须逐行显式给 UUID** —— 表是 `id TEXT PRIMARY KEY` 无默认值；
   `gen_random_uuid()` 只在驱动层注册为函数（`routes_integration_test.go:76-82`），不是列默认。
3. `asset_tag TEXT UNIQUE` → 给 NULL 或互异值。

| # | 用例 | 断言 |
|---|---|---|
| **U1** | 播种 **1001** 条 → `GET /api/assets/export` | `encoding/csv` 解析出的**记录**数 == **1002**（含表头）。**夹具为什么是 1001 而不是 600**：600 只能区分「上限 500」与「上限 ≥600」，一个把硬顶改成 1000 的假修法能全绿通过（变异 M3）。1001 让「任何有限上限 ≤1000」都红 |
| **U1b** | 播种 **501** 条 | 记录数 == **502**。501 是旧代码出错的**最小判别点**（旧行为静默少 1 行） |
| **U1c** | 播种 **500** 条 | 记录数 == **501**。新老行为应当一致的对照点 |
| **U2** | 1001 条 → `?format=json` | `data` 数组长度 == 1001。**必须与 U1 同基座（真 service）** —— 用 `mockAssetService` 断言行数是空转（其 `List` 忽略 `AssetFilter`，回灌 mock 值），且 M4 将不可能变红 |
| **U3** | 同 U1 | `X-Total-Count` == 实际 body 数据行数（两值互校，防头/体脱节）；空表时为 `"0"` |
| **U4** | 空表 | 仅表头 1 行、200；`X-Total-Count: 0`（区分「空」与「错」） |
| **U5** | 回归钉子：`GET /api/assets?page_size=600` | 返回条数仍 ≤ 500（证明 500 硬顶**保留**，未被 R4 误删） |
| **U6** | 取数阶段报错（`mockAssetService` 注入 error —— 这是 mock 的**合法**用途：注入外部依赖故障） | 500 且**响应里没有 CSV 头**（当前实现先取数、后写头，是好性质，钉住它别被提前 `c.Header` 破坏） |
| **U7** | 同 U1 | `Content-Length` 存在且 == body 字节长度 —— 只有「缓冲后一次性写出」才可预知长度，故这条是 D-3 的守门 |

### 6.2 变异表（T-31：必须红在**断言**上，不能红在编译上；编译失败判 INVALID）

| # | 变异 | 期望 | 证明什么 |
|---|---|---|---|
| **M1** | handler 改回 `PageSize: 500` | U1 **FAIL** | 用例真的在测行数 |
| **M2** | 删掉 `X-Total-Count` 头 | U3 **FAIL** | 头被真的断言 |
| **M3** | 把上限改成**有限大值**（如 1000）而非去掉 | U1 **FAIL** | 1001 夹具的价值；600 抓不到这条 |
| **M4** | JSON 分支注入 `items = items[:500]`（人工注入式变异，因两分支共用同一次取数，非自然可改） | U2 **FAIL** | 两个分支都覆盖 |
| **M5** | 删掉 `asset_service.go:87` 的 500 硬顶 | U5 **FAIL** | 反向破坏被钉住 |
| **M6** | 改回 `csv.NewWriter(c.Writer)` 直接写（不缓冲） | U7 **FAIL** | D-3 的原子性被断言 |
| — | `X-Total-Count` 写成独立 `COUNT(*)` 而非常量 `len(items)` | U3 应仍 PASS → **不作为守门变异** | **诚实标注**：这条变异抓不到，所以 D-2 的理由是**消除竞态**而非「测试能抓」 |

---

## 7. 不做的事 / 登记项

| 登记 | 内容 | 为什么不在本轮 |
|---|---|---|
| **G-50** | 导出**字段**不保真：CSV 仅 4 列，缺 `asset_tag` / `serial_number` / 位置 / IP，不足以真正对账。**新增字符串列必须经 `safeCSV`**（否则 Excel 公式注入 DDE 回归） | 需要产品判断（对账必需字段集）+ IP 需 join `AssetNetwork`（一资产多网卡语义未定） |
| **G-51** | `/alerts/false-positives/export` **无界**导出（F10）+ **导出资源策略**（无并发闸门 / 无内存上限，触发条件见 D-8） | 失败模式相反，修法不同（流式/分批下载）；有前端按钮，影响面含 UI |
| **G-52** | 导出端点的**能力门禁** + **审计内容**（审计已有，但不含导出条数） | 授权粒度是独立缺陷类；加门禁是行为突变，打断现有只读脚本。见 D-6 |
| — | 导出支持 `status` / `type` / `keyword` 过滤 | 未要求；`List` 已支持，未来成本低。**注**：它是 D-8「不设上限」之外，未来若真需要上限时**必须先有**的前置（否则上限是死路）—— 一并记在 G-51 的触发条件里 |
| — | 导出支持 `page/page_size`（候选 B 作为增强） | 与 D-1 冲突（会让「完整」重新变成「需调用方拼」）；登记待议 |

---

## 8. 验收门禁

沿用既定门禁（全绿才 commit + push）。文档步骤（本步）无代码改动，门禁应保持全绿。

```
cd backend && gofmt -l ./internal ./cmd ./tests   # 必须空
go vet ./... && go test ./... -count=1 && go build ./...
cd .. && ./scripts/db_smoke.sh
cd frontend && npx tsc --noEmit && npx eslint src --ext .ts,.tsx && npx vitest run
```

改 spec 的步骤额外要求（**并入上面同一段跑**，M31 §5.1 同款）：

```
cd frontend
npm run validate:api                                       # swagger-cli；T-59：description 不得以反引号开头
npm run gen:api && git diff --exit-code -- src/services/api.types.ts   # 判据是**退出码**，不是空输出（T-58）
```

- 本轮**不新增** `TestDBSmoke_*` 用例 → `scripts/db_smoke.sh` 的 `-run` 白名单（T-42）**不需要改**。
  新用例都在 `internal/api/`，由 `go test ./...` 覆盖。

---

## 9. 审查记录

**rev1 → rev2 三路对抗审查**（正确性+边界 / 安全 / 一致性），所有发现均已并入上文。逐条留痕：

### 已被采纳并改写正文的发现

| # | 发现 | 严重度 | 并入处 |
|---|---|---|---|
| A1 | **「导出无审计」是事实错误**（两个审查者独立复现）：`AuditLog` 是 `protected` 组级中间件（`routes.go:243`），export 自动被覆盖 | 重要 | F12 / D-6 / §3.4 |
| A2 | **D-6 是 false dichotomy**：存在既不改变诚实调用方行为、也不是能力门禁的低成本动作 | 重要 | **D-5**（限流）/ D-6 改写 |
| A3 | **去上限引入新的 DoS 放大面**（单请求 O(500)→O(N)、100 突发、`MaxOpenConns=100`） | 重要 | **D-5** / R6 / G-51 |
| A4 | **A2 的「原子写出」在现实现下不成立**：`csv.NewWriter(c.Writer)` 边写边发且丢弃错误 | 重要 | **D-3** / U7 / M6 |
| A5 | **截断点表述不准**：`pageSize > 500` 在 500 时**不触发**，线上截断点唯一是 handler 字面量；硬顶是「调大入参」修法的第二道闸 | 重要 | **§2.1**（新增）/ R1 |
| A6 | §3.2「`List` 本身支持取全量」**与代码相反**（被硬顶截住） | 重要 | §3.2 C 段重写 / D-1 |
| A7 | **U1 用 600 行证明不了「没有上限」**（改成 1000 可全绿） | 重要 | **U1 夹具改 1001** / 新增 **M3** |
| A8 | **U2 放在 mock 基座 = 空转**，且 M4 不可能红（与本文档 R1 自相矛盾） | 重要 | U2 移基座 / R7 |
| A9 | **新增 service 方法会打断 `mockAssetService` 编译**（T-31 判 INVALID） | 重要 | §5 必改项 / **R7** |
| A10 | **D-1 与 §5 自相矛盾**（「List 取全量」vs「保留硬顶」） | 重要 | D-1 改写 |
| A11 | **测试基座播种坑**：schema 列集 ≠ `models.Asset`（`no such column: brand`）、`id` 无默认值、`asset_tag` UNIQUE | 重要 | §6.1 三条硬约束 |
| A12 | **R3 量级估错了对象**（算 CSV 字节，实际驻留 `[]models.Asset`，差约一个数量级） | 重要 | R3 重写 |
| A13 | `X-Total-Count` 浏览器读不到（CORS 未 expose） | 次要 | §5 加 `cors.go:36` |
| A14 | §5/§8 过期声明清单漏 `routes_integration_test.go:772` 的理由串 | 次要 | §5 表格 |
| A15 | `openapi.yaml` **零 `headers:` 先例** | 次要 | §5 / §8 |
| A16 | 去掉警告后 description 该写什么，rev1 没说 | 次要 | §5 |
| A17 | **§3.2 对 M27-B 的类比不准确**（Zabbix 上限也是自设常量） | 次要 | §3.2 C 段重写 |
| A18 | **TODO 编号跳号**（G-50 未被占用） | 次要 | §5/§7 改为 G-50/51/52 |
| A19 | F10 行号指错（397 → 403） | 吹毛求疵 | F10 |
| A20 | U1 的「行数」应用 `csv.Reader` 解析记录数，非裸 `\n` 计数（字段含换行时 `csv.Writer` 会输出带引号的多行记录） | 吹毛求疵 | U1 断言措辞 |
| A21 | §8 门禁代码块漏 `validate:api` / `gen:api` 漂移检查 | 次要 | §8 |
| A22 | §6.1「真 migrations」措辞不准（是 testdata 的 sqlite 兼容版） | 次要 | §6.1 |
| A23 | `db_smoke` 白名单不适用，应明写以免实现者惯性空改 | 次要 | §8 |
| A24 | AUTHZ:155 的 **`sanitizeFilename` 也是错的**（asset export 用静态字面量） | 次要 | §3.4 第 1 点 |
| A25 | G-50 扩列须写明「新列必须过 `safeCSV`」 | 次要 | G-50 |
| A26 | 补充边界：正好 500 / 501 / DB 错误路径 / 1 行 | 次要 | U1b / U1c / U6 |
| A27 | U5 第二条断言（`X-Total-Count` 不出现在列表端点）恒真、价值近零 | 吹毛求疵 | U5 收窄为单条 |
| A28 | M4 是人工注入式变异，非自然可改 | 吹毛求疵 | M4 标注 |
| A29 | §9「三路」vs 四标签的措辞 | 吹毛求疵 | 本节标题 |

### 审查者提出但**未采纳**的发现（附理由）

| # | 提出 | 未采纳理由 |
|---|---|---|
| N1 | 安全审查建议「csv 用 `bytes.Buffer` 一次写」 | **已采纳**为 D-3（列此仅为说明它不是被忽略） |
| N2 | 安全审查建议「加并发闸门（semaphore/singleflight，容量 1–2）」 | 改用**既有模式**的限流（D-5，一行）达到同等目的；真正的闸门登记 G-51 + 触发条件。理由：CLAUDE.md「找最近的既有模式去匹配，别发明新的」 |
| N3 | 安全审查建议「可配置行数上限，超过则拒绝」 | 见 D-8：导出无过滤参数，上限会变成**死路**（不可缩小的请求被永久拒绝）。改为「显式不设 + 登记触发条件」 |
| N4 | 安全审查建议「审计 row 补导出**行数**」 | 属**审计内容**变更，与能力门禁同属「授权/审计粒度」缺陷类，一并登记 G-52。理由：本轮主题是行数保真，把审计 schema 变更拉进来会扩大爆炸半径（审计行被多处消费） |

### 审查者声明「无法证实或证伪」的断言（**继承到下一轮，不得当作已证**）

1. **生产 assets 真实行数** —— 决定 R3/R6 的实际严重度。仓库内无数据。§0 的「上千 VM + 物理机 + 网络设备」来自用户环境描述，非本仓库取证。
2. **§0 的「对账工作流」是否存在** —— 仓库内无 `/assets/export` 调用方（F9 已核），外部脚本**不可见**。
3. **R5「无脚本依赖截断」** —— 只能证明**仓库内**无调用方；「无证据」≠「不存在」。
4. **R3/R6 的量级估算** —— 未实测，文档已自标为量级假设。
5. **`X-Total-Count` 的生成漂移门禁能否拦住头/描述漂移** —— 未实跑。
6. **`go test` 实跑 1001 行夹具** —— 未实跑（仓内不存在该用例）；本步只核验了**可行路径**（裸 SQL + 显式 id + 无阻断约束），§6.1 的「可执行」据此成立，但**未产出实跑证据**。

### 安全审查对 D-6 核心主张的独立核验结论

> **未能证伪，主张成立。** 逐项：只读用户可翻页拿全量（成立）；两端点鉴权链完全相同、无 capability 差异（成立）；
> export 不返回 List 拿不到的行/字段（成立：同一 `List`、无软删除、无 tenant、无权限 scope、JSON 分支同结构）；
> `Asset` 无凭据类字段（成立）；CSV 公式注入已被 `safeCSV` 覆盖（成立）；`Content-Disposition` 无注入面（成立）。
> **但**结论方向正确 ≠ 可以什么都不补 —— 故新增 D-5（限流）与 R6/G-51。

**审查副产品（已核验为真，供后续复用）**：`apierr` 的 5xx 只把内部错误写日志、**响应体不含内部 err**
（`apierr/apierr.go:34-56`，含控制字符剥离）→ 导出失败**无 SQL/DSN 泄露面**。

---

## 10. 实现记录

待填（含逐项 before/after 实测、变异实跑输出、门禁输出）
