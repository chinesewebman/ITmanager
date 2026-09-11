# FIX-PLAN-HARDENING：安全与运维正确性收口（M28）

状态：需求文档 **rev2**（已过三视角审查，处置见 §7；可进入编码）
日期：2026-09-11
关联台账：TODO.md G-8 / G-11 / G-12② / G-29 / G-34 / G-35 / G-38
关联文档：`FIX-PLAN-TRUSTED-PROXY.md`（G-7，同一个 `ClientIP()` 消费者、同一份网段匹配 helper）、`FIX-PLAN-ERROR-REDACT.md`（G-28/G-34/G-35 的母文档）、`FIX-PLAN-LOG-HYGIENE.md`（G-16，日志卫生）

> **rev2 说明**：rev1 经三视角审查后改动较大，其中 C 项（compose 锚点写法错误 + 服务数 9→10）、E 项（并发用例会假绿）、B 项（会打红现有用例 + 变异不可编译）、F 项（哨兵方案自相矛盾 → 整项降级为登记）为实质性更正。**rev1 的所有"实测"结论均由 rev2 重新独立复现**，未复现者不写入本文档。

---

## 0. 流程缩放决策（显式声明，非静默偏离）

既定工作流是「需求文档 → 三视角审查 → 细节文档 → 再审查 → 编码 → 单测 → 审计 → 推送」。
本模块（M28）由若干**互相独立**的小修组成，其中多数改动量在 20 行以内。

**决定**：本模块**合并为一份需求文档 + 一轮三视角审查**，不再为每个小修单独产出一份细节文档。
理由是细节文档的价值在于「消除语义歧义、给出可执行步骤」，而各项的歧义点已在 §2 逐条钉死（含 before/after 与实测证据），C/D/E 是配置与机械改动，单独再写一遍只增加篇幅不增加信息。

**边界（中止条件）**：三路审查对任一项给出「设计层面未定」结论时，该项**单独拆出**，补细节文档 + 第二轮审查后再动手。
**实际触发情况**：**F 项触发了此条件**（§2.6）——G-34 的修法需重写脱敏规则 3 的值边界，G-35 的修法需在规则 1 与规则 3 之间建立耦合。两项本轮**均不修**，降级为登记。这是 §0 的第一次实际生效，记录在此。

| 项 | 台账 | 类型 | 本轮交付 | 是否需单独细节文档 |
|---|---|---|---|---|
| A | G-11 | 安全控制静默失效 | 复用既有 helper + 接入 + 用例 | 否 |
| B | G-8 | 守卫 fail-open | fail-closed + 用例 | 否 |
| C | G-29 | 运维（磁盘写满） | compose 锚点 + 部署文档 | 否 |
| D | G-12② | 死代码 | 删 1 个私有函数 | 否 |
| E | G-38 | 并发（潜在） | 加锁 + `-race` 用例 | 否 |
| F | G-34/G-35 | 脱敏边界 | **仅登记**（含实测边界表），零代码 | **已拆出**（下一模块） |

---

## 1. 事实更正（先于方案，避免把错误抄进台账）

**G-8 的台账描述有误**。TODO.md:64 写「该中间件靠 `c.GetString("auth_type") == "apikey"` 判定」。
实际代码 `backend/internal/middleware/auth.go:239` 是：

```go
if c.GetString("api_key_id") != "" {
```

`auth_type` **作为 gin context 键**全仓零命中（`grep -rn '"auth_type"' backend/` 无结果）。
（注意：`auth_type` 作为 **SQL 列名**是存在的——`backend/migrations/000001_init.up.sql:22` 的 `users.auth_type`。措辞按「不存在名为 `auth_type` 的 gin context 键」表述，避免被迁移文件反驳。）

结论不变（确实是 fail-open），但判据按 `api_key_id` 重述。§5.3 的台账更新会改掉这句。

---

## 2. 逐项：失败模式 → 修法

### 2.1 A — G-11：API Key 白名单接受 CIDR，鉴权侧按字符串精确比对

**失败模式（具体）**：运维给某 API Key 配置 `ip_whitelist: ["10.20.0.0/16"]`（机房出口网段）。
写入侧 `handlers/api_key_handler.go:82-94` 的 `validateIPWhitelist` 用 `net.ParseCIDR` **接受**该值，保存成功、UI 回显正常。
鉴权侧 `middleware/auth.go:147-161`（实测行号，非 rev1 写的 148-153）：

```go
if len(key.IPWhitelist) > 0 {
    clientIP := c.ClientIP()
    ipAllowed := false
    for _, ip := range key.IPWhitelist {
        if ip == clientIP {        // 字符串精确比较
```

`"10.20.0.0/16" == "10.20.31.7"` 恒假 → 该 Key **永久 403**，报错文案却是「IP地址不在允许列表中」——
运维看到的现象是「我明明把整个网段加进去了，还是被拒」，排查方向被引到网络/代理上（叠加上 G-7 的 XFF 语义，极易误判为「代理没配对」）。
同型还有：`::1` 与 `0:0:0:0:0:0:0:1` 是同一地址、`::ffff:10.20.31.7` 与 `10.20.31.7` 是同一地址，字符串比较全判不等。

**严重性**：**安全控制静默失效**——白名单是访问控制，配了却永不生效的那一半（CIDR）会让运维以为自己收紧了面，实际没有。

#### 修法：复用同包既有 helper，不新写匹配逻辑

`internal/middleware/trusted_proxy_warn.go:47-89` **已经有一份**完整的、被测试钉住的等价实现（G-7 的产物）：

- `parseTrustedNets([]string) []*net.IPNet`（:47-71）：裸 IP → `/32` 或 `/128`，含 `/` 走 `net.ParseCIDR`，非法条目跳过；
- `isTrustedPeer(remoteAddr string, nets []*net.IPNet) bool`（:74-89）：剥 `host:port` → `ParseIP` → 循环 `Contains`（OR），取不到 IP 返回 false（fail-closed）；
- 已有测试 `trusted_proxy_warn_test.go:120` `TestWarnUntrustedForwardedFor_受信表支持裸IP与CIDR`。

