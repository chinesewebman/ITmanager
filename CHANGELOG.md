# Changelog

ITmanager 项目所有重要变更记录。版本遵循 [SemVer](https://semver.org/)。

## [未发布] — v2.1.2 之后

> 基线：`git describe` = **v2.1.2-12-gdad2b6f**（最新 tag v2.1.2，最后提交 2026-07-01）。
> 本节按 `git log v2.1.2..HEAD` 的 12 个提交归纳，**只记录 commit 可证实的内容**；尚未打 tag。

### 功能 (feat)

- **Zabbix runtime config UI** (`2dc3367`) — 运行时配置界面 + save/test/sync，release 模式占位符检查
- **NetBox + GLPI runtime config UI** (`8676297`) — v2.2 follow-up，三家集成统一走运行时配置
- **Zabbix metric fallback worker** (`03560e8`) — `Zabbix item.get → metric_snapshots` 兜底落库
- **B4 资产软退役 + IP 释放** (`223c11e` 后端 + `20723a3` 前端退役/恢复 UI)
- **C7 首次登录强改密** (`c848ff5` 首次登录强改密 + 非首次可跳，`dad2b6f` 改密页 + 首次跳引导)

### 修复 (fix)

- **B1-1 / B1-2 / B1-3 settings 死表单** (`3725f40`) — 修复 + 补 CI 前端测试

- **M26 同步导入保真 — 外部时间戳与词表落库** (`1d51f9f`) — GLPI 的挂钟时间按 `Asia/Shanghai`
  解析成 UTC 再落 `TIMESTAMP`（此前全库时间偏 8 小时）；GLPI 状态词表补全，越界档位改为**计数透出**而非静默丢票；
  新增迁移 `000026_tickets_glpi_external_id_unique`（部分唯一索引 + `ON CONFLICT`，重复导入幂等）。
- **M27 Zabbix 同步导入保真 — 去重键与截断可见** (`fd080c2` → `2bd5e7d` → `d38e581` → `f6d31c5` → `b029469`)
  — **A（去重键）**：`SyncFromZabbix` 原按 `status='problem'` 判「已存在」，运维点一次「确认」后
  本地行变 `acknowledged` → 下一轮同步**再插一行**，同一 trigger 出现两行、ack 状态丢失（TODO G-27）。
  判据改为「**同一 trigger 的同一次故障发生**」：`lastchange` 可用时按 `(trigger_id, problem_start.Unix())`
  精确去重，缺失时退回「同 trigger 且未解决」（Go 形态 `status != "resolved"`）。
  新增迁移 `000027_alerts_zabbix_identity_unique`（部分唯一索引 `WHERE source='zabbix' AND trigger_id IS NOT NULL
  AND trigger_id <> ''` + 同事务 `ON CONFLICT ... DO NOTHING`；预过滤是 TOCTOU，无仲裁者时并发撞车会整批回滚）。
  **B（截断可见）**：`trigger.get` 上限 100 → 5000，且请求 `limit+1` 让「正好这么多」与「被截断」可区分；
  `SyncFromZabbix` 返回值 +1（`truncated` 0/1 标志），经 `zabbix_truncated` 透出到设置页提示；
  顺带去掉无人读取的 `selectItems`（每次同步白拉一份 items）。
  验证：sqlite 六行行为表 + 真 PG 冒烟 4 条 + 变异反证 M1–M9/S-1…S-4 全红；见 `docs/IMPL-ZABBIX-SYNC.md` §10。
  新 trap：T-50（部分索引 + `ON CONFLICT` 谓词必须**蕴含**索引谓词，写宽即 `42P10`）、T-51（`migrate.Down`
  只滚最新一层，新增迁移会让既有回滚用例静默错位）。

- **M28 安全与运维正确性收口 — 5 项台账结案 + 3 条新 trap**（`5cec594` 需求文档 → `d7eb44e` A →
  `e149b11` B → `0050c85` C → `0b1d7cc` D/E → 本次提交台账；方案 `docs/FIX-PLAN-HARDENING.md`）
  — **A（G-11 API Key 白名单 CIDR 永不命中）**：写入侧一直接受 CIDR（`validateIPWhitelist`），
  鉴权侧却做 `entry == clientIP` **字符串精确比较** → 填了 CIDR 的 Key 永远 403，且**两侧都不报错**。
  鉴权侧改用 `ipAllowedByWhitelist()` 复用 `parseTrustedNets` + `isTrustedPeer`（裸 IP 补 `/32`/`/128`，
  CIDR 走 `Contains`，IPv6 文本形式差异一并解决）。**B（G-8 `RejectAPIKeyAuth` fail-open）**：守卫靠
  `api_key_id` 非空判「是 API Key 身份」，而该键**只由前置 `AuthMiddleware` 设置** → 任何漏挂它的路由上
  守卫静默放行；判据改为「身份是否已建立」（`user_id` 为空 ⇒ 上游没跑）→ 500 + `Abort`（选 500 不选 403：
  服务器配置错误要进错误率告警）。**C（G-29 容器日志无上限）**：compose 加 `x-logging` 锚点
  （`json-file` + `max-size: 10m` / `max-file: 5`），10 个服务各挂一行。**D（G-12② 死函数）**：删除
  `healthCheck`——硬编码返回 `"database": "connected"` 却**不做任何 ping**，真探针是
  `livenessHandler`/`readinessHandler`。**E（G-38 包级 map 竞态）**：`notification.customSenders`
  加 `sync.RWMutex`（生产当前无写入者，属消除未来竞态面）。
  验证：27 包全绿 + `vet`/`gofmt` 干净 + `-race` 干净；变异 M28-M1…M28-M7 全红在**断言**上
  （首版两条变异曾因删块后 `fmt` 未使用而红在编译上，见 T-31）；新增测试含 19 组表驱动的
  `TestIPAllowedByWhitelist` 与 4 个真实请求穿过的中间件/路由级用例。
  新 trap：T-52（安全控制两侧必须共享语义）、T-53（fail-open 守卫判据不能是「只有前置才会设的键」）、
  T-54（同一个 `URL.Path`，`%#v` 安全、裸拼接可被 `%0d%0a` 伪造日志行）。
- ⚠️ **行为突变告知（M28/A 的副作用，运维需复核）**：A 修完后，**存量**库里此前因字符串精确比较而
  **永不命中**的 CIDR 白名单条目**将开始生效**。若某把 Key 的 `ip_whitelist` 里留着 `0.0.0.0/0`
  或 `::/0`（在旧行为下只是"死条目"、不产生任何限制），修复后它会**真的放行任意来源**。
  请复核现有 API Key 的 `ip_whitelist` 是否仍是期望值（G-42 登记了「写入侧无过宽 CIDR 守卫」这件事，
  本轮刻意不做——拒绝 `/0` 会把运维锁死在"不能编辑既有条目"上，正确做法是保存时告警，需独立一轮）。
- **M28/F 新登记（未修，非本轮引入）**：**G-43** —— `apierr.Respond` 的 5xx 日志行裸拼 `URL.Path`，
  可用 `%0d%0a` 伪造日志行（CWE-117，实测复现；`gin.Logger()` 用 `%#v` 反而安全，`audit.go:97` 落库侧
  同源）。**G-34/G-35**（`redact` 规则 2/3 的值边界漏 + 规则 1 输出被规则 3 二次误伤）由「未修」改为
  **「已登记、待独立模块」**，附实测边界表：`password=&SECRET` / `password=;SECRET` /
  `Authorization: Bearer "SECRET` 三条真漏，而 rev1 曾误判为漏的 `token="S` **实际不漏**。

- **M29 不可信文本的日志 / 落库出口净化 — 一个真泄漏 + 两处静默丢行**（`399a627` 需求文档 rev2 →
  `77ebf3c` A → `f858479` B → `0ab13b2` C → `fd7ff79` D/E → `bc6930a` F → `ef94e2a` G →
  `2daae5a` 真 PG → `613e407` 补测试；方案 `docs/FIX-PLAN-LOG-INJECTION.md`）
  — **A（`redact.StripControl` 收成唯一实现）**：控制字符净化此前在 `redact` / `notification` /
  `handlers` 三处各写一遍（同一份判据三种写法），收成一个导出函数，另两处改为委托；`strings.Map`
  按 rune 迭代顺带把非法字节换成 U+FFFD，PostgreSQL 22021 那一类一并关掉。
  **B（G-43 `apierr` 5xx 日志行）**：`URL.Path` 是**解码后**的请求路径，`%0d%0a` 会变成真 CR/LF 落进来，
  裸拼能向 `gin.DefaultErrorWriter` 伪造出与真实错误行**无从分辨**的第二行（CWE-117，真 socket 打 raw
  请求行实测）→ 整行过 `StripControl`（method/path/code 全不可信），内层先 Strip 再 Text。
  **C（G-44① 审计字段）**：`Path`/`RequestID` **完全不截断**（列宽 `varchar(500)`/`varchar(50)`），
  `UserAgent`/`Username`/`Resource` 用按**字节**的 `truncate`；而 `RequestID` 列最窄、`AuditLog` 又挂在
  **未认证**的 `POST /api/auth/login`（`routes.go:227`）→ 任意人发个超长 `X-Request-ID` 就让登录审计行
  **静默消失**（22001 整行 INSERT 被拒），byte 截断把 `"中"*200` 切成半个汉字则撞 22021（同样丢行）。
  改 `sanitizeField`（`StripControl` + 按 **rune** 截断到列宽 —— `varchar(n)` 数的是字符）五字段统一套用；
  **不做 `redact.Text`**：审计字段是取证材料，脱敏会破坏证据价值。
  **D/E（httpx / Zabbix 错误文本）**：`redactedErr.Error()` 与 Zabbix JSON-RPC 错误（HTTP 200，
  不经 httpx）两个出口补 StripControl；`httpx` 注释写明覆盖面**只含 httpx 起源的错误**。
  **F（G-44② `markFailed` 脱敏顺序 —— 真泄漏）**：原顺序 `redact.Text → stripControlChars` 是**反的**，
  `password=abc\nDEF` 被遮成 `password=***\nDEF` 后删掉 `\n`，等于把**未遮盖的尾部接回**凭据串
  （`pass\nword=SECRET` 更是整条泄漏）；同包 `sanitizeSnippet` 一直是对的 —— 同一判据两种写法。
  改 Strip → Text → ToValidUTF8 → 按 rune 截断，并把"顺序不是安全边界"那句错误注释换成实测反例；
  `worker.go` 三处日志出口的 `ch.Name` 一并净化（G-31 残余①，旧行号 144/150/157）。
  **G（`timeparse` 日志 id）**：`logTimeUnusable` 的 `id` 是第三方可控字符串却用 `%s`（`log.Printf`
  不转义）→ 改 `%q`。
  验证：27 包全绿 + `vet`/`gofmt` 干净；真 PG 冒烟新增 `TestDBSmoke_AuditFieldTruncation`
  （还原 `sanitizeField` 即复现修前的 `ERROR: value too long for type character varying(500)
  (SQLSTATE 22001)` → 丢行；修后 `path=500 runes / request_id=50 runes` 且均为合法 UTF-8）；
  变异 M29-MA/MB/MC1/MC2/MD/ME/MF/MF2/MG 全红在**断言**上（首版 B/C/D/E 四条因删掉调用后
  `redact` 变成未使用 import 而**红在编译上**，按 T-31 改成可编译变异后重跑；MF2 首轮**存活** →
  补 `TestHandleAlertEvent_无收件人日志不伪造行`，见 `613e407`）。
  新 trap：T-55（安全控制的两半，**顺序**也是语义的一部分）。
- ⚠️ **行为突变告知（M29，运维需知）**：① **超长路径的审计行从「丢失」变为「截断后入库」** ——
  `path` 上限 **500 字符**（超出部分丢弃）、`request_id` 50、`user_agent` 500、`username` 100，
  此前这些请求在审计链上是**空洞**，现在会留下截断记录（多字节字符按整字符截，不会再切出非法 UTF-8）。
  ② `error_msg` 里被控制字符切开的凭据**从「尾部泄漏」变为「完整遮盖」** —— 若有用例/脚本匹配历史
  `error_msg` 文本需复核。③ 登录路由带超长 `X-Request-ID` 的请求**开始正常留审计行**（此前静默不落库，
  审计量的基线会上升）。
- **M29 新登记（未修，非本轮引入）**：**G-45** —— `internal/integration/` 对第三方字段**零截断**，
  超长即 22001，而 GORM 批插在同一事务里 → **整批回滚**（外在表现是「同步报成功但一条都没进来」），
  与 G-44 同族但修法不同（要逐字段定义按字符截断 + 截断计数透出），独立一轮。**G-34/G-35**
  仍为「已登记、待独立模块」（`redact` 规则 2/3 的值边界漏 + 规则 1 输出被规则 3 二次误伤）。

- **M30 脱敏值边界与 URL 分段 — G-34/G-35 两项结案 + 修掉两族净回归**（`f62b8fd` 需求文档 rev2 →
  `b05df00` 先固化不变量 → `8b03232` F1/F2/F3+F3b/F4 → `7253331` 审计迭代；
  方案 `docs/FIX-PLAN-REDACT-BOUNDARY.md`）
  — **G-34（值边界漏）**：`password=&SECRET`、`password=;SECRET`、`,SECRET`、`Authorization: Bearer "SECRET`
  （从配置文件复制粘贴出来的常态）此前**整条明文**。修法：规则 2 值类收任意个起始引号 `["']*`、
  规则 3 值类收捕获回写的引号组 `(["']*)`（写成非捕获会破坏「引号形态保留」契约）+ 起分隔符
  `[&,;][^\s]*`、分隔符补全角 `：`/`＝`。**G-35（过度脱敏）**：`Text("http://token:8080/x")` →
  `http://token:***`，主机名命中敏感词时端口被规则 3 二次误伤。修法：`Text` 按 URL 匹配**分段**，
  URL 段只过 `URL()`；并补 **F3b** —— `URL()` 的 host 里出现键值分隔符（`=`/`＝`/`：`）时塌缩为
  `<invalid-url>`，这是分段的**必要条件**（否则 `http://access_token=SECRET` 从已遮变明文）。
  **两族净回归（审计两路独立报出 + 11520 组新旧差分复核）**：分段拆掉了「规则 3 看得见规则 1 输出」
  这个偶然兜底，`<敏感键>=<URL>` 的值退回明文（`password=https://example.com` 旧版 `password=***`；
  差分又补出重复分隔符一族 `password==http://SECRET/x`）。修法不再另写「凭据前缀模式」（它与规则
  2/3 必须逐字同集，实测漏过两族），改为 `eatsFollowing()` **问规则本身**：这种形态**不切分**，
  让规则 2/3 连值一起吞。顺带修掉一族既有泄漏（非本轮引入）：分隔符量词 `+`，`password=="SECRET"`
  此前 → `password=***"SECRET"` 明文，与 F2 已修的 `password=""abc` 同族。
  验证：先固化 §1.4 的 8 组边界锁定 + 回归面再改代码（修复项 34 行先跑红作证）；
  变异 M30-M1…M10 全红在**断言**上（无存活、无编译失败）；差分 11520 组净回归 0、真泄漏 0；
  27 包 `go test` 全绿、`db_smoke.sh` ✅、`tsc` 0。新 trap：**T-56**（一条规则的输出是另一条规则的
  输入时，两条边界互相咬合 —— 拆开串联会静默放出明文）。
  新登记：**G-46**（结构化值内元素明文，需嵌套匹配，低危）。
- ⚠️ **行为突变告知（M30，运维需知）**：① **值首字符是 `&`/`,`/`;` 时遮盖范围到下一个空白** ——
  `?a=1&password=&b=2` → `?a=1&password=***`（`b=2` 被吃掉）；这是刻意的价值排序（宁可丢信息，
  不回显原串），不是 bug。② **全角 `：`/`＝` 也成为键值分隔符**，中文没有词间空格 →
  `重置 password：请联系管理员` 会整句被吞成 `重置 password：***`。③ **连续分隔符整体保留** ——
  `password==SECRET` → `password==***`（此前 `password=***`），遮盖性不变，只是键名后多留了分隔符。
  ④ **URL 的端口开始保留、host 含 `=` 的 URL 变为 `<invalid-url>`** —— 排障时看到
  `http://token:8080` 是正常的（此前是 `http://token:***`）；看到 `<invalid-url>` 说明该 URL 的
  host 形如 `key=value`（不是真实主机），这是**有意丢弃**，不要为了看原串把脱敏关掉。

- **M31 OpenAPI 契约完整化 — 未文档化路由 22 → 0，断言升级为双向集合相等**（`22689ef` 需求文档 rev2 →
  `be6e55c` 步骤 2 → `95f3f98` 步骤 3 → `b3a26e3` 步骤 4 → `90195af` 步骤 5 + `e73710c` 补漏 →
  `dee8470` 步骤 6；方案 `docs/FIX-PLAN-OPENAPI-CONTRACT.md`）
  — **G-37 残余结案**：此前 spec 只声明 71 条、真实路由 93 条，缺口 22 条（含 `/auth/me`、整组
  `/auth/api-keys`、`/audit-logs`、告警批量、资产退役/恢复/导出、`/health`、`/integrations/*`）。
  按组分两步补完，schema **全部来自 handler 实读**（`c.JSON(...)` 逐个字段抄，拿不准的写
  `additionalProperties` 而不是编造字段）。**实测：93/93 全文档化、spec 零幻影**。
  **门禁思路换向（D-1）**：从「spec ⊆ 路由」（只挡幻影）升级为**集合相等**（两个方向都断言），
  且**明确不设 allowlist** —— allowlist 会把「未文档化」洗成合法态，理由字符串是软控制、
  交付态恒绿 vacuous。另补一条用例钉住**鉴权语义**：顶层 `security: [{BearerAuth: []}]` 生效后，
  spec 标 `security: []` 的端点必须**恰好等于**运行时无凭据可达的端点（实测两侧各 3 条：
  `GET /health`、`POST /auth/login`、`POST /auth/logout`）。顺带修正 `/auth/logout` 原先
  **反着错**的标注（它没挂 `AuthMiddleware`，spec 却说它要 Bearer）。
  **变异 M31-M1..M7**：M1（删 spec path）、M4（spec 方法名改大写）、M5（删公开端点的
  `security: []`）、M6（给受保护端点加 `security: []`）、M7（删顶层 `security`）**只有新断言能抓**，
  旧断言全绿；M2（改动词）、M3（删路由注册）旧断言也红，只作交叉验证 —— **计划态把 M3 说成
  「旧断言覆盖不到的方向」是错的，已实测更正**（旧断言遍历 spec 逐条核真实路由，删注册正好命中）。
  **工具链接线**：`npm run validate:api`（swagger-cli，依赖早已在 `package.json` 却从未接线 ——
  README 声称的「validate 通过」此前是**陈旧声明**）；`gen:api` 漂移检查纳入每步验收
  （CI 有硬门禁）。
  验证：`gofmt`/`go vet`/`go build`/`go test ./...` 全绿 + `db_smoke.sh` ✅ + `validate:api` valid +
  `tsc`/`eslint` 0 + `vitest` 18 passed（仅受影响文件）。新 trap：**T-57**（`map[K]bool` 当集合用时
  「键存在」≠「值为真」）、**T-58**（空输出 ≠ 无漂移，判据要看退出码）、**T-59**（YAML
  `description` 不能以反引号开头，报错却指向缩进）。
  新登记：**G-47**（Swagger/`openapi.yaml` 匿名可达且无开关，改由部署文档的网络层约束兜底）、
  **G-48**（`Asset.status` 两套词表，退役写的 `retired` 不在契约 enum 内）、
  **G-49**（`/assets/export` 固定前 500 条且丢弃 total → 静默截断）。
- **M32 导出保真 — G-49 结案：`/assets/export` 从「静默前 500 条」改为全量 + 完整性声明**（`0863584` 需求文档 rev2 →
  `41c010f` 步骤 2 细节文档 → `ff46735` 步骤 3 实现 → `89ddc5a` 步骤 4 守门用例 + 变异；
  方案 `docs/FIX-PLAN-EXPORT-FIDELITY.md` / `docs/IMPL-EXPORT-FIDELITY.md`）
  — **缺陷**：`ExportAssets` 固定 `AssetFilter{Page:1, PageSize:500}` 且丢弃返回的 `total`。资产超过
  500 台时导出的 CSV **不完整且没有任何提示** —— 运维拿它做盘点/对账会得出**错误结论**（不是「少了点
  数据」，是结论反了）。**修法**：导出改走 service 新增的专用全量方法 `ListAll`（**`List` 与其
  `pageSize > 500 → 500` 硬顶一字未动** —— 那是交互式分页的防拉爆闸，语义与导出不同）；新增响应头
  `X-Total-Count = len(items)`（**不另发 `COUNT(*)`**：头值恒等于 body 行数，不存在 COUNT 与 SELECT
  之间的竞态窗口）+ 显式 `Content-Length`，两者供调用方自校验「收全了没有」；CSV 改为**先写满
  `bytes.Buffer` → `Flush` → 查 `w.Error()` → `c.Data` 一次写出**（原实现边写边发且 `defer w.Flush()`
  把错误全丢 —— 取数/序列化中途失败会产出「半截 CSV + 已 200」，本身就是静默不完整）；排序
  `created_at DESC, id DESC`（`id` 兜底让同刻批量导入的产物可 diff）；CORS `Expose-Headers` 补
  `X-Total-Count`。**显式决策**：不设行数上限（无过滤参数 → 任何上限只是把 500 换成另一个静默截断
  点，D-8）、不加 capability 门禁（导出不返回列表页拿不到的行，不构成提权，D-6）、不扩 CSV 列（D-9）。
  **回归网**：6 条集成用例（1001 行全量 / 501 与 500 边界对照 / JSON 分支同全量 / 空表 / `List` 硬顶
  保留的反向钉子）+ 1 条 handler 错误路径用例（500 不得带 CSV 头），夹具前三条 name 含逗号/换行/公式
  前缀，让 `csv.Writer` 转义与 `safeCSV` 两个分支真正被覆盖。**变异 M32-M1..M8 全红**（含 M3「`ListAll`
  加 `.Limit(1000)`」—— 有限上限而非去掉，夹具取 1001 正是为抓它）。新 trap：**T-60**（限流桶是包级
  共享的 → 集成测试同端点调用会累加，`-count=3` 全线红且报的是业务断言）。
  新登记：**G-50**（CSV 列不保真）、**G-51**（两个导出端点的资源姿态相反、缺统一策略）、
  **G-52**（导出的能力门禁与审计内容）。
- ⚠️ **行为突变告知（M32，运维需知）**：**`GET /api/assets/export` 的产物会变大** —— 此前最多 500 行，
  现在**全量**。依赖「导出只有 500 行」的下游脚本（对账/盘点 diff、按行数做的断言）会看到行数变化；
  这是**修复**而非回归：原先的 500 行是静默截断，产物不完整且无提示。同时该端点**新增独立限流
  **10 次/分钟 per IP**（原只有组级 100/min）—— 高频轮询导出的脚本需要降低频率或改为按需触发。
  判定产物是否完整：比对响应头 `X-Total-Count` 与 CSV 数据行数（不含表头），或校验 `Content-Length`。
- **M33 第三方文本截断保真 — G-45 结案：同步路径的第三方字段「剥控制字符 + 按字符截断」并透出计数**（`f20e5f0` 需求文档 rev2 →
  `3a430ea` 步骤 2 细节文档 rev2 → `95e2803` 步骤 3a helper 层 → `e0a324b` 步骤 3b 四条同步路径接入 → `adeab55` 补漏
  `db_smoke_test.go` 的 4 处签名连带；方案 `docs/FIX-PLAN-TRUNCATION.md` / `docs/IMPL-TRUNCATION.md`）
  — **缺陷**：`internal/integration/` 对第三方字段**零截断**，任何一条超 `varchar(n)` 撞 `22001`（含 NUL 撞 `22021`），
  而 GORM `CreateInBatches` 是「一次调用 = 一个事务」→ **整批 0 行**；三条手动路径（NetBox / Zabbix 告警 / GLPI）表现为
  HTTP 500 且响应里不说是哪条哪个字段，指标 worker（`main.go:58`，`Tick: 5*time.Minute`）**无 HTTP 面、无前端入口** →
  坏行从未落库 ⇒ 下一轮再被选中 ⇒ **永久卡死**。TEXT 列不是安全区：NUL 照样拒，而 `alerts.problem` /
  `tickets.description` 与有界列同处一条事务。
  **变更内容**：新增 `redact.TruncateRunes(s, max) (string, bool)`（按 **rune** 截断 —— PG `varchar(n)` 数的是**字符**，
  按 byte 切会切出非法 UTF-8 → `22021`，等于没修）；新增 `internal/integration/truncate.go`：9 个列宽常量 + `sanitizeText`
  （TEXT 列只剥不截，纯函数）+ `fieldCounter`（`truncate`/`text`/`count`/`String`，**截断与剥离分开记账**：截断进 API
  计数、剥离只进日志明细）+ `logFieldSanitization`；四条路径接入 —— `SyncFromNetBox`（`name`/`brand`/`model`/`sn`/
  `site_name`）、`SyncFromZabbix`（`trigger_name`/`host_name` 截断 + `problem` 只剥）、`SyncFromGLPI`（`title` 截断 +
  `description` 只剥）、`SyncMetricsFromZabbix`（`key`）；`SyncFrom*` 返回值各加一个 `fieldsTruncated`，`SyncAll` 与
  handler 按 `type` 透出新键，worker 接住原本被 `_` 丢弃的计数。`ConvertTo*` 签名、schema、`metric_sync` 的分批语义一字未动。
  **受影响下游**：`POST /api/integrations/sync` 的 `data.synced` 新增 `netbox_field_truncations` / `zabbix_field_truncations` /
  `glpi_field_truncations` / `zabbix_metrics_field_truncations`（**被截断的字段处数**，不是条数）；键**随 `type` 而变**
  （`type=all` 不含指标键 → 四个键永不同时存在），**失败分支不写键** → 调用方一律 `?? 0` 兜底；既有 `zabbix_truncated`
  （0/1 标志）语义与键名不变。**前端 `Settings.tsx` 本轮未补文案** → 新计数目前只在响应体与后端日志可见。
  **自校验方法**：`cd backend && go test ./internal/redact/... ./internal/integration/...`（U1/U2/U5/U7a + 日志守卫四态；
  实测 `ok`）+ `go test -race ./internal/integration/...`（D-10 的并发面）+ 真 PG `./scripts/db_smoke.sh`（白名单 **T-42**；
  本轮**未加**新用例）。
  **附带副作用**：`fieldCounter` 是本包**新引入**的聚合器（既有先例全是裸计数器），日志里出现
  `[netbox] 字段截断 N 处，明细：name×3,sn×1` 与 `problem(stripped)×1` 句式（**只记字段名与次数，不记原值**）；
  指标 worker 在 `ft>0` 时多一行 `tick ok: written=N, field_truncations=M`。
  **未随本轮落地**（逐项有登记）：前端文案（D6）、`audit_logs.resource` 的常量/模型对齐（D7 → **G-55**）、
  `openapi.yaml` 键清单 + `gen:api`（D8 → **G-57**）、真 PG 的 U3/U4/U6/U7b/U8 与冒烟白名单（→ **G-53** / **T-61**）。
- ⚠️ **行为突变告知（M33，运维需知）**：① **同步导入的第三方字段开始被改写** —— 超列宽的字符串**截断尾部**（口径是
  **字符**：多字节值整字符截，不会切出非法 UTF-8），含 NUL/控制字符的字符串**剥离后**才落库；在此之前这类行会让**整批**
  同步失败（手动路径 500 / 指标路径每 5 分钟静默失败），所以「以前同步报错、现在成功但值短了」是**修复后的正常形态**。
  ② **同一源字段可能被两处改写且口径不同**：Zabbix `trigger.description` → `trigger_name`（截 500）与 `problem`
  （只剥不截）；与源端逐字比对会不同（`\t\n\r` 的剥离是与审计口径一致的有意取舍，见 `TODO.md` G-45 的「已知残留」段 —— 该项本轮未单独登记）。
  ③ **响应体新增键**：按 key 取值的下游脚本加 `?? 0`；**枚举** `synced` 键的脚本会看到新键（键随 `type` 而变，见 G-56）。
  ④ **日志**：真有截断/剥离时才多打一行（无事不刷）；指标 worker 多一行 tick 汇总。
  判定「有没有被改短」：先看 `<path>_field_truncations` 是否 > 0，再看日志明细里的字段名。
- ⚠️ **行为突变告知（M31，运维需知）**：**无运行时行为变更** —— 本轮零 Go 业务代码改动
  （只改 spec、生成物、测试与文档）。但**有一条部署侧的新要求**：见下条。
- ⚠️ **部署侧新增要求（M31/§8.4.5）**：**不得把后端 8080 直接映射到 `0.0.0.0`**。
  `/openapi.yaml` 与 `/swagger/*any` 是**刻意公开**的（无鉴权、无开关，release 下同样可达），
  直连等于把 API 的完整形状（路径/参数/schema/枚举）对全网公开，并绕过 nginx 的 TLS/安全头/限流。
  compose 默认已绑 `127.0.0.1:8080`，**改 compose 或加端口映射时不要动它**；
  排查见 `08-部署运维.md` §8.4.5（`ss -tlnp | grep 8080`）。

### 文档 (docs)

- **TRAPS.md** (`e7c1a0e`) — 集中 27 个项目 trap（B1-4）

- **M34 — D-1/D-2 tickets 收尾 + G-25 残余闭环（2026-09-12）**

- **D-1 tickets 表 schema 收尾** (`4496087`) — `migrations/000028_tickets_schema_align.up.sql`：`DROP NOT NULL ticket_type`（让模型 `gorm:\"size:20\"` 可落库空串）；12 条 `ADD COLUMN IF NOT EXISTS` 显式零增量（与 `000013` 重复声明是为幂等保留、非新缺陷）。
- **D-2 工单号 used-set+max+1** (`7fb2ca9`) — `models/ticket.go` 把 `generateTicketNumber` 与 `AssignTicketNumbers` 统一改「当日 used-set 求 max + 1」算法，**空洞免疫**（M26 的 G-25 残余边界「删过工单后 count 回退撞号」由此闭环）；新增 `usedTicketLabels` + `parseSeqSuffix` 工具函数。
- **真 PG 守门 3 条** (`80be839`) — `tests/db_smoke_test.go` 新增 `TestDBSmoke_TicketsSchemaRoundTrip`（24 字段全开 insert）+ `TestDBSmoke_GenerateTicketNumberDayScoped`（25 张同号全 distinct：A..Z + AA）+ `TestDBSmoke_TicketNumberRetry`（23505 唯一索引兜底）。`scripts/db_smoke.sh` 白名单 + 1。
- **FIX-PLAN + IMPL** (`bc63311` / `135e6ac`) — `docs/FIX-PLAN-D1-D2-TICKETS.md` + `docs/IMPL-D1-D2-TICKETS.md` 单独成文，D-1（tickets-only 子集）+ D-2 算法与测试守门逐条写定。
- **M34-R3 G-58 U7b 常量漂移闭环** (`36cd221` + `8cbcac6` + `3778dd9`) — `truncate.go` 导出 `ColumnWidths() map[string]int`；U7b 改为从该 map 实时读 9 个期望值，**双向漂移守门**：M7b（Go 常量偏小）+ M7a（DDL 偏宽）均能被 U7b 抓到（M33 变异反证已发现该漏洞、登记为 G-58）。`require.Len` + `require.True(ok)` 防新增/缺列时静默通过；新 U7b 日志行 "live ColumnWidths 读取，G-58 闭环"。
- **M34-R4 G-59 worker tick-log 守门** (`8b90b96` + `9ec65a1` + `9cc60aa`) — `metric_sync_test.go` 新增 `TestMetricSyncWorker_TickOkLogWritten` + `TestMetricSyncWorker_TickErrorLogWritten`（+88 lines，复用 `truncate_test.go` 的 `captureLog` 工具）。`log.Printf → no-op` 的 M11 变异现在能被两条新测试的红点抓到（subagent 实测：tick ok 行缺失 / tick error 缓冲空）；`-race` 通过。
- **门禁** — `gofmt -l` 干净、`go vet ./...` 干净（仅 sqlite3 C warning 系既有）、`go test ./internal/{redact,integration,models}/...` 全绿、`scripts/db_smoke.sh` 全新+升级两条路径全绿（37+3 = 40 cases，M34-R3 沿用）、Down 链 13 次改 14 次并逐行核过。

### 工程 (chore)

- **C6 docker-compose healthcheck** (`77bfdd9`) — 7 服务加 healthcheck + `depends_on: service_healthy`
- **Go 1.25 fmt 重排** (`3bae43f` 既有文件 / `96f14d8` metric_sync 文件)
- **compose 运行时收口**（G-9/G-10/G-13 收尾，本次提交；方案 `docs/FIX-PLAN-COMPOSE-RUNTIME.md`）
  — **破坏性变更**：① secret 不再有硬编码默认值，三个必需变量（`NMP_DATABASE_PASSWORD` /
  `NMP_AUTH_JWT_SECRET` / `NMP_AUTH_API_KEY_PEPPER`）走 `${VAR:?}`，缺值 compose 直接报错；
  ② 默认 `docker compose up -d` 只起主链 4 服务（postgres/redis/api/web），
  netbox/zabbix/glpi/graylog/elasticsearch/mongoDB 移入 `profiles: ["aux"]`；
  ③ 端口收敛：postgres 只绑 `127.0.0.1:5432`、redis 不发布、api 只绑 `127.0.0.1:8080`；
  ④ 新增 `backend/Dockerfile`、`frontend/Dockerfile`（此前被 `.gitignore` 的裸规则挡住，从未进仓库）。
  另：迁移改由 api 启动时执行（单副本约束），三个 CLI 注入 `MigrationsFS`。

### 版本号缺口 — 待补

- ⚠️ **v2.1.1、v2.1.2 的 CHANGELOG 条目缺失**（tag 已存在，条目未写）—— **待补**，不臆造发布内容。
- 本文件上一版最新条目为 [v2.1.0]；v2.1.2 之后的提交尚未打 tag，故列于「未发布」。

## [v2.1.0] - 2026-06-27

🔧 **审计 P2 改进** — 13 项跨模块打磨 (性能 / 一致性 / 可观测 / 数据完整性)

### 性能 (3 项)

- **rate_limit 单例化** (`internal/middleware/rate_limit.go`) — `RateLimit(cfg)` 改包级 cache, 同 cfg 复用同一 `rateLimiter` + gc goroutine; 生产 50+ 路由从 50+ goroutine 收敛到 N (N=distinct cfg)
- **eventbus 指数退避** (`internal/eventbus/eventbus.go:299`) — 重试退避改 `base << attempt` (100ms, 200ms, 400ms), 替代线性 `base*(attempt+1)`, 缓解 retry 风暴下后端压力
- **alert_service IncludeStats opt-in** (`internal/service/alert_service.go:30`) — `AlertFilter.IncludeStats` 字段, 默认 false; gRPC 翻页路径省 1 次 stats 全表聚合查询 (HTTP 列表页显式设 true 保持兼容)

### 一致性 (3 项)

- **middleware slog 一致** — `audit.go` / `api_key_tracker.go` 全量 `log.Printf` → `slog.Warn/Info` (async/sync warn + flush/failure + count), 与项目 logger 体系对齐
- **service slog 一致** — `alert_service.go` `writeNotificationTrigger` 2 处 `gin.DefaultErrorWriter` → `slog.Warn`; `publish()` `_ = err` 静默吞 → `slog.Warn` 含 topic + err
- **eventbus stats 命名** — 新增 `HandlerFinalFails` 字段 (retry 耗尽最终失败), 与 `HandlerErrs` (含 retry 中) 区分, 告警阈值更准

### 可观测 / 测试 (3 项)

- **audit UUID warn** (`audit.go:108`) — `user_id` 非合法 uuid 时 `slog.Warn` forensic 价值 (原始字符串仍在 `Username`), 旧版静默丢弃
- **eventbus payload 上限** (`eventbus.go:90`) — `Config.MaxPayloadSize` 默认 64KB, 超限返 `ErrPayloadTooLarge` 不入 chan, 防大 payload 撑爆内存 + DLQ 表
- **cursor benchmark** (`cursor_test.go`) — `BenchmarkEncode` (Encode ~992ns/op) + `BenchmarkDecode` 防 encode/decode 实现退化

### 数据完整性 (2 项 — M4-5 扫描发现)

- **oncall CreatePolicy 事务化** (`oncall_service.go:168`) — `policy + N× levels` 包 `db.Transaction`, 防 level 失败产生半成品 policy
- **oncall DeletePolicy 事务化** (`oncall_service.go:241`) — `levels + policy` 包 `db.Transaction`, 防 policy 删失败导致 levels 残留

### 工具 (1 项)

- **api_key_tracker 可重入** (`api_key_tracker.go:60`) — `sync.Once` 改 `sync.Mutex + lazy init`, 加 `resetAPIKeyTracker()` 测试隔离 helper

### 测试

- backend: 818 → **825 passed** (+7 新 tests: rate_limit singleton / audit UUID warn / eventbus payload+FinalFails×2 / alert IncludeStats opt-out)
- 全量 28 包 0 fail
- 审计来源: `docs/audit-v2.0.1.md`

### 兼容性

- ✅ 0 DB migration (纯代码 + 字段新增 opt-in)
- ✅ HTTP API 行为不变 (IncludeStats 显式 true 保持)
- ✅ gRPC 行为不变 (默认 false 已与原 _ 跳过 stats 一致)

## [v2.0.2] - 2026-06-27

🛠️ **审计 P1 修复** — 4 项跨模块数据完整性 / 稳定性 / 审计语义

### 修复 (审计 v2.0.1 → v2.0.2 patch)

- **fix(audit-P1)**: `resourceFromPath` 跳过动态段找下一个静态段 — 旧逻辑遇到 `:` 开头段直接 return `""`，导致 `/api/:tenant/users` 等路径的审计 resource 字段丢失。改为 continue 跳过动态段继续找第一个静态段。
  - 新增 4 case 测试：tenant 前缀 / id 中缀 / 纯动态 / 首段动态 + api 段
- **fix(eventbus-P1)**: dispatch panic recover — handler panic 之前会让 worker goroutine 死亡，chan 持续入事件无人消费，最终 ErrBufferFull 风暴。`dispatch` 加 defer recover，panic 后日志 + DLQ 落库，worker 继续服务后续事件。
- **fix(eventbus-P1)**: `Subscribe` 跨 topic 聚合 — 旧版只统计最后一个 Subscribe 的 topic 的 handler 数（A=3 + B=5 应=8 报 5），监控失真。改为聚合所有 topic 的 handler 总和。
- **fix(oncall-P1)**: `DeleteSchedule` 两步 DELETE 包事务 — 旧实现先删 schedule 再删 shifts，若第二步失败（FK/DB）则 shifts 永久孤立。改为 `db.Transaction` 包裹两次 DELETE，失败回滚 schedule。

### 风格

- **style(middleware)**: `DefaultSkipPaths` 4 行 whitespace 重对齐

### 测试

- backend: 818 → **827 passed** (+9 tests)
- 全量 28 包 0 fail
- 审计来源：`/Users/apple/study/ITmanager/docs/audit-v2.0.1.md` (待归档)

### 升级指引

- ✅ 0 DB migration (纯代码修复)
- ✅ 0 API 变更 (审计/事件总线字段不变)
- ✅ 0 行为破坏性变更

## [v2.0.1] - 2026-06-17

🔌 **gRPC 内部通信** — AlertService s2s + proto contract

### 新增

- **gRPC server** (`internal/grpcserver/alert_server.go`) — 内部服务间通信
  - 端口 `:50051` (env `GRPC_PORT` 覆盖)
  - 启动: `cmd/server/grpc.go` (独立 `startGRPCServer` 函数)
- **proto 定义** (`api/proto/alert/v1/alert.proto`)
  - `AlertService` 4 RPC: `ListAlerts` / `GetAlert` / `AckAlert` / `ResolveAlert`
  - 支持 v2.0 cursor 分页 + v1.x `page/size` 兼容
  - 完整 Severity / AlertStatus enum (1-4 / pending/acked/resolved)
- **生成代码**: `alert.pb.go` (771 行) + `alert_grpc.pb.go` (237 行)
- **模型适配**: `TriggerName/Problem/HostID/HostIP/TriggerID/ResolveTime/ResolveUser` → proto
- **10 单元测试** (`alert_server_test.go` + `test_helpers_test.go`)
  - cursor 满页/部分页 + 非法 cursor
  - severity/status 转换 (int↔enum, string↔enum)
  - id 必填校验

### 依赖

- `google.golang.org/grpc@v1.81.1` + `google.golang.org/protobuf@v1.36.11`
- `protoc-gen-go` + `protoc-gen-go-grpc` 安装路径: `~/go/bin/`

### 兼容性

- ✅ 不破坏既有 AlertService interface (gRPC 仅消费)
- ✅ 不破坏 REST API (gRPC 是新增, 不是替代)
- ✅ v2.0 cursor / v1.x page 模式同 RPC 内支持

### 客户端使用

```go
conn, _ := grpc.NewClient("localhost:50051", grpc.WithTransportCredentials(insecure.NewCredentials()))
client := alertv1.NewAlertServiceClient(conn)
resp, _ := client.ListAlerts(ctx, &alertv1.ListAlertsRequest{Limit: 50})
```

### 明确不做 (单机 + 内部)

- ❌ gRPC-gateway (REST 已有, 重复)
- ❌ TLS/mTLS (单机内部, 127.0.0.1 only)
- ❌ 反射服务 (生产不暴露)

## [v2.0.0] - 2026-06-17

🚀 **主版本** — 性能与解耦 (cursor 分页 + event bus)

详见 [ADR-0002](docs/adr/0002-v2-scope.md) + [13-实施规划.md §21](13-实施规划.md)。

### 新增

- **cursor 分页** (`internal/cursor/cursor.go`) — 高效翻页 O(log N)
  - base64 + NUL 分隔的紧凑编码, 包含 (timestamp, id) 二元组
  - alert / ticket / audit 服务支持 cursor 入参
  - handler 接受 `?cursor=` + 响应 `next_cursor`
  - schema 加 `(created_at DESC, id DESC)` 联合索引 (alerts / tickets / audit_logs)
  - 老 `?page=N&size=M` 接口完全兼容 (cursor=null 时走 offset 模式)
- **event bus** (`internal/eventbus/bus.go`) — in-process pub/sub
  - 1024 buffer, 4 dispatcher goroutine, 3 retry + 100ms 退避
  - 5 topic: `alert.created` / `alert.resolved` / `ticket.created` / `ticket.resolved` / `user.locked`
  - SQLite DLQ (event_dlq 表), handler 失败 3 次后落死信
  - 优雅关闭 (Close 幂等), Stats 接口 (published / dispatched / dlq / retries / pending)
- **notification worker 接入 bus** (`internal/notification/worker.go`)
  - `SubscribeToBus(bus)` 注册 `alert.created` / `alert.resolved` handler
  - 双轨并行: 5s tick 老 path + event bus 新 path
- **AuditService 新建** (`internal/service/audit_service.go`) — 4 维过滤 + cursor
  - 端点: `GET /api/v1/audit-logs` (admin/debug)
- **AlertService 注入 bus** (`internal/service/alert_service.go`)
  - `WithBus(bus)` 注入
  - `Resolve` 调 `bus.Publish(alert.resolved)` 触发通知

### 数据库迁移

- `000010_v2_eventbus_cursor.up.sql`
  - 新增 `event_dlq` 表 (event_bus_id / topic / payload / last_error / failed_at)
  - `alerts` 加 `(created_at DESC, id DESC)` 联合索引
  - `tickets` 加 `(created_at DESC, id DESC)` 联合索引
  - `audit_logs` 加 `(created_at DESC, id DESC)` 联合索引

### 兼容性

- v1.x 客户端继续用 `?page=N&size=M` (cursor=null 走 offset 兼容) ✓
- DB schema 增量迁移 (新表 + 索引, 不破坏老数据) ✓
- 部署方式不变 (单 binary + sqlite/postgres) ✓

### 推迟到 v2.0.1+ (gRPC 风险评估后)

- ⏳ gRPC 内部通信 (v2.0.1, 估 12-15h, 风险评估中)

### 明确不做 (v2.0 scope 外)

- ❌ Redis 部署 (v1.4 in-process LRU 够, 推迟 v3.0)
- ❌ Kafka/NATS (永久不做, 单机不需要)
- ❌ 多实例部署 (推迟 v3.0)
- ❌ 全文搜索 ES (推迟 v3.0)
- ❌ PWA 移动端 (推迟 v2.1)

## [v1.4.0] - 2026-06-17

🔒 **次版本** — 后端稳定性 + 性能 + 可观测性 (rate limit / 通知 worker / 缓存 / 审计日志)

### 新增 Middleware / Service

- **rate limit middleware** (`internal/middleware/rate_limit.go`) — 滑窗限流
  - per-IP + per-path 维度, 内存 store (留 Redis 后端接口 v2.0)
  - 标准 headers: `X-RateLimit-Limit` / `Remaining` / `Reset` / `Retry-After` (429)
  - 路由级策略: login 5/min, password reset 3/min, protected 100/min
  - 18 routes 在 protected group 接入
- **notification worker** (`internal/notification/sender.go` + `worker.go`) — 异步真发
  - 三种 Sender: DingTalk / Email (SMTP+TLS) / Webhook
  - 5s tick 拉 pending 消息 (IN 查询避免 N+1), 30s per-msg 超时
  - stale-while-error: worker 挂掉不会丢消息, 启动时自动捞 pending
  - `channel_service.Test` 移除 stub, 走 Sender interface 真发
- **cache LRU** (`internal/cache/cache.go`) — 进程内缓存
  - `Cache interface` (Get/Set/Delete/GetOrLoad/Stats/Clear), 30s TTL
  - `GetOrLoad` stale-while-error: load 失败回 fallback, 命中走缓存
  - LRU 淘汰 + 并发安全
  - `NewDashboardServiceWithCache` 注入, dashboard stats 30s TTL
- **audit log middleware** (`internal/middleware/audit_log.go`) — 审计落库
  - 异步落 `models.AuditLog` (user_id / action / resource / status_code / ip / user_agent)
  - `SkipPaths` 跳过探针 (/healthz /metrics), `SkipMethods` 跳过 GET (可配置)
  - 写失败不影响业务响应 (fire-and-forget)
  - gorm `Session{SkipDefaultTransaction: true}` 避免无谓事务

### 测试

- 新增 81 tests:
  - rate limit: 11 (sliding window / headers / KeyFunc / 并发 / GC)
  - notification: 20 (HTTP 集成 / 错误路径 / worker 3 路径)
  - cache: 16 (TTL / LRU 淘汰 / GetOrLoad 5 场景)
  - audit log: 9 (落库 / 跳过 / 失败重试 / 状态码捕获)
  - database: +7 (autoMigrate / Init / 错误路径 / DSN)
  - middleware 辅助: 18 (split path / build entry / custom action)
- backend go test: 621 → **702** PASS (+81)
- vitest: 147 (未动)
- **总**: 768 → **849** tests
- **database 覆盖率**: 21.1% → **55.3%** (+34.2%)
- tsc: 0 errors (维持)

### 估时 vs 实测

| | 估时 | 实测 | 加速 |
|---|---|---|---|
| Batch 1 (3 项) | 13h | ~4.5h | 2.9x |
| Batch 2 (2 项) | 5h | ~1.5h | 3.3x |
| **v1.4 总** | **18h** | **~6h** | **3x** |

## [v1.3.0] - 2026-06-17

🎨 **次版本** — 中优先级 UX 改进 (路由元信息 + 状态展示一致性 + 响应式)

### 新增组件 / Hook

- **`useDocumentTitle`** (`src/hooks/useDocumentTitle.ts`) — 路由级 title 同步
  - 格式: `document.title = 'ITmanager - {page}'`
  - 卸载时还原 base, 避免 SPA 切换残留
  - 12 个 page 接入: 资产管理 / 告警中心 / 工单管理 / 仪表盘 / 故障 Runbook / 值班管理 / 系统设置 / 告警抑制 / 指标快照 / 资产诊断 / 机房机柜 / 网络拓扑
- **`SeverityTag`** (`src/components/SeverityTag.tsx`) — 严重度统一展示
  - P0-P5 六档配色 (gray/blue/gold/orange/red/magenta), 圆角 + 图标
  - AlertTable / Runbook 两处接入, 删除冗余 `SEVERITY_COLOR` map
- **`AppBreadcrumb`** (`src/components/AppBreadcrumb.tsx`) — 自动面包屑
  - 解析 `useLocation().pathname` + 路由表生成面包屑
  - 详情页 `:id` 参数转 `ID: <name>` (读 `useParams` + 资源 cache)
- **`useResponsiveTable`** (`src/hooks/useResponsiveTable.tsx`) — 响应式断点 hook
  - 用 AntD `Grid.useBreakpoint` 检 `{ xs }`, mobile (xs) 时表格 → 卡片列表
  - 配 `MobileCardList` 组件 (Stack + Card, 不引 antd-mobile 15MB 冗余)
  - 资产页接入, mobile 视口避免横向溢出
- **`CommandPaletteTrigger`** — Header 触发按钮 (🔍 搜索 ⌘K)
  - 配合 `useCommandPaletteStore` (zustand) 跨组件共享 open 状态
  - `useGlobalHotkey` 用 `useCommandPaletteStore.getState()` 防 stale closure

### 改进

- **批量操作进度** — Alerts 的批量 ack/resolve 从单次 API 改逐条 await
  - 后端 bulk endpoint 不可见进度, 改前端循环可显示 X/Y
  - 加 `Modal` + `<Progress percent={Math.round(done/total*100)}>` + 失败计数
  - trade-off: 网络请求 N 倍, N<100 可接受
- **键盘可达性** — Progress modal `closable={false}` 避免 Esc 中断后台进程

### 测试

- 新增 15 tests: useDocumentTitle 4 / SeverityTag 6 / AppBreadcrumb 5
- vitest: 128 → **147** PASS (+19)
- tsc: 0 errors (维持 v1.2.0 成果)
- backend go test: 621/621 (未动)

## [v1.2.0] - 2026-06-17

✨ **次版本** — UI/UX 易用性改进

### 新增组件

- **`EmptyState`** (`src/components/EmptyState.tsx`) — 统一空状态组件
  - 5 个 preset: `no-assets` / `no-alerts` / `no-tickets` / `no-racks` / `no-search-result`
  - 标题 + 描述 + 操作按钮三段式布局
  - 紧凑模式 (`compact` prop) 适配表格内嵌
  - 6 个 page 已接入: Alerts / Assets / Tickets / AlertSuppressions / Runbook / Topology
- **`LoadingSkeleton`** (`src/components/LoadingSkeleton.tsx`) — 统一加载占位
  - 5 个 variant: `table` / `kpi-cards` / `detail` / `chart` / `list`
  - 用 AntD `Skeleton` active 动画，首屏不抖
  - 路由级 `Suspense fallback` 改用 skeleton 替"加载中…"文字
- **`StatusPage`** (`src/pages/StatusPage.tsx`) — 通用状态页
  - 3 个导出: `NotFoundPage` (404) / `ForbiddenPage` (403) / `ServerErrorPage` (500)
  - 提供"返回上一页"+"返回首页"两个动作
  - 状态码色块: 4xx 蓝/紫 (用户侧), 5xx 红 (服务器侧)

### 改进

- **暗色模式 Header 配色** — Header 暗色模式用 `colorBgElevated`，与 Sider 形成反差（避免"中间割裂"）
- **Login 页美化** — 大 logo + 三色渐变背景 + 渐变 logo + 圆角阴影
  - 记住用户名 (Checkbox + localStorage)
  - 忘记密码链接 (placeholder)
  - 用 `useNavigate` 跳回 401 重定向目标 (替代 `window.location.href` 丢 state)
- **404 路由** — 不再静默跳首页，渲染 `<NotFoundPage>`

### 顺手修复 — 类型卫生 (24 → 0 tsc error)

6 个 test 文件补 `import '@testing-library/jest-dom'`，消掉 baseline 23 个 `toBeInTheDocument` 类型错。
`Oncall.tsx:142` `levelsJson` 字段加 cast `EscalationPolicy & { levelsJson?: string }`。

### 测试

- frontend: **128** / 128 pass (was 126, +2 Login 新增: 记住用户名 + 恢复)
- backend: 621 / 621 (no change)
- tsc: **0 errors** (was 24 baseline, -24 净消)

### 性能数据

- v1.2 估时 6h → 实测 **~1.5h** (4x 加速, 含 tsc 24→0 顺带改)
- 涉及 12 文件, 净增 3 组件 + Login 改写 + 5 test 加 import

## [v1.1.0] - 2026-06-17

✨ **次版本** — 代码审计 P1+P2 修复

### 修复 (P1 — v1.0.3 已 ship, 在 v1.1 累计)

- **API key `last_used_at` 异步批量写**: 1000 QPS → 1 UPDATE/30s
- **NetBox `SyncDevices` 分页 (100/page)**: 不再漏 50+ 设备
- **`SyncAll` `errors.Join` 合并失败**: 监控告警不再漏报
- **401 CustomEvent + `useNavigate`**: 保留路由 state + 登录跳回

### 改进 (P2)

- **M3-P2-3 错误类型化**: Create 撞 unique 约束 → `service.ErrAlreadyExists` → 409
  - 5 services + 5 handlers: asset/ticket/channel/runbook/alert_suppression
  - `isUniqueViolation()` helper (gorm ErrDuplicatedKey + SQLSTATE 23505)
- **M3-P2-1 分页索引**: 4 张表 `idx_*_created_at_desc` (assets/tickets/runbooks/users)
- **M3-P2-2 Bulk 事务**: `BulkAcknowledge/Resolve/Delete` 包 `gorm.Transaction`
- **M2-P2-1 Zabbix auth TTL + auto-relogin**: 30min TTL + 主动重登 + 检测 -10002 自动重试
- **M3-P2-4 Notification trigger**: Acknowledge/Resolve 落 `notification_logs` (pending)
  - 实际发送 (dingtalk/email) 由 v1.2 worker 消费
- **M4-P2-1 拦截器清理**: `api.ts` 空壳请求拦截器删除 (since C-F5 cookie auth)
- **M1-P2 ADR-001 rate limit 归属**: 4 方案对比，推荐 per-route middleware (v1.2 实现)

### 数据库

- `000008_list_pagination`: assets/tickets/runbooks/users `created_at DESC` 索引
- `000009_notification_logs`: notification_logs 表 + 2 索引

### 测试

- backend: **621** / 621 pass (was 608, **+13**)
- frontend: 126 / 126 pass (no change)
- tsc: 24 = baseline (0 new)

### 文档

- `docs/adr/0001-rate-limit-归属.md`

## [v1.0.2] - 2026-06-17

🐛 **补丁版** — 文档改进

### 改进

- **README.md**: 加 6 个 GitHub badges
  - Release / CI / License / Go / React / Docker
  - 顶部状态从 6/16 推进到 6/17 v1.0.2
- 快速开始章节用 `make deploy` 重写（替代旧 `make run` 路径）

[v1.0.2]: https://github.com/chinesewebman/ITmanager/releases/tag/v1.0.2

## [v1.0.1] - 2026-06-17

🐛 **补丁版** — 一键部署便利性

### 改进

- **Makefile**: 新增 3 个一键部署 target
  - `make deploy` — install + docker-up + db-migrate + db-seed（含演示数据）
  - `make deploy-min` — 同上但无种子（生产环境首次部署）
  - `make deploy-status` — 8 服务健康检查（PG/Redis/API/Web/NetBox/Zabbix/GLPI/Graylog）
- 私有 `_wait_for_pg` target 轮询 PG 60s（防 migrate 抢跑）
- 链式依赖自动按序，**任一失败立即停下**

[v1.0.1]: https://github.com/chinesewebman/ITmanager/releases/tag/v1.0.1

## [v1.0.0] - 2026-06-17

🎉 **首个稳定版本** — 10 个 PR 落地，覆盖 P0-P2 全链路

### 重大功能 (P0)

| 模块 | 端点 | 说明 |
|---|---|---|
| 资产诊断 | `GET /diagnostics/assets/{id}/timeline` | 聚合 alerts/tickets/status 历史 |
| 告警抑制 | `POST /suppressions` | 规则引擎（去重/静默/抑制） |

### 重要功能 (P1)

| 模块 | 功能 | 说明 |
|---|---|---|
| 值班升级 (P1-2) | 值班+升级引擎 | oncall 表 + 升级策略 |
| 拓扑图 (P1-1) | 网络拓扑可视化 | 一龙开发 |

### 次要功能 (P2)

| 模块 | 功能 | 说明 |
|---|---|---|
| 故障 Runbook (P2-1) | Runbook 引擎 | 故障自动恢复 |
| 指标快照 (P2-2) | Zabbix 兜底 | 离线数据降级 |

### 小改进 (S 级 / 6/17)

| 改进 | 内容 | Commit |
|---|---|---|
| 暗色模式 (S-1) | Antd darkAlgorithm + zustand persist | `ab05d3d` |
| 误报 ML (S-2) | 标记误报 + CSV 训练集导出 | `e502701` |
| Cmd+K (S-3) | 全局搜索跨资源（资产/告警/工单） | `f7e98eb` |

### 小改进 (A 级 / 6/17)

| 改进 | 内容 | Commit |
|---|---|---|
| A-1 网络探活 | ICMP ping + traceroute + exec.LookPath 验证 | `6edd9c9` |
| A-2 复盘 PDF | 嵌入霞鹜文楷 TC (OFL, 15MB) 中文 PDF + io.Writer 流式 | `ea70644` |
| A-3 KPI 仪表盘 | MTTR/MTTD/告警密度/SLA + 阈值常量 | `2b893bc` |
| A 级 review | traceroute dead code + binary 验证 + 字体 + 流式 + KPI sqlmock | `3a667e8` |

### 测试指标

| 指标 | 数值 |
|---|---|
| 后端 packages | 22 |
| 后端测试 | 603 pass |
| 前端 test files | 19 |
| 前端测试 | 117 pass |
| 覆盖率 | 61.2% |
| tsc baseline | 24 errors (0 new from baseline) |

### 性能数据

| 模块 | 估时 | 实测 | 加速 |
|---|---|---|---|
| S 级 3 项 | 4.5h | 3.5h | 1.3x |
| A 级 3 项 | 9h | 2.75h | 3.3x |
| A 级 review | 1h | 0.5h | 2x |
| **总计** | **14.5h** | **6.75h** | **2.1x** |

### 已知限制

- 中文 PDF 字体 15MB 嵌入二进制（binary 增大约 15MB）— 见 `assets/FONT-LICENSE.txt`
- KPI 阈值硬编码（`KPI_THRESHOLDS`）— v1.1 改为环境变量注入
- tsc 24 errors 为 baseline（testing-library `toBeInTheDocument` 类型 + 1 Oncall levelsJson），非本次引入

### 安装/升级

```bash
# 拉取 v1.0.0
git checkout v1.0.0

# 后端
cd backend && go mod download && go run cmd/server/main.go

# 前端
cd frontend && npm install && npm run dev
```

[v1.0.0]: https://github.com/chinesewebman/ITmanager/releases/tag/v1.0.0
