# FIX-PLAN-LOG-HYGIENE：日志不泄漏凭据 / 级别配置真正生效（TODO G-16）

- **状态**：**rev3 — 已实现并验证**（12 项变异反证全红、`go test ./...` 26 包绿、`go vet` / `gofmt` 干净）→ 已合入 main
- **日期**：2026-09-09
- **关联**：TODO G-16；由 compose 轮审查发现，2026-09-09 安全审计补实证（P4）
- **影响面**：**所有部署形态**（裸机 / compose）默认 `log.level: info` 下把**每条 SQL 与参数展开值**写进容器日志；转发到 Graylog/Splunk 后，只读日志账号即可拿到可离线爆破的凭据材料

## 1. 问题（What / Why）

### 1.1 现象（全部有实测证据）

| # | 证据 | 位置 | 内容 |
|---|---|---|---|
| E-1 | 建号语句带**完整 bcrypt 哈希** | `/tmp/smoke-compose2.log:184` | `INSERT INTO "users" (…,"password_hash",…) VALUES ('smokeadmin','$2a$10$SElqtKSJ…')` |
| E-2 | 鉴权查询带 **API Key 哈希** | `internal/middleware/auth.go:134` | `SELECT … WHERE key_hash = '<sha256>'` —— 哈希是「可验证的凭据材料」，可离线校验猜测 |
| E-3 | 改密语句带**新哈希** | `internal/api/handlers/auth_handler.go:280`（赋值）→ `:284`（`database.DB.Save`） | `UPDATE users SET password_hash='$2a$10$…'` |
| E-4 | ANSI 转义混入日志 | 同上日志行 | `\x1b[0m\x1b[33m[4.777ms]\x1b[34;1m[rows:1]\x1b[0m`（`Colorful: true`） |
| E-5 | **gin Recovery 把 Cookie 明文打进日志** | `gin@v1.9.1/recovery.go:74-91` | `httputil.DumpRequest`（:74）只屏蔽 `Authorization`（:76-81），**Cookie 完整保留**；本项目浏览器端凭据正是 httpOnly cookie `auth_token`（`auth_handler.go:137` 种、`frontend/src/services/api.ts:17` 带），默认 `server.mode: debug`（`config.go:155`、`docker-compose.yml:67`）→ 任意 handler panic 即把**可直接重放的 JWT** 写进日志 |
| E-6 | seed 主动打印默认密码 | `cmd/seed/main.go:74,94,107` | `创建管理员用户: admin (密码: admin123)`；`Makefile:195` 的 `make deploy` 会执行它 |

E-5 比 E-1 更严重：bcrypt 哈希要离线爆破，JWT 是**直接重放**。

### 1.2 根因（四层）

1. **级别层**：`internal/database/database.go:38-46` 把 `logger.Config.LogLevel` **硬编码** `logger.Info` —— gorm 在 Info 级别对**每条**语句 `Trace`，与 `cfg.Log.Level` 无任何连接。
2. **参数层（普通路径）**：`ParameterizedQueries` 未设置 → 零值 `false` → `callbacks.go:136-139` 命中 `(*logger).ParamsFilter`（`logger.go:193-198`）返回原参数 → `Dialector.Explain` 展开。
3. **参数层（`Scan` 路径，审查发现）**：`(*DB).Scan` 会把 logger 换成 `traceRecorder`（`finisher_api.go:527-533`），而 `traceRecorder.ParamsFilter` 走**包级** `RecorderParamsFilter`（`logger.go:220-225`），默认是恒等 no-op（`logger.go:85-88`）→ 参数在记录阶段就被展开，随后由真实 logger 原样打印。**仅设 `ParameterizedQueries: true` 挡不住这条路**。本仓库生产代码有该路径：`internal/service/topology_service.go:74`、`dashboard_service.go:119,163,179,191,222`、`alert_service.go:174,186`。
4. **输出层**：gorm logger 用 `log.New(log.Writer(), …)`（标准库 `log`，默认 **stderr**），与 `pkg/logger`（stdout + 自己的级别过滤）**完全两条路** → `logger.Init(&cfg.Log)` 对 SQL 日志零作用。这是「release 也没关」的答案。

