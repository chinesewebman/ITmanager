# FIX-PLAN-ERROR-REDACT：错误文本旁路泄漏凭据（TODO G-28）

- **状态**：**已交付（2026-09-09）** — rev2 并入三视角审查（§7），实现/验证/变异反证见 §8
- **日期**：2026-09-09
- **关联**：TODO G-28（G-16 三视角审查发现）。G-16 堵的是 **SQL 参数**，本条堵的是**错误文本**——两条互不相干的路径（同 T-32 的教训）。
- **影响面**：钉钉 / 自定义 webhook 的 URL 凭据随错误文本进入 ① 应用日志（**stderr**）② `notification_logs.error_msg`（DB，进备份、进只读账号视野）③ `gin.DefaultErrorWriter`（**stderr**，人工点「测试发送」即可触发）④ 集成连通测试的 **HTTP 400 响应体**

## 1. 问题（What / Why）

### 1.1 现象（全部有代码证据）

| # | 证据 | 位置 | 内容 |
|---|---|---|---|
| E-1 | 钉钉发送失败 → `*url.Error` 含完整 URL | `internal/notification/sender.go:135-143` | `d.client.Do(req)`（`:140`）失败时 Go 返回 `&url.Error{Op:"Post", URL:"https://oapi.dingtalk.com/robot/send?access_token=SECRET", Err:…}`，`Error()` 即 `Post "https://…?access_token=SECRET": dial tcp …`（`net/url/url.go:29-36` 的 `"%s %q: %s"`） |
| E-2 | 自定义 webhook 同型，且**凭据常在 path** | `sender.go:277-288` | `w.cfg.URL` 交给 `Do`（`:285`）；Slack 形态 `https://hooks.slack.com/services/T…/B…/SECRET`、飞书形态 `https://open.feishu.cn/open-apis/bot/v2/hook/<token>` —— 凭据在 **path** |
| E-3 | **配置写错就能触发**（不依赖网络失败） | `sender.go:135-138` / `:277-280` | `http.NewRequestWithContext` 内部是 `url.Parse`，失败返回同样含完整原串的 `*url.Error{Op:"parse"}`（`net/url/url.go:485/491/504`）；且 `url.Parse` **不做** `stripPassword`（只有 `http.Client.do` 做，`client.go:624-631`/`:1034`）→ **userinfo 密码也会带出** |
| E-4 | 出口①：应用日志（**stderr**） | `internal/notification/worker.go:153` | `log.Printf("… send err for channel %s: %v", ch.Name, err)`。标准库 `log` 默认写 `os.Stderr`（`log.go:87`），全仓无 `log.SetOutput` 重定向 → **与出口③同一条流**；`log.level` 管不到它（与 G-16 的 T-32 同因） |
| E-5 | 出口②：入库 | `worker.go:246`、`:253` → `markFailed`（`worker.go:271-282`，>500 截断）→ `notification_logs.error_msg`（`internal/models/alert.go:130`，varchar(500)） | 进 DB 备份、进只读 DBA 视野 |
| E-6 | 出口③：stderr（**人工可触发**） | `internal/apierr/apierr.go:38-41` 把 `internalErr.Error()` 原文写 `gin.DefaultErrorWriter`；链路 A：`internal/api/handlers/channel_handler.go:85` `apierr.Internal(c, "测试发送失败", err)` ← `internal/service/channel_service.go:96` ← 同一个 `*url.Error` | 管理员在 UI 点「测试发送」即触发，不必等告警 |
| E-7 | 出口④：**HTTP 400 响应体** | `internal/api/handlers/integration_handler.go:121`、`:174`、`:213` | `apierr.BadRequest(c, "Zabbix 连通失败: "+err.Error())` —— `BadRequest` 传的 `internalErr` 是 `nil`（`apierr.go:51-53`），**既不过 `apierr.go:36` 的脱敏/记录分支，也不落日志**，内部文本原文进响应体。err 来自 `httpx.go:174`/`:167`，含 `*url.Error`（网络失败）或上游 4xx 响应体原文 |
| E-8 | 潜在面：panic 值 | `internal/middleware/recovery.go:27-28` `slog.Any("panic", err)` | 全仓 3 处 `panic(` 均为固定文案/重抛，无带凭据的源 |
| E-9 | 同函数内第四个日志点 | `worker.go:146` `log.Printf("… resolver err for channel %s: %v", ch.Name, err)` | 当前 `Resolver`/`NewSender` 的错误不含 URL（只有 JSON 类型错误与必填校验），但第三方 `RegisterSender` 的构造器错误不受控 |

**已核实为「非向量」**（写下来，避免下一轮重复调查）：

