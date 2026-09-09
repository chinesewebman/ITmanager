# 测试现状报告

**最后更新**: 2026-09-09（本次只增量更新本节与下方「2026-09-09 增量」；其余章节仍是 2026-06-16 快照）
**HEAD**: `bcb406d`（覆盖率表快照）→ 当前 `main`
**状态**: ✅ 959 backend 测试函数全过（`go test ./... -count=1`，27 个包）+ `db_smoke.sh` 两条真 PG 路径绿

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