### 1.3 同族缺陷（同一次审查发现，同一主题，本轮一并最小修）

| # | 缺陷 | 证据 | 后果 |
|---|---|---|---|
| H-1 | `pkg/logger` 级别过滤**完全失效** | `pkg/logger/logger.go:44` 原样存 `cfg.Level`；`:140` 的 `order` 只有大写键 → 配置 `level: "info"`（`backend/config.yaml:88`）时 `order["info"]==0` 等价 DEBUG，**所有 DEBUG 行照打** | 项目自身日志不受控；且与 gorm 的映射表词汇分叉 |
| H-2 | gin 中间件**重复挂载** | `routes.go:118` 的 `gin.Default()` 已含 Logger+Recovery，`:143-144` 又 `Use` 一遍 | 每条请求两行 access log、panic 被恢复两次（E-5 的 dump 也执行两次） |
| H-3 | `log.level` **无默认值** | `config.go:153-166` 的 `SetDefault` 不含它；缺键时 `cfg.Log.Level == ""` | 自定义 config.yaml 下两条日志路径都退化为最啰嗦档 |

### 1.4 为什么一直没被发现

- 本地看全量 SQL「有用」，`log.level: info` 与 gorm 的 Info 看起来一致，没人核对是否同一条路；
- 单测走 sqlmock / `SetDBForTest`，**只有 `internal/database/database_test.go:120` 直接调 `Init`**，而它只断言连接失败文案，不碰 logger 配置；
- 容器日志无人看，直到 compose 冒烟落盘才发现（E-1）。

### 1.5 危害定性

| 场景 | 后果 |
|---|---|
| 日志落盘/转发（compose 默认 + 常见采集） | bcrypt 哈希离线爆破；API Key 哈希可离线验证；**JWT 可直接重放**（E-5） |
| PCI DSS 4.2/3.3 类要求 | 「认证凭据不得出现在日志中」——当前**直接违规** |
| 噪声 | 每条 SQL 一行 + 双份 access log，磁盘与检索成本双输 |

## 2. 方案（How）

### 2.1 候选对比

| # | 方案 | 取舍 |
|---|---|---|
| A | 只把 `LogLevel` 接到 `cfg.Log.Level` | 慢查询日志仍带展开参数 → 敏感值照样落日志 → **不够** |
| B | 只加 `ParameterizedQueries: true` | **`Scan` 路径仍泄漏**（§1.2-3）→ 不够 |
| **C** | **B + `RecorderParamsFilter` 去参数 + 级别接配置 + 关 ANSI** | ✅ 采用：gorm 生成的 SQL 文本永不含参数值，级别跟配置走 |

### 2.2 定案（可执行细节）

1. **`ParameterizedQueries: true` + `RecorderParamsFilter` 去参数，两者都做、都不做开关。**
   任何「能把它翻成泄漏」的配置键都是定时炸弹。排障需要具体值时用 psql 手工复现（写进 `08-部署运维.md`）。
   `RecorderParamsFilter` 是 gorm 官方提供的覆盖点（`logger.go:84` 注释原文「allows to be run-over by a different implementation」），在 `Init` 里设一次为 `func(ctx, sql, params...) (string, []interface{}) { return sql, nil }`。
2. **级别映射**（纯函数，**名字避开包级变量**）：

   | `cfg.Log.Level`（先 `ToLower(TrimSpace(...))`） | gorm 级别 | 行为 |
   |---|---|---|
   | `debug` | `Info` | 逐条打印 SQL 骨架 |
   | `info` / `warn` / `""`（未配置） | `Warn` | 只打慢查询 + 错误 |
   | `error` | `Error` | 只打错误 |
   | 其它（含拼写错误） | `Warn` | 安全兜底，**不**降级成 Info |

   命名：函数 `mapGormLogLevel(level string) gormlogger.LogLevel`；包级变量 `var gormLogLevel = gormlogger.Warn`（**必须显式初始化**——gorm 的零值是 `Silent`=1 之前的 0，`Trace` 首行 `LogLevel <= Silent` 直接 return，会比 Warn 更静默，违反 R-1 的缓解）。
