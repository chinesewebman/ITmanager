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

### T-37. 校验用的键 ≠ 落库用的键 —— gorm `Updates(map)` 会把 Go 字段名解析到同一列
**状态**: FIXED | **类别**: 输入校验 / ORM 语义 (G-33 M1 rev3) | **修复日期**: 2026-09-09
**现象**：`ChannelService.Update` 的 fail-closed 校验只认小写键 `updates["config"]` / `updates["type"]`，但落库走的是 gorm 的 `Updates(map)`。gorm 对每个键调 `Schema.LookUpField(k)`（先 `FieldsByDBName`、再 `FieldsByName`，**均大小写敏感**），于是 `{"Config": …}` / `{"Type": …}` 完全跳过校验照样写列，`{"id": …}` 还能改主键。真 PG 18 实测：修复前这些请求全部 **HTTP 200** 且坏值落库（`{"Config":12345}` → `config="12345.0"`；`{"Type":"dingtalk"}` + 存量 `{"url":…}` → 坏组合；`{"id":<新uuid>}` → 主键被改，引用它的 `notification_logs.channel_id` 悬空，本仓库无外键约束）。
**根因**：校验层与写入层**各自解析同一份输入**，且解析规则不同（精确小写键 vs ORM 的名字解析）。安全审计与正确性审计独立命中同一处——说明这是「防御建在约定上」的典型形态，不是笔误。
**检测方法**：任何「先校验 map/对象、再把同一个 map/对象交给 ORM」的路径，问两句：① 校验的键集合是否等于落库的键集合？② 有没有大小写变体、Go 字段名、别名能到达同一列？写一条 `{"Config": …}` 的请求打过去看状态码。
**修法**：入口处**键归一化到小写 + 白名单**（`name/type/config/is_enabled/is_default`，其余 → 400），保证「校验的键 == 落库的键」；白名单同时挡掉 `id`/`created_at`。`Update` 还额外把校验后的 `(type, config)` 写回 map（写入值 == 校验值），消除并发交错的坏组合。
**推广**：`Create` 走结构体绑定（JSON 解码大小写不敏感）时没有这个问题——**同一个 API 的读写两条路径可以有完全不同的键解析规则**，只测一条不足以证明契约成立。

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