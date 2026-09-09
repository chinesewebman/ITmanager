# FIX-PLAN-NOTIFY-CHANNEL：通知渠道「配置契约」错位 + 钉钉加签缺失 + 业务失败被当成功（TODO G-33）

- **状态**：rev6 — M1 已实现并过三路审计收口（§7.2/§7.3）；**M2（钉钉加签 + 回执校验）已实现并过两路审计收口**，见 §7.4（交付）与 §7.5（审计处置）。当前 **986 backend 测试函数 / 174 frontend 测试全绿**；变异 V-9/V-10/V-13/V-14/V-15/V-16a/V-18..V-22 + M2-1..M2-8 + M7-1..M7-9 共二十八条红在断言上、V-16b 绿为对照组、V-11 已失效见 §4
- **关联**：G-33（seed 企微键名 / 钉钉 `SignSecret` 未生效）、G-28（错误文本脱敏）、G-31（回显点残余）、G-36（企微 sender）、G-37（OpenAPI 渠道路径漂移）
- **发现路径**：G-28 第三轮审计后盘点「配置 → Sender」链路时发现
- **rev2 变更**：见 §7 处置表。阻塞项 2 个（B-1 表单端口类型、B-2 变异设计错误）、HIGH 5 个（H-1 脱敏点、H-2 `Update` fail-open、H-3 既有测试、H-4 seed 邮件行、H-5 400 文案）全部落入 §2/§3/§4。
- **rev3 变更**：实现后三路只读审计（安全 / 正确性 / 测试有效性）回执，处置见 §7.2。两个审计独立命中同一个 HIGH（`Update` 键名绕过，真 PG 实测 HTTP 200 落库），另有前端 Modal 串记录（H-2）、契约只钉必填键（M-2）、400 body 回显 Type 的非 URL 形态（安全 M-1）等；本 rev 的代码/测试改动即 §7.2 的处置结果。
- **rev6 变更**：M2 交付后的**两路只读审计**（安全 / 正确性，各自独立设计变异）回执处置见 §7.5。两路**独立命中同一个 MEDIUM**——`dingRespErr` 对「合法 JSON 但无 `errcode`」（`{}`/`null`/`{"errmsg":"ok"}`）判成功，与「无法确认送达即失败」自相矛盾；另修 `sanitizeSnippet`/`markFailed` 的控制字符（NUL 让 PG 22021 拒收 → 行永远 pending 无限重发）、webhook 回执「先命中就 return」、回执限读 4KiB 截断合法 JSON、`errcode` 类型错报成「不是合法 JSON」、`u.Query()` 静默丢畸形 query 参数，并把 `InDelta` 从 60s 收窄到 10s。新增残余 R-14/R-15。
- **rev5 变更**：M2 交付（§7.4）。钉钉加签按消歧义公式实现并用**独立实现算得的签名向量**钉住（key/msg 写反 → 恒定 310000 的失败形态被 M2-1 变异覆盖）；钉钉回执 `errcode != 0` 与「回执非 JSON」都算失败（fail-closed）；通用 webhook 做 best-effort `errcode`/`code` 校验（不认识形状时维持只看 HTTP 状态）；回执文本限读 4KiB + 脱敏 + 按 rune 截到 200。新增残余 R-12/R-13。
- **rev4 变更**：第三路（测试有效性）审计回执的处置见 §7.3。核心结论：**代码无新缺陷，缺的是测试承重力**——seed 钉钉行 `sign_secret` 改名无测试钉住（H-1）、前端 `is_enabled` 保留逻辑与 wechat 禁用项、兜底样本键名、`touched` 跳过语义均存在「变异存活」；本轮逐条补断言并重跑变异（V-18..V-22）。M-2/M-3 两条存活判为结构性、明确接受（见 §7.3）。

## 1. 问题（What / Why）

### 1.1 现象（全部有代码证据）

**① 前端表单写出的键名与后端 `channelConfig` 完全不一致 → UI 建的渠道必然发送失败**

| 前端写的键（`frontend/src/pages/Settings.tsx`） | 后端认的键（`backend/internal/notification/sender.go:98-117`） |
|---|---|
| `config.smtp`（:778） | `smtp_host` |
| `config.port`（:781） | `smtp_port` |
| `config.username`（:784） | `smtp_user` |
| `config.password`（:787） | `smtp_password` |
| `config.webhook`（:794） | `url`（webhook 类型）/ `webhook_url`（dingtalk 类型） |
| —— 表单没有 `from` / `to` 字段 | email 构造器要求 `from` 非空、`to` 至少一项（`sender.go:192-197`） |

`handleSaveChannel` 把表单对象 `JSON.stringify` 后发（`Settings.tsx:388`），后端 `ChannelService.Create` 只校验 `Name != ""`（`channel_service.go:51-62`）→ **坏配置照存**，直到 worker 发送时才失败：email 报 `smtp_host/port/user/from are required`、webhook 报 `url is required`。`Test` 按钮（`channel_service.go:87-97`）同理。

**② 前端下拉提供 `wechat`，后端不支持该类型**

`Settings.tsx:764` 的选项含 `{ label: '企业微信', value: 'wechat' }`；`NewSender` 的 switch 只有 `dingtalk`/`email`/`webhook`（`sender.go:69-81`）→ `unsupported channel type: wechat`。前端 mock 兜底数据（`Settings.tsx:40-41`）也用 `type: 'wechat'` + `config.webhook`。

**③ 钉钉 `sign_secret` 解析后从未使用 → 开了加签的机器人永远发不出去**

`channelConfig.SignSecret`（`sender.go:106`）全仓**只有这一处**出现（`grep -rn SignSecret internal/ cmd/` 仅命中定义行）；`DingTalkSender.Send` 直接用 `d.cfg.WebhookURL`（`sender.go:164`）。钉钉开启「加签」后，未带 `timestamp`/`sign` 的请求返回 **HTTP 200 + `{"errcode":310000,"errmsg":"sign not match"}`** —— 因为 ③，这一失败还被 ④ 吞掉。

**④ 业务失败被当成功（HTTP 200 但 `errcode != 0`）**

`DingTalkSender.Send`（`sender.go:175-177`）与 `WebhookSender.Send`（`sender.go:322-324`）只判 `resp.StatusCode/100 != 2`，**不读响应体**。钉钉/企微都以 200 + `errcode` 表达业务失败（钉钉 `310000` 签名错、`300001` 限流；企微 `40058` 参数错）→ 通知静默丢失，`notification_logs.status` 记 `success`。

**⑤ seed 的坏行（`cmd/seed/main.go:245-248`）**

| 行 | 现状 | 缺陷 |
|---|---|---|
| 邮件通知 | `{"smtp_host":…,"smtp_port":587,"smtp_user":…,"from":…}`，`IsEnabled: true, IsDefault: true` | **缺 `to`** → `NewEmailSender` 必拒（`sender.go:195-197`）。默认启用 + 默认渠道，是**活着的坏行** |
| 钉钉群通知 | `{"webhook_url":…,"secret":""}` | 键名应为 `sign_secret`（`sender.go:106`） |
| 企业微信通知 | `type:"webhook"` + `{"webhook_url":…}`，`IsEnabled: false` | 键名应为 `url`（`sender.go:290`）；且企微 body 形状是 `{"msgtype":"text","text":{"content":…}}`，而 `WebhookSender` 发 `{"content":…}`（`sender.go:306`）→ 即使键名修好也发不出去（M3） |

seed 用 `db.Create` **直写 DB**，绕过 `ChannelService` 的校验——M1 的写入端防线覆盖不到它，必须另有 seed 侧断言（§4 V-6）。

**⑥ 编辑渠道会把 `is_enabled` 强制打开**

`Settings.tsx:389` 的 payload 硬编码 `is_enabled: true`；表单 `initialValues`（`Settings.tsx:742-750`）只有 `name`/`type`/`config`。列表页有独立开关（`Settings.tsx:462-467`）→ 编辑一个已停用的渠道并保存，会把它静默启用。

**⑦ OpenAPI 与实现矛盾：`config` 声明为 `object`，实现只吃字符串**

`backend/internal/api/openapi.yaml:2433-2434`（`NotificationChannel`）与 `:2448-2449`（`ChannelInput`）都写 `config: type: object`；但 `models.NotificationChannel.Config` 是 `string`（`models/alert.go:109`）→ `Create` 传对象时 `ShouldBindJSON` 直接失败返 400「请求参数错误」，`Update` 传对象时 gorm 报 `unsupported type map[string]interface{}` 返 500。**对象形态从来没有工作过**，文档是错的。

### 1.2 根因

**没有「配置契约」这个唯一事实来源**：后端用 `channelConfig` 的 JSON tag 定义契约，前端表单、seed、OpenAPI 各自手写，四份互不知情；写入端（`ChannelService.Create/Update`）只校验名称，**不校验配置能否构造出 Sender** → 错配要到「告警真的发不出去」才暴露，且（因 ④）有时连报错都没有。

## 2. 方案（How）

### 2.1 候选对比

| 候选 | 做法 | 取舍 |
|---|---|---|
| A 只改前端键名 | 表单字段对齐后端 tag | 最小，但后端仍无防线，下次任何写入端（seed/脚本/第三方）再错配还是静默 |
| B 后端兼容旧键（别名读取） | `channelConfig` 接受 `webhook`/`webhook_url`/`url` 三选一 | 存量坏行自动恢复；**但正是别名让本次错配长期不暴露**——同一个 bug 会再发生一次 |
| C 写入端校验 + 修前端 + 单一契约 | `Create/Update` 用 `notification.NewSender` 试构造，失败 → 400；前端表单、seed、OpenAPI 对齐后端 tag；契约样本文件跨语言共用 | 错配在**保存时**就暴露；无别名、契约唯一 |

### 2.2 定案：C（写入端 fail-fast + 各端对齐），分三期

#### M1（本轮）配置契约对齐

**1. service 侧新增校验 helper（`channel_service.go`）**

```go
// validateChannelConfig 用 notification.NewSender 试构造（构造器不发网络 I/O），
// 失败原因经 redact.Text 后随 ErrInvalidInput 返回。
// 脱敏必须在**这里**（源头收口）：handler 的 400 body 会原样回显 err.Error()，
// 而 apierr.BadRequest 的 internalErr 恒为 nil（apierr.go:52-55），
// Respond 只在 status>=500 时才脱敏（apierr.go:37-45）→ 400 出口没有任何兜底。
func validateChannelConfig(chType, cfg string) error {
	_, err := notification.NewSender(&models.NotificationChannel{Type: chType, Config: cfg})
	if err != nil {
		return fmt.Errorf("%w: %s", ErrInvalidInput, redact.Text(err.Error()))
	}
	return nil
}
```

**校验范围只做「必填存在性」**，不做 URL 语法/scheme 白名单（`NewWebhookSender` 只判 `cfg.URL == ""`、`NewDingTalkSender` 只判 `webhook_url != ""`，都不解析 URL）。因此 `{"url":"not a url"}`、`{"url":"ftp://x"}`、开了加签但 `sign_secret:""` 都能通过保存、到发送时才失败——**这是已知边界，文档写明**（见 §2.3、R-4）。空 `Config` 是 fail-closed：`parseConfig("")` 返 nil error，但三个构造器随后都会拒空配置。

**2. `Create`：名称与配置两处校验，错误都带原因**

```go
if ch == nil || ch.Name == "" {
	return fmt.Errorf("%w: 渠道名称不能为空", ErrInvalidInput)
}
if err := validateChannelConfig(ch.Type, ch.Config); err != nil {
	return err
}
```

**3. `Update`：合并后的 (type, config) 校验，非字符串一律 fail-closed**

规则（逐条对应审查 H-2/M-2）：

| updates 含 | 行为 |
|---|---|
| 键不在白名单（`name/type/config/is_enabled/is_default`） | `ErrInvalidInput`，**先归一化到小写再判**。键名是调用方可控字符串且 gorm 的 `Updates(map)` 走 `Schema.LookUpField`（先列名再 Go 字段名）→ 只精确匹配小写键会让 `{"Config": …}` / `{"Type": …}` 绕过校验照样写列、`{"id": …}` 改主键（安全审计 H-1 / 正确性审计 H-1，真 PG 实测修复前 HTTP 200） |
| 不含 `type` 也不含 `config` | 跳过校验（只改名称/开关）——**唯一**跳过路径 |
| 含 `config`，值为 `string` | 用「新 config + 生效 type」校验 |
| 含 `config`，值**非** `string`（对象/数组/数字/布尔/null） | `ErrInvalidInput`（fail-closed）。**不得**写成 `if s, ok := v.(string); ok { 校验 }`——那是 fail-open：实测 `float64(12345)` → 落库 `config="12345.0"`、`true` → `config="1"`，坏配置静默入库 |
| 含 `type`，值为 `string` | 用「生效 type + **已存** config」校验（`ch` 已在 `:66-72` 读出）→ 拦住「只改 type 不改 config」造出 `type=dingtalk` + 存量 `{"url":…}` |
| 含 `type`，值非 `string` | `ErrInvalidInput` |

生效值 = `updates` 有则取之，无则取已存行的 `ch.Type` / `ch.Config`。校验通过后把 `effType`/`effConfig` **一并写回 `updates`**（写入值 == 校验值）：否则两个并发局部更新各自读到旧行、交错写出的组合谁都没校验过（正确性审计 M-3）。

**4. handler 侧（`channel_handler.go`）两处分支**

- `CreateChannel`（`:43-45`）：把硬编码的 `apierr.BadRequest(c, "渠道名称不能为空")` 改为 `apierr.BadRequest(c, err.Error())`——否则构造器原因被吞掉，与 §2.2 自相矛盾。
- `UpdateChannel`（`:53-69`）：**当前没有 `ErrInvalidInput` 分支** → 新增校验错误会落到 `apierr.Internal` 返 500。必须补 `if errors.Is(err, service.ErrInvalidInput) { apierr.BadRequest(c, err.Error()); return }`。
- 两处的安全性依赖「service 已 `redact.Text`」这一前提（§4 V-7 有断言钉住）。

**5. 前端 `Settings.tsx` 表单**

| 类型 | 字段（`Form.Item name`） | 控件 |
|---|---|---|
| email | `['config','smtp_host']` / `['config','smtp_port']` / `['config','smtp_user']` / `['config','smtp_password']` / `['config','from']` / `['config','to']` | `Input` / **`InputNumber`** / `Input` / `Input.Password` / `Input` / `Select mode="tags"`（→ 数组） |
| dingtalk | `['config','webhook_url']` / `['config','sign_secret']` | `Input` / `Input.Password` |
| webhook | `['config','url']` / `['config','secret']` | `Input` / `Input.Password` |

- **`smtp_port` 必须用 `InputNumber`**（B-1）：`<Input type="number">` 的 value 是**字符串**，`JSON.stringify` 后是 `"smtp_port":"587"`，后端 `SMTPPort int` 反序列化失败 → M1 上线后 UI 自己保存的邮件渠道会被 400 挡住；且手抄样本的 V-1 会假绿。
- 下拉去掉 `wechat` 可选性（保留一条 `disabled: true` 的「企业微信（暂不支持）」，否则存量行显示裸值 `wechat`）、`channelColumns` 的 `typeMap` 保留 `wechat` 显示映射（存量行仍要能显示）、mock 兜底数据（`:40-41`）改成正确键名。
- 编辑存量 `wechat` 行时渲染下线提示 `Alert`，**不**按 Webhook 渲染（键名对不上、必填项永远过不了，用户只会看到英文裸路径报错——正确性审计 M-1）。
- payload（`:382-390`）改为 `is_enabled: channelModal.data ? channelModal.data.is_enabled : true`，不再硬编码 `true`。
- **弹窗打开时 `form.resetFields()`**（正确性审计 H-2）：`Form.initialValues` 只在挂载时应用一次，而 `resetFields()` 会重置到**当前** `initialValues`（prop 每次渲染都更新）。缺这一步，连续编辑两条渠道会把上一条的 name/type/config 写进当前记录（后端校验**拦不住**：写进去的组合自洽）。

**6. seed（`cmd/seed/main.go:245-248`）**

- 邮件行补 `"to": ["ops@company.com"]`；
- 钉钉行 `"secret": ""` → `"sign_secret": ""`；
- 企微行 `"webhook_url"` → `"url"`（键名对齐；**body 形状仍不兼容，M3 修**，行保持 `IsEnabled: false`）。

**7. 契约样本单一来源（跨语言）**

新增 `frontend/src/pages/__fixtures__/channelConfigSamples.json`，三类型各一份「表单真产出」的 config 对象（`smtp_port` 是**数字**）：

```json
{
  "email":    {"smtp_host":"smtp.example.com","smtp_port":587,"smtp_user":"nmp@example.com","smtp_password":"pw","from":"nmp@example.com","to":["ops@example.com"]},
  "dingtalk": {"webhook_url":"https://oapi.dingtalk.com/robot/send?access_token=xxx","sign_secret":"SECxxxx"},
  "webhook":  {"url":"https://example.com/hook","secret":"s"}
}
```

- 前端测试：填表 → 抓 `createChannel` 的 payload → `JSON.parse(payload.config)` 与样本 **deep-equal**；
- 后端测试 A（service）：读同一文件 → 逐类型 `NewSender` 必须成功；
- 后端测试 B（notification，rev3 补）：样本**键集合** ↔ `channelConfig` 的 json tag 集合逐类型 `ElementsMatch`。缺这条时表单与样本「一起改名」（尤其可选键 `sign_secret`/`secret`/`smtp_password`）前两条都绿、契约已破（正确性审计 M-2）。

三条腿合起来才闭环：前端表单 ↔ 样本（A 的前端侧）、样本 ↔ 构造器（后端 A）、样本 ↔ tag（后端 B）。文件缺失必须**测试失败**，不得 skip。

**8. OpenAPI（`backend/internal/api/openapi.yaml`）**

- `NotificationChannel.config`（:2433）与 `ChannelInput.config`（:2448）由 `type: object` 改为 `type: string`，描述注明「JSON 字符串，键名见 `notification.channelConfig`」。理由见 §1.1 ⑦：对象形态从未工作过（Create 400 / Update 500）。
- rev3：响应模型 `type` 的 enum 加回 `wechat`（存量行会返回它，响应侧必须能表达——正确性审计 M-4）；输入模型 `type` 收窄为 `[email, dingtalk, webhook]`；`ChannelInput` 补 `is_enabled`（前端一直在发，生成类型里却没有——正确性审计 L-5）。
- 已知遗留：本文件里渠道路径写作 `/notification/channels`，真实路由与前端都是 `/notification-channels`（`routes.go:363`），`/test` 动词也不一致 → 登记 **G-37**（文档-only，不在本轮改）。

**9. 构造器错误不回显调用方字符串（`notification/sender.go`）**

`NewSender` 的 default 分支原为 `fmt.Errorf("unsupported channel type: %s", ch.Type)`，该文本经 service → handler 原样进 400 body。`redact.Text` 只挡 URL / 键值形态，裸 token、JWT、percent 编码、无 scheme URL、多行文本都能穿过（安全审计 M-1）。改为静态文案 `unsupported channel type (支持: email/dingtalk/webhook)`——诊断信息（支持哪几种）不损失，调用方字符串一律不回显。**代价**：拼错的类型名不再出现在错误里（前端下拉本来就限制了取值），登记 R-9。

由此 400 出口**不再有任何调用方可控内容**（`parseConfig` 的 JSON 错误只含 offset/字段名；三个构造器只做存在性检查，URL 解析发生在 `Send` 且那里已用 `redact.URL`/`urlErrCause`）。`redact.Text` 保留为纵深防御，其变异因此不再能红（见 V-11 的说明）。

#### M2（已实现，见 §7.4）钉钉加签 + 响应回执校验

- **加签公式（消歧义写法）**：`key = sign_secret`，`msg = timestamp + "\n" + sign_secret`，`sig = base64(HMAC-SHA256(key, msg))`，`sign = url.QueryEscape(sig)`。**不要**写成 `HMAC-SHA256(secret, timestamp+"\n"+secret)` 这种 key/msg 顺序歧义的记法（写反 → 恒定 `310000`）。
- 追加 query 用 `url.Values` + `Encode()`（与钉钉 `quote_plus` 等价），**不手拼 `&timestamp=`**（webhook_url 无 query 时首参必须是 `?`）；键序无关。
- **签名 URL 是限时凭据**：只放局部变量，不回写 `d.cfg.WebhookURL`、不落 `notification_logs.recipient`、不裸打日志。
- **`sign_secret` 永不进错误文本**（L-1）：实测 `redact.Text("sign failed for secret SS")` 原样返回，散文里的裸凭据挡不住 → 约定 + 一条断言。
- 钉钉：解析 `{"errcode":N,"errmsg":"…"}`，`N != 0` → 返回错误（含 `errcode` + 截断后的 `errmsg`）。
- 通用 webhook：**best-effort** —— 响应体是合法 JSON 且含数值型 `errcode`/`code` 且非 0 时视为失败；否则维持「只看 HTTP 状态」（不假定第三方形状）。
- 读体 `io.LimitReader(resp.Body, 4<<10)`（限**读取量**）；进错误文本前再 `redact.Text` + 截到固定 rune 上界（建议 200）——`LimitReader` 不限制嵌入文本长度。上游响应体一律视为不可信（G-31 残余）。

#### M3（再下一轮）企微渠道端到端

新增 `wechat` sender（body `{"msgtype":"text","text":{"content":…}}`，校验 `errcode`）；`NewSender` 注册；seed 企微行改 `type: "wechat"`；前端下拉加回 `wechat`（登记 G-36）。

### 2.3 不做的事（登记）

- **不做别名兼容**（候选 B）：见 R-2。
- **不做存量坏行的数据迁移**：配置是管理员可编辑的，重新保存一次即可（§4.1 给逐行步骤）。
- **不做 URL 语法/scheme 校验**：M1 只校验必填存在性（§2.2 第 1 条）。若后续要收紧，登记为独立条目。
- **不实现企微 sender**：M3。

## 3. Where（变更清单）

| 文件 | 动作 |
|---|---|
| `backend/internal/service/channel_service.go` | 新增 `validateChannelConfig`；`Create` 名称+配置校验；`Update` 键归一化+白名单、合并校验、非字符串 fail-closed、写入值==校验值（M1） |
| `backend/internal/service/channel_service_test.go` | V-1/V-2/V-3/V-4/V-8 + 键白名单（rev3）；**改 2 个既有必红用例**（见下） |
| `backend/internal/api/handlers/channel_handler.go` | `CreateChannel` 回显 `err.Error()`；`UpdateChannel` 补 `ErrInvalidInput` → 400（M1） |
| `backend/internal/api/handlers/channel_config_contract_test.go` | **新建**（rev2 表里误记为 `ticket_channel_dashboard_user_handler_test.go`，rev3 更正）：真 service + 手写 DDL sqlite，400 文案 / 400 body 不回显 / 键白名单（M1） |
| `backend/internal/notification/sender_contract_test.go` | **新建**（rev3）：样本键集合 ↔ `channelConfig` json tag（M-2 的第三条腿） |
| `frontend/src/types/index.ts` | 手写 `NotificationChannel.config` `Record<string,any>` → `string`（与生成类型对齐，L-3） |
| `frontend/src/pages/Settings.tsx` | 表单键名对齐 + `InputNumber` + email 补 `from`/`to` + 下拉去 `wechat` + mock 数据 + `is_enabled` 保留（M1） |
| `frontend/src/pages/Settings.test.tsx` | 渠道 payload 与样本 deep-equal（M1） |
| `frontend/src/pages/__fixtures__/channelConfigSamples.json` | **新建**，跨语言契约样本（M1） |
| `backend/cmd/seed/main.go` | 邮件行补 `to`、钉钉行 `sign_secret`、企微行 `url`（M1） |
| `backend/cmd/seed/main_test.go` | `TestSeed_空DB_创建通知渠道` 增加「每行都能构造 Sender」断言（M1） |
| `backend/internal/api/openapi.yaml` | `config` 两处 `object` → `string`（M1） |
| `backend/internal/notification/sender.go` | 未知类型错误不回显 `ch.Type`（rev3 / 安全 M-1）；加签 + 回执校验（M2）；`wechat` sender（M3） |

**既有测试同步（M1 必做，否则红）**：

| 用例 | 现状 | 处理 |
|---|---|---|
| `channel_service_test.go:122-137` `Create_成功` | `Type:"dingtalk", Config:"{}"` | 改用合法 config（如 `webhook` + `{"url":"https://example.com/hook"}`） |
| `channel_service_test.go:146-160` `Create_唯一冲突…` | `{Name:"dup"}`，`Type` 为空 | 补合法 `Type`+`Config`，否则新校验在 INSERT **之前**返 `ErrInvalidInput`，sqlmock 期望全落空（也说明「校验早于落库」是承重顺序） |
| `TestChannelHandler_CreateChannel_空name返400` | 只断言 400 状态 | 保持绿（文案未断言）；新增文案断言另写 |
| `TestSeed_空DB_创建通知渠道` | 只断言 3 行 + 类型集合 | 追加构造断言 |

## 4. 验证清单

| # | 验证点 | 方式 |
|---|---|---|
| V-1 | **跨语言契约**：`__fixtures__/channelConfigSamples.json` 三类型逐条喂 `notification.NewSender` 必须成功 | 后端单测 |
| V-2 | 坏配置**不落库**：email 缺 `to` / webhook 缺 `url` / 未知类型 `wechat` → `ErrInvalidInput`，且 DB **无新增行** | 单测（**真 sqlite**，见下） |
| V-3 | 合法配置落库；`Update` 只改 `is_enabled`（不含 `config`/`type`）不触发校验；只改 `type` 时用「新 type + 已存 config」校验并拒绝 | 单测 |
| V-4 | `Update` 的 `config` 为对象/数组/数字/布尔/null → `ErrInvalidInput`（fail-closed），DB 值不变 | 单测 |
| V-5 | 前端单测：三类型 payload 的 `config` 与样本 **deep-equal**（含 `smtp_port` 为数字） | `npx vitest run src/pages/Settings.test.tsx` |
| V-6 | seed 每行 config 都能构造 Sender（含企微行键名 `url`） | `cmd/seed` 单测 |
| V-7 | **HTTP 400 body 不含调用方可控字符串**：`Create` 六形态（URL / 裸 token / JWT / percent 编码 / 无 scheme / 多行）逐条 + `Update` 一条，断言 body 含 `unsupported channel type`、不含各自片段 | handler 单测 |
| V-8 | 合法/非法两方向的 400 文案断言：`Create` 空名称 → body 含 `渠道名称不能为空`；`Update` 坏 config → body 含构造器原因 | handler 单测 |
| V-9 | **变异反证 A**：把 `validateChannelConfig` 里的 `NewSender` 调用换成 no-op（`var err error`）→ V-2 必红 | 变异 |
| V-10 | **变异反证 B**：前端 `smtp_port` 改回 `<Input type="number">` → V-5 必红（样本是数字 587） | 变异 |
| V-11 | **变异反证 C（已被取代）**：rev2 的「删 `redact.Text` → V-7 必红」在 rev3 后**不再能红**——400 出口已无调用方可控内容，`redact.Text` 退化为纵深防御。由 V-13 取代 | 变异 |
| V-12 | 存量坏行修复步骤：见 §4.1（按类型列出「改什么、怎么验」），不再只是「重新保存一次」 | 手工 |
| V-13 | **变异反证 D**：把 `sender.go` 的 default 分支改回 `fmt.Errorf("unsupported channel type: %s", ch.Type)` → V-7（service + handler 两侧）必红 | 变异 |
| V-14 | **变异反证 E**：删掉 `Update` 的键归一化+白名单（直接 `updates` 落库）→ 键白名单用例（service + handler）必红 | 变异 |
| V-15 | **变异反证 F**：删掉前端打开弹窗时的 `form.resetFields()` → H-2 用例（连续编辑两条渠道）必红 | 变异 |
| V-16 | **变异反证 G**：把 `channelConfig.SignSecret` 的 json tag 改成 `sign_secret2` → 契约第三条腿（V-17）必红，而「样本可构造」仍绿（证明新断言补上了 M-2 的缺口） | 变异 |
| V-17 | 契约第三条腿：样本键集合 ↔ `channelConfig` json tag 逐类型 `ElementsMatch`，且清单里的键本身必须是真实 tag | notification 单测 |
| V-18 | **变异反证 H**：seed 钉钉行 `sign_secret` → `secret` → V-6（键集合断言）必红（rev4 前存活） | 变异 |
| V-19 | **变异反证 I**：前端 `is_enabled` 改回硬编码 `true` → 「连续编辑两条渠道」的 payload 断言必红（rev4 前存活） | 变异 |
| V-20 | **变异反证 J**：wechat 下拉项去掉 `disabled: true` → 「wechat 下拉项被禁用」必红（rev4 前存活） | 变异 |
| V-21 | **变异反证 K**：兜底样本 `smtp_host` → `smtp` → 「兜底样本键名可回填」必红（rev4 前存活） | 变异 |
| V-22 | **变异反证 L**：`Update` 的 `touched` 初值改 `true`（恒校验）→ 「只改name不触发校验」必红（rev4 前该子用例不承重） | 变异 |
| V-23 | 签名公式与 query 形态：独立向量（Python `hmac` 算得，非被测代码自证）逐项断言 `timestamp`/`sign`，并断言**不等于** key/msg 写反的值；无 query 时首参是 `?`；已有 query 时原参数保留；base64 的 `+`/`=` 被 percent 编码 | notification 单测 |
| V-24 | 加签请求端到端：httptest 捕获真实请求的 query，断言原 `access_token` 保留、`timestamp` 是当前毫秒、`sign` 用回执 timestamp 独立重算一致；未配 `sign_secret` 时**不加签** | notification 单测 |
| V-25 | 钉钉回执校验：`errcode=310000` → 错误含 errcode 与 errmsg；`errcode=0` → 成功；**回执非 JSON → fail-closed** | notification 单测 |
| V-26 | 通用 webhook best-effort：`errcode`/`code` 非 0 → 失败；`0` / 非数值 / 非 JSON / 空体 → 成功（9 例表驱动） | notification 单测 |
| V-27 | 回执文本卫生：`sanitizeSnippet` 按 **rune** 截到 200（不切多字节字符）、URL 凭据被脱敏、非法 UTF-8 被清理；`respBody` 限读 4KiB | notification 单测 |
| V-28 | **M2 变异 M2-1..M2-8 八条全红在断言上**（详见 §7.4）：key/msg 写反、手拼 query、两处去掉回执校验、钉钉回执 fail-open、去 `redact.Text`、按字节截断、无 secret 也加签 | 变异 |

**§4.1 存量坏行修复步骤（V-12，照做即可）**

修复前后端只拦新写入，已落库的坏行不会自己变好。逐行操作（系统设置 → 通知设置 → 该行「编辑」→ 按新键名重填 → 保存）：

| 行 | 要改什么 | 保存后怎么验 |
|---|---|---|
| 邮件（`type=email`） | 补齐 `to`（至少一个收件人）；确认 `smtp_port` 是**数字**、`from` 非空 | 点该行「测试」，成功即通 |
| 钉钉（`type=dingtalk`） | `secret` → `sign_secret`；`webhook_url` 保持 | 同上（加签尚未实现，M2 前留空即可） |
| 存量 `type=wechat` / `type=webhook` 但配置是企微形状 | 改选「Webhook」并填 `url`；或先删行（企微渠道要等 M3） | 保存被 400 挡住时看提示文案（已带原因） |
| 其它 `type` 不在 `[email,dingtalk,webhook]` 的行 | 改选受支持类型并重填配置 | 同上 |

无法逐行操作时（如批量环境）可对照同一张表直接改 DB，但改完必须点一次「测试」——**构造成功不等于能发出去**（R-4）。

**测试基建注意（来自测试审查）**：

- V-2/V-3/V-4 需要**真 sqlite**，不能只用 sqlmock：
  - `models.NotificationChannel.ID` 带 `gorm:"default:gen_random_uuid()"`（`models/alert.go:104`）→ sqlite 上 `AutoMigrate` 必炸，**手写 `CREATE TABLE`**（照 `cmd/seed/main_test.go:56+` 的既有模式）；
  - sqlmock 下「未落库」是**恒真的空断言**（没有 INSERT 期望 = 无期望可违反）→ 必须真 sqlite + `Count` + **正控**（同一 fixture 的合法配置确实插进去 1 行）。
- V-9 的变异必须**红在断言上**，不能红在编译上：去掉调用会产生未使用变量/import → 用 `var err error` 这类可编译改法。
- 不 mock 被测代码自身；`NewSender` 固定走真实构造器（**不得**用 `Resolver`——service 包测试里从不注册 mock，二者等价，用 `Resolver` 做变异会存活，见 §7 B-2）。

## 5. Risk

- **R-1（错配换了个地方藏）**：后端校验只在写入时跑，**存量坏行**仍是坏的且不会被发现；管理员只看到「告警发不出去」。缓解：`Test` 按钮的错误信息（`channel_service.go:92-96`）会带构造器原因，UI 上直接可见；§4 V-12 给「重新保存一次」的步骤。
- **R-2（别名兼容的诱惑）**：若为存量行加别名读取，本次这类错配**下次仍会静默通过**（别名把键名错配变成不可见）。缓解：明确不做别名，用「写入端 fail-fast + 跨语言样本」两处强制同步。
- **R-3（跨语言契约仍会漂移）**：单侧手抄样本会假绿（T-31）。缓解：样本文件**两侧共读**（表单 ↔ 样本 ↔ 构造器 ↔ tag 三条腿，§2.2 第 7 条）。rev3 补上第三条腿前，可选键（`sign_secret`/`secret`/`smtp_password`）两侧同时改名仍会全绿——现已由 V-17 钉住。
- **R-4（校验范围被误读为「配置可用」）**：构造成功 ≠ 能发出去（URL 不解析、`sign_secret` 可空、SMTP 密码可空）。若文档/UI 暗示「保存成功就能发」，运维会误判。缓解：§2.2 第 1 条与 §2.3 写明边界；M2 加签后仍需人工填 `sign_secret`。
- **R-5（`Update` fail-closed 的兼容性）**：现存客户端若按 OpenAPI 传 `config` 对象，今天会 500（`Update`）/400（`Create`），M1 后变 400 带原因——**行为改善不是回归**；OpenAPI 同步改为 `string`。若确有第三方依赖对象形态，需另立条目（无证据）。
- **R-6（400 文案回显内部原因）**：渠道路由整组 `canManage` + `RejectAPIKeyAuth`（`routes.go:362-365`），400 body 只对能读到 config 明文的管理员可见；且原因在 service 侧已 `redact.Text`。缓解：V-7 断言钉住脱敏；若日后放开路由权限，需重审。
- **R-7（seed 直写 DB 绕过校验）**：M1 的写入端防线覆盖不到 seed。缓解：V-6 在 seed 侧独立断言每行可构造；seed 未来若改用 service，此断言可退化为冗余。
- **R-8（`Create/Update` 引入 `notification` 依赖）**：`channel_service.go` 已 import `notification`（`Test` 用 `Resolver`），无新依赖、无 import 环；校验固定用 `NewSender`（不走 mock 注册表）。
- **R-9（未知类型的诊断信息变少）**：不回显 `ch.Type` 后，拼错的类型名不再出现在 400 文案里（只剩「支持: email/dingtalk/webhook」）。缓解：前端下拉限制了取值；`Test` 链路的失败仍带类型上下文（那条路径不经 400 body）。若日后确需回显，必须走「白名单字符 + 固定长度」而不是原样拼接。
- **R-10（键白名单的兼容性）**：`Update` 现在对白名单外的键返 400（此前是静默更新或 500）。已知调用方（前端 `Settings.tsx`、`handleToggleChannel`）只发 `name/type/config/is_enabled`；若第三方依赖改 `id`/`created_at`，那本来就该拦（会造出悬空外键，本仓库无外键约束）。无证据表明存在此类调用方。
- **R-11（`Update` 仍是读-校验-写）**：rev3 让「写入值 == 校验值」，但两个并发请求仍可能互相覆盖（last-writer-wins）。彻底消除需要事务 + 行锁（`clause.Locking{Strength:"UPDATE"}`，PG 限定）。当前渠道更新是低频管理操作，接受该窗口；若将来渠道配置改由自动化高频写入，再上事务。
- **R-12（best-effort 回执校验可能误判既有 webhook）**：通用 webhook 现在把 `{"code":200}` / `{"errcode":200}` 这类「非 0 但其实是成功」的形状判为失败——那是第三方协议里真实存在的写法。失败模式：某条今天能用的 webhook 在 M2 上线后开始被记成 `failed`（并触发重发）。缓解：① 只对**合法 JSON 且含数值型 `errcode`/`code`** 判定，纯文本（Slack 的 `ok`）、`{"success":true}`、字符串型 code 一律放行；② 失败文案带原始回执片段，`notification_logs.error_msg` 里一眼能看出是误判；③ 钉钉走严格路径（钉钉协议固定 `errcode`），不受此风险影响。若线上出现误判，处置是「该渠道改用钉钉/邮件」或后续给 webhook 加开关（不在本轮）。
- **R-13（钉钉回执不可用的 fail-closed 边界）**：钉钉链路把「2xx 但回执**不是合法 JSON**、或**缺 `errcode`**（`{}`/`null`/`{"errmsg":"ok"}`）」一律判为失败（rev6 收紧，原先只挡「非 JSON」，见 §7.5 MED-1）。失败模式：运维在钉钉机器人前挂了一层网关，网关对成功请求返回 200 + 空体/HTML（如 204 改写）→ 消息其实到了群里，但被记成 `failed` 并重发（重复告警）。缓解：钉钉官方 webhook 恒返 `{"errcode":N,…}`；若确有此形态，属网关改造问题，应让网关透传上游回执。文档在此显式登记，不做「空体也算成功」的妥协——那会把真正丢消息的情况也吞掉。
- **R-14（>64KiB 回执仍被截断）**：`respBody` 的读取上界 rev6 由 4KiB 提到 64KiB（修「截断合法 JSON → 真送达判失败」），但上界仍然存在。失败模式：回执超过 64KiB（网关包装了整个请求上下文）时截断 → 钉钉侧「合法回执」变「不是合法 JSON」判 failed 并重发，webhook 侧反之吞掉一个本该报的错。缓解：钉钉/企微回执都是几百字节量级；截断阈值有注释说明职责（只挡无限流）。若真遇到，处置是让网关透传而非再抬上界。
- **R-15（`Send` 现在要等响应体读完）**：M2 之前 2xx 后立即返回；现在要读到 64KiB 或 EOF 才能校验回执。失败模式：对端发完 200 头后挂住不写 body → `Send` 阻塞到 `http.Client.Timeout`（10s），worker 侧 30s ctx 先到则由 ctx 兜底。缓解：有界、且 worker 是串行批处理 + 每渠道独立 ctx；已实测（正确性审计 LOW-5）400ms ctx 下按 ctx 返回。

## 6. 分期与状态

| 期 | 内容 | 状态 |
|---|---|---|
| M1 | 配置契约对齐（后端写入校验 + 前端表单 + seed + OpenAPI + 跨语言样本） | 已实现 + 三路审计收口（rev4），待推送验证 CI |
| M2 | 钉钉加签 + 响应回执校验 | **已实现**（§7.4），待推送验证 CI |
| M3 | 企微渠道（`wechat` sender + seed + 前端） | 登记 G-36 |

## 7. 审查记录与处置（rev1 → rev2 → rev3）

### 7.1 实现前审查（rev1 → rev2）

三路只读审查（正确性 / 安全 / 测试与前端），全部结论已回源码复核。

| 编号 | 来源 | 结论 | 处置 |
|---|---|---|---|
| **B-1** | 正确性 + 测试前端 | `Settings.tsx:782` 用 `<Input type="number">` → `smtp_port` 是字符串，`SMTPPort int` 反序列化失败；M1 会挡住 UI 自己保存的邮件渠道，V-1 手抄样本还会假绿 | §2.2 第 5 条改 `InputNumber`；V-5 用共享样本 deep-equal；V-10 变异反证 |
| **B-2** | 正确性 + 测试前端 | §4 原 V-4「校验改用 `Resolver` → V-2 红」不成立：service 包测试零 `RegisterSender`，`Resolver` ≡ `NewSender`，变异存活；且无导出注销函数会 flaky | 改为 V-9「`NewSender` 调用换 no-op」；§4 明确禁止用 `Resolver` 做变异 |
| **H-1** | 安全 | 400 分支既不过 `redact.Text` 也不落日志（`apierr.go:37-45,52-55`）；§3 漏 `Create` 分支，照仓库既有写法 `BadRequest(c, err.Error())` 会绕过脱敏 | §2.2 第 1/4 条：脱敏钉在 service helper（源头收口），两处 handler 回显 `err.Error()`；V-7 断言 400 body 无凭据；V-11 变异反证 |
| **H-2** | 安全 | `Update` 的 `map` 路径非字符串 `config` 会 fail-open：实测 `float64(12345)` → `config="12345.0"`、`true` → `"1"` 静默落库 | §2.2 第 3 条 fail-closed；V-4 覆盖对象/数组/数字/布尔/null |
| **H-3** | 三路 | M1 会打红既有测试，变更清单未登记 | §3 增「既有测试同步」表（2 处必红 + 2 处扩展） |
| **H-4** | 正确性 + 安全 | seed 邮件行缺 `to`，且 `IsEnabled: true, IsDefault: true` 是活着的坏行；seed 直写 DB 绕过校验 | §2.2 第 6 条补 `to`；V-6 seed 侧构造断言；R-7 |
| **H-5** | 正确性 + 安全 | `Create` 的 `ErrInvalidInput` 文案硬编码「渠道名称不能为空」，会吞掉构造器原因；`Update` 无 `ErrInvalidInput` 分支 → 新错误返 500 | §2.2 第 4 条两处分支 |
| **H-6** | 测试前端 | sqlmock 下「未落库」是空断言；`AutoMigrate` 在 sqlite 因 `gen_random_uuid()` 必炸 | §4 测试基建：真 sqlite + 手写 DDL + `Count` + 正控 |
| **H-7** | 正确性 + 安全 | `Update` 只改 `type` 不改 `config` 可绕过校验，造出坏组合 | §2.2 第 3 条「合并后的 (type, config)」；V-3 |
| **H-8** | 安全 | OpenAPI 声明 `config: object` 与实现矛盾 | §1.1 ⑦ + §2.2 第 8 条改 `string`；R-5 |
| **M-1** | 安全 | 「构造成功 ≠ 配置可用」：构造器不解析 URL、`sign_secret`/SMTP 密码可空 | §2.2 第 1 条 + §2.3 写明校验范围；R-4 |
| **M-2** | 正确性 + 安全 | `is_enabled` 硬编码 `true`，编辑已停用渠道会静默启用 | §2.2 第 5 条 |
| **M-3** | 安全 | M2 回执：`LimitReader` 不限制嵌入文本长度；裸凭据不被 `redact.Text` 拦 | §2.2 M2：截断 rune 上界 200 + 指向 G-31 |
| **M-4** | 安全 | 加签公式记法歧义（key/msg 写反 → 恒定 `310000`）；`&timestamp=` 手拼首参错误 | §2.2 M2 改消歧义写法 + `url.Values` |
| **M-5** | 安全 | 签名后的 URL 是限时凭据 | §2.2 M2：只放局部变量，不回写 cfg / 不落 recipient / 不裸打日志 |
| **L-1** | 安全 | `redact.Text` 挡不住散文里的裸 secret | §2.2 M2 约定 + 断言 |
| **L-2** | 测试前端 | V-3 原设计「用坏配置行做种子」不充分 | 改为 V-3 真 sqlite 读回 DB 值断言 |
| **L-3** | 安全 | 前端 `smtp_port` 字符串（同 B-1） | 同 B-1 |

**已核实无问题（不必再动）**：四个构造器 + `parseConfig` 的错误文本当前不含 URL/凭据值（真实包实测 10 组坏配置）；空 `Config` 与未知/空 `Type` 均 fail-closed；`Test` 链路已过脱敏（`channel_handler.go:85` → `apierr.Internal` 500 → `apierr.go:43`）；生产 gorm logger 恒参数化（`gorm_logger.go:55,70-73`），`map` 值展开的 SQL 不落日志。

### 7.2 实现后审计（rev2 → rev3）

三路只读审计（安全 / 正确性 / 测试有效性）对 M1 实现做复审。安全与正确性两路**独立命中同一个 HIGH**（键名绕过），两路都在 `/tmp` 副本里跑真 PG / 真 sqlite 复现，未改动工作树。

| 编号 | 来源 | 结论（含证据） | 处置 |
|---|---|---|---|
| **H-1** | 安全 + 正确性 | `Update` 的 fail-closed 建在「精确匹配小写键」上，而 gorm `Updates(map)` 对每个键走 `Schema.LookUpField`（先列名再 Go 字段名，均大小写敏感）→ `{"Config":12345}` 落库 `"12345.0"`、`{"Type":"dingtalk"}` 落库坏组合、`{"id":…}` 改主键；**真 PG 18 实测修复前 HTTP 200**，小写键正控被拦 | §2.2 第 3 条：键归一化到小写 + 白名单（`name/type/config/is_enabled/is_default`），未知键 → 400；service + handler 各一组用例（含 `{"Config":"{}"}`、`{"Type":"dingtalk"}`、`{"Config":true}` 三条 400 且 DB 不变）+ 大写键正控；V-14 变异 |
| **H-2** | 正确性 | 渠道 Modal 未销毁/未重灌：连续编辑两条渠道时表单显示**上一条**的 name/type/config 并把它写进当前记录（写进去的组合自洽，后端校验拦不住）。jsdom 实测第二次编辑仍显示「渠道A」 | §2.2 第 5 条：打开弹窗时 `form.resetFields()`（重置到当前 `initialValues`）；前端新增「连续编辑两条渠道」用例 + V-15 变异 |
| **M-1** | 安全 | 400 body 脱敏只覆盖 URL / 键值形态：`sender.go` 的 `unsupported channel type: %s` 回显调用方可控 `Type`，裸 token / JWT / percent 编码 / 无 scheme / 多行全部穿过 `redact.Text` | §2.2 第 9 条：default 分支改静态文案，六形态用例钉住（service + handler）+ V-13 变异 |
| **M-2** | 正确性 | 契约只钉住必填键：表单与样本「一起改名」（尤其可选键 `sign_secret`/`secret`/`smtp_password`）时前端 deep-equal 与后端「样本可构造」双绿，契约已破 | §2.2 第 7 条第三条腿：样本键集合 ↔ `channelConfig` json tag（`sender_contract_test.go`）+ V-16 变异 |
| **M-3** | 正确性 | `Update` 读-校验-写三段无事务：并发交错可写出「A 的 type + B 的 config」（两请求都返 200） | §2.2 第 3 条：校验通过后把 `effType`/`effConfig` 写回 `updates`（写入值 == 校验值）；残余窗口见 R-11 |
| **M-4** | 正确性 | 响应模型 `type` 的 enum 去掉了 `wechat`，但存量行仍会返回它 | §2.2 第 8 条：响应 enum 加回 `wechat`，输入 enum 收窄 |
| **M-5** | 正确性 | §4 V-12「存量坏行修复步骤在文档中可照做」——文档里没有步骤 | §4.1 新增逐行操作表 |
| **M-1(c)** | 正确性 | 存量 `wechat` 行保存时**不会**被后端 400 挡住（请求发不出）：类型框显示裸值、按 Webhook 渲染必填项，用户只看到英文裸路径 | §2.2 第 5 条：下拉保留禁用项显示「企业微信（暂不支持）」+ 编辑时渲染下线提示；前端用例 |
| **L-1** | 正确性 | `ErrInvalidInput` 哨兵前缀 `invalid input: ` 会出现在用户可见文案里（全仓既有形态，非本轮引入） | 不在本轮改（改共享哨兵会波及 10+ handler 文案）→ 并入 **G-31** 登记 |
| **L-2** | 正确性 | §3 变更清单把 handler 测试记成了 `ticket_channel_dashboard_user_handler_test.go` | §3 更正为 `channel_config_contract_test.go` |
| **L-3** | 正确性 | 第三份手写类型 `frontend/src/types/index.ts:113-118` 仍是 `config: Record<string,any>` + `wechat` | 已改为 `config: string` 并注明 `wechat` 仅存量 |
| **L-4** | 正确性 | 设计文档 `network-monitor-design.md:3280` 的邮件键名注释仍是 `username`/`password` | 已改为 `smtp_user`/`smtp_password` |
| **L-5** | 正确性 | `ChannelInput` 缺 `is_enabled`（前端一直在发） | §2.2 第 8 条：补字段 + 重新生成 `api.types.ts` |
| **L-1/L-2/L-3（安全）** | 安全 | 无请求体大小限制；手写类型漂移；`Update` 可改 `id`/`created_at` | 前两项随 H-1/M-1 一并消除（400 body 定长、类型对齐）；`id`/`created_at` 由键白名单挡住 |
| **G-37** | 本轮发现 | OpenAPI 渠道路径写作 `/notification/channels`，真实路由是 `/notification-channels`（`routes.go:363`），`/test` 动词也不一致 | 文档-only，登记 G-37，不在本轮改 |

**安全审计已核实无问题**：Create 侧 fail-closed 完整（结构体绑定 + JSON 大小写不敏感解码，无同类绕过）；越权面未变（整组 `canManage` + `RejectAPIKeyAuth`）；400 文本无新增日志 / DB 出口；config 里的口令/密钥无任何路径进错误文本（18 组坏配置实测）；契约样本无真实凭据。

**正确性审计已核实无问题**：测试基线为真（副本里 27 包全 `ok`、968 测试函数、前端 27 文件 170 测试、`gofmt`/`go vet`/`tsc`/`lint` 全干净）；`api.types.ts` 与 OpenAPI 无漂移（重新生成 byte-identical）；V-9/V-10/V-11 独立复跑全红在断言上；`Create` 的 fail-fast 顺序（校验早于 INSERT）；handler 错误映射完备；前端表单键名逐键对齐；`notification_channels` 只有 `ChannelService` 与 `cmd/seed` 两个写入点。

**一条耦合提示（非缺陷，记录备查）**：`channel_service_test.go` 读 `../../../frontend/...` 的样本，跨出 Go module。CI（`ci.yml:30-39`，`working-directory: backend` 但 checkout 全仓）与 `make test` 都成立，只在「只挂载 backend 目录」的容器里会红——这是 §2.2 第 7 条刻意选择的单源方案的代价。

### 7.3 第三路（测试有效性）审计处置（rev3 → rev4）

第三路审计以 20:15 快照 + 全文件内容比对为基准（代码文件与仓库**逐字节一致**），在 `/tmp/audit2` 副本里自设计 **20 条后端 + 9 条前端**变异逐条实测。结论：**代码无新缺陷**，`validateChannelConfig` 100% / `Create` 88.9% / `Update` 93.8% 覆盖、未覆盖的只有 DB 错误出口等既有分支；但 4 处「变异存活」= 断言不承重，另有 3 条结构性存活。

| 编号 | 结论（含证据） | 处置 |
|---|---|---|
| **H-1** | seed 钉钉行 `sign_secret` 无测试钉住：`NewDingTalkSender` 只要求 `webhook_url` 非空，把键名改成 `secret` 仍能构造成功 → 变异 S17 存活（`cmd/seed/main_test.go` 只断言 `NewSender` 成功） | `TestSeed_空DB_创建通知渠道` 增加逐类型**键集合断言**（email 5 键 / dingtalk `webhook_url`+`sign_secret` / webhook `url`）；V-18 变异 |
| **M-1** | 前端 `is_enabled` 保留逻辑无断言：「连续编辑」用例只看表单显示值，改回硬编码 `true` 全绿（变异 F3 存活）→ 编辑已停用渠道会静默启用、真发告警 | 用例 fixture 的渠道 B 改 `is_enabled: false`，保存后断言 `updateChannel` payload 的 `is_enabled === false`；V-19 变异 |
| **M-2** | `redact.Text` 在 `validateChannelConfig` 中已不可验证（变异 S6 存活）——唯一能造出「带凭据错误文本」的向量已随安全 M-1 移除 | **接受**（rev3 §4 V-11 已自曝）。保留为纵深防御 + 代码注释约定；残余风险「将来构造器把值拼进错误文本」由 §2.2 第 9 条约定与评审清单兜 |
| **M-3** | `Update` 写回 `effType/effConfig` 无测试（变异 S10 存活）：顺序执行下写回与不写回行为等价，任何非并发测试都区分不了 | **接受**（结构性）。需 `-race` + 可控交错，成本高于收益；残余窗口已在 R-11 记录 |
| **L-1** | 前端兜底样本（`listChannels` 失败分支）无测试，改回坏键全绿（变异 F7 存活） | 新增「兜底样本键名可回填」用例（点开兜底行编辑，断言 `SMTP服务器` 回填值）；V-21 变异 |
| **L-2** | wechat 下拉项的 `disabled: true` 无测试（变异 F4b 存活）→ UI 会重新可选 wechat（后端 400 挡住，非静默失败） | 新增「wechat 下拉项被禁用」用例（断言 `ant-select-item-option-disabled`）；V-20 变异 |
| **L-3** | 「只改name不触发校验」子用例不承重：种子行合法，恒校验也通过（变异 `touched=true` 存活于该子用例） | 种子改为一**存量坏行**（`type=email, config={}`），name-only 更新必须成功；V-22 变异 |
| **L-4** | 契约第三条腿的残余共因：样本+表单+前端测试+`allowedConfigKeys` **四处同步**改 `sign_secret`→`secret` 仍全绿（`secret` 是 webhook 段的合法 tag，清单校验过不了语义） | **接受**（可达性低：需改到测试文件本身）。语义级校验会引入第二份手写映射，与「最小改动」冲突 |
| **L-5** | 前端用例耦合 antd 内部细节（`.ant-modal`、`ant-select-item-option-disabled`、`"保 存"` CJK 空格）；email 用例 2.5–4.8s vs `testTimeout: 20000` | **接受**（连跑稳定）。antd 升级时按整体假红处理；本轮新用例沿用同一耦合风格 |
| **L-6** | G-33 describe 未重置 `apiKeyApi.list`，继承上一 describe 的 `mockRejectedValue`（stderr 噪声 + 顺序耦合，无断言影响） | describe 的 `beforeEach` 补 `apiKeyApi.list` 的 `mockResolvedValue` |
| **L-7** | `Update` 的 DB 写失败路径未覆盖（`channel_service.go:144-146`） | **不修**（既有形态、非 M1 新分支），登记备查 |

**审计已核实无问题（承重力正证）**：S1–S5、S7–S9、S11–S19、F1/F2/F4/F5/F6/F8/F9 全部**红在断言上**（无红在编译/超时/空断言）；`assert.Zero` 有正控且正控在「跳过 INSERT」变异下**红**（S20），证明「坏配置不落库」不是恒真；service/handler 测试用**真 sqlite**（非 sqlmock），`-count=10` 无偶发；`NotContains` 均与 `Contains` 成对；不 mock 被测代码（handler 契约用真 service + 真 sqlite，前端只 mock `services/api` 与 `antd.message`）；fixture 缺失即失败（不 skip）。

**本轮复跑（rev4）**：`python3 /tmp/mutate4.py` 五条全部按预期红（S17 / L-3 / F3 / F4b / F7），并逐条确认红在断言上（`expected true to be false`、`toHaveValue(smtp.example.com)`、`to contain 'ant-select-item-option-disabled'`）。全量：backend 27 包 `ok` / 971 测试函数 / `gofmt`+`go vet` 干净；frontend 27 文件 **174** 测试 / `tsc` 0 错 / `lint` 0 warning。

### 7.4 M2 交付：钉钉加签 + 回执校验（rev5）

代码（`backend/internal/notification/sender.go`）：

| 改动 | 要点 |
|---|---|
| `dingTalkSignedURL(webhookURL, signSecret, tsMillis)` | 消歧义公式 `key=sign_secret`、`msg=timestamp+"\n"+sign_secret`、`sign=base64(HMAC-SHA256(key,msg))`；用 `url.Values.Encode()` 追加 `timestamp`/`sign`（等价钉钉 `quote_plus`，不手拼 `&timestamp=`）；ts 作为参数传入 → 纯函数可测 |
| `DingTalkSender.Send` | `sign_secret` 非空才加签；签名 URL 只放局部变量 `target`，错误文本仍用 `redact.URL(d.cfg.WebhookURL)`（不回显签名）；2xx 后调 `dingRespErr` |
| `dingRespErr` | `errcode != 0` → 错误（含 errcode + 脱敏截断后的 errmsg）；**回执非 JSON → fail-closed**（R-13） |
| `webhookRespErr` | best-effort：仅「合法 JSON + 数值型 `errcode`/`code` + 非 0」判失败，其余形状维持只看 HTTP 状态（R-12） |
| `sanitizeSnippet` / `respBody` | 回执限读 4KiB → `redact.Text` → `ToValidUTF8` → 按 **rune** 截到 200（口径同 `worker.markFailed`） |

验证与反证：

- 新增 `sender_sign_resp_test.go`（10 个测试函数）：V-23 签名向量（**独立实现**算得 `w3RMHXzixTMdzr8OHJUmVLS4IoPJVdu+Ut1LE48MePE=`，并断言 ≠ key/msg 写反的 `g422EgUWUUtq1vqcbsWy00w6OM8jnLYKr0K4GIfygTQ=`）、V-24 端到端加签/不加签、V-25 钉钉回执三态、V-26 webhook 9 例表驱动、V-27 文本卫生。
- 覆盖：`sanitizeSnippet`/`respBody`/`dingRespErr`/`webhookRespErr`/`dingTalkSignedURL` **100%**，`DingTalkSender.Send` 94.7%。（**rev6 更正归因**：未覆盖块实为 `sender.go:266-268`——带 `sign_secret` 且 URL 非法时 `urlErrCause` 的脱敏出口，不是这里原先写的 `NewRequestWithContext` 分支；已由 `TestDingTalkSender_加签时URL非法不泄漏原串` 覆盖，正确性审计 LOW-6。）
- **变异 8/8 红在断言上**（`/tmp/mutate5.py`）：

| # | 变异 | 红在哪 |
|---|---|---|
| M2-1 | 加签 key/msg 写反 | 签名向量断言（写反得到另一个值） |
| M2-2 | 手拼 query（不编码 `+`/`=`） | `%2B`/`%3D` 断言 |
| M2-3 | 钉钉不校验回执 | `errcode=310000` 用例 |
| M2-4 | webhook 不校验回执 | `errcode=93000` 用例 |
| M2-5 | 钉钉回执非 JSON 改 fail-open | fail-closed 用例 |
| M2-6 | `sanitizeSnippet` 去掉 `redact.Text` | `SECRETQUERY` 泄漏断言 |
| M2-7 | 按**字节**截断（保持可编译：`_ = utf8.RuneCountInString(s)`） | `expected 200 / actual 68` rune |
| M2-8 | 无 `sign_secret` 也加签 | 「不加签」用例 |

**一条方法学记录**：M2-7 第一次写成直接 `s[:maxRespSnippet]`，结果 `unicode/utf8` 变成未使用 import → **红在编译上**，按 T-31 的规矩改成可编译变异（保留 `_ = utf8.RuneCountInString(s)`）后才红在断言上（`expected 200 / actual 68`）。脚本已加「红在编译上」的显式告警，避免把编译失败当成反证成立。

全量：backend **27 包全绿 / 981 测试函数**（+10）/ `gofmt`+`go vet` 干净；frontend 未改（27 文件 174 测试）。

### 7.5 M2 两路只读审计处置（rev5 → rev6）

审计方式：安全 / 正确性两路各起独立只读子代理（仓库 `cp -a` 到 `/tmp` 副本跑测试与变异，原仓库零写入），各自**独立设计变异**（安全 7 条 + 正确性 MU1–MU16 条），并对照钉钉官方文档与独立 Python 实现复核签名公式。两路**独立命中同一个 MEDIUM**，无 HIGH/阻塞项。

| # | 路 | 严重度 | 发现 | 处置 |
|---|---|---|---|---|
| MED-1 | 安全 + 正确性（**独立命中**） | MEDIUM | `dingRespErr` 对「合法 JSON 但无 `errcode`」判成功：`{}`、`null`、`{"errmsg":"ok"}`、`{"errcode":null}` 的零值 0 被当成 `errcode=0` → 与「无法确认送达即失败」自相矛盾；网关/代理把空响应序列化成 `null` 时消息没进群却记 success 且不再重发 | `ErrCode` 改 `*int`，**缺键/null 一律返错**（`缺 errcode: …`）。钉钉恒返 errcode，对真实钉钉零影响；新增 5 例表驱动用例 |
| MED-2 | 安全 | MEDIUM | `sanitizeSnippet` 只做「脱敏/UTF-8/截断」，**保留控制字符**：NUL 进 `notification_logs.error_msg` → PG 22021 拒收 → 行永远 pending 被无限重发；CR/LF 可把行式消费的日志/error_msg 伪造成多条记录 | 新增 `stripControlChars`（口径同 `handlers.sanitizeAuditUsername`：`r < 0x20 \|\| r == 0x7f`，drop）；`sanitizeSnippet` 与 `worker.markFailed`（**入库出口**，覆盖 SMTP/驱动等非 sender 来源）都接上 |
| LOW-1 | 正确性 | LOW | `webhookRespErr` 先命中就 `return`：`{"errcode":0,"code":1}` 判成功、`{"code":0,"errcode":1}` 判失败——同义两种结果；键序分支零覆盖（变异存活） | 两个键都看完再决定；表驱动补 4 例（含负值） |
| LOW-2 | 正确性 | LOW | `respBody` 限读 4KiB 按字节截断 → 合法 JSON 被切断：钉钉侧「真送达」判失败、webhook 侧「真失败」判成功 | 上界提到 64KiB；用例用 >4KiB 的合法成功回执钉住；残余登记 R-14 |
| LOW-3 | 正确性 | LOW | 字符串/浮点 `errcode`（合法 JSON）被报成「不是合法 JSON」，把排障带偏到「网关改写了响应」 | `errors.As(*json.UnmarshalTypeError)` → 专用文案「errcode 类型不是整数」，行为仍 fail-closed |
| LOW-4 | 正确性 | LOW | `dingTalkSignedURL` 用 `u.Query()` 吞掉 `ParseQuery` 的 error 再 `Encode()` 重写 → `?access_token=%zz` 这类畸形参数**无声消失**（M2 新引入的往返），钉钉只报 310000，会误判成签名问题 | 显式 `url.ParseQuery` + 检查 error，畸形 fail-closed；用例断言返错而非丢参 |
| LOW-5 | 正确性 | LOW | `Send` 现在要等响应体读完（最多 `Client.Timeout` 10s），是 M2 引入的行为变化 | 不改代码（有界 + worker 侧 30s ctx 兜底）；登记 R-15 |
| LOW-6 | 正确性 | LOW | §7.4 把 `Send` 唯一未覆盖分支认错（实为 `sender.go:266-268` 加签分支的 `urlErrCause` 出口，且零测试） | 文档归因更正；补 `TestDingTalkSender_加签时URL非法不泄漏原串`（同时覆盖该脱敏出口） |
| LOW-7 | 安全 | LOW | `InDelta(…, 60_000)` 过宽：把时间戳改成 50 秒前仍存活 | 收窄到 10s（`M7-8` 证明其承重） |
| INFO | 安全 | — | `customSenders`（`sender.go:93-96`）是**既有**无同步的包级 map（本提交未引入，生产无写入者，仅测试用） | 不在本轮动；登记 TODO **G-38** |

**未采纳**（明确记录，避免下一轮重复讨论）：

- **`redact.Text` 对「裸」凭据无效**（`secret is SECtest123` 原样输出）：属形状识别的固有边界，已在 G-34/G-35 登记，不在本轮扩大改动。
- **负值 `errcode`/`code` 放行**（变异 `!= 0 → > 0` 存活）：无测试覆盖但钉钉/常见 webhook 不返负值；本轮**顺手补了一条负值用例**（`{"errcode":-1}` → 失败），语义不变。
- **`sanitizeSnippet` 去掉 `TrimSpace` 存活**：纯外观缺口，无断言价值。

**变异反证 9/9 红在断言上**（`/tmp/mutate7.py`，逐条确认非编译失败）：

| # | 变异 | 红在哪 |
|---|---|---|
| M7-1 | 缺 `errcode` 判成功（恢复 `r.ErrCode != nil && *r.ErrCode != 0`） | `回执缺errcode_fail_closed` 5 例子用例 |
| M7-2 | 删掉 `UnmarshalTypeError` 专用文案 | `errcode类型错误报专用文案` |
| M7-3 | webhook 回执先命中就 return | `{"errcode":0,"code":1}` 用例 |
| M7-4 | `sanitizeSnippet` 不剥控制字符 | `控制字符被剥` 子用例（`Not equal`） |
| M7-5 | `markFailed` 不剥控制字符 | `TestMarkFailed_控制字符被剥`（`should not contain "\x00"`） |
| M7-6 | 限读退回 4KiB | `TestRespBody_限读64KiB` 两条断言 |
| M7-7 | 加签退回 `u.Query()` | `畸形query返错而不是静默丢参` |
| M7-8 | 时间戳取 50 秒前 | `Max difference … allowed is 10000, but difference was 50001` |
| M7-9 | 加签分支错误文本不剥 `url.Error` | `加签时URL非法不泄漏原串`（原文含 `SECRETPATH`） |

全量：backend **27 包全绿 / 986 测试函数**（+5）/ `-race` 绿 / `gofmt`+`go vet` 干净；`stripControlChars`/`sanitizeSnippet`/`respBody`/`dingTalkSignedURL`/`dingRespErr`/`webhookRespErr`/`markFailed`/`DingTalkSender.Send` 覆盖 **100%**；frontend 未改（27 文件 174 测试）。