3. **可测的 config 构造**：`gormLoggerConfig(level gormlogger.LogLevel) gormlogger.Config` 返回 `{SlowThreshold: time.Second, LogLevel: level, IgnoreRecordNotFoundError: true, ParameterizedQueries: true, Colorful: false}`（级别→gorm 级别的映射发生在 `mapGormLogLevel`，调用点在 `newGormLogger`）。`Init` 用它建 logger。
   （审查发现：`gormlogger.New` 返回接口，测试**拿不到** `Config` 字段，所以必须把配置构造抽成纯函数才能断言。）
4. **注入方式**：包级 setter `database.SetGormLogLevel(level string)`，照抄既有 `SetMigrationsFS` 模式（`database.go:22-27`：导出/包级变量 + 极简 setter，无锁，靠「main 单 goroutine 在 Init 前调用」的隐式契约）。**不改 `Init` 签名**，避免 5 个调用点 + 测试的连锁改动。
5. **5 个调用点各加一行** `database.SetGormLogLevel(cfg.Log.Level)`（`Init` 之前，`cfg` 均已可用）：
   `cmd/server/main.go:40`、`cmd/seed/main.go:26`、`cmd/migrate/main.go:35`、`cmd/admin-bootstrap/main.go:54`、`cmd/set-role/main.go:50`。
   （E-1 的行由 `admin-bootstrap` 创建，见 `scripts/smoke-compose.sh:99-101`，不是 seed。）
6. **gin 收口**：`routes.go:118` 的 `gin.Default()` → `gin.New()`，并在原 `:143-144` 位置显式挂一次 `gin.Logger()` + `middleware.Recovery()`（新文件 `internal/middleware/recovery.go`：**手写 `defer/recover`**——`gin.CustomRecovery` 也挡不住 dump，原因见 §8.2-2；只记 panic 值 + `debug.Stack()` + request_id/path/method，**不 dump 请求头**，返回 500）。这样 access log 只剩一行、panic 只恢复一次、Cookie 不再入日志。
7. **seed 去密码**：三条日志删掉括号里的明文，改为「默认演示密码，首次登录强制改密」（密码仍在文档/代码常量里，不因日志而丢失可发现性）。
8. **`pkg/logger` 级别归一化**：`Init` 里 `level = strings.ToUpper(strings.TrimSpace(cfg.Level))`，空串落 `"INFO"`（与 config 默认一致）。同时 `config.go` 补 `viper.SetDefault("log.level", "info")`（顺带修好 H-3 与 `config.go:157-160` 记录的 env 覆盖坑）。
9. `SlowThreshold` 保持 1s、`IgnoreRecordNotFoundError` 保持 true（与本缺陷无关，不动）。

## 3. Where（变更清单）

| 文件 | 改动 |
|---|---|
| `backend/internal/database/gorm_logger.go`（新） | `mapGormLogLevel` / `gormLoggerConfig` / `RecorderParamsFilter` 覆盖 + 「为什么恒定参数化」注释 |
| `backend/internal/database/database.go` | 包级 `gormLogLevel` + `SetGormLogLevel`；`Init` → `newGormLogger(log.Writer())` → `gormLoggerConfig(gormLogLevel)` |
| `backend/internal/database/gorm_logger_test.go`（新） | 级别映射表 + config 字段断言 + 端到端「有骨架/无参数值」双路径（普通查询 + `Scan`） |
| `backend/internal/middleware/recovery.go`（新） | `Recovery()`：**手写 recover** + slog + `debug.Stack`，不 dump 头 |
| `backend/internal/middleware/recovery_test.go`（新） | panic 路由：断言 500、日志含 panic 值、**不含** Cookie/Authorization 值 |
| `backend/internal/api/routes.go` | `gin.Default()`→`gin.New()`；`gin.Logger()` + `middleware.Recovery()` 移到链首（Logger 在外、Recovery 在内，见 §8.4 A-2），删掉重复挂载 |
| `backend/pkg/logger/logger.go` | `Init` 级别归一化（ToUpper/TrimSpace，空串→INFO）；`shouldLog` 未知级别按 INFO（fail-closed，见 §8.4 A-1） |
| `backend/pkg/logger/logger_test.go` | 补「小写 info 能过滤 DEBUG」「空串→INFO」「未知级别 fail-closed」用例 |
| `backend/internal/config/config.go` | `viper.SetDefault("log.level", "info")` |
| `backend/internal/config/config_test.go` | 补「缺 log.level 键时默认 info」用例 |
| `backend/cmd/{server,seed,migrate,admin-bootstrap,set-role}/main.go` | 各加一行 `database.SetGormLogLevel(cfg.Log.Level)` |
| `backend/cmd/seed/main.go` | 3 条日志去掉明文密码 |
| `08-部署运维.md` | 新增 `#### 8.4.3 日志（log.level 与 SQL 日志）`（插在 `### 8.5 备份方案` 之前） |
| `TODO.md` | G-16 结案；新增 G-28/G-29/G-30（见 §6） |
| `docs/TRAPS.md` | 新增 T-32（两条日志路径互不相干、级别配置看着一致实则无效）、T-33（库级钩子绕过 `ParameterizedQueries`） |