| 面 | 结论 | 依据 |
|---|---|---|
| email 发送 | 标准库自身文案不含口令（**服务端可控响应文本除外**） | `net/smtp`：`PlainAuth`/`Client.Auth` 的错误为 `smtp: server doesn't support AUTH`、`unencrypted connection`、`wrong host name`；`tls.Dial`/`SendMail` 失败是 `dial tcp host:port: …`。但 `c.Auth` 失败返回 `&textproto.Error{Code, Msg}`，`Msg` 是**服务端可控**文本，异常/恶意 SMTP 可在 535 里回显 base64 AUTH blob → 理论上可解出口令。登记为残余（§6） |
| `parseConfig` | JSON 错误只含类型/字符，不含原值 | `channelConfig` 无 map / 整数键字段；`UnmarshalTypeError.Value` 是类型名，语法错误只报 `invalid character 'x'`。（**注意**：`encoding/json` 对 map 的整数键会带原值 `decode.go:792/801`，本结构体无此形态） |
| eventbus DLQ | **发送**错误不进 DLQ | `handleAlertEvent` 的发送失败只打日志、最终 `:156 return nil`；只有 payload 解析失败（`:115`）与 DB 查询失败（`:120`）才 `return err` → DLQ |
| `notification_logs.recipient` | **当前无写入者** | 全仓 `grep -rn "Recipient:"` 零命中；只有 `models/alert.go:127` 定义与 `worker.go:252` 的读；`writeNotificationTrigger`（`alert_service.go:352-377`）不设该字段 → webhook URL 目前不会落库（**潜在**：将来若设，等于把 token 存进可查询的表） |
| `notification_logs` 其余列 | 无凭据 | `content` 写入者唯一（`alert_service.go:369-376`，`Alert <uuid> → <status> by user <userID>`）；`channel_name` = 管理员自由文本；`markSkipped` 固定文案（`worker.go:291`） |
| 前端 / HTTP | 无 notification_logs 端点 | `routes.go:363` 只有 `/notification-channels` 分组 → `error_msg` 的暴露面是 DB/备份，不是 HTTP。渠道 `Config`（含 `webhook_url`/`smtp_password`）**是有意回显**给管理员（整组 `canManage` + `RejectAPIKeyAuth`），不在本条范围 |
| `audit_logs` | 不存 body/query/header | `middleware/audit.go:91-131` 只写 Method/Path/IP/UA/RequestID；`entry.ErrorMsg` 全仓无写入者 |
| readiness 探测 | DB ping 错误不含 host/口令 | `routes.go:483/487` 把 ping 错误写进未认证响应体，但 pgx `pgconn/errors.go:69` 文案是 `failed to connect to \`user=%s database=%s\`` |
| gin access log | 含 RawQuery，但当前无端点从 query 读凭据 | `routes.go:141` `gin.Logger()` 记录 `path + "?" + RawQuery`；已 grep 全部 `c.Query`/`DefaultQuery` 调用，键为 `asset_id/key/from/to/limit/days/host/count/maxHops/severity/enabled/offset/keyword`，无凭据 |

### 1.2 根因

单一根因：**URL 被当作诊断信息塞进错误文本，而 URL 是凭据载体**。

`*url.Error` 把 URL 放进 `Error()` 是 Go 语言层的设计；我们在 4 个位置原样 `return err`（`sender.go:137`、`:142`、`:279`、`:287`），于是凭据沿四个出口流出去，而出口侧没有任何脱敏。

**为什么一直没被发现**：现有测试用 `httptest.NewServer`，URL 里既没有凭据、请求也不会失败；断言只看 `assert.Error(t, err)` 与 `assert.Contains(t, err.Error(), "403")`。**「连接失败 + 带 token 的 URL」这个组合从未被跑过**（`TestEmailSender_发到无效SMTP返错` 虽然真的连接失败，但配置里没有口令、断言只有 `assert.Error`）。

### 1.3 同族缺陷（同一次审查发现，本轮一并最小修）

| # | 缺陷 | 证据 | 后果 |
|---|---|---|---|
| S-1 | `markFailed` **按字节**截断 `errMsg[:500]` | `worker.go:272-274` | 切断多字节 UTF-8 字符 → PostgreSQL 拒收（`22021 invalid byte sequence`）→ UPDATE 失败。而列是 `varchar(500)`（**字符**数，`migrations/000009_notification_logs.up.sql:15`）→ 字节截断本就不对齐 |
| S-2 | `markFailed` 的 UPDATE **错误被丢弃** | `worker.go:275-281`（返回值未接收） | 与 S-1 叠加：写库失败无声无息 → 行永远 `pending` → worker 每轮重发同一条通知 |
| S-3 | 集成连通测试把内部错误原文回显给客户端 | `integration_handler.go:121/174/213` | 见 E-7 |

## 2. 方案（How）

### 2.1 候选对比

| 方案 | 做法 | 取舍 |
|---|---|---|
| **A（定案）** | **源头**：sender 构造错误时永不放入 URL（只留 `scheme://host` + 底层 cause），`*url.Error` 用 `errors.As` 递归剥壳；**出口**：四个落盘点各过一遍 `redact.Text`（结构性识别，不依赖参数名白名单） | 源头修的是真因；出口兜底第三方 sender 与未知出口。代价：错误文本变短（丢 path） |
| B | 只在出口按参数名黑名单脱敏 | 单一改动点，但黑名单必然漏（未知参数名、飞书/Slack 的 path token）——审查指出 rev1 的出口层正是这类黑名单 |
| C | 改协议把 token 放 header | 钉钉/企微协议固定 token 在 query，改不了；也改不了用户自建 webhook 的形态 |

### 2.2 定案（可执行细节）

#### ① 新包 `internal/redact`（两个纯函数，无状态、无第三方依赖）

```go
// URL 返回不含凭据的 URL 摘要：scheme://host。
// 丢 userinfo / path / query；解析失败、无 host、或 scheme 为空 → "<invalid-url>"。
func URL(raw string) string

// Text 把文本里的凭据替换为 ***，三条**结构性**规则（不依赖参数名白名单）：
//  1. 任何 scheme://… 形状的子串 → URL()（丢 userinfo/path/query，覆盖 Slack/飞书的 path token）
//  2. Authorization: Bearer|Basic <cred> → 值换 ***
//  3. 键值形态 name=value / name: value → 值换 ***，名字集合：
//     access_token / token / secret / password / passwd / pwd / api[-_]?key / apikey /
//     app_secret / user_token / sign / x-webhook-secret（大小写不敏感，要求词边界）
func Text(s string) string
```