**rev1 曾计划在 `internal/apikey` 新写 `IPAllowed`，该计划作废**：A 项唯一的运行时消费者就是 `middleware/auth.go`（handlers 侧只改注释、不需要匹配函数），而 helper 与消费者**同在 `middleware` 包内**。把网络 ACL 解析塞进一个只有 45 行、内容是 HMAC/常量时间比较的包（`apikey.go:26-45`）是混职责，且违反「找最近的既有模式去匹配」。

改动（`middleware/auth.go:147-161`）：

```go
// before
if len(key.IPWhitelist) > 0 {
    clientIP := c.ClientIP()
    ipAllowed := false
    for _, ip := range key.IPWhitelist {
        if ip == clientIP { ipAllowed = true; break }
    }
    if !ipAllowed { apierr.Forbidden(c, "IP地址不在允许列表中"); c.Abort(); return }
}

// after
if !ipAllowedByWhitelist(key.IPWhitelist, c.ClientIP()) {
    apierr.Forbidden(c, "IP地址不在允许列表中")
    c.Abort()
    return
}
```

新增包内薄包装（放在 `auth.go` 或 `trusted_proxy_warn.go` 邻近处，5 行）：

```go
// ipAllowedByWhitelist 判断客户端 IP 是否命中 API Key 白名单。
// 条目可为裸 IP 或 CIDR，解析与匹配复用 G-7 的 parseTrustedNets/isTrustedPeer，
// 口径与 server.trusted_proxies 完全一致。
// 空名单 = 不限制（返回 true）——与调用点原来的 `len(...) > 0` 守卫语义等价。
func ipAllowedByWhitelist(list []string, clientIP string) bool {
    if len(list) == 0 {
        return true
    }
    return isTrustedPeer(clientIP, parseTrustedNets(list))
}
```

**空名单语义必须双保险**：`isTrustedPeer` 对空表返回 **false**（与 `ipAllowedByWhitelist` 想要的 true 相反），差异由上面这层包装吃掉。中间件**不删**原来的 `len(...) > 0` 判断也没意义（包装已处理），统一收进包装函数，调用点从「两层判断」变成「一次调用」。用例必须同时钉住「空名单 → 放行」与「非空且不命中 → 403」。

**不改存量数据**：不把 `10.1.2.3` 重写成 `10.1.2.3/32`。存储形态由匹配逻辑兼容，改写存量数据是无收益的破坏性动作。**但行为会突变**，见 §5.4 的 CHANGELOG 要求。

**过宽网段的护栏：本轮不做，登记为 G-42**（见 §6）。判断依据：API Key 白名单是**运维自选的限制**，不是 `trusted_proxies` 那种信任边界；`/0` 等价于「不限制」，而「不限制」本来就是留空即得的合法配置。若在写入侧拒绝过宽条目，会让存量已存 `/0` 的 Key **无法再被编辑**（改任何字段都过不了校验），是可用性倒退，且超出 G-11 的范围。这与 `validateTrustedProxies`（`internal/config/config.go:267-330`，拒 `/8` 以下 v4、`::ffff:0:0/96`、主机位非零）的差异是**刻意的**，理由记入台账。

**验证（全部可证伪，已实测前置行为）**：
1. **纯函数用例**（`middleware` 包，走 `isTrustedPeer`/`parseTrustedNets` 的既有测试文件）：CIDR 命中、CIDR 不命中、裸 IP 命中、`::ffff:10.20.31.7` 命中 `10.20.0.0/16`、`0:0:0:0:0:0:0:1` 命中 `::1`、空名单放行、空串条目跳过、脏条目跳过、clientIP 非法 → false。
2. **承重用例（端到端，无 PG）**：`routes_integration_test.go` 已有同型用例 `TestRoutes_APIKey白名单按真实客户端IP判定`（:1345，sqlite 内存库 + `setupTestRouter` :101 + `mintWriteKeyWithWhitelist` :1239）。把白名单从单个 IP 改成 `10.20.0.0/16` 形态，断言落在网段内 → 200。**修前该断言必红（403），这是本项的核心反证。**
3. **中间件级用例（sqlmock，无 PG）**：`auth_scope_test.go` 的 `newAPIKeyAuthDB`(:61-68) / `setupAuthEnv`(:88-103) 模式，用 `httptest.NewRequest` 的 `RemoteAddr` 控制 `ClientIP()`。已实测：白名单 `["10.20.0.0/16"]` + `RemoteAddr="10.20.31.7:1234"` 在**修前**返回 `403 forbidden IP地址不在允许列表中`，全程不碰 Postgres。
4. **变异反证**（均为**可编译**形态，遵 T-31）：
   - A-1：把 `ipAllowedByWhitelist` 的 `return isTrustedPeer(...)` 改成 `return list[0] == clientIP`（保留 `len==0` 分支 → 可编译）→ 用例 2 红在断言。
   - A-2：`ipnet.Contains(ip)` → `ipnet.IP.Equal(ip)`（`*net.IPNet` 有导出字段 `IP net.IP`，实测可编译）→ CIDR 用例红在断言。

---

### 2.2 B — G-8：`RejectAPIKeyAuth` 依赖上游中间件，未挂载时静默放行

**失败模式（具体）**：`RejectAPIKeyAuth`（`middleware/auth.go:237-246`）靠 `c.GetString("api_key_id") != ""` 判「当前是 API Key 身份」。该键由 `handleAPIKeyAuth` 设置，而 `handleAPIKeyAuth` 只在 `AuthMiddleware` 内被调用（唯一调用点 `auth.go:87`）。
若有人新增路由组、把写操作端点直接注册到 `r.Group(...)`（或忘了在链上放 `AuthMiddleware`），`api_key_id` 恒空 → 中间件走 `c.Next()` **放行**。该守卫的本意是「长期凭据不得自我复制」（铸造新 Key / 改密），失效后果是泄漏的 write Key 可以无限续期。
当前 6 处用法（`routes.go:251/259/277/279/281/372`）全在 `protected` 组下（`AuthMiddleware` 挂在 `routes.go:240`），故是**潜在陷阱而非现存漏洞**——但这正是最坏的一类：看起来在，实际不设防。