**不动**：`audit_logs`（不存 body/头，已核实）、`httpx`（不主动打日志，但它的错误文本是 G-28 的输入）、`apierr`（G-28）。

## 4. 验证清单

| 编号 | 验证项 | 手段 | 变异反证 |
|---|---|---|---|
| L-1 | 映射表：`debug→Info`、`info/warn/""→Warn`、`error→Error`、`INFO`（大写）/` info `（带空格）→`Warn`、非法→Warn | 表驱动 | 把 `""` 改 `Info` → 红 |
| L-2 | 包级默认值 = `Warn`（不是零值 `Silent`） | 断言 `gormLogLevel` | 去掉显式初始化 → 红 |
| L-3 | `gormLoggerConfig` 恒定 `ParameterizedQueries: true` + `Colorful: false` + `SlowThreshold: 1s` | 纯函数断言 | 改回 false → 红 |
| L-4 | **端到端（普通路径）**：`LogLevel: Info` 的 logger + sqlite 内存库 + 手写 DDL，`db.Exec` 插一条含 `$2a$10$SECRET` 的值 → 日志**含 `INSERT INTO` 骨架**且**不含 `$2a$`** | `gorm_logger_test.go` | 只断言「不含」= 空转（T-31）；把 `ParameterizedQueries` 改 false → 红 |
| L-5 | **端到端（`Scan` 路径）**：`db.Raw("SELECT ... WHERE password_hash = ?", secret).Scan(&dst)` → 日志不含 `$2a$` | 同文件 | 删掉 `RecorderParamsFilter` 覆盖 → 红 |
| L-6 | gin：panic 路由返回 500，日志含 panic 值、**不含** Cookie 值 | `recovery_test.go`（httptest + 捕获 slog） | 换回 `gin.Recovery()` → 红 |
| L-7 | `pkg/logger`：`Init(&LogConfig{Level:"info"})` 后 `Debug` 不输出、`Info` 输出；`Level:""` 同 INFO | 现有测试文件 | 去掉 ToUpper → 红 |
| L-8 | 配置默认：缺 `log.level` 键 → `cfg.Log.Level == "info"` | config 测试 | 去掉 SetDefault → 红 |
| L-9 | 无回归：`go vet`、`go test ./... -count=1`、`db_smoke.sh`、`smoke-compose.sh` + `docker compose logs api \| grep -E '\$2a\$|key_hash = '` 无命中 | 本地 | — |

## 5. Risk

- **R-1（可观测性下降，中）**：看不到参数值。
  **缓解**：`log.level: debug` 仍打印**语句骨架**（表名/列名/WHERE 形态齐全）；需要具体值时 psql/EXPLAIN 手工复现（写进 `08-部署运维.md`）；慢查询仍打印（参数化）。