设计要点（针对审查意见）：
- **规则 1 是结构性的**：只要形状像 URL 就塌缩成 `scheme://host`，因此飞书/Slack 的 path token 也挡得住（rev1 只列 query 参数名，被审查指为「黑名单必漏」）；
- 规则 3 **不收**裸 `key`（会误伤 `primary key=…` 这类正常文本）；`?key=` 在 URL 里由规则 1 覆盖（企微形态）；
- 规则 3 只替换**值**、保留名字（`access_token=***`），便于定位渠道；非敏感参数（`?page=2`）原样保留；
- 两个函数都是纯函数、无 I/O，测试无需 DB/网络。

#### ② sender 侧（根因修复，`internal/notification/sender.go`）

```go
// urlErrCause 剥掉 *url.Error 外壳：它的 Error() 含完整 URL（可能带凭据）。
// 递归剥（重定向链可能嵌套），上限 4 层；内层为 nil 或超限 → 返回固定文案
// （绝不把带 URL 的原串回传）。
func urlErrCause(err error) error

// 钉钉（:135-143）与自定义 webhook（:277-288）同型改法：
req, err := http.NewRequestWithContext(...)
if err != nil {
    return fmt.Errorf("dingtalk: 无效的 webhook_url: %w", urlErrCause(err)) // 不带 URL
}
resp, err := d.client.Do(req)
if err != nil {
    return fmt.Errorf("dingtalk: POST %s: %w", redact.URL(d.cfg.WebhookURL), urlErrCause(err))
}
```

- 保留 host 与底层 cause（`dial tcp 1.2.3.4:443: connect: connection refused`、`context deadline exceeded`、`no such host`）→ 定位能力不丢；
- 非 2xx 分支（`sender.go:146`、`:291`）本来就只有状态码，**不动**；
- email sender（`:174-`）**不动**（见 §1.1 非向量表，残余见 §6）。

#### ③ 出口兜底（五处）

| 位置 | 改法 |
|---|---|
| `worker.go:146` resolver 日志 | `redact.Text(err.Error())` |
| `worker.go:153` 应用日志 | `redact.Text(err.Error())` |
| `worker.go:271-282` `markFailed` | ① `errMsg = redact.Text(errMsg)`；② **按 rune 截断**到 500（修 S-1）；③ UPDATE 错误不再丢弃，`log.Printf` 一行（修 S-2，日志值再过 `redact.Text`） |
| `apierr.go:40` 5xx 分支 | `internalErr.Error()` → `redact.Text(internalErr.Error())` |
| `integration_handler.go:121/174/213` 400 文案 | `"Zabbix 连通失败: " + redact.Text(err.Error())`（修 S-3；保留 host 与原因，管理员诊断能力不变） |

`internal/apierr`、`internal/api/handlers` 引入 `internal/redact` 不产生 import 环（redact 只依赖 `net/url`、`regexp`；`unicode/utf8` 用在 `worker.go` 的 rune 截断）。

## 3. Where（变更清单）