**修法**：fail-closed。判据不是「有没有 `api_key_id`」，而是**「有没有身份」**。

已穷尽核实两条认证路径**都会**设置 `user_id`（非测试代码中仅此两处）：
- JWT 路径 `auth.go:120`：`c.Set("user_id", claims.UserID)`
- API Key 路径 `auth.go:194`：`c.Set("user_id", key.UserID.String())`

且无任何中间件清空/覆盖这些键。因此 `user_id == ""` **在生产路径上**等价于「`AuthMiddleware` 没跑」。这个判据不会误伤合法 JWT 会话。

```go
func RejectAPIKeyAuth() gin.HandlerFunc {
    return func(c *gin.Context) {
        if c.GetString("api_key_id") != "" {
            apierr.Forbidden(c, "API Key 不能执行该操作,请使用登录会话")
            c.Abort()
            return
        }
        // 上游没有建立身份 = AuthMiddleware 没跑（路由挂载顺序错误）。
        // 此时无法区分「JWT 会话」与「匿名」，只能 fail-closed。
        if c.GetString("user_id") == "" {
            apierr.Internal(c, "服务器配置错误",
                fmt.Errorf("RejectAPIKeyAuth 之前没有认证中间件建立身份 (path=%s %s)",
                    c.FullPath(), c.Request.Method))
            c.Abort()
            return
        }
        c.Next()
    }
}
```

**判据的近似性（诚实声明）**：`VerifyToken` 不校验 `claims.UserID` 非空（`auth.go:48-64`），所以一个签名合法但 `user_id=""` 的 token 会命中这个分支。生产不会产生这种 token（`GenerateToken` 传的是 DB user ID），可达性极低，但语义上它是「把鉴权失败错报成服务器故障」。**不为此加代码**（违背最小改动），仅在此承认。若将来要更干净，由 `AuthMiddleware` 显式设 `c.Set("authenticated", true)`。

**依赖补充**：`auth.go` 当前**不 import `fmt`**（已实测 `grep -c "fmt\."` = 0），需在 import 块新增；`apierr.Internal` 签名 `(c *gin.Context, message string, internalErr error)` 已核对（`apierr/apierr.go:89`）。

**关于「路由注册期自检」**：台账待办提过这个选项。**gin 的公开 API 做不到**——`(*gin.Engine).Routes()` 返回的 `RouteInfo` 只有 `Method`/`Path`/`Handler`/`HandlerFunc`，**不暴露路由的中间件链**，无法在启动期断言「使用 `RejectAPIKeyAuth` 的路由前面有 `AuthMiddleware`」。
**处置**：不做静态自检，由上面的**运行时**判据承担（它本质上就是顺序检查，且更强——静态检查看不出「组被重建」这类动态情况）。取舍写进文档而非默默跳过。

**⚠️ 修法会打红一个现存用例，必须同批更新**：`auth_scope_test.go:422-458` `TestRejectAPIKeyAuth` 的「会话身份放行」子例（:428）**既不设 `api_key_id` 也不设 `user_id`**——它模拟的是「AuthMiddleware 已跑、只是 JWT 会话」，但没设身份键。新判据会立刻把它打成 500。
**处置**：同批修改 `auth_scope_test.go:436-441` 的模拟中间件，会话分支补 `ctx.Set("user_id", uuid.NewString())`。这同时成为新判据的**正向对照用例**（模拟 AuthMiddleware 真跑过）。**rev1 漏列了这一条，是审查抓到的。**

**验证**：
1. `auth_scope_test.go` 三子例：空上下文（无任何身份键）→ **500 且 handler 未执行**（修前 200+执行，核心反证）；只设 `api_key_id` → 403；设 `user_id` 不设 `api_key_id`（模拟 JWT）→ 200 且 handler 执行。
2. 「500 且已 Abort」的观测方式：`w.Code == 500` + 链尾探针 handler 的 `reached == false`（沿用该文件 :442-452 既有模式），不需要 `c.IsAborted()`。
3. 用例需重定向 `gin.DefaultErrorWriter`（`old := gin.DefaultErrorWriter; gin.DefaultErrorWriter = io.Discard; t.Cleanup(...)`）——否则 5xx 分支会往 stderr 打 `[ERR] …`（已实测），污染测试输出。
4. **变异反证**（rev1 的两条**都不可编译**——因为 `fmt` 是新增代码里唯一使用点，删掉那段就 `imported and not used`，红在编译上，违反 T-31。已改写为可编译形态）：
   - B-1：`if c.GetString("user_id") == ""` → `if false`（保留 `fmt.Errorf` 那段 → `fmt` 仍被使用）→ 用例 1 红在断言。
   - B-2：`apierr.Internal(c, "服务器配置错误", fmt.Errorf(...))` → `_ = fmt.Errorf("...")` + `c.Next()` → 用例 1 红在断言。

---

### 2.3 C — G-29：容器日志无轮转

**失败模式（具体）**：`docker-compose.yml` 全仓 `logging:` 计数 = **0**（已实测）。
Docker 默认 `json-file` 驱动**不轮转**，日志在 `/var/lib/docker/containers/<id>/<id>-json.log` 单调增长且无上限。
长期运行后宿主盘写满 → postgres 写入失败、容器异常重启、`docker compose logs` 本身不可用。G-16 把 SQL 参数化后单条日志变短，但**没有改变「无上限」**。

**服务清单：10 个，不是 9 个**（rev1 写错，漏了 `mongoDB`）：

| # | 服务 | 行 | profile |
|---|---|---|---|
| 1 | postgres | :14 | 默认 |
| 2 | redis | :37 | 默认 |
| 3 | api | :54 | 默认 |
| 4 | web | :98 | 默认 |
| 5 | netbox | :122 | `aux` |
| 6 | zabbix | :150 | `aux` |
| 7 | glpi | :172 | `aux` |
| 8 | graylog | :195 | `aux` |
| 9 | elasticsearch | :222 | `aux` |
| 10 | **mongoDB** | :240 | `aux` |

漏配 `mongoDB`（Graylog 的依赖）的后果不只是少一个上限——**rev1 设计的校验「`grep -c max-size` == 9」在只配 9 个服务时恰好通过**，等于承重检查自己掩盖了缺陷。

**修法**：YAML 锚点。**锚点自身必须含 `logging:` 键**——rev1 写成裸的 `driver`/`options`，`<<:` 会把锚点的条目**平铺**进服务，于是 `driver` 变成服务的顶层属性。已实测 rev1 的写法报错：

```
validating docker-compose.yml: services.postgres Additional property driver is not allowed
```

正确写法（已实测 `docker compose config` 通过、`logging` 正确嵌套在服务下）：

```yaml
x-logging: &default-logging
  logging:
    driver: json-file
    options:
      max-size: "10m"
      max-file: "5"

services:
  postgres:
    <<: *default-logging
    ...
```

数值 `10m × 5 = 50MB/服务`（10 服务合计 ~500MB 上限）**沿用台账 TODO.md:106 的原始建议**，不是本轮新选的数。

**落点文档**：`08-部署运维.md` 在**仓库根**（不在 `docs/` 下）；其 §8.4.3 已存在且主题是「日志（`log.level` 与 SQL 日志）」（:314），与容器日志轮转不同题。**新增 `§8.4.4 容器日志轮转（json-file 上限）`**，不改动 §8.4.3。TODO.md:106 原文写的「§8.4.3」是台账自带的错，§5.3 一并更正。

**验证**：
1. 前置：`docker compose config` 因 `${NMP_DATABASE_PASSWORD:?}` / `${NMP_AUTH_JWT_SECRET:?}` / `${NMP_AUTH_API_KEY_PEPPER:?}` 强制变量，**裸跑会直接报错退出**（已实测）。检查命令必须先给占位值。
2. 承重检查（robust，不吃锚点回显、不吃 profile）：
   ```bash
   NMP_DATABASE_PASSWORD=x NMP_AUTH_JWT_SECRET=x NMP_AUTH_API_KEY_PEPPER=x \
     docker compose --profile aux config --format json \
     | python3 -c "import json,sys; d=json.load(sys.stdin); \
        n=[k for k,s in d['services'].items() if 'logging' not in s]; \
        print('missing:', n); assert not n, n; print('ok', len(d['services']))"
   ```
   期望 `ok 10`、`missing: []`。已实测 `--format json` 在该 compose 版本可用，且能逐服务判 `logging` 是否存在（**比 `grep -c max-size` 稳**：后者会数到 `x-logging` 锚点的回显，rev1 的期望值 9 在任何跑法下都对不上）。
3. 人工复核 `08-部署运维.md` §8.4.4 的上限值与 compose 一致。

---

### 2.4 D — G-12②：`healthCheck` 死函数

**失败模式（具体）**：`api/routes.go:464` 定义了 `healthCheck`，全仓**零调用点**（已独立复核：`grep -rn 'healthCheck' backend/` 含 `_test.go` 与字符串形式，只命中定义行自身）。健康检查走的是 `/health` 的其它 handler。
风险不是运行时的，是认知的：读代码的人会以为存在一条健康检查逻辑并据此推理（例如「探针会不会因此返回 200」），而它永远不会执行。

**修法**：删除该函数。它是**私有**符号、零引用（删了由编译期证明）。

**验证**：`go build ./...` + `go vet ./...` + 全量 `go test ./...` 绿。

---

### 2.5 E — G-38：`customSenders` 是无同步的包级 map

**失败模式（具体）**：`notification/sender.go:95-104`

```go
var customSenders = map[string]Sender{}
func RegisterSender(channelType string, s Sender) { customSenders[channelType] = s }
func Resolver(ch *models.NotificationChannel) (Sender, error) {
    if s, ok := customSenders[ch.Type]; ok { return s, nil }
    return NewSender(ch)
}
```

消费点**只有 `Resolver`**（:101-103，已穷尽 `grep -rn customSenders`）。当前**生产无写入者**（`RegisterSender` 只在测试里被调用），故非活缺陷。
但它是「将来会被当成插件点」的形状：一旦有人在启动后注册、或测试与 worker goroutine 并发，就是 map 并发读写 → Go runtime 直接 `fatal error: concurrent map read and map write`（**不可 recover**，整个进程挂掉）。

**修法（最小）**：`sync.RWMutex` 包一层（不引入新依赖、不改调用方签名）。项目惯例即 `sync.RWMutex`（`middleware/rate_limit.go:79`、`metrics/metrics.go:38`、`eventbus/eventbus.go:119`、`cache/cache.go:51` 等；全仓无一处对读多写少的包级 map 用 `sync.Map`）。

```go
var (
    customSendersMu sync.RWMutex
    customSenders   = map[string]Sender{}
)

func RegisterSender(channelType string, s Sender) {
    customSendersMu.Lock()
    defer customSendersMu.Unlock()
    customSenders[channelType] = s
}

func lookupCustomSender(channelType string) (Sender, bool) {
    customSendersMu.RLock()
    defer customSendersMu.RUnlock()
    s, ok := customSenders[channelType]
    return s, ok
}
```

`Resolver` 改调 `lookupCustomSender`。

**不 retire `RegisterSender`**（与 D 项删死代码不同类，需在文档里写明，否则读起来像双标）：`healthCheck` 是**私有 + 零引用**（删了编译期可证）；`RegisterSender` 是**导出符号**，是 `Resolver` 的公开入口，有 5 处测试依赖（`notification_test.go:151/195/231/265/554`）。退役它属接口层重构（台账提到的 `WorkerConfig.Resolver` 注入点），见 §6。

**验证**：
1. **并发用例读者必须用 `Resolver`，不能用 `NewSender`**——`NewSender`（`sender.go:77-91`）只按 `ch.Type` 走 switch **从不读 `customSenders`**，rev1 写的「`RegisterSender` + `NewSender`」两者不访问同一内存，`-race` 修前也是绿的 = 假绿。已实测改正后（writer `RegisterSender` + reader `Resolver`）：修前 `-race` 5/5 红、修后 5/5 绿。
2. 用例加 `close(start)` 起跑线 + 多轮循环（本机 8 核下无 barrier 也能稳定检出，但跨 CI/单核需 barrier 才稳）。
3. **测试清理**：现有 `notification_test.go` 用裸 `delete(customSenders, …)`（:152/196/232/266/555）清理，加了锁之后这些是**绕过 mutex 的写**。当前用例串行无害，但新用例必须照既有模式自带 `defer delete(...)`，否则残留已注册 sender 会让后续用例行为漂移；顺带给这些裸 `delete` 加一句注释说明「测试专用、串行执行」。
4. 文件校正：**不存在 `sender_test.go`**，实际是 `notification_test.go`。

---

### 2.6 F — G-34 / G-35：本轮**不修**，降级为登记（§0 中止条件实际触发）

**rev1 的处置是「修 G-35、登记 G-34」，rev2 改为「两个都不修、都登记」。**理由：

**G-34（漏脱敏）**：值以分隔符/引号开头的形态。修它要重写规则 3（`redact.go:63`）的值边界——那正是该规则**声称覆盖**的语义核心，属「安全控制的核心逻辑」。在未过完整需求 + 审查 + 变异矩阵前动它，风险是「把已知的低可达性漏检换成未审计的新绕过面」。

**G-35（过度脱敏）**：rev1 提的哨兵方案，**按 rev1 自己写的 R4 放弃条件已经触发**——rev1 原文自己承认「`\x00` 属于『非空白非引号』，会被值类吃掉，所以哨兵方案需要让规则 3 的值类显式排除它」。**让规则 3 的值类迁就规则 1 的输出字符集，这就是跨规则耦合本身**。且实测：把哨兵插在键位置（`http://token\x00:8080`）会让 `kvSecretRe` 干脆不匹配 → 原串连哨兵一起输出，正是 R4 担心的「哨兵泄漏到最终输出」。
（rev1 §2.6 同段还有一句自相矛盾的话——先说值类「天然排除 `\x00`」、紧接着说「会被值类吃掉」。实测 `\x00` **匹配** `^[^\s"'&,;]+$`，括注是对的、开头那句是错的。该段已删。）

**关于优先级**：G-34 是安全面（凭据可能落日志）、G-35 是可用性面（排障丢端口），rev1 先修可用性面是**优先级倒置**——这点审查说得对。但正解不是「改修 G-34」，而是**本轮两个都不修**：用「保留一个已知的低可达性漏检」换「不引入未审计的绕过面」，这个取舍成立。

**实测边界表（本轮交付物，写入 TODO 与 TRAPS 供下一模块直接使用）**：

| 输入 | `Text()` 实际输出 | 判定 |
|---|---|---|
| `password=&SECRET` | `password=&SECRET` | **漏**（G-34） |
| `password=;SECRET` | `password=;SECRET` | **漏**（G-34） |
| `Authorization: Bearer "SECRET` | `Authorization: Bearer "SECRET` | **漏**（G-34） |
| `token="S` | `token="***` | **不漏**（rev1 误列为漏；规则 3 的可选引号组 `(["']?)` 消费了 `"`，值被正确抹掉） |
| `http://token:8080/x` | `http://token:***` | **过度**（G-35） |

（三条泄漏项已用 /tmp 独立探针实测；`token="S` 一项推翻 rev1 的判断。）

**本轮只做**：把上表写入 TODO.md 的 G-34/G-35 条目与 TRAPS.md，作为下一模块的输入。**不写测试断言当前漏检行为**——那等于把安全缺陷注册成契约（rev1 在此处自相矛盾：既说「断言当前确实漏」又说「不写成绿灯断言」）。也不加 `//go:build` 标签的「期望值测试」，理由是下一模块会连同规则一起重写，届时用例一并写更省事。

---

## 3. Risk

**R1（A 项）— 标准库语义假设。** rev1 担心「`net.IPNet.Contains` 是否真的处理 4-in-6」。
**已实测关闭**：rev1 的 `IPAllowed` 实现（含 `ParseCIDR`+`Contains`+`Equal`）原样跑 8 组输入全部符合预期（`::ffff:10.20.31.7` ⊂ `10.20.0.0/16` = true；`Equal(::1, 0:0:0:0:0:0:0:1)` = true；`10.20.31.7` ⊂ `::ffff:10.20.0.0/112` = true；空 clientIP → false；空名单 → true；脏条目 → false）。GOROOT `net/ip.go:480-484` 的 `Contains` 确有 `if x := ip.To4(); x != nil { ip = x }`；`ip.go:388-390` 的 `Equal` 文档逐字为 "An IPv4 address and that same address in IPv6 form are considered to be equal"。
**且 rev2 复用的是项目内已实测过的 `isTrustedPeer`**（`config.go:307-316` 的注释记录了 `::ffff:0:0/96` 的实测绕过），比新写更稳。**R1 关闭。**

**R2（A 项）— 空名单语义反转。** `isTrustedPeer` 对空表返回 false，若包装函数漏判会让**所有**没配白名单的 Key 被拒（生产大面积 403）。
**缓解**：包装函数第一行 `if len(list) == 0 { return true }`；用例显式钉住「空名单 + 任意 ClientIP → 200」；变异 A-1 若把该行删掉，用例必须红。这是本项最危险的失败模式，因为它**方向朝外**（不是漏放行，是把正常用户全拒）。

**R3（B 项，认证类高风险）— fail-closed 误伤。** 新增 `user_id == ""` → 500，若存在**第三条**认证路径设了身份键但不设 `user_id`，这些端点所有请求立刻 500。
**缓解**：① 已穷尽核实非测试代码中 `c.Set("user_id", …)` 仅 `auth.go:120`/`:194` 两处、`c.Set("api_key_id",…)` 仅 `:197`，无第三条路径、无中间件清空；② 已确认修法会打红**已知的**一个用例（`auth_scope_test.go:428`「会话身份放行」）——这是**预期的**、同批修掉，不是未知误伤；③ 全量 `go test ./...` 兜底；④ B 项单独一个 commit，可独立回滚。

**R4（F 项）— 已由「不修」消除。** 原风险是哨兵方案引入跨规则耦合与输出污染。rev2 直接放弃该方案，风险归零；代价是 G-34 的漏检保留一轮（已在 §2.6 与 §5.4 显式记录，不静默）。

**R5（C 项，基础设施）— 锚点合并与静默漏配。**
**缓解**：`docker compose config` 必须通过（已实测 rev1 的写法会报 `Additional property driver is not allowed`——这个检查**确实能抓住** rev1 的错误，说明它承重）；改用 `--format json` 逐服务判 `logging` 存在性，避免 `grep -c` 数到锚点回显而给出假绿。

**R6（E 项）— 用例假绿。** rev1 的并发用例用 `NewSender` 当读者，根本不访问 `customSenders`，修前也是绿的。
**缓解**：读者改用 `Resolver`；已实测修前 5/5 红、修后 5/5 绿。**这条是「测试写对了名字但没写对语义」的典型，靠实测而非推理发现。**

---

## 4. Where（变更清单）

| 文件 | 项 | 改动 |
|---|---|---|
| `backend/internal/middleware/auth.go` | A、B | A：:147-161 换为 `ipAllowedByWhitelist` 调用 + 新增该包装函数；B：`RejectAPIKeyAuth`(:237-246) 加 `user_id` fail-closed；**import 新增 `fmt`** |
| `backend/internal/middleware/auth_scope_test.go` | A、B | B：:436-441 模拟中间件补 `ctx.Set("user_id", …)`（否则打红）；新增空上下文 500 用例（含 `gin.DefaultErrorWriter` 重定向） |
| `backend/internal/middleware/trusted_proxy_warn_test.go` | A | 纯函数用例（CIDR/裸 IP/4-in-6/空名单/脏条目） |
| `backend/internal/api/routes_integration_test.go` | A | :1345 承重用例改用 CIDR 白名单，断言 200 |
| `backend/internal/api/handlers/api_key_handler.go` | A | **仅注释**（语义不变，仍接受 CIDR） |
| `backend/internal/notification/sender.go` | E | :95-104 `sync.RWMutex` + `lookupCustomSender`；`Resolver` 改调 |
| `backend/internal/notification/notification_test.go` | E | 并发用例（writer `RegisterSender` + reader `Resolver`，barrier + 多轮） |
| `backend/internal/api/routes.go` | D | 删除 :464 `healthCheck` |
| `docker-compose.yml` | C | `x-logging` 锚点（**含 `logging:` 键**）+ 10 个服务 `<<` |
| `08-部署运维.md`（**仓库根**） | C | 新增 §8.4.4 容器日志轮转；不动 §8.4.3 |
| `TODO.md` | G | 结案 G-8/G-11/G-12②/G-29/G-38；G-34/G-35 改为登记（含边界表）；更正 G-8 的 `auth_type`、G-29 的「§8.4.3」；**新增 G-42 / G-43** |
| `docs/TRAPS.md` | G | 新增 T-52 / T-53 |
| `CHANGELOG.md` | G | M28 条目（含**行为突变告知**，见 §5.4） |
| `docs/FIX-PLAN-HARDENING.md` | — | 本文档（§7 审查记录 / §8 实现记录） |

---

## 5. 验证清单与台账动作

### 5.1 门槛（改写为在本项目工具链下**可度量**的形式）
- `go build ./...`、`go vet ./...`、`gofmt -l` 干净。
- `go test ./...` 全包绿；`go test -race ./internal/notification/...` 绿。
- 覆盖率：**Go 标准工具链只有语句覆盖，无分支覆盖**（`-covermode` 仅 set/count/atomic）；CI（`.github/workflows/ci.yml`）当前**没有**覆盖率步骤。因此门槛落实为**改动符号的语句覆盖**：
  ```bash
  cd backend && go test -coverprofile=/tmp/c.out ./internal/middleware/... ./internal/notification/... \
    && go tool cover -func=/tmp/c.out | grep -E 'ipAllowedByWhitelist|RejectAPIKeyAuth|lookupCustomSender'
  ```
  `ipAllowedByWhitelist`/`lookupCustomSender` 目标 **100%**（薄函数，表驱动可穷尽）；`RejectAPIKeyAuth` ≥80%。**「分支覆盖 ≥80%」这一指标本项目无法度量，以「语句覆盖 + 变异反证」替代**——这是既有事实，本轮显式写明，不再留一个测不了的指标。
- **每条新断言都要有对应的可编译变异反证**（M28-M1..Mn），逐条确认红在**断言**上、不是红在编译上（T-31，`docs/TRAPS.md:147`）。

### 5.2 真 PG / compose 侧
- C 项：§2.3 的 `--format json` 检查通过（`ok 10`、`missing: []`）。
- A/B/E 项：**不需要 PG**（A 走 sqlmock 或 sqlite 集成路由，B 纯 context，E 纯内存），**不新增** `scripts/db_smoke.sh` 白名单；本轮**无迁移**，`db_smoke.sh` 完全不改。
- 收尾跑一次 `scripts/db_smoke.sh` 作为无回归证明（不改它，只跑）。

### 5.3 台账动作（G）
- TODO.md 结案 5 项（G-8/G-11/G-12②/G-29/G-38），逐项写明「已交付 + 文件:行 + 变异编号」。
- **更正**：G-8 的 `auth_type` → `api_key_id`；G-29 的「§8.4.3」→「§8.4.4（仓库根 `08-部署运维.md`）」。
- G-34/G-35 由「未修」改为「**已登记、待独立模块**」，附 §2.6 的实测边界表与所需前置（需求 + 审查 + 变异矩阵）。
- **新增 G-42**：API Key 白名单无「过宽网段」护栏（`/0`、`::ffff:0:0/96`），与 `validateTrustedProxies` 的既有规范不一致。本轮**刻意不做**，理由见 §2.1（写入侧拒绝会让存量 `/0` 的 Key 无法编辑，是可用性倒退，超出 G-11 范围）。
- **新增 G-43**：`apierr.Respond` 的 5xx 日志行里 `c.Request.URL.Path` **未净化**且**用户可控**（`redact.Text` 只作用于 `internalErr`）。实测 `%0d%0a` 会解码成真实 CR/LF，可向 `gin.DefaultErrorWriter` 伪造日志行。与项目既有的 `sanitizeAuditUsername`(`handlers/auth_handler.go:42`)、`stripControlChars`(`notification/sender.go:150`) 规范冲突。影响**所有** 5xx（不只 B 项），超 M28 范围 → 独立登记。第二处同形态：`middleware/audit.go:54/97` 把 `URL.Path` 原样入 `Path` 列。
- TRAPS.md 新增：
  - **T-52**「安全控制的两侧语义必须同源」——写入侧接受 CIDR、校验侧字符串比较，是「控制存在但静默失效」的典型；判据：**任何 accept 某格式的校验器，都要有一处运行时按该格式消费它的证据**（A 项）。
  - **T-53**「fail-open 的守卫不要用『某键非空』当正向判据」——要用「身份是否已建立」正向判据，否则守卫的失效模式与被守卫的漏洞同构（B 项）。

### 5.4 行为突变告知（写入 CHANGELOG，零代码成本）
A 项修完后，**存量**库里此前因字符串比较而**永不命中**的 CIDR 白名单条目**将开始生效**。请复核现有 API Key 的 `ip_whitelist` 是否仍是期望值。
具体惊讶场景：某 Key 的 `ip_whitelist` 历史上随便填了 `0.0.0.0/0`（反正不生效），升级后该 Key 立刻可从任意地址使用。
CHANGELOG 的 M28 条目必须显式写这一段。

### 5.5 提交粒度（按既定要求：每个阶段性步骤单独 push）
1. 本文档 rev2 + §7 审查记录
2. A 项：包装函数 + 接入 + 三类用例 + 变异
3. B 项：fail-closed + 修既有用例 + 变异（**单独 commit，可独立回滚**）
4. C 项：compose 锚点 + 08 文档 §8.4.4
5. D + E 项：死函数删除 + map 竞态（各自独立可回滚，合一次推）
6. 台账：TODO/TRAPS/CHANGELOG（含 G-42/G-43 新增与行为突变告知）

F 项无代码，不单独占一次推送。

---

## 6. 边界（本轮不做的事）

| 项 | 为什么不做 |
|---|---|
| **G-34 / G-35** | §0 中止条件触发（修法需跨规则耦合 / 重写安全控制核心）；§2.6 已登记 + 实测边界表 |
| **G-42**（新）过宽网段护栏 | 写入侧拒绝会让存量 `/0` 的 Key 无法编辑（可用性倒退）；API Key 白名单是运维自选的限制、非信任边界。已登记 |
| **G-43**（新）`apierr` 日志行的 `URL.Path` 未净化 | 影响所有 5xx，属日志卫生范畴（G-16/G-28 的延续），超 M28「收口」范围。已登记 |
| **G-39** `AlertRule.NotifyChannels` 只写不读 | 需先定义「告警命中哪条规则」的语义（优先级/覆盖/合并），是**产品语义决策**，需用户拍板 |
| **R1/R2/R3（v3 架构）** | 破坏性（drop `lines` 表），须单独许可 |
| **G-40** 凭据静态加密 | 需要密钥管理方案（KMS/环境变量轮转），独立模块 |
| **G-5** JWT 不回查 DB | 改认证主路径语义（吊销延迟），属安全策略决策 |
| `RegisterSender` 退役改注入 | 接口层重构；E 项只做加锁。`RegisterSender` 是导出符号 + 有测试依赖，与 D 项的私有死函数不同类（§2.5） |

---

## 7. 审查记录（三视角 → 处置，2026-09-11）

三路独立只读审查（正确性/可编译性、测试承重力、安全/一致性），各自带实测探针。以下为**处置表**；每条均已由我**独立复现**后才采纳（未复现者不采纳）。

### 7.1 阻断项（全部采纳并已改文档）

| # | 来源 | 发现 | 复现 | 处置 |
|---|---|---|---|---|
| 1 | 正确性 + 测试（各自独立命中） | §2.3 的 YAML 锚点写法错误：裸 `driver`/`options` 经 `<<:` 平铺成服务顶层属性，`docker compose config` 报 `Additional property driver is not allowed` | ✅ 我在 /tmp 复现：原写法报错；改为锚点内含 `logging:` 后通过 | §2.3 改正写法 + 给出实测通过的命令 |
| 2 | 正确性 + 测试 + 安全（三路独立命中） | 服务数不是 9 是 **10**（漏 `mongoDB`）；且 rev1 的 `grep -c max-size == 9` 期望值在任何 profile/锚点回显组合下都对不上，**承重检查会掩盖漏配** | ✅ 静态 `grep` 确认 10 个服务、6 个 `aux` profile；实际跑 `--profile aux config --services` = 10 | §2.3 改 10 + 换用 `--format json` 逐服务判 `logging` |
| 3 | 测试 | §2.5 的并发用例写成 `RegisterSender` + `NewSender`，而 `NewSender` **不读** `customSenders` → 修前也绿 = 假绿 | ✅ 读 `sender.go:77-104`：`NewSender` 是纯 switch，读点在 `Resolver` | §2.5 改读者为 `Resolver`，并记录实测（修前 5/5 红、修后 5/5 绿） |
| 4 | 测试 | §2.2 的 fail-closed 会打红**现存**用例 `TestRejectAPIKeyAuth`「会话身份放行」（它既不设 `api_key_id` 也不设 `user_id`） | ✅ 读 `auth_scope_test.go:422-458` 确认 :428 的 `apiKeyID=""`、:436-441 只设 `api_key_id` | §2.2 加「⚠️ 会打红，必须同批更新 :436-441」+ §4 变更清单 + §5.5 单独 commit |
| 5 | 测试 | §2.2 的两条变异**都不可编译**（`fmt` 是新增代码里唯一使用点，删段即 `imported and not used`），红在编译上，违反 T-31 | ✅ 确认 `grep -c "fmt\." auth.go` = 0 | §2.2 改写为 `if false` / `_ = fmt.Errorf(...)` 两个可编译形态 |

### 7.2 高（采纳）

| # | 来源 | 发现 | 复现 | 处置 |
|---|---|---|---|---|
| 6 | 安全 | **A 项复用错了位置**：项目已有 `parseTrustedNets`/`isTrustedPeer`（`trusted_proxy_warn.go:47-89`，同包、有测试），rev1 却要新写 `IPAllowed` 放进只有 45 行的 `apikey` 包；且「handlers 也 import apikey」是**空论据**（handlers 要的是 `Hash` 不是网段匹配） | ✅ 读源码确认 helper 与消费者**同在 `middleware` 包**，语义逐条等价 | §2.1 整段重写为「复用」；`IPAllowed` 计划作废 |
| 7 | 安全 | **优先级倒置 + R4 已触发**：rev1 先修可用性面（G-35）、推迟安全面（G-34）；且哨兵方案按 rev1 自己写的 R4 放弃条件已经满足（需让规则 3 的值类迁就规则 1 的输出） | ✅ 读 rev1 §2.6 原文，确认同段自相矛盾（「天然排除 `\x00`」vs「会被值类吃掉」） | §2.6 整段重写为「本轮两个都不修、都登记」，附实测边界表 |
| 8 | 安全 | C 项服务数漏 `mongoDB` 会让承重检查静默漏配（同 #2，安全视角给出不同后果论证） | 同 #2 | 同 #2 |

### 7.3 中（采纳）

| # | 来源 | 发现 | 复现 | 处置 |
|---|---|---|---|---|
| 9 | 正确性 + 安全 + 测试（三路） | G-34 边界表里的 `token="S` **实际并不漏**（规则 3 的可选引号组 `(["']?)` 消费了 `"`） | ✅ 探针实测：`token="S` → `token="***` | §2.6 边界表改为 3 条漏检 + 明确标注该项 rev1 误判 |
| 10 | 正确性 + 测试 | 多处行号错位：`user_id` 设置在 `auth.go:120`/`:194`（rev1 写 113/197，且 :197 实为 `api_key_id`）；白名单块 147-161（rev1 写 148-153）；`customSenders` :95-104（rev1 写 93-96） | ✅ 逐条 grep 复核 | 全文改用实测行号 |
| 11 | 正确性 + 测试 | 测试文件名错：不存在 `auth_test.go`/`sender_test.go`，实为 `auth_scope_test.go`/`notification_test.go` | ✅ `ls` 确认 | §4 变更清单改正 |
| 12 | 正确性 + 安全 | 目标文档错：`docs/08-部署运维.md` 不存在，实际在**仓库根**；其 §8.4.3 已存在且主题是 `log.level`/SQL 日志 | ✅ `ls` + `grep -n '^#### 8\.4'` 确认 | §2.3 落点改 **§8.4.4**，路径改仓库根；并在 §5.3 更正 TODO 里的同款错误 |
| 13 | 安全 | `apierr.Respond` 5xx 行的 `c.Request.URL.Path` 用户可控且未净化（CRLF 可伪造日志行），与既有 `sanitizeAuditUsername`/`stripControlChars` 规范冲突 | 采纳为**新登记项 G-43**（超 M28 范围，未在 /tmp 逐字复现 CRLF，标注为「审查实测、本轮未复现」） | §5.3 新增 G-43 |
| 14 | 安全 | 过宽网段（`/0`、`::ffff:0:0/96`）无护栏，与 `validateTrustedProxies` 既有规范不一致 | ✅ 读 `config.go:267-330` 确认既有护栏存在 | **刻意不做**，新增 **G-42** 登记 + §2.1 写明取舍理由（可用性倒退） |
| 15 | 测试 | §5.1「分支覆盖 ≥80%」在 Go 工具链下不可度量（`-covermode` 只有语句级；CI 无覆盖率步骤） | ✅ 读 `.github/workflows/ci.yml` 与 `Makefile` | §5.1 改写为「改动符号语句覆盖」+ 可执行命令，并显式声明以语句覆盖 + 变异替代分支覆盖 |

### 7.4 低（采纳）

| # | 发现 | 处置 |
|---|---|---|
| 16 | §1「`auth_type` 整个仓库不存在」不准确——它作为 **SQL 列**存在于 `000001_init.up.sql:22` | §1 措辞改为「不存在名为 `auth_type` 的 gin context 键」 |
| 17 | D 项删私有死函数 vs E 项保留导出死符号，像双标 | §2.5 写明两类死代码的区别（私有+零引用 vs 导出+测试依赖） |
| 18 | B 的 `user_id` 判据把「中间件没跑」与「token 带空 user_id」混为一谈（`VerifyToken` 不校验 `claims.UserID` 非空） | §2.2 加「判据的近似性」诚实声明；不为此加代码 |
| 19 | 并发用例的测试清理若走裸 `delete(customSenders,…)` 会绕过新 mutex | §2.5 验证点 3 要求新用例自带清理 + 给既有裸 `delete` 加注释 |
| 20 | `10m × 5` 无容量论证 | §2.3 说明该数值**沿用台账 TODO.md:106**，非本轮新选 |
| 21 | §2.2 末「这条日志不会带可控内容」的断言过宽（新增字段不可控，但同行 `URL.Path` 可控） | 措辞收窄为「新增的 `FullPath()`/`Method` 不可控」+ 引出 G-43 |

### 7.5 未采纳 / 保留

| 项 | 理由 |
|---|---|
| 安全审查建议「给 `RegisterSender` 加包内 `unregisterSender` 测试助手」 | 超出最小改动；改为在既有裸 `delete` 处加注释（#19） |
| 测试审查建议「G-34 用 `//go:build g34_unfixed` 标签写期望值测试」 | 下一模块会连同规则一起重写，届时用例一并写更省事；本轮只登记边界表（§2.6） |
| 安全审查「R1 可降级为与 config 包同口径回归」 | 采纳方向（改复用 helper），但 R1 仍保留在 §3 作为**已关闭**项记录，因为「标准库语义」与「复用谁的实现」是两个独立断言 |

### 7.6 审查共识

三路一致：**没有需要推倒重来的设计缺陷**（A 的标准库语义、B 的判据健全性、D 的死函数判定、E 的加锁惯例均经独立复核为真），但有 5 条会直接导致**假绿或假红**的可执行性缺陷（7.1 全部），已逐条改文档。§0 的中止条件被 F 项触发并已按约定处置，合并为一份文档的决策本身在其余各项上成立。

---

## 8. 实现记录

（待填）
