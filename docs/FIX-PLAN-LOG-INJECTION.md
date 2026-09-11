# M29：不可信文本的日志 / 落库出口净化

> 2026-09-12 起。上游：G-43（M28/F 实测发现）、G-31 残余①、M29 普查中新发现的
> 「审计行静默丢失」与「脱敏顺序反了导致凭据尾部泄漏」。
> 状态：需求文档 **rev2**（三路对抗审查后修订；rev1 的 5 处错误见 §7）。
> **未动代码**。

## 0. 缩放决策（先定规模，再写内容）

**为什么一个文档而不是两个**：改动面 6 个文件、无 schema 变更、无新依赖、无并发/状态机，
按 `CLAUDE.md` 的「脚本 / 胶水 / 配置驱动代码」一档走**代码审计兜底**，不摆两轮文档仪仗。
但**三视角对抗审查照做**（§7），因为这次的失败模式是「看起来修了、其实漏了另一条出口」——
rev1 被审查抓出的 5 处错误证明了这个判断（其中 1 处会写出**修不好自己声称修的东西**的代码）。

**为什么现在做**：M28/F 已经把 G-43 实测出来了（不是理论可能），而 G-31 残余① 在台账里
躺了 3 天。它们是同一个洞的两半：**「第三方/请求侧可控文本 → 行式消费的日志或落库」**。
分开修等于把同一份判断写两遍。

## 1. 事实与实测（先证伪，再动手）

### 1.1 普查：哪些出口能承载 CR/LF

判据是「**行式消费**的出口」（容器 json-file 日志、`error_msg`/`path` 等列、文本日志）
+「文本来源是否可控」+「**落库列是否有长度/编码约束**」。逐条实测/推演：

| # | 出口 | 文本来源 | 能到达 | 结论 |
|---|---|---|---|---|
| 1 | `apierr.go:41` 5xx 行（裸拼 `URL.Path`） | 请求路径（**解码后**） | CR/LF（实测 §1.2） | **修** |
| 2 | `audit.go:97` `Path` 落库 | 同上 | CR/LF **+ 22001 丢行**（§1.5） | **修** |
| 3 | `notification/worker.go:144/150/157` 日志 | `ch.Name`（运维配置）+ 第三方错误文本 | CR/LF | **修**（rev1 漏了 :144） |
| 4 | `notification/worker.go:301` `markFailed` 就地日志 | 上游响应体 | CR/LF | **修** |
| 5 | `notification/worker.go:279-287` `markFailed` **入 `error_msg` 列** | 同 #4 | **脱敏顺序反了 → 凭据尾部泄漏**（实测 §1.4） | **修**（rev1 误判为「不动」） |
| 6 | `httpx.go:41` `redactedErr.Error()` | 上游响应体 + URL | CR/LF | **修** |
| 7 | `integration/zabbix.go:238/242` `fmt.Errorf` | **HTTP 200 响应体里的 JSON-RPC 错误文本** | CR/LF + 无脱敏 | **修**（rev1 误判为「由 #6 覆盖」） |
| 8 | `audit.go:99/101` UA / `X-Request-ID` 落库 | 请求头 | **22001/22021 丢行**（§1.5） | **修**（rev1 误判为「不修」） |
| 9 | `integration/timeparse.go:104` `id` 用 `%s` | Zabbix `triggerid` | CR/LF（**只有 `raw` 是 `%q`**） | **修**（rev1 误判为「不修」） |
| 10 | `audit.go:98` IP（`ClientIP()`） | 头 / 连接 | 不能（gin `validateHeader` 只返合法 IP） | 不修 |
| 11 | `middleware/recovery.go:27-32` `slog.*` | 路径 / panic 值 | 不能（实测 slog 转义 §1.3） | 不修 |
| 12 | gin 访问日志（`gin.Logger()`） | 路径 | 不能（实测 `%#v` §1.3） | 不修 |
| 13 | `integration/` 同步路径 → `tickets.title` 等**有长度约束的列** | GLPI/Zabbix/NetBox 字段 | 22001 → **整批回滚** | **登记不修**（G-45，§1.7） |
| 14 | `config`/`database`/`migrate`/`seed` 的 `log.Printf` | 运维自己的配置值 | 能，但非外部输入 | 不修（§6） |

**关键区分（写进代码注释，防后人一刀切）**：**脱敏（`redact.Text`）与转义（剥控制字符）
是两件事**——`redact` 管「别泄漏凭据」，完全不碰 CR/LF；`slog`/`%q`/`%#v` 自带转义，
所以 #11/#12 的出口**不需要**再加净化，加了是噪声。
但反过来**不成立**：剥控制字符**不能**替代脱敏（#7 就是只有前者没有后者）。