- **R-2（漏改调用点，低）**：CLI 忘了调 `SetGormLogLevel` → 默认 `Warn`（安全侧），少打日志而非多泄漏；§3 列全 5 个点，实现后 grep 复核。
- **R-3（残留面：错误文本，中）**：`ParameterizedQueries` 只管 SQL 文本，**`err` 不经它过滤**。已证实的旁路：① gorm 错误路径原样打印 `err`（`logger.go:167-172`），pgx 编码错误自带值（`unable to encode "abc" into binary format for uuid`，`pgtype.go:1905`），触发点如 `First(&x, "id = ?", c.Param("id"))`；② `apierr.Respond` 把内部 err 写 stderr（`apierr.go:36-41`），PG 冲突 message 含 `Key (col)=(value)`；③ 钉钉 webhook `access_token` 在 URL 里 → `notification/sender.go:135-140` 的 `*url.Error` → `worker.go:153` 日志 + `:280` 落 `notification_logs.error_msg`；④ `httpx.go:167` 把上游响应体拼进 err。
  **处置**：本轮**不扩散**，登记为 **G-28（高）**（统一错误文本脱敏层，覆盖上述四处）。本文口径据此改为「**gorm 生成的 SQL 文本**永不含参数值」，不再宣称全局「参数永不出日志」。
- **R-4（`Init` 会被测试调用，低）**：`internal/database/database_test.go:120` 直接调 `Init`（只断言连接失败文案）。新增包级变量若有测试写入，需照 `TestSetMigrationsFS_二次注入覆盖`（`:199-211`）的 `defer` 恢复。
- **R-5（`RecorderParamsFilter` 是 gorm 包级全局，中）**：会改变**进程内所有** gorm 用户的记录行为。本进程只有本项目一个 gorm 使用者（已 grep 无第二处）；行为变化仅限「记录到的 SQL 用占位符」，不影响执行（`Dialector.Explain` 只用于日志）。已核实 `Recorder` 唯一使用点是 `finisher_api.go:529` 的日志路径。
- **R-6（gin 中间件顺序变更，低）**：`gin.Default()`→`gin.New()` 后，Logger/Recovery 从「最外层 + 重复」变成「CORS/metrics 之后各一次」。panic 仍被捕获、access log 仍写一行；测试 L-6 钉住。
- **R-7（`pkg/logger` 归一化会改变现网日志量，低）**：修好后 `level: info` 才真正过滤掉 DEBUG 行 —— 这正是 H-1 的预期效果；不影响 ERROR/WARN/INFO。

## 6. 边界（不做的事）与登记

- 不引入「生产可开的参数展开开关」（设计上拒绝）。
- 不改 `SlowThreshold` / `IgnoreRecordNotFoundError`。
- 不把 gorm 日志改道到 `pkg/logger`（需自定义 `logger.Interface`，超出本缺陷范围）。
- **登记（不在本轮修，写进 TODO）**：
  - **G-28（高）** 凭据经错误文本落日志/落库：钉钉 `access_token`（URL→err→日志+`notification_logs.error_msg`）、`apierr.Internal` 透传内部 err 到 stderr、`httpx` 响应体入 err、gorm 错误路径 pgx 编码错误带值。需统一脱敏层（`redact` 包 + 4 个调用点）+ 自定义 gorm logger 只打 SQLSTATE。
  - **G-29（中）** 容器日志无轮转上限（`docker-compose.yml` 无 `logging.max-size/max-file`）。
  - **G-30（低）** 冒烟脚本 argv 传密码（`db_smoke.sh:93`、`smoke-compose.sh:100,106`）；compose 占位 secret（`docker-compose.yml:134,205-206`）。

## 7. 审查记录（三视角 → 处置）

