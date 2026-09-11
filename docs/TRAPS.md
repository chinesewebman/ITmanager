# ITmanager — Trap 集中清单

> **维护**: 2026-07-01 B1-4 创建 (v2.3)
> **范围**: 项目从 v1.0 → v2.3 累积踩过的实战陷阱
> **使用**: 给接手人 / 未来自己 debug 时一眼对照
> **来源**: `~/.hermes/skills/software-development/itmanager-feature-impl/references/*.md` + skill SKILL.md "Top traps"
>
> **状态约定**:
> - **ACTIVE** — 当前仍然存在,改动时必须检查
> - **FIXED** — 已修复, 但容易回退/复发,值得知道避免再踩
> - **HISTORICAL** — 历史 trap,描述过时,新代码不会再撞,仅供考古

---

## 一、Go 后端陷阱

### T-1. gorm `Create` 默认产生 `INSERT ... RETURNING "id"` (PG)
**状态**: ACTIVE | **类别**: sqlmock 测试
**现象**: sqlmock 报 "call to Query was not expected, next expectation is ExpectedExec"。
**解法**: 用 `ExpectQuery` + `WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(id))`, 不是 ExpectExec。
**陷阱**: `PreferSimpleProtocol: true` 时相反 — 用 ExpectExec + 事务 Begin/Commit (Trap 13)。

### T-2. gorm Create 默认包事务 → 测试要 Begin
**状态**: ACTIVE | **类别**: sqlmock 测试
**现象**: 单条 Insert 报 "call to database transaction Begin was not expected"。
**解法**: 中间件用 `Session(&gorm.Session{SkipDefaultTransaction: true})`, 或测试 ExpectBegin+Exec+Commit 三件套。

### T-3. gorm `First(id)` 实际发 2 个 bind arg
**状态**: ACTIVE | **类别**: sqlmock 测试
**现象**: `WithArgs(id)` 报 "expected 2 args, got 1"。
**根因**: gorm 隐式追加 `ORDER BY id LIMIT 1`, SQL 末尾是 `$1, $2`, args = [id, 1]。
**解法**: `WithArgs(id, 1)` 或 `WithArgs(id, sqlmock.AnyArg())`。

### T-4. gorm `Offset(0).Limit(N)` 不发 offset bind
**状态**: ACTIVE | **类别**: sqlmock 测试
**现象**: page=1 测试 OK, page=2 测试报 arg 数不对。
**根因**: `Offset(0)` 被 elide, SQL 是 `LIMIT $1` (1 arg); `Offset(2)` 是 `LIMIT $2 OFFSET $1` (2 args)。
**解法**: 默认 page 路径测 1 arg, 非默认 page 测 2 args。

### T-5. gorm `IN ?` clause 1 个 arg (slice)
**状态**: ACTIVE | **类别**: sqlmock 测试
**现象**: `db.Where("id IN ?", []string{...})` 报 "expected N args, got 1"。
**解法**: `WithArgs(sqlmock.AnyArg())`, 不要展开 slice。

### T-6. gorm model `column:` tag 缺失 = 静默数据丢失
**状态**: ACTIVE | **类别**: 生产 bug,数据完整性
**现象**: 字段永远 nil,无报错。`IPv6Address *string` 无 `gorm:"column:ipv_address"` tag → 读 `ipv_address` 列 = nil forever。
**根因**: gorm v2 不验证列存在性,字段缺 tag 用 snake_case 推断,但你的 schema 可能不同名。
**解法**: **永远审计 model tag vs DB schema**, 加 sqlmock row 测试时**先把列名列对**。

### T-7. AutoMigrate 是 per-model 查询,不是单事务
**状态**: ACTIVE | **类别**: sqlmock 测试
**现象**: 期望 Begin/Exec/Commit 失败,gorm 跑 N 个独立 query (SELECT count + CREATE TABLE + N×CREATE INDEX), 顺序随机。
**解法**: `sqlmock.New(sqlmock.QueryMatcherOption(...))` + `MatchExpectationsInOrder(false)` + ~50 个 generic expectation for 12 model。

### T-8. closure 捕获 New() 改的 config → nil panic
**状态**: ACTIVE | **类别**: production panic
**现象**: `New(cfg)` 内改 `cfg.KeyFunc`, 返回的 closure 还捕获原 cfg → 请求来时 keyFn() = nil deref。
**解法**: closure 捕获前先把需要的东西拷到 local var: `keyFn := rl.cfg.KeyFunc; return func(c) { key := keyFn(c) }`。

### T-9. pre-commit gofmt hook 假阳性
**状态**: ACTIVE | **类别**: hook
**现象**: 你 `go fmt` 过了, commit 还是报 "Go files need formatting"。
**根因 1**: hook 跑 `go fmt ./...`, 改了你没碰的旧文件 (Go 1.25 更激进 import 排序)。
**根因 2**: hook 有自己的 gofmt cache。
**解法**:
- (a) `gofmt -l .` 空=真干净 → 用 `git commit --no-verify` 一次
- (b) **推荐**: 先 commit `chore(go): re-format` 把 gofmt 改的无关文件清掉, 再 commit feat
- 检测: `git status --short` 看 ` M ` (空格+M = unstaged 但 gofmt 动了)

### T-10. git add 是叠加,不是替换
**状态**: ACTIVE | **类别**: commit hygiene
**现象**: 想做小而专的 chore commit,结果 diff stat 14 files / +616 行 — 上次 staged 的 feat 文件还在 index 里。
**解法**:
- (a) `git reset --soft HEAD~1` + `git reset HEAD <要排除的>` 拆开重做
- (b) `git commit --only <paths>` (前提: paths 已在 index)
- (c) **最稳**: commit 前必跑 `git status --short` 看 staged 区 `M ` 标记

### T-11. service 层 trigger 阻塞主流程
**状态**: ACTIVE | **类别**: concurrency
**现象**: status 变更触发通知/审计 → race,主流程等通知完成慢。
**解法**: trigger helper, fail-only-log, fire-and-forget。看 audit-batch-2026-06-17 §10。

### T-12. 外部 3rd-party 集成 token 永不过期
**状态**: ACTIVE | **类别**: Zabbix/NetBox/Jira
**现象**: session token 失效不检测,调接口一直 401。
**解法**: 3-layer: expiresAt 跟踪 + active re-login 检测 + -10002 auto-retry。audit-batch-2026-06-17 §11。

### T-13. Worker 复用 IntegrationService,不要自己 New client
**状态**: ACTIVE | **类别**: cron worker
**现象**: worker 自己 `NewZabbixClient(cfg, nil)`, UI Reload URL 后 worker 仍用旧 URL 静默漂移;auth cache 分裂;Prometheus label 重复计数。
**解法**: worker 构造函数收 `*IntegrationService`, 内部用 `w.svc.zabbix`。见 v2.3-cron-worker-pattern.md §2-3。

### T-14. Worker Stop() 必须查 `started` + `stopped` 双 bool
**状态**: ACTIVE | **类别**: close-channel panic
**现象**: 单 `started` bool → 二次 Stop 仍过检查 → `close(w.stop)` → `panic: close of closed channel`。
**解法**: 加 `stopped bool` 字段, `if !w.started || w.stopped` 命中抢先 return。
**影响范围**: `MetricSyncWorker.Stop`, `notification.Worker.Stop` (待修)。

### T-15. newMockDB 必须显式 `r.Use(gin.Recovery())`
**状态**: ACTIVE | **类别**: handler test
**现象**: svc=nil 触发的 panic 穿透 testing.tRunner,整个 test 进程 panic 而不是 fail。
**解法**: test router factory `gin.New()` 之后立即 `r.Use(gin.Recovery())`。

### T-28. gorm `default` tag 对 string 字段是 Go 侧参数替换,不是 DB 默认值
**状态**: ACTIVE | **类别**: gorm / jsonb 零值 (G-20)
**现象**: 给 `string` 字段加 `default:'[]'`,以为「零值会被跳过、交给列默认值」。实测 `Create` 时 gorm 把字面量**替换进参数**(DryRun `VARS=[… [] {}]`),列照旧出现在 INSERT 里 —— 该字段不进 `FieldsWithDefaultDBValue`(`schema/schema.go:286-289`),所以只有 `default:(-)`(`schema/field.go:231-232`)才是「省略该列」。两者在 update 路径都不生效:`Updates(结构体)` 的零值被 gorm 跳过(静默 no-op),`Select(...).Updates` / `Updates(map)` 直接写出 `''`。
**解法**: 写入期不变量用模型钩子(`BeforeSave`)做应用层归一;`ALTER COLUMN ... SET DEFAULT` 只作非 gorm 写入方的兜底。完整写入矩阵见 `docs/FIX-PLAN-ASSET-JSONB.md` §2.3,回归测试 `internal/models/hooks_test.go`。

---

### T-29. 列改名会让引用它的唯一约束**悄悄换语义**(`unique_asset`)
**状态**: FIXED | **类别**: 迁移 / 唯一约束 / 演示数据 (G-20 轮) | **修复日期**: 2026-09-09
**现象**: `cmd/seed` 在真 PG 上 `exit 1`,日志 `27 处创建服务器失败 + 9 处创建交换机失败: duplicate key value violates unique constraint "unique_asset"` —— 48 个演示资产只落库 12 个(每站点第一个机柜的 4 个)。
**根因**: 000001 建的约束是 `UNIQUE (asset_tag, idc_id)`;000013 把 `assets.idc_id` **改名**为 `site_id`,PG 自动把约束重定向到新列名 —— **约束名没变,语义从「全局唯一」变成「站内唯一」**。而 seed 的 `asset_tag` 只含 `site.Code`(`AST-DC-BJ-01-001`),同站点 4 个机柜生成同一个 tag。改名之前 `assets` 的 jsonb 缺陷(G-20)让所有资产插入先失败,把这个缺陷盖住了。
**检测方法**: ① 任何 `ALTER TABLE ... RENAME COLUMN` 之后,查 `pg_constraint` 确认引用该列的约束/索引是否符合预期(`SELECT conname, pg_get_constraintdef(oid) FROM pg_constraint WHERE conrelid='assets'::regclass`);② 演示数据生成器产出的唯一键字段,必须在**约束的真实列组合**上唯一,不能只靠肉眼「看起来唯一」;③ 真库跑一次 `cmd/seed` 并检查退出码。
**解法**: `asset_tag` 补上 `rack.Name`(同表 `SN`/`Name` 早已含机柜名,只有 tag 漏网);`cmd/seed/main_test.go` 加「站内唯一 + 48 资产」断言,测试 schema 补 `UNIQUE (asset_tag, site_id)` 镜像真库。**推广**: 「静默吞错的循环 + `exit 0`」会把数据缺陷藏成绿灯 —— 种子/批处理必须让失败计数决定退出码(见 G-20)。

---

### T-30. `ON CONFLICT` 要三层同时正确(列名 / 真 UNIQUE 仲裁器 / 非空 SET),sqlite 只挡得住一层
**状态**: FIXED | **类别**: gorm / Postgres / 测试手段 (G-22 轮) | **修复日期**: 2026-09-09
**现象**: `integration` 的三条同步路径在真 PG 上**全部失败**（NetBox `42703 column "netbox_id" does not exist`、Zabbix `42703 column "Status"`、GLPI `42702 column reference "id" is ambiguous` —— SET 的歧义在**解析期**就报错，早于仲裁索引检查，所以「缺唯一索引」不是 GLPI 当时的报错原因），而 `go test ./...` 全绿 —— 因为没有任何一条用例走到 DB 写入。
**三层根因**（互相独立，任一处都会让整条语句失败）：
1. **列名层**：`clause.Assignments`/`clause.Column` **不做 Go 字段名 → DB 列名映射**，调用方传 `"Name"`/`"netbox_id"` 原样进 SQL。用 `clause.AssignmentColumns([]string{"name",…})` 并**把参数语义写成 DB 列名**。
2. **约束层**：`ON CONFLICT (col)` 要求 `col` 上有 **UNIQUE 约束或唯一索引**（普通索引 → `42P10`）。`HasIndex` 只按**名字**判存在，所以 AutoMigrate 不会把既有的普通索引升级成唯一（`uniqueIndex` 与 `index` 生成的索引名同为 `idx_<表>_<列>`）→ 必须写迁移。PG 唯一索引**允许多个 NULL**，所以整列唯一即可，不需要部分索引。
3. **SET 层**：空更新列表时 gorm 渲染 `SET "id"="id"` —— 在 `ON CONFLICT DO UPDATE` 里 `id` 同时存在于目标表与 `excluded` → `42702 ambiguous`。空列表必须显式 `DoNothing`。
**检测方法**（照抄 `internal/integration/upsert_test.go` + `tests/db_smoke_test.go:TestDBSmoke_NetBoxUpsert`）：
- **DryRun 断言渲染出的 SQL 字符串**（`ON CONFLICT (\`net_box_id\`)`、`\`name\`=\`excluded\`.\`name\``、不含 `` `UpdatedAt` ``）—— 跨方言确定性，是列名层的唯一守卫；
- **sqlite 功能测试**只能抓「冲突目标不存在」这一层：**sqlite 列名解析大小写不敏感**，`SET "Name"=EXCLUDED.Name` 在 sqlite 上**静默成功**（实测）；
- **真 PG** 抓剩下的：唯一索引形态（`pg_indexes.indexdef` 含 `CREATE UNIQUE INDEX`）、重复拒绝、多 NULL 共存、真调用点端到端、脏数据挡住迁移的原子失败。
**其它实测坑**：① `CREATE INDEX CONCURRENTLY` 不能进事务块，而迁移执行器按事务跑整个文件 → 只能用普通建索引，且 `DROP INDEX` 的 **ACCESS EXCLUSIVE 持有到 COMMIT**，`assets` 期间**读写全阻塞**（不是只阻塞写）；② 迁移失败日志里**没有 PG 的 DETAIL（哪个键重复）**：驱动返回的 `*pgconn.PgError.Error()` 只拼 `Severity: Message (SQLSTATE Code)`（pgx v5.5.1 `pgconn/errors.go:51-53`），Detail 字段不在其中，而应用只记录 `err.Error()`（gorm 的 `TranslateError` 本仓库未开启，没有翻译介入）→ 只剩 `could not create unique index … (SQLSTATE 23505)`，定位重复值只能靠升级前自检 SQL。
**推广**：「SQL 拼对了」不等于「在目标方言上跑得通」，也不等于「语义对」。本轮修复**解锁**的隐藏语义缺陷：更新列里放一个硬编码常量（`status="active"`）会把本地已退役资产静默改回 active（F-7）—— 修好一条语句后，要重新审一遍它**现在真的会写什么**。

---

### T-31. 两类假绿：前置缺失走 `Skip`、测试另抄一份生产清单
**状态**: FIXED | **类别**: 测试有效性 / 变异反证 (G-22 轮) | **修复日期**: 2026-09-09
**现象**（G-22 的测试有效性审计实测，两条都发生在「本轮交付物的守门用例」上）：
1. **前置缺失 → 整批 Skip → 脚本 EXIT=0**：`TestDBSmoke_NetBoxUpsert` / `TestDBSmoke_DownPreservesLegacyColumns` 用 `t.Skipf("库未应用到 000015")` 做前置。把 `000015` 两个迁移文件删掉后，两个用例都 Skip，`scripts/db_smoke.sh` **退出码 0** —— 本轮唯一的真库守门**静默消失**，而 CI 全绿。
2. **测试另抄一份生产清单**：DryRun 用例自己写死 `cols := []string{"name", …, "updated_at"}` 再断言渲染结果。调用点 `service.go` 的清单被改成别的（比如加回 `tags`）时，测试断言的仍是自己那份 → **照样绿**。
**根因**：`t.Skip` 的语义是「本用例不适用」，被误用成「前置不满足就算了」；而「断言的字面量」与「生产实际用的值」是两个来源，两者会漂移。
**检测方法**：
- 前置**必须**用 `Fatalf` 的场景：本轮的交付物守门（迁移存在、索引唯一、关键列在）；`Skip` 只留给「本库本就不适用」（如非升级路径库）。
- 加**守恒断言**兜底：`TestDBSmoke_MigrateRunner` 断言 `schema_migrations` 行数 **等于** `embed` 内 `migrations/*.up.sql` 的数量，且本轮关键版本（15）已记录 —— 这样「迁移文件没进 embed / 被删」都会红。
- 断言**引用生产变量**而不是复制字面量：`require.Equal(t, []string{…}, netboxUpdateCols)`（`netboxUpdateCols` 就是调用点用的那个包级变量）。
**变异反证的第二个坑**：变异必须**红在断言上，不是红在编译上**。第一次删掉批次内去重时只删了 `if` 块、留下 `seen` 变量 → `declared and not used` 编译失败，也是「红」，但**没有证明断言有效**；把 `seen` 声明一起删掉后才看到真正的断言失败（期望 1 实得 2）。→ 变异后先看红的原因。
**推广**：绿灯的**原因**要能说清。写完守门用例，做一次「把它要守的东西删掉」的变异 —— 若仍然绿，那这条用例的价值是 0。同类：`go test -run` 白名单里名字打错 → 匹配不到也是 EXIT=0（本轮由守恒断言间接兜住）。

---

### T-32. 「一个 `log.level`，两条互不相干的日志路径」——配置看起来生效，实际两条都没接上
**状态**: FIXED | **类别**: 可观测性 / 配置未贯通 (G-16 轮) | **修复日期**: 2026-09-09
**现象**：`config.yaml` 写着 `log.level: info`，运维以为级别已配置。实测两条日志路径**各自失效**：
1. **gorm SQL 日志**：`internal/database/database.go` 硬编码 `logger.Info` —— 与 `cfg.Log.Level` 毫无关系，release 下照样逐条打印 SQL。
2. **应用日志**：`pkg/logger` 的 `shouldLog` 级别表只含**大写**键（`DEBUG`/`INFO`/`WARN`/`ERROR`），而 `Init` 把 `cfg.Level` **原样**存进 `currentLevel`；`config.yaml` 写的是小写 `info` → `order["info"]` 取到零值 `0`，恰好等于 DEBUG 的序号 → **级别过滤完全失效**，debug 行照打。

两者叠加的结果是「改了配置，日志一条没少」，而配置本身看起来完全正确。
**根因**：配置项有多个消费者，但只有一个消费者被接上；且「消费者」用的键空间（大小写/词表）与配置来源不一致，而**不一致是静默的**——查表 miss 得到零值，零值又恰好是合法级别。
**检测方法**：
- 配置项有几个消费路径就写几条「改配置 → 行为变化」的测试。G-16 的做法：`TestInit_小写info真的过滤DEBUG`（把 level 设成 `info`，断言 DEBUG 行**不出现**）比 `TestInit_级别归一化`（只断言存进去的字符串）更值钱——前者才钉住行为。
- 查表类逻辑不要依赖零值语义：`map[k]` 的 miss 必须显式判定（`v, ok := m[k]; if !ok {…}`），否则「拼写错误 / 大小写不符 / 词表外值」全都退化成某个合法值。
- 归一化放在**入口**（`Init` 里 `strings.ToUpper(strings.TrimSpace(...))` + 空串给默认档），不要在每个查表点各归一一次。
- 反向检查：把 `log.level` 调成 `error` 起一次服务，确认 SQL 与 DEBUG 行都消失——这是唯一能自证「配置真的接上了」的检查。
**推广**：`SetDefault` 也属于这一族——viper 只对 `AllKeys`（yaml 键 + `SetDefault` 键）做 env 覆盖，shipped yaml 缺键时纯 env 注入会被静默忽略（见 G-13）。**配置文件的键、env 名、消费点三者必须各有一条测试**，否则漂移永远静默。

---

### T-33. 库级钩子绕过：设了 `ParameterizedQueries` 参数照样落日志
**状态**: FIXED | **类别**: 凭据泄漏 / 第三方库内部路径 (G-16 轮) | **修复日期**: 2026-09-09
**现象**：给 gorm 配 `logger.Config{ParameterizedQueries: true}` 后，普通查询日志确实只剩占位符，但 `DB.Scan` 的日志**仍带参数值**——实测打出 `SELECT password_hash FROM secrets WHERE password_hash = "$2a$10$…"`。
**根因**：`ParameterizedQueries` 只在 `(*logger).ParamsFilter` 里生效（`gorm.io/gorm/logger/logger.go:193-198`）。而 `(*DB).Scan` 会把 logger 换成内部的 `traceRecorder`（`finisher_api.go:527-533`），后者的 `ParamsFilter` 调的是**包级变量** `logger.RecorderParamsFilter`（`logger.go:220-225`），默认是恒等 no-op（`logger.go:85-88`）→ 参数原样展开。也就是说：**同一个库里有第二条过滤通道，配置项只覆盖了其中一条。**
**检测方法**：
- 别只测「典型路径」。这里的缺口只有走 `Scan`（`Find` 不走）才暴露；测试必须**两条路径各一条**：`TestGormLogger_普通查询不落参数值` + `TestGormLogger_Scan路径不落参数值`。
- 断言写法：先 `require.Contains(out, "SELECT password_hash FROM secrets")`（证明**这条日志真的被打印了**），再 `assert.NotContains(out, "$2a$10$")`。只写 NotContains 会在「日志压根没输出」时假绿。
- 修完做一次「删掉修复」的变异，确认测试**红在断言上**且红的时候能把泄漏原文打出来——这是唯一能证明过滤真的生效的方式。
**推广**：第三方库的「配置开关」经常只覆盖它自己的主路径，内部包装器（recorder / proxy / adapter）走另一条。审计时先找**同一个语义有几处实现**（这里是 `ParamsFilter` × 2），再确认配置是否覆盖全部。
**边界（实测）**：还有第三条路 —— `migrator.printSQLLogger`（`gorm.io/gorm@v1.30.0/migrator/migrator.go:45-53`）内嵌的是 `logger.Interface`、**不实现** `ParamsFilter`，且用 `fmt.Println` 不看 `LogLevel`；但它只在 `DryRun` 时挂载（`migrator.go:115`），本仓库生产路径不置 `DryRun`（仅 `internal/integration/upsert_test.go:112`），故当前不可达 —— 记录在此，避免下次「已全部覆盖」的结论又被推翻。

---

### T-34. 凭据藏在**错误文本**里：`*url.Error` 会带着完整 URL 走遍四个出口
**状态**: FIXED | **类别**: 凭据泄漏 / 错误值传播 (G-28 轮) | **修复日期**: 2026-09-09
**现象**：钉钉 webhook 的 token 在 query（`…/robot/send?access_token=SECRET`）、飞书/Slack 的在 path（`/hook/<token>`、`/services/T/B/SECRET`）、自建 webhook 的在 header、集成 URL 可能在 userinfo。发送失败时 Go 返回 `*url.Error`，`Error()` 是 `"%s %q: %s"`（`net/url/url.go:29-36`）——**完整 URL 连凭据一起**。同一个错误值同时流进四个出口：① 应用日志（标准库 `log` 默认 stderr，`log.go:87`，`log.level` 管不到）② `notification_logs.error_msg`（varchar(500)，进备份、只读 DB 账号可见）③ `gin.DefaultErrorWriter`（`apierr.Respond` 5xx 分支）④ HTTP 400 响应体（`apierr.BadRequest` 的 `internalErr` 是 nil，绕开了 5xx 的脱敏分支）。
**根因**：G-16 堵的是 **SQL 参数**，这是**错误文本**，两条互不相干的路径（同 T-32 的教训）；而「脱敏」如果按参数名做黑名单（`access_token`/`key`/`secret`…），必然漏掉 path、header、userinfo 三种形态。
**关键事实（实测，别凭直觉）**：
- `http.Client.do` 的 `stripPassword`（`client.go:624-631`/`:1034`）**只掩 userinfo 的密码**，query 与 path 里的 token 原样保留；`url.Parse` 的失败文本**连 userinfo 都不掩**。
- 规则必须是**结构性**的：形状像 URL 就塌缩成 `scheme://host`（丢 userinfo/path/query），再做 `Authorization: Bearer|Basic` 与键值形态的值替换。V-5 用 6 类绕过 + 3 类「不误伤」钉住（SQL 约束名、`?page=2`、`monkey=banana` 必须原样返回）。
- **脱敏与截断的先后不是安全边界**：审查一度把「先截断后脱敏」列为应被捕获的变异（M5），实测四种构造下**两者都不泄漏**——规则 1 是形状识别（URL 被截断后仍是 URL 形状 → `<invalid-url>`），规则 3 的值类以定界符/串尾为界。把顺序写成注释里的「安全理由」是自欺，已改写；变异表移除该条并写明依据。
- 值类的**边界**是双刃：`access_token=SECRET中中中…` 会把后续汉字一并吞掉（过度脱敏、不泄漏）——测试构造必须在值后加定界符，否则测的不是你想测的东西。
**检测方法**：
- 失败要**确定性**：`net.Listen` 拿地址后**只 bind 不 Accept** + 200ms ctx 超时 → `errors.Is(err, context.DeadlineExceeded)` 成立、`*url.Error{Op:"Post"}`（实测）。`connection refused` 的文案跨平台不稳，别拿它当锚。
- 正向断言必须成对：先 `require.Contains(err, "http://127.0.0.1:")`（`scheme://host` 只可能来自脱敏函数 → 钉住它真被调用），再 `NotContains(SECRET)`。只写 NotContains 会在「错误被吞成空串」时假绿。
- 入库路径要用**真 sqlite 写+读回**（sqlmock 断不了 map 更新的参数值），并断言 `status="failed"`（证明 UPDATE 真生效——旧版吞掉 `.Error` 会让行停在 pending 被无限重发）。
- 截断按 **rune**：列是 `varchar(500)`（**字符**数），按字节切会切断多字节字符 → PG 报 `22021` 拒收 → 该行永远 pending。测试用「18 字节键值 + 600 个三字节汉字」让 500 的边界落在字符中间。
**第二轮（2026-09-09 晚，审计回执）——三条新增教训**：
- **凭据类错误要在「源头」收口，出口兜底只是补充**：G-28 首轮在 4 个日志出口接脱敏，却漏了 `integration/service.go:282/289/296` 与 `metric_sync.go:111` 四处同类写法（安全审计 H-1，实测集成 URL 的 `?access_token=` 原文落 stderr）。逐个出口补 = 下次新增日志点又漏；正确位置是**错误产生处**（`httpx` 出口包一层 `redactedErr`，`Error()` 过 `redact.Text`、`Unwrap()` 保链），一处覆盖全部消费者。
- **正则值类的「排除集」要按语义最小化**：值类排除 `'`/`\` 会让 `?auth=AB'CDEF` 的尾部残留（M-3）；排除 `}` `]` `<` `>` 会让 `{"password":"ab}c"}` 漏尾、`password=}SECRET` **整条不匹配**（正确性审计 P1）。判断标准：这个字符**真的**是值的定界符吗？query/JSON 里真正的定界符只有空白、引号、`&,;`。
- **`scheme://` 不是 URL 的唯一形状**：配置里漏写 scheme 的 `//host/path/SECRET`（M-2）整条不匹配 → 尾部泄漏；`URL()` 还要挡 `https://user:`（net/url 把无密码 userinfo 并进 `Host`，原样返回等于回显凭据）。
- **错误文本可能是非法 UTF-8**：第三方（SMTP 服务端、上游响应体）可控文本写进 `varchar` 列前必须 `strings.ToValidUTF8`——PG 22021 拒收 → 行停在 `pending` 被**无限重发**（正确性审计 P4；`utf8.ValidString` 在 sqlite 上测得出，PG 上才致命）。
**推广**：审计一个「值会不会泄漏」时，先列**这个值有几个出口**（日志 / DB 列 / 错误 writer / HTTP body），再列**它能以几种形态出现**（URL query / path / userinfo / header / JSON 键值）——只做其中一格就是假的安全感；最后问一句：**脱敏点是不是在错误产生处**（若不是，列出「还有哪些出口没接」）。

---

### T-35. 双层防御下「单层变异不红」≠ 测试失效；但也别把哨兵断言当屏障
**状态**: FIXED | **类别**: 测试有效性 / 变异反证 (G-28 第三轮) | **修复日期**: 2026-09-09
**现象**：给 NetBox / GLPI 的 400 回显补上「不泄漏 URL 凭据」的用例后，把 handler 层 `redact.Text` 去掉（审计 HIGH-2 指出的零覆盖点）**测试仍然全绿**——初看像「用例白写了」。
**根因**：泄漏有两道闸：`httpx` 出口的 `redactedErr`（§9.1 源头收口）已经把 `*url.Error` 的 URL 塌缩过一遍，handler 层的 `redact.Text` 是**纵深防御**。任何一层单独去掉，出口文本都不含凭据，用例自然不红。
**关键事实**：
- 用例钉住的是「出口不泄漏」这一**事实**，不区分由哪一层实现。要证明用例有效，做**组合变异**：同时去掉两层 → 实测红在断言（泄漏原文含 `?token=SUPERSECRET`）。
- 反过来说：**变异绿要解释清楚是「屏障在别处」还是「用例测了空气」**。前者补组合变异，后者补用例——两者的处置完全不同，别混。
- 哨兵断言（如「header 里的 secret 不该出现在错误文本」——当前实现下恒真）值得保留，但**要标注它不是屏障**：将来有人把请求 dump 进错误文本时它会红，在此之前它对变异没有约束力。
**检测方法**：写变异时先问「这条断言今天**可能**红吗？」——不可能，就是哨兵；再问「真正的屏障在哪一层？」，然后对**那一层**做变异。
**推广**：多层防御的安全属性，变异反证必须成组施加（覆盖每一层的单独与组合），并把「哪一层是真屏障、哪一层是兜底」写进文档——否则下次审计还会把「单层绿」重新报一遍。

---

### T-36. `if s, ok := v.(string); ok { 校验 }` 是 **fail-open** —— 类型断言顺手写成的静默放行
**状态**: FIXED | **类别**: 输入校验 / fail-closed (G-33 M1) | **修复日期**: 2026-09-09
**现象**：`ChannelService.Update` 收的是 `map[string]any`（来自 JSON body）。最自然的写法是「含 `config` 就取字符串校验」，即 `if s, ok := updates["config"].(string); ok { validate(s) }`。但这样**非字符串值直接绕过校验**，还会被 gorm 落库：实测 `float64(12345)` → `config="12345.0"`、`true` → `config="1"`（`Updates(map)` 把值交给 `clause.Assignment` 原样下传，`field.Set` 的类型错误被丢弃）。对象/数组则更晚才炸（`unsupported type map[string]interface{}` → 500）。
**根因**：`ok` 分支写了「合法时要做什么」，却没写「不合法时要做什么」——类型断言把「不是我要的类型」和「我没检查」合并成了同一条静默路径。这类写法在**校验**语境里是 fail-open，在**取值**语境里才是合理的。
**检测方法**：凡是 `x, ok := v.(T)` 出现在**校验/授权/脱敏**函数里，问一句「`!ok` 时发生了什么？」。若答案是「继续往下走」，就是本 trap。变体：`if v != nil { 校验 }`（nil 放行）、`switch` 缺 `default` 分支。
**修法**：`!ok` 一律显式拒绝（`ErrInvalidInput`），并给每个非期望类型各留一条用例（对象/数组/数字/布尔/null）。M1 的 V-4 就是这么钉的。
**推广**：fail-closed 的验收标准不是「坏输入被拒」，而是「**每个**非法形态都有确定的拒绝路径」——审查时列输入形态清单，比读代码更容易发现漏网的那一个。
**复发（2026-09-10，M18）**：`validateTicketEnumValues`（`TicketService` 的枚举列取值校验）是同一仓库**第二处**——本轮是**先知道 T-36 才动手写**的，仍然在第一版里写成了 `if s, ok := v.(string); ok { … }`。说明这条 trap 的抗性不来自「知道」，而来自**写校验函数时把 `!ok` 当成一个必须回答的问题**：本次最终写法把 `!isString` 摆进同一个拒绝条件（`if !isString || !f.allowed[s] {`），`!ok` 无处可逃。危害在 tickets 上更重：`null` 会撞 `tickets.status NOT NULL` → **500**，而数字/布尔/对象/数组会**静默落库**成为筛不出来的票。
**检测方法补充（M18）**：变异反证时把 `!isString ||` 翻成 `isString &&`（即退回 fail-open），**并检查红掉的子用例名恰好是非字符串那几条**——全组一起红说明用例没分层，等于没测出「哪条路被放行了」。

### T-37. 校验用的键 ≠ 落库用的键 —— gorm `Updates(map)` 会把 Go 字段名解析到同一列
**状态**: FIXED | **类别**: 输入校验 / ORM 语义 (G-33 M1 rev3) | **修复日期**: 2026-09-09
**现象**：`ChannelService.Update` 的 fail-closed 校验只认小写键 `updates["config"]` / `updates["type"]`，但落库走的是 gorm 的 `Updates(map)`。gorm 对每个键调 `Schema.LookUpField(k)`（先 `FieldsByDBName`、再 `FieldsByName`，**均大小写敏感**），于是 `{"Config": …}` / `{"Type": …}` 完全跳过校验照样写列，`{"id": …}` 还能改主键。真 PG 18 实测：修复前这些请求全部 **HTTP 200** 且坏值落库（`{"Config":12345}` → `config="12345.0"`；`{"Type":"dingtalk"}` + 存量 `{"url":…}` → 坏组合；`{"id":<新uuid>}` → 主键被改，引用它的 `notification_logs.channel_id` 悬空，本仓库无外键约束）。
**根因**：校验层与写入层**各自解析同一份输入**，且解析规则不同（精确小写键 vs ORM 的名字解析）。安全审计与正确性审计独立命中同一处——说明这是「防御建在约定上」的典型形态，不是笔误。
**检测方法**：任何「先校验 map/对象、再把同一个 map/对象交给 ORM」的路径，问两句：① 校验的键集合是否等于落库的键集合？② 有没有大小写变体、Go 字段名、别名能到达同一列？写一条 `{"Config": …}` 的请求打过去看状态码。
**修法**：入口处**键归一化到小写 + 白名单**（`name/type/config/is_enabled/is_default`，其余 → 400），保证「校验的键 == 落库的键」；白名单同时挡掉 `id`/`created_at`。`Update` 还额外把校验后的 `(type, config)` 写回 map（写入值 == 校验值），消除并发交错的坏组合。
**推广**：`Create` 走结构体绑定（JSON 解码大小写不敏感）时没有这个问题——**同一个 API 的读写两条路径可以有完全不同的键解析规则**，只测一条不足以证明契约成立。
**复发（2026-09-10，M17）**：`TicketService.Update`（`PUT /tickets/:id`）同款——同一仓库**第二处**独立命中，说明这不是 channel 的笔误而是**本仓 `Updates(map)` 路径的系统性形态**。危害升级：改主键会让 D-3 建立的 `alerts.ticket_id` 悬空（无外键约束，见 T-37 现象里同类描述）。**修法取舍与 channel 相反**：改用**禁改集合**（`id`/`ticket_number`/`created_at`/`updated_at`）而非可改白名单——tickets 有 20 个业务列且绝大多数本就该可改，白名单既维护不起，也会把外部对接要写的 `external_id`/`resolved_at` **静默丢掉**（静默丢字段比拒绝坏字段更难发现）。**取舍判据**：可写列少且全需校验 → 白名单；可写列多且多数无需校验 → 禁改集合 + 归一化。两者共同的不变式仍是「校验的键 == 落库的键」。
**检测方法补充**：新增任何 `Updates(map)` / `Model(&x).Updates(...)` 路径时，先 grep 该 service 有无键归一化，再补一条 `{"ID": …}` 或 `{"GoFieldName": …}` 的用例。判据是**能否红**，不是代码里有没有注释。

### T-38. 变异脚本被中途 kill → 源文件**停在变异态**，后续测试红/绿都在骗你
**状态**: FIXED | **类别**: 变异反证 / 工具 (G-33 M3) | **修复日期**: 2026-09-09
**现象**：把 `/tmp/mutate8.py` 放后台跑（`run_in_background`），期间手工编辑了它正在变异的同一个文件（`sender.go`），随后 `TaskStop` 终止脚本。再跑单测时 `TestWeChatSender_发送失败不泄漏URL凭据` 变红，错误文本里赫然是完整 URL 连 `?key=SECRET`——**看起来像刚写的脱敏代码失效**。实际是脚本被杀在 `M8-7`（把 `redact.URL(w.cfg.URL)` 换成 `w.cfg.URL`）的测试等待中，`finally` 里的 `shutil.copy(bak, src)` 没执行到。
**根因**：脚本的恢复逻辑写在 `finally`，而进程被 kill（SIGTERM 到进程组）时 Python 不保证执行 `finally`。更隐蔽的是：**变异态代码与真实代码只差一个 token**，肉眼扫 diff 时极易当成自己的改动。
**关键事实**：
- 编辑工具会提示 "the file had been modified on disk since you last read it" —— 这是**唯一**的预警信号，别当噪声忽略。
- 判断残留别靠记忆，靠**结构自检**：`grep -n` 变异点的原样 + `git diff` 全量复核（本次就是 `grep -n 'wechat: POST'` 一眼看出 `w.cfg.URL`）。
- 后台跑变异与手工编辑同一批文件**互斥**；要边等边改，先把变异改到不重叠的文件。
**检测方法**：任何 `run_in_background` 的变异/改写脚本被 `TaskStop` 后，先做一次「变异点原样自检」再跑测试；测试红时先问「这是真实缺陷还是残留变异？」。
**修法**：脚本恢复放 `try/finally` 之外再加 `atexit` + 启动时校验备份一致性；更简单——**变异串行前台跑**（单条 30–60s，整轮 5–10min，能接受），或跑完立刻 `git diff` 复核。
**推广**：任何「临时改源文件 → 跑 → 还原」的自动化（变异、codemod、格式化实验）都有这个失效模式；还原失败时**测试结果的方向是随机的**（可能假红，也可能假绿——若残留变异恰好不触发断言）。


---

### T-39. `CreateInBatches` 的 BeforeXxx 钩子对**整片 slice 跑完才 INSERT** —— 批内自增/查库算出来的值必然重复
**状态**: FIXED | **类别**: gorm / 批量写入 / 唯一索引 (G-25 轮) | **修复日期**: 2026-09-10
**现象**：`SyncFromGLPI` 一次同步 ≥2 张新工单**整批失败**，`SyncAll` 把它记成 `glpi: GLPI 批量插入失败: … UNIQUE constraint failed: tickets.ticket_number`；逐条建单（走 `TicketService.Create`）一切正常。看起来像「唯一索引配错了」或「排重没做」。
**根因**：`Ticket.BeforeCreate → generateTicketNumber` 用「**当天已建条数**」算号（`count → A/B/C…`）。gorm 的回调顺序是 `before_create` **先于** `gorm:create`，而 `before_create` 用 `callMethod` 遍历整片 slice 的**每一行**——于是 N 行的钩子全部在同一个 INSERT 之前跑完，各自 `Count()` 查到的是**同一个值**，N 行拿到同一个号。实测同一条 INSERT 的 VALUES 里两行都是 `TICKET-20260910-A`，唯一索引把整条语句拒掉。
**关键事实**：
- 受影响的是**任何「钩子内查库/依赖已落库状态」的字段生成**：自增号、按当天计数、依赖前一行写入的派生值。逐条 `Create` 不受影响（每行插完再算下一行），所以**单条用例永远测不出来** —— 这正是 `TestSyncFromGLPI_两次同步不重复` 当初只喂 1 张票的原因。
- 钩子里的逃生门（`if t.X == ""` 才生成）是批量路径唯一的合法接入点：**插入前把值填好，钩子就不会覆盖**。
- 失败是**整批原子回滚**（gorm 默认把 `CreateInBatches` 包在事务里），不是「插进去一部分」，别按部分成功去写善后。
**检测方法**：给批量路径写用例时**至少喂 2 行**（1 行是假绿的重灾区）；断言分两层 —— ① `require.NoError` 卡住整批被拒，② 遍历结果断言「N 行全部入库 **且** 该字段互不相同」。变异反证就是**删掉插入前的预分配调用**，必须红在 ①的文案上，而不是编译错或 SQL 匹配错。
**修法**：批量前显式预分配 —— 查**一次**当天条数，从该序号起给每行连续赋值（`models.AssignTicketNumbers`，`internal/models/ticket.go`），已有的号跳过不覆盖。
**残留边界**：预分配仍是 **count-based**，与逐条路径同源 —— **删过工单**后 count 回退会算出已占用的号（逐条路径的 5 次重试也救不了：5 次算出的号相同 → 恒 409）。要根治得换成「取当天已用序号的最大值 +1」或数据库序列。
**推广**：「钩子里查库算值」与「批量插入」是**互相不知道对方存在**的两套机制；只要两者相遇，钩子看到的数据库状态就整体滞后一个批次。审任何 `CreateInBatches` / `Save` 切片时，先问：这批字段里有没有哪个是钩子现算的？


---

### T-40. `TicketService.Create` 的撞号重试循环**在显式事务里失效** —— PG 里一条语句失败即整事务 abort，重试没有立足点
**状态**: ACTIVE | **类别**: gorm / 事务 / PG 错误语义 (D-3 轮) | **记录日期**: 2026-09-10
**现象**：`Create` 对 `ticket_number` 唯一冲突会「重算号 + 重试 ≤5 次」，是 D-2 的修法。`CreateFromAlert`（告警一键建单）把建票放进显式事务后，撞号**不再重试**，直接整单失败返回 5xx。行为与 `Create` 不一致，容易被当成 bug 去「补上重试」。
**根因**：两条路径的事务边界不同。`Create` 的每次尝试是**自带隐式事务**的一条语句 —— 失败只回滚那一条，可以再来一次。而在 `db.Transaction(func(tx *gorm.DB))` 里，PG 的语义是**语句级错误即让整个事务进入 aborted 状态**，后续任何语句（包括重试的那条 INSERT）都会被拒：`current transaction is aborted, commands ignored until end of transaction block`。重试循环在显式事务里**不是慢，是不可能**，除非引入 `SAVEPOINT`。
**关键事实**：
- 这不是 sqlite/PG 差异，是**显式事务 vs 隐式事务**的差异；写「事务里失败要重试」之前先看这条语句有没有自己的事务。
- 顺带一个正确的副作用：整事务回滚意味着 `CreateFromAlert` 失败时**认领（条件 UPDATE 写下的 `alerts.ticket_id`）一并撤销**，不会留下指向不存在工单的悬空指针。这正是要的语义 —— 但也意味着「认领 + 建票」必须同事务，不能为了能重试而拆成两个事务。
- 客户端显式传号的路径同样不重试（`clientSuppliedNumber`），语义是「这个号被占了」→ 409，别混。
**检测方法**：给「事务内建票」写失败注入用例时，断言**两件事**：① `require.Error`（撞号确实失败），② 认领被回滚（`alerts.ticket_id` 仍为 NULL / 无残留工单）。只断言 ① 会把「重试成功」和「整单失败」都判成绿。真库造撞号的办法见 `TestTicketService_CreateFromAlert_插票失败时认领回滚`（补唯一索引 + 删当天一条让 count 重算到已占用的号）。
**修法**：保持不重试 + 整事务回滚（当前实现）。若将来确实需要重试，唯一正路是 `SAVEPOINT`（`tx.SavePoint("x")` / `tx.RollbackTo("x")`），且要重新论证与认领的原子性关系 —— **不要**为了让重试跑起来把认领和建票拆成两个事务。
**残留边界**：撞号时用户看到 5xx 而非「自动重试后成功」。根因是 `generateTicketNumber` 仍是 count-based（同 T-39 残留边界），撞号在「删过工单」后是必然的而非偶发。
**推广**：看到一个「失败后重试」的循环被搬进 `Transaction(...)` 里，先问一句：**这条失败语句自己有没有事务？** 没有的话，重试在那个位置就是死代码 —— 它永远不会执行第二次。

### T-41. 迁移文件撞号 = 静默丢一个迁移
**状态**: FIXED (2026-09-10, M16) | **类别**: 迁移 / 数据完整性
**现象**: `backend/migrations/` 里出现两个 `0000NN_*.up.sql`（例如两个人都以为下一个号是自己的）。
`internal/migrate` 的 `Load()` 按版本号归并到 `byVer[ver]`，后读到的（`fs.ReadDir` 字典序）**直接覆盖**前一个 ——
**其中一个迁移永不执行**，而 `schema_migrations` 照样记下该版本。没有报错、没有日志、没有测试会红。
**为什么难查**: 表现出来是「半年前加的索引/回填根本没生效」，没人会想到去怀疑迁移执行器；
而 `migrate.Up` 又因为 `appliedSet[m.version]` 命中而跳过该版本，看起来一切正常。
**解法**:
1. `Load()` 现在**撞号即返回 error**（启动即失败），不再覆盖；up / down 两侧都守，
   判据用文件名（`upFile`/`downFile`）而不是 `upSQL != ""` —— 0 字节的 SQL 文件会让内容判据漏检；
2. 新增迁移前先 `ls backend/migrations/ | sort | tail`，用**实际存在的最大号 + 1**，不要用「计划里预留的号」。

**注意**: 「让号」会留下版本号空洞，这是无害的 —— `Up` 按版本升序、`Down` 只回滚**已应用的最大版本**。
但**给 Down 链加断言时要数清楚**：`backend/tests/db_smoke_test.go` 的 `TestDBSmoke_DownPreservesLegacyColumns`
按「当前最高版本」逐次 Down，且链上断言全是 `assert.False(索引还在)` —— **多滚一层也是全绿**。
该用例已补首尾两条正向断言（000023 被滚掉 / 000012 必须还在）堵住这一点。
**登记**: `docs/FIX-PLAN-M16-PRIORITY.md` §7。


---

## 二、前端陷阱

### T-16. Settings.tsx 死表单 (B1-1/B1-2 修复中)
**状态**: PARTIAL FIX | **修复**: B1-1 (API 密钥), B1-2 (通知渠道)
**现象**: 表单/按钮渲染了但没接 API,看起来能保存其实啥也不发生。
**根因**: 早期迭代时只画了 UI,后端 API 跟前端没同步。
**检测方法**: 全 Settings 集成卡扫一遍 — `<Input>`/`<Button>` 找 `onClick`/`onFinish` 是否实接 API;`<Form.Item name>` 是否真的 `name` 到 state。
**推广**: 任何新加 Settings 集成页必须有 Save/Test/Sync 三按钮实接 API。

### T-17. CI 缺前端测试 (B1-3 已修)
**状态**: FIXED | **修复 commit**: 3725f40
**现象**: 前端 push 后只看 build, vitest/tsc 跑不跑无兜底。
**解法**: CI 加 `npx tsc --noEmit` + `npx vitest run`。

### T-18. Modal Save 接 API 必须 stringify/parse config 嵌套对象
**状态**: ACTIVE | **类别**: 后端 model JSON 字段
**现象**: 后端 `NotificationChannel.Config` 是 `string` (JSON-serialized),前端 form 是 nested object → 直接发会塞错位置。
**解法**: save 时 `JSON.stringify(configObj)`, edit 时 parse 回 nested object (try/catch 兜底)。

### T-19. ESLint `--report-unused-disable-directives` 严抓 disable 注释
**状态**: ACTIVE | **类别**: lint
**现象**: 你加 `// eslint-disable-next-line`, 后来代码改完 disable 不再需要 → 报 "Unused eslint-disable directive"。
**解法**: 加 disable 前想清楚,改完代码后检查 disable 是否多余。

### T-20. Antd v5 Modal `transitionName=""` + waitFor timeout
**状态**: ACTIVE | **类别**: vitest
**现象**: 测试断言 `expect(input).not.toBeInTheDocument()`, Modal 关闭后 DOM 还在 (jsdom transition 不 fire onTransitionEnd)。
**解法**: `transitionName=""` 禁用动画 + `await waitFor(() => expect(...).not.toBeInTheDocument(), { timeout: 3000 })`。

### T-21. tsc baseline 24 errors = 不要新增
**状态**: ACTIVE | **类别**: regression 检测
**现象**: 项目 tsc --noEmit 历史 24 errors, 新代码提交又想"我代码没问题" — 但 baseline 不许涨。
**解法**: 改前先 `git stash -u` 跑一遍记 baseline,改完对比。
**新 test**: 用 `.toBeTruthy()` 替代 `.toBeInTheDocument()` 避免贡献 baseline。

---

## 三、跨模块陷阱

### T-22. 新 endpoint 漏 1 处 = 编译/404/nil panic
**状态**: ACTIVE | **类别**: 新 feature
**现象**: 加 endpoint 只改 service 忘 route,或只改 route 忘 handler,或忘 openapi.yaml sync。
**解法**: **4 件套检查清单** — service 构造 + handler 构造 + route 注册 + handler 实现, 每次加 endpoint 必查 4 项。
**openapi.yaml**: 项目手维护 48K,新 path + schema fields 必同步。

### T-23. mockXxxService 编译失败 = 接口加方法忘 mock field
**状态**: ACTIVE | **类别**: service 测试
**现象**: service 接口加方法,但 hand-rolled mock struct 没同步加字段 → 编译错。
**解法**: 接口加方法必同步 mock struct field + mock method。

### T-24. seed test "no column X" 错误
**状态**: ACTIVE | **类别**: migration 同步
**现象**: model 加列, gorm AutoMigrate 不被 seed test 用 (手写 SQL), `cmd/seed/main_test.go` CREATE TABLE 缺列 → 测报错。
**解法**: model 加列必同步 `cmd/seed/main_test.go` 的手写 CREATE TABLE。

### T-25. routes.go duplicate `alerts := ...` (patch residue)
**状态**: ACTIVE | **类别**: patch hygiene
**现象**: patch 加 handler 时复制粘贴 `alerts := ...` 行,留下 duplicate → 编译歧义。
**解法**: patch 后扫一遍 routes.go block scope, 检查重复定义。

### T-26. `t.Context()` 不是 Go 1.23
**状态**: ACTIVE | **类别**: test helper
**现象**: `testing.T.Context()` 是 Go 1.24+,项目用 1.23 → 编译错。
**解法**: 用 `context.Background()`, grep `t\.Context()` 全仓审计。

### T-27. patch `old_string` 多匹配
**状态**: ACTIVE | **类别**: 工具使用
**现象**: `old_string: "}"` 或 `"if err != nil"` 之类短 snippet 命中 8+ matches → patch 拒绝。
**解法**: 包含 2-3 行 surrounding context, 或 `replace_all=true` (有意全改时)。

### T-42. `scripts/db_smoke.sh` 用 `-run` 白名单挑用例 —— 新用例**静默不跑**
**状态**: ACTIVE | **类别**: 测试基建 / 假绿
**现象**: 2026-09-11（M19 轮）新增 `TestDBSmoke_AlertBulkTransitionGuards` 后跑 `bash scripts/db_smoke.sh`，输出里从 `TestDBSmoke_TicketPriorityNormalize` 直接跳到 `TestDBSmoke_DownPreservesLegacyColumns`，**新用例一行都没有**，而脚本照旧打印 `结果: ✅ 迁移(全新+升级) + 冒烟断言全部通过`。原因：脚本第 196–205 行用显式 `-run 'TestDBSmoke_A|TestDBSmoke_B|…'` 列举要跑的用例，不在此列的测试根本不执行，`go test` 也不会报「你没跑它」——**新写的真库用例再严密，也只是躺在文件里**。这比「用例写错了」危险得多：写错会红，不跑永远绿。
**检测方法**: ① 新增 dbsmoke 用例后，**去脚本输出里找到它的 `--- PASS:` 那一行**，找不到就是没跑；② 更省事的判据：`grep -c '^func TestDBSmoke_' backend/tests/db_smoke_test.go` 与 `-run` 白名单里的分支数对不上就是漏了（注意白名单只覆盖 `TestDBSmoke_` 前缀里该路径适用的那些，另有 `SMOKE_EXPECT_UPGRADE` 那条路径单独一份名单，**两份都要看**）。
**解法**: 加进对应路径的 `-run` 白名单（全新路径 / 升级路径各一份），重跑并确认输出里出现该用例。命名前缀统一为 `TestDBSmoke_` 只是为了让白名单可读，减少漏加概率，**不解决**「漏加就静默」这个本质问题。

### T-43. 状态机只编码在前端，服务端按 id 裸更新 = 状态可被回退
**状态**: ACTIVE | **类别**: 逻辑 / 前后端职责
**现象**: 2026-09-11（M19 轮，告警）。合法状态迁移只写在 `frontend/src/components/AlertTable.tsx` 的 `getAlertActions` 里（problem 才给「确认」、problem/acknowledged 才给「解决」），后端 `Acknowledge`/`Resolve`/`BulkAcknowledge`/`BulkResolve` 四条写路径**只按 id 更新、对当前状态零检查**。于是 `PUT /alerts/{id}/ack` 打在已 resolved 的告警上会把状态**回退**成 acknowledged——该行掉出 `dashboard_service.go` 的 `ResolvedAlerts` 计数、重新落进待处理桶、并多发一条通知；重复 resolve 把 `resolve_time` 推到 now → MTTR 虚高。
**为什么前端那道守卫不算防线**: 列表 5s 轮询。A 与 B 两个值班看同一条，A 先解决，B 的页面仍是旧状态（按钮还在）→ B 点下去服务端照单全收。**可见性判断只配用来省一次请求**；这条规矩 D-3（告警一键建单）已经立过（幂等交后端兜底），本 trap 是它的反面，两处可以互相印证。
**检测方法**: 全仓找「只按主键更新 + 有一个 status/state 列」的写路径（grep `Where("id = ?"` 后跟 `Updates`），看 UPDATE 的 WHERE 里有没有带上合法源状态；再找前端有没有对应的按钮显隐逻辑——**只要显隐逻辑存在而后端没有对应的 WHERE 守卫，就是同一个洞**。批量路径（`id IN ?`）别漏。
**解法**: 合法源状态写进 UPDATE 的 WHERE（`id = ? AND status IN (...)`），**不要**写成「读出来在 Go 里判断再写」——后者有 TOCTOU 窗口，会原样复现这个缺陷。`RowsAffected == 0` 走冷路径复读一次，区分「幂等成功（已在目标态）/ 状态冲突 / 记录不存在」三种。重复请求分两类：已在**目标态** → 幂等成功且**不重写时间戳**（重写会污染 MTTR/MTTD）；已在**更后的终态** → 拒绝（409，不是 500 也不是 400）。批量路径**不因个别 id 状态不合法而整批失败**，让它们落空、`affected` 如实报数。

### T-44. 在 service 层的**消费方**判 `gorm.ErrRecordNotFound` —— 判据永不命中，语义错误静默降级成 500
**状态**: ACTIVE | **类别**: 逻辑 / 分层边界
**现象**: 2026-09-11（M21 轮，gRPC）。`grpcserver/alert_server.go` 的 `GetAlert` 写的是
`if errors.Is(err, gorm.ErrRecordNotFound) { return NotFound }`，但 `service.Get` 返回的是
`service.ErrNotFound`（`alert_service.go:225` 把 gorm 的错误**在 service 层就翻译掉了**）。
判据永不命中 → 「告警不存在」被报成 `codes.Internal`，客户端当服务端故障去重试。
同一个 RPC 里还并行存在第二种病：`status.Errorf(codes.Internal, "%v", err)` 把原始错误文本
**塞进响应**，与 `apierr.Respond` 的出口契约（G-28：原始文本只进日志、且经 `redact.Text`）相反。

**为什么容易被写出来**: `gorm.ErrRecordNotFound` 在 service 层是**正确的**判据
（那里正是 gorm 调用的最近处，全仓 20+ 处在用且都对）。错的是把它带到**上层**：分层之后
上层根本见不到 gorm 的错误类型了。看起来「和别处一样」，实际语义完全不同。

**检测方法**: `grep -rn "gorm.ErrRecordNotFound" --include="*.go" . | grep -v _test`，
凡是出现在 `internal/service/` **之外**的，逐一核对它拿到的是不是 gorm 的原始错误。
另一种形态是 `apierr.TranslateDBError`（它只认 gorm 错误）—— 见 §8 登记，当前无生产调用方。

**解法**: 上层只认 service 的哨兵错误（`ErrNotFound`/`ErrInvalidState`/…），翻译集中到一个函数里
（`grpcserver.serviceErrToStatus` / `apierr.Respond` 家族），**覆盖 service 包全部哨兵**而不是
只覆盖今天可达的那几支 —— 漏掉的那支正是会静默降级的那支。非哨兵错误一律 Internal/500，
文案用通用串，原始文本只经 `redact.Text` 进日志。

### T-45. 无 `ORDER BY` 的查询里「第一张 / 第一条」**没有定义** —— 存的时候按这个序、取的时候按那个序
**状态**: ACTIVE | **类别**: 逻辑 / SQL 语义
**现象**: 2026-09-11（M23 轮，资产退役/恢复）。`asset_service.go` 里 `Retire` 取「第一张有 IP 的
网卡」把地址存进资产级的 `last_known_ip4/6`，`Restore` 再把地址写回「第一张网卡」，两处都写
`Where("asset_id = ?").Find(&networks)` —— **都没有 `ORDER BY`**。Postgres 对不带 `ORDER BY` 的
查询不保证行序：走 `asset_id` 索引时按 `(asset_id, ctid)` 排，而 `Retire` 恰好把该资产下**每一张**
网卡都 UPDATE 了一遍（清空 IP），**ctid 全变** → 退役前后两次 SELECT 的「第一行」可能不是同一张卡，
恢复时 IP 就落到别的网卡上。

**为什么容易被写出来**: 单机小表上顺序"看起来"是稳定的（物理序），跑几次结果一致就容易当成
契约。但顺序由**查询计划**决定，`ANALYZE` / 数据量 / 索引选择一变就变；而 UPDATE 改 ctid 是
**同一段代码自己触发的**，不需要外部条件。另外「第一张」在需求上本来就没定义过 —— 没人写下来，
于是两边各按自己的读法理解。

**检测方法**: `grep -rn '\.Find(&' --include='*.go' internal/ | grep -v 'Order('`，凡是
「顺序会影响结果」的读（取第一条 / 取第一张关联行 / 拿 index 0）都要复核：调用方是否依赖行序？
若依赖，本次读与**消费该顺序的另一处读**是否都带同一套 `ORDER BY`？

**解法**: 给这类读加**显式全序**（`Order("created_at ASC, id ASC")`；带决胜列，因为同一批插入的
`created_at` 可能相同），并把它收敛成**一个 helper** 让存/取两侧共用 —— 两侧各写一份 `ORDER BY`
迟早分叉。本仓约定见 `alert`/`ticket` 的 `created_at DESC, id DESC`。
**注意排序只是让两侧一致，不等于语义正确**：`Retire` 只记 IP 不记它原在哪张卡（资产级列），
所以 IP 原属 NIC[1] 时恢复仍会落到 NIC[0] —— 要精确还原得加来源列，属独立决策。

**伴随的第二种形态（同一轮发现）**: `Restore` 的写回循环里 IPv4 那支有 `i == 0` 守卫、IPv6 那支
**没有** → N 张网卡的资产恢复后每张卡都被写上**同一个 IPv6**（一个地址挂在 N 个接口上）。
两支不对称的守卫是典型的漏写；把它写成「先取第一张、再写一张」（不循环）可以让这种不对称
无从发生。已由 `TestAssetService_Restore_多网卡时只写回第一张` 钉住。

### T-46. gorm `Updates(map)` 里的 `clause.Expr` **不回写 struct 字段** —— 响应体与库静默不一致
**状态**: ACTIVE | **类别**: 逻辑 / ORM 语义
**现象**: 2026-09-11（M24 轮，工单 `closed_at`）。把判据下推到 SQL 表达式
（`updates["closed_at"] = gorm.Expr("CASE WHEN ? = 'closed' THEN COALESCE(?, closed_at, ?) ELSE NULL END", …)`）
之后，`Updates(updates)` 成功执行、**库里值完全正确**，但 `return &t` 里的 `t.ClosedAt` 还是
`First` 读到的旧值 —— 刚关闭的工单 API 回 `closed_at: null`，重开的工单回**旧**的关闭时间。
全程没有任何报错。

**根因**: `gorm.io/gorm@v1.30.0/schema/field.go:582` 的 `fallbackSetter`（`Updates(map)` 用它把
map 的值回写进 struct 字段）末支是：

```go
} else if _, ok := v.(clause.Expr); !ok {
    return fmt.Errorf("failed to set value %#v to field %s", v, field.Name)
}
```

注意这个写法：**`v` 真的是 `clause.Expr` 时该条件不成立** → if 链走完 → 落到末尾 `return`（`err`
仍是 nil），**既不设值也不报错**，字段静默保持原样。那个 `fmt.Errorf` 是留给别的、无法赋值的类型的
（把 Expr 排除掉，正是为了不误报）。**同一个 map 里其它"普通标量值"的键都会正常回写** ——
这就是"平时没问题"的原因，也是它难被发现的原因：一次 `Updates` 里只有表达式那一列不同步。

**为什么危险**: 危险的不是这条 UPDATE，而是**同一个 handler 里返回值与写库值来自两个真相源**。
黑盒测试也照不出来 —— 单测若只断言「库里的值对」就全绿，只有断言**返回值**才会红（本轮补的
`require.NotNil(t, got.ClosedAt, "返回给 handler 的 closed_at 不得是空")` 正是为此）。
它是「把判据下推到 SQL 以消除读-改-写竞态」这个**正确修法的伴生代价**：越是用表达式换掉 Go 侧判断，
越容易踩。

**检测线索**: 任何 `Model(&x).Updates(map…)` 跟着 `return &x` 的地方，只要 map 里出现 `gorm.Expr`
（或 `clause.Assignment` 之类"不是标量"的值），就必须复核返回值从哪来。
`grep -rn 'gorm\.Expr(' --include='*.go' internal/` 逐处看宿主变量有没有被当返回值用。

**解法**: 写完**重读**一次拿真值，并注意用**新 struct 实例**接：

```go
if injectedClosedAt {
    var fresh models.Ticket
    if err := s.db.WithContext(ctx).First(&fresh, "id = ?", t.ID).Error; err != nil { return nil, err }
    t = fresh
}
```

**不要**写成 `First(&t, "id = ?", t.ID)` —— 两支都会踩（实测，非推断）：① gorm 见 dest 已带主键
会**追加**一条主键条件（SQL 变 `WHERE id = $1 AND "tickets"."id" = $2 ORDER BY "tickets"."id" LIMIT $3`，
sqlmock 报 `expected 2, but got 3 arguments`）；② 填不回这个已装满旧值的 struct，用例读到**上一轮的
旧时间**而不是 nil。第 ① 支本仓有独立先例 `asset_service.go:338`（`Restore` 末尾用新 struct 实例重读，
注释写的是「避免事务 `Model.Updates` 把 `asset.ID` 写回后再 `First` 触发重复 bind」）；第 ② 支是
M24 新踩到的。

**另一面（同轮，成对记）**: 把判据放进 SQL 表达式而不是 Go 里 `if t.Status == …`，是为了消除
**读-改-写竞态** —— `t` 是函数开头 `First` 读到的**快照**，两个并发 PUT（一个重开、一个关闭）都卡在
读之后、写之前时会落成 `status='closed'` 且 `closed_at IS NULL` 的自相矛盾行（旧的盲目写法反而
自洽）。代价就是上面的返回值问题，收益是判据与写入在**同一条 UPDATE** 里原子求值。
**注意：行为等价的 Go 快照实现在黑盒用例下全绿**（行为确实等价，差别只在并发窗口），要守住这个
设计选择只能靠**白盒形态断言**：`assert.IsType(t, clause.Expr{}, updates["closed_at"])` ——
本轮变异实验证实，退回 Go 快照只红这一条。

### T-47. 变异反证的**三种假信号** —— 它们都长得像「断言没守住」，实际断言根本没跑或测错了东西

**背景**: 本项目要求每条实现都做变异反证(把实现改坏, 看断言是否变红), 且**红色必须落在预判的那条
断言上** —— 「只要红就行」不算证据。M25 一轮里连续踩到三种假信号, 脚本都报「没红」, 但真因都不在断言。

**信号 1: 变异体让变量/import 变成未使用 → 变异根本没编译** (M25 步骤 2a, `/tmp/m25c_mut.py` V-3/V-6)
删掉 `sort.Strings(keys)` 或去掉 `if hadOld && …` 里的 `hadOld` 后, `sort` 与 `hadOld` 成为未使用 →
`go test` 输出 `build failed`, **没有 `--- FAIL:` 行**。判定脚本只找 `--- FAIL:` → 报「未变红 —— 该变异
没被断言守住」, 把人推向「断言是不是空转」的错误方向(白查一轮)。

**检测线索**: 判「没红」之前先看输出里有没有 `build failed` / `declared and not used`。
两者必须**分开报**: 编译失败 = 变异无效, 不是断言无效。
**解法**: 变异体保持可编译 —— 保留变量使用(`_ = hadOld`), 或改成等价但可编译的写法(如排序改逆序);
脚本显式识别编译失败并单独报错。

**信号 2: 断言引用被测的常量 → 改常量断言跟着改, 永远绿** (同轮 V-7)
用例写的是 `assert.Equal(t, ticketHistoryValueMaxRunes+len(mark), len([]rune(*got)))`, 把截断阈值
500 改成 10 后断言两侧同时变成 10 → **仍然通过**。这是「断言用被测量自身」, 等于没有断言。

**检测线索**: 测试文件里被测包的常量/函数出现在**期望值**一侧(不只是输入一侧)就要复核 ——
输入侧用常量没问题(构造数据), **期望值必须是字面量**。
**解法**: 期望值写死(`assert.Equal(t, 500+len(mark), …)`), 常量改动才会红。

**信号 3: 守门人所在的那一轮根本没执行** (M25 步骤 1, `scripts/db_smoke.sh:197`)
`db_smoke.sh` 的第二轮(upgrade 库)以 `[[ "$rc" -eq 0 ]]` 为门禁 —— fresh 轮一红, upgrade 轮**整轮不跑**。
把「落在 fresh 轮的变异」与「落在 upgrade 轮的变异」**合并成一次运行**(为省一次 ~90s 容器启动)时,
前者的失败让后者的守门人没跑, 于是报告成「未变红」。

**检测线索**: 变异脚本必须能区分「测试跑了且通过」与「测试压根没跑」—— 检查输出里有没有那一轮的
开始标记(如 `② 存量升级路径`)、有没有目标用例的 `=== RUN` 行(T-42 是同一个病的另一面: 白名单让
用例静默不跑)。
**解法**: **不同轮次/不同基座的变异各跑各的**, 不为省时间合并; 脚本记录目标用例是否真被执行过。

**共同教训**: 「变异没让测试变红」有四种可能 —— 断言空转 / 变异无效(编译失败、等价变异) /
守门人没跑 / 测试假绿(T-42) —— **只有第一种是断言的问题**。判定「断言没守住」之前先把另外三种排除掉。

### T-48. sqlite 与 pgx 对 `time.Time` 的处理**相反** —— 任何 sqlite 用例都**结构上不可能**测出 `.UTC()` 的有无

**背景**: M26(同步导入保真)要给 GLPI 的挂钟时间(Asia/Shanghai)做时区转换, 转错了就是**全库时间偏 8 小时**
—— 数据看着有值、只是全错, 是最难发现的那种。原以为「写一条端到端用例断言读回值」就够了。

**踩到的真相**(真 PG 18.4 + pgx v5.5.1 实测):

| 写入同一时刻 | pgx(真 PG)落库 | sqlite 落库 | 读回渲染 |
|---|---|---|---|
| `10:00` Location=`Asia/Shanghai` | `10:00:00`(**丢偏移**) | `10:00:00+08:00`(保留偏移) | PG **18:00 ❌** / sqlite 10:00 |
| `02:00` Location=`UTC` | `02:00:00` | `02:00:00+00:00` | 两者均 10:00 ✅ |

pgx 对 `TIMESTAMP`(无时区)列只写**挂钟数字**、丢弃 Location; sqlite 把偏移一起写进字符串再原样还原。
两者对同一个 `time.Time` 产生**不同**的落库结果, 而 sqlite 那条路的结果**恰好等于**「没调 `.UTC()` 时
pgx 会写出的值」—— 于是「有没有 `.UTC()`」在 sqlite 基座上**不可观测**。

**更隐蔽的第二层(真正的杀招)**: 断言写成 `got.UTC().Format("15:04")` 时, **断言自己又 `.UTC()` 了一次**,
把被测 `.UTC()` 的效果抹平 —— 变异「去掉 `.UTC()`」**不红**。这条假绿躲过了三轮细节审查, 是写实现时
做变异反证才炸出来的(见 `docs/IMPL-SYNC-FIDELITY.md` §7 R1)。

**检测线索**:
1. 断言里对**被测函数已经归一化过**的值再调一次同样的归一化(`.UTC()`/`strings.TrimSpace`/`Sort`)。
2. 用例只在 sqlite 上跑, 却声称守的是**驱动层/方言层**语义。
3. 变异「去掉归一化调用」时, 没有用例变红。

**解法**:
- 纯函数层: 钉**归一化本身**的可观测效果 —— `assert.Equal(t, time.UTC, got.Location())`, 而不是再 `.Format()` 一次。
- 落库层: **必须**上真 PG 断言 `created_at::text` 的字面值(`TestDBSmoke_GLPITimeZoneWallClock`)。
  变异「去掉 `.UTC()`」→ `02:00:00` 变 `10:00:00` → 红, 已实测。

**同类**: T-47(变异假信号)、T-30(sqlite 列名解析大小写不敏感 → 只有断言渲染 SQL 才测得出)。

### T-49. gorm 的 `res.RowsAffected` 在带 `RETURNING` 的批量插入上**等于 `len(batch)`** —— 不是实际插入行数

**背景**: M26 的 `SyncFromGLPI` 用 `CreateInBatches` + `ON CONFLICT DO NOTHING` 做幂等导入, 需要返回
「真正新增了几条」。直觉写法是 `res := tx.Clauses(...).CreateInBatches(...); synced = int(res.RowsAffected)`。

**踩到的真相**(真 PG 18.4 + gorm v1.30.0 实测): `Ticket.ID` 带 `default:gen_random_uuid()` → gorm 把 id 从
INSERT 列表剔除并追加 `RETURNING "id"` → create 回调改走 `QueryContext` + `gorm.Scan`, 而
`scan.go` 的 slice 分支**拿 `RowsAffected` 当 slice 下标**。结果: **只要 ≥1 行被插入,
`RowsAffected` 就等于 `len(batch)`** —— 1 冲突 + 2 新 → 实际插 2, `RowsAffected = 3`; 101 行(1 冲突) → 101。
sqlite 上同样(实测)。而 `ON CONFLICT` 存在的**唯一场景**恰好就是「批里有冲突行」—— 也就是虚报必然发生的那一格。

**已排除的替代**: `db.Omit("RETURNING")` 无效(SQL 里 RETURNING 还在); `clause.Returning{}` 空列会 panic
(`scan.go` 对不可寻址 slice 做 `SetLen`)。

**检测线索**: 批量写入后 `RowsAffected` 恰好等于 `len(batch)` —— 尤其当批里有冲突行时。SQL 日志里
出现 `RETURNING "id"`。

**解法**: 在**同一事务内**对目标行做 COUNT 前后差(`before` / `after`), 而不是信 `RowsAffected`。
守它的用例必须构造**混合批次**(1 冲突 + 1 新): 只有混合批次才能同时区分「真值 1」与「虚报 2」。

### T-50. 部分唯一索引 + `ON CONFLICT`：`WHERE` 必须**蕴含**索引谓词 —— 写宽一段就是 `42P10`

**背景**: M27/D-4 给 `alerts` 建了**部分**唯一索引
`(trigger_id, problem_start) WHERE source='zabbix' AND trigger_id IS NOT NULL AND trigger_id <> ''`,
插入走 `clause.OnConflict{Columns, TargetWhere}`。直觉是「照抄索引谓词就行」, 但很容易漏抄一段
(或反过来觉得「写宽一点更保险」)。

**踩到的真相**(真 PG 18.4 实测, 四种写法各跑一遍):

| # | `ON CONFLICT` 的 WHERE | 结果 |
|---|---|---|
| A | 不写 | `ERROR: there is no unique or exclusion constraint matching the ON CONFLICT specification` |
| B | `trigger_id IS NOT NULL AND trigger_id <> ''`(**少 `source='zabbix'`**) | 同样 `42P10` |
| C | 与索引谓词逐字相同 | `INSERT 0 1` —— 正常 |
| D | 逐字相同 + `AND 1=1` | `INSERT 0 0`(冲突被跳过)—— **也正常** |

即: PG 判的是 **蕴含关系**(ON CONFLICT 的谓词必须蕴含索引谓词), **不是文本相等** —— D 证明多写恒真项没关系,
B 证明**少写一段就废**。所以「写宽一点更保险」是**反的**: 谓词越宽越推不出索引谓词。

同一件事在 sqlite 上宽松得多(仲裁者按**解析树**比较), 于是出现最坏的组合:
**`go test` 全绿、真 PG 上每一次同步都 500**。

**检测线索**: 报错文本逐字是 `there is no unique or exclusion constraint matching the ON CONFLICT specification`,
SQLSTATE `42P10`; 或「sqlite 全绿但真 PG 冒烟红」。**注意 `42P10` 与 `23505` 是两件事**:
`23505` = 有仲裁者但撞了(ON CONFLICT 没生效/没写), `42P10` = 压根找不到仲裁者(谓词/列不匹配)。

**解法**:
1. `TargetWhere` 从索引定义处**复制粘贴**同一段文本, 不要手敲;
2. 真 PG 用例断言 `pg_indexes.indexdef` 的**规范化**文本(不是迁移源码字面 —— PG 会改写成
   `(source)::text = 'zabbix'::text`), 并配一条**反向行为用例**(谓词覆盖不到的来源同键必须能插);
3. 变异反证必须**删掉 `TargetWhere`** 跑一次真 PG 用例 —— 只跑 sqlite 抓不到 `42P10`。

**来源**: M27/A(2026-09-11, `docs/IMPL-ZABBIX-SYNC.md` §8 的 S-1)。同形陷阱见 M26/000026(GLPI 侧)。

---

### T-51. `migrate.Down` 只滚**最新已应用**那一层 —— 新增迁移会让既有「回滚 N 层」用例**静默错位**, 且错位方向指向别处

**背景**: 迁移回滚类冒烟用例(`TestDBSmoke_DownPreservesLegacyColumns`、
`TestDBSmoke_Migration026BlockedByDuplicates`)写的是「Down 一次 → 断言某一层的索引没了」。

**踩到的真相**: `migrate.Down`(`internal/migrate/migrate.go`)只回滚**最新已应用版本**, 不是「你指定的那一层」。
M27 加 000027 时, `Migration026BlockedByDuplicates` 的 `migrate.Down(db)` 滚的变成了 000027,
于是 `require.True(idxGone)` 变红, 而**失败信息说的是「down 000026 没删掉索引」—— 指向完全错误的方向**,
照着它查会浪费一轮。同样地, `DownPreservesLegacyColumns` 的「十三次 Down」整体后移一位。

两个方向都危险:
- **少滚一层** → 断言在错误对象上求值, 可能**假绿**(链上断言全是「索引没了」的 `assert.False`, 晚一步仍为真);
- **多滚一层** → 拆掉后面用例的前置, 一个红变一片红(`Fatalf` 级联)。

**检测线索**: 新增/删除迁移后, 回滚类用例的红点出现在「上一次 Down 的对象」上。
`MigrationReapply` / `MigrateRunner` 这类守恒断言(已应用数 == embed 内 `*.up.sql` 数)能更早暴露。

**解法**:
1. 「滚到第 N 层」写成**循环**(`SELECT count(*) FROM schema_migrations WHERE version > N` + `migrate.Down` 直到 0),
   不写死次数 —— 版本号是不变量, 序数会整体后移;
2. 用例首尾各加一条**正向**断言钉住「头一次 Down 滚的是谁」(少了它就只剩 `assert.False`, 多滚少滚都发现不了);
3. 只写「回滚 0000NN」, **不写「第 N 次」** —— 序数文案是这类注释里最容易变成假话的部分;
4. 新增迁移时**同一次提交**里改掉所有回滚类用例。

**来源**: M27 步骤 3(2026-09-11); 更早的同形教训见 `docs/FIX-PLAN-NETBOX-UPSERT.md` §4 V-6 的 M10
(反向变异「只删最后一次 Down」**不会变红、静默空转**)。

---

### T-52. 安全控制的两侧必须**共享同一套语义** —— 校验器接受的形态, 必须有消费者按该形态消费

**背景**: API Key 支持 `ip_whitelist`。写入侧(`handlers/api_key_handler.go` 的 `validateIPWhitelist`)
校验的是「裸 IP **或 CIDR** 都能过」, 鉴权侧(`middleware/auth.go`)却是 `entry == clientIP`
**字符串精确比较**。

**踩到的真相**: 两侧各自看都「对」, 合起来是**填了 CIDR 的 Key 永远匹配不上** —— 白名单
从"限制来源"退化成"必然 403", 而写入侧的校验**还在放行这个值**, 于是没有任何一处报错。
IPv6 文本形式(`::1` vs `0:0:0:0:0:0:0:1`)是同一个洞的第二张脸。

**检测线索**: 找到每一个「写入侧校验函数」, 列出它**接受哪些形态**, 再去消费侧确认
**每一种形态都真的能被识别**。红旗: 校验用 `ParseIP(x) != nil || ParseCIDR(x) != nil` 这类
**并集**判断, 而消费侧只有 `==`。同形红旗: 校验做了规范化(去空格/小写/补前缀), 消费侧没做。

**解法**:
1. 把「形态判断」收敛到**一个函数**, 两侧调用同一份(`parseTrustedNets` + `Contains`),
   裸 IP 补 `/32`、`/128`, 让 `Contains` 天然覆盖 `==` 的语义;
2. 语义差异(空表 = 放行 vs 空表 = 谁都不信)**写进注释**, 否则后人会当同一语义复用
   (`trusted_proxies` 空表「谁都不信」, API Key 空名单「不做检查」, 正好相反);
3. 用例必须**穿过真实请求**(路由级), 只测校验函数或只测匹配函数都会全绿。

**来源**: M28/A(2026-09-11, G-11, `docs/FIX-PLAN-HARDENING.md` §8.1)。变异 M28-M1/M2 全红在断言上。

---

### T-53. fail-open 守卫的判据不能是「**只有前置中间件才会设置的那个键**」—— 那是自证

**背景**: `RejectAPIKeyAuth` 要禁止「用 API Key 调改密/铸 Key」(防长期凭据自我复制),
判据写的是 `c.GetString("api_key_id") != ""`。

**踩到的真相**: `api_key_id` **只由 `handleAPIKeyAuth` 设置**。某条路由若忘了先挂
`AuthMiddleware`, 该键恒为空 → 守卫判「不是 API Key 身份」→ `c.Next()` **放行**。
守卫**看起来在、实际不设防**, 而且它在所有现存路由上都表现正常(全在 `protected` 组内),
只在**未来某次漏挂**时才咬人 —— 属"潜伏期无限、发作时静默"。

**通用形状**: 判据键的**唯一写入者**恰好就是「守卫要防的那条路径」时, 该判据只能证明
「防的那条路走过了」, 证明不了「该防的路没走」。同形的还有 `c.GetString("auth_type") == "apikey"`
一类「按类型字符串分流」的写法。

**检测线索**: 对每个守卫问一句 —— **「前置不存在时, 这个判据取什么值?」** 如果答案是
"取假值 → 走放行分支", 就是 fail-open。反向判据应当是「**前置不存在时必然为真**」的那个:
这里是「身份已建立」(`user_id` 为空 ⇒ 上游没跑), 因为 `AuthMiddleware` 的**两条**路径
(JWT / API Key)都必然设置它。

**解法**:
1. 判据改挂**所有**认证路径都设置的键(或反向: 挂"身份未建立"这个状态), 缺失即 500 + `Abort`;
2. 选 **500 而非 403** —— 这是服务器配置错误, 要进错误率告警; 403 会被当成正常拒绝而静默;
3. 别指望启动期静态自检: gin 的 `Routes()` 只返回 `RouteInfo`, **拿不到中间件链**,
   注册期断言顺序不可能; 运行期 fail-closed 严格更强(连"组被重建"都能发现);
4. 加这个判据会**打破既有正向用例**(原「会话身份放行」子例两个键都没设), 同一次提交里补键。

**来源**: M28/B(2026-09-11, G-8, `docs/FIX-PLAN-HARDENING.md` §8.2)。变异 M28-M3/M4 红在断言上
(注意: 该修复的两条**初版**变异因删块后 `fmt` 未使用而红在**编译**上, 见 T-31)。

---

### T-54. 同一个 `URL.Path`, 安不安全取决于**怎么写进日志** —— `%#v` 转义 vs 裸拼接(CWE-117)

**背景**: 一个 5xx 日志行里要带请求路径。三处都取了 `c.Request.URL.Path`。

**踩到的真相**: Go 的 `net/http` 读完请求行后按 `url.ParseRequestURI` **解码**转义,
所以 `%0d%0a` 会变成**真实的 CR/LF** 落进 `URL.Path`(裸 CR/LF 进不了请求行, 但转义形式可以)。
于是裸拼接的那处日志被"撑成两行":

```
[ERR] GET /api/assets/abc
[ERR] FORGED code=internal_error internal=boom
```

第二行与真实错误行**无从区分**。三处对照, 结论完全相反:

| 位置 | 写法 | 结论 |
|---|---|---|
| `internal/apierr/apierr.go:41` | `"[ERR] " + … + URL.Path` | **可伪造**(裸拼接) |
| `middleware/audit.go:97` | `Path: c.Request.URL.Path` 落库 | **存储型同源**(导出/渲染时伪造行) |
| `middleware/audit.go:54` | 同名取值只用于 `SkipPaths` 查表 | 无影响 |
| `gin.Logger()` 中间件 | `%#v` 输出 | **安全**(CR/LF 变字面量 `\r\n`) |

**命中前提(别夸大)**: 需已认证 + 一条会走 5xx 的路由 + `%0d%0a` 落在**路径段内**。
gin 按**解码后**的 path 匹配, 所以 `/api/assets%0d%0aX` 会直接 404、**到不了** handler,
必须塞进 `:param` 段(如 `/api/assets/abc%0d%0a…`)。

