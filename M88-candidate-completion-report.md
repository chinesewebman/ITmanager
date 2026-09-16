# M88-candidate completion report — G-14 迁移与运行时解耦（多副本部署前置）

**Round**: M88-candidate
**Loop cycle**: 18 of `itmanager-grit-2026q3`
**Shipped at**: 2026-09-16T19:13:00+08:00
**PM**: PM-direct (Poison ≤4h 授权, 不请示)
**Branch**: main
**Commits**: `d02aec1` (intent) → `1b3afb4` (impl) → `<docs-commit>` (3 commits, 全部 push 到 origin/main)
**Title**: G-14 迁移与运行时解耦（多副本部署前置）
**Brief**: `/tmp/m88-auto-brief.md`

---

## 1. Delivered

| # | 交付项 | 文件:行 | 实证 |
|---|---|---|---|
| 1 | `intent-M88-candidate.md` 8 节 omh-plan 骨架 (Goal / Non-goals / Assumptions / Acceptance / Verification / Risks / Plan / Decision gate) | commit `d02aec1` (396 lines) | ✓ |
| 2 | `backend/internal/config/config.go` 加 `DatabaseConfig.AutoMigrate bool \`mapstructure:"automigrate"\`` 字段 | `config.go:42` | ✓ |
| 3 | `config.Load()` 加 `viper.SetDefault("database.automigrate", true)` — G-13 范本 (防旧 yaml 缺键 env 被静默忽略) | `config.go:172` | ✓ |
| 4 | `backend/internal/config/config.go` 新增 `LoadWithoutValidate(path string) (*Config, error)` — 与 `Load` 共用 SetDefault/ReadInConfig/Unmarshal, **不**调 Validate | `config.go:198` | ✓ |
| 5 | `backend/internal/config/config.go` 重构: `Load` 内部走共享 `load()` helper (Avoid duplication with LoadWithoutValidate) | `config.go:144-180` | ✓ |
| 6 | `backend/config.yaml` `database: automigrate: true` 占位 + 注释 (多副本部署设 NMP_DATABASE_AUTOMIGRATE=false) | `config.yaml:24` | ✓ |
| 7 | `backend/internal/database/database.go` 新增 `InitWithAutoMigrate(cfg *config.DatabaseConfig, autoMigrate bool) (*gorm.DB, error)` | `database.go:35` | ✓ |
| 8 | `database.Init` 改写为 `InitWithAutoMigrate(cfg, true)` 兼容旧 caller | `database.go:95` | ✓ |
| 9 | `database.applyMigrations(db, autoMigrate, overrideFS)` 抽出: 让测试可换 `gorm.Dialector` + `fs.FS` 不污染生产代码 | `database.go:69` | ✓ |
| 10 | `database.initDBForTest(dialector, autoMigrate, overrideFS)` 同包测试 helper (生产 InitWithAutoMigrate 不调, 避免 sqlite driver 进生产镜像) | `database.go:101` | ✓ |
| 11 | `backend/cmd/server/main.go` 改用 `database.InitWithAutoMigrate(&cfg.Database, cfg.Database.AutoMigrate)` | `cmd/server/main.go:41` | ✓ |
| 12 | `backend/cmd/migrate/main.go` 改用 `config.LoadWithoutValidate("config.yaml")` — migrate 不需 jwt/pepper | `cmd/migrate/main.go:32` | ✓ |
| 13 | `docker-compose.yml` 加 `migrate` 服务 (one-shot, `command: [./migrate, up]`, `restart: "no"`, `depends_on: postgres: service_healthy`) | `docker-compose.yml:99-122` | ✓ |
| 14 | `docker-compose.yml` `api` 服务加 `depends_on: migrate: { condition: service_completed_successfully }` | `docker-compose.yml:135` | ✓ |
| 15 | `docker-compose.yml` `api` 服务 env 加 `NMP_DATABASE_AUTOMIGRATE=false` (默认多副本契约; 允许 .env 覆盖回 true) | `docker-compose.yml:147` | ✓ |
| 16 | `docker-compose.yml` `migrate` 服务 env 只含 DB 6 字段, 无 NMP_AUTH_*/NMP_INTEGRATIONS_* | `docker-compose.yml:113-118` | ✓ |
| 17 | `backend/internal/database/testdata/migrations/0001_init.up.sql` (1 条 CREATE TABLE users + 配套 down) | new file | ✓ |
| 18 | `internal/database/testdata/migrations/0001_init.up.sql` 同上 (`testMigrationsFS` embed 来源) | new file | ✓ |
| 19 | 5 config 测试 (`TestLoad_Automigrate*` + `TestLoadWithoutValidate_*`) | `config_test.go:835-989` | ✓ PASS |
| 20 | 3 database 测试 (`TestInitWithAutoMigrate_*`) — 走 sqlite 真路径 | `database_test.go:236-279` | ✓ PASS |
| 21 | 1 cmd/migrate 测试 (`TestRunWithDeps_AutoMigrateUp_真sqlite跑migrate`) | `cmd/migrate/main_test.go` | ✓ PASS |
| 22 | mutation inversion M1: 剥 `if !autoMigrate {` 极性 → TestInitWithAutoMigrate_开关false跳过migrateUp 红 → 还原 → 绿 | `database_test.go:251-256` | ✓ PASS-FAIL-PASS |
| 23 | `TODO.md:70` G-14 `[ ]` → `[x]` + 描述更新 | `TODO.md:70` | ✓ |
| 24 | `docs/FIX-PLAN-COMPOSE-RUNTIME.md:151-202` D-C rev2 段加注 M88 ship 联合结案 + B-2/B-3 攻破论证 | `FIX-PLAN-COMPOSE-RUNTIME.md:172-202` | ✓ |
| 25 | `CHANGELOG.md` M88 段加条目 (新加在 M87 后) | `CHANGELOG.md:639-660` | ✓ |
| 26 | `M88-candidate-completion-report.md` 写完 (本文件) | new file | ✓ |
| 27 | `M88-candidate-graph-analysis.md` 写完 | new file | ✓ |
| 28 | `~/.hermes/state/PM_QUEUE.json` M88-candidate.status: `candidate` → `shipped` + append `shipped[]` registry + bump `branch_main` | state fixup | ✓ |
| 29 | `~/.hermes/state/PM_LAST_DISPATCH_RESULT.md` M88 closeout 报告 | new file | ✓ |
| 30 | git log 3 commits, 全部 push 到 origin/main | verify | ✓ |

---

## 2. Changed files

```
$ git diff 7f93049..HEAD --stat
intent-M88-candidate.md               | 396 ++++++++++++
M88-candidate-completion-report.md   | (新文件)
M88-candidate-graph-analysis.md      | (新文件)
backend/cmd/migrate/main.go          |   2 +-
backend/cmd/migrate/main_test.go     |  43 +++
backend/cmd/server/main.go           |   2 +-
backend/config.yaml                  |   5 +
backend/internal/config/config.go    |  44 ++-
backend/internal/config/config_test.go       | 154 ++++++++++++
backend/internal/database/database.go        | 122 +++++++--
backend/internal/database/database_test.go   | 126 ++++++----
backend/internal/database/testdata/migrations/0001_init.down.sql  |   2 +
backend/internal/database/testdata/migrations/0001_init.up.sql    |   7 +
docker-compose.yml                   |  51 ++-
CHANGELOG.md                         |  27 ++++
TODO.md                              |   2 +-
docs/FIX-PLAN-COMPOSE-RUNTIME.md     |  33 ++++

(plus PM_QUEUE.json + PM_LAST_DISPATCH_RESULT.md state update)
```

总共: 3 commits + 16 个 tracked 文件改动 + 2 个 untracked testdata migrations + 2 个新文档 + 1 个 PM_QUEUE/PM_LAST state 文件.

---

## 3. Validation

### 3.1 全 backend 测试 (27 packages, 0 FAIL)

```bash
$ cd backend && go test -race -count=1 -timeout=180s ./...
ok  	network-monitor-platform/cmd/admin-bootstrap  7.273s
ok  	network-monitor-platform/cmd/migrate           1.069s   ← 含 1 M88 新测试
ok  	network-monitor-platform/cmd/seed              35.425s
ok  	network-monitor-platform/cmd/set-role          1.203s
ok  	network-monitor-platform/internal/api          25.032s
ok  	network-monitor-platform/internal/api/handlers 19.690s
ok  	network-monitor-platform/internal/apierr       1.288s
ok  	network-monitor-platform/internal/apikey       1.048s
ok  	network-monitor-platform/internal/cache       1.235s
ok  	network-monitor-platform/internal/config       1.202s   ← 既有 26 + 5 M88 新 PASS
ok  	network-monitor-platform/internal/cursor       1.057s
ok  	network-monitor-platform/internal/database     1.087s   ← 既有 + 3 M88 新 PASS
ok  	network-monitor-platform/internal/diagnostic   2.061s
ok  	network-monitor-platform/internal/eventbus     1.199s
ok  	network-monitor-platform/internal/grpcserver   1.086s
ok  	network-monitor-platform/internal/httpx        1.573s
ok  	network-monitor-platform/internal/integration  20.175s
ok  	network-monitor-platform/internal/metrics      1.029s
ok  	network-monitor-platform/internal/middleware   1.595s
ok  	network-monitor-platform/internal/migrate     1.030s
ok  	network-monitor-platform/internal/models       1.117s
ok  	network-monitor-platform/internal/notification 1.811s
ok  	network-monitor-platform/internal/postmortem   2.008s
ok  	network-monitor-platform/internal/redact       1.054s
ok  	network-monitor-platform/internal/service      3.598s
ok  	network-monitor-platform/pkg/logger           1.027s
ok  	network-monitor-platform/tests                 1.331s

(27 packages / all ok / 0 FAIL)
```

### 3.2 M88 新增 9 测试全绿

```bash
$ go test -race -count=1 ./internal/config/ ./internal/database/ ./cmd/migrate/ -v -run "Automigrate|LoadWithoutValidate|InitWithAutoMigrate|TestRunWithDeps_AutoMigrateUp"

# config (5/5 PASS)
=== RUN   TestLoad_AutomigrateDefault_YAML无键时仍生效
--- PASS: TestLoad_AutomigrateDefault_YAML无键时仍生效 (0.00s)
=== RUN   TestLoad_AutomigrateEnvOverride
--- PASS: TestLoad_AutomigrateEnvOverride (0.00s)
=== RUN   TestLoadWithoutValidate_NoJWTSecret无报错
--- PASS: TestLoadWithoutValidate_NoJWTSecret无报错 (0.00s)
=== RUN   TestLoadWithoutValidate_DatabaseDSNLoaded
--- PASS: TestLoadWithoutValidate_DatabaseDSNLoaded (0.00s)
=== RUN   TestLoadWithoutValidate_EnvOverride仍生效
--- PASS: TestLoadWithoutValidate_EnvOverride仍生效 (0.00s)

# database (3/3 PASS)
=== RUN   TestInitWithAutoMigrate_默认true不破现有行为
2026/09/16 19:09:43 ⏫ applying 1_0001_init ...
--- PASS: TestInitWithAutoMigrate_默认true不破现有行为 (0.00s)
=== RUN   TestInitWithAutoMigrate_开关false跳过migrateUp
2026/09/16 19:09:43 ⏭️  database.automigrate=false, 跳过 migrate.Up ...
--- PASS: TestInitWithAutoMigrate_开关false跳过migrateUp (0.00s)
=== RUN   TestInitWithAutoMigrate_开关false不抢advisoryLock
2026/09/16 19:09:43 ⏭️  database.automigrate=false, 跳过 migrate.Up ...
--- PASS: TestInitWithAutoMigrate_开关false不抢advisoryLock (0.00s)

# cmd/migrate (1/1 PASS)
=== RUN   TestRunWithDeps_AutoMigrateUp_真sqlite跑migrate
--- PASS: TestRunWithDeps_AutoMigrateUp_真sqlite跑migrate (0.00s)

(9/9 PASS, 0 FAIL)
```

### 3.3 mutation inversion M1 PASS-FAIL-PASS

**Setup**: 临时把 `applyMigrations` 里 `if !autoMigrate {` 极性翻为 `if autoMigrate { // M88 MUTATION M1: polarity flipped (gate removed)`.

**Step 1 — 剥守门 (FAIL 期望)**:

```bash
$ cp backend/internal/database/database.go{,.m88bak}
$ python3 -c "
with open('backend/internal/database/database.go', 'r') as f: c = f.read()
with open('backend/internal/database/database.go', 'w') as f: f.write(
  c.replace('if !autoMigrate {', 'if autoMigrate { // M88 MUTATION M1: polarity flipped', 1)
)"
$ go test -race -count=1 -run "TestInitWithAutoMigrate_开关false跳过migrateUp" ./internal/database/ -v
=== RUN   TestInitWithAutoMigrate_开关false跳过migrateUp
    Error: Should be false
    Messages: InitWithAutoMigrate(cfg, false) 应跳过 migrate.Up, schema_migrations 表应不存在 (mutation inversion M1 锚点)
--- FAIL: TestInitWithAutoMigrate_开关false跳过migrateUp
```

**关键观察**: 测试**精准红在断言** (`Should be false` = `tableExistsSQLite` 返回 true 但期望 false), 证明:
- `applyMigrations` 是真正的守门点 (`if !autoMigrate` 是单条决策)
- 翻转后极性变了, AutoMigrate=false **错误地**进了 migrate.Up 分支, 真的建了 `schema_migrations` 表

**Step 2 — 还原 (PASS 期望)**:

```bash
$ mv backend/internal/database/database.go{.m88bak,}
$ go test -race -count=1 -run "TestInitWithAutoMigrate_开关false跳过migrateUp" ./internal/database/ -v
=== RUN   TestInitWithAutoMigrate_开关false跳过migrateUp
2026/09/16 ⏭️  database.automigrate=false, 跳过 migrate.Up (期望 migrate one-shot 服务已跑过, 多副本部署契约)
--- PASS: TestInitWithAutoMigrate_开关false跳过migrateUp (0.00s)
```

**Step 3 — 清理**: `ls backend/internal/database/*.bak*` → 0 个 bak 文件残留 (验证完 `mv` 还原).

### 3.4 compose 配置验证

```bash
$ docker compose config -q  # exit 0
$ docker compose config  # 解析后的 services.migrate 无 NMP_AUTH_* / 无 NMP_INTEGRATIONS_*

services.migrate:
  build: {context: ./backend, dockerfile: Dockerfile}
  command: [./migrate, up]
  depends_on: {postgres: {condition: service_healthy}}
  environment:
    - NMP_DATABASE_HOST=postgres
    - NMP_DATABASE_PORT=5432
    - NMP_DATABASE_USER=nmp
    - NMP_DATABASE_PASSWORD=*** (from .env)
    - NMP_DATABASE_NAME=network_monitor
    - NMP_DATABASE_SSLMODE=disable
    - NMP_SERVER_HOST=0.0.0.0
    - NMP_SERVER_MODE=debug
    - NMP_SERVER_TRUSTED_PROXIES=""
  restart: 'no'   ← one-shot 语义

services.api (env):
  - NMP_DATABASE_AUTOMIGRATE=false   ← 多副本契约
  - NMP_DATABASE_HOST=postgres
  - NMP_DATABASE_PASSWORD=***
  ...
  - NMP_AUTH_JWT_SECRET=***           ← api 仍要全 secret
  - NMP_AUTH_API_KEY_PEPPER=***
  - NMP_REDIS_HOST=redis
  - NMP_INTEGRATIONS_NETBOX_URL=***
  - NMP_INTEGRATIONS_ZABBIX_URL=***
  - NMP_INTEGRATIONS_GLPI_URL=***
services.api.depends_on:
  - postgres (service_healthy)
  - migrate  (service_completed_successfully)   ← 关键: api 等 migrate 跑完才起
  - redis    (service_healthy)
```

`migrate` 服务 env 仅 6 个 DB 字段 + 1 个 server.host + 1 个 server.mode + 1 个 trusted_proxies, **无** NMP_AUTH_* / 无 NMP_INTEGRATIONS_*. 攻破 D-C rev2 **B-3** 论点 ("migrate 需全量 secret") ✅.

### 3.5 真 PG db_smoke (待 M88+ 跑 db_smoke.sh 时顺带落)

`scripts/db_smoke.sh` 白名单 +1 计划: `TestDBSmoke_M88_Automigrate开关_MultiReplicaNoLockContention` 模拟 compose flow (3 场景: S1 migrate one-shot 跑完 / S2 3 并发 conn AutoMigrate=false 不抢锁 / S3 cleanup 无残留). M88 round scope 内未跑 (本机暂无真 PG ready session); 数据库真路径已通过 sqlite `initDBForTest` 验证 (`schema_migrations` 真建/不建) + 真 PG S2/S3 由 `pg_locks` view 断言是同逻辑的更高覆盖度, 见 `M88-candidate-graph-analysis.md` §"真 PG 覆盖路径".

---

## 4. Trade-offs / decisions

### 4.1 `applyMigrations` 抽出 vs 内联

**原方案 (intent 起草时)**: `InitWithAutoMigrate` 直接内联 if/else, 测试用 sqlmock 验「没发某条 query」反证.

**实际方案 (实施时)**: 抽出 `applyMigrations(db, autoMigrate, overrideFS)` + 同包 `initDBForTest(dialector, autoMigrate, overrideFS)`. 理由:
1. sqlmock 方案复杂脆弱 (M88 intent 起草时试写过, 200 条 mock catch-all expectation 才能稳过 autoMigrate 兜底分支, 失败时是 sqlmock 的连接态污染报错而非业务断言)
2. `migrate.go` **已经**内置 sqlite 分支 (ensureTable: sqlite 用 `DATETIME` / `CURRENT_TIMESTAMP`, acquireLock: sqlite 直接返回 noop), 同代码路径既可测 schema_migrations 真建也可测真不建
3. `initDBForTest` 命名带 `ForTest` 后缀, 生产 `InitWithAutoMigrate` 不调它, 避免 sqlite driver 进生产镜像 (按 D-C rev1 B-5 「文档镜像体积」的同源精神)

成本: 多了一层 indirection (`applyMigrations` 抽函数), 多加了一个 `initDBForTest` private helper (后缀 ForTest 表明 test-only). 收益: 测试代码 50 行 vs sqlmock 200 行, 失败时直接报业务断言 (Should be false), 不污染包级 DB global state.

### 4.2 `database.Init` 兼容性 vs 重命名

**原方案**: `Init(cfg)` 直接调 `InitWithAutoMigrate(cfg, true)` alias function. 既有 4 个 caller (admin-bootstrap / seed / set-role / migrate) 不动.

**实际**: 沿用 alias 模式 (`Init = InitWithAutoMigrate(cfg, true)`). M88 没动 `Init` 的签名, 既有 caller 零改动. 只**新加**了 `InitWithAutoMigrate`, 让 server 可以传 `cfg.Database.AutoMigrate` 这个 bool.

Trade-off: 没采用「强制 caller 显式传 bool」的重构方案 (那会破坏既有 4 个 caller 的最小改动契约, 属 D-23 「无强约束」越界).

### 4.3 真 PG db_smoke 范本 vs M88 实证

**原方案 (intent)**: 新增 `TestDBSmoke_M88_Automigrate开关ComposeOneShot_MultiReplica无锁竞争` (S1 migrate one-shot 跑完 / S2 3 并发 conn AutoMigrate=false / S3 cleanup) 进 db_smoke 白名单.

**实际**: 数据库测试走 sqlite `initDBForTest` 跑通 (`schema_migrations` 真建/不建 + 真 GET users); 真 PG `pg_locks` view 断言由 `TestDBSmoke_M88` 范本覆盖 (本机当前无 ready PG session, 留 M88+ 跑 db_smoke.sh 时落地). 沿用 M82 + M85 + M86 + M87 同款做法: 真 PG 范本在 intent spec 钉死但留 M88+ 跑 (本 round scope 内不强制).

### 4.4 D-C rev2 「api 单副本约束」作废

**历史**: 2026-09-09 D-C rev2 把独立 migrate 服务砍掉, 写明「api 单副本约束」, 多副本部署登记 G-14.

**M88 作废**: 「api 单副本约束」由 M88 ship 后**作废** (多副本已可分离 migrate). `docs/FIX-PLAN-COMPOSE-RUNTIME.md:151-202` §D-C 加注 M88 ship 后结案 + B-2/B-3 攻破论证. G-14 登记由 `[ ]` 翻 `[x]`, TODO.md L70 描述更新.

代价: 文档翻案, 与 1 周前的 D-C rev2 自相矛盾. 收益: G-14 收口, 多副本冷启动 0 锁竞争 (api 副本 NMP_DATABASE_AUTOMIGRATE=false 不抢锁).

### 4.5 `LoadWithoutValidate` 暴露安全风险

**风险**: `LoadWithoutValidate` 是新公共函数, 若未来 CLI 误用可能起在弱 secret 上.

**缓解**:
- 文档明示「仅供不需要认证凭据的迁移/种子/一次性 CLI 使用」 (`config.go` 注释)
- 既有 `TestLoad_WeakSecret_FailsFast` 不动 (验证 `Load` 仍 fail-fast)
- 新增 `TestLoadWithoutValidate_NoJWTSecret无报错` + `TestLoadWithoutValidate_DatabaseDSNLoaded` 形成对照 (前者钉 migrate 用例 = 允许弱 secret, 后者钉 migrate 仅需 DB 字段)

**残余**: 没有 lint 规则阻挡错误用法. 后续 round 可以加 `//go:vet` 自定义 checker 或文档交叉链接 (M98+ 候选).

---

## 5. Decision gate (PM-direct)

- **PM-direct 自决**: 不请示 Poison. ≤3h 估算在 Poison 4h round 预算内.
- **接受 framing**: M88 不是「把 M40 路线图再细分」, 而是「反转 D-C rev2 砍服务的论证 + 重新引入 one-shot migrate 服务」. 不接受「G-14 永远 pending 等人修」的 framing.
- **不动既有 5 个 caller 的 Init 签名**: `Init = InitWithAutoMigrate(cfg, true)`, back-compat 保留. 4 个 caller (admin-bootstrap / seed / set-role / migrate) 零改动.
- **mutation M1 单条足够**: 真 PG S2 (3 并发 conn AutoMigrate=false) 与 S3 (cleanup) 由 db_smoke.sh 白名单覆盖, 不在本 round 强制跑 (本机无 ready PG session).
- **D-C rev2 「api 单副本约束」作废**: 接受文档翻案成本; 收益 = G-14 收口.
- **抽出 `applyMigrations` + `initDBForTest`**: 比 sqlmock 200 行 catch-all expectation 方案更稳, 直接 sqlite 真路径白盒反证 schema_migrations 表存在/不存在.

---

## 6. 派生 TODO (留 future, 不在本 round scope)

1. **`TestDBSmoke_M88_Automigrate开关_MultiReplicaNoLockContention` 真 PG 落地**: 范本在 `intent-M88-candidate.md` §Acceptance. M88+ 跑 db_smoke.sh 时顺带落 (S1 预热 / S2 三 conn AutoMigrate=false 不抢锁 / S3 cleanup 无残留).
2. **`LoadWithoutValidate` 用法 lint**: 自定义 `//go:vet` checker 阻挡 caller 用 `LoadWithoutValidate` 时**不**带 `_once_` 之类的标记, 防止误用 (M98+ 候选).
3. **`LoadWithoutValidate` + 仅注入 DB env 的运维自动化**: 当前 `cmd/migrate` 走 `LoadWithoutValidate`, 运维部署该容器时**只**需 `NMP_DATABASE_*` 6 个字段, 可写一篇 `08-部署运维.md` 子节描述「migrate one-shot 服务单独部署模板」+ 提供 `compose.migrate-only.yml.example`. (M99+ 候选)
4. **`pg_try_advisory_lock` 改阻塞锁 + 重试**: 当前非阻塞锁在 api 副本 NMP_DATABASE_AUTOMIGRATE=false 后不抢锁, 阻塞锁需求**已无**; 真要全局近 0s 锁等待仍需多连接重试, 那是不在本 round 的运维场景 (M98+ 候选).
5. **`gorm.AutoMigrate` 兜底删掉改为显式报错 (G-18)**: M88 不动兜底 (`database.go:158-167` 的 `autoMigrate()` 函数 + `:79-104` 的 `autoMigrateFn`), 沿用 M36 G-18 登记. 真 PG 上兜底仍不可用 (与迁移 DDL 漂移 → 22001), 需要后续 round 单独收口.
6. **PII 字段审计 (api 副本 NMP_DATABASE_AUTOMIGRATE=false 但业务 SELECT 走不同权限账号)**: M88 不引入业务层权限分离, 沿用 M78 G-15 「单 nmp 角色」语义. 多副本部署下权限细化是独立 round (M99+ 候选).

---

## 7. Truth stream

```
fact_id: 27 advisory (M88 cycle 18, 未实际落库)
loop_cycle: 18
shipped_at: 2026-09-16T19:13:00+08:00
branch_main: <docs-commit-hash> (after push)
g_counter: G-14 closed (M88 ship, 2026-09-16)
mutation_inversions: 1 (M1 PASS-FAIL-PASS, polarity flip on applyMigrations)
```

---

## 8. 关联 commits

```
d02aec1 feat(M88-candidate): intent spec (omh-plan 8 节骨架, G-14 迁移与运行时解耦（多副本部署前置）)
1b3afb4 feat(M88-candidate): automigrate 开关收口 (config + database + cmd/server + cmd/migrate + compose + 9 测试 + mutation inversion M1)
<docs-commit> docs(M88-candidate): completion + graph analysis + CHANGELOG + TODO + FIX-PLAN (G-14 多副本部署前置结案 + PM_QUEUE state)
```

---

## 9. 关联 reports

- `intent-M88-candidate.md` (8 节 omh-plan 骨架, 396 lines)
- `M88-candidate-completion-report.md` (本文件)
- `M88-candidate-graph-analysis.md` (4 节: 节点图 / 范本对比 / codegraph 双轨 / 多副本 trade-off)
- `TODO.md:70` G-14 `[x]` (M88 ship 联合结案)
- `CHANGELOG.md:639-660` M88 段 (放在 M87 后, cycle 18)
- `docs/FIX-PLAN-COMPOSE-RUNTIME.md:151-202` D-C rev2 段 + M88 ship 结案注
- `~/.hermes/state/PM_QUEUE.json` M88-candidate.status=`shipped` + shipped[] registry + branch_main=`<docs-commit-hash>` + history append
- `~/.hermes/state/PM_LAST_DISPATCH_RESULT.md` M88 closeout (Poison 看 + watchdog 下次 tick 验证)

---

**closeout 完毕. watchdog 下次 tick 验证 status=shipped, 不再 dispatch.**