| 文件 | 动作 |
|---|---|
| **新增** `backend/internal/redact/redact.go` | `URL` / `Text` |
| **新增** `backend/internal/redact/redact_test.go` | V-4 / V-5 |
| 改 `backend/internal/notification/sender.go` | `urlErrCause` + 4 处 return |
| 改 `backend/internal/notification/worker.go` | `:146`/`:153` 日志、`markFailed`（脱敏 + rune 截断 + 记错） |
| 改 `backend/internal/apierr/apierr.go` | `:40` 脱敏 |
| 改 `backend/internal/api/handlers/integration_handler.go` | 三处 400 文案 |
| 改 `backend/internal/notification/notification_test.go` | V-1 / V-2 / V-3 / V-6 / V-9 |
| 改 `backend/internal/apierr/apierr_test.go` | V-7 |
| 改 `backend/internal/api/handlers/integration_handler_test.go` | V-10 |
| 改 `backend/internal/httpx/httpx.go`（第二轮） | `redactedErr` 出口收口（H-1） |
| 改 `backend/internal/redact/redact.go` + `redact_test.go`（第二轮） | scheme-relative 分支、值类补 `'`/`\`、`host:` 尾冒号（M-2/M-3） |
| 新增 `backend/internal/integration/sync_log_redact_test.go`（第二轮） | V-14 |
| 文档 | `TODO.md`（G-28 结案 + 新登记 G-31/G-32/G-33）、`docs/TRAPS.md` **T-34**、`TESTING.md` 增量、`08-部署运维.md` §8.4.3「错误文本也已收口」 |

## 4. 验证清单

单测（每条都先有「证明路径真的被走到」的**正向**断言，T-31/T-33 教训；`Contains`/`NotContains` 一律传 `err.Error()` —— 传 `error` 值在 testify 下走 `len()` 直接 Fail）：

| # | 用例 | 断言 |
|---|---|---|
| V-1 | 钉钉 Send：`net.Listen` 拿地址后**只 bind 不 Accept** + `ctx` 200ms 超时，URL 带 `?access_token=SUPERSECRET` | `require.Error`；`errors.Is(err, context.DeadlineExceeded)`（证明底层 cause 被保留）；`require.Contains(err.Error(), "http://127.0.0.1:")`（`http://` 只可能来自 `redact.URL`，钉住它真的被调用）；`NotContains` `SUPERSECRET` / `access_token` / `/robot/send` |
| V-2 | 自定义 webhook，URL path 带 `SECRETPATH` | 同 V-1，另 `NotContains(SECRETPATH)` |
| V-3 | 非法 URL（`http://[::1`，全平台 `url.Parse` 失败） | `require.Error`；`require.Contains(err.Error(), "missing ']' in host")`（不绑中文措辞）；`NotContains("http://[::1")` |
| V-4 | `redact.URL` 表驱动 | 带 userinfo / query / path / 端口的 URL → `https://host:port`；空串 / 非法 / 无 host / **scheme 为空（`//host/p`）** → `<invalid-url>` |
| V-5 | `redact.Text` 表驱动 | ① URL 塌缩：`https://hooks.slack.com/services/T/B/SECRET` → `https://hooks.slack.com`（**path token**）、飞书 hook、带 userinfo、带 query；② `Authorization: Bearer eyJ…`；③ 键值形态（大小写混合、`"password":"…"`、`?a=1&token=x`）；④ **不被误伤**：`pq: duplicate key value violates unique constraint "users_username_key"` 原样返回、`?page=2` 原样返回、值含 `$` 不破坏替换语义 |
| V-6 | `markFailed`（**真 sqlite 写 + 读回**，包内自带 `notification_logs` 建表 + `SetMaxOpenConns(1)`） | 输入 `"access_token=SUPERSECRET x" + strings.Repeat("中", 600)`（值后有空格定界——否则规则 3 的值类会把汉字一起吞掉；脱敏后 618 rune / 1818 字节，500 的边界落在汉字中间）：`require.NoError(db.First)`、`require.Equal("failed", got.Status)`（**证明 UPDATE 真的生效**）、`require.Contains(got.ErrorMsg, "access_token=***")`、`NotContains("SUPERSECRET")`、`Equal(500, utf8.RuneCountInString(got.ErrorMsg))`、`True(utf8.ValidString(...))`（修 S-1 后按 rune 截断） |
| V-7 | `apierr.Respond` 5xx（替换 `gin.DefaultErrorWriter` 到 buffer，运行时读取已确认 `apierr.go:38`；**不加 `t.Parallel()`**） | 注入 `zabbix: Post "http://127.0.0.1/api_jsonrpc.php?auth=SUPERSECRET": dial tcp: refused` → `require.Contains(buf, "internal=")`（证明 5xx 分支真的被走到）、`require.Contains(buf, "http://127.0.0.1")`（URL 塌缩）、`NotContains` `SUPERSECRET`/`auth=` |
| V-9 | 出口①（`worker.go:153`）：`log.SetOutput(&buf)` + `RegisterSender` 注入带 token URL 的错误 → `handleAlertEvent` | `require.Equal(1, hits)`（sender 真的被调）、`require.Contains(buf, "send err for channel")`（证明日志行被走到）、`require.Contains(buf, "https://oapi.dingtalk.com")`、`require.Contains(buf, "secret=***")`、`NotContains` `SUPERSECRET`/`TOPLEVELSECRET`/`access_token` |
| V-10 | 集成连通测试 400 文案（**真 `IntegrationService`**——nil svc 会 panic，与既有 `*_svcNil_返500` 用例区分） | ① `http://127.0.0.1:1/api_jsonrpc.php?auth=SUPERSECRET`（立刻 refuse，1.5s 重试）→ 400 body 解 JSON 后 `Contains("Zabbix 连通失败")`、`Contains("http://127.0.0.1:1")`、`NotContains` `SUPERSECRET`/`auth=`；② `http://admin:SUPERSECRET@[::1/api_jsonrpc.php`（parse 失败路径无 `stripPassword`）→ `Contains("missing ']' in host")`、`NotContains` `SUPERSECRET`/`admin:` |
| V-8 | 全量 | `go test ./... -count=1` 26 包绿、`go vet` 干净、`gofmt` 无 diff、`-race`（新增全局 `log.SetOutput`/`DefaultErrorWriter` 用例须复位） |
| V-11 | `urlErrCause` 表驱动（补覆盖率缺口，钉住 R-3 的兜底分支） | 剥一层拿到底层 cause、非 `*url.Error` 原样返回、`Err == nil` → `"未知错误"`、嵌套 5 层 → `"未知错误"`（**绝不回传带 URL 的原串**） |
| V-12 | `markFailed` 写库失败（sqlmock：`ExpectBegin` + `ExpectExec(UPDATE "notification_logs")` 返错 + `ExpectRollback`） | `require.Contains(buf, "markFailed")`（修 S-2 前该错误被静默丢弃）+ `ExpectationsWereMet` |
| V-13 | `httpx.Do` 两条出错路径（第二轮，安全审计 H-1） | ① 重试耗尽：`BaseURL=http://127.0.0.1:1/api?access_token=SUPERSECRET`（`MaxRetries=0` 保持快）→ `NotContains(secret)` + `Contains("http://127.0.0.1:1")` + `errors.As(*url.Error)`（**Unwrap 保留错误链**）；② 4xx 响应体回显 URL：`NotContains(secret)` + `Contains("→ 400")` |
| V-14 | `SyncAll` 三行失败日志（第二轮，出口侧再钉一遍） | 三客户端同指 `127.0.0.1:1` + `?access_token=` → `require.Error`；`Contains` 三行 `「… 同步失败」`（证明日志行被走到）、`NotContains(secret)`、`Contains("http://127.0.0.1:1")` |

**变异反证**（每项必须红在**断言**上，不能红在编译上）：

| 变异 | 期望红在 |
|---|---|
| M1 `sender.go` 退回 `return err` | V-1 / V-2 |
| M2 `redact.URL` 返回原串 | V-1 / V-2 / V-4 |
| M3 `redact.Text` 恒等返回 | V-5 / V-6 / V-7 / V-9 / V-10 |
| M4 `markFailed` 去掉脱敏 | V-6 |
| ~~M5 `markFailed` 改成先截断后脱敏~~ | **已移除：该变异不可观测**（见 §7.2）。实测四种构造（480×`A`+userinfo URL、键值+600 汉字、520×`A`+query URL、`password=`+600×`B`）下，先截断后脱敏与先脱敏后截断**均无凭据残留**——规则 1 是形状识别（URL 被截断后仍是 URL 形状，落到 `<invalid-url>`），规则 3 的值类以「到定界符/串尾」为界，截断不改变可识别性。故「先脱敏」只是让截断作用在最终文本上，**不是安全边界**；`worker.go` 注释已按此改写 |
| M6 `apierr` 去掉脱敏 | V-7 |
| M7 `urlErrCause` 恒等返回 err | V-1 / V-3 |
| M8 删 `worker.go:153` 的 `redact.Text` | V-9 |
| M9 `redact.Text` 去掉规则 1（URL 塌缩） | V-5 ① 与 V-10 |
| M10 `markFailed` 退回字节截断 | V-6（`utf8.ValidString` / rune 长度） |
| M11 `httpx` 三处 return 退回裸 `fmt.Errorf` | V-13 ①②、V-14（实测红文本见 §9.3） |
| M12 去掉 scheme-relative 分支 | V-4、V-5 |
| M13 值类恢复排除 `'` | V-5 |
| M14 值类恢复排除 `}` `]` `<` `>` | V-5（`{"password":"***}c"}` / `password=}SUPERSECRET` 原样） |
| M15 规则 2 值类恢复排除 `,` | V-5（`Bearer ***,def`） |
| M16 `strings.ToValidUTF8` → `strings.Clone`（恒等） | V-15 |

## 5. Risk

| # | 具体失败模式 | 缓解 |
|---|---|---|
| R-1 | **过度脱敏**：规则 1 把正常诊断里的 URL 路径也抹掉（`https://zabbix.example.com/api_jsonrpc.php` → `https://zabbix.example.com`），排障时少一截信息 | 保留 host 与底层 cause；V-5 ④ 钉住「SQL 约束名 / `?page=2` / 无敏感内容」原样返回；渠道配置本身管理员可见 |
| R-2 | **改错错误文本导致既有断言破裂** | 非 2xx 文案（`dingtalk http 403`）与 email 文案**不动**；现有 `TestDingTalkSender_HTTP非2xx返错`、`apierr` 五个用例、`TestTranslateDBError_*` 必须保持绿（已逐个核对，见 §7 C-Q4） |
| R-3 | **`urlErrCause` 递归剥壳死循环 / 超限时反而回传带 URL 的原串** | 上限 4 层；内层 nil 或超限 → 固定文案（`"未知错误"`），绝不回传原串 |
| R-4 | **测试假绿**：`NotContains` 在 err 为空串 / DB 行不存在 / buffer 为空时恒真 | 每个用例先 `require.Error` + 正向 `require.Contains`；V-6 先 `require.NoError(First)` + `Equal("failed", Status)`；V-7/V-9 先 `require.Contains` 日志前缀 |
| R-5 | **改 rune 截断引入行为变化**（原来 500 字节，现在 500 字符 → 写库文本变长） | 列是 `varchar(500)`（字符），rune 截断才是语义对齐；V-6 用 `Equal(500, utf8.RuneCountInString)` 钉住；sqlite 不校验 UTF-8，故另加 `utf8.ValidString` 断言 |
| R-6 | 漏掉第五条出口（未来新增日志点） | §1.1 已把出口清单写全（含 gin access log、`worker.go:146`、HTTP 400 body）；`redact` 是纯函数，新出口一行接上 |

## 6. 边界（不做的事）与登记

- **不改** email sender 的文案（非向量，见 §1.1）；**残余**：恶意/异常 SMTP 服务器可在 AUTH 失败的 `textproto.Error.Msg` 里回显 base64 blob → 理论上可解出口令（需服务端可控，属信任边界）。
- **已改** `httpx` 的 4xx 上游响应体拼接（`httpx.go:167`）：第二轮在 httpx 出口统一过 `redact.Text`（§9.1），响应体里回显的 URL 凭据一并塌缩；「上游响应内容是否可信」的其余面仍归 **G-31**。
- **残余（登记 G-34）**：规则 3 的值**以** `&`/`,`/`;` 或引号开头时不匹配（如 `password=&SECRET`）；规则 2 的值以引号开头同理。RE2 无反向引用，无法做「同名引号配对」；当前用「引号可选 + 值类到定界符」近似，代价是这两个窄形态漏脱敏。修法见 G-34。
- **残余（登记 G-35）**：规则 1 先跑会让规则 3 二次作用于结果——主机名恰好命中敏感词时端口被当值抹掉（`Text("http://token:8080/x")` → `http://token:***`）。过度脱敏、不泄漏，故只登记。
- **不**顺手修 `notification_logs.recipient` 的潜在风险（当前无写入者）。
- **不**做全局 `log` 重定向 / slog 替换（G-16 已定：两条日志路径各自接级别；本轮只处理错误文本）。
- **不**动 `recovery.go` 的 `slog.Any("panic", err)`（无已知带凭据的 panic 源）。
- **不**动渠道 `Config` 的明文回显（`routes.go:362-364` 显式决定：整组 `canManage` + `RejectAPIKeyAuth`）。
- **不**改 gorm logger 的错误输出（pgx `%#v` 编码错误带值，`pgtype.go:1905`；`gorm_logger.go` 只参数化了 SQL 文本）—— 当前无「凭据 → 非字符串列」的调用点 → **G-32** 登记。
- **不**修 seed 的企微渠道配置键（`cmd/seed/main.go:248` 用 `webhook_url`，而 webhook sender 只认 `url` → 该渠道恒 `webhook: url is required`）与钉钉 `SignSecret` 未参与签名（`sender.go:78` 解析后从未使用）—— 功能缺陷，非安全 → **G-33** 登记。

## 7. 审查记录（三视角 → 处置）

三份只读审计（正确性 / 安全 / 测试有效性）共 30 条发现，处置如下。

### 7.1 已采纳并落入 rev2

| 审查发现 | 来源 | 处置 |
|---|---|---|
| 出口①是 **stderr** 不是 stdout（`log.go:87`） | 正确性 M-1 | §1.1 E-4 改写，并注明与出口③同流 |
| 出口层挡不住 path / header / body 形态（与「黑名单必漏」自相矛盾） | 正确性 M-2、安全 H-3 | `Text` 改为**结构性**三规则（URL 塌缩 / Auth 头 / 键值），V-5 补 6 类绕过用例，M9 钉住 |
| 第四条出口：HTTP 400 响应体（`integration_handler.go:121/174/213`，同类写法 30 处） | 正确性 M-4、安全 H-1 | 新增 E-7 + §2.2③ 修三处；其余 27 处（多为 DB/校验错误，不含 URL）登记 G-31 |
| G-28 原始登记的第 4 项（pgx 编码错误带值）被 rev1 静默丢掉 | 安全 H-2 | 登记 **G-32**（需自定义 gorm logger，本轮不做，理由写明） |
| `worker.go:146` resolver 日志未列入出口 | 正确性 L-7、安全 H-5 | 新增 E-9 + §2.2③ 一并脱敏 |
| `markFailed` 按字节截断会切 UTF-8 → PG 拒收（sqlite 测不出） | 正确性 M-3、测试 L-3 | 新增 §1.3 S-1，改按 rune 截断；V-6 用 `utf8.ValidString` + rune 长度断言 |
| `markFailed` 吞 UPDATE 错误 → 行永远 pending、无限重发 | 测试 B-2 | 新增 §1.3 S-2，记日志；V-6 加 `Equal("failed", Status)` 正向断言 |
| M5 变异行为等价（query 形态截断后仍能匹配） | 测试 B-1 | **第二轮修正**：审查建议的 userinfo 跨界构造同样不可观测（规则 1 不依赖 `@`）→ 实测后**整条 M5 移除**，见 §4 变异表与 §7.2 |
| `require.Contains(err, …)` 传 error 值在 testify 下必 Fail | 测试 H-1 | §4 顶部统一要求 `err.Error()` |
| `connection refused` 跨平台不稳 / `Contains(host)` 不钉住被测代码 | 测试 H-2 | V-1 改「只 bind 不 Accept + 200ms ctx」确定性构造，用 `errors.Is(DeadlineExceeded)` + `Contains("http://127.0.0.1:")` |
| 出口①零测试零变异 | 测试 H-3 | 新增 V-9 + M8 |
| V-3 绑中文措辞 / 不应断言 `errors.As(url.Error)` | 测试 M-1 | 改断 `missing ']' in host` |
| V-7 正向断言缺失 + 全局变量不能并行 | 测试 M-2 | V-7 补正向断言 + 禁 `t.Parallel()` |
| `newTestDB` 不可复用（`package models_test`、无 `NotificationLog` 分支） | 测试 M-3 | V-6 在 notification 包内自带建表与 helper |
| sqlite `:memory:` 多连接不可见 | 测试 L-2 | V-6 `SetMaxOpenConns(1)` |
| V-5 缺「非 URL 文本不被误伤」用例 | 测试 L-4 | V-5 ④ 补 SQL 约束名 / `?page=2` |
| `redact.URL` 未定义 scheme 为空的行为 | 正确性 L-5 | §2.2① 写明 → `<invalid-url>`，V-4 补用例 |
| `urlErrCause` 内层 nil / 超限回传原串 | 正确性 L-6 | §2.2② 写明：nil/超限 → 固定文案 |
| 行号 off-by-one（`return err` 在 137/142/279/287） | 正确性 L-1 | §1.2 已改 |
| `handleAlertEvent` 非向量表表述过泛 | 正确性 L-2 | §1.1 改为「只有 payload/DB 错误才 return err」 |
| json 表述过泛（map 整数键会带值） | 正确性 L-3 | §1.1 限定到 `channelConfig` |
| email 非向量是绝对结论 | 正确性 L-4 | §1.1 + §6 补「服务端可控响应文本除外」 |
| 现有测试表述不精确 | 正确性 L-8 | §1.2 补 `TestEmailSender_发到无效SMTP返错` 的反例说明 |
| `writeNotificationTrigger` 行号偏移 | 正确性 L-9 | §1.1 改为 `:352-377` / 字面量 `:369-376` |
| gin access log 带 RawQuery | 安全 H-4 | §1.1 非向量表补一行 |
| 渠道 Config 明文回显可能被误读为缺陷 | 安全 H-6 | §1.1 + §6 指向 `routes.go:362-364` 的有意决定 |
| seed 企微键写错 + 钉钉 `SignSecret` 未使用 | 安全 H-7 | 登记 **G-33**（功能缺陷，非安全） |

### 7.2 未采纳（附理由）

| 审查建议 | 理由 |
|---|---|
| 「只在出口脱敏、不动源头」（候选 B） | 出口层是黑名单/结构识别的兜底，无法保证第三方 sender 的错误形态；源头修复成本 4 行且是唯一能保证「错误值本身不带凭据」的位置 |
| 「`Text` 加裸 `key`」 | 误伤面大（`primary key=…`）；企微 `?key=` 由规则 1 覆盖 |
| 本轮就改 gorm logger 只打 SQLSTATE（H-2） | 需替换 gorm logger 的错误输出路径，爆炸半径超出「错误文本脱敏」；且当前无凭据调用点 → G-32 |
| 本轮就改 email 的 `textproto.Error` 处理 | 需服务端可控才成立，属信任边界；改法（丢弃服务端文本）会损失 SMTP 排障信息 → §6 登记残余 |
| 把「先脱敏后截断」当安全边界并配变异 M5 | **实测不可观测**：四种构造（userinfo URL / 键值+汉字 / query URL / `password=`）下先截断后脱敏均无凭据残留（规则 1 是形状识别，截断后仍是 URL 形状 → `<invalid-url>`；规则 3 值类以定界符/串尾为界）。顺序保留只为让截断作用于最终文本，不列为变异 |

## 8. 实现记录

### 8.1 落盘（2026-09-09）

| 文件 | 改动 |
|---|---|
| 新增 `internal/redact/redact.go` | `URL`（scheme://host，丢 userinfo/path/query；解析失败或无 host → `<invalid-url>`）+ `Text`（规则 1 URL 塌缩 / 规则 2 Authorization 头 / 规则 3 键值形态，保留键名与定界符） |
| 新增 `internal/redact/redact_test.go` | V-4 / V-5（含 6 类绕过 + 3 类不误伤） |
| `notification/sender.go` | 新增 `urlErrCause`（剥 `*url.Error`，上限 4 层，nil/超限 → `"未知错误"`）；`DingTalkSender.Send` 与 `WebhookSender.Send` 各 2 处 return（parse 失败不包原 err、Do 失败只留 host + cause） |
| `notification/worker.go` | `:149`/`:156` 两行日志过 `redact.Text`；`markFailed` 先脱敏、按 rune 截断到 500、UPDATE 错误记日志 |
| `apierr/apierr.go` | 5xx 内部日志过 `redact.Text` |
| `api/handlers/integration_handler.go` | 三处连通测试 400 文案过 `redact.Text` |
| 测试 | `notification_test.go` V-1/V-2/V-3/V-6/V-9/V-11/V-12；`apierr_test.go` V-7；`integration_handler_test.go` V-10（真 `IntegrationService`） |

### 8.2 验证结果（2026-09-09）

- `go test ./... -count=1`：**全绿**（含既有 26 包）；`go vet ./...` 干净；`gofmt -l` 无 diff。
- `-race`（`redact`/`notification`/`apierr`/`api/handlers`）：全绿。
- 覆盖率（语句）：`redact` **100%**、`urlErrCause` **100%**、`markFailed` **100%**、`apierr` 88.5%；`notification` 包整体 72.1%（其余为未改动的 email/worker tick 路径）。
- **变异反证 9/9 全部红在断言上、无一红在编译上**：

| 变异 | 结果 | 首个红断言 |
|---|---|---|
| M1 `sender` 退回裸 `err` | RED | V-1 `NotContains("SUPERSECRET")` |
| M2 `redact.URL` 返回原串 | RED | V-1 `NotContains("SUPERSECRET")` + V-4 |
| M3 `redact.Text` 恒等 | RED | V-6 `Contains("access_token=***")` + V-7/V-10 |
| M4 `markFailed` 去掉脱敏 | RED | V-6 `Contains("access_token=***")` |
| M6 `apierr` 去掉脱敏 | RED | V-7 `NotContains("SUPERSECRET")` |
| M7 `urlErrCause` 恒等 | RED | V-1 `NotContains("SUPERSECRET")` |
| M8 日志行去掉脱敏 | RED | V-9 `Contains("secret=***")` |
| M9 去掉 URL 塌缩规则 | RED | V-5① + V-10 |
| M10 退回字节截断 | RED | V-6 `Equal(500, RuneCount)` + `ValidString` |
| ~~M5 先截断后脱敏~~ | 移除 | 实测不可观测（见 §4 变异表、§7.2） |

- 实现期发现（并入 rev2）：**规则 3 的值类以定界符/串尾为界**，故 `access_token=SECRET中中中…` 会把后续汉字一并吞掉（过度脱敏，不泄漏）；V-6 的构造据此在值后加空格定界。

## 9. 第二轮：审计回执与处置（2026-09-09 晚）

第一轮三份只读审计（正确性 / 安全 / 测试有效性）在实现落盘后复查，安全与正确性两份先回。
本节记录这一轮的发现、修法与反证；**测试有效性审计尚未回**，回后并入本节。

### 9.1 安全审计：H-1（高，已修）

| 项 | 内容 |
|---|---|
| 发现 | `integration/service.go:282/289/296` 与 `metric_sync.go:111` 是 `log.Printf("… %v", err)`，**未过 `redact.Text`**；err 经 `%w` 链挂着 httpx 的 `*url.Error`（完整 URL）。实测 NetBox URL 带 `?access_token=SECRET` → 日志原文含 token |
| 定案 | **在 httpx 出口收口**，不在 4 个调用点补：① 一处覆盖全部消费者（含未来新增日志点）；② 4 处补丁只挡已知出口，下次新增日志点又会漏；③ 与「源头修复优先于出口兜底」的既有定案（§7.2 第 1 行）一致 |
| 实现 | `httpx.go` 新增 `redactedErr{err}`：`Error()` = `redact.Text(e.err.Error())`，`Unwrap()` 透传 → 错误链与 `errors.Is/As` 不变；三处 return（ctx 取消 / 4xx / 重试耗尽）统一包一层 |
| 反证 | V-13（两条路径 + `errors.As(*url.Error)`）、V-14（`SyncAll` 三行日志出口）；M11 退回裸 `fmt.Errorf` → 三个用例全红，红文本实测含 `access_token=SUPERSECRET` |

### 9.2 安全审计：M-2 / M-3（中，已修）

| 项 | 内容 |
|---|---|
| M-2 | 规则 1 要求 `scheme://`，`//host/path/SECRET` 形态整条不匹配 → 尾部泄漏。修：新增 scheme-relative 分支，authority 限定「带点域名 / `localhost` / `[IPv6]`」；`URL()` 对 `Scheme==""` 返回 `//host`（保留定位信息）。误伤守门：`//data/memory.db`、`// 注释` 原样返回 |
| M-3 | ① 值类 `[^\s"'<>\\]+` 把 `'`/`\` 当终点 → `?auth=AB'CDEF` 的 `CDEF` 残留；② `redact.URL("https://user:")` 是恒等（net/url 把无密码 userinfo 并进 `Host`）→ 回显疑似凭据。修：值类去掉 `'`/`\` 排除；`URL()` 对 `Host` 尾冒号（含 `https://user:`）返回 `<invalid-url>` |
| 反证 | V-4/V-5 新增 4 条用例；M12（去分支）、M13（恢复 `'` 排除）各自红在断言上 |

### 9.3 正确性审计：P1–P4 处置

| 发现 | 处置 |
|---|---|
| **P1** 规则 3 值类排除 `}` `]` `<` `>`（与规则 1 已放宽的值类不一致）→ `{"password":"ab}c"}` 漏 `}c`、`password=}SECRET` **整条不匹配**；规则 2 排除 `,` → `Bearer abc,def` 漏 `,def` | **已修**：规则 3 值类放宽为 `[^\s"'&,;]+`，规则 2 放宽为 `[^\s"']+`（值里的 `,`/`;` 也是凭据的一部分）。V-5 新增 4 条；M14/M15 红在断言上。剩余窄形态（值**以**分隔符/引号开头）→ **G-34** |
| **P2** 集成同步日志未脱敏 | 与安全 H-1 同一条 → §9.1 已闭合（审计员用「rev2 + 旧 httpx」配对快照复现红、当前快照绿） |
| **P3** 规则 1 输出被规则 3 二次误伤：`Text("http://token:8080/x")` → `http://token:***` | **登记 G-35**（过度脱敏、不泄漏；`token`/`secret` 当主机名罕见） |
| **P4** `markFailed` 只在 `>500 rune` 时做 `[]rune` 转换 → ≤500 rune 的**非法 UTF-8** 原样写库，真 PG 22021 拒收 → 行永远 `pending` 被无限重发（S-1 的窄化版，非本轮回归） | **已修**：`redact.Text` 之后加 `strings.ToValidUTF8(errMsg, "�")`（非法字节替换为替换符，SQL 侧不再可能因编码被拒）。V-15 钉住；M16 把该行改成 `strings.Clone`（恒等）→ 红在 `utf8.ValidString`。**真 PG 实测**（一次性 `postgres:18-alpine` 容器）：`convert_from('\x736d74703a20fffe','UTF8')` → `ERROR: invalid byte sequence for encoding "UTF8": 0xff`（exit 1）；替换后 `smtp: ��` 插入成功（exit 0） |
| 代理 userinfo 泄漏（另一审计员的临时探针，未留证据） | **未复现**：`net/http` 的 proxyconnect 路径返回 `*net.OpError`（不含代理 userinfo）；不作为缺陷 |

### 9.4 第二轮验证

- `go test ./... -count=1`：**27 包全绿**（含 `cmd/*` 与 `tests`）；`go vet ./...` 干净；`gofmt -l` 无输出。
- `-race`（`redact`/`httpx`/`integration`/`notification`/`apierr`/`api/handlers`）：全绿。
- 覆盖率：`redact` **100%**、`URL` **100%**、`markFailed` **100%**、`httpx` 88.1%、`integration` 84.6%、`notification` 72.2%。
- 变异：第二轮 M11–M16 六项**全部红在断言上**（M11 曾以「删 import」形态红在编译上，已改为「三处 return 退回裸 `fmt.Errorf`」）。
- 推送前由**单一进程**复跑（审计员观察到本轮变异脚本并发改写工作树，期间任何测试结论不作数）——工作树已确认是修复版（`git status` 无临时探针文件）。