**检测线索**: grep `URL.Path` / `RequestURI` / `User-Agent` / `Referer` 等**请求侧字符串**
进入日志或落库的位置, 逐个看是 `%#v`/`%q`/结构化字段, 还是 `+` 拼接。
**脱敏 ≠ 转义**: `redact.Text` 处理的是"别泄漏", 完全不碰 CR/LF, 两者不能互相顶替。

**解法**: 拼接处加一层 `logSafe()`(剥 `\r`/`\n` 及控制字符)或改用 `%q`; 落库侧同理;
日志采集端(容器 json-file → 采集器)做多行合并只能缓解、不能替代源头转义。

**来源**: M28/F(2026-09-11, G-43)。**实测**: 真 socket 打 raw 请求行
(`GET /api/assets/abc%0d%0a[ERR]%20FORGED HTTP/1.1`)复现两行输出, 非推演。

### T-55. 安全控制的两半, **顺序**也是语义的一部分 —— 先脱敏后剥控制字符会**重新组装**出被切开的凭据

**背景**: 一个出口要同时做两件事 —— `redact.Text`(别泄漏凭据) 与 `StripControl`(别伪造行 /
别让 PG 因非法字节拒收)。直觉是"两件独立的事, 谁先谁后无所谓", M29 rev1 的注释就是这么写的。

**踩到的真相**: 顺序反了会**泄漏**。`Text` 是**形状识别**(键值形态的值类以空白 / 引号为界),
而 CR/LF 正是"空白"的一种 —— 它会**截断值类**:

| 输入 | 先 `Text` 后 `Strip`(错) | 先 `Strip` 后 `Text`(对) |
|---|---|---|
| `password=abc\nDEF` | `password=***DEF` —— 未遮盖的尾部被**接回**一个已被认成凭据的串上 | `password=***` |
| `pass\nword=SECRET` | `password=SECRET` —— 拼接前 `Text` 看不到 `password=` 这个键, **整条明文留下** | `password=***` |

第二行是"整条泄漏"而不只是尾部。反过来先 Strip, 拼接发生在**识别之前**, 凭据完整 → 遮盖完整。

**M29 的实例**: `notification.markFailed` 写的就是 `Text → … → stripControlChars`, 且旁边注释
断言"顺序不是安全边界" —— 是**活泄漏**, 不是理论可能(同包 `sender.sanitizeSnippet` 一直是对的,
同一份判据两种写法)。修正顺带保证输出合法 UTF-8(`strings.Map` 把非法字节迭代成 U+FFFD),
于是 PG 的 22021 一并关掉。

**阈值口径**: 哪两半算"共享语义"由 T-52 管(校验器接受的形态必须有消费者); 本条只管**顺序**。

**检测线索**: 同一个出口附近同时出现 `redact.Text` 与某种 strip/trim/escape 时, **看相邻两行的
先后**; 注释里若写着"顺序不重要", 那句注释本身就是线索 —— 去读被调函数的**值类边界**,
别信注释。`grep -n -A2 'redact.Text'`, 逐处确认 Strip 在前。

**解法**: 固定 **StripControl → Text → ToValidUTF8 → 按 rune 截断**。
守门用例要**同时**断言"正顺序不泄漏"与"反顺序**确实会**泄漏" —— 只断言前者的话,
把两行调回来用例照样绿(判据不承重)。

**来源**: M29-F(2026-09-12, G-44②, `docs/FIX-PLAN-LOG-INJECTION.md` §1.4/§2.6/§8)。
变异 M29-MF(把两行调回 Text→Strip)红在断言上。与 T-52 互相引用; **不重复 T-54** ——
T-54 说的是"脱敏 ≠ 转义"(两件事不能互相顶替), 本条说的是两件事**都要做时**的先后。

### T-56. 一条规则的输出是另一条规则的输入时, 两条边界**互相咬合** —— 拆开串联会静默放出明文

**背景**: `redact.Text` 曾把规则 1(URL 塌缩)与规则 2/3(Authorization / 键值)按**三次
`ReplaceAll` 顺序执行**串起来, 于是规则 3 的输入里含有规则 1 的**输出**。谁都没写过这条依赖,
但它在承重: `password=https://example.com` 能被遮成 `password=***`, 靠的正是规则 3 看到
规则 1 已经把 URL 塌缩成了 `https://example.com` 这个"值"。

**踩到的真相**: M30 为了修 G-35(端口被规则 3 二次误伤)改成**分段** —— URL 段只过规则 1,
非 URL 段才过规则 2/3, 两条不再串联。方向是对的, 但拆开串联**同时拆掉了那个偶然的兜底**:
非 URL 段只剩悬空的 `password=`(没有值 → 规则 3 不匹配), URL 段又只做塌缩,
整个值**退回明文** —— `password=https://example.com` 从 `***` 变回原样。改动的**目标**是删掉
一处过度脱敏, 实际**附带**放出了一族明文, 而所有"修复项"新用例照样绿。

**检测线索**: 改动形如"先 A 再 B"的串联(A 的输出当 B 的输入)时, 问三句:
① B 的输入从"A 的输出"换成"原文子串"后, B 还能匹配到原来的东西吗?
② 有没有输入是**靠 A 的输出形态**才被 B 认出来的? ③ 只跑新增用例**证明不了**没有净回归 ——
必须跑**差分**(新旧实现喂同一张大用例表, 判据: 旧遮住而新明文 = 净回归)。
M30 的差分表是 12 前缀 × 8 分隔符 × 20 值 × 6 后缀 = 11520 组; 第一版修法只覆盖了
"值=URL"一族, 差分又跑出第二族(重复分隔符 `password==http://…`)与一族既有泄漏。

**解法**: ① 写死**咬合不变式**并写在注释里 —— M30 的是"`URL()` 的输出不含规则 2/3 会认的
凭据形状"(path/query/userinfo 由塌缩丢弃, host 里的键值分隔符塌缩成 `<invalid-url>`);
② 补齐该不变式比补特例稳: 拒绝集必须与规则 3 的分隔符类**同集**(`=`/`＝`/`：`), 差一个字符
就是一族明文; ③ **不要另写一份"必须与既有规则逐字同集"的模式** —— M30 试过"凭据前缀模式",
漏了引号形态与重复分隔符两族; 最终改为**问规则本身**(把哨兵字符接在段尾过一遍规则,
看替换结果是否以 `***` 收尾), 漏的只会是规则漏的, 不会更多。

**来源**: M30(2026-09-12, G-34/G-35, `docs/FIX-PLAN-REDACT-BOUNDARY.md` §8)。两路审计
(正确性 / 安全)**独立**报出同一族净回归, 差分复核补出第二族。变异 M30-M8(不切分判据恒假)、
M30-M9(哨兵换成值类排除字符)、M30-M10(拒绝集收窄回半角)全红在断言上。
与 T-52(两侧共享语义)、T-55(顺序也是语义)互引 —— T-52 管"两侧语义是否同集", T-55 管"两件事
的先后", 本条管"**串联被拆开后**两侧的相互依赖"。

---

## 四、历史 / 已修陷阱 (供考古)

### H-1. pre-commit hook 改 `cmd/server/main.go` 漏 build
**状态**: HISTORICAL (Trap 16) | **修法**: `cmd/server/main.go` 不再在 .gitignore, 文件已 tracked。
**当前**: `backend/cmd/server/main.go` + `grpc.go` 都正常 tracked, trap 已无意义。改 main.go 后**仍建议**手动 `cd backend && go build ./...` 验证 (CI 现在也会跑)。

### H-2. vet 缓存假阳性
**状态**: HISTORICAL (Trap 2) | **现象**: vet 说 "undefined: X", 实际存在。
**修法**: `go test` 才是 ground truth, 不信 vet 缓存。
**当前**: 偶尔仍发生, 解法不变。

### H-3. `cmd/server/main.go` 被 `.gitignore` 拦截
**状态**: HISTORICAL (Trap 14) | **修法**: Trap 14 修过, 现在 tracked。

### H-4. Trap 21 (v2.2 Zabbix) — handler test router Recovery
**状态**: FIXED | **修法**: handler test helper 已统一 `r.Use(gin.Recovery())`。

---

## 五、Traps 索引 (按 trap 号 → 本文档映射)

| skill trap # | 本文档 | 状态 |
|---|---|---|
| 1 | T-9 | ACTIVE |
| 2 | H-2 (vet 缓存) | HISTORICAL |
| 3 | T-27 | ACTIVE |
| 4 | T-9 (same as 1) | ACTIVE |
| 5 | T-1 | ACTIVE |
| 6 | T-2 | ACTIVE |
| 7 | T-7 | ACTIVE |
| 8 | T-27 (same as 3) | ACTIVE |
| 9 | T-3 | ACTIVE |
| 10 | T-4 | ACTIVE |
| 11 | T-6 | ACTIVE |
| 12 | T-9 (same as 1) | ACTIVE |
| 13 | T-1 (postgres variant) | ACTIVE |
| 14 | H-3 | HISTORICAL |
| 15 | (历史 main.go import) | HISTORICAL |
| 16 | H-1 | HISTORICAL |
| 17 | (handler cursor 解析) | ACTIVE |
| 18 | (next_cursor 触发条件) | ACTIVE |
| 19 | (DI setter race) | ACTIVE |
| 20 | T-22 | ACTIVE |
| 21 | T-15 | ACTIVE |
| 22 | T-9 (Go 1.25 variant) | ACTIVE |
| 23 | T-10 | ACTIVE |
| 24 | T-13 | ACTIVE |
| 25 | T-14 | ACTIVE |
| — (G-20 轮) | T-28 | ACTIVE |
| — (G-20 轮) | T-29 | FIXED |
| — (G-22 轮) | T-30 | FIXED |
| — (G-22 轮) | T-31 | FIXED |
| — (G-16 轮) | T-32 | FIXED |
| — (G-16 轮) | T-33 | FIXED |
| — (G-28 轮) | T-34 | FIXED |
| — (G-28 轮) | T-35 | FIXED |
| — (G-33 M1 轮) | T-36 | FIXED |
| — (G-33 M1 rev3) | T-37 | FIXED |
| — (G-33 M3) | T-38 | FIXED |
| — (G-25 轮) | T-39 | FIXED |
| — (D-3 轮) | T-40 | ACTIVE |
| — (M16 轮) | T-41 | FIXED |
| — (M19 轮) | T-42 | ACTIVE |
| — (M19 轮) | T-43 | ACTIVE |
| — (M21 轮) | T-44 | ACTIVE |
| — (M23 轮) | T-45 | ACTIVE |
| — (M24 轮) | T-46 | ACTIVE |
| — (M25 轮) | T-47 | ACTIVE |
| — (M26 轮) | T-48 | ACTIVE |
| — (M26 轮) | T-49 | ACTIVE |
| — (M27 轮) | T-50 | ACTIVE |
| — (M27 轮) | T-51 | ACTIVE |
| — (M28 轮) | T-52 | ACTIVE |
| — (M28 轮) | T-53 | ACTIVE |
| — (M28 轮) | T-54 | ACTIVE |
| — (M29 轮) | T-55 | ACTIVE |
| — (M30 轮) | T-56 | ACTIVE |

---

## 六、Trap 添加约定

新 trap 必须满足:
1. **真实踩过** (不是理论可能)
2. **有 commit/日期/SHA 可追溯**
3. **有明确"如何检测" + "如何修"**
4. **状态**: ACTIVE / FIXED / HISTORICAL, 不允许"无状态"

加新 trap:
1. 找本文档对应类别章节
2. 加 entry, 给唯一编号 (T-N, 接上一个)
3. 更新 §五 索引
4. commit: `docs(traps): T-N <one-liner>`

---

_文档生成于 B1-4 (v2.3 + nightly batch 自动化)。下次审计后请更新状态列。_