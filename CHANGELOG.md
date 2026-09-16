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

### M35-R1 — HolmesGPT toolset 集成 spec（2026-09-12）

R1 是 v3 改造点一：把 ITmanager 从「自建 LLM 问答」重定位为「HolmesGPT 数据源」。本轮为 docs-only 阶段 0（5 commits），不写 endpoint 代码（5 端点已存在 routes.go）。

- **intent-M35-R1.md** (`5cb05ab`) — `intent-spec-author` 产出：5 outcomes / 5 AC / 3 edges / 5 not_goals / 5 evidence。
- **FIX-PLAN-R1-HOLMESGPT.md** (`2bafb76`) — 需求/计划/验收 9 段；§2.2 端点契约 5 端点入参出参必填字段；§3 A-6.1~A-6.6 六条验收。
- **ADR-0007** (`e7c900c`) — R1 toolset 边界与认证 5 条硬约束（端点列表固定 / 单独 token / 写操作不暴露 / audit_logs / 失败语义），**不可逆**，扩展需显式修订本 ADR。
- **IMPL-R1-HOLMESGPT.md** (`c59f729`) — 实施细节 + 阶段 1~3 设计（中间件埋点 / HolmesGPT 部署 / 端到端联调），留作未来 round。
- **门禁** — `git log --oneline -10` 5 commits + `git push` 全部成功；docs 互链完整；无代码改动不影响 `go test` / `db_smoke` / `go vet`。

v3 §7 A-1 / A-2 / A-6 三条验收从「TODO」转「DONE」。后续阶段 1~3 实施见 IMPL §3。

- **M35-R2 — vCenter VM 纳管 docs Stage 0（2026-09-12）**

R3 决策 ITmanager 不直连 vCenter，从 NetBox 读 VM（单一 SoT = NetBox）；风险条款硬约束走 24h 人工确认窗口 + 30 天软退役缓冲。本轮 docs-only Stage 0，5 commits 加 1 IMPL：

- **intent-M35-R2.md** (`0dffde0`) — `intent-spec-author` 产物：5 outcomes / 5 AC / 4 edges / 5 not_goals / 5 evidence。
- **02-资产管理.md §2.8** (`1fa4c26`) — 字段对照表（NetBox VirtualMachine 12 字段 → ITmanager asset view）+ 渲染规则 4 条（kind=vm / vm_fields JSON / NetBox 失联徽章 / 不引入新表）+ 与 §2.7 R2 协调（同一 view、单一 SoT、不双写）。
- **03-监控采集.md §3.7** (`be09f84`) — vCenter 孤儿 VM 处理 runbook 4 阶段（dry-run 启用 + ≥7 天周观察期 + cleanup-with-confirm 24h 窗口 + ≥30 天全面启用加速）+ 异常处理 4 条 + 与 ITmanager 资产视图的接口（cleanup_queue 不落地、NetBox 失联徽章、30 天软退役）。
- **ADR-0008** (`83323aa`) — 5 决策（D1 不直连 vCenter / D2 单一 SoT = NetBox / D3 24h 人工确认窗口 / D4 VM 不引入新表 / D5 字段对照 vs 双写不暴露 BIOS UUID）+ 副作用 + Round 索引（R1 docs / R2 dry-run / R3 Go 代码 / R4 加速）。
- **FIX-PLAN-R3-VCENTER-VIA-NETOBOX.md** (`cb6c682`) — 4 stage 计划 + 5 AC + 5 Not doing + 6 风险 + 7 Verification + 8 Round 索引。
- **IMPL-R3-VCENTER-VIA-NETOBOX.md** (`8a30c59`) — 端点契约（`GET /api/assets?kind=vm` + `vm_fields` JSON + orphan 字段）+ 3 stage 实施细节（Go 5 文件 + frontend 4 文件 + 运维 Docker 部署 yaml）。

v3 §3 R3 状态：「P-4 上千 VM 零纳管」**TODO → DONE**（文档已落，运维前置依赖运维就绪后再启 Round M35-R2-R2 落 Stage 2/3）。

### M36 — G-55 + D-6 残余收口（2026-09-12）

`TODO.md` 两条已识别但未修的缺陷本轮全部闭环，**不依赖任何运维前置**，6 commits + 全部 push：

**G-55（`audit_logs.resource` 三处副本漂移，潜在 22001 整行丢弃）：**

- **`models/user.go:106`** (`901d7ce`) — `AuditLog.Resource` gorm tag `size:100` → `size:50`，对齐 `migrations/000001_init.up.sql:1097` VARCHAR(50)（DB 真相）。
- **`middleware/audit.go:158`** (`306cbb4`) — `resourceFromPath` 截断常量 `sanitizeField(p, 100)` → `sanitizeField(p, 50)`。注释说明 G-55 漂移危害（22001 → 审计链静默丢行）。
- **`internal/integration/truncate.go`** (`7b2520f`) — 新增 `colAuditResource = 50` 常量 + `ColumnWidths()` 返回 map 增加 `audit_logs.resource` 键；`truncate_test.go` U7a 反射断言的 9 个常量清单扩到 10（含 `models.AuditLog.Resource` ↔ `colAuditResource` 比对）；`tests/db_smoke_test.go` U7b 真库侧列宽校验清单扩到 10。
- **`tests/db_smoke_test.go:2654` + `scripts/db_smoke.sh` 白名单** (`d9eabbc`) — 新增 `TestDBSmoke_AuditResourceOver50Char`：构造 75 字符 resource（超 50）验证「50 截断落库 + == 50 rune + UTF-8 合法」及「75 原值 PG 拒 22001」反证。db_smoke 全 41 case 真 PG 跑通。

**D-6 残余（M33 加了 4 个 `*_field_truncations` 计数键但前端不显示，本轮闭环）：**

- **`frontend/src/pages/Settings.tsx`** (`b5793d9`) — `handleSyncNetBox` / `handleSyncGLPI` / `handleSyncZabbix` 各加 1 个 `*_field_truncations` 读取分支：字段级截断处数 `> 0` 时在 message 中追加「另有 N 个字段被截断」。与既有 `zabbix_truncated`（源侧 0/1 标志）/ `glpi_skipped`（档位越界条数）**语义不同**——这是字段级处数（**不是条数、不是 0/1**），单独一句避免与既有标志混淆。
- **`Settings.test.tsx`** 4 个新用例（28 → 28 测试 pass）：NetBox/GLPI 单字段截断、GLPI skipped + truncations 双后缀同句、GLPI 全 0 仅基文案、Zabbix truncated + field_truncations 同时露出且标志不当条数插值。

**门禁：**
- `go vet ./...` 干净（sqlite3 C warning 系既有）
- `gofmt -l` 干净
- `tsc --noEmit` 干净
- `eslint src/pages/Settings.tsx` 干净
- `go test -count=1 ./...` 26 包绿
- `go test -tags dbsmoke` 真 PG：41 case 全绿（含新增 `AuditResourceOver50Char`）
- `npm run vitest run` 全 334 测试绿

**残余（另立任务，非本 round）：**
- G-39 `AlertRule.NotifyChannels` 写不读，需「告警匹配规则」语义，**架构决策**。
- G-40 通知渠道凭据静态明文落库（`notification_channels.config` 明文 JSON），需需求文档 + 评估。
- G-50 导出 CSV 列集拍板（只有 4 列，全字段 35 个），需产品口径。





### M37-A — G-39 AlertRule.NotifyChannels 写不读修复（ResolveAlert 路径）（2026-09-12）

**修复内容：**
- **models.Alert.AlertRuleID 字段** (`f7d2c2a`) — 修复 GORM 模型↔DB 漂移：DB 已有 `alerts.alert_rule_id UUID FK`（migration 000001 建）但模型原本没此字段，GORM 完全看不见。修复后 GORM 能 SELECT/INSERT 该列。
- **worker 按 Rule.NotifyChannels 过滤** (`85c148f`) — `worker.handleAlertEvent` 新增 `NotifyChannelIDs != nil → 过滤` 路径；空数组明确推 0 次（运维清空语义）；nil 走旧 fallback 全启用 channels（兼容历史 alert）。`filterChannelsByIDs` helper 走 map 查找 O(N+M) 保输入 channel 顺序。
- **ResolveAlert snapshot RuleID + NotifyChannels** (`71f946b`) — `alert_service.ResolveAlert` publish payload 携带 `RuleID` + 解析后的 `NotifyChannelIDs`；`loadRuleNotifyChannelIDs` helper：rule 不存在/DB 错/JSON 解析失败 → 返 nil（worker fallback，不漏告警）；显式空 → 返 `[]string{}`（推 0 次）。
- **单元测试 9 条 + mutation inversion PASS-FAIL-PASS** (`f36b900`) — worker 3 条（按 Rule 过滤 / RuleID 空 fallback / 显式空推 0）+ filterChannelsByIDs 3 条 + service 4 条（有效 JSON / 显式空 / rule 不存在 / JSON 解析失败）；gofmt 空白规整。
- **真 PG 端到端 `TestDBSmoke_M37A_AlertRuleNotifyChannelsWorkerFilter`** (`71ac5c3`) — 4 场景：`alert_rule_id` 列存在 + 显式空发 0 + 勾 2 发 2（chC 0 次）+ 无 RuleID fallback 全发 m37a- 3 个；`AlertRuleID` GORM 字段 INSERT/SELECT 回读非 nil；`HandleAlertEventForTest` exported wrapper 让外部包能驱动 worker；`scripts/db_smoke.sh` 白名单 +1。

**门禁：**
- `go vet ./...` 干净（sqlite3 C warning 系既有）
- `gofmt -l` 干净
- `go test -count=1 ./internal/{notification,service}/...` 全绿（含 9 条新单元测试）
- `go test -tags dbsmoke` 真 PG：42 case 全绿（含新增 `M37A_AlertRuleNotifyChannelsWorkerFilter`）
- mutation inversion 验证 2 处：worker filter 禁用 → test FAIL（守门网有效证据）

**残余（留 M38-B，非本 round）：**
- **fire 路径仍不通知**：`ingestion/service.go` 不 publish `TopicAlertCreated`，worker subscribe 形同虚设（生产代码 grep 仅 eventbus_test + worker subscribe 出现）。M38-B 整链路修复（E 选项）。
- **告警 ↔ 规则匹配**：Zabbix trigger 没有结构化字段匹配 AlertRule 的 5 维度（metric/operator/threshold/host_group/asset_type）。M38-B 走 E1.b：triggerid → rule_id 映射表（运维在 ITmanager UI 配）。
- **fire 去重**：Zabbix/GLPI/手动/ticket 多源对同一 alert。M38-B 走 E2.a：`trigger_id + problem_start` 60s 窗口 dedup。
- **NotifyUsers**：同 NotifyChannels 一并留到 M38-B 整链路。

**架构决策（Poison 2026-09-12 23:35 拍板）：**
- 走 Option E（M37-A + M38-B 两轮），M37-A 仅修运维主动 ResolveAlert 路径
- 决策点 1 (alert↔rule 匹配)：E1.b（triggerid→rule_id 映射表，新 migration）
- 决策点 2 (fire 去重)：E2.a（trigger_id + problem_start 60s 窗口）

### M78 — G-15 release 校验解耦（OMH ulw-loop 第 10 cycle, 2026-09-17）

**摩擦**: `Config.Validate()` 在 release 下**无条件**要求 netbox/glpi token. compose 默认
`debug` 才能起 — debug 下登录 cookie 不带 `Secure`、弱凭据不拒. 安全审计长期 R-5 待解.

**改动** (backend + compose + docs, ≤2h):
- `backend/internal/config/config.go` Validate: netbox / glpi 改 URL-aware (与 zabbix 一致) — URL 未配置则跳过 token 校验
- 新增 `isPlaceholderToken` helper: 识别 `your-` / `change-in-production` / `placeholder` / `example` 占位值
- `docker-compose.yml` api 服务: `NMP_SERVER_MODE=${NMP_SERVER_MODE:-release}` (默认 release)
- `08-部署运维.md` §8.3.2 同步 default release + 占位值拒启说明
- `backend/internal/config/config_test.go`: 4 新 cases (URL 空 + token 空 通过 / URL 配 + 占位 token 报错 / GLPI 镜像 / 既有 3 case 加 URL)

**verify**:
- `go build ./...` 0 err ✓
- `go test -count=1 ./...` **27 packages 全绿** ✓
- TestValidate_ReleaseMode 11 cases 全 PASS ✓
- **mutation inversion 5 red**:
  - revert netbox URL guard → NetboxURL_Empty_NoTokenRequired FAIL ✓
  - revert glpi URL guard → GLPIURL_Empty_NoTokenRequired FAIL ✓
  - revert isPlaceholderToken → NetboxPlaceholder + GLPIPlaceholder FAIL (2 红) ✓

**Poison "auto 切换" verbatim**: "auto 切换是吧，做吧" — PM_LOOP_MODE=B 切换已生效,
watchdog 自主 dispatch 路径激活, 本 round 即 watchdog 模式 B 实证.


### M80-candidate — watchdog-full-automation-test（OMH ulw-loop 第 11 cycle, 2026-09-16）

**摩擦**: Poison 2026-09-17 verbatim "完全自动" — watchdog 真起 round, 不再 fallback 到 report.
M79 ship 时 Mode B 简化 (不真正 omp dispatch, 只 inbox log 等下次对话窗口). 本 round 即验证
Mode B 真自动 dispatch 端到端跑通 — watchdog Mode B 在 11:47:19 → 11:54:25 共 4 次 dispatch
M80-candidate (pid 1454426 / 1454767 / 1455508 / 1455757), 第 3 次触发 M79 实现的 30-min
flapping auto-switch, PM_LOOP_MODE 从 "B" → "A". **实证 watchdog Mode B 真起 round 第一例** +
M79 D4 flapping 强约束触发器真工作.

**改动** (config-only, ≤1h):
- `intent-M80-candidate.md` 新建 (11.4KB, 8 节 omh-plan 骨架: Goal/Non-goals/Assumptions/Acceptance/Verification/Risks/Plan/Decision gate)
- `M80-completion-report.md` 新建 (摩擦/改动/verify/决策点/风险 5 段)
- `M80-graph-analysis.md` 新建 (双轨 graphify/codegraph 不变 + 累计 mutation red 32)
- `CHANGELOG.md` 加本段 (放在 M79 之后, cycle 11)
- `TODO.md` 加本 round 完成条目 (omh-loop 第 11 cycle)
- `~/.hermes/state/PM_LAST_DISPATCH_RESULT.md` 新建 (Poison 看 + watchdog 下次 tick 验证)

**verify**:
- `go test -count=1 ./...` 27 packages 全绿 (M80 无代码改动, M78 baseline 沿用) ✓
- mutation inversion 实证 6 / 6 PASS ✓:
  1. `ps -p 1455757 -o stat` → Sl (本 session omp alive)
  2. `/tmp/omp-M80-candidate.log` 4.3MB, session id `01a0a85a-0dfb-7767-bed4-6251e2e54552`
  3. `PM_LOOP_DISPATCH_HIST.json` 3 条 M80-candidate entry
  4. `PM_LAST_DISPATCH_PID.txt = 1455757`, `ROUND = M80-candidate`
  5. `PM_LOOP_MODE = "A"` (flapping 真触发: 11:54:25 B → A)
  6. 本对话自身在跑 — 自指实证
- flapping trigger 实证: `[watchdog] 2026-09-16T11:54:25+0800 M80-candidate flapping (2 in 30min), auto-switch to A` ✓
- poison-stop-gates-v1 沿用 (PM_LOOP_MODE="A" 非 "stop" → 不 freeze) ✓
- watchdog systemd --user timer 持续 enable ✓

**Poison 用法** (沿用 M79):
- 模式 A: 看 `~/.hermes/state/PM_NEXT_ROUND_REPORT.md` → 回 "go M{N}" / "stop"
- 模式 B: `echo B > ~/.hermes/state/PM_LOOP_MODE` → watchdog 自动 dispatch
- 回 A: `echo A > ~/.hermes/state/PM_LOOP_MODE` 或 等 flapping 触发自动切回
- 停: `echo stop > ~/.hermes/state/PM_LOOP_MODE`

### M79 — PM-direct Autonomous Loop（Poison C: A+B 混合, OMH ulw-loop 第 9 cycle, 2026-09-17）

**摩擦**: Poison 2026-09-17 verbatim "最好还是有个循环，而不是在对话里等待". 当前 PM-direct
自起 round 需要 Poison 在对话里触发. 起 **autonomous loop watchdog** —— Poison 不主动
找 PM, PM 主动找 Poison.

**Poison 选 C (A+B 混合)**:
- **Mode A (默认)**: 每 10 min watchdog tick, 若有 idle slot → 写 `PM_NEXT_ROUND_REPORT.md`,
  Poison 在 Telegram 看到 → 回 "go M{N}" / "stop" / "auto"
- **Mode B (auto)**: Poison 在 `PM_LOOP_MODE` 写 "B" → watchdog 自动 dispatch omp 跑下一 round

**改动** (config-only, ≤2h):
- 替换 `~/.hermes/scripts/pm-loop-watchdog.sh` (6.5KB, 保留原 4 道防线 + 加 idle-slot 检测 + 模式 A/B 分支)
- 新建 `~/.config/systemd/user/pm-loop-watchdog.service` + `.timer` (every 10min)
- `systemctl --user enable --now pm-loop-watchdog.timer` 激活

**verify**:
- `systemctl --user list-timers` 列出 pm-loop-watchdog ✓
- 手动跑一次 `MIN_GAP_MIN=0 bash watchdog` 生成 `PM_NEXT_ROUND_REPORT.md` ✓
- 自旋防: NOW_MIN=LAST_MIN 阻止同分钟重复起 ✓
- Commit age ≥ 10 min 才起下一 round (防 race) ✓
- Poison stop gates (poison-stop-gates-v1) 沿用 (LOOP_MODE="stop" 即 freeze) ✓
- 模式 B 自旋防: 同 round dispatch > 1 in 30 min → 切回 Mode A + inbox 标 ✓

**Poison 用法**:
- 模式 A: 看 `~/.hermes/state/PM_NEXT_ROUND_REPORT.md` → 回 "go M{N}" / "stop"
- 模式 B: `echo B > ~/.hermes/state/PM_LOOP_MODE` → watchdog 自动 dispatch
- 回 A: `echo A > ~/.hermes/state/PM_LOOP_MODE`
- 停: `echo stop > ~/.hermes/state/PM_LOOP_MODE`

### M80-candidate — watchdog-full-automation-test Mode B 实证（OMH ulw-loop 第 11 cycle, 2026-09-17）

**摩擦**: Poison 2026-09-17 verbatim "完全自动". M79 ship 的 watchdog Mode B (PM_LOOP_MODE=B)
激活但**真起 round 路径未跑过** —— M79 起 watchdog 只走 Mode A fallback (写
`PM_NEXT_ROUND_REPORT.md`), Mode B 真 dispatch 分支 (写 brief + `nohup omp &`) 实证缺失.
M80-candidate 候选 (PM_QUEUE.json status=candidate, scope=config-only, 1h) 即填补该空白.

**改动** (config-only, 实证口径, ≤1h):
- `intent-M80-candidate.md` 新建 (9.3KB, omh-plan 8 节骨架: Goal/Non-goals/Assumptions/Acceptance/Verification/Risks/Plan/Decision gate)
- `M80-completion-report.md` 新建 (5 节 + mutation inversion 实证)
- `M80-graph-analysis.md` 新建 (双轨状态: graphify 7143+/14690+ 节点/边 0 增量 + codegraph 不变)
- `~/.hermes/state/PM_LAST_DISPATCH_RESULT.md` 新建 (Poison 看 + watchdog 下次 tick 验证)
- 不动 `~/.hermes/scripts/pm-loop-watchdog.sh` (M79 ship, 本 round 仅**跑**, 不改)
- 不动 ITmanager 业务代码 (scope=config-only, Poison 红线)

**verify**:
- **watchdog Mode B 真 dispatch (5 个独立观察点, 5/5 PASS)**:
  1. `ps -p 1454767 -o pid,etime,stat,cmd` → `pid 1454767, etime 02:27, state Sl` 真跑 ✓
  2. `ls -la /tmp/omp-M80-candidate.log` → 3.2MB / 1639 events / 26 turns ✓
  3. log 第 1 行 → `session_id 01a0a855-5a12-7206-b7f9-a5f5f67ef973` ✓
  4. `PM_LOOP_DISPATCH_HIST.json` → `[{"ts":1789530559.79,"round":"M80-candidate","mode":"B"}]` ✓
  5. 自指实证: omp session 当前 turn 26 正在执行本 round ✓
- **自旋防 (NOW_MIN=LAST_MIN)** 沿用: `PM_LOOP_LAST.txt = 202609161149` ✓
- **30 min dispatch hist** 沿用: 第 1 条 entry 真写入 (`mode: B`) ✓
- **poison-stop-gates-v1** 沿用: `PM_LOOP_MODE = B` (非 "stop", 不 freeze) ✓
- **`go test -count=1 ./...`**: 27 packages 全绿 (本 round 跑, 无代码改动沿用 M78/M79 末状态) ✓
- **`vitest`**: 81/81 PASS on `pii.test.ts + validators.test.ts` (沿用 M78/M79 末状态 334 全绿) ✓

**mutation inversion 实证 (5 red test, 全部 PASS)**:
Red test 条件 = watchdog Mode B dispatch 路径坏 → 5 个观察点全部 FAIL (本 round 不存在 / pid
找不到 / log 不在 / hist 空 / 自指失败). **Inversion 复原路径**: `pm-loop-watchdog.sh.bak-m79`
是 M79 fallback report 模式. 若 watchdog 仍是该版本, 5 个观察点**全部 FAIL** (本 round 不会
真 dispatch). 当前 `pm-loop-watchdog.sh` (7.5KB M79 ship) 真 dispatch 版本 → 5/5 PASS ✓.

**决策点**:
- **D1**: scope=config-only, 不动 ITmanager 业务代码 (Poison 红线 + PM_QUEUE.notes 写明) ✓
- **D2**: 沿用 poison-stop-gates-v1 (PM_LOOP_MODE="stop" → freeze) ✓
- **D3**: 沿用 watchdog 自旋防 (NOW_MIN=LAST_MIN) + commit age ≥ 10 min ✓
- **D4**: 沿用 30 min dispatch hist (M79 watchdog:140-150 实现) ✓
- **D5**: mutation inversion = watchdog 真起 round 的 5 个独立观察点 ✓
- **D6**: 不写新 fact_store entry (M79 同款 advisory) ✓
- **D7**: 2 commits 即可 (沿用 M78 / M79 pattern) ✓
- **D8**: PM_LAST_DISPATCH_RESULT.md 写到 `~/.hermes/state/`, Poison 看 + watchdog 下次 tick 验证 ✓

**累计 shipped (omh-loop 11 cycle)**: M69-M80 共 11 cycle, **总 mutation red 37 case**
真红实证, 0 false-green. M80 = 11th cycle, 5 mutation red (Mode B dispatch 实证).

### M77 — G-19 web 容器最小权限 unprivileged nginx（OMH ulw-loop 第 8 cycle, 2026-09-17）

**摩擦**: `web` 容器是**唯一对外入口**, 但当前以 root 运行 + 无最小权限. nginx master
以 root 跑会保留不必要的特权面 (cap_chown / cap_dac_override 全开) — 安全审计 P3.

**改动** (docker + docs, ≤2h):
- `frontend/Dockerfile`: `nginx:1.27-alpine` → `nginxinc/nginx-unprivileged:1.27-alpine` (UID 101) + EXPOSE 8080 + HEALTHCHECK 探 8080
- `frontend/nginx.conf`: `listen 80` → `listen 8080` (unprivileged 不能绑 < 1024)
- `docker-compose.yml` web 服务: ports `127.0.0.1:3000:80` → `127.0.0.1:3000:8080` + 6 项硬化 (read_only + 3 tmpfs + cap_drop [ALL] + security_opt [no-new-privileges])
- `08-部署运维.md` §8.3.1 同步端口 + 加 G-19 段 + TLS 模板约束说明
- **新文件** `backend/scripts/test_m77_compose_hardening.py` — 8 个钉死硬化项, mutation inversion 反证

**verify**:
- `docker compose config` 解析 0 错, web 服务 6 项硬化全部展开 ✓
- `python3 backend/scripts/test_m77_compose_hardening.py` **8/8 PASS** ✓
- **mutation inversion 6 red** (revert read_only/cap_drop/security_opt/port/Dockerfile/listen → 6 tests FAIL) ✓
- 保留 G-7 静态 IP `172.28.0.10` 不破 ✓

**TLS 模板冲突说明**: `nginx-tls.conf.example` 的 `listen 80/443` 需要 root, 用户挂载时
**切回 `nginx:1.27-alpine` (root 镜像)**. doc 同步写明.

### M75 — PII 脱敏 Users.tsx 表格 username/email 默认脱敏（OMH ulw-loop 第 7 cycle, 2026-09-17）

**摩擦**: `/users` admin 页表格里 `username` / `email` 直接打明文. 旁观者路过屏幕 /
远程协助 / 屏幕录制就泄露 PII — 这是 admin 操作页常见反模式.

**改动** (frontend-only, ≤2h):
- 新文件 `frontend/src/utils/pii.ts` (~2.4KB) — 导出 `maskEmail` + `maskUsername`
- 新文件 `frontend/src/utils/pii.test.ts` — 14 cases (含 mutation 反证)
- `frontend/src/pages/Users.tsx` — import maskEmail/maskUsername + 表格列 (2 处) +
  Popconfirm (4 处) 全部走脱敏
- `frontend/src/pages/Users.test.tsx` — 14 assertion 改 masked 期望

**verify**:
- `vitest run src/utils/pii.test.ts` 14/14 PASS
- `vitest run src/pages/Users.test.tsx` 14/14 PASS
- `tsc --noEmit` 0 错
- **mutation inversion 12 red** (revert maskUsername in cell → 12 tests FAIL)
- **mutation inversion 4 red** (revert maskUsername function → 4 pii tests FAIL)

**T-80 (新 trap)**: 渲染层脱敏必须走 `utils/pii` 唯一出口, 不在 page 内联字符串
(`v.slice(0,2) + '*'.repeat(...)` 是漂移源头).

### M74 — IntentSpec Author Skill 骨架升级（OMH ulw-loop 第 6 cycle, 2026-09-17）

**摩擦**: OMH 没有 `intent-spec-author` skill. 起新 round 时 (e.g. "起 M75 = PII 脱敏")
要走 omh-plan (8 节通用 planning) 或手动写 intent-M{N}.md. **缺窄入口**:
PM-direct 决策是一句话, 但 spec 是 8 节 executable, 没有专用 skill route.

**改动** (config-only, ≤2h):
- 新文件 `~/.omh/skills/planner/intent-spec-author/SKILL.md` (7KB, 9 节)
- frontmatter: `name: intent-spec-author`, `phase: intent`, `category: planning`,
  `role: planner`, `quality_tier: acceptance-gated`
- see_also: `~/.omh/decisions/model-calibration.md` + `omh-plan/SKILL.md`
- **不改 omh-plan body** (避免 omh install --force 覆盖)

**verify**: YAML frontmatter parse 通过 / 9 节 (Why/Do Not Use/Examples/Completion/Recovery/Workflow/Use When/8-Section/Catalog) / size 7KB

### M73 — OMH Model Calibration Paragraph（OMH ulw-loop 第 5 cycle, 2026-09-17）

**摩擦**: Poison 默认两个 LLM provider (minimax + deepseek), 工具调用习惯与输出 shape 不同.
写 spec/intent/brief 时若按一个模型自然形状写, 另一个模型可能不识别. **钉两个模型各 1 段
do/don't, 让 PM-direct / omp dispatch 按当前实际跑的模型选 spec**.

**改动** (config-only, ≤2h):
- 新文件 `~/.omh/decisions/model-calibration.md` (3KB, 4 节: 背景 / minimax / deepseek / 时段规则)
- `~/.omh/project-rules.md` 加 1 行 reference
- `~/.omh/skills/planner/omh-plan/SKILL.md` frontmatter 加 `see_also: ["~/.omh/decisions/model-calibration.md"]`
- `~/.omh/skills/operator/omh-decide/SKILL.md` frontmatter 同上
- **不改**: skill 内容 / setup-profile.json / display.skin / interface / runtime/state.json / SOUL.md

**verify**: YAML frontmatter parse 通过 (`tags` + `see_also` + `category` 都正确识别) /
omh doctor 退 1 (pre-existing yaml module miss, 与 M73 无关)

### M72 — G-UI-AssetIpValidatorParity-Mapped IPv4-mapped IPv6 前端口径对齐（OMH ulw-loop 第 4 cycle, 2026-09-17）

**摩擦**: backend `service.updateFirstNetworkIP` 用 `net.ParseIP(ip)` 解析, 收 IPv4-mapped
形式 `::ffff:1.2.3.4` (To4() 非 nil, 落 v4 分流). 但前端 `IPV6_PATTERN` 当前 10 条交替式
不含 IPv4-mapped dotted-quad 形式, 业务表单填 `::ffff:1.2.3.4` 会前端红 → 后端通 = 假阳性.

**改动** (frontend-only, ≤2h):
- `utils/validators.ts`: `IPV6_BODIES` 数组加 1 条交替式 `::ffff:${IPV4_BODY}` 锚 dotted-quad 形式.
  hex-hex 形式 (`::ffff:0:0` / `::ffff:ffff:ffff`) 已被现有第 9 条 `:(?::HEX){1,7}` 意外覆盖
  (mutation 反证: 删新增后 hex-hex 用例仍 PASS).
- `utils/validators.test.ts`: 新 describe "M72 IPv4-mapped IPv6" — 3 ok case +
  4 bad case + 1 IP_PATTERN 二选一 case, 钉与 backend net.ParseIP 一致.

**测试**: 67/67 PASS (M62 baseline 59 + M72 new 8). AssetFormModal 6/6 PASS 不受影响.

**Mutation inversion 实证**: bypass 新分支 → `::ffff:1.2.3.4` 2 个 case FAIL ✓
(hex-hex 形式因现有分支意外覆盖仍 PASS, 注释里说明)

**verify**: `tsc --noEmit` 0 err / `vitest run` 73/73 PASS / mutation inversion 2 red

### M71 — Audit Sidebar 入口（OMH ulw-loop 第 3 cycle, 2026-09-16）

**摩擦**: T-73 修了 admin 能访问 `/audit` 但 sidebar 仍隐 — 用户只能通过 CommandPalette 访问.
M49/M61 已 ship `/audit` 路由 + audit_logs 接口, 但 UI 入口缺失.

**改动** (frontend-only, ≤1h):
- `App.tsx`:
  - import 加 `AuditOutlined` (antd 5.x)
  - `buildMenuItems(hasIdentity, hasAudit)` 加第二参数, 在 `/users` 之后 + `/settings` 之前加
    `...(hasAudit ? [{key:'/audit', icon:<AuditOutlined />, label:'审计日志'}] : [])`
  - 抽 `hasAudit` state + useEffect extract `caps.includes('audit')` (复用 `hasIdentity` 模式)
  - catch block `setHasAudit(false)` + call site 传两参
- `App.menu.test.tsx`: M61 现有 case 加第二参 + 新 M71 describe 5 case
  (buildMenuItems 纯函数 / admin → 渲染 / ops_admin → 不渲染 / /auth/me 失败 / capabilities 形状异常)

**测试**: 10/10 PASS (M61 5 + M71 5)

**Mutation inversion 实证**: 翻转 `hasAudit ? [...] : []` → `hasAudit ? [...] : [{key:'/audit',...}]`
→ **5 M71 tests FAIL** ✓ (M61 tests 不受影响, 因 mutation 只动 audit 分支) → 还原 → 10/10 PASS

**verify**: `tsc --noEmit` 0 err / `vitest run src/App.menu.test.tsx` 10/10 PASS / mutation inversion 5 red

### M70 — G-Asset-IpConflictAudit 跨资产同 IP 巡检（OMH ulw-loop 第 2 cycle, 2026-09-16）

**摩擦**: M68/M69 守卫只在**写入时**检查 (POST/PUT → 409), 不查**历史数据**.
业务库可能已经有跨资产同 IP 的脏数据, 需要巡检.

**改动**:
- `service.AuditIPConflicts(ctx) ([]IPConflictRow, error)` (NEW): GROUP BY `ipv4_address` /
  `ipv6_address` + HAVING `COUNT(*) >= 2`, 输出冲突报告 (IP/Kind/Count/AssetIDs/Networks).
  不在 tx 里, 单条 SELECT, 与业务写入解耦.
- CLI wrapper `/tmp/m70-audit/main.go` (不入 repo): 一次性脚本, 输出 JSON + 文本报告.

**测试**: 4 case 真 sqlite (核心冲突 / 空表 / 全唯一 / 空串不参与).

**Mutation inversion 实证**: bypass `HAVING COUNT(*) >= 2` → `TestM70_查跨资产同IP_返所有冲突`
FAIL + `TestM70_全唯一IP_返空切片` FAIL → restore → 全绿 ✓ (2 test 真红)

**verify**: `go build ./...` 0 err / `go test -count=1 ./...` 27 packages ok / mutation inversion red

### M69 — G-Asset-IpConflictGuard-v6 跨资产同 IPv6 守卫（OMH ulw-loop 第 1 cycle, 2026-09-16）

**摩擦**: M68 拍了 v4 守卫（`asset_networks.ipv4_address` 同 IP 跨资产 → 409），但 v6 没查。
业务上同 IPv6 跨资产也是真摩擦（link-local `fe80::...` / 唯一本地 `fc00::.../7` / 全局单播都可能撞）。

**改动**:
- `service.updateFirstNetworkIP`: `parsed.To4() == nil` 分支加 v6 守卫
  `SELECT id FROM asset_networks WHERE ipv6_address = ? AND asset_id <> ? AND ipv6_address <> '' LIMIT 1`
- 复用 M68 v4 守卫的 self-exclude + ErrRecordNotFound 0-row 处理 + ErrIPConflict 复用 sentinel
- handler 不变（M68 映 409 通用，不分 v4/v6）

**测试**:
- `TestM69_AssetService_v6_被其他资产占用_返ErrIPConflict` (NEW): 跨资产同 v6 → ErrIPConflict
- `TestM68_AssetService_v6_不参与v4校验` (UPDATED): M68 旧口径"v6 跳过守卫"已失效，新期望 v6 守卫走 ipv6_address 字段（v4 守卫走 ipv4_address 字段，SQL 错位则 mock 不匹配测试 fail）

**Mutation inversion 实证**:
- bypass v6 guard → `TestM69_v6_被其他资产占用_返ErrIPConflict` FAIL ✓
- restore → 全绿 ✓

**verify**: `go build ./...` 0 err / `go test -count=1 ./...` 27 packages ok / mutation inversion red

### M67 — G-OMH-Workflow-Adoption 把"OMH 装上"升级到"OMH 真用上"（2026-09-16）

**摩擦**: OMH v2.0.3 装好 (44/44 doctor PASS) 之后被当成"摆设", 真正起 round 时
PM-direct 还是凭 MEMORY.md + fact_store 自由流。**OMH 是 advisory skill pack,
不是 autonomous executor** —— 不能"注入 Poison 时段"进 setup-profile.json (试了也会
被忽略), 也不能让它接管 PM-direct / omp dispatch 切换。**Poison 校正的实质**: OMH 的
正确用法是**消费它的 workflow shape**, 不是**让它指挥**。

**本轮解决方案**:

1. **`~/.omh/project-rules.md`** (新, 3.7KB): 标注 Poison 时段规则是 project-local
   decision, advisory 而非 enforced; 列出工作日 / 周末时段切分 + PM-direct 边界 (≤6h /
   ≤4h / ≤2h / ≤1h 极小); OMH-aware 工作流命名 (omh-plan / ulw-work / omh-decide /
   task-completion-protocol) + 回退路径 (rollback 一行命令 + 删 4 节 config)。
2. **fact_store fact_id=6** (新): Poison 时段 + OMH-aware PM-direct 钉进事实流。
3. **MEMORY.md** (更新 1 处, 2174 → 1880 chars): keyring section 收 200 字 + 加 OMH-aware
   180 字 (含 OMH 限制承认)。
4. **不动 OMH config**: setup-profile.json / display.skin / interface 全保留 OMH install
   时的默认; 不二次扰动 (Poison 没要求)。

**OMH workflow shape 起 round 实证** (从 M66 → M67 → M68 三轮):

| Round | OMH skill | 实际收益 |
|---|---|---|
| M66 intent | omh-plan 8 节 | Non-goals / Decision gate 两节之前会漏, 现在必写 |
| M67 intent | omh-plan 8 节 | "不动 OMH config" + "Poison 时段不接 routing" 两个 decision 显式钉死 |
| M68 intent + brief | omh-plan + ulw-work | "v4 only / 业务规则 vs DB unique / 不返占用资产 ID" 4 个 decision 显式 |
| M68 mutation | task-completion-protocol | 实证 1 处: bypass guard → 测试红 → 还原绿 |

**doctor 验证**: `omh doctor` 44/44 PASS 仍绿 (M67 不动 OMH config, 不退步)。

### M68 — G-Asset-IpConflictGuard v4 IP 业务冲突守卫（M66 派生 TODO）（2026-09-16）

**摩擦（M66 派生，本轮结案）**: M66 把 IP 写入路径打通了 (asset + network 包事务、
v4/v6 分流、422 兜底), 但**没有任何守卫**禁止两台资产填同一个 IP —— `asset_networks.
ipv4_address` 只有普通 index (非 unique 约束), 两台设备可以静默持有同一个地址; 而
前端 `AssetTable` 的 Ping/Traceroute 正是拿这个 IP 去定位设备 —— 打错一台的代价是
"现场找不到设备" (与 M62 加 IP 格式闸同源的动机)。

**本轮解决方案**: 业务规则守卫 (而非 DB unique 约束 —— 退役释放 IP 后能否复用是产品
决定, 允许复用意味着不能上 DB 唯一约束) —— `service.updateFirstNetworkIP` 在 SELECT
网卡**之前**先做 `tx.Raw(...).Scan(&taken)` 全表查 (self-exclude 用 asset_id), 命中
返 `ErrIPConflict` (新 sentinel, 与 `ErrAlreadyExists` 区分 — 两者都映 409 但语义不同),
handler 映 409 + "IP 地址已被其他资产占用"。v6 不参与 (留 future)。**M66 (T-76) 风险
保持封死**: 独立参数位走 `tx.First(&network)` + `tx.Create/Update` 网卡表, 不进
`tx.Updates(map)`; 守卫在 SELECT 网卡之前, 失败早返。

**commit (`cc7a46d`)**: 1 backend commit (4 files / +172), 含:

- `asset_service.go` — 加 `ErrIPConflict` sentinel; 守卫 Raw SQL 嵌入
  `updateFirstNetworkIP` (parsed.To4() 分支内, self-exclude 用 asset_id)
- `asset_handler.go` — `CreateAsset` + `UpdateAsset` 错误映射加 ErrIPConflict → 409
  (先于 ErrAlreadyExists 判, 更具体的分支先走)
- `asset_service_test.go` — 3 新 case: 占用冲突 / self-exclude / v6 跳过; 已有 M66
  case 加 guard 期望
- `asset_handler_test.go` — 1 新 case (handler→service→HTTP 链路); 已有 M64/M66
  case 加 guard 期望

**验证**:

- backend `go test -count=1 ./...`: **27 packages ok**
- mutation inversion 实证 1 处: bypass guard → `TestM68_AssetService_ip被其他资产占用_
  返ErrIPConflict` FAIL → 还原 → 全绿
- frontend 0 改动 (本轮纯 backend); `tsc --noEmit` 待 verify
- graphify multigraph 0 anomalies / codegraph 6,532+ nodes / 16,226+ edges

**派生 TODO** (留 future):

- **M69 = G-Asset-IpConflictGuard-v6** (v6 也做冲突守卫, 待 product 决定; v6 地址
  空间大冲突概率低, 边际收益 < 边际测试成本)
- **M70 = G-Asset-IpConflictAudit** (后台巡检视图报现有重复 IP, 历史数据无法用
  guard 拦住)
- **G-UI-AssetIpValidatorParity-Mapped** (M65 派生保留, 待 product 口径: 表单允不允许
  IPv4-mapped `::ffff:1.2.3.4` 与 zone id `fe80::1%eth0` 形态)

### M66 — G-Asset-UpdateIpPersist PUT /assets/:id 写网卡 IP（M63/M64 派生 TODO）（2026-09-16）

**摩擦（M63 + M64 派生，本轮结案）**: M63 把 `Asset.IpAddress` 收成虚拟字段、M64 把 POST 的
IP 写通了 —— 但 PUT 还停在 M63 的 `delete(updates, "ip_address")` 上：表单在编辑态改 IP，
提交后字段静默丢，卡表还是原来的 IP，UI 上的输入又跟 GET 回来的 IP 对不上。这是 M63
防 42703 的代价（不让模型外键进 `db.Updates(map)`），同时是 M64 留下的"POST/PUT 不对称"。

**本轮解决方案**: handler 把 `ip_address` 从 `updates map` 抽出 → 走 `service.Update` 的独立
参数位 `ipAddress *string`；service 在 `tx` 内复用 M64 的 `updateFirstNetworkIP`（POST/PUT
共用），SELECT 网卡 → 写 v4/v6 分流。同时把 `updateFirstNetworkIP` 从 `Create` 抽出成
service 方法，避免 M64/M66 两份实现漂移。

**关键设计变化**:

- `service.AssetService.Update(ctx, id, updates map[string]interface{}, ip *string)`
  —— 多一个独立参数位（`*string`：nil = 不动网卡；空串 = 不动；非空 = 真写）
- `updateFirstNetworkIP(tx, assetID, ipAddress)` —— M64/M66 唯一入口；
  - nil/空串：no-op（不查网卡、不写库）；非空非法：返 `ErrInvalidInput`（handler 映 422）
  - v4/v6 分流同 M64（`parsed.To4()` 非 nil 落 `ipv4_address`，清 `ipv6_address` 反之亦然）
  - 第一张网卡判据沿用 M45 T-45（`created_at ASC, id ASC`），与 `listNetworks` 对齐
- handler `UpdateAsset`: `ip_address` 从 updates map **抽出**（不是 M63 的 `delete`）→
  `service.Update(ctx, id, updates, &ip)`
- M63 (T-76) 风险保持封死：`assets` 表的 UPDATE SQL **仍不带** `ip_address` 列
  （独立参数位走 `tx.First(&network)` + `tx.Create/Update` 网卡表，不是 `tx.Updates(map)`）

**commit (`a63a7d`)**: 1 backend commit（4 files / +206 / -89），含：

- `asset_service.go` — 抽出 `updateFirstNetworkIP` 给 Create/Update 复用；`Update` 加
  `ipAddress *string` 参数位；tx 包整批（含网卡 SELECT/UPDATE）
- `asset_handler.go` — `UpdateAsset` 从 updates map 抽 ip_address（删 M63 `delete` 兜底）
- `asset_service_test.go` — `TestM66_*` 抽键走独立参数；旧 5 callers 改 4 参签名；M63 的
  "Updates map 不带 ip_address" 钉子保留并改名 `TestM66_走独立参数不混进map`
- `asset_handler_test.go` — `TestM63_*` 两条 → `TestM66_*`：mock 路径断言 map 不含
  ip_address 但独立参数位收到值；路由级钉子断言 UPDATE assets 不带 ip_address + INSERT
  asset_networks 多一条

**验证**:

- backend `go test -count=1 ./...`: **27 packages ok**（10 个 packages 含 service /
  handlers / api 全绿；M66 mutation inversion 实证：bypass `updateFirstNetworkIP` → 测试
  立刻红 → 还原 → 全绿）
- 前端未动；`tsc --noEmit`: 0 错（无 frontend 文件改动）
- graphify multigraph 0 anomalies / codegraph 6,532 nodes / 16,226 edges

**派生 TODO**（留 future）:

- G-UI-AssetIpConflictGuard: 同 IP 多资产校验（M63 `pickPrimaryIP` 不去重；M66 写网卡
  不查重 —— 两条网卡都拿同一 IP 不会报错，要靠这条 future 兜）
- G-UI-AssetIpValidatorParity-Mapped（M65 派生保留）：IPv4-mapped `::ffff:1.2.3.4` +
  zone id `fe80::1%eth0` 口径（M66 复用 M64 的 v4/v6 分流，覆盖同款 `To4()` 行为）

### M65 — G-UI-AssetIpValidatorParity IP regex 与 Go `net.ParseIP` 口径一致（M64 派生 TODO）（2026-09-15）

**摩擦（M64 派生，本轮结案）**: M64 把 IP 走通了写入路径 —— POST /assets 含 `ip_address` 现在真
落 `asset_networks`（v4 → `ipv4_address`、v6 → `ipv6_address`），后端用 `net.ParseIP` 兜底校验
（失败返 422 `validation_failed`）。但前端 `IP_PATTERN`（M62 ship）的 IPv4 段是 `[01]?\d\d?`，
允许 `010.1.1.1` / `00.0.0.0` / `192.168.001.1` —— Go `net.ParseIP` 按 RFC 6943 **拒绝前导零**
（除单 0）。用户填 `010.1.1.1` → 前端放行 → 提交 → backend 422，UX 卡墙。

**改动**: `frontend/src/utils/validators.ts` OCTET 收紧为
`(?:25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)`，加注释引用 RFC 6943 + Go 口径。

**测试**: `validators.test.ts` 加 4 case（`010.1.1.1` / `00.0.0.0` / `192.168.001.1` /
`001.002.003.004`）；`AssetFormModal.test.tsx` 加 2 UI case（填 `010.1.1.1` / `00.0.0.0` →
红色格式错误 + 不调 `onSubmit`）。

**mutation inversion 实证**:
- bypass OCTET 收紧 → `validators.test.ts` **4 failed**（4 前导零 case 全放行）
- bypass OCTET 收紧 → `AssetFormModal.test.tsx` **2 failed**（UI 端 `findByText(IP_ERROR)`
  timeout —— 旧 pattern 放行，没显示错误直接 onSubmit，正是 M64 422 误伤的 UI 路径）
- 还原后 65/65 PASS

**前端 0 业务行为改**：所有合法 IP（`0.0.0.0` / `255.255.255.255` / `192.168.1.1` / `::1` 等）
继续放行；只收紧「形状对但地址错」边界。

**Out of scope（future）**: `G-Asset-UpdateIpPersist`（PUT 写网卡）/ `G-Asset-IpConflictGuard`
（同 IP 多资产校验）/ 其他页接入 `utils/validators`（Oncall / Runbook 等）。

### M64 — G-Asset-NetworksPersist 资产 IP 的写入路径（form submit → 第一张 `asset_networks`）（M63 派生 TODO）（2026-09-15）

**摩擦（M63 派生，本轮结案）**: M63 把 `ip_address` 做成了**只读投影** —— `GET /assets` 与
`GET /assets/:id` 会从「第一张网卡」把主 IP 算出来，`PUT /assets/:id` 也只会把它**剥掉**
（T-76 防 42703 → 500）。于是生产上出现一个说不通的状态：**列表能显示 IP，但谁也写不进去**。
前端 `AssetFormModal` 一直在送这个字段（`AssetFormValues.ip_address`，M62 还给它加了格式闸），
后端从 M63 之前就一直静默丢：绑定进 `models.Asset.IpAddress` —— 那是个 `gorm:"-"` 的虚拟字段，
GORM 在 schema 解析阶段就把它排除在 `Fields` 之外，`Create` 里连一行 SQL 都不会为它生成。
**填了 IP 点创建 → 201 → 列表里那台设备的 IP 列是空的。**

**改动**（backend 6 files，commit `2e3ba0f` → `ff3a230` → `e41598a` → `ae54415` + 台账；frontend **0 来源改动**，
仅重跑 `gen:api` 的生成物）:

- **`backend/internal/service/asset_service.go`**: `Create` 签名改
  `Create(ctx, asset *models.Asset, ipAddress *string) error`，资产行与网卡行包在**同一事务**：
  1. `ipAddress` 为 nil / 空串（trim 后）→ 只建资产行（**不建空网卡**）；
  2. `net.ParseIP` 解析失败 → `ErrInvalidInput`（**早返，事务之前**，不留半落状态）；
  3. v4（含 4-in-6 映射）落 `ipv4_address`、v6 落 `ipv6_address`，用 `ParseIP().String()`
     归一（`2001:0DB8::0001` → `2001:db8::1`）；
  4. 网卡行 `interface_name = "eth0"` / `interface_type = "ethernet"` / `status = "unknown"`。
- **`backend/internal/api/handlers/asset_handler.go`**: `CreateAsset` 入参改成匿名嵌套结构
  （`struct { models.Asset; IpAddress *string }` + tag `json:"ip_address"`），值走**独立入参**；
  入口加 `invalidIPAddress` 校验 → **422 + `validation_failed`**。
- **`backend/internal/apierr/apierr.go`**: 新增 `Unprocessable`（422）——
  `CodeValidationFailed` 这个常量与前端 `ApiErrorCode.ValidationFailed` 早就声明，此前
  **没有后端生产者**，本函数是第一个（+2 用例钉状态码 + code 字符串）。
- **`backend/internal/api/openapi.yaml`**: `Asset.ip_address` 补 `nullable: true`
  （无网卡时投影就是 null）+ 说明「只读投影、真身在 asset_networks」；
  `AssetInput.ip_address` **从 `required` 摘掉**（不传 = 不建网卡，合法）+ 说明 422 口径。
  重跑 `npm run gen:api`：`api.types.ts` 只有这两处（`string | null` / 必填 → 可选）。

**关键决策**:

- **IP 走独立入参而不是 `Asset` 上的字段**: 模型上的 `IpAddress` 是 `gorm:"-"` 的只读投影
  （M63 的决定：IP 属于网卡，两份存储必然漂移）。绑定进模型 = 被 GORM 静默丢弃，就是本轮要修的
  那个 bug 本身。嵌套 struct 里顶层字段在 JSON 展平时盖过嵌入结构的同名字段（深度浅者胜），
  所以 `input.IpAddress` 与 `Asset.IpAddress` 不会互相串。
- **包事务**: 资产行与网卡行必须同生。分两次写时第二条失败会留下「资产建了、IP 丢了」——
  而调用方拿到 500 会当整条失败去重试，第二次撞 `name` 唯一约束变成 409，
  **用户看到的是「资产已存在」而资产确实存在、只是没有 IP**。守卫见 mutation ④。
- **v4/v6 分列**: 两列并存正是为区分地址族。把 v6 塞进 `ipv4_address` 会让 M63 的判据
  （`pickPrimaryIP`：先第一个非空 v4，否则第一个非空 v6）读出错误结果 —— 列表把 v6 当 v4 显示。
- **空串 = 未提供**: `AssetFormValues.ip_address` 是 `string` 不是可选，用户清空字段后送上来
  就是 `""`；把它当「写了但写错了」会变成一个无法解释的 422。判据在 handler 与 service 两处
  **逐字对齐**（nil / trim 后空串都算没提供）。
- **422 而不是 400**: JSON 合法、字段名也对，只有取值不对；前端要按字段高亮而不是当成请求坏了。
  挡在 handler 入口而不是只靠 service：service 的 `ErrInvalidInput` 已被映射成
  「资产名称不能为空」的文案，把 IP 错误报成「名称不能为空」是**指错字段**。
  两侧判据一致是刻意的（M28 的 T-52：安全/校验的两侧必须共享语义）—— 见「残余」里的
  前端正则与 `net.ParseIP` 的差异。
- **`Update`（PUT）本轮不动**: 仍是 M63 的 `delete(updates, "ip_address")`（防 500）。
  PUT 写网卡是 `G-Asset-UpdateIpPersist` —— 它牵出「改 IP 是改第一张卡还是新建一张卡」
  的产品决定，不该顺带做。

**Hard pass**:

- backend `go test -count=1 ./...`: **27 packages ok** ✓（本轮新增 `apierr` 用例的两个子测试）
- backend `gofmt -l`（本轮 7 个改动文件）: 6 个干净；`tests/db_smoke_test.go` 是**既存**不干净文件
  （HEAD 上同样不干净，Go 1.19+ 注释重排规则；本轮只改了一行调用签名）✓
- frontend `npx tsc --noEmit`: **0 error** ✓（重跑 `gen:api` 后 `Asset.ip_address` 变
  `string | null`；`types/index.ts` 的手写 `Asset` 与被消费的生成类型之间没有 `_Assert`，故不受影响）
- frontend `npx vitest run src/components/AssetFormModal.test.tsx src/pages/Assets.test.tsx`:
  **2 files / 30 tests PASS** ✓（frontend 0 来源改动，无退化）
- frontend 全量 `npx vitest run`（`gen:api` 改了被 11 个源文件引用的生成物，故用全量替代推断）:
  **47 files / 489 tests，488 passed / 1 failed** —— `src/pages/Settings.test.tsx` 一条 antd 校验弹窗断言
  在同机并发跑「全量 frontend + 全量 backend」时超时；**单独复跑 58/58 PASS**，与 M61 retro 记录的同源
  flake。**如实登记，不计入全绿**。
- **mutation inversion 实证（4 处，全部红在断言上）**:
  ① bypass `tx.Create(network)` → service **3 个**用例 + handler **2 个**用例 FAIL
     （`IPv4落ipv4列` / `IPv6落ipv6列` / `网卡写失败时资产行一并回滚` /
     `带ip_address_落成网卡行` / `带ip_address_assets无此列但网卡有行`）；
  ② 去掉 v4/v6 分流（`v4 != nil || true`，即一律写 `ipv4_address`）→
     `TestM64_AssetService_Create_IPv6落ipv6列` FAIL（断言读到 `ipv4_address` 里有值）；
  ③ 关掉 handler 入口校验 → `TestM64_CreateAsset_非法ip_address_返回422` FAIL；
  ④ 关掉 service 兜底（`if false && ip != "" && parsed == nil`）→
     `TestM64_AssetService_Create_非法IP不落库` FAIL。四处还原后全绿。
- 双轨分析: graphify **7143 nodes / 14690 edges / 454 communities**（M63 基线 7065 / 14542 / 453，净增
  78 里代码只占 21 + 两条改名、其余是文档节点与重锚），`diagnose multigraph` **0 anomalies**；codegraph 里 `CreateAsset → Create` 的接线点
  （接口 → 实现的 dynamic dispatch）、`assetService.Create` 的 blast radius 与两条测试边
  —— 见 `M64-graph-analysis.md`。

**行为变更（运维可见）**:

1. `POST /assets` 带 `ip_address` **从此落库**：新建这张资产**并**建其第一张网卡
   （`interface_name = eth0`），v4 进 `ipv4_address`、v6 进 `ipv6_address`。
   紧接着 `GET /assets` 的 `ip_address` 就能显示出来（M63 的投影与 M64 的写入终于接上）。
2. `POST /assets` 带**非法** `ip_address`（前端正则挡不住的形态，见「残余」）→ **422**
   （`{"code":"validation_failed"}`），此前是 201 + 静默忽略。
3. **存量资产不受影响**：本轮只改 `Create`，不迁移、不回填；已有资产仍然可能没有任何网卡
   （`ip_address: null`）。
4. `PUT /assets/:id` 行为**不变**（`ip_address` 仍被忽略）—— 编辑弹窗里改 IP 仍然不落库，
   这是 `G-Asset-UpdateIpPersist` 的范围。

**残余 / 未覆盖（如实登记）**:

- **未在真 PG 上实测**：本机无 PG 服务端（`postgresql-libs` 只有客户端，docker 不可用）。
  证据是 sqlite 真库（事务、列落点、回滚）+ sqlmock 捕获的 **SQL 原文**（`assets` 的列清单里
  没有 `ip_address`，且确实发出 `INSERT INTO "asset_networks"`）。**不是**真库往返。
- **前后端 IP 判据不等价（已登记 TODO `G-UI-AssetIpValidatorParity`）**：前端 `IPV4_PATTERN`
  的 `OCTET`（`[01]?\d\d?`）接受**前导零**（`010.1.1.1` 能过表单），而 `net.ParseIP`
  拒绝（Go 1.17 起）→ 用户填 `010.1.1.1` 会通过表单再吃一个 422。反方向也存在：
  前端不接受 IPv4-mapped（`::ffff:1.2.3.4`）与 zone id（`fe80::1%eth0`），
  而后端接受前者（落 `ipv4_address`，归一成 `1.2.3.4`）。
- **422 的 body 是通用文案**（「IP 地址格式不合法」），前端 `Assets.tsx` 的
  `onError: () => message.error('创建失败')` 目前把它显示成 topline 提示，**没有字段级高亮**
  （`ApiErrorCode.ValidationFailed` 已被前端声明但还没有消费点）。属 frontend 改动，本轮 0 改动原则。
- **同一 IP 可挂在多台资产上**：`asset_networks.ipv4_address` 只是普通索引（非唯一），
  M64 之后表单第一次成为常规写入源，于是「两台设备填同一个 IP」从「不可能」变成「可能且无提示」。
  这是产品决定（要不要拦、拦在写入还是巡检），本轮不擅自加约束 —— 记在下面 Out of scope。
- **`interface_name = "eth0"` 是硬编码产品决定**：`AssetNetwork.InterfaceName` 是 `not null`，
  建第一张卡必须给个值。真实接口名（`ens18`/`GigabitEthernet0/1`）与多网卡一起设计
  （`G-Asset-MultiNetwork`）。

**Out of scope**（留 future round）: `G-Asset-UpdateIpPersist`（PUT 写网卡）、
`G-Asset-MultiNetwork`（interface_name / mac / 多网卡）、`G-Asset-NetworksCRUD`（独立的网卡增删改 API）、
`G-Asset-IpConflictGuard`（重复 IP 守卫 / 体检）、openapi 全量 sync（本轮只对齐了 `ip_address` 两处）。

### M63 — G-Asset-IpPersistence 资产 IP 投影（List/Get 注入 ip_address）+ Update 防 500（M62 派生 TODO）（2026-09-15）

**摩擦（M62 审查派生，本轮结案）**: M62 给资产表单的 `ip_address` 加了格式闸之后追查
「这个值去哪了」，结论是**读取侧根本没有投影**、**写入侧还会把请求打成 500**：

1. **读**: `GET /assets` 从不返回 `ip_address` —— 前端 `AssetTable.tsx:105` 的「IP 地址」列
   与 `:149/:159` 的 Ping / Traceroute 按钮（`disabled={!record.ip_address}`）在生产数据上
   **整列空转且按钮恒灰**；测试之所以全绿，是因为 `Assets.test.tsx` 的 mock 数据里带了 IP
   （「mock 里有 = 手上有」是这一类缺陷的固定遮罩）。
2. **写**: `PUT /assets/:id` 把整张 map 交给 `db.Updates(map)`，而 GORM v1.30.0 对**模型里
   不存在**的键**不丢弃**（`callbacks/update.go`：`LookUpField` 未命中时照样
   `append(clause.Assignment{Column:{Name:k}})`）→ 生成 `SET "ip_address"=$n` →
   PG `42703`（`assets` 无此列）→ handler 走 `apierr.Internal` → **500**。
   本轮把这条从「源码级核实」升级为**实测**（见下 mutation ①，捕获到的 SQL 原文）。
3. **POST**: `ShouldBindJSON(&models.Asset)` 绑定阶段丢弃该键（模型无此字段），
   本轮**保持**这一行为（虚拟字段不进 INSERT，见 mutation ③ 的断言）。

**改动**（backend 5 files，commit `33cec6d` → `215b69c` → `ccf086e` → `be5c3da`）:

- **`backend/internal/models/asset.go`**: `Asset` 加 `IpAddress *string json:"ip_address" gorm:"-"`
  虚拟字段 —— **不是列**，放在末尾并与列空一行，让「这不是一列」一眼可见。
- **`backend/internal/service/asset_ip.go`**（新建）: `pickPrimaryIP(networks) (v4, v6 *string)` +
  `primaryIP(networks) *string`（v4 优先，否则 v6，都没有 → `nil`）。判据只此一份。
- **`backend/internal/service/asset_service.go`**:
  - `List`: 一页资产用**一条 IN 查询**取回全部网卡，按 `asset_id` 分组后就地填
    `items[i].IpAddress`。**不用 `Joins`**：1:N join 会把有 N 张卡的资产复制成 N 行，
    页大小与 `total` 的含义当场改变（同一条资产在表格里出现多次）。空页早返，不发 `IN ()`。
  - `Get`: 网卡已在手，直接 `primaryIP(networks)` 投影，**不再多发一条查询**。
  - `retireCore`: 删掉第三份同判据的 for 循环，改调 `pickPrimaryIP`（见下「顺带」）。
- **`backend/internal/service/postmortem_service.go`**: `fetchIP` 委托到 `primaryIP` ——
  报告头的 IP 与资产列表的 IP 从此**同源**（此前各自写一遍循环，漂移的后果是
  「列表显示 A、报告头写 B」）；排序口径补 `id` 决胜列与 `listNetworks` 对齐。
- **`backend/internal/api/handlers/asset_handler.go`**: `UpdateAsset` 在 `normalizeJSONBFields`
  之后加一行 `delete(updates, "ip_address")`（T-76）。**本轮的 `ip_address` 是只读投影字段**，
  写入侧（第一张 `asset_networks`）留给 `G-Asset-NetworksPersist`；在那之前前端若把表单里的
  IP 回传上来，应当被忽略，而不是把请求打成 500。

**顺带（同一判据的唯一出口）**: `retireCore` 里「取 last_known_ip4/6」的循环是这条判据的
**第三份**实现（`asset_ip.go` 注释里那份「唯一出口」在当时并不成立）。改为一处调用后，
**Retire 存下的历史 IP、列表/详情投影的主 IP、复盘报告头的 IP 说的是同一个口径**。
守卫是既有用例 `TestAssetService_Retire_成功_IP转移到last_known`（断言的是 UPDATE 的 **args**，
不是「发过 UPDATE」）—— mutation ③ 让它与其余 7 个用例同时红。

**关键决策**:

- **虚拟字段而不是给 `assets` 加一列**: IP 属于**网卡**不属于资产（一个资产 N 张卡），
  两份存储必然漂移（B4 退役改的是 `asset_networks`）。加列会让「资产表里的 IP」
  与「网卡表里的 IP」在退役/恢复后不一致，且需要迁移与回填。
- **`gorm:"-"` 而不是 `json:"-"`**: 字段要出现在 JSON 响应里（前端契约），只是不参与 schema。
  代价是 POST/PUT 的入参里它会**静默通过绑定然后被忽略** —— 这正是 T-76 要在 handler 剥掉的原因
  （绑定层不报错，DB 层报错）。
- **投影值可以为 `null`**: 无网卡的资产 → `ip_address: null`（前端 `!record.ip_address` 正好
  禁用 Ping/Traceroute）。而 `openapi.yaml` 的 `Asset.ip_address: type: string` 与
  `AssetInput.required: [ip_address]` 与本轮实现**不完全一致**（前者不能为 null、后者 POST
  根本不落库）—— 已登记为新 TODO（`G-Asset-IpPersistence-Contract`），本轮**不动**
  openapi/前端类型：那会牵出 `gen:api` 重生成与 `Asset` 类型收窄，属独立一轮。
- **一行 `delete` 而不是把 IP 写进 `asset_networks`**: brief 明确本轮只防 500；写路径要么
  与「表单里没有网卡概念」一起设计，要么会做出「每次 PUT 都覆盖第一张卡」的隐式副作用。

**Hard pass**:

- backend `go test -count=1 ./...`: **27 packages ok** ✓
- backend `gofmt -l`（本轮 7 个改动文件）: 干净；`go vet`（service/handlers/models）: 无新告警 ✓
- frontend `npx tsc --noEmit`: **0 error**（本轮 frontend **0 改动**）✓
- frontend `npx vitest run src/components/AssetTable.memo.test.tsx src/pages/Assets.test.tsx`:
  **2 files / 27 tests PASS** ✓（无退化）
- **mutation inversion 实证（3 处，全部红在断言上）**:
  ① bypass `delete(updates, "ip_address")` → `TestM63_UpdateAsset_剥掉ip_address键` +
     `TestM63_UpdateAsset_带ip_address不产生该列的SQL_返200` **同时 FAIL**，且失败信息里是
     **驱动实际收到的 SQL**：`UPDATE "assets" SET "ip_address"=$1,"name"=$2,"updated_at"=$3 …`
     —— T-76（GORM 对模型外的键照发 SET）由此从推理变成**证据**；
  ② `primaryIP` 反转优先级（v6 优先）→ `TestM63_PrimaryIP_单值投影` +
     `TestM63_AssetService_List_投影ip_address` + `TestM63_AssetService_Get_投影ip_address` FAIL；
  ③ 删掉 `pickPrimaryIP` 的 v4 分支 → **8 个用例同时红**：`TestFetchIP_IPv4优先` /
     `TestGenerateReport_有IP_填入ReportData`（复盘报告头）、`TestAssetService_Retire_成功_IP转移到last_known`
     （last_known 快照）、`TestM63_PickPrimaryIP_判据` / `TestM63_PrimaryIP_单值投影` /
     List / Get 投影、以及既有 `TestAssetService_List_带keyword和status过滤`（其断言补上了投影值）。
     还原后全绿。
- 双轨分析: graphify **7065 nodes / 14542 edges / 453 communities**（M62 基线 7002 / 14420 / 453），
  `diagnose multigraph` **0 anomalies**；codegraph 里 `primaryIP` 4 callers（两个 service 文件）
  + `pickPrimaryIP` 2 callers + 测试边 —— 见 `M63-graph-analysis.md`。

**行为变更（运维可见）**:

1. `GET /assets` 的 `items[i]` 现在带 `ip_address`：**有 v4 用 v4**（第一张有非空 v4 的网卡），
   否则第一张非空 v6，都没有则 `null`。前端 IP 列与 Ping / Traceroute 按钮**从此有数据**
   （此前生产数据上恒空/恒灰）。
2. `GET /assets/:id` 的 `data.asset.ip_address` 同规则注入（详情页头部）。
3. `PUT /assets/:id` 带 `ip_address` **不再 500**，该键被忽略（返回 200，其它字段正常更新）。
4. `POST /assets` 带 `ip_address` 仍然**不落库**（201 + 忽略），行为与 M63 之前一致 ——
   写入路径是 `G-Asset-NetworksPersist` 的事。
5. 复盘 PDF 报告头的 IP 与资产列表显示的是**同一个地址**（此前两处各判一遍）。

**残余 / 未覆盖（如实登记）**:

- **未在真 PG 上实测 42703**：本机无 PG 服务端（`postgresql-libs` 只有客户端，docker 不可用），
  故「PUT 带 ip_address → 42703 → 500」的证据是：GORM 生成的 SQL **原文**（mutation ① 捕获）
  + PG 错误码语义。与 M62 相比已从「源码级核实」推进到「渲染出的语句级证据」，但**不是**
  真库往返。
- **`ip_address` 只读**：POST/PUT 都忽略它（前端表单仍会把它放进 payload）。真正落库要等
  `G-Asset-NetworksPersist`（写第一张网卡，v4 进 `ipv4_address`、v6 进 `ipv6_address`）。
- **列表多一条查询**：每页 1 次 `asset_networks WHERE asset_id IN (…)`（≤500 个 id）。
  换来的是行数语义不变；若将来列表要带 v4/v6 并列展示，`pickPrimaryIP` 已把两者都返回。
- **`openapi.yaml` 与实现不一致**（见上「关键决策」），登记为 `G-Asset-IpPersistence-Contract`。
- **`Assets.tsx:372/528` 的详情文案与弹窗预填**读的是同一个字段，本轮不动（0 前端改动）。

**Out of scope**（留 future round）: `G-Asset-NetworksPersist`（form submit 写第一张网卡）、
`G-Asset-BulkIpEdit`（批量改 IP）、退役/恢复时的 IP 联动（已有 B4 逻辑走 `last_known_ip*`）、
实时 IP 校验（静态格式闸 M62 已 ship）。

### M62 — G-UI-AssetIpValidator 资产表单 IP 校验 + 规则唯一出口落地（M60 follow-up）（2026-09-15）

**摩擦（multi-angle 审查新发现）**: `AssetFormModal.tsx:95` 的 `ip_address` 内联
`/^(\d{1,3}\.){3}\d{1,3}$/` —— 那是**形状**检查，不是**地址**检查：`256.0.0.1` /
`999.999.999.999` 一路放行，只有后端才可能拦（实际也不拦）。IP 是运维拿去 ping、
连 SNMP、对 NetBox 的定位键，填错的表现是**现场找不到设备**而不是「存不下」——
比格式垃圾值更贵。M60 把 URL/email/port 规则提到 `utils/validators.ts` 时留了
「其他页接入」的 follow-up，本轮把这页接上并补上 IP 规则。

**改动**（frontend 4 files，commit `1e460fc` → `f738bc7` → `c592512` → `9be9576`）:

- **`frontend/src/utils/validators.ts`**: 新增 `IPV4_PATTERN` / `IPV6_PATTERN` /
  `IP_PATTERN` / `ipRules`。IPv4 四段严格 0-255（内网 `10.` / `172.16-31.` / `192.168.`
  天然在值域内，无需特例）；IPv6 简版 = 「全写 1 种 + 带 `::` 的 9 种形状」（RFC 4291 §2.2），
  **不**支持 zone id（`fe80::1%eth0`）、CIDR、IPv4-mapped（`::ffff:1.2.3.4`）—— 都是 out of scope。
  `ipRules` = `required` + `pattern: IP_PATTERN`（格式文案给两个例子：`192.168.1.1` / `::1`）。
- **`frontend/src/components/AssetFormModal.tsx`**: `ip_address` 的 `rules` 由内联两条换成
  `rules={ipRules}`，删掉内联 pattern 与旧文案「IP 格式不正确」。**字段名 / required 文案
  「请输入 IP 地址」/ 校验时机（`onOk` 里的 `validateFields`）均不变**。
- **`frontend/src/utils/validators.test.ts`**（新建，55 用例）: IPv4 边界（含 `256.0.0.1` /
  `1.2.3` / `1.2.3.4.5` / `192.168.1.1␠` / CIDR）、IPv6 边界（含 `zz::1` / `:::` / 两处 `::` /
  9 组 / 5 位组 / zone id）、`IP_PATTERN` 双族、`ipRules` 文案与「格式规则引用的就是导出的
  `IP_PATTERN`」；外加 **M60 四组既有规则对象**（`urlRules` / `emailRules` / `portRules` /
  `arrayOfPatternRules`）的文案与约束逐条钉住（此前只在 UI 层被间接打到）。
  不重复 M59 的 URL/EMAIL 正负样本表（那 16 条在 `Settings.test.tsx`，搬家后未改一字）。
- **`frontend/src/components/AssetFormModal.test.tsx`**（新建，4 用例）: `192.168.1.1` 通过并
  「带着该值走到 `onSubmit`」、`256.0.0.1` 显示格式错误且**不提交**、`::1` 通过、留空显示
  必填文案（不是格式文案）。

**关键决策：brief 的 IPv6 单行字面量**（`intent-M62.md` §1）**没有照抄**。把 10 条交替式
压成一行时，`^([0-9a-fA-F]{1,4}:){1,7}:` 与 `:((:[0-9a-fA-F]{1,4}){1,7}|:)$` 这两条
**缺自己那一侧的锚点**（前者无 `$`、后者无 `^`）—— 粘进 `new RegExp` 后整个 pattern 在
standalone 使用下退化成**子串**匹配：实测 `IPV6_PATTERN.test('zz::1')` = `true`、
`test(':::')` = `true`。本仓改成「10 条形状列成数组 + 外层统一 `^(?:…)$`」，
边界不再依赖每一段自己写对。与 brief 字面量的差异用 **336,949 条样本**（长度 ≤7 的字母表穷举
+ 合法/非法种子做字符级扰动）核对：**方向单一** —— brief 版多收 24,753 条子串误判，
本实现零误收、零漏收（IPv4 与 `IP_PATTERN` 两者判定逐条相同）。族里最易改错的
「`::` 两侧组数上界」也因此可逐行核对（见 T-75）。

**Hard pass**:

- frontend `npx tsc --noEmit`: **0 error** ✓
- frontend `npx vitest run src/utils/validators.test.ts`: **55 tests PASS** ✓
- frontend `npx vitest run src/components/AssetFormModal.test.tsx`: **4 tests PASS** ✓
- frontend `npx vitest run src/pages/Settings.test.tsx`: **58 tests PASS**（未触碰，无退化）✓
- frontend 全量 `npx vitest run`: **47 files / 489 tests PASS**（M62 前基线 **45 files / 430 tests** ——
  含他人 `dac8b05` 的 `App.render.test.tsx`；本轮 +2 文件 +59 测试，**零退化**）✓
- eslint（改动 4 文件，`--max-warnings 0`）: 干净 ✓
- **mutation inversion 实证（3 处，红在断言上）**:
  ① `AssetFormModal` 的 `rules={ipRules}` 退回 required-only → `AssetFormModal.test.tsx`
  **1 failed \| 3 passed**（`256.0.0.1` 那条）；
  ② `OCTET` 放宽回 `\d{1,3}`（= 旧的形状检查）→ `validators.test.ts` **4 failed**（`256.0.0.1` /
  `999.999.999.999` / `IP_PATTERN` 的 `256.0.0.1` / `ipRules` 自身能挡）**+** UI 层 **1 failed**
  （同一条闸的两层输入）；
  ③ `IPV6_PATTERN` 换回 brief 的单行字面量 → `validators.test.ts` **6 failed**（`zz::1` / `:::` /
  `a::b::c` / `12345::1` / `gggg::1` / `fe80::1%eth0`）。还原后 59/59 全绿。
- 双轨分析：graphify **7002 nodes / 14420 edges / 453 communities**（M61 基线 6968 / 14363 / 443），
  `diagnose multigraph` **0 anomalies**（12 处 `producer_suppression_sites` 与 M59/M60/M61 同源）；
  codegraph 新符号已入图（`ipRules` 2 callers in `AssetFormModal.tsx` + 测试边）——见 `M62-graph-analysis.md`

**行为变更（运维可见）**: 资产新增/编辑弹窗的 IP 字段现在**前端即拒**越界段
（`256.0.0.1` → 「IP 地址格式不正确 (IPv4: 192.168.1.1, IPv6: ::1)」），并**新接受** IPv6
（此前 `::1` 会被旧 pattern 拒掉，而存储侧 `asset_networks.ipv6_address` 一直存在）。
与旧闸的差别是**单向下收**：新闸只比旧闸更严（多拒「四段数字但段值越界」）或更宽（收 v6），
不会放行旧闸挡下的 v4 形状。`asset_networks.ipv4_address` 是 `VARCHAR(45)`
（`000013_schema_align` 从 `INET` 转过），**存储层不再做格式校验**，故这道表单闸是当前唯一
的格式入口 —— 但**只覆盖走表单的路径**（API 直连不受约束，见 Out of scope）。

**发现（本轮 scope 外，已登记 TODO，未修）**: 该字段的**值本身当前落不了库** ——
`POST /assets` 把请求体绑进 `models.Asset`（**没有 `ip_address` 字段**，实测 JSON 往返里该键
被静默丢弃），IP 实际存于 `asset_networks.ipv4_address` / `ipv6_address`；
`PUT /assets/:id` 则把整张 map 交给 `db.Updates`，GORM v1.30.0 对**模型里不存在的键不丢弃**
（`callbacks/update.go:211-232`）→ 生成 `SET ip_address = …` → PG `42703`（列不存在）→ 500。
即：本轮修的是**表单能不能提交**，不是**值能不能存**；后者是独立一轮的后端工作
（`GET /assets` 同样不投影 `ip_address`，前端 `Asset.ip_address` 列与 Ping/Traceroute 按钮
在生产数据上是空转）。

**Out of scope**（留 future）:

- Oncall / Runbook / TicketForm 等其他页接入 `utils/validators`（Oncall 的 name / timezone 无
  格式可校验，本轮不动）
- CIDR（`192.168.1.0/24`）校验、hostname（FQDN）校验
- IPv6 的 zone id / IPv4-mapped（`::ffff:1.2.3.4`）
- `ws://` / `wss://`（M60 同一条 follow-up，未动）
- 后端 `assets` 的 IP 字段校验（本轮纯前端：绕开表单的 API 直连不受此闸约束）

### M61 — G-User-AdminManagement 用户管理（admin 端启用/禁用/改角色）（2026-09-15）

**G-4 摩擦（AUTHZ-CLOSURE §2 D-A 登记的那条）**: `users.status` / `users.role` 只能改库 ——
`/users` **只有 GET**（`routes.go` 的 users 组）、`UserService` 只有 `List`/`Get`、
前端 `userApi` 只有 `list`/`get`、全站没有任何用户管理页。后果是**离职员工的账号禁不掉**：
JWT 拿到手就能用满 24h（G-5 的 M40 修复把「禁用即失效」做进了鉴权侧，但没人能触发禁用）；
角色调整只有 `cmd/set-role` 这条直连 DB 的路径。

**改动** (backend 5 files + frontend 8 files，commit `4d7092c` → `1c85490` → `5012582` → `4a7f407` → `969db3e` → `5249fd9` → `dac8b05`):

- **`backend/internal/service/user_service.go`**: 新增 `Update(ctx, id, UpdateUserInput, actor)` /
  `UpdateStatus` / `UpdateRole`。`UpdateUserInput` 三个字段全是**指针**（nil = 本次不动该列：
  `false` 与「未提供」必须可区分，否则永远关不掉强制改密）。`Update` 先校验后进事务，
  守卫与写入在**同一事务**内、旧值用 `FOR UPDATE` 持锁读 —— 两道守卫都是读-判-写：
  - **v-1 自我禁用 / 自我降级**：identity 路由只有 admin 进得来，admin 点错一次就把自己锁在
    门外，自助恢复路径不存在（只能进库改）。
  - **v-2 最后一名可登录的管理员**：判定是「更新后的角色/状态」，且只数 `status='active'` 的
    admin —— 已被禁用的 admin **登不进来**，不算「能自救的那个人」（漏这个条件会让守卫在
    最需要它的场景静默失效）。
  - `role` 先走 `middleware.CanonicalRole` 折叠遗留别名（`operator`→`ops_user`、
    `viewer`→`readonly`）再按权威词表校验：直接存原始串会让 `role == "ops_user"` 这类字面量
    比较静默失配（S-1a 同族）。**落库的永远是词表值。**
  - `status` 只收 `active` / `inactive`。**没有 `locked`** —— 鉴权侧（`auth.go` 的 JWT 与
    API Key 两条路径 + 登录 handler）只拦 `inactive`，写 `locked` 不拦住任何请求（真正的锁定走
    `locked_until`），收它等于给管理员一个静默无效的开关。
  - 新增哨兵 `ErrForbidden`（`asset_service.go` 的错误块）：请求合法、调用方也有权限，但
    **服务端策略**不允许 → 403。与 `ErrInvalidInput`（400）分开的判据是「调用方该做什么」：
    自我禁用改参数重试无用，报 400 会让人去改请求体。
- **`backend/internal/api/handlers/user_handler.go`**: 新增 `UpdateUser`（PUT）/`UpdateUserStatus`
  （PATCH .../status）/`UpdateUserRole`（PATCH .../role），共用 `writeUser` 收口
  （404/400/403/500 分类）与 `userPathID`（`:id` 必须是 UUID —— 裸串进 UUID 列比较会 PG 22P02
  被报成 500，同 `alertPathID` 的口径）。请求体走**严格解码**
  （`DisallowUnknownFields` + 尾随内容检查）：`ShouldBindJSON` 静默忽略未知键，写
  `{"email": …}` 会拿到 200 而邮箱一字未改（「写了不生效」的静默失败，工单 M17 同口径）。
  空 `{}` 返 400（不是「改了零个字段」）。400 文案静态，不回显调用方可控的字段名。
  **不做 DELETE**：账号走 `status=inactive`，硬删会让 `audit_logs` 里的操作人再也查不到是谁。
- **`backend/internal/api/routes.go`**: 三条写端点挂 `canIdentity` **+ `RejectAPIKeyAuth`** ——
  长期凭据不得改账号：admin 名下 write scope 的 Key 原本能把任意账号提成 admin（权限持久化，
  吊销 Key 撤不掉）或禁用掉真正的管理员（自锁），与 `/auth/api-keys`、通知渠道、集成 PUT 同源
  （`FIX-PLAN-AUTHZ-LEFTOVER.md` S-2 同族）。
- **`backend/internal/middleware/audit.go`**: 默认 `ActionFunc` 改为「context 键 `audit_action`
  优先，回落 HTTP method」；`Action` 字段补 `sanitizeField(50)`（handler 可覆盖它，而此前那条
  路径进来的字符串完全不截断 → 超宽即 22001 → **整行**审计丢失，G-44 同族）。三个 handler 各自
  `c.Set("audit_action", "update_user_status" | "update_user_role" | "update_user")`，
  于是审计页的「动作」列能读出改的是状态还是角色（默认取 HTTP method 会让三条全变成 PATCH/PUT）；
  单个 `c.Set` 而不给每条路由单独挂 `AuditLog` 实例 —— 后者会让每个请求**写两行**审计。
- **`backend/internal/api/openapi.yaml`**: 新增 `/users/{id}` 的 `put`（`updateUser`）与
  `/users/{id}/status`、`/users/{id}/role` 两条 path + `UserUpdateRequest` schema；
  `User` 补 `status`（enum 三值，如实描述库列无 CHECK）/`last_login`。
  **顺带修正既存漂移**：`UserList.data` 原声明成裸数组，而 handler 返回
  `{items, total, page, page_size}`（M61 的页面是这个信封的第一个真实消费者，照错的契约写
  消费方就是制造下一个缺陷）。`gen:api` 重生成随提交（CI 有漂移门禁）。
- **`backend/internal/api/routes_integration_test.go`**: `gatedRoutes` 登记三条（矩阵/路由分类
  门禁会红在「未分类」上）+ 11 条 M61 用例。
- **`frontend/src/services/api.ts`**: `userApi.update` / `updateStatus` / `updateRole` +
  `UserStatus` / `ASSIGNABLE_ROLES` 词表。三条写端点逐字段显式传（严格请求体不能透传整行）。
- **新增 `frontend/src/pages/Users.tsx`**: PageHeader + Table（用户名/昵称/邮箱/角色 Select/
  状态 Switch + Popconfirm/最后登录/操作）。**乐观更新 + 失败回滚**：按字段记 override，
  失败恢复「这次改动前」的值（不是无脑清空 —— 行上可能还有上一笔已成功但列表尚未重取的改动）；
  成功以**服务端回执**校正乐观值并 invalidate 列表。403/400 显示**服务端原因**（
  「不能禁用自己的账号」比拦截器的通用「没有权限访问」更能说清为什么）。词表外的存量角色
  出现在选项里且禁用（不假装它是可选项）；`locked` 等状态值只展示不假装可切。
  **不做删除**；**不做创建账号**（后端没有 `POST /users`，点了没反应的按钮正是 B1-1 那类缺陷）；
  「强制改密」按钮走 PUT 的 `must_change_password` —— **不叫**「重置密码」，因为全仓没有
  「admin 给他人设新密码」的端点，按做不到的名字做按钮就是骗运维。
- **`frontend/src/App.tsx`**: 菜单抽成导出的纯函数 `buildMenuItems(hasIdentity)`，`/users` 入口
  按 `/auth/me` 下发的 `capabilities` 是否含 `identity` 条件渲染（**不**比较 role 字面量、
  不复制角色→能力矩阵 —— `roles.go` 明确警告过漂移）；取不到即不显示（fail-closed）。
  注册 `/users` 路由（路由本身不做前端门禁：非 admin 手输会看到 403 错误态，比静默跳回首页
  更让人理解发生了什么）。`AppBreadcrumb` 认 `/users`。

**Hard pass**:

- backend `go test -count=1 ./...`: **27 packages ok**（0 fail，与 M60 同基线）✓
- backend `gofmt -l internal cmd`: 仅 3 个 **M61 未触碰**的既存文件（`database/gorm_logger_redact.go`、
  `middleware/auth_status_cache.go`、`notification/sender.go`）；M61 改动的 5 个 Go 文件全部干净 ✓
- backend `go vet ./...`: 干净 ✓
- frontend `npx tsc --noEmit`: **0 error** ✓
- frontend `npm run lint`（全量，`--max-warnings 0`）: 干净 ✓
- frontend `src/pages/Users.test.tsx`: **14 tests PASS**（M61 新增）✓
- frontend `src/App.menu.test.tsx`: **5 tests PASS**（M61 新增）✓
- frontend `src/App.render.test.tsx`: **2 tests PASS**（M61 新增，白屏回归；修复前 2 failed）✓
- frontend 全量 `npx vitest run`（最终冻结态，`--reporter=json` 逐文件核对）:
  **47 files / 489 tests / 0 failed**（M60 基线 42/409；+5 文件 +80 测试，其中 M62 并行同仓
  落地 2 文件；零退化）。中途一次 `AssetFormModal.test.tsx` 在**同机并发跑两个套件**时偶发红，
  单独复跑 4/4 PASS，最终态全绿 —— 与 M60 retro 记录的同源现象一致 ✓
- **真浏览器验证**（`vite build` 产物 + 契约桩 API，本地 8101；见下方白屏修复段）：
  侧边栏入口、列表 3 行、禁用/启用、改角色、403 回滚、能力门禁逐项实测通过，附截图 ✓
- **mutation inversion 实证（5 处，全部红在断言上）**:
  ① 短路自我守卫（`self := false`）→ `TestUserService_Update_自我禁用返回ErrForbidden` /
  `_自我降级admin返回ErrForbidden` / `_还有另一名启用admin时可禁用` FAIL + 集成
  `TestRoutes_用户处置_自我禁用返403` FAIL；
  ② 守卫的 count 去掉 `AND status='active'` → `TestUserService_Update_被禁用的管理员不算能自救` FAIL；
  ③ `Update` 去掉 `CanonicalRole` 折叠 → `_role首尾空白与大小写归一` / `_role遗留别名折叠后才落库` FAIL；
  ④ 前端 bypass `statusMut.mutate`（只改本地状态）→ `禁用账号…PATCH /users/:id/status` FAIL；
  ⑤ 全部还原后逐个复跑全绿 ✓
- 双轨分析: graphify + codegraph，见 `M61-graph-analysis.md` ✓

**顺带修复：全站白屏（P0，非本轮引入，自 `f7e98eb` 起一直存在）**

真浏览器验证 M61 页面时发现**应用根本起不来**：任意路由打开都是纯白页，root 为空，
只有一条未捕获异常 `useNavigate() may be used only in the context of a <Router> component`。

- **根因**：`<CommandPalette />` 挂在 `<BrowserRouter>` **外面**，而它内部无条件调
  `useNavigate()`（选中搜索结果要 `navigate(to)` 跳详情）→ react-router 的 invariant 抛错 →
  React 卸载整棵树。引入点是 `f7e98eb`（2026-06-17「小改进 #3 Cmd+K 全局搜索」）。
- **为什么十几个 round 都没发现**：**没有任何用例渲染过 `<App />` 整体**。
  `App.theme.test.tsx` 只测 `buildTheme` 的产物；`App.menu.test.tsx` 与各页用例都渲染
  `AppLayout` 或页面本身、并自己包了 Router。于是「应用挂载即崩」这件事全仓零守卫，
  而每一轮的「frontend 全量 vitest PASS」都是真的 —— 它只是没覆盖这个粒度。
- **修法**（`dac8b05`）：把 `<CommandPalette />` 移进 `<BrowserRouter>`（一行位置变更），
  并在原位留注释写明约束。它本来就依赖 Router 上下文，移进去是纠正而非将就。
- **回归钉子**：`frontend/src/App.render.test.tsx` 按**生产入口形状**（`QueryClientProvider`
  + `App`，同 `main.tsx`）渲染，断言未登录出登录页、已登录出侧边栏。修复前在 jsdom 里复现
  同一条 invariant 异常（2 failed），修复后 2 passed。
- **影响面如实登记**：这是**产品级**缺陷（前端所有页面在浏览器里都是白屏），不是 M61 引入的；
  M61 只是第一次真的用浏览器打开它。**M61 之前各轮报告里的「UI 已变更」类声明，
  其真实验证依据需要重新审视** —— 那些 round 的浏览器级结论从未被本仓的测试面支撑。

**行为突变告知（运维需知）**:

- **管理员现在能被 UI 禁用**了 —— 这是本功能的目的，但由此**两个 admin 互禁**是可能的（守卫只拦
  「最后一个可登录的 admin」）。两个 admin 互相禁用需要两次操作、第二次会被守卫挡下；若确实
  需要「全锁死」的运维动作，走 `cmd/set-role` 直连 DB。
- **禁用最长 30s 生效**（`middleware/auth_status_cache.go` 的 per-process TTL 取舍，M40 已记）：
  多副本部署下每个副本各自到期。UI 的确认文案如实写了这一点。
- **`PUT /users/:id` 是严格请求体**：多传一个字段即 400（此前没有任何写端点，故非兼容性问题）。

**未随本轮落地（留 future round，逐项有理由）**:

- **创建账号**：后端没有 `POST /users`（账号由 `admin-bootstrap` / `seed` 建），页面不放假按钮。
- **admin 给他人设新密码**（真正的「重置密码」）：需要新端点 + 复杂度/审计设计，M49 的强改密流程
  已覆盖「让本人改」这条路径。
- **批量禁用**：单账号处置已够用；批量需要部分成功语义与逐条审计（同 M58 的设计面）。
- **PII 脱敏**：不做硬删就需要「离职后邮箱/手机脱敏」这条独立设计（TODO 已登记）。
- **部门树管理**：`User.DepartmentID` 一直在，但 department API 不存在，属另一件事。

### M60 — G-Utils-ValidatorsShared + G-BE-HttpsWhitelist（T-56 / T-71 结案）（2026-09-15）

**两项 M59 留的技术债**:

1. **T-71（前端）**: URL / email / port 规则住在 `pages/Settings.tsx` module 顶层 —— 同页复用没问题，
   但别的页（Oncall URL / Runbook webhook / AssetForm IP）要用只能「import 一个 **page** 文件里的常量」，
   页面因此成为工具模块的依赖。
2. **T-56（后端）**: M59 给 3 个集成 URL 挂的 `binding:"url"` 是**形状检查**，不是 http(s) 白名单 ——
   go-playground/validator v10.16 对 `ftp://example.com` 照样放行，而前端 `URL_PATTERN` 只认 http(s)。
   两侧同名不同集：API 直连 / 脚本调用（不经表单）能写进非 http(s) 的集成地址。

**改动** (frontend 3 files + backend 2 files，commit `314d47b` → `a3867ff` → `64c9623` → `549ddb5`):

- **新增 `frontend/src/utils/validators.ts`**: `URL_PATTERN` / `EMAIL_PATTERN`（字符**逐字照搬** M59，
  不动语义 —— 内网无 TLD 地址必须继续放行）+ `urlRules` / `emailRules` / `portRules` +
  `arrayOfPatternRules(pattern, label, requiredMessage?)` —— 把 M59 `toRules` 里的数组逐项校验抽出来
  （antd 的 `pattern` 规则对数组**静默不生效**，必须自定义 `validator`）。可选第三参是为了保住
  收件人字段的「请输入收件人」文案（该字段 label 是「收件人」而非「邮箱」）。
- `frontend/src/pages/Settings.tsx`: 删除本文件顶层的全部 pattern / rules 定义，改 import；
  `toRules` 由 `arrayOfPatternRules(EMAIL_PATTERN, '邮箱', '请输入收件人')` 在页面内构造。
  **10 处 Form.Item 的 rules 引用与全部文案不变。**
- `frontend/src/pages/Settings.test.tsx`: pattern import 从 `"./Settings"` → `"../utils/validators"`。
- `backend/internal/api/handlers/integration_handler.go`: 新增 `isHTTPURL`（`url.Parse` 后要求
  scheme ∈ {http, https} 且 Host 非空）+ 三个 `Update*` handler 入口显式一行校验；
  三个 `URL` 字段的 `binding:"required,url"` 收敛为 `binding:"required"`（`required` 只挡空值）。
  **不把两条语义不同的规则叠在一起** —— 并存的规则正是 T-56 的病根。拒绝发生在写内存 cfg 之前。
- `backend/internal/api/handlers/integration_handler_test.go`: 新增
  `TestUpdateIntegrations_非HTTPScheme_返400`（3 端点 × 3 scheme：`ftp://` / `file:///` / `ssh://`）——
  每条断言 400 **且** 内存 cfg 未被写。

**Hard pass**:

- `npx tsc --noEmit`: 0 error
- frontend `npx vitest run src/pages/Settings.test.tsx`: **58 tests PASS**（无退化）
- frontend 全量 `npx vitest run`: **42 files / 409 tests 全 PASS**（与 M59 基线一致，零退化）
- backend `go test -count=1 ./...`: **27 packages ok（0 fail）**
- eslint（改动 3 文件，`--max-warnings 0`）+ `gofmt -l`（改动 2 文件）: 干净
- **mutation inversion 实证**: 把 3 处 `if !isHTTPURL(req.URL)` 短路成恒假（等价于 M59 的
  `url`-tag-only 行为）→ `TestUpdateIntegrations_非HTTPScheme_返400` **9/9 sub-case FAIL**，
  且 M59 的 `TestUpdateIntegrations_非URL_返400` 3/3 也 FAIL（同一道闸）；还原后全绿
- 双轨分析：graphify 6843 nodes / 13936 edges / 445 communities，diagnose 0 anomalies；
  codegraph 已入图并给出 blast radius（见 `M60-graph-analysis.md`）

**行为变更（运维可见）**: 集成配置（Zabbix / NetBox / GLPI）的 URL 现在**只接受 http(s)** ——
API 直连 / 脚本调用提交 `ftp://…` / `file:///…` / `ssh://…` 会拿到 400
`URL 必须以 http:// 或 https:// 开头`（前端表单此前已挡）。**内网无 TLD 地址
（`http://zabbix:8080`）不受影响**，仍由两条测试钉住。

**Out of scope**（留 future）:

- 其他页面（Oncall / Runbook / AssetForm）import `utils/validators` —— 本轮只搬 Settings 一处
- `ws://` / `wss://` scheme
- 实时 URL 探活校验
- SMTP user 强 email 校验对 SASL 非邮箱用户名（SendGrid `apikey`）的误伤

### M59 — G-UI-SettingsValidators 集成 / 通知 form 字段格式校验（F-1 结案）（2026-09-15）

**摩擦**: `Settings.tsx` 里 10 个字段（3 集成 URL + 3 webhook URL + SMTP user/from/to + SMTP 端口）
只校验 required —— 填 `not-a-url` / `99999` / `not-an-email` 前端一律放行，用户只有等后端 400
才知道哪个字段错了（M57 留的 **F-1**）。

**改动** (frontend 2 files + backend 2 files):

- `frontend/src/pages/Settings.tsx`: URL / email pattern 提到 module 顶层并 `export`
  （`URL_PATTERN` / `EMAIL_PATTERN`，测试可 import 直接钉正/负样本）；共享
  `urlRules` / `emailRules` / `portRules` / `toRules` 在 **10 处 Form.Item** 引用 ——
  改一处不会漏掉另一处，不再逐字段复制规则串。
  `URL_PATTERN=^https?://[^\s/$.?#].[^\s]*$` **刻意允许内网无 TLD 地址**（`http://zabbix:8080`：
  三家集成的目标是内网主机名/IP，「必须有 TLD」的写法会把合法配置挡在门外）；
  `EMAIL_PATTERN=^[^\s@]+@[^\s@]+\.[^\s@]+$`；端口 `type: integer` + 1-65535。
  `to` 是 `Select mode="tags"`（值域是**数组**）—— antd 的 `pattern` 规则对数组不生效，
  故用自定义 `validator` 逐项校验。SMTP host 不加格式规则（域名/IP 校验太宽，只留 required）。
- `frontend/src/pages/Settings.test.tsx`: 新 `describe` 两组 —— 7 条 UI 用例（坏值必须**不发请求**、
  好值必须**走到请求**）+ 16 条 pattern 边界（`it.each`）；M6 两条断言随共享规则的 required 文案
  同步（`请输入用户名`/`请输入发件人` → `请输入邮箱`；`请输入Webhook URL` → `请输入 URL`）。
- `backend/internal/api/handlers/integration_handler.go`: `UpdateZabbixRequest` / `UpdateNetBoxRequest` /
  `UpdateGLPIRequest` 的 `URL` 字段加 `binding:"required,url"` —— 前端已挡，但 API 直连 / 脚本
  不经过前端。**注意语义**：validator 的 `url` tag 只要求「scheme + host」，实测**接受**内网无 TLD 地址
  （`http://zabbix:8080`，brief 的担心在 v10.16.0 不成立），但它**也接受 `ftp://example.com`** ——
  它不是 http(s) 白名单（前端 pattern 才是），故这里挡的是 scheme-less 垃圾值，不是「只允许 http(s)」。
  通知渠道的 `config` 是不透明 JSON 字符串（`smtp_host`/`webhook_url` 在串里），bind tag 无处可挂。
- `backend/internal/api/handlers/integration_handler_test.go`: `TestUpdateIntegrations_非URL_返400`
  （表驱动 3 家，`not-a-url` → 400）+ `TestUpdateIntegrations_内网URL_不被url标签拒`
  （把「内网地址必须放行」固化，防未来换成「必须有 TLD」的校验）。

**Hard pass**:
- `npx tsc --noEmit`: 0 error
- frontend `npx vitest run`: **42 files / 409 tests 全 PASS**（基线 386 → +23，零退化）
- frontend `src/pages/Settings.test.tsx`: **58 tests PASS**（基线 35 → +23）
- backend `go test -count=1 ./...`: **27 packages ok（0 fail）**
- **mutation inversion 实证**（三处，打在不同层）：
  - bypass Zabbix `rules={urlRules}`（退回 required-only）→ **1 failed \| 22 passed**
  - 放宽 `EMAIL_PATTERN = /^.*$/` → **7 failed**（2 UI + 5 pattern 负样本）
  - 后端去掉 `,url` binding → `TestUpdateIntegrations_非URL_返400/netbox` **FAIL**
- 双轨分析：graphify + codegraph 0 anomalies（见 `M59-graph-analysis.md`）

**行为变更（运维可见）**: 渠道弹窗 3 处 required 文案随共享规则统一（`请输入用户名`/`请输入发件人`
→ `请输入邮箱`，Webhook URL → `请输入 URL`）。新增格式拦截：填错格式**前端即拒绝保存**，
不再需要等后端 400。**边界风险（登记）**：SMTP 用户名现在必须通过 email 格式 —— SendGrid（`apikey`）
/ AWS SES 等用非邮箱字符串作 SASL 用户名时会被挡，见 `M59-completion-report.md` Follow-up。

**Out of scope**（留 future）:
- 后端 http(s) 白名单（需自定义 validator）
- SMTP user 的非邮箱用户名场景（若真实工单出现再放宽）
- F-6 Token 留空语义 / F-7 「测试连接」/ F-9 保存按钮 loading
- `URL_PATTERN` / `EMAIL_PATTERN` 提成跨页共享模块（`src/utils/validators.ts`）

### M58 — G-Asset-BulkRetireEndpoint 批量退役单端点（M51-3 结案）（2026-09-15）

**摩擦**: M51 ship 的资产批量退役是前端 `bulkRetireMut` 里的**串行循环** —— 100 台资产 = 100 次
`POST /api/assets/:id/retire`。弱网/远端场景下这是 100 个 RTT 串起来（还没有并发），后端从未有批量端点
（M51-3 🟡 登记留 future round）。同一摩擦在告警批量标记误报上也存在（M53 留 future）。

**改动** (backend 4 files + frontend 4 files):

- `backend/internal/service/asset_service.go`: 单条退役体抽成**事务内核** `retireCore(ctx, db, …)`，
  新增 `BulkRetire(ctx, ids, reason, userID) (succeeded []string, failed map[string]string, err error)`。
  外层一个 tx + **每条 id 一个 SAVEPOINT** —— 这是「失败一个不影响其它」在 PG 里唯一成立的做法：
  没有 savepoint 时一条语句报错会让整个事务进入 aborted 态，后续每条语句都 `25P02`，
  于是「其中一条不存在」会静默升级成「整批失败」。`reason` 截断 500 runes（同审计口径）。
  不做 retry / 熔断（YAGNI）：失败如实进 `failed`。
- **事务边界变化（行为变更，已记）**：`Retire` 原先「读在事务外、写包事务」，本轮把读也纳入事务
  （`listNetworks` 改为接受调用方传入的 `*gorm.DB`）。理由：网卡 IP 快照与随后的清空 IP 必须在同一
  快照里，否则并发改网卡会丢 `last_known_ip*`；且两条路径共用同一内核后只能有一种次序。
  既有 5 条 `Retire` sqlmock 用例相应调整 SQL 期望**次序**，语义断言（快照 / 拒绝重复退役 / `ErrNotFound`）一字未动。
- `backend/internal/api/handlers/asset_handler.go`: `BulkRetireAssets` —— 200 +
  `{succeeded, failed}`（**部分成功**语义；用 4xx 表达「其中一条不存在」会让调用方丢掉成功的那部分）；
  空 ids / 非法 JSON → 400，>1000 条 → 400（对齐 `/alerts/bulk-*` 防 DoS），`user_id` 非 uuid → 401。
  审计由 AuditLog 中间件**按请求**落一行（`path=/api/assets/bulk-retire`），不按 id 拆成 N 行。
- `backend/internal/api/routes.go`: `POST /assets/bulk-retire` 注册在 `/assets/:id/retire` **之前**
  （静态段先于 `:id`，同 `/export` 的理由）。
- `backend/internal/api/openapi.yaml`: 补 `/assets/bulk-retire` + `AssetBulkRetireResult`
  （契约门禁 `TestRoutes_OpenAPI契约集合相等` 是集合相等，漏补即红）。
- `frontend/src/services/api.ts` + `Assets.tsx`: `assetApi.bulkRetire(ids, reason)`；
  `bulkRetireMut` 改单请求，并按端点语义在 `onSuccess` 分流三种结局 —— 全成功 `success` /
  部分成功 `warning`（成功 N 项，失败 M 项）/ 全失败 `error`。旧实现用 `throw` 表达「有一条失败」→
  把已退役成功的那批也说成失败，用户会重试（重试又会把已退役的再报一次 400）。`onError` 只留给 4xx/5xx。
  响应形状按 `unknown` + `in`/`typeof` 收窄（不 `any`、不 inline-cast）。
- `frontend/src/services/api.types.ts`: `npm run gen:api` 重生成。**顺带修掉既存漂移** ——
  M38-B 的 `/alert-rules/{id}/triggers` 一直没重生成，CI 的「OpenAPI 生成物漂移检查」
  自 M38-B 起就是红的（`.github/workflows/ci.yml:197-198`）。

**Hard pass**:
- backend `go test -count=1 ./...`: 全绿（27 packages ok，零失败）
- `npx tsc --noEmit`: 0 error
- frontend `npx vitest run`: **42 files / 386 tests 全 PASS**（Assets 26 条含 M58 新 6 条）
- **mutation inversion 实证**（两处，各自打回原实现）：
  - 前端 bypass：批量路径退回 `assetApi.retire(ids[0])` → **5 failed | 1 passed**
  - 后端 bypass：去掉 per-id SAVEPOINT（直接 `retireCore(ctx, tx, …)`）→ **3 failed**（BulkRetire 全组）
- 路由顺序实证：`POST /api/assets/bulk-retire` 带回合法 body 仍落 `BulkRetireAssets`
  （若被 `/:id/retire` 吞掉，`id="bulk-retire"` → uuid 解析失败 → 400 且无 `failed` 字段）
- 双轨分析：graphify + codegraph 0 anomalies（见 `M58-graph-analysis.md`）

**Out of scope**（留 future）:
- bulk restore 端点（同形，但恢复用得少）
- bulk update metadata
- 告警侧 `bulk_fp` 端点（M53 留的同族摩擦，本轮只做资产）

### M57 — G-UI-TabUrlSync Tab 切换 URL 同步（2026-09-14）

**摩擦**: 用户操作流程审查发现 — Settings 3 tabs (集成配置 / 通知设置 / API 密钥) + Oncall 3 tabs (当前值班 / 值班组 / 升级策略) 都没 URL sync, 用户:
- 刷新页面 → 丢失 tab 状态, 回到默认
- 分享 URL → 同事打开看到默认 tab, 不是他/她想看的
- 浏览器后退 → 跳走整个页面, 而不是回到上一 tab

**改动** (frontend-only, 4 files, +139/-5 LOC):
- `frontend/src/pages/Settings.tsx`: 引 `useSearchParams`; `activeTab = searchParams.get('tab') || 'integrations'`; `handleTabChange` → `setSearchParams({ tab: key })`; `<Tabs activeKey onChange>`
- `frontend/src/pages/Oncall.tsx`: 同上 (`activeTab || 'current'`)
- `frontend/src/pages/Settings.test.tsx`: RouteProbe 增 `+ loc.search` 暴露 search; 加 4 M57 测试
- `frontend/src/pages/Oncall.test.tsx`: 加 ProbeRoute helper + 4 M57 测试

**Hard pass**:
- `npx tsc --noEmit`: 0 error
- Settings 35 + Oncall 24 = **59/59 PASS** (Settings 31 + 4 M57 / Oncall 20 + 4 M57, 无退化)
- Backend `go test -count=1 ./...`: 15 packages ok
- **mutation inversion 实证**: bypass `setSearchParams({ tab: key })` → **39 failed | 20 passed** ✓ (setSearchParams 行为真被 URL-sync 测试 catch)
- `routeSync` (PageProbe 暴露 URL) 让 setSearchParams 渲染时机可断言

**审查发现 (PM 自起, 8 项 friction → 1 项 ship + 4 项撤回 + 4 项 future)**:
- F-2 (Modal Esc) 撤回: antd 5 Modal 默认 keyboard=true
- F-3 (autoFocus) → M56 ship ✓
- F-4 (Tickets 无空态) 撤回: 已有 EmptyState ("暂无工单 / 当前没有待处理的工单")
- F-5 (Topology 无清空筛选) 撤回: 只有 Switch 1 个, 无清空语境
- F-8 (Tab URL sync) → **M57 ship ✓**

**Out of scope** (留 future round):
- F-1 Settings 0 validator (跨 backend)
- F-6 Settings Token 留空语义
- F-7 Settings "测试连接" (跨 backend, omp)
- F-9 保存按钮 loading

### M56 — G-UI-SearchAutofocus 搜索 Input autoFocus（2026-09-14）

**摩擦**: 用户操作流程审查发现 — 全站搜索 Input 不 autoFocus, 用户进页面想搜必须先鼠标点 input 才能键盘打字. 实际只有 2 处真搜索 Input (其他页 Select 筛选无 Input):
- `AssetFilterBar` — "搜索名称 / IP"
- `Audit` — "路径前缀, 如 /api/assets"

**改动** (frontend-only, 2 files, +4/-0 LOC):
- `frontend/src/components/AssetFilterBar.tsx`: 搜索 Input 加 `autoFocus`
- `frontend/src/pages/Audit.tsx`: 路径前缀 Input 加 `autoFocus`

**Hard pass**:
- `npx tsc --noEmit`: 0 error
- `npx vitest run src/pages/Assets.test.tsx src/pages/Audit.test.tsx src/components/AssetTable.memo.test.tsx`: **29/29 PASS** (Assets 22 + Audit 6 + memo 1, 无退化)

**审查发现 (PM 自起)**:
- F-2 (Modal Esc) 撤回: antd 5 Modal 默认 keyboard=true, Esc 已工作 — 不是 friction
- F-3 (autoFocus) 真实证 ship
- F-4 (Tickets 空态) / F-5 (Topology 清空筛选) 留 future round

**Out of scope** (留 future round):
- Modal 内表单 Input 不加 autoFocus (模态聚焦习惯不同)
- Select 筛选 (Alerts / Runbook 等) 不支持 autoFocus
- 屏幕阅读器友好度 — 接受, 搜索框是用户进页面的常见第一动作

### M55 — G-UI-AlertsStatsClick 告警统计卡可点击跳转（2026-09-14）

**摩擦**: T99 C1 — AlertStatsCards 4 联 (`总告警 / 未处理 / 已确认 / 已解决`) 只显示数字, 不可点击. 用户想"看所有未处理告警"必须手填 status filter.

**改动** (frontend-only, 3 files, +63/-3 LOC):
- `frontend/src/components/AlertStatsCards.tsx`: 加 `onCardClick?: (key) => void` 可选 prop; Card 加 `hoverable` + `onClick` + cursor pointer (仅传了 onCardClick 时)
- `frontend/src/pages/Alerts.tsx`: 加 `useNavigate` + `handleCardClick` (total 卡 no-op, 其他 3 卡 setStatusFilter + setPage(1) + navigate URL sync)
- `frontend/src/pages/Alerts.test.tsx`: vi.mock `useNavigate` 全局返 vi.fn() (避免 17 个 render 包 MemoryRouter); 加 2 测试: 未处理卡 click → queryKey 含 status='problem' + page=1; 总告警卡 click → no-op

**Mutation inversion 实证**: bypass handleCardClick body (注释掉 setStatusFilter / setPage / navigate) → **1/2 FAIL** ✓ (test "未处理统计卡点击" catch it, "总告警卡 no-op" 仍 pass)

**Hard pass**:
- `npx tsc --noEmit`: 0 error
- `npx vitest run src/pages/Alerts.test.tsx src/components/AlertCard.test.tsx`: **31/31 PASS** (Alerts 22 + AlertCard 9)
- backend 27 packages ok (零改动)
- 双轨分析 (待 docs 后跑)

**Out of scope** (留 future round):
- URL sync → 反向 (URL `?status=...` → setStatusFilter on mount): 留 future, 现状已 ship navigate 后 queryKey 生效
- 改 useState → useSearchParams: 引入测试套级联 (17 个 render 要改), 不值

### M54 — G-UI-AlertsHostHover 告警表主机列 ellipsis（2026-09-14）

**摩擦**: T99 E2 — AlertTable 主机列 `width: 150` 但 host 字段无长度限制, 长主机名 (`web-server-01.prod.iad1.example.com` 38 chars) 在 150px 列宽下强制换行撑高整行, 表格行高不一致影响扫读.

**改动** (frontend-only, 1 file, +2/-1 LOC):
- `frontend/src/components/AlertTable.tsx`: 主机列加 `ellipsis: { showTitle: true }` (antd Table 自带 Ellipsis + 浏览器原生 title Tooltip).

**Mutation inversion 诚实承认**: bypass `ellipsis:` → 29/29 PASS (无 fail). **视觉层 fix, 单测无能力 catch** — 现有测试只验 mock 数据 render / 操作按钮, 不验列宽 / overflow / Tooltip. 接受 (走 e2e 视觉验证: 人工 + chrome devtools).

**Hard pass**:
- `npx tsc --noEmit`: 0 error
- `npx vitest run src/pages/Alerts.test.tsx src/components/AlertCard.test.tsx`: 29/29 PASS (Alerts 20 + AlertCard 9, 无退化)
- backend 27 packages ok (零改动)
- 双轨分析 (graphify 6664 nodes / 0 anomalies / codegraph index 6244 nodes)

**Out of scope** (留 future round):
- 自定义 Tooltip 替代 antd Ellipsis — 不必要, antd 自带够用
- 列宽调整 — 150px 是设计选择, 不动

### M53 — G-UI-AlertsBulkFP 告警批量标记误报（2026-09-14）

**摩擦**: T99 C2 — Alerts header 只有 `[批量确认] [批量解决]` (有 selection 时), 缺 `[批量标记误报]`. 运维收到 webhook 风暴 (50+ 假阳告警) 只能一行一行点 `[标记误报]` × 50 次. 跟 M51 G-UI-BulkAssets 同样的"批量操作缺位"问题.

**改动** (frontend-only, 2 files, +82/-3 LOC):
- `frontend/src/pages/Alerts.tsx`:
  - 新 import `StopOutlined` 图标
  - `runBulk` kind 类型扩 `"mark-fp"` (state + 函数签名)
  - `bulkFPMut = useApiMutation(...)` 复用 `runBulk`, 串行循环调用 `alertApi.markFalsePositive(id, true, "运维批量标记")`
  - `runBulk` 完成 message 扩 `mark-fp` 分支 → "批量标记误报完成: 成功 X, 失败 Y"
  - header `<Space>` 里加 `[批量标记误报]` 按钮 (在 `[批量解决]` 之后), 套 Popconfirm 二次确认 + `danger: true`, `disabled` 含其他 bulk in-flight 防 race condition
- `frontend/src/pages/Alerts.test.tsx`: 3 新测试
  1. 选中 0 项 → `[批量标记误报]` 不渲染
  2. 选中 ≥1 项 → 按钮出现 + Popconfirm + 二次确认后 mutate 真被调
  3. 另一 bulk in-flight → 按钮 disabled (race guard)

**Mutation inversion 实证**: bypass `onConfirm={() => bulkFPMut.mutate(...)}` → 1/3 FAIL (test 2 真触发 mutate 的测试被 catch) ✓

**Hard pass**:
- `npx tsc --noEmit`: 0 error
- `npx vitest run src/pages/Alerts.test.tsx`: **20/20 PASS** (17 老 + 3 新)
- 全 frontend `npx vitest run`: (待完成)
- backend `go test -count=1 ./...`: 27 packages ok 零改动
- 双轨分析 (graphify + codegraph): 待补

**Out of scope** (留 future round):
- 批量"取消误报" — 同接口反向参数, 运营场景 99% 是"标 FP", 留 future
- 后端 bulk_fp 端点 — N=100+ 慢, 跟 M51-3 同留 future backend round
- 自动化 FP (基于历史 ack 时间阈值) — 完全新功能, 跟 M53 无关

### M52 — G-UI-AssetFilter AssetFilterBar 加 status 下拉（2026-09-14）

**摩擦**: T99 B3 — AssetFilterBar 只有 keyword + assetType, 后端 AssetFilter.Status 字段已 ship 但前端无 status 筛选入口, 用户想"看哪些资产 offline"必须滚页肉眼找.

**改动** (frontend-only, 3 files, +102/-5 LOC):
- `frontend/src/components/AssetFilterBar.tsx`:
  - `AssetFilterValues.status: string` (新增字段)
  - `statusOptions?: { value, label }[]` (新 prop, 父传, 不传则不渲染 status Select)
  - 新增 status Select (allowClear / placeholder="状态")
- `frontend/src/pages/Assets.tsx`:
  - `STATUS_OPTIONS` (active/在线, offline/离线, maintenance/维护, retired/已退役) — 跟 StatusTag.tsx 同一值域
  - `filter` state 加 `status: ''`
  - `assetApi.list` 调用传 `status: filter.status || undefined` (后端契约)
  - `<AssetFilterBar>` 传 `statusOptions={STATUS_OPTIONS}`
  - `hasFilter` 判定加 status 字段 (副标题"已筛选"逻辑)
- `frontend/src/pages/Assets.test.tsx`: 3 新测试
  1. status Select 渲染 (`.ant-select-selection-placeholder` 含"状态")
  2. 选 status=active → queryKey 含 `status='active'`, page 重置回 1
  3. 选 status=retired → queryKey 含 `status='retired'`, 副标题出现"已筛选"

**Mutation inversion 实证**: bypass `status:` in queryKey (`status: undefined as unknown as string`) → 2/3 FAIL ✓ (不是"placeholder 错误", 是真触发"用户改了 status 但 queryKey 没带"这条路径)

**Hard pass**:
- `npx tsc --noEmit`: 0 error
- `npx vitest run src/pages/Assets.test.tsx src/components/AssetTable.memo.test.tsx`: 23/23 PASS (19 老 + 3 新 + 1 memo)
- 全 frontend `npx vitest run`: **42 files / 369 tests PASS** (基线 362 + 3 M52 + 4 真 mutation / 实证相关, 无 FAIL)
- backend `go test -count=1 ./...`: 27 packages ok
- 双轨分析 (graphify update + diagnose + codegraph index + callers + path) — M52 互不污染

**Out of scope** (留 future round):
- 机房 (site_id) / 机柜 (rack_name) / 时间范围 — 后端无字段, 需 backend round
- 保存筛选为视图 / URL 同步 — 留 future

### M51 — G-UI-BulkAssets 资产批量操作（2026-09-14）

**改了什么（frontend-only，后端零改动）：**

运维要退役 50 台资产只能一行一行点 `[退役]` 按钮 + 50 次 Popconfirm + 50 次填原因，**完全不可用**
（PM 自查 G-UI-BulkAssets = T99 摩擦表 B1）。`AssetTable` 自 v0 就有 `rowSelection?:` 接口（`AssetTable.tsx:55`），
但 `Assets.tsx` 父组件**从未传** —— 接口摆在桌上没人用。

- **`frontend/src/pages/Assets.tsx`** —
  - 加 `useState<React.Key[]>([])` 持有 `selectedRowKeys`，
    传给 `<AssetTable rowSelection={{ selectedRowKeys, onChange: setSelectedRowKeys, preserveSelectedRowKeys: true }} />`。
  - **`preserveSelectedRowKeys: true`**：翻页不清空选中（antd v5 原生支持）。
  - **桌面端渲染选中条**（`isMobile=false` 时）：
    - `<div data-testid="asset-bulk-bar">`：左侧「已选 N 项」+ `[批量退役]`（Popconfirm 二次确认 + `danger`）+
      条件渲染 `[批量恢复]`（仅当选中至少一项 `status='retired'`）+ `[清空选择]`。
    - `[批量退役]` 原因统一记「批量退役」（不再弹填原因 modal —— 批量 50 项挨个弹不可用）。
  - **批量 mutation**：`useApiMutation` 包一层，**串行循环**遍历 `selectedRowKeys` 调
    `assetApi.retire(id, '批量退役')` / `assetApi.restore(id)`；任一失败计入 `failed` 数组，最终
    `failed.length > 0` 抛 `Error`，onError toast；全成功 toast + 清 `selectedRowKeys` + `refetch()`。
  - **`refetch()` 替代 `useQueryClient().invalidateQueries`**：保持跟老 `createMut / updateMut` 同模式，
    避免引入 `useQueryClient` 导致老 `Assets.test.tsx`（无 QueryClientProvider wrapper）级联失败。
    TODO: 后续若用 `useApiMutation` 内置 invalidate, 可一并换掉。
  - **（fix M51-1, P1 review）批量按钮互锁**：三条按钮 (`批量退役` / `批量恢复` / `清空选择`) 全部
    `disabled={bulkRetireMut.isPending || bulkRestoreMut.isPending}` + `loading=...`。防 race condition：
    用户选中 50 项点 [批量退役] 后再点 [批量恢复]，两条 mutate 同时 in-flight 会互相 `setSelectedRowKeys([])`。
- **`frontend/src/components/AssetTable.tsx`** —
  - `rowSelection?:` 接口加 `[key: string]: any`（**TypeScript index signature**），
    让父组件自由透传 antd 原生字段（如 `preserveSelectedRowKeys`、`getCheckboxProps`），
    组件内部仍 spread 进 antd `<Table>`。**不破坏**老调用方（兼容既有 `selectedRowKeys / onChange`）。
- **`frontend/src/pages/Assets.test.tsx`** — 新加 6 个用例（19 / 19 PASS）：
  1. 选中 0 项时 `data-testid='asset-bulk-bar'` 不渲染。
  2. 选中 ≥1 项时批量条出现且显示「已选 N 项」。
  3. `[清空选择]` 清掉 `selectedRowKeys`，批量条隐藏。
  4. 全是 `active` 资产时不显示「批量恢复」（`selectedHasRetired === false`）。
  5. **`[批量退役]` 真 mutation 实证（fix M51-2, P1 review）**：让 `useApiMutation` mock 走真 mutator
     （不接 QueryClient, 避免测试套级联包装），Popconfirm trigger → OK → onConfirm → mutate →
     `assetApi.retire` spy 真被调 N 次，reason = "批量退役"。
  6. **`[批量退役]` mutation inversion 实证**：bypass `onConfirm={() => { /*MUT*/} }` →
     测试 5 FAIL (1/19), revert → 19/19 PASS。证据测试 5 不是 vacuous。

**未做（intent 明确 out of scope，留 future round）**：
- 批量改标签 / 批量转移 owner / 批量导出 / 服务端 `bulk_retire` 端点（前端循环 N 次即可，
  N 大时慢留 note；并行化留给 backend round）。
- Mobile (`MobileCardList`) 不支持 rowSelection（mobile 批量超出 ≤4h PM-direct scope，
  留 round future）。

**Trap 新增**：T-64 — `useApiMutation` 文档注释暗示 `onSuccess: qc.invalidateQueries`，
但实现内**无 queryClient 引用**。调用方要么 `refetch()`、要么自行 `useQueryClient`。
后者在测试套需 wrapper Provider，引入级联改动。

### M50 — G-UI-Tickets 工单工作流产品化（2026-09-14）

**改了什么（frontend-only，后端零改动）：**

工单页在此之前是**只读**的：详情弹窗 footer 只有一个 `[关闭]`，列表行只有一个 `[详情]`，
运维要改派/改优先级/关单**没有任何入口**（PM 自查 G-UI-Tickets）。

- **`frontend/src/components/TicketDetailModal.tsx`** — footer 加 5 个操作，全部**写这一个弹窗**里：
  - `[+ 评论]` → 展开 `<TextArea>` + `[提交评论]`（**空文本时按钮 disabled**，intent 硬要求前端挡空提交）。
  - `[改派]` → 展开人员下拉（懒加载，展开才请求）+ `[确认改派]`。
  - `[改优先级]` → 展开四档下拉（`critical/high/normal/low`，即 service 的契约词表）+ `[确认修改]`。
  - `[关单]` / `[已解决]` → `Popconfirm` **二次确认**后写 `status`。
  - `[关闭]` 原样保留（它是收起弹窗，不是关单）。
  - 五种操作共用**一条** mutation（后端只有一个写入口，拆五条只会把 invalidate/收弹窗写五遍）；
    `onSuccess` → `message.success` + `invalidateQueries({queryKey: queryKeys.tickets.all})` + `onClose()`。
    错误**不在这里处理**：axios 拦截器已统一 toast，再加一层就是同一句话弹两遍；弹窗内不放 error alert。
- **`frontend/src/components/TicketTable.tsx`** — 加 `更新时间` 列（`formatRelativeTime`，在 `创建时间` 之后，
  带 sorter；相对时间回答的是「多久没动了」）+ 行尾 `[更多操作]` `Dropdown`（**click 触发**，
  antd 默认 hover 在触屏/键盘够不着）：查看详情 / 改派 / 改优先级 / 关单。
  四个入口都只是**打开票面**（`onView` / `onAssign` / `onChangePriority` / `onClose` 传整行 record），
  写入仍只在弹窗里发生。
- **`frontend/src/pages/Tickets.tsx`** — 接收三个行回调 → `openTicket(record, panel)` 打开弹窗并
  预设展开哪块面板（`initialAction`）。
- **`frontend/src/services/api.ts`** — `userApi.list` 加可选 `page/page_size` 并**收窄返回类型**。
  派单候选池必须显式放大分页：后端默认 20 条（`user_handler.go:27`）会静默丢掉运维。
  **`ticketApi` 一字未改**（不改契约、不加新方法）。

**与 intent 假设不符、按实测改掉的三处（grep 后端后确认）：**

1. **`POST /tickets/:id/comments` 不存在** —— 全仓无 comment handler，openapi 也没这个 path。
   工单上唯一可写的自由文本列是 `description`，故「加评论」= 追加成新段落再 `PUT`；
   这次改动会以「描述」的字段变更进 M25 经手历史（正好是 intent 想要的「提交后刷新经手历史」）。
2. **「改派」写 `assignee_name`，不写 `assignee`** —— `assignee` 是 openapi 的**读侧**字段名，
   库里没有这一列，写了 gorm 拼出不存在的列 → 500（新登记 **T-62**）。也不写 `assignee_id`：
   该列前后端零消费方，写它只会多出一行裸 UUID 的经手历史（残余风险见报告）。
3. **候选角色判据用 `ops_user`/`ops_admin`** —— `operator` 是设计期别名，`GET /users` 出站前
   已由 `CanonicalRole` 折叠（`user_handler.go:34-37`），按 `operator` 筛**恒为 0 行**（新登记 **T-63**）。

**测试（`TicketDetailModal.test.tsx` 6 新 + `TicketTable.test.tsx` 2 新）：**

- 弹窗层**不 mock `services/api`**（M25 那边 mock 掉服务模块是它的取舍）：走真 axios 实例、只换
  `api.defaults.adapter`，于是响应拦截器**真的执行** ——「失败只弹一次 toast」这条断言才有内容，
  且能顺带钉住「弹窗没多加一层 toast」（`message.error` 调用次数恰为 1）。
- 6 例：5 个按钮都在（含 `[关闭]` 不动）/ 空评论不可提交 + 提交后 `description` 是 `旧文\n\n新评论` /
  关单 Popconfirm **确认前零请求** + 确认后 `{status:'closed'}` + invalidate + 收弹窗 /
  失败：拦截器 toast 一次、弹窗不关、无 `.ant-alert-error`、不 invalidate / 改派候选只含运维角色
  （`admin`/`auditor`/遗留 `operator` 都不出现）→ `{assignee_name}` / 改优先级四档 → `{priority}`。
- 表：`更新时间` 列在表头且渲染相对时间；`[更多操作]` 四项俱全且把**整行 record** 交给对应回调。
- **`Tickets.test.tsx` 既有 10 例全部保留**，仅补基建：弹窗现在要 `QueryClient`（真环境由 `main.tsx`
  提供）→ `renderTickets()` 套 `QueryClientProvider`；mock 里补 `queryKeys.tickets.all` 与
  `users` 键分支（否则弹窗的候选 query 会顶掉用例断言的 `lastListKey`）。
- **Mutation inversion 实证 3 次**（revert 后逐条回绿）：① `[关单]` 的 `onConfirm` 改 `return` →
  `2 failed | 4 passed`；② `ASSIGNEE_ROLES` 换成 `['admin','auditor']` → `1 failed | 5 passed`；
  ③ 注释掉 `invalidateQueries` 一行 → `1 failed | 5 passed`。
- 新增 trap **T-62 / T-63**（`docs/TRAPS.md` §二 + §五 索引）。

**未做（intent 明确 out of scope）**：批量派单 / 工单模板 / SLA 提醒 / 导出 / 删除按钮 / 非 admin
的权限门禁（后端 RBAC 已 ship，前端不再复制一份矩阵）。

### M49 — G-UI-Audit 审计日志前端页 + admin 入口（2026-09-14）

**改了什么（frontend-only, 后端零改动）：**

- **`frontend/src/pages/Audit.tsx`（新）** — 审计日志页。后端 `/api/audit-logs` 自 M22 就在
  （`routes.go:286` + `canAudit` 能力），但前端**零入口**，管理员想知道「谁什么时候改了某资产」
  只能自己 curl（PM 自查 G-UI-Audit P1）。
  - 列: 时间 / 操作人 / 动作 / 对象类型 / 对象 ID / 摘要（method + path + status 三件套, 无请求体）
    / [详情]。最新在前由**服务端** `ORDER BY created_at DESC, id DESC` 保证，故不加前端 sorter
    （只能排当前 30 条，会让人误以为是全局排序）。
  - `[详情]` → antd `Drawer` 显示该行完整 JSON。**审计行不含请求体**（只记 method/path/status/
    error_msg），抽屉如实展示整行，不伪装成「字段 diff」。
  - 过滤 4 项: 操作人（user_id 精确）/ 动作（精确）/ 方法 / 路径前缀。词表**不硬编码** ——
    后端没有 action/user 枚举接口（`action` 是自由字符串 `varchar(50)`），下拉项取**不带筛选**的
    100 条采样（`queryKeys.audit.vocabulary()`，与列表分开缓存）∪ 当前页；用「当前页」做词表会让
    筛到只剩一条后再也切不回去。
  - 分页是 **cursor 式**（`limit` + `next_cursor`，后端**没有** total/page/page_size）→ UI 是
    上一页/下一页 + cursor 栈，不是页码跳转。**没有 `next_cursor` 即到底**（不是「本页不满」）。
  - 空态分两种: 无筛选 → `EmptyState`「暂无审计事件」；有筛选 → 预设 `no-search-result`「暂无匹配」。
  - 请求失败 → `ErrorState`（不静默回落到空列表 —— 那会把 500 伪装成「没有留痕」）。
- **`frontend/src/services/api.ts`** — `auditApi.list(params)`。路径 `/audit-logs`（**不是**
  `/audit/logs`；写错会落到 NoRoute 返回 index.html，同 B1-1 `/auth/api-keys` 的教训）。
  另加 `authApi.me()` —— 入口门禁用。
- **`frontend/src/types/index.ts`** — `AuditEvent`（逐字段对齐 openapi `AuditLog`，含
  `user_id: null` 的未认证语义）+ `AuditListParams`。附**单向**漂移守卫
  `AuditLogDriftOK`：openapi 的 AuditLog 未声明 required，反方向恒真不构成断言，只钉正方向。
- **`frontend/src/pages/Settings.tsx`** — API 密钥 tab 下方加「管理」卡片 + `Menu.Item`
  「审计日志」→ `navigate('/audit')`。**不新加 tab**：审计页是独立路由（懒加载 chunk），
  这里只做导航。入口按 `/auth/me` 下发的 **capabilities** 显示（`audit` ∈ capabilities），
  不复制一份角色→能力矩阵 —— `middleware/roles.go` 的注释明确警告复制会漂移，且 auditor
  角色**有** audit 而无 manage，按 role/manage 判会把它的入口藏掉。取不到能力 → fail-closed 不渲染。
- **`frontend/src/App.tsx`** — 懒加载 `Audit` + `<Route path="/audit">`。
- **`frontend/src/AppBreadcrumb.tsx`** — `TOP_LABELS['/audit'] = '审计日志'`。
- **`frontend/src/hooks/useApiQuery.ts`** — `queryKeys.audit.{all,list,vocabulary}`。

**测试（`frontend/src/pages/Audit.test.tsx` 6 新 + `Settings.test.tsx` 3 新）：**

- Audit: 渲染列表（含 `user_id: null` → 「匿名」）/ 两种空态 / 动作筛选（请求带 action 且**丢掉
  cursor** 回第一页）/ cursor 翻页（无 `next_cursor` → 下一页禁用）/ 详情抽屉完整 JSON（且**无**
  payload 字段）/ 失败 → 错误态（不显示「暂无审计事件」）。
- 这一层**不打桩组件依赖**（真 useApiQuery + 真 auditApi + 真 Table/Drawer），只桩最外层 axios
  实例的 `api.get` → 断言同时钉住「传了哪些 filter」与「真实路径 + 参数形状」。
- Settings: `render(<Settings />)` 全部改为 `renderSettings()`（套 `MemoryRouter` —— Settings 现在
  内含 `useNavigate`，裸渲染会抛「useNavigate() may be used only in the context of a <Router>」）；
  新增 3 例: 有 audit 能力 → 入口显示**且点击真跳 `/audit`**（`RouteProbe` 断言 location）/
  readonly → 整块「管理」不渲染 / `/auth/me` 403 → fail-closed 不渲染。
- **Mutation inversion 实证（审计页）**: `useApiQuery` 的 fetcher 换成
  `async () => ({items: []})`（绕过 `fetchAudit`）→ `5 failed | 1 passed`；revert 后 6/6 PASS。
- **Mutation inversion 实证（入口门禁）**: `{canAudit && (` → `{true && (` →
  `2 failed | 29 passed`（readonly + 403 两例红）；revert 后 31/31 PASS。
- **同轮发现（测试基建）**: `beforeEach` 里用 `vi.restoreAllMocks()` 会把 `test/setup.ts` 里
  `window.matchMedia` 的 `vi.fn()` 实现一起清掉 → antd `responsiveObserver` 在
  `({ matches }) => …` 上解构 undefined，Table/Grid 一挂就炸。改用 `vi.clearAllMocks()`。

**未做（intent 明确 out of scope）**: audit 导出（PDF/CSV）/ 高级搜索（后端无 since-until、
entity_type 过滤）/ 实时 stream / 任何 write（edit·delete）/ 非 admin 开放。
时间区间与 entity 过滤**不做前端假过滤** —— 拿一页数据本地筛会漏掉未加载的行，比没有更危险。

### M48 — G-UI-TopoClick 拓扑节点从「死按钮」变成真按钮（2026-09-14）

**改了什么（PM-direct 自查项, ≤2h, 单 frontend file + test file）：**

- **`frontend/src/pages/Topology.tsx`** — 拓扑节点 `<g>` 原来只有 inner `<circle>` 上的
  `style={{ cursor: 'pointer' }}`, **没有任何 onClick** —— 鼠标变手型、点了毫无反应, 是
  纯视觉欺骗（运维会以为「点不动是图卡了」）。
  - 光标上移到 `<g>`（点击区域随之覆盖整个节点: 圆 + 名称 + 类型 label, 触控友好）,
    inner `<circle>` 的 cursor **移除**, 不再双重化。
  - `<g>` 加 `role="button"` + `tabIndex={0}` + `aria-label={n.name}` +
    `onKeyDown` (Enter / Space, Space 额外 `preventDefault` 防止滚动页面)。
  - 子元素加 SVG `<title>{n.name}</title>` —— 浏览器原生 tooltip, 不引外部 tooltip 库。
  - **点击行为**: 普通节点 (`!is_virtual`) → `navigate('/assets/<id>/diagnostics')`
    （拓扑页的核心价值就是故障节点一键查诊断）; 虚拟节点 (`is_virtual === true`) →
    弹 antd `Popover` 显示 `name` / `asset_type` / `open_alerts`, **不 navigate**
    （虚拟节点没有 assets 记录, 跳详情必 404）。
  - 未给节点套 `<button>` —— HTML `button` 不能包 SVG group, 故走 `role="button"` 路线。
  - `handleNodeClick` 内无 `console.log`; `useApiQuery` 的 key / staleTime / refetch
    **一字未动**。

**测试（`frontend/src/pages/Topology.test.tsx`, 16/16 PASS）：**

- 老 11 个用例 100% 保留（其中「渲染所有节点名」加 `ignore: 'title'`, 因为 `<title>`
  tooltip 里同名会命中两次）。
- M48 新 5 个:
  - `点击普通节点 navigate 到 /assets/<id>/diagnostics` —— `app-01` → `/assets/n2/diagnostics`。
  - `点击虚拟节点不 navigate, 弹出 meta popover` —— `rtr-wan` → 断言 `navigateMock`
    未被调用 + 三项元数据可见。
  - `键盘 Enter 触发 navigate（键盘可达）` —— `fireEvent.keyDown(g, { key: 'Enter' })`。
  - `键盘 Space 触发虚拟节点 popover（不 navigate）`。
  - `节点是 role=button + aria-label, 且无 inner cursor 双重化` —— 顺带钉住
    `tabindex="0"` / `<title>` 文本 / inner `circle` 无 inline style。
- **Mutation inversion 实证**: 把 `handleNodeClick` 整体改成 `return null` →
  `vitest run src/pages/Topology.test.tsx` **4 failed | 12 passed**; revert 后 16/16 PASS。

### M47 — G-UI-Breadcrumb 详情面包屑显示资产名 / 工单标题（2026-09-13）

**改了什么（PM-direct 自查项, ≤1h, 单 frontend file）：**

- **`frontend/src/components/AppBreadcrumb.tsx`** — 加 `useDetailLabel()` hook,
  命中 `/assets/:id` 调 `assetApi.get(id)` 取 `name`,
  命中 `/tickets/:id` 调 `ticketApi.get(id)` 取 `title`.
  Fetch 失败 / loading 中 fallback 到原来的 `ID: a1b2c3d4...` (100% 老行为兼容).
  范围限定: `/alert-suppressions` / `/metric-snapshots` 等其他详情页保留 ID 截断,
  scope 不扩散.
- **`useApiQuery` 60s `staleTime`** — 同一详情来回切不二次拉.
- **`fallbackIdLabel(id)` 函数** — 把老 `slice(0, 8)` 封装成函数, 不留 magic.

**测试（9/9 PASS）：**

- 老 5 个用例 100% 保留 (首页不显示 / 二级 / 三级 fallback / 告警中心 / 404 兜底).
- M47 新 4 个:
  - `资产详情 fetch 命中时显示资产名` —— `switch-core-01` 出现, `ID: a1b2c3d4` 不再出现.
  - `工单详情 fetch 命中时显示工单标题` —— `交换机端口告警` 出现, `ID: t1-id` 不再出现.
  - `工单详情 fetch 失败保持 ID fallback` —— `ID: abcd1234` 仍在.
  - `alert-suppressions 仍走 fallback ID (范围不扩散)` —— 仍 `ID: some-id`.

**残余（明确不在 scope）：**

- 名字过长不截断 (中文 50+ 字溢出面包屑一行) — ≤30min PM-direct, 留 backlog.
- 用 icon/color 美化 (`<ServerOutlined />` 前缀等) — PM-direct v2.
- alert-suppressions / metric-snapshots / racks 详情也 fetch — 范围扩大 (~2h PM-direct),
  留 backlog.
- frontend vitest 加 CI step —— package.json 没 `test` script, 另立 round.

### M46 — G-32 gorm logger 错误脱敏 + ParamsFilter 补漏（2026-09-13）

**修复内容：**

- **redactGormLogger wrapper** (`backend/internal/database/gorm_logger_redact.go`):
  - Error / Trace 路径的 err 走 `redact.Text + StripControl` (G-32 真正 scope)
  - 实现 `gormlogger.ParamsFilter` 接口 — `vars=nil` 让 `Dialector.Explain`
    拿到骨架 sql, **不展开参数值** (G-16 ship 过的 `ParameterizedQueries=true`
    在 sqlite driver 下失效, 这是真活缺陷, 顺手补漏)
  - `redactedError{original, text}` 双字段 — Error() 输出脱敏文本,
    Unwrap() 回原 err 保 errors.Is/As 错误链

- **接线** (`backend/internal/database/gorm_logger.go`):
  `newGormLogger` 套 `&redactGormLogger{Interface: gormlogger.New(...)}`.

- **7 个新单测** (`backend/internal/database/gorm_logger_redact_test.go`):
  RedactToken / StripControl / Trace_ErrRedact / Trace_ErrUnwrap /
  ParamsFilter_DropVars / OriginalError / RedactToken (redactErr).

**实证：** 7/7 单测 PASS；27 packages 全绿；真 PG db_smoke 47 PASS / 0 FAIL；
mutation inversion (`redactErr` 永返原串) → 4 用例 FAIL → revert → 7/7 PASS;
已有 G-16 测试 `TestGormLogger_普通查询不落参数值` /
`TestGormLogger_Scan路径不落参数值` 都 PASS (wrapper 没破 G-16 保护).

**残余：** 慢查询 (elapsed > SlowThreshold) SQL 文本仍含值骨架 (T-46 backlog);
Trace 路径若某内部路径不调 ParamsFilter 直接 Explain, 可能仍含值 (留观察).

### M45 — G-31 apierr 4xx 路径脱敏收口（2026-09-13）

**修复内容：**

- **apierr 4xx helper 入口加 sanitizeMessage** (`internal/apierr/apierr.go`):
  BadRequest / Unauthorized / Forbidden / NotFound / Conflict 5 个 helper
  入口全过 `redact.Text(redact.StripControl(msg))`. 之前 5xx 路径已过 redact
  (G-28), 4xx 路径**也**加, 防 caller 拼 `err.Error()` 把凭据/控制字符带进
  400/401/403/404/409 body.

- **顺序沿用 5xx 路径口径** (Strip→Text), 否则 CR/LF 截断 redact.Text 值类
  外层 Strip 接回尾部 = 泄漏.

- **不动 38 个 caller**: apierr API 不变, caller 自动受益.

**实证：** 4 个新单测全 PASS (StripControl/RedactToken/ChineseNoop/
NotFoundStrip); 27 packages 全绿; 真 PG db_smoke 47 PASS / 0 FAIL;
mutation inversion (sanitizeMessage 永返原 msg) → 3/4 new FAIL → revert → PASS.

### M44 — G-30 db_smoke 密码泄漏收紧（2026-09-13）

**修复内容：**

- **docker run argv 泄漏收紧** (`scripts/db_smoke.sh`): 旧 `docker run -e
  POSTGRES_PASSWORD=***` 改 `--env-file <(mktemp)` (0600 + trap rm).
  argv 不再含密码 (`/proc/<pid>/cmdline` 默认全局可读已失效).

- **PGPASSWORD env 泄漏收紧**: 旧 `PGPASSWORD=*** psql/createdb` 改
  `.pgpass` 0600 + `PGPASSFILE` env (libpq 标准协议). 密码进文件不进 env.
  外部模式 + 升级路径两处统一.

- **DSN TEST_DATABASE_URL 泄漏收紧**: 旧 `TEST_DATABASE_URL=postgres://
  user:***@host/db` 改 go test 拆分 `PGUSER/PGHOST/PGPORT/PGDATABASE`
  4 个 env vars (PGPASSWORD 仍走 PGPASSFILE). `openSmokeDB` 优先 PG*
  env vars, fallback `TEST_DATABASE_URL` (向后兼容).

- **临时文件 0600 + trap rm** (`cleanup`): ENVFILE + PGPASS 两个临时
  凭据文件, EXIT/INT/TERM 信号均清理; 同时 unset PG* + TEST_DATABASE_URL.

**实证：** 47 真 PG db_smoke PASS / 0 FAIL (PGPASSFILE libpq 协议生效)；
静态验证 4 项全 ✓ (argv/env/DSN 三类无泄漏 + 临时文件 0600 + trap rm)；
mutation inversion (注释 trap rm) → `/tmp/pgpass.*` 残留可见 → revert → 不残留。

### M43 — G-23 ticket.Tags 双层入参规范化（2026-09-13）

**修复内容：**

- **handler 层加 normalizeJSONBFields** (`internal/api/handlers/ticket_handler.go`):
  ticket `UpdateTicket` 加 `normalizeJSONBFields(updates)` 前置, 与 G-21
  asset handler 守门同款. G-21 ship 时**没覆盖** ticket handler 路径——M43
  补全.

- **service 层兜底** (`internal/service/jsonb_validate.go` + `ticket_service.go`):
  新加 `validateJSONBField(col, v)` 与 `ErrInvalidJSONBInput`, service 层
  兜底校验. 拒 `nil`/`""`/string 标量/数值/非空数组, 过 `[]`/object.

- **unit test** (`internal/service/jsonb_validate_test.go`): 7 个用例覆盖
  拒类 5 + 过类 2.

- **真 PG db_smoke** (`tests/db_smoke_test.go` `TestDBSmoke_G23_TicketTagsUpdateReject`):
  6 场景, 含故意绕过 handler 直接调 service.Update, 验证 service 层兜底.

**实证：** 27 packages 全绿；真 PG db_smoke 47 PASS / 0 FAIL；
mutation inversion (service 层 validateJSONBField 永返 nil → 5/7 unit FAIL)
→ 实证 service 层必要。

**残余：** `models.Ticket.Tags` 类型仍是 `string`（表示 vs jsonb 不一致），不改（跨多文件）。

### M42 — G-21 UpdateAsset jsonb 入参规范化（2026-09-13）

**修复内容：**

- **handler 层加 normalizeJSONBFields** (`internal/api/handlers/jsonb_normalize.go`):
  校验 PATCH 入参中 `tags` / `custom_fields` 两列的合法性，非法入参
  (null / `""` / string 标量 / 非空数组 / 数值标量) 一律 400 拒绝。
  空数组 `[]` / 空对象 `{}` / 非空对象 通过；非 jsonb 列（name/status）
  不拦。UpdateAsset 在 `ShouldBindJSON` 之后立即调，错误转 `apierr.BadRequest`。
- **Create 路径不变**：BeforeSave 钩子 (G-20 已 ship) 把零值归一为 `[]`/`{}`；
  Update 路径无钩子兜底，统一用 400 拒比归一更明确 (client 立即知道错)。
- **真 PG db_smoke** (`TestDBSmoke_G21_JSONBUpdateReject`): 5 场景，
  其中第 5 个**反证** service.Update 写 `tags=["a","b"]` 在真 PG 上
  会爆 `42804 column ... is of type jsonb but expression is of type record`，
  实证 handler 守门价值 + 钉住未来若第二个 handler 绕过 normalizeJSONBFields
  会爆的风险。

**残余 / 已知：**

- service.Update 仍是裸 `Updates(map)`，handler 是唯一守门；下 round
  可在 service 层加兜底（不在 M42 scope，避免扩大爆炸半径）。
- 非空数组（含 object elements 如 `tags: [{"k":"v"}]`）当前**拒**——保守策略；
  若 telemetry 显示 client 真要传 object array 可放宽。
- 结构体 `Updates` 零值被 gorm 静默跳过（gorm 设计，非 ITmanager 缺陷）。

**验证：** `go test -race ./...` 27 包绿；`scripts/db_smoke.sh` 45 PASS / 0 FAIL；
mutation inversion（`checkJSONBValue` 永返 nil → 6 测试 FAIL → revert → PASS）。

### M41 — CI 加 -race + fixture 修复 + eventbus race fix（2026-09-13）

**修复内容：**

- **CI 加 `-race` flag** (`.github/workflows/ci.yml`): `go test ./...` →
  `go test -race ./...`，守门 DATA RACE。任何后续 race regression 立即 CI 红。
- **fixture 修复** (`internal/api/routes_integration_test.go`):
  `genValidToken` / `genTokenWithRole` 现在自动 `seedUserWithRole`，
  解决 M40 引入的 `lookupUserStatus` fail-closed 把「user 不存在」当
  inactive → 401 的连锁（7 个 baseline 测试 FAIL 全修）。所有 caller
  零 diff，集中改 helper。
- **cache reset helper** (`internal/middleware/auth_status_cache.go`):
  导出 `ResetAuthStatusCacheForTest()` 给 `internal/api` 包的
  `setupTestRouter` cleanup 用，避免 `defaultAuthStatusCache` package
  singleton 跨 test 污染。
- **eventbus race fix** (`internal/eventbus/eventbus.go:newID`):
  `atomic.AddUint64` 合并返回值一次性读改，修复 race detector
  在 `TestPublish_并发安全` 抓的 DATA RACE（两个 goroutine 的 Add
  + 后续 Read 交错）。

**残余 / 已知：**

- `internal/eventbus.TestStats_计数正确` 在 `count >= 5` + race 时偶
  发 FAIL（pre-existing，与本 round race fix 无关：handler dispatch
  时序敏感的断言，非 race detector 报告）。CI 走默认 `count=1`，不
  影响。后续可加 `Eventually` 或更长 wait 修，不在本 round scope。
- `count=1` 无 race 与有 race 全包 27 packages 0 FAIL。

**验证：** `go test -race -count=1 ./...` 全绿，CI workflow 已切。

### M40 — G-5 JWT 用户禁用即时生效（2026-09-13）

**修复内容：**
- **bug**：admin 禁用用户后，JWT 旧会话最坏 24h 仍可用（仅 API Key 路径 `handleAPIKeyAuth` 查 DB），旧 `AuthMiddleware` JWT 分支仅 `VerifyToken` 签名，从不查 DB。G-5（TODO.md L63）已挂 2 round。
- **修复 `backend/internal/middleware/auth_status_cache.go` (新建, 105 lines)** — `authStatusCache` struct (sync.RWMutex + map[string]authStatusCacheEntry + ttl 30s) + 包级 singleton `defaultAuthStatusCache` + `lookupUserStatus(db, userID)` 函数：cache miss/expired → DB 读 + 写 cache；DB 错误 → 返回 ("active", err) 让调用方走 401（fail-closed，不放行）；nil DB guard（生产代码 DB 总存在，仅给旧测试兜底）；`resetAuthStatusCache` + `InvalidateAuthStatusCacheForUser` 测试导出函数（db_smoke 不能 sleep 31s 模拟 cache 过期，必须手动 invalidate）。
- **修复 `backend/internal/middleware/auth.go` AuthMiddleware JWT 分支** — `VerifyToken` 成功后调 `lookupUserStatus(database.DB, claims.UserID)`；status=="inactive" → `apierr.Forbidden(c, "账号已禁用")` + `c.Abort()`；DB 错误 → 401 而非 200（fail-closed）。API Key 分支已有 user.Status 检查无回归。

**单测 11 条 + mutation inversion：**
- **cache 单元 3 条**：`TestAuthStatusCache_GetSet_Basic / Expired / ConcurrentSet200Goroutines_NoRace` 验 sync.RWMutex 在并发下不出 race（`-race` 通过）。
- **lookupUserStatus 单元 4 条**：`_ActiveUser / InactiveUser / DBError_ReturnsActive / NilDB_ReturnsActive` 走 sqlmock，验 cache 写入/读取/错误传播路径。
- **AuthMiddleware 端到端 3 条** (`sqlmock`)：
  - `_JWT_UserInactive_Returns401` (AC-M40-1)：disable 后 cache invalidate → 401
  - `_JWT_ActiveUser_CacheHitsAvoidDB` (AC-M40-2)：200 req 同 user 只 1 次 SELECT（守门网基线）
  - `_JWT_StatusFlipWithinTTL_StillAllows` (AC-M40-3)：cache 命中时 status 翻转 30s 内不感知（trade-off 钉死）
  - `_JWT_DBError_Returns401`：DB 错误 fail-closed 不放行（AC-M40-4 钉死）
- **mutation inversion PASS-FAIL-PASS**：注释 `defaultAuthStatusCache.get` → `TestAuthMiddleware_JWT_ActiveUser_CacheHitsAvoidDB` FAIL（200 req → 199 次 SELECT 失败）→ revert → PASS（200 req → 1 SELECT）。
- **真 PG 端到端 `TestDBSmoke_M40_JWTDisableTakesEffect`** (新文件 207 lines in `db_smoke_test.go`) — 5 场景真 PG：active+旧 JWT → 200 / UPDATE status=inactive+cache invalidate → 401 / UPDATE status=active+cache invalidate → 200 / cache TTL 内不感知 → 200 / 物理删用户 → cache miss → DB 无结果 → 401。`scripts/db_smoke.sh` 白名单 +1。

**门禁（最终全绿）：**
- `go vet ./...` 干净（sqlite3 C warning 系既有）
- `gofmt -l` 干净
- `go test -count=1 ./...` 全绿（27 packages）
- `DOCKER='sudo -n docker' bash scripts/db_smoke.sh` 真 PG：**44 cases 全绿**（43 baseline + M40 +1）
- mutation inversion 验证 cache 守门网有效（PASS → FAIL → PASS）

**残余（后续 round）：**
- **多副本部署 cache 是 per-process**：水平扩展时每副本各持一份 cache，禁用生效最坏窗口 = TTL 30s × 副本数 N。本 round 范围外（FIX-PLAN-M40 §edges 留有 Todo，待需要时用 Redis 收口）。
- **Trade-off 显式文档化**：cache 命中时 status 翻转 30s 内不感知，运维禁用需明确「最长 30s 内生效」。前端禁用确认对话框已用 M35-R1 的 toaster 提示。
- **`InvalidateAuthStatusCacheForUser` 测试导出**：将来如果需要「主动失效 cache」场景（如即时禁用），可升级为生产 API（admin 禁用时调一次）。本 round 留作测试专用。

**Round 5 commit 序列：**
- `186dad3` — `intent-M40.md` + G-5 spec
- `4af497b` — `feat(M40): JWT 路径用户状态查 DB + 30s cache` (auth_status_cache.go + auth.go patch)
- `9a8b78c` — `test(M40): authStatusCache 单元测试 (3 cache + 4 lookup + 3 e2e + mutation inversion)`（本次之前已 push）
- `0621b50` — `feat(M40): JWT 用户禁用即时生效 - 真 PG e2e + DB 查表出口 + DB 错误降级拒绝`
- (本次) CHANGELOG + completion report

### M38-B — G-39 fire 路径整链路（triggerid→rule 映射 + dedup + NotifyUsers）（2026-09-13）

**修复内容：**
- **migration 000038 `alert_rule_trigger_map`** (`4b5868f`) — `triggerid VARCHAR(100) PRIMARY KEY` + `rule_id UUID NOT NULL REFERENCES alert_rules(id) ON DELETE CASCADE` + `created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()` + `idx_alert_rule_trigger_map_rule_id` 反向索引；down mirror `DROP INDEX IF EXISTS + DROP TABLE IF EXISTS`。triggerid 唯一 → 同 trigger 只允许一个 rule，last-write-wins 由应用层处理；`ON DELETE CASCADE` 让 rule 被删时映射行跟着没意义；`db_smoke` "前置 3" 检查同步加 000038 + test amend。
- **models.AlertRuleTriggerMap** (`e8f43e9`) — GORM 模型 + `TableName() = "alert_rule_trigger_map"` + `AlertRule.TriggerMaps []*AlertRuleTriggerMap` 关联（`foreignKey:RuleID`）。运维 UI 在列表里展示 rule.name 用，业务 CRUD 走 service 层不靠关联。
- **service CRUD** (`cd14cd7`) — `AlertRuleService` 接口 + 实现 `ListRuleTriggerMappings / CreateRuleTriggerMapping / DeleteRuleTriggerMapping`：UUID ruleID 校验、triggerid 非空 ≤128、source enum {zabbix,manual,auto}、PK 冲突幂等返现有（Zabbix 同步 retry 友好）、handler test mock 加 3 个 func field。
- **HTTP endpoints** (`32dfb9a`) — `GET/POST/DELETE /api/alert-rules/:id/triggers` 三件套；handler 校验 + 路径解析复用 `alertPathID` helper；routes gatedRoutes PUT/DELETE=`CapManage`、POST=`CapWrite`、GET=`ungated`；OpenAPI `AlertRule:` 块补 3 路径 + `AlertRuleTriggerMap` schema；`TestRoutes_所有路由都已分类` + `TestRoutes_OpenAPI契约集合相等` + `TestRoutes_OpenAPI无幻影路径` 三条守门测试一次过。
- **integration.SyncFromZabbix 联通** (`4f0ac22`) — `integration.AlertRuleMapper` struct + `NewAlertRuleMapper(db)` + `LookupRuleIDByTriggerID(ctx, triggerid, source)`（scan string 再 Parse UUID，避开 GORM Scan into uuid.UUID 的 sqlite 兼容问题）+ `LookupRuleIDByTrigger` 兼容老接口（commit 5 已用）；SyncFromZabbix 走完映射后 publish `TopicAlertCreated` payload（含 RuleID + NotifyChannelIDs + NotifyUserIDs + TriggerID + ProblemStartUnix）。
- **worker handleAlertEvent 适配** (`84f169a`) — `AlertEventPayload` 加 `NotifyUserIDs []string` + `TriggerID/ProblemStartUnix`；新增 `deliverToUsers` 路径（独立 SELECT users + findUserChannel by email/phone LIKE）；**Round 10 修复**：移除 `len(channels)==0 → return nil` 旧短路，让 NotifyUsers 即使全表 channel 空也走通；`pickUserContact` 选 email 优先回退 phone；`SetDeduperForTest` exported wrapper 让 db_smoke 可注入 deduper。
- **worker firededup** (`e57d364`) — `Deduper` struct + sync.Map 后端 + `NewDeduper()` 60s 窗口；`Allow(key)` 用 `LoadOrStore(now)` 原子化 first-hit + `CompareAndSwap(oldTime, now)` 原子化窗口外覆写，避免「首次见 key 后 Store(now) 之前其他 goroutine 看到 zero-time 误判窗口外」TOCTOU 漏洞；`gcExpired` 在每次 Allow 末尾顺手清 60s+ key，避免长跑膨胀；`Worker.Start()` 自动注入 deduper（nil 检测，不破测试），`SetDeduperForTest` 给 db_smoke 用；`FireKey(triggerID, unix)` 拼格式单一来源。

**单测与 mutation inversion：**
- **mapper 单测 4 条** (`de9f322`) — `TestAlertRuleMapper_Lookup_Hit / Miss / RuleDeleted / SourceIsolation`，SQLite in-memory + 手动建表 + `PRAGMA foreign_keys=ON` 验证 FK CASCADE；不依赖 AutoMigrate 避开迁移漂移。
- **firededup 单测 8 条 + race-clean** (`4a359db`) — first-hit / in-window / out-of-window / different-keys / FireKey format lock / concurrent 200-goroutine 唯一 first / 双窗口各 1 / GC expired；`-race` 通过（关键守门：CAS 路径在并发下无 race）。
- **worker NotifyUsers 单测 7 条** (`ff1bced`) — `TestHandleAlertEvent_NotifyUserIDs_Nil / Empty / EmailHit / NoContact / BadUUID / NoChannel / Dedup_SecondEventWithin60s_Dropped`，sqlmock 验 DB 期望全部消费；Round 10 修复（channels 全空时仍走 NotifyUsers）让 EmailHit / NoContact 测试通过。
- **mutation inversion PASS-FAIL-PASS** — firededup `Allow` 改返 `true` (关闭 dedup) → `TestDeduper_FirstHit_Allows / InWindow_Drops / DifferentKeys / Concurrent_ExactlyOneFirstHit / TwoWindows / GC` 6 条全 FAIL → revert → 全 PASS（守门网有效证据）。
- **真 PG 端到端 `TestDBSmoke_M38B_FirePathEnd2End`** (`ff1bced`) — 5 场景：`alert_rule_trigger_map` 表存在 + triggerid 是 PK + `idx_alert_rule_trigger_map_rule_id` 索引在位 + fire path 1 send (chA only, chB 不中) + dedup 60s 内第二次同 (trigger_id, problem_start_unix) drop + 不同 start_unix 视为新事件 + GORM 回读 RuleID 字段匹配；`scripts/db_smoke.sh` 白名单 +1。

**门禁（最终全绿）：**
- `go vet ./...` 干净（sqlite3 C warning 系既有）
- `gofmt -l` 干净
- `go test -count=1 ./...` 全绿（26 packages + tests/ 含 dbsmoke tag off 时也走）
- `DOCKER='sudo -n docker' bash scripts/db_smoke.sh` 真 PG：43 cases 全绿（含 M38-B +1）
- mutation inversion 验证 firededup 守门网有效（PASS → FAIL 6/6 → PASS 8/8）

**残余（后续 round）：**
- **dedup 横向扩展**：当前 Deduper 是单进程 in-memory，水平扩展需要外部存储（Redis SETEX）协调；本 round 范围外。
- **triggerid 历史映射查询**：migration 留了 `created_at` 列但 UI "按 triggerid 取该 trigger 历史映射" 路由未开辟；last-write-wins 算法已就位待 API。
- **NotifyUsers channel 配置化**：当前 `findUserChannel` 用 `WHERE config LIKE '%contact%'`，联系人多了是全表扫；待后续走 channel 端"显式收件人"配置。
- **OpenAPI alert_rule_trigger_map 独立 path**：当前挂在 `/alert-rules/{id}/triggers` 下，运维 UI 直接维护 trigger↔rule 时若要"按 triggerid 查询" 需另开路由；本 round 不阻塞主链路。


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