| 视角 | 发现 | 处置 |
|---|---|---|
| 正确性 | `Scan` 路径不受 `ParameterizedQueries` 保护 | **并入本轮**（§2.2-1，L-5） |
| 正确性 | 错误路径 `err` 不过滤，pgx 编码错误带值 | 口径收窄 + 登记 G-28（R-3） |
| 正确性 | L-2 不可实现（`New` 返回接口） | 抽 `gormLoggerConfig` 纯函数（§2.2-3） |
| 正确性 | 「无测试经 `Init`」不成立 | 修正 R-4 |
| 正确性 | postgres 占位形态是 `$1$` 非 `?` | L-4 按各自形态断言 |
| 安全 | gin Recovery dump Cookie → JWT 明文 | **并入本轮**（H-2/E-5，§2.2-6，L-6） |
| 安全 | 钉钉 token / apierr / httpx 错误文本 | 登记 G-28 |
| 安全 | seed 打印默认密码 | **并入本轮**（§2.2-7） |
| 安全 | 容器日志无轮转 | 登记 G-29 |
| 一致性 | **阻断**：`gormLogLevel` 同名函数+变量 | 函数改名 `mapGormLogLevel`（§2.2-2） |
| 一致性 | 零值 `Silent` 比 Warn 更静默 | 显式初始化 + L-2 |
| 一致性 | `pkg/logger` 只认大写 → 级别失效 | **并入本轮**（H-1，§2.2-8，L-7） |
| 一致性 | L-3 默认映射下必然假绿 | 必须用 debug 档并双断言（L-4） |
| 一致性 | `sqlite3_uuid` 在 database 包不可见 | 用裸 `sqlite.Open(":memory:")` + 手写 DDL |
| 一致性 | `docs/08-部署运维.md` 路径错 | 实际在仓库根 `08-部署运维.md`，落点 `:313` 前 |

## 8. 实现记录（rev3，2026-09-09）

### 8.1 落地清单

| 层 | 文件 | 内容 |
|---|---|---|
| gorm 级别 | `internal/database/gorm_logger.go`（新）、`database.go` | `mapGormLogLevel`（`debug`→Info / `info`·`warn`·空串·非法→Warn / `error`→Error）+ 包级默认显式 `Warn`；`gormLoggerConfig` 抽成纯函数便于断言 |
| 参数化 | 同上 | `ParameterizedQueries: true` + 覆盖 `gormlogger.RecorderParamsFilter`（`DB.Scan` 的旁路） |
| 调用点 | `cmd/{server,seed,migrate,admin-bootstrap,set-role}/main.go` | 各加一行 `database.SetGormLogLevel(cfg.Log.Level)`（必须在 `database.Init` **之前**） |
| Recovery | `internal/middleware/recovery.go`（新）、`api/routes.go` | 手写 `defer/recover`；`gin.Default()` → `gin.New()` + 显式 `gin.Logger()`（顺带修掉 G-12 ① 的重复挂载） |
| 级别归一 | `pkg/logger/logger.go` | `Init` 里 `ToUpper(TrimSpace(...))` + 空串默认 `INFO` |
| 配置键 | `internal/config/config.go` | `viper.SetDefault("log.level", "info")` |
| 输出卫生 | `cmd/seed/main.go` | 三行提示去掉明文默认密码 |

### 8.2 实现期间对 rev2 的三处更正

1. **E-5 有前提**：gin 的请求头 dump 只在 `IsDebugging()` 时发生（`gin@v1.9.1/recovery.go:85-91`）。本项目 `server.mode` 默认 `debug`，所以成立；但这意味着**测试必须显式进 `gin.DebugMode`**，否则变异成 `gin.Recovery()` 也照样绿（第一次写就踩了这个假绿）。
2. **`gin.CustomRecovery` 也挡不住**：dump 发生在 `CustomRecoveryWithWriter` **内部**、调用自定义 handler **之前**（`recovery.go:74-98`）—— 传什么 handler 都会先 dump 一次。所以只能完全手写 recover（`middleware/recovery.go`），并让测试同时盯住 `gin.DefaultErrorWriter` 必须为空。
3. **pgx 的 `DETAIL` 不是向量**：rev2 曾推测 PG 约束冲突消息会带 `Key (col)=(value)`。实测 pgx v5.5.1 的 `PgError.Error()` 只拼 `Severity: Message (SQLSTATE)`（`pgconn/errors.go:51-53`），**DETAIL 不进 error**。真正的错误文本泄漏是 `*url.Error` 里的完整 URL（钉钉 webhook 的 `access_token`）→ 登记为 G-28。

### 8.3 变异反证（12/12 红在断言上）

| # | 变异 | 红点 |
|---|---|---|
| M1 | `mapGormLogLevel` 默认分支改 `Info` | 默认档用例 |
| M2 | 包级 `gormLogLevel` 去掉显式初始化 | 零值用例（gorm 零值 < Silent → 一条不打印） |
| M3 | `ParameterizedQueries` → `false` | 两条 SQL 参数用例 |
| M4 | 删 `RecorderParamsFilter` 赋值 | **Scan 路径实测打出 `password_hash = "$2a$10$…"`** |
| M5 | `Colorful` → `true` | 配置纯函数用例 |
| M6 | `routes.go` 换回 `gin.Recovery()` | **实测 dump 出 `Cookie: auth_token=eyJ…`** |
| M7 | 去掉 `ToUpper` | 小写 `info` 不再过滤 DEBUG |
| M8 | 删 `SetDefault("log.level")` | env 覆盖用例 |
| M9 | recovery 记录 Cookie | 头字段用例 |
| M10 | 手写 recover → `gin.CustomRecovery` | `DefaultErrorWriter` 非空 |
| M11 | `SetGormLogLevel` 改成空操作 | debug 档不再打印（setter 没接到 logger 上） |
| M12 | `shouldLog` 去掉未知级别兜底 | `shouldLog(WARNING,DEBUG)=true`（拼错的级别变成最啰嗦档） |

> M4/M6 的变异不只是「测试红」，而是**把泄漏原文打了出来** —— 这是唯一能证明过滤真的生效的证据。

### 8.4 多角度审计后的修正（rev3.1，2026-09-09）

三视角审计（正确性 / 安全 / 一致性）跑完后改了 3 处代码 + 1 处测试，均为审计新发现：

| # | 视角 | 发现 | 处置 |
|---|---|---|---|
| A-1 | 正确性 | `pkg/logger` 对**未知级别 fail-open**：`shouldLog` 的 `order[currentLevel]` 查表 miss 得 0（== DEBUG），`log.level: warning`（合理拼写）反而变成最啰嗦档，与同轮 `mapGormLogLevel` 的 fail-closed **相反** | 已修：未知级别按 `INFO` 处理（`logger.go:145-155`）+ `TestShouldLog` 加 `WARNING`/`""` 三例；变异 M12 红 |
| A-2 | 正确性 | `gin.Default()` → `gin.New()` 顺带**丢掉了最外层的 Recovery**：旧链里 CORS/metrics/trusted-proxy 三层若 panic，原先由 `gin.Default()` 的 Recovery 兜住，现在会直接逃出 `ServeHTTP`（连接重置、无 access log、无结构化 panic 日志） | 已修：把 `gin.Logger()` + `middleware.Recovery()` 移到中间件链**最前**（`routes.go:136-146`）。Logger 在外保证 panic 请求仍有 access log；Recovery 在 CORS/metrics 之前恢复外层兜底。这三层当前无 panic 源，属回归修复 |
| A-3 | 正确性 | 第三条 gorm 路径：`migrator.printSQLLogger`（gorm `migrator/migrator.go:45-53`）内嵌的是 `logger.Interface`、**不实现** `ParamsFilter`，且 `fmt.Println` 不看级别；仅 `DryRun` 时挂载，生产不可达 | 不改代码，边界写进 `docs/TRAPS.md` T-33 |
| A-4 | 正确性 | 测试覆写包级 `RecorderParamsFilter` 后不复位 | 已修：`openDBWithCurrentLevel` 用 `t.Cleanup` 还原 |
| A-5 | 安全 | 「凭据不再落日志」的结论对 **G-28 那条路**仍不成立：`*url.Error` 带钉钉 `access_token` → `worker.go:153` 应用日志 + `notification_logs.error_msg`（可复现，两条触发路径：测试发送 / 告警事件自动触发） | **不在本轮修**：G-28 已登记为独立模块（TODO 有完整复现步骤与修法），本轮交付边界见 §6 |
| A-6 | 一致性 | `TestGormLogLevel_默认不是零值` 只断言「非零」，改成 `Error` 仍绿 | 已修：改为 `assert.Equal(gormlogger.Warn, gormLogLevel)` |

审计同时确认：CORS 头在 panic 后仍保留、`debug.Stack()` 不含局部变量、`gin.Recovery()` 无 `ErrAbortHandler` 重抛分支（故「掩盖 abort」不成立）、`pgx` 解析 DSN 失败会 redact 口令、`audit_logs` 不存 body/头、集成客户端与 `httpx` 错误不含凭据。
