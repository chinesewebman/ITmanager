# 测试现状报告

**最后更新**: 2026-09-09（本次只增量更新本节与下方「通知渠道配置契约轮」的 rev7 数字；其余章节仍是 2026-06-16 快照）
**HEAD**: `bcb406d`（覆盖率表快照）→ 当前 `main`
**状态**: ✅ 998 backend 测试函数全过（`go test ./... -count=1`，27 个包）+ 176 frontend 测试全过（`npx vitest run`，27 文件）+ `db_smoke.sh` 两条真 PG 路径绿

## 🆕 2026-09-10 增量（M16 工单优先级词表归一，见 `docs/FIX-PLAN-M16-PRIORITY.md`）

| 维度 | 数值 / 说明 |
|---|---|
| Backend 测试函数 | **1031**（`grep -rh "^func Test" --include=*_test.go`；含 **20** 个 `dbsmoke` 标签用例 —— 注意本节以下各 dated 快照里的「12 个」是当时口径，此后已增到 19，本轮 +1 = 20） |
| 新增守门测试 | `internal/integration/glpi_e2e_test.go` 的 `TestGLPIE2E_ConvertToTicket_优先级限定契约词表`（**GLPI 映射的 1/2/3/5/6 号档此前零覆盖**，只喂过 `Priority: 4`）、`internal/service` 的 `TestPriorityFromSeverity_输出限定在契约词表内`、`internal/migrate` 的 `TestLoad_同版本号撞号必须报错`（表驱动含「第一个 up 是 0 字节」+ 两条不误伤）、`tests/db_smoke_test.go` 的 `TestDBSmoke_TicketPriorityNormalize`（存量归一 / high 对照行不动 / 全表无词表外值） |
| 既有断言同步 | `ticket_from_alert_test.go` 两处 `medium`→`normal`；`ticket_service_test.go` 的 `Create_成功_默认值生效` 补 `Priority` 默认断言、`Create_传值保留` 补「已传值不被覆盖」；`rack_ticket_handler_test.go` 的 mock 夹具 `medium`→`normal` |
| dbsmoke 升级路径 | `scripts/db_smoke.sh` 预置两行存量工单（`LEGACY-M16-1` medium / `LEGACY-M16-2` high，插在 users 之后因 `creator_id` 是 FK），新用例已加进**升级路径**那行的 `-run` 白名单；`TestDBSmoke_DownPreservesLegacyColumns` 由九次 Down 改十次（23→21→…→13）并补**首尾正向断言** |
| 变异反证 | **V-1..V-9 九条全红在业务断言上**（脚本 `/tmp/m16_mut.py`，逐条校验「不是编译错」+ 还原后 sha256 逐字节一致）：V-1 去掉 `Create` 的 priority 兜底、V-2 `priorityFromSeverity` 回退 medium、V-3 GLPI 映射回退 medium、V-4 `Load()` 的 up 撞号守卫失效、V-9 `Load()` 的 down 撞号守卫失效、V-5 迁移去掉 WHERE、V-6 迁移删掉 UPDATE、V-7 回滚链多滚一层、V-8 回滚链少滚一层 |
| 真 PG 实测 | `scripts/db_smoke.sh` 全新 + 升级两条路径全绿（含新增用例**确实执行**而非 skip） |
| 附带修复 | `migrate.Load()` 同版本号撞号由**静默覆盖**改为**报错**（up/down 两侧都守；判据用文件名 `upFile`/`downFile`，`upSQL != ""` 兼作标志位会在 0 字节 SQL 文件上漏检）；登记 `docs/TRAPS.md` T-41 |
| 残余（另立任务） | `POST/PUT /tickets` 的 priority **取值**校验（`PUT` 是任意 `map` 直落 `Updates()`，mass-assignment 面更宽）；前端两处同义词字典按设计保留一个版本，且**无测试覆盖**（删掉不会红，靠注释守） |

> 验证口径：`go test ./... -count=1` 全绿、`go vet -tags dbsmoke ./tests/` 干净；frontend `tsc --noEmit` + `eslint` + `vitest run` 干净。

### 续：M3 企微 `wechat` sender 端到端（rev7.1，TODO G-36 关闭）

| 维度 | 数值 / 说明 |
|---|---|
| Backend 测试函数 | **998**（+12，新增 `internal/notification/sender_wechat_test.go`：body **结构断言**、`errcode != 0` 判失败、缺 `errcode` fail-closed 5 例、回执非 JSON、构造校验、非 2xx、连接失败不泄漏 key、非法 URL 不泄漏原串、工厂分派，+ 安全审计 M-1/L-3 的 `回执回显key被抹掉`/`CredentialValues_编码形态与键名过滤`/`ScrubSecrets_无凭据时原样返回`） |
| 复用面 | `dingRespErr` → `errcodeRespErr(raw, label)`（钉钉/企微同口径）；钉钉既有 5 组文案断言**逐字未改**，证明重构零行为变化 |
| 前端 | Settings 14 条用例全绿（替换 2 条 + 新增「不填 URL 不可保存」「保存失败只记状态码与已脱敏文案」2 条）：wechat 表单产出与样本一致 + **保存前**断言无 secret 输入框；存量 `webhook_url` 行 URL 留空待补填。`tsc`/`lint` 干净 |
| 覆盖 | `WeChatSender`（构造/`Type`/`Send`）与 `errcodeRespErr` **100%**；`notification` 包语句覆盖 79.4% |
| 变异反证 | **M8-1..M8-15 十五条全红在断言上**：body 退回 `{"content":…}`、`msgtype` 改 markdown、不校验回执、缺 `errcode` 判成功、工厂去 `wechat`、seed 改回 `webhook`、回显完整 URL、前端删 wechat 分支、前端下拉去选项、前端删 required 规则、样本键名漂移、回执不做值级脱敏、`credentialValues` 不跳空值、前端 `console.error` 记整个 error 对象、`credentialValues` 退回 `u.Query()` 漏 `%20` 形态 |
| 测试设计 trap | 前端「无 secret 输入框」断言必须放**保存前**：保存触发 `resetFields()` → 表单回到「类型未选中」的兜底分支（那里有 secret）→ 保存后断言恒假红。实测踩过一次 |
| 两路只读审计 | 正确性：无 HIGH/MEDIUM，6 条 LOW 全处置 + 观察项 → G-39；安全：无 HIGH，**M-1（企微 key 经 `errmsg` 泄漏）已修**（值级抹除）、L-3（前端 `console.error` 带请求体明文）已修、L-2 并入 G-31、INFO-4/5 登记。详见 `docs/FIX-PLAN-NOTIFY-CHANNEL.md` §7.7 |
| 残余 | 存量 `type=webhook` 企微行（M1 期 seed 产物）需用户改选类型重存；不加启发式迁移（误改第三方 webhook 的风险高于收益）。值级抹除只覆盖**本渠道 URL query 里**的凭据值（变换后回显如 base64 不命中） |

> 验证口径：`go test ./... -count=1` 27 包全绿、`gofmt -l` 干净；frontend `tsc --noEmit` + `eslint --max-warnings 0` 干净。

---

---

## 🆕 2026-09-09 增量（通知渠道配置契约轮 M2：钉钉加签 + 回执校验，TODO G-33）

| 维度 | 数值 / 说明 |
|---|---|
| Backend 测试函数 | **981**（+10；新增 `internal/notification/sender_sign_resp_test.go`：签名向量、端到端加签/不加签、钉钉回执三态、webhook best-effort 9 例、文本卫生、`respBody` 限读） |
| 签名向量 | 由**独立实现**算得（Python `hmac`+`base64`，secret=`SECtest123`、ts=`1700000000000`）→ `w3RMHXzixTMdzr8OHJUmVLS4IoPJVdu+Ut1LE48MePE=`；同时断言**不等于** key/msg 写反的值（`g422EgUWUUtq1vqcbsWy00w6OM8jnLYKr0K4GIfygTQ=`）。不拿被测代码自证 |
| 覆盖 | `sanitizeSnippet`/`respBody`/`dingRespErr`/`webhookRespErr`/`dingTalkSignedURL` **100%**，`DingTalkSender.Send` 94.7%（未覆盖为 `NewRequestWithContext` 出错分支） |
| 变异反证 | **M2-1..M2-8 八条全红在断言上**：key/msg 写反、手拼 query（`+`/`=` 不编码）、钉钉不校验回执、webhook 不校验回执、钉钉回执 fail-open、去 `redact.Text`、按**字节**截断（保持可编译的改法）、无 `sign_secret` 也加签 |
| 新增 trap 候选 | **变异必须可编译**（T-31 复现）：M2-7 首次写成 `s[:200]` → `unicode/utf8` 变未使用 import → 红在编译上；改成 `_ = utf8.RuneCountInString(s)` 后才红在断言（`expected 200 / actual 68`） |
| 新增残余 | **R-12**（webhook best-effort 会把 `{"code":200}` 判失败）、**R-13**（钉钉回执非 JSON 走 fail-closed，网关改写响应体会误判）——见 `docs/FIX-PLAN-NOTIFY-CHANNEL.md` §5 |

> 验证口径：`go test ./... -count=1` 27 包全绿、`go vet` / `gofmt -l` 干净；frontend 本轮未改（27 文件 174 测试）。

### 续：M2 两路审计处置（rev6）

| 维度 | 数值 / 说明 |
|---|---|
| Backend 测试函数 | **986**（+5：钉钉回执缺 `errcode` 5 例表驱动、`errcode` 类型错专用文案、加签时 URL 非法不泄漏、`markFailed` 控制字符、`respBody` 限读改 64KiB + >4KiB 合法回执） |
| 两路独立命中 | 安全 / 正确性两个只读子代理**各自独立**报出同一个 MEDIUM：`dingRespErr` 对「合法 JSON 但无 `errcode`」判成功（`{}`/`null`/`{"errmsg":"ok"}`）→ `ErrCode *int` + 缺键返错 |
| 控制字符 | 新增 `stripControlChars`（口径同 `handlers.sanitizeAuditUsername`），`sanitizeSnippet` + `worker.markFailed`（入库出口）双接：NUL → PG 22021 拒收 → 行永远 pending 无限重发；CR/LF → 行式消费的日志/error_msg 可被伪造 |
| 覆盖 | `stripControlChars`/`sanitizeSnippet`/`respBody`/`dingTalkSignedURL`/`dingRespErr`/`webhookRespErr`/`markFailed`/`DingTalkSender.Send` **100%** |
| 变异反证 | **M7-1..M7-9 九条全红在断言上**（缺 errcode 判成功、类型错文案、webhook 先命中就 return、两处不剥控制字符、限读退回 4KiB、加签退回 `u.Query()`、时间戳取 50s 前、加签分支不剥 `url.Error`）；逐条确认非编译失败 |
| 新增残余 | **R-14**（>64KiB 回执仍截断）、**R-15**（`Send` 需等 body 读完）；`customSenders` 无同步（既有）→ **G-38** |

> 验证口径：`go test ./... -count=1` 27 包全绿 + `-race ./internal/notification/...` 绿、`go vet` / `gofmt -l` 干净；frontend 本轮未改（27 文件 174 测试）。

---

## 🆕 2026-09-09 增量（通知渠道配置契约轮，TODO G-33 M1）

| 维度 | 数值 / 说明 |
|---|---|
| Backend 测试函数 | **971**（+12 相对 2026-09-09 早期快照；`grep -rh "^func Test"`，含 12 个 `dbsmoke` 标签用例）。新增：`channel_service_test.go` 6 个（跨语言样本 V-1、坏配置不落库 V-2、Update 合并校验 V-3、非字符串 config fail-closed V-4、错误文本带原因且脱敏 V-8、**键白名单 H-1**）、`channel_config_contract_test.go` 5 个（Create 六形态不回显 M-1、Update 不回显 M-1、Create 400 文案 V-8、Update 坏 config 返 400、**Update 键白名单 H-1**）、`notification/sender_contract_test.go` 1 个（**样本键集合 ↔ `channelConfig` json tag，契约第三条腿 M-2**）、`cmd/seed` 扩展既有用例（每行可构造 V-6 + rev4 追加**逐类型键集合断言**，钉住可选键 `sign_secret`） |
| Frontend 测试数 | **174**（27 文件；新增 7 个：email/dingtalk/webhook 表单产出与样本 deep-equal（含端口必须是数字）、**连续编辑两条渠道（H-2，rev4 追加 `is_enabled` payload 断言）**、**存量 wechat 行显示可读标签 + 下线提示**、**兜底样本键名可回填（L-1）**、**wechat 下拉项被禁用（L-2）**） |
| 跨语言契约单一来源 | `frontend/src/pages/__fixtures__/channelConfigSamples.json` —— **三条腿**：① 前端表单产出 deep-equal 它；② 后端 `channel_service_test.go` 读同一文件喂 `notification.NewSender`；③ `sender_contract_test.go` 断言样本键集合 == `channelConfig` 的 json tag 集合（路径 `../../../frontend/...`）。缺 ③ 时「表单与样本一起改名」（尤其可选键）两侧都绿（正确性审计 M-2） |
| 变异反证 | **V-9 / V-10 / V-13 / V-14 / V-15 / V-16a 六条红在断言上，V-16b 绿（对照组）**：V-13 `sender.go` 恢复回显 `ch.Type` → service + handler 的不回显用例红；V-14 停用键白名单 → 键白名单用例红（`{"id":…}`/`{"foo":…}` 被放行）；V-15 去掉打开弹窗的 `form.resetFields()` → 连续编辑用例红；V-16a 把 `SignSecret` 的 tag 改成 `sign_secret2` → 契约第三条腿红，而 **V-16b「样本可构造」仍绿**（证明 M-2 缺口真实存在、新断言确实补上了它）；V-9/V-10 在 rev3 后复验仍红。**V-11（删 `redact.Text`）已失效**：400 出口不再有任何调用方可控内容，该变异保持可编译后实测**绿**（`_ = redact.Text` 版本），文档 §4 已写明由 V-13 取代——变异失效本身就是修复生效的证据。**rev4（第三路测试有效性审计）新增 5 条，全红在断言上**：V-18 seed 钉钉行 `sign_secret`→`secret`（rev4 前存活，`NewDingTalkSender` 不要求该键）→ seed 键集合断言红；V-19 前端 `is_enabled` 改回硬编码 `true` → 编辑 payload 断言红；V-20 wechat 下拉项去 `disabled` → 禁用断言红；V-21 兜底样本 `smtp_host`→`smtp` → 回填断言红；V-22 `touched` 初值改 `true` → 「只改name不触发校验」红（该子用例此前不承重）。**两条存活判为结构性、明确接受**：删 `redact.Text`（无向量可造）、`Update` 写回 `effType/effConfig`（顺序执行下行为等价，需 `-race` + 可控交错） |
| 新增 trap | `docs/TRAPS.md` **T-36**（fail-closed 的写法陷阱：`if s, ok := v.(string); ok { 校验 }` 是 fail-open——`Update(map)` 会把 `float64/bool` 静默写成 `"12345.0"`/`"1"`）、**T-37**（校验用的键 ≠ 落库用的键：gorm `Updates(map)` 的 `LookUpField` 把 Go 字段名解析到同一列，`{"Config":…}` 绕过小写键校验照样落库） |
| 为什么加 | UI 表单、seed、OpenAPI、后端 `channelConfig` 四份契约各自手写且无人校验；写入端只校验名称 → 错配要等「告警发不出去」才暴露，钉钉/企微 HTTP 200 + `errcode != 0` 还会把它吞成 success |

> 验证口径：`go test ./... -count=1` 27 包全绿、`go vet` / `gofmt -l` 干净、`npx tsc --noEmit` 0 错、`npm run lint` 0 warning、`npx vitest run` 27 文件 **174** 测试全过。

---

## 🆕 2026-09-09 增量（错误文本脱敏轮，TODO G-28）

| 维度 | 数值 / 说明 |
|---|---|
| Backend 测试函数 | **959**（+20；`grep -rh "^func Test"`，含 12 个 `dbsmoke` 标签用例；第二轮 +3：V-13 `httpx` 两条出错路径、V-14 `SyncAll` 日志出口、V-15 `markFailed` 非法 UTF-8；第三轮 +4：webhook parse 失败路径、NetBox/GLPI 400 回显、resolver 日志、`urlErrCause` depth=4） |
| 新增守门测试 | `internal/redact/redact_test.go`（V-4 `URL` 表驱动 10 例 / V-5 `Text` 12 例 + 6 例不误伤）、`notification_test.go` V-1/V-2（只 bind 不 Accept + 200ms ctx）、V-3（`url.Parse` 失败路径）、V-6（**真 sqlite 写+读回**，脱敏 + rune 截断 + UPDATE 真生效）、V-9（`log.SetOutput` 捕获失败日志）、V-11（`urlErrCause` 剥壳与 nil/超限兜底）、V-12（写库失败必须留痕）、`apierr_test.go` V-7（5xx 内部日志）、`integration_handler_test.go` V-10（真 `IntegrationService`，query token 与 userinfo 两条路径） |
| 关键函数覆盖 | `redact.URL` / `redact.Text` / `urlErrCause` / `markFailed` **各 100%**（语句）；包级：`redact` **100%**、`apierr` **88.5%**、`notification` **73.0%**（第三轮实测）、`httpx` 88.1%、`integration` 84.6%（其余为未改动的 email / tick 路径） |
| 变异反证 | **首轮 9 项 + 第二轮 6 项，全部红在断言上**（首轮 M1 退回裸 `err`、M2 `URL` 返回原串、M3 `Text` 恒等、M4 `markFailed` 去脱敏、M6 `apierr` 去脱敏、M7 `urlErrCause` 恒等、M8 日志去脱敏、M9 去掉 URL 塌缩、M10 退回字节截断；第二轮 M11 `httpx` 三处 return 退回裸 `fmt.Errorf`、M12 去掉 scheme-relative 分支、M13 值类恢复排除 `'`、M14 值类恢复排除 `}` `]` `<` `>`、M15 规则 2 恢复排除 `,`、M16 `ToValidUTF8`→`strings.Clone`）。审查建议的 **M5「先截断后脱敏」经四组构造实测不可观测**，已移除并写明依据（顺序不是安全边界）——见 `docs/FIX-PLAN-ERROR-REDACT.md` §4/§7.2/§9 |
| 新增 trap | `docs/TRAPS.md` **T-34**（凭据藏在错误文本里：`*url.Error` 带完整 URL 走遍四个出口；脱敏必须结构性、顺序不是边界、截断按 rune；**第二轮补**：源头收口优先于出口兜底、正则排除集要按语义最小化、`scheme://` 不是 URL 唯一形状、错误文本可能是非法 UTF-8） |
| 为什么加 | G-16 堵了 SQL 参数，**错误文本**是另一条路：钉钉/飞书/Slack 的 token 在 query/path、集成 URL 可能在 userinfo，失败时 `*url.Error.Error()` 原文连凭据一起落进应用日志、`notification_logs.error_msg`、`gin.DefaultErrorWriter`、HTTP 400 body 四处；`markFailed` 还按字节截断（切断 UTF-8 → PG 22021 拒收 → 行永远 pending 被无限重发）且丢弃 UPDATE 错误（无声无息） |

> **第二轮（审计回执，2026-09-09 晚）**：安全审计 H-1（集成 4 处日志未脱敏 → 改在 `httpx` 出口收口）、M-2/M-3（scheme-relative、`'`/`\`、`https://user:`），正确性审计 P1（值类边界漏尾/整条不匹配）、P4（非法 UTF-8 写库 → PG 22021 → 行永远 pending）全部修复并各配变异；P3（过度脱敏不泄漏）与「值以分隔符开头」的窄形态登记 **G-34/G-35**。处置明细见 `docs/FIX-PLAN-ERROR-REDACT.md` §9。

> **第三轮（测试有效性审计，2026-09-09 深夜）**：审计员在 `/tmp` 隔离快照独立复现 9 项变异**全部红在断言上**、无假绿、无 `NotContains` 空转，同时指出 4 处覆盖空洞并全部闭合：webhook parse 失败 return（HIGH-1）、NetBox/GLPI 400 回显脱敏（HIGH-2）、resolver 日志（MED-1）、`urlErrCause` depth=4 丢 cause（MED-2）。新增变异 U1/U2/U4 单层即红、U3′ 组合红。**一条值得记住的结论**：双层防御下「单层变异不红」不等于用例失效——httpx 的源头收口已经把 URL 塌缩，handler 层的 `redact.Text` 是兜底；要证明用例有效必须做组合变异（见 `docs/TRAPS.md` **T-35**）。
>
> 本轮的构造经验（写进 T-34）：① 失败要**确定性**——`net.Listen` 只 bind 不 Accept + 短 ctx，比 `connection refused` 的文案稳；② 正向断言必须成对（先 `Contains("scheme://host")` 证明脱敏函数真被调用，再 `NotContains(SECRET)`），否则「错误被吞成空串」也会绿；③ 入库断言用**真 sqlite**（sqlmock 断不了 map 更新的参数值）；④ 值类以定界符/串尾为界 —— 测试里密钥后面必须加空格，否则后续汉字会被一并吞掉（过度脱敏，测的就不是截断了）；⑤ **PG 侧的编码行为 sqlite 测不出来**——P4 的「非法 UTF-8 被 22021 拒收」用一次性 `postgres:18-alpine` 容器实测（`convert_from('\x…fffe','UTF8')` → exit 1，替换后 exit 0），sqlite 只用于钉 `utf8.ValidString`。

---

## 🆕 2026-09-09 增量（日志卫生轮，TODO G-16）

| 维度 | 数值 / 说明 |
|---|---|
| Backend 测试函数 | **939**（+11；`grep -c "^func Test"`，含 12 个 `dbsmoke` 标签用例） |
| 新增守门测试 | `internal/database/gorm_logger_test.go`（级别映射表 / 包级默认非零值 / 配置纯函数 / **普通查询与 `Scan` 两条路径都不落参数值** / setter 端到端接线）、`internal/middleware/recovery_test.go`（panic 不 dump 请求头）、`internal/api/middleware_chain_test.go` 的 `TestMiddleware_Recovery不dump请求头`（链路级）、`pkg/logger` 的级别归一化 + 小写 `info` 真过滤、`internal/config` 的 `NPM_LOG_LEVEL` 覆盖 |
| 关键函数覆盖 | `mapGormLogLevel` / `gormLoggerConfig` / `newGormLogger` / `dropRecorderParams` / `SetGormLogLevel` / `Recovery` **各 100%**；包级：`middleware` **91.1%**（+1.9）、`config` **96.9%**、`logger` **72.1%**、`database` **63.8%** |
| 变异反证 | **12 项全红且都红在断言上**：M1 映射默认值→Info、M2 包级默认去初始化、M3 `ParameterizedQueries`→false、M4 删 `RecorderParamsFilter`（Scan 路径实测打出 `password_hash = "$2a$10$…"`）、M5 `Colorful`→true、M6 `routes.go` 换回 `gin.Recovery()`（实测 dump 出 `Cookie: auth_token=eyJ…`）、M7 去 `ToUpper`、M8 删 `SetDefault`、M9 recovery 记 Cookie、M10 手写 recover→`gin.CustomRecovery`、M11 setter 改空操作、M12 `shouldLog` 去掉未知级别兜底 |
| 审计后修正 | 三视角审计另发现 3 处并已修：`pkg/logger` 未知级别 fail-open（现按 INFO）、`gin.New()` 后丢掉最外层 Recovery（Logger/Recovery 移到链首）、测试覆写包级 `RecorderParamsFilter` 不复位。详见 `docs/FIX-PLAN-LOG-HYGIENE.md` §8.4 |
| 新增 trap | `docs/TRAPS.md` **T-32**（一个 `log.level`、两条互不相干的日志路径，看着生效实则两条都没接上）、**T-33**（库级钩子绕过：设了 `ParameterizedQueries` 参数照样落日志） |
| 为什么加 | 原先「容器日志里有完整 bcrypt 哈希 + 可重放的 JWT」：gorm 级别硬编码 `Info` 且参数全展开、`pkg/logger` 级别过滤因大小写不匹配而失效、gin `Recovery` 在 debug 模式 dump 整个 Cookie。单测走 sqlmock/内存库，不看 logger 输出，所以全绿 |

> 本轮测试的两条硬要求（写进了 T-31/T-33）：① 断言前先 `require.Contains` 证明日志**真的被打印**，否则「不含敏感值」在日志被关掉时也成立；② 变异必须**红在断言上**，M4/M6 的价值在于红的时候把泄漏原文打了出来。

---

## 🆕 2026-09-09 增量（schema 漂移修复轮）

| 维度 | 数值 / 说明 |
|---|---|
| Backend 测试函数 | **928**（`grep -c "^func Test"`；含 12 个 `dbsmoke` 标签用例） |
| 包级语句覆盖 | `internal/middleware` **89.2%**、`internal/service` **83.3%**、`internal/integration` **80.8%**、`internal/migrate` **74.6%**、`internal/models` **43.4%**（models 多为纯结构体/标签，无逻辑可覆盖） |
| 关键函数覆盖 | `SyncFromNetBox` **94.4%**、`SyncFromGLPI` **85.2%**、`SyncFromZabbix` **82.8%**、`assetService.Update` **84.6%** |
| 口径说明 | Go **没有分支覆盖工具**（`-covermode` 只有 set/count/atomic，全是语句/块级）。「分支覆盖 ≥80%」的验收意图改由「新增函数语句覆盖 100% + 关键分支表驱动显式枚举」落实 |
| 新增守门测试 | `tests/schema_drift_test.go`（纯解析，无需 DB，永远跑）、`tests/db_smoke_test.go`（build tag `dbsmoke`，需真 Postgres） |
| 真库冒烟 | `scripts/db_smoke.sh` —— 起临时 PG 容器跑两条路径：① 空库 `migrate.Up` 建库 + 核心链路 + 类型往返 + 唯一约束 + 迁移重放 + jsonb 列默认值 + NetBox upsert（唯一索引形态 / 重复拒绝 / 多 NULL / 真同步端到端 / 人工列不被覆盖 / 脏数据挡住迁移的负循环）；② 存量库（000001~000012，含预置的存量资产/用户）只应用 000013 之后的迁移 + role 回填 + jsonb 回填 + 回滚不丢旧列。CI 已加 `dbsmoke` job（postgres service，脚本走 `SMOKE_PG_HOST` 外部模式） |
| 为什么加 | 单测走 sqlite/mock，与生产 `migrate.Up` 路径不同 —— 曾出现「psql 跑得通、生产执行器必炸」的假绿；也只有真库能发现类型转换（`inet→varchar` 带 `/32`、TEXT 上二次 JSON 编码）与 role 回填失效 |
| 假绿防线（G-22 轮） | ① **前置缺失 → `Fatalf`**：`MigrateRunner` 断言 `schema_migrations` 行数 == `embed` 内 `*.up.sql` 数、且本轮关键版本已记录（删迁移文件即红，不再是「全 SKIP + EXIT=0」）；② 断言**引用生产变量**（`netboxUpdateCols`）而非复制字面量；③ 变异反证 15 项全红，且要求**红在断言上而不是编译上**（见 `docs/TRAPS.md` T-31） |

> 手工跑真库冒烟：`scripts/db_smoke.sh`（需 docker + 本地 postgres 镜像），或 `SMOKE_PG_HOST=127.0.0.1 SMOKE_PG_PORT=5432 scripts/db_smoke.sh`（复用已有 PG，需本机 psql）。

---

## 📊 总览

| 维度 | 数值 | 备注 |
|---|---|---|
| Backend 测试数 | **401** | 20 packages, `-race` 验证无 race |
| Frontend 测试数 | **53** | 8 files, vitest 1.6.1 |
| Backend 覆盖率 | **61.2%** | 超过 `开发计划.md` Phase 6 目标 (60%) |
| Frontend 覆盖率 | 未测 | vitest 配 `--coverage` 未启用 |
| 平均测试密度 | ~6.5 tests/pkg | service/apikey 100% 覆盖 |
| Race-free | ✅ | 102/102 race tests pass |

---

## 🏆 Backend 覆盖率 (按 package 排序)

| Package | Coverage | Tests | 备注 |
|---|---|---:|---|
| `internal/apikey` | **100.0%** | 17 | hash + pepper + prefix |
| `internal/config` | **94.3%** | 30 | Viper + validate |
| `internal/metrics` | **90.2%** | 9 | prometheus + handler |
| `internal/apierr` | **88.5%** | 19 | Conflict/TooManyItems/All 错误码 |
| `internal/httpx` | **87.8%** | 4 | 熔断器 + 重试 + context |
| `internal/api` | **85.5%** | 49+28+21 | handler + middleware chain + routes |
| `internal/cmd/seed` | **73.7%** | 12 | seedData 全链路 |
| `internal/migrate` | **72.9%** | 7+10 | migrate runner + cmd/migrate |
| `internal/cmd/admin-bootstrap` | **66.1%** | 17 | env 校验 + runWithDeps |
| `internal/pkg/logger` | **58.9%** | 7 | zap + structured |
| `internal/cmd/migrate` | **54.3%** | 10 | runWithDeps + migrateReset |
| `internal/service` | **43.4%** | 28+5+17=~50 | mock-based, 多 service 补测 |
| `internal/integration` | **39.5%** | 13 | E2E with mock server |
| `internal/middleware` | **36.5%** | 10 | CORS + auth + metrics |
| `internal/models` | **36.1%** | 13 | User + Ticket BeforeCreate hook |
| **TOTAL** | **61.2%** | **401** | - |

---

## 🖥️ Frontend 覆盖率

| File | Tests | 覆盖 |
|---|---:|---|
| `pages/Login.test.tsx` | 4 | ✅ |
| `pages/Dashboard.test.tsx` | 2 | ✅ |
| `pages/Assets.test.tsx` | 2 | ✅ |
| `pages/Racks.test.tsx` | 2 | ✅ |
| `pages/Alerts.test.tsx` | 2 | ✅ |
| `pages/Tickets.test.tsx` | 2 | ✅ |
| `hooks/useApiQuery.test.ts` | 8 | ✅ |
| `stores/index.test.ts` | 31 | ✅ (8 zustand store) |
| **TOTAL** | **53** | 8 files |

---

## 🐛 已修复的 Bug（按 commit）

### `97a5c46` — handlers 8 bug
1. **FailedLogin race** (auth_handler) — `gorm.Expr("failed_login+1")` 原子 + re-fetch
2. **弱密码** — 加 8 字符+字母数字校验
3. **改回旧密码** — `CompareHashAndPassword(new)` 拦截
4. **rate_limit 越界** — `[1, 100000]` 校验
5. **IP whitelist 越界** — `net.ParseIP/CIDR` 校验
6. **uuid parse 静默** — 返 401 不吞
7. **type=garbage 接受** — switch 严格
8. **Name 重名** — UNIQUE index + 409 Conflict

### `6c047d8` — httpx 2 bug
9. **half-open race** — atomic CAS 移入 Mutex
10. **ctx 取消触发熔断** — 4xx/cancel 不记 failure

### `5830d8d` — service 17 bug
11. #13 `AlertFilter.Severity` string → int
12. #14 `Limit=0` 默认 100
13. #15 #24 #25 `Update/UpdateRule` 重复 First/Get
14. #17 `BulkDelete/Ack/Resolve` 1000 上限 + `ErrTooManyItems`
15. #18 `idToIndex` 死代码
16. #19 `usedMap` 键匹配
17. #26 `User.List()` 加分页
18. #27 #29 Dashboard 单条 SQL 聚合 + Machines/Networks 区分

### `bcb406d` — cmd + 前端 + 中间件 + hooks
19. **admin role 检测** — `Scan` → `Row().Scan` (cmd/admin-bootstrap)
20. **testdata schema 漂移** — sites/racks 字段名跟 models 对齐
21. **asset_networks.ipv_address** → `ipv6_address`

---

## 🚀 跑测试

```bash
# 后端
cd backend
go test ./... -count=1                    # 401 tests
go test -race ./... -count=1              # race 验证
go test -cover ./... -coverprofile=cov.out  # 覆盖率
go tool cover -html=cov.out -o cov.html   # HTML 报告

# 前端
cd frontend
./node_modules/.bin/vitest run            # 53 tests
./node_modules/.bin/vitest run --coverage # 覆盖率（待配置）

# 预提交（已配 pre-commit hook）
git commit -m "..."                       # 自动跑 gofmt + swagger validate + .bak 拦截
```

---

## 📋 CI 现状

`.github/workflows/ci.yml` 当前只跑：
- backend: `go vet` + `go test` + `go build`
- 不跑 `-race` / `frontend` / 覆盖率

**待办**:
- 加 `go test -race` 步骤
- 加 frontend vitest 步骤
- 加 coverage badge

---

## 🔜 下一步方向

1. **migrate to pg_test**: integration e2e 用真 PG test 替 sqlite (提升真实性)
2. **frontend coverage 启用**: vitest 配 `@vitest/coverage-v8`
3. **service 43% → 70%**: 增 alert/rack/ticket handler-level coverage
4. **CI 加速**: cache go modules + parallel jobs
5. **mutation testing**: go-mutants 验证测试质量