### 1.2 实测 ①：`%0d%0a` 穿透到 `URL.Path`（G-43 的复现）

真 socket 打 raw 请求行（`httptest` 的 `NewRequest` 会绕开服务器读请求行这一段）：

```
GET /api/assets/abc%0d%0a[ERR]%20FORGED HTTP/1.1
```

命中 `/api/assets/:id`（**gin 按解码后的 path 匹配**，所以必须塞进 `:param` 段；
`/api/assets%0d%0aX` 会 404、到不了 handler），handler 触发 5xx 后日志实际输出**两行**：

```
[ERR] GET /api/assets/abc
[ERR] FORGED code=internal_error internal=boom
```

第二行与一条真实错误行**无从区分**。

### 1.3 实测 ②：slog / gin 自带转义（所以「全部出口都加净化」是错的）

```
TextHandler  -> time=… level=ERROR msg=msg path="a\r\n[FAKE] forged"
SetDefault   -> （同上，默认 handler 也是 TextHandler 语义）
gin.Logger() -> … | GET      "/api/assets\r\nFORGED LINE"
```

`log.Printf` **不转义**（它只加时间前缀）——这正是 #1/#3/#4/#9 要修的原因。

### 1.4 实测 ③（rev1 错误）：脱敏与剥控制字符的顺序**是**安全边界

rev1 §2.2 写「顺序不重要」。用**真实的 `redact.Text`** 跑临时探针（探针已删）：

| 输入 | `Text` | `Strip→Text` | `Text→Strip` |
|---|---|---|---|
| `password=YWJjZGVmZ2hp\namtsbW5vcHFy` | `password=***\namtsbW5vcHFy` | `password=***` | `password=***amtsbW5vcHFy` ← **尾部泄漏** |
| `pass\nword=SECRET` | 原样 | `password=***` | `password=SECRET` ← **整条泄漏** |
| `Authorization: Bearer SEC\nRET` | — | 遮盖 | `…***RET` ← **尾部泄漏** |

机理：控制字符会**截断**脱敏规则的值类（`Text` 只遮盖到 `\n` 为止），之后再把它删掉，
等于把未遮盖的尾部**接回**一个已经被认成凭据的串上。**必须先剥控制字符，再脱敏。**

**这不是理论**：`worker.go:279-287` 的 `markFailed` 今天就是 `Text → … → stripControlChars`
（写进 `notification_logs.error_msg` 的第三方文本），而同包的 `sender.go:183-190`
`sanitizeSnippet` 是 `stripControlChars → Text`（正确）。**同一个包两个相反顺序，
错的那个在往库里写**，且它的注释白纸黑字写着「顺序不是安全边界」。按 T-52
（一个安全控制的两半必须共享语义），这条注释本身就是缺陷的一部分。

### 1.5 新发现：审计行静默丢失（不止 `path`）

`buildAuditEntry`（`audit.go:91-131`）五个字符串字段里，**三个没有截断或截错了尺子**：

| 字段 | 代码 | 列宽（`migrations/000013:280/283/284` + `models/user.go:101-116`） | 问题 |
|---|---|---|---|
| `Path` | `c.Request.URL.Path` | `varchar(500)` | **完全不截断** → 22001 |
| `RequestID` | `c.GetHeader("X-Request-ID")` | `varchar(50)` | **完全不截断** → 22001 |
| `UserAgent` | `truncate(...,500)` | `varchar(500)` | **按字节**截断 → 多字节被劈开 → 22021 |
| `Username` | `truncate(username,100)` | `varchar(100)` | 同上（`auth.go:122/186` 路径不经 `sanitizeAuditUsername`） |
| `Resource` | `resourceFromPath` → `truncate(p,100)` | `varchar(100)` | 同上（取自 `FullPath()`，风险低） |

后果：INSERT 撞 `22001 value too long` 或 `22021 invalid byte sequence` → **整行审计记录
静默丢失**，只留一行 `slog.Warn("audit: failed to write audit log (async)")`。
**审计丢行**比日志难看严重得多：那是取证链。

**为什么这条只能在真 PG 上验证**：sqlite **不强制** `VARCHAR(n)` 长度（T-48 同族陷阱），
sqlite 用例结构上不可能测出 22001/22021，只写 sqlite 用例会**假绿**（§3 R4）。

### 1.6 可达性（诚实分级，rev1 在这里说错过一次）

- **未认证、远程、零技巧**：`POST /api/auth/login` + `X-Request-ID: <51+ 字符>`。
  `AuditLog` **也挂在登录路由上**（`routes.go:227`，rev1 误称「只挂在 protected 组」），
  该路由未认证可达，且 `RequestID` 不截断、列宽 50 → **登录爆破可以做到不留审计痕迹**。
  这是本模块**唯一未认证可达**的面，也是最该先修的一条。
  （登录路由的 path 是常量 `/api/auth/login`，**路径注入在这里不可达**——两件事别混。）
- **已认证**：长路径 / `%0d%0a` 路径须落在 `:id` 段（`/api/assets%0d%0aX` 会 404）。
- **第三方**：#3–#7/#9 需要上游（SMTP 服务端、被劫持或配置错的上游）回一段带 CR/LF 的文本。
- 全部属 CWE-117 / 审计篡改；**除登录路由那条外都不是未认证远程可利用**。

### 1.7 登记不修：同步路径的第三方文本长度（G-45）

`integration/` 目录**零**截断实现（已 grep 确认：无 `truncate`/`RuneCount`/`ToValidUTF8`）。
`SyncFromGLPI` 的 `Title`、`SyncFromZabbix` 的 `TriggerName`/`HostName`、`SyncFromNetBox`
的 `name/brand/model/sn/site_name` 都直接落有长度约束的列（手工建单路径有
`truncateRunes(title,255)`，同步路径没有）→ 源侧一条超长值即 22001 →
`CreateInBatches` **整批回滚**（`service.go:281`），整次同步失败。

**本轮不修**（改法与字段一一对应、属独立一轮；且要配「截断透出」，与 M27 的
`truncated` 标志同一族设计），登记为 **G-45**。**§5.3 结 G-44 时不得写成「该类已闭合」**。

## 2. 逐项修法

统一口径：**先 `StripControl`（剥控制字符，顺带把非法字节换成 U+FFFD）→ 再按需 `Text`
（脱敏）→ 最后按 rune 截断到列宽**。

### 2.1 A：新增 `redact.StripControl`（唯一净化实现）

```go
// internal/redact/redact.go
// StripControl 删除控制字符（C0 含 HTAB/CR/LF/NUL、DEL）。
// 与 Text 的分工：Text 管「别泄漏凭据」，本函数管「别伪造行」——两者互不替代，
// 且**顺序是安全边界**：先 Text 后 Strip 会把被控制字符切开的凭据尾部接回去
// （实测：password=abc\nDEF → Text 只遮到 \n → Strip 后尾部 DEF 明文泄漏）。必须先 Strip。
// 含 HTAB 是既定口径（与 notification.stripControlChars / sanitizeAuditUsername 一致）。
func StripControl(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
}
```

落 `redact` 而不是各包自带：它已是「不可信文本」的公共工具、无内部依赖、被 `apierr`
与 `httpx` 同时需要（`apierr` 不能反向 import `notification` 或 `httpx`）。
`strings.Map` 顺带保证输出是**合法 UTF-8**（非法字节 → U+FFFD），这正是它同时关掉
22021 那一类的原因。

**三处委托**（不删不改语义，一处实现多处调用）：
`notification.stripControlChars`（`sender.go:171`）、`middleware/audit.go` 的字段净化、
`handlers.sanitizeAuditUsername`（`auth_handler.go:42`，第三份实现；其 **96 字节预算保留**
——下游改成 rune 安全后它已属保守冗余，改它属范围蔓延，只换掉里面的循环）。

### 2.2 B：`apierr` 5xx 日志行（G-43）

```go
// before
gin.DefaultErrorWriter.Write([]byte(
    "[ERR] " + c.Request.Method + " " + c.Request.URL.Path +
        " code=" + code + " internal=" + redact.Text(internalErr.Error()) + "\n"))

// after：整行过一遍净化（method/path/code 全都不可信或半可信），再补回换行
line := redact.StripControl("[ERR] " + c.Request.Method + " " + c.Request.URL.Path +
    " code=" + code + " internal=" + redact.Text(internalErr.Error()))
gin.DefaultErrorWriter.Write([]byte(line + "\n"))
```

净化在**整行**（不只 `internal` 字段），所以写在最外层。

### 2.3 C：`audit` 字段净化 + **按 rune** 截断

rev1 写的是 `truncate(redact.StripControl(path), 500)` —— **修不好它声称修的东西**：
`audit.go:174-178` 的 `truncate` 是按**字节**切（`s[:max]`），`"/api/assets/" + "中"*200`
（612 B）会被切成 `bytes=500 / runes=176 / validUTF8=false` → PG 22021 → **照样丢行**，
只是从「超长」换成「非法编码」，而 §5.2 那条 507 字符 ASCII 用例会**假绿**。

```go
// audit.go：新增 rune 版（口径同 service.truncateRunes，同一理由：varchar(n) 按字符计数）
func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max { return s }
	return string(r[:max])
}

// sanitizeField 净化 + 按字符截断到列宽。
// 不做 Text（脱敏）：审计字段是取证材料，脱敏会破坏其证据价值 —— 只防伪造与丢行。
func sanitizeField(s string, maxRunes int) string {
	return truncateRunes(redact.StripControl(s), maxRunes)
}
```

逐字段套用（**既有 `truncate` 不动**——它还被 UA/username/error_msg/resource 用，
原地改语义会连带改四处）：

| 字段 | after | 宽度 |
|---|---|---|
| `Path` | `sanitizeField(c.Request.URL.Path, 500)` | 500 |
| `RequestID` | `sanitizeField(c.GetHeader("X-Request-ID"), 50)` | 50 |
| `UserAgent` | `sanitizeField(c.GetHeader("User-Agent"), 500)` | 500 |
| `Username` | `sanitizeField(username, 100)` | 100 |
| `Resource` | `sanitizeField(p, 100)`（在 `resourceFromPath` 内） | 100 |
| `ErrorMsg` | 死路径（全仓无 setter），**保持原样并提一句** | — |

`Username` 也过一遍是**纵深防御**：`sanitizeAuditUsername` 只在登录路由生效，
`protected` 组走的是 `auth.go` set 的原值。

### 2.4 D：`httpx.redactedErr` 净化

```go
func (e *redactedErr) Error() string { return redact.StripControl(redact.Text(e.err.Error())) }
func (e *redactedErr) Unwrap() error { return e.err }   // 不动：errors.Is/As 照旧
```

覆盖 `integration/service.go:459/466/476`、`metric_sync.go:111` 等
`log.Printf("%v", err)` 出口 —— 与 G-28 当年选 httpx 出口的理由相同（**逐个出口接必然漏**）。
但**只覆盖 httpx 起源的错误**（rev1 的「覆盖全部消费点」不成立，见 E）。

### 2.5 E：`zabbix.go` 的第三方错误文本出口（rev1 漏项）

```go
// internal/integration/zabbix.go:238/242 —— apiResp.Error.Message 是 HTTP 200 响应体里的
// JSON-RPC 错误文本（第三方完全可控，json.Unmarshal 会把 "\r\n" 解成真 CR/LF），
// 且 HTTP 200 → httpx 根本不构造 redactedErr，错误是裸 fmt.Errorf
return nil, fmt.Errorf("Zabbix API 错 %d: %s", apiResp.Error.Code,
    redact.StripControl(redact.Text(apiResp.Error.Message)))
```

两处（`:238` 含「(重登后)」变体）同改。这条同时是 G-28 的残余（原先连 `redact.Text` 都没有）。

### 2.6 F：`notification` 日志四处 + `markFailed` 顺序修正

- `worker.go:144/150/157`：三行的 `ch.Name` 与 `err.Error()` 一并
  `redact.StripControl(redact.Text(...))`（rev1 只修了 err，漏了 `ch.Name`——
  `notification_channels.name` 仅校验非空，含 CR/LF 的渠道名照样能伪造日志行）。
- `worker.go:301`：同上。
- **`markFailed`（`:279-287`）：把 `stripControlChars` 提到 `redact.Text` 之前**
  （§1.4 的活泄漏）。`ToValidUTF8` 与 rune 截断保持原位（Strip 已保证合法 UTF-8，
  前者成为兜底，无害）。同时**改写那段「顺序不是安全边界」的注释**并附实测反例——
  错误注释会教会下一个人犯同样的错（T-52）。
- `sanitizeSnippet`（`sender.go:183`）**已经是对的，不动**，作为正确顺序的参照。

### 2.7 G：`timeparse.go:104` 的 `id` 改用 `%q`

```go
// before: log.Printf("M26: %s %s 的 %s=%q 不可用（status=%d）", what, id, field, raw, st)
// after:  id 同用 %q —— 它是 Zabbix JSON 里的 triggerid，可任意字符串
log.Printf("M26: %s %q 的 %s=%q 不可用（status=%d）", what, id, field, raw, st)
```

（GLPI 侧 `local.ExternalID` 是 `Sprintf("%d", t.ID)`，安全。）

## 3. Risk（高风险改动：多文件 + 安全控制语义，给 2+ 具体失败模式）

- **R1：`audit.Path` 改为「截断后入库」让既有检索行为改变**。失败模式：运维按完整 path
  搜不到那条记录，且**不知道被截断**。缓解：① 只在超长时截断（≤500 字符一字不动）；
  ② `08-部署运维.md` 审计一节写明「path 上限 500 字符，超出部分丢弃」；③ 用例钉住
  「507 字符路径 → 落库 500 字符 + **不丢行**」。
- **R2：`redactedErr.Error()` 剥控制字符改变既有断言的期望值**（集成错误文案被多处断言）。
  失败模式：改动后一批断言变红，被误判为「净化引入回归」而回退 → 洞留着。缓解：§5.1
  先跑全量基线；红了的逐个确认是否为「断言写错了」，**不允许为了让测试变绿而放弃净化**。
- **R3：`StripControl` 连合法的 HTAB 也删**（`\t` < 0x20）。失败模式：日志里表格对齐变形。
  **这是既有两个实现的既定行为**（M28 已上线），保持一致优先；函数注释写明「含 HTAB」
  以免后人当 bug 修（修了就与 `markFailed` 的入库口径分家 → T-52）。
- **R4（假绿风险）**：sqlite 不强制 `VARCHAR(n)`、也不校验 UTF-8，若只写 sqlite 用例，
  §2.3 的修复**看起来也能过**（T-48 同族）。缓解：§5.2 强制真 PG 用例 + **变异反证**
  （去掉截断 → 该用例必须红），且用例必须用**多字节 path**（ASCII 长路径测不出 22021）。
- **R5：`RequestID` 截断到 50 破坏长关联 ID**。失败模式：上游代理发的 correlation id
  >50 字符被截断，跨系统对不齐。缓解：**丢整行比截断关联 ID 严重得多**（取整行优先）；
  全仓 `X-Request-ID` 只读不生成（已 grep），不存在自家生成的超长 ID；文档记明 50 上限。
- **R6：`markFailed` 调序后 `error_msg` 内容变化**，若有用例断言了「带 CR/LF 的错误文本
  原样入库」会变红。缓解：同上 R2 的处理原则；此变化是**修复本身**（旧行为是泄漏）。

## 4. Where

| 文件 | 改动 |
|---|---|
| `backend/internal/redact/redact.go` | 新增 `StripControl`（+ 顺序反例注释） |
| `backend/internal/apierr/apierr.go` | 5xx 行整行净化（B） |
| `backend/internal/middleware/audit.go` | `truncateRunes` + `sanitizeField`，五字段套用（C） |
| `backend/internal/httpx/httpx.go` | `redactedErr.Error()` 净化（D） |
| `backend/internal/integration/zabbix.go` | :238/:242 第三方错误文本净化脱敏（E） |
| `backend/internal/integration/timeparse.go` | :104 `id` 改 `%q`（G） |
| `backend/internal/notification/sender.go` | `stripControlChars` 委托 + 注释（A）；`sanitizeSnippet` 不动 |
| `backend/internal/notification/worker.go` | 四处日志净化 + `markFailed` 调序与注释（F） |
| `backend/internal/api/handlers/auth_handler.go` | `sanitizeAuditUsername` 循环委托（A） |
| 测试 | `redact`/`apierr`（真 socket 回归）/`audit`（含多字节）/`httpx`/`zabbix`/`notification` + 真 PG 冒烟一条 |
| 文档 | `08-部署运维.md` 审计一节补「path 上限 500」；`CHANGELOG.md`；`TODO.md` 结 G-43/G-31①、新增 G-44/G-45；`TRAPS.md` 增补 |

## 5. 验证清单

### 5.1 门槛

- `go test ./...` 全量 + `-race` + `go vet` + `gofmt -l` 空（**先跑基线**，见 R2）。
- 每个修复点至少 1 条**走真实分支**的用例：真 socket（B）、带会话的审计写入（C）、
  多字节 path（C）、带 CR/LF 的上游响应体（D）、Zabbix JSON-RPC 错误文本（E）、
  顺序反例（F：`password=abc\nDEF` → `error_msg` 中**不得**出现 `DEF`）、日志捕获（E/F）。
- 变异反证：删掉每一处净化/调序 → 对应用例**必须红在断言上**（不是编译上，T-31）。
  已知反例形态：只删 `if` 块会留下未使用变量 → 红在编译 → **不算通过**。

### 5.2 真 PG（`scripts/db_smoke.sh` + `tests/`）

- **必须走 middleware 构路由器**，不能像 `db_smoke_test.go:174` 那样直接 `db.Create`
  （`buildAuditEntry` 未导出，直插测不到本次改的代码 → 变异门禁恒不成立）。
- 用例：`RequestID` 60 字符 + 多字节长 path（`"中"*200`）的审计行 **能落库**、
  落库值 ≤ 列宽、**行数不减**（修前：22001/22021 → 丢行）。
- **新用例必须加进 `db_smoke.sh` 的 `-run` 白名单**（T-42：漏了就静默不跑，
  新装轮 :198 / 升级轮 :205 两处）。
- 回归：既有 audit 相关冒烟用例保持绿。

### 5.3 台账动作

- `TODO.md`：G-43 结案（文件:行 + 变异编号）；G-31 残余① 结案；新增 **G-44**
  （审计四字段无截断/错尺子导致丢行 + `markFailed` 脱敏顺序，**本模块交付**）、
  **G-45**（§1.7 同步路径第三方文本长度，登记不修）；**顺带结掉陈旧的 G-33**
  （台账称 DingTalk `SignSecret` 未使用、企微 seed 键错，实测均已实现并有测试 → 台账陈旧）。
- `TRAPS.md`：增补一条——不重复 T-54（脱敏≠转义），写**新的一类**：
  「安全控制的两半（脱敏 / 剥控制字符）**顺序**是语义的一部分，顺序反了会重新组装出
  被控制字符切开的凭据」（rev1 的注释错误 + `markFailed` 的活泄漏都是它的实例），
  与 T-52 互相引用。
- `CHANGELOG.md`：M29 条目 + **行为突变告知**（① 超长路径的审计行从「丢失」变为
  「截断后入库」；② `error_msg` 里被控制字符切开的凭据从「尾部泄漏」变为「完整遮盖」；
  ③ 登录路由带超长 `X-Request-ID` 的请求开始正常留审计行）。

## 6. 不做的事（明确排除，避免范围蔓延）

1. **不重构日志为结构化**（`log.Printf` → `slog`）：收益是长期可观测性，属独立一轮。
2. **不处理 §1.1 #14**（`config`/`database`/`migrate`/`seed` 的日志）：文本源是运维自己的
   配置文件，不是外部输入；净化它们会让「配置里就有换行」的真实问题更难看见。
3. **不给 `slog`/`gin.Logger` 出口加净化**：实测已转义（§1.3），加了是噪声且会掩盖真问题。
4. **不给 `audit.go` 的 `Method` 加净化**：gin 的 method 来自 `http.MethodXxx` 白名单式解析，
   非法 method 在协议层被拒（实施时用一条用例确认，不靠推理）。
5. **不引入新依赖**（不引 `bluemonday` 之类）。
6. **不修 §1.7 的同步路径第三方文本长度**（G-45，独立一轮；本轮只在台账登记）。
7. **不动 `audit.ErrorMsg`**（全仓无 setter 的死路径，按约定「提一句，别删」）。

## 7. 审查记录（三路对抗审查 → 处置）

三路：安全视角、一致性视角、正确性视角。**每条都先自行复现再决定**（复现方式记在「复核」列）。

### rev1 的错误（被审查抓出，已全部改入上文）

| # | rev1 的错误 | 级别 | 复核 | 处置 |
|---|---|---|---|---|
| 1 | §2.2/§2.4 称「脱敏/剥控制字符顺序不重要」 | 阻断 | 真实 `redact.Text` 探针复现（§1.4 表） | 改：顺序是安全边界，**Strip→Text**；并发现 `markFailed` 是**活泄漏** |
| 2 | §1.4 用 `POST /api/assets/<507>` | 高 | 路由表无 `POST /:id` | 改：`PUT`/任意 `:id` 路由 |
| 3 | 「`AuditLog` 只挂在 protected 组」 | 中 | `routes.go:227` 登录路由也挂 | 改：未认证可达，§1.6 重写 |
| 4 | §2.3 按字节截断「修好了」长路径丢行 | 阻断 | 实测 `"中"*200` → `runes=176/validUTF8=false` | 改：`truncateRunes`（§2.3）；§5.2 用例改多字节 |
| 5 | §2.4「一处覆盖 6 个消费点」 | 高 | `zabbix.go:238/242` 是 HTTP 200 裸 `fmt.Errorf` | 改：新增 E 项 + 降级措辞 |

### 审查新发现（rev1 未覆盖，全部复核后**采纳**）

| # | 发现 | 级别 | 复核 | 处置 |
|---|---|---|---|---|
| 6 | `audit.request_id` 无截断 + 列宽 50 + 未认证可达 | 阻断 | `audit.go:101` + `000013:284` + `routes.go:227` | 修（§2.3） |
| 7 | `truncate` 按字节 → UA/username 多字节 22021 | 阻断 | `audit.go:174-178` 实读 | 修（§2.3） |
| 8 | `timeparse.go:104` `id` 用 `%s` | 高 | 实读，`raw` 才是 `%q` | 修（§2.7） |
| 9 | 同步路径第三方文本 → 有长度约束的列，整批回滚 | 高 | `integration/` 零截断（grep 确认） | **登记 G-45，本轮不修**（§1.7） |
| 10 | `worker.go:144/150/157` 的 `ch.Name` 未净化 | 中 | 实读，`ch.Name` 用 `%s` | 修（§2.6） |
| 11 | `sanitizeAuditUsername` 是第三份控制字符实现 | 低 | 实读 `auth_handler.go:42-57` | 委托（§2.1） |

### 驳回

- 「§1.3 的 slog 转义结论不成立」：三路各自独立复现，结论一致（`TextHandler`/默认 handler
  /`gin.Logger()` 均转义；`log.Printf` 不转义）→ **驳回**。
- 「`markFailed` 已三层齐备、无需改动（正确性视角的 ✓ 项）」：只核了**覆盖**未核**顺序**；
  §1.4 的探针表明该顺序会泄漏 → **驳回，按 §2.6 修正**。

## 8. 实现记录

**交付于 2026-09-12。** commit 链：`399a627`（本文档 rev2，三路审查后修订）→ `77ebf3c` A →
`f858479` B → `0ab13b2` C → `fd7ff79` D/E → `bc6930a` F → `ef94e2a` G → `2daae5a` 真 PG 冒烟 →
`613e407` 补一条覆盖用例。

### 8.1 逐项 before / after

| 项 | 位置 | before | after |
|---|---|---|---|
| A | `redact.StripControl`（新增） | 同一份「剥控制字符」判据在 `notification.stripControlChars`（`sender.go`）与 `handlers.sanitizeAuditUsername` 的循环（`auth_handler.go`）里各写一遍；audit 侧没有 | 导出 `redact.StripControl`（rune 迭代，删 `<0x20` 与 `0x7f`）；上述两处改为**委托**（各留一层薄壳，不动调用点） |
| B | `apierr.Respond`（`apierr.go:47-50`） | `Write([]byte("[ERR] " + method + " " + URL.Path + " code=" + code + " internal=" + redact.Text(err) + "\n"))` | `internal := redact.Text(redact.StripControl(err.Error()))`；`line := redact.StripControl("[ERR] " + … + internal)`；`Write(line + "\n")` |
| C | `buildAuditEntry`（`audit.go:92-143`）+ 新增 `truncateRunes`/`sanitizeField`（`audit.go:193-216`） | `Path` 原样（列 500）；`RequestID` 原样（列 **50**）；`UserAgent`/`Username`/`Resource` 走按 **byte** 的 `truncate`；`ErrorMsg` 原样 | 五字段统一 `sanitizeField(v, 列宽)` = `truncateRunes(redact.StripControl(v), n)`（500/50/500/100/1000/100）。**不套 `redact.Text`**：审计是取证材料 |
| D | `httpx.redactedErr.Error`（`httpx.go:49`） | `redact.Text(e.err.Error())` | `redact.Text(redact.StripControl(e.err.Error()))`；函数注释写明覆盖面**只含 httpx 起源的错误** |
| E | `integration/zabbixErrText`（`zabbix.go:211`，新增） | `fmt.Errorf("Zabbix API 错 %d: %s", code, apiResp.Error.Message)`（`:251` 与 `:255` 两处） | 两处都包 `zabbixErrText` = `redact.Text(redact.StripControl(msg))`（HTTP 200 的 JSON-RPC 错误不走 httpx） |
| F | `notification/worker.go` | `markFailed` 顺序 `redact.Text` → `ToValidUTF8` → 按 rune 截断 → `stripControlChars`（**反的**，且注释称「顺序不是安全边界」）；`ch.Name` 三处原样 | `markFailed`：`stripControlChars` → `redact.Text` → `ToValidUTF8` → 按 rune 截断；`ch.Name` 三处（`147`/`154`/`162`，旧行号 144/150/157）过 `stripControlChars`；错误注释换成实测反例 |
| G | `integration/logTimeUnusable`（`timeparse.go:106`） | `log.Printf("M26: %s %s 的 %s=%q 不可用…", what, id, field, raw, st)` | `id` 改 `%q` |

### 8.2 变异反证（逐条红在**断言**上）

T-31 的坑本轮踩了两次：首版 B/C/D/E 四条变异写成「删掉 `redact.` 调用」，结果 `redact` 变成
未使用 import → 红在**编译**上（不算通过）；改成可编译的等价变异后重跑，见下表。

| 变异 | 改法（保证可编译） | 结果 |
|---|---|---|
| M29-MA | `redact.StripControl` 函数体改恒等（`_ = strings.TrimSpace; return s`） | **红**：`TestStripControl_删控制字符保留可打印`（含 `CRLF_伪造行` 子例） |
| M29-MB | B 的整行 `redact.StripControl(` → `redact.Text(` | **红**：`TestRespond_5xx_路径含CRLF不伪造日志行` |
| M29-MC1 | `sanitizeField` 里 `redact.StripControl(s)` → `redact.Text(s)` | **红**：`TestSanitizeField_按字符截断且输出合法UTF8` |
| M29-MC2 | `truncateRunes` 函数体改 `return truncate(s, max)`（退回按 byte） | **红**：`TestSanitizeField_…` + `TestBuildAuditEntry_长路径与超长RequestID被净化` |
| M29-MD | `redactedErr.Error` 去掉 `StripControl` | **红**：`TestDo_错误文本不含URL凭据` |
| M29-ME | `zabbixErrText` 去掉 `StripControl` | **红**：`TestZabbixE2E_APIError_错误文本被净化` |
| M29-MF | `markFailed` 两行调回 `Text → Strip` | **红**：`TestMarkFailed_顺序必须先Strip再Text` |
| M29-MF2 | `no recipient in config` 那处去掉 `stripControlChars(ch.Name)` | **首轮存活** → 见 §8.3，补用例后 **红**：`TestHandleAlertEvent_无收件人日志不伪造行` |
| M29-MF3 | send/resolver 两处**同时**去掉 `stripControlChars(ch.Name)`（`count==2`，只改一处会漏） | **红**：`TestHandleAlertEvent_日志不得被渠道名伪造` |
| M29-MG | `logTimeUnusable` 的 `id` 回 `%s` | **红**：`TestLogTimeUnusable_id被转义` |

### 8.3 首轮存活的变异 → 补测试（`613e407`）

`M29-MF2` 首轮**全绿**：`worker.go:147`「no recipient in config」这条日志此前**零覆盖** ——
把它的 `stripControlChars` 去掉，send/resolver 两条既有用例照旧通过。渠道名由管理员经 API 设置
（`name` 只校验非空），CR/LF 能伪造行式消费的日志行，与另两行**同源同风险**。
补 `TestHandleAlertEvent_无收件人日志不伪造行`（渠道名带 `\r\n`，断言渠道名与消息**同处一行**），
重跑红在断言上（`should not contain "\r"`）。这是「变异反证」而不是「覆盖率」抓出来的缺口。

### 8.4 真 PG（`scripts/db_smoke.sh`）

新增 `TestDBSmoke_AuditFieldTruncation`（`backend/tests/db_smoke_test.go`）：走**真实
`SetupRouter` + `middleware.AuditLog`**（不是直插 —— `buildAuditEntry` 未导出，直插测不到本次改的
代码，变异门禁恒不成立），请求 `/api/assets/` + `"中"×600`、`X-Request-ID` = marker + `\r\n` + 60×`r`；
断言审计行**落库**、`path`/`request_id` 为 500/50 runes、两者均为合法 UTF-8、无 CR/LF、前缀保留。

- **修前**（把 `sanitizeField` 还原成改动前的形态）：`ERROR: value too long for type character varying(500)
  (SQLSTATE 22001)` → 整行 INSERT 被拒 → **行丢失、用例红**。
- **修后**：`✅ 审计字段截断落库: path=500 runes, request_id="trunc-f0b861d6rrrr…rrrr"`（50 字符，前缀保留）。
- 白名单：`db_smoke.sh` 新装轮的 `-run` 已加该用例（T-42：不加就**静默不跑**）。
- 全新 + 升级两条路径全绿；既有 audit 相关冒烟用例保持绿。

### 8.5 门禁

`gofmt -l ./internal ./cmd ./tests` 空；`go vet ./...` 干净；`go build ./...` 通过；
`go test ./... -count=1` 27 包全绿；`./scripts/db_smoke.sh` ✅（全新 + 升级）。
前端本轮零改动，无 `tsc`/`eslint`/`vitest` 影响面。

### 8.6 台账

§5.3 的动作清单已全部执行：`TODO.md`（G-43 结案、G-31 残余① 结案、陈旧的 G-33 结案，
新增 **G-44**（已交付）与 **G-45**（登记不修））、`TRAPS.md` 新增 **T-55**（与 T-52 互引，
明确**不重复** T-54）、`CHANGELOG.md` M29 条目 + 三条行为突变告知、
`08-部署运维.md` §8.4.3 补「控制字符这一半」与审计列宽口径。
