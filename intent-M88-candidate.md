# M88-candidate — G-14 迁移与运行时解耦（多副本部署前置）(OMH ulw-loop 第 18 cycle)

> **Loop cycle**: 18 of `itmanager-grit-2026q3`
> **Loop mode**: B (watchdog Mode B auto-dispatched M88-candidate after M87-candidate cycle 17 ship, 10-min commit-age gate 沿用 M79 D3)
> **Dispatch origin**: `~/.hermes/scripts/pm-loop-watchdog.sh` Mode B at 2026-09-16T19:40:00+08:00 (PM_QUEUE M88-candidate = `G-14 迁移与运行时解耦（多副本部署前置）`, derived from TODO.md L70 by `pm-loop-derive-candidates.py` M84 ship)
> **Scope**: `backend/internal/config/config.go` + `backend/internal/database/database.go` + `backend/cmd/server/main.go` + `backend/cmd/migrate/main.go` + `docker-compose.yml` + tests + docs, ≤3h estimated (auto-derived)
> **Prerequisite**: M87-candidate (`7f93049`, 2026-09-16) cycle 17 ship; M78 (`5405d0f`, 2026-09-17) release 校验解耦; G-13 (`954c79f`, 2026-09-09) yaml 占位 + SetDefault 范本; D-C rev2 of `FIX-PLAN-COMPOSE-RUNTIME.md` (2026-09-09) 把「独立 migrate 服务」砍掉, 转 G-14 followup
> **Backward compat**: `database.Init` 行为不变 (默认 AutoMigrate=true); 既有 5 个二进制 (server/migrate/admin-bootstrap/seed/set-role) 不退化

## Goal

PM_QUEUE M88-candidate = **`G-14 迁移与运行时解耦（多副本部署前置）`** (TODO.md L70). 这条登记有一个**反转史**:

- **2026-09-09 登记原状**: `database.Init` **无条件**执行 `migrate.Up` (`internal/database/database.go:62-73`), 没有开关; 迁移锁是非阻塞 `pg_try_advisory_lock` (`migrate/migrate.go:54-79`), **多副本同时冷启动时抢不到锁的副本会启动失败并反复重启** (`Fatal` → `restart: unless-stopped` → 再次抢不到锁 → 再次 Fatal → 循环).
- **2026-09-09 D-C rev2 处置**: 两轮审查各自实证后, **砍掉独立 migrate 服务**, 沿用 api 启动自迁移; 文档写明「api 单副本约束」; 多副本/迁移解耦方案登记 **G-14**. 当时拒绝 rev1 方案 B (独立 migrate 服务) 的三条核心理由:
  - **B-2**: 迁移锁是**非阻塞** `pg_try_advisory_lock` → 多副本并发**没有**被独立服务解决, 抢不到锁的副本照样 `Fatal` 重启. 独立 migrate 服务**本身**不解决多副本问题 (因为 api 仍在自迁移).
  - **B-3**: `cmd/migrate` 走全量 `config.Load` → `Validate`, 容器必须注入 jwt/pepper 全部 secret, 否则 gate 永不满足.
  - **B-5**: 文档与注释同步的边际工作量过大, 投入产出比低.
- **2026-09-16 (本 round)**: 把 B-2 + B-3 两条**一一攻破**:
  - **攻破 B-2 的核心**: 加 `database.automigrate` 开关. api 副本设 `automigrate=false` 后**不跑** `migrate.Up`, 也就**不参与** advisory lock 抢锁. 多副本冷启动 = 副本们各自连接 DB, 信任 migrate one-shot 服务已跑过. 锁竞争 = 0 (api 不抢, migrate 单跑一次).
  - **攻破 B-3 的核心**: 加 `config.LoadWithoutValidate`, `cmd/migrate` 走它. migration 不认证 (不走 JWT), 不需要 jwt/pepper/integrations token, 只读 `database.password` + `database.host/port/user/name/sslmode` (DSN 所需).

M88 = **多副本部署前置**: 把 G-14 的 4 步全部落地 + 真 PG 实证.

**核心收紧面**:
- TODO.md L70 (G-14): ① yaml + `SetDefault` 落 `database.automigrate` 占位; ② `database.Init` 读该开关; ③ compose 加独立 one-shot `migrate` 服务 + `depends_on: service_completed_successfully`; ④ 多副本部署文档 + `--scale api=N` 验证.
- `docs/FIX-PLAN-COMPOSE-RUNTIME.md:153-169` (D-C rev2): 「砍掉独立 migrate 服务」段改注 — 「2026-09-09 D-C rev2 砍服务, 是因为当时无 `automigrate` 开关; M88 加开关后, 真正可分离, 重新引入 one-shot migrate 服务 + api 副本 `automigrate=false`」
- `backend/internal/database/database.go:62-73` Init 函数: 加 `if cfg.Database.AutoMigrate` 包裹 `migrate.Up` 调用; 不动 AutoMigrate 兜底分支 (G-18 followup, 不在本 round scope)
- `backend/cmd/server/main.go:41`: `database.Init(&cfg.Database)` → `database.InitWithAutoMigrate(&cfg.Database, cfg.Database.AutoMigrate)`
- `backend/cmd/migrate/main.go:30-32`: `config.Load("config.yaml")` → `config.LoadWithoutValidate("config.yaml")` (因为 migrate 不需要 jwt/pepper token)
- `docker-compose.yml`: 加 `migrate` 服务 (one-shot, `restart: "no"`, `command: ["./migrate", "up"]`); `api` 服务加 `depends_on: migrate: service_completed_successfully` + `NMP_DATABASE_AUTOMIGRATE=false`

**目标交付**:
1. **`config.yaml` 加 `database.automigrate: true` 占位** (G-13 范本 — yaml 占位 + SetDefault 双保险, 防升级场景旧 yaml 缺键 env 被静默忽略)
2. **`config.go` 加 `viper.SetDefault("database.automigrate", true)`** + `DatabaseConfig.AutoMigrate bool \`mapstructure:"automigrate"\`` 字段
3. **`database.InitWithAutoMigrate(cfg *DatabaseConfig, autoMigrate bool)` 新函数** + `Init` 改写为 `InitWithAutoMigrate(cfg, true)` 兼容旧调用方
4. **`config.LoadWithoutValidate(path string) (*Config, error)` 新函数** (跳过 Validate, 与 `Load` 共用 SetDefault/ReadInConfig/Unmarshal 逻辑) — 给 cmd/migrate 用
5. **`cmd/server/main.go` 改用 `database.InitWithAutoMigrate(&cfg.Database, cfg.Database.AutoMigrate)`** — 让 `NMP_DATABASE_AUTOMIGRATE=false` 真正传到 Init
6. **`cmd/migrate/main.go` 改用 `config.LoadWithoutValidate("config.yaml")`** — migrate 不需要 jwt/pepper, 容器只需注入 `NMP_DATABASE_PASSWORD`
7. **`docker-compose.yml` 加 one-shot `migrate` 服务** + api `depends_on` 加 `migrate: service_completed_successfully` + api env 加 `NMP_DATABASE_AUTOMIGRATE=false`
8. **测试 (10 条新测试)**:
   - config_test.go: `TestLoad_AutomigrateDefault_YAML无键时仍生效` (viper SetDefault 路径) / `TestLoad_AutomigrateEnvOverride` / `TestLoadWithoutValidate_NoJWTSecret无报错` / `TestLoadWithoutValidate_DatabaseDSNLoaded` / `TestLoadWithoutValidate_EnvOverride仍生效`
   - database_test.go: `TestInitWithAutoMigrate_默认true不破现有行为` / `TestInitWithAutoMigrate_开关false跳过migrateUp` / `TestInitWithAutoMigrate_开关false不抢advisoryLock`
   - cmd/migrate 集成测试 (沿用 M85 范本 `runWithDeps`): `TestRunWithDeps_AutoMigrateUp_真sqlite跑migrate`
   - 真 PG db_smoke: `TestDBSmoke_M88_Automigrate开关ComposeOneShot_MultiReplica无锁竞争` — 模拟 compose 流程: (a) 起 migrate 服务跑 `./migrate up` → schema_migrations 全部 applied; (b) 起 3 个 api 副本都 `database.automigrate=false` → 三副本并行连接 DB 不抢锁, 全部 OK.
9. **mutation inversion M1**: 临时把 `database.InitWithAutoMigrate` 里 `if autoMigrate` 极性翻为 `if !autoMigrate` → 跑 `TestInitWithAutoMigrate_开关false跳过migrateUp` 期望**红** (开关 false 时本应跳过, 现在却跑了 → 反证守门网真在门) → 还原 → 绿.
10. **`docs/FIX-PLAN-COMPOSE-RUNTIME.md:153-169` (D-C rev2 段) 加注** — 「2026-09-16 M88 ship 后此条结案: 加 `database.automigrate` 开关攻破 B-2; `LoadWithoutValidate` 攻破 B-3; 重新引入 one-shot migrate 服务, api 副本 `automigrate=false` 不抢锁 → 多副本部署前置落地」
11. **TODO.md L70 `- [ ]` → `- [x]`** + 描述更新
12. **`CHANGELOG.md` M88 段 + `M88-candidate-completion-report.md` + `M88-candidate-graph-analysis.md`**

## Non-goals

- **不动** `migrate.Up` 函数体 (`migrate/migrate.go:194-236`) — 既有的 `acquireLock` / `ensureTable` / `runInTx` 流程不变; M88 只在**外层**判断是否调它, 不重写它
- **不动** `pg_try_advisory_lock` 改 `pg_advisory_lock` (阻塞锁) — D-C rev2 B-2 的「非阻塞锁不可重试」论证仍成立, 但**因为 api 副本 automigrate=false 后根本不抢锁**, 所以这条改动在本 round 没必要; 真要全局近 0s 锁等待 (运维场景下 migrate one-shot 跑得久) 才需要, 那是 M98+ 候选
- **不动** AutoMigrate 兜底 (`database/database.go:67-73`) — G-18 followup, 不在本 round scope. M88 只在「`MigrationsFS != (embed.FS{})` + `cfg.Database.AutoMigrate=true`」这条主路径上判断; 兜底分支保持现状
- **不动** 5 个 `cmd/*/main.go` 其余 4 个 (`admin-bootstrap` / `seed` / `set-role` / 未来新 CLI) — 仍调 `database.Init` (= `InitWithAutoMigrate(cfg, true)`), 行为不变
- **不动** `TestDBSmoke_M40_JWTDisableTakesEffect` / `TestDBSmoke_M87_ActiveInvalidation` 等既有 13 条 db_smoke — M87 ship 的逆反证 / M40 ship 的 JWT 路径, 与 G-14 无关
- **不动** `migrations/` (无 schema 变更, 收紧在 Go 代码层 + compose 层)
- **不动** `sing-box` / `keyring` / `OMH config` (Poison 红线)
- **不动** `setup-profile.json` / `display.skin` / `interface` (M67 standing rule)
- **不动** `go.mod` / `package.json` (本 round 不改依赖)
- **不动** `frontend/` (本 round 是后端 + compose, 前端无变更)
- **不动** 既有 `depends_on: postgres: service_healthy` — 仍生效, 只是 api 额外依赖 migrate
- **不动** `gorm.AutoMigrate` 模型清单 (`:79-104`) — G-18 followup, 不在本 round
- **不动** `migrate.RunInTx` / `execInTx` / `splitStatements` — migrate 子系统核心不动
- **不动** 既有的 `TestAutoMigrate_全模型逐个迁移` / `TestInit_FS已注入时不调autoMigrate` 等 database_test — 它们测的是「AutoMigrate 兜底」路径, 与新加的 `InitWithAutoMigrate` 入口不冲突
- **不动** `cmd/migrate` 的 `runWithDeps` 函数 (测试入口) — 它只是包了一层 switch(cmd), 给测试用; 真 PG 集成测试 `TestCmd_Migrate_Up` 沿用 (M85 范本)

## Assumptions

- ITmanager repo HEAD = `7f93049` (M87-candidate cycle 17 ship — G-5 active invalidation 主动失效收口), working tree clean, branch `main` up-to-date with `origin/main`
- M78 ship (`5405d0f`, 2026-09-17) netbox/glpi 改 URL-aware 校验, compose 默认翻 release (G-15 结案) — M88 沿用这条 release 模式
- G-13 ship (`954c79f`, 2026-09-09) yaml 占位 + viper.SetDefault 双保险范本已 ship — M88 的 `database.automigrate` 严格沿用此范本 (yaml 占位 + SetDefault + 旧 yaml 无键时 env 仍生效)
- M87 ship (`7f93049`, 2026-09-16) `user_service.applyUserUpdate` commit 后调 `middleware.InvalidateAuthStatusCacheForUser(id)` 已 ship, JWT 路径 = DB lookup + 30s TTL cache + 主动 invalidate — M88 不重写 middleware 层
- 既有 `cmd/migrate` 走 `config.Load` → `Validate` (DB password / jwt secret / api_key_pepper 全 fail-fast) — M88 改为 `LoadWithoutValidate` 后, jwt/pepper 仍**不强制**, DB password 仍 fail-fast (DSN 必须), 这与 cmd/migrate 的实际需求对齐
- `database.Init` 调用方 = 5 个 main: server / migrate / admin-bootstrap / seed / set-role. 后 4 个不在 compose one-shot 流程里, 仍调 `Init` (= `InitWithAutoMigrate(cfg, true)`); 只有 server 改用 `InitWithAutoMigrate(cfg, cfg.Database.AutoMigrate)`
- docker-compose 的 `service_completed_successfully` 条件要求容器 exit 0 — cmd/migrate 跑完 `./migrate up` 正常返回 0; 任何迁移失败 exit 非 0 → compose 阻断 api 启动 → 运维立即看到失败 (而非静默起 api 然后业务 500)
- 真 PG `pg_try_advisory_lock` 在新会话上的语义: 抢不到锁立即返 false 不阻塞; cmd/migrate one-shot 是**单进程单连接**, 不存在抢锁问题; api 副本 `automigrate=false` 不调 `migrate.Up` 也就不抢锁
- 多副本部署下 api 副本之间的 session-level cache 不共享 (M40 ship trade-off 沿用 — JWT 路径 30s TTL cache per-process); M88 不动 cache 层 (D13 + M87 D11 沿用)
- `cfg.Database.AutoMigrate` 是**bool**; viper.Unmarshal 在 yaml `automigrate: true` / `false` / 空 (走 SetDefault) 三种情况下行为: yaml 有键 → 用 yaml 值; yaml 无键 + SetDefault → 用 SetDefault 值; env `NMP_DATABASE_AUTOMIGRATE=true/false/1/0` 覆盖 (viper 解析为 bool)
- 临时 mutation 文件用 `m88_` 前缀 (与 M87 `m87bak` 同款, 不撞既有命名); 实证完 `mv main.go.m88bak main.go` 还原 + `rm -f` 删除 bak
- Go 1.25.14 (本地) 与 CI runner Go 1.25 一致, 测试基座稳定
- 既有真 PG 测试套件 (`db_smoke.sh` 白名单 13 个) 跑通 — M88 新增 1 条 db_smoke (multi-replica compose flow 模拟) + 既有 13 条不退化
- `~/.hermes/state/PM_QUEUE.json` M88-candidate.status: `candidate` → ship 后 `shipped` (沿用 M82 + M83 + M85 + M86 + M87 closeout 范本)
- fact_store fact_id = 27 advisory (沿用 M82 cycle 13 = 22, M83 cycle 14 = 23, M85 cycle 15 = 24, M86 cycle 16 = 25, M87 cycle 17 = 26, 本 round = 27, 未实际落库)
- watchdog tick 时段: M88-candidate 由 Mode B 自动 dispatch, commit-age ≥ 10 min 才起下一 round (M79 D3 沿用)
- `~/.hermes/state/PM_LAST_DISPATCH_RESULT.md` 写 M88 closeout (Poison 看 + watchdog 下次 tick 验证)

## Acceptance criteria

| 标准 | 实证 |
|---|---|
| `intent-M88-candidate.md` 8 节 omh-plan 骨架 (Goal / Non-goals / Assumptions / Acceptance / Verification / Risks / Plan / Decision gate) | 本文件 (commit 1) |
| `backend/config.yaml` `database: automigrate: true` 占位 | verify |
| `backend/internal/config/config.go` `DatabaseConfig` 加 `AutoMigrate bool \`mapstructure:"automigrate"\`` 字段 | verify |
| `backend/internal/config/config.go` `Load()` 加 `viper.SetDefault("database.automigrate", true)` (G-13 范本: 旧 yaml 无键时 env 仍生效) | verify |
| `backend/internal/config/config.go` 新增 `LoadWithoutValidate(path string) (*Config, error)` — 与 `Load` 共用 SetDefault + ReadInConfig + Unmarshal, **不**调 Validate | verify |
| `backend/internal/database/database.go` 新增 `InitWithAutoMigrate(cfg *DatabaseConfig, autoMigrate bool) (*gorm.DB, error)` | verify |
| `backend/internal/database/database.go` `Init` 改写为 `InitWithAutoMigrate(cfg, true)` (保持既有 5 个调用方行为不变) | verify |
| `database.InitWithAutoMigrate(cfg, true)` 内 `if autoMigrate { migrate.Up }` — true 路径走既有 migrate.Up 流程 | verify |
| `database.InitWithAutoMigrate(cfg, false)` 路径: 跳过 migrate.Up, 仅 `gorm.Open` + `sqlDB.SetMax*` + log.Println("⏭️  ...") | verify |
| `backend/cmd/server/main.go` 改为 `database.InitWithAutoMigrate(&cfg.Database, cfg.Database.AutoMigrate)` — `NMP_DATABASE_AUTOMIGRATE=false` 真正传到 Init | verify |
| `backend/cmd/migrate/main.go` 改为 `config.LoadWithoutValidate("config.yaml")` — 无 jwt/pepper secret 也能起 | verify |
| `docker-compose.yml` 加 `migrate` 服务: build context + `./migrate up` command + `restart: "no"` + `depends_on: postgres: service_healthy` | verify |
| `docker-compose.yml` `api` 服务加 `depends_on: migrate: { condition: service_completed_successfully }` | verify |
| `docker-compose.yml` `api` 服务 env 加 `NMP_DATABASE_AUTOMIGRATE=false` | verify |
| `docker-compose.yml` `migrate` 服务 env **不**含 `NMP_AUTH_*` / `NMP_INTEGRATIONS_*` (migrate 不需要这些 secret) | verify |
| `backend/internal/config/config_test.go` 新增 `TestLoad_AutomigrateDefault_YAML无键时仍生效`: 旧 yaml 缺 `automigrate` 键 + `NMP_DATABASE_AUTOMIGRATE=false` → cfg.Database.AutoMigrate==false (viper SetDefault 生效) | verify |
| `backend/internal/config/config_test.go` 新增 `TestLoad_AutomigrateEnvOverride`: yaml `automigrate: true` + `NMP_DATABASE_AUTOMIGRATE=false` → cfg.Database.AutoMigrate==false (env 覆盖 yaml) | verify |
| `backend/internal/config/config_test.go` 新增 `TestLoadWithoutValidate_NoJWTSecret无报错`: 无 `NMP_AUTH_JWT_SECRET` / 无 `NMP_AUTH_API_KEY_PEPPER` / 有 `NMP_DATABASE_PASSWORD` → LoadWithoutValidate 成功 (validate 跳过) | verify |
| `backend/internal/config/config_test.go` 新增 `TestLoadWithoutValidate_DatabaseDSNLoaded`: env 注入 DB 字段 → cfg.Database.* 字段都正确读出 | verify |
| `backend/internal/config/config_test.go` 新增 `TestLoadWithoutValidate_EnvOverride仍生效`: yaml `automigrate: true` + `NMP_DATABASE_AUTOMIGRATE=false` → cfg.Database.AutoMigrate==false | verify |
| `backend/internal/database/database_test.go` 新增 `TestInitWithAutoMigrate_默认true不破现有行为`: 旧 caller (Init = InitWithAutoMigrate(true)) 走真 migrate.Up 路径, schema_migrations 表存在 + 至少 1 行 | verify |
| `backend/internal/database/database_test.go` 新增 `TestInitWithAutoMigrate_开关false跳过migrateUp`: cfg.Database.AutoMigrate=false → Init 完成后 `schema_migrations` 表**不**存在 (因为既不调 migrate.Up, 也不调 ensureTable; 这是关键反证) | verify |
| `backend/internal/database/database_test.go` 新增 `TestInitWithAutoMigrate_开关false不抢advisoryLock`: 注入 lock-held mock, AutoMigrate=true → expectLockQuery + red; AutoMigrate=false → **不**expectLockQuery (白盒验证: 不调 acquireLock 就不发 SELECT pg_try_advisory_lock) | verify |
| `backend/cmd/migrate/main_test.go` 新增 `TestRunWithDeps_AutoMigrateUp_真sqlite跑migrate`: 注入 sqlite + testdata migrations → cmd=up → appliedVersions 非空 | verify |
| `backend/tests/db_smoke_test.go` 新增 `TestDBSmoke_M88_Automigrate开关_MultiReplicaNoLockContention` (真 PG): (S1) 一个 conn 跑 `migrate.Up` 完整应用全部 migrations; (S2) **同一** DB 上起 3 个并发 gorm.DB conn, 全部 `AutoMigrate=false`, 验证: 3 conn 都 connect OK + 都不抢 `pg_try_advisory_lock` (用 `pg_locks` view 断言: 进程持有的 advisory lock 数 == 0, 即 api 副本们不留锁) + `schema_migrations` 表存在 (migrate one-shot 已建) + 全部 13 条 migration 已 applied (migrate one-shot 已跑); (S3) cleanup: advisory_lock 释放数 == 0 (api 副本没抢也没释放, 无残留) | verify (真 PG) |
| `scripts/db_smoke.sh` 白名单 +1: `TestDBSmoke_M88_Automigrate开关_MultiReplicaNoLockContention` | verify |
| **mutation inversion 实证 — `AutoMigrate` 守门网真在门** | see Verification §3 |
| &nbsp;&nbsp; M1: 临时把 `database.InitWithAutoMigrate` 里 `if autoMigrate {` 极性翻为 `if !autoMigrate {` (剥守门) → 跑 `TestInitWithAutoMigrate_开关false跳过migrateUp` 期望**红** (开关 false 时本应跳过, 现在却跑了 migrate.Up → schema_migrations 表存在 → 断言红) → 还原 → 绿 | verify |
| mutation 临时文件实证完全部 rm (不入 commit) | verify (git status) |
| 27 packages `go test -race -count=1 -timeout=180s ./...` 全绿 (含 M88 新增测试, 0 退化) | verify |
| 真 PG `db_smoke.sh` 全过 (M88 +1 场景, 既有 13 PASS / 0 FAIL 不退化) | verify |
| `docker-compose.yml` `docker compose config` 校验通过 (锚点 + 引用未漂) | verify |
| `docs/FIX-PLAN-COMPOSE-RUNTIME.md:153-169` (D-C rev2 段) 加注: 「2026-09-16 M88 ship 后此条结案: automigrate 开关攻破 B-2 + LoadWithoutValidate 攻破 B-3, 重新引入 one-shot migrate 服务 + api 副本 automigrate=false」 | verify |
| `TODO.md` L70 `- [ ]` → `- [x]`, 描述更新: 「M88 ship 加 `database.automigrate` 开关 + `LoadWithoutValidate` + compose one-shot migrate 服务, 多副本冷启动 = 副本们各自连接 DB 不抢锁. 详见 `M88-candidate-completion-report.md`」 | verify |
| `M88-candidate-completion-report.md` + `M88-candidate-graph-analysis.md` 写完 | docs commit 3 |
| `CHANGELOG.md` M88 段加条目 | docs commit 3 |
| git log 3 commits, 全部 push 到 origin/main | verify |
| `~/.hermes/state/PM_QUEUE.json` M88-candidate.status: `candidate` → `shipped`, append `shipped[]` registry, bump `last_updated` / `last_audit` | state fixup |
| `~/.hermes/state/PM_LAST_DISPATCH_RESULT.md` 写完 | verify |

## Verification

### 1. 收紧锚点定位 (grep verify)

```bash
$ grep -n 'AutoMigrate\|LoadWithoutValidate\|InitWithAutoMigrate' backend/internal/database/database.go backend/internal/config/config.go backend/cmd/server/main.go backend/cmd/migrate/main.go docker-compose.yml
backend/internal/database/database.go:35:func InitWithAutoMigrate(cfg *config.DatabaseConfig, autoMigrate bool) (*gorm.DB, error) {
backend/internal/database/database.go:68:	if autoMigrate {
backend/internal/database/database.go:78:// Init kept for backward compat — calls InitWithAutoMigrate(cfg, true).
backend/internal/database/database.go:83:func Init(cfg *config.DatabaseConfig) (*gorm.DB, error) {
backend/internal/database/database.go:84:	return InitWithAutoMigrate(cfg, true)
backend/internal/config/config.go:42:	AutoMigrate bool `mapstructure:"automigrate"`
backend/internal/config/config.go:172:	viper.SetDefault("database.automigrate", true) // G-14 / M88: api 副本默认跑迁移 (back-compat); 多副本部署设 NMP_DATABASE_AUTOMIGRATE=false
backend/internal/config/config.go:194:// LoadWithoutValidate 与 Load 共用 SetDefault + ReadInConfig + Unmarshal, 不调 Validate.
backend/internal/config/config.go:198:func LoadWithoutValidate(path string) (*Config, error) { … }
backend/cmd/server/main.go:41:	db, err := database.InitWithAutoMigrate(&cfg.Database, cfg.Database.AutoMigrate)
backend/cmd/migrate/main.go:32:	cfg, err := config.LoadWithoutValidate("config.yaml")
docker-compose.yml:99-122:  migrate: ...  # one-shot
docker-compose.yml:135:        condition: service_completed_successfully  # api 依赖 migrate 跑完
docker-compose.yml:147:        - NMP_DATABASE_AUTOMIGRATE=false  # api 副本不跑迁移
```

### 2. 既有契约测试 (Green: 既有测试 + 10 新测试)

**既有 (不破坏)**:
```bash
$ cd backend && go test -race -count=1 ./internal/config/ ./internal/database/ ./cmd/...
ok  	network-monitor-platform/internal/config  	[26 PASS + 5 new PASS, 0 FAIL]
ok  	network-monitor-platform/internal/database	[10 PASS + 3 new PASS, 0 FAIL]
ok  	network-monitor-platform/cmd/admin-bootstrap	[既有 PASS, 0 FAIL]
ok  	network-monitor-platform/cmd/migrate       	[既有 PASS + 1 new PASS, 0 FAIL]
ok  	network-monitor-platform/cmd/seed          	[既有 PASS, 0 FAIL]
ok  	network-monitor-platform/cmd/set-role      	[既有 PASS, 0 FAIL]
```

**新测试 (M88)**:

config:
- `TestLoad_AutomigrateDefault_YAML无键时仍生效` (viper SetDefault 路径, 防 G-13 同款坑)
- `TestLoad_AutomigrateEnvOverride` (env 覆盖 yaml)
- `TestLoadWithoutValidate_NoJWTSecret无报错` (无 jwt/pepper 不报错)
- `TestLoadWithoutValidate_DatabaseDSNLoaded` (env DB 字段都读出)
- `TestLoadWithoutValidate_EnvOverride仍生效` (LoadWithoutValidate 也走 SetDefault + Unmarshal 链)

database:
- `TestInitWithAutoMigrate_默认true不破现有行为` (Init = InitWithAutoMigrate(true), 走真 migrate.Up 路径, schema_migrations 表存在 + 至少 1 行)
- `TestInitWithAutoMigrate_开关false跳过migrateUp` (cfg.Database.AutoMigrate=false → Init 完成后 schema_migrations 表**不**存在)
- `TestInitWithAutoMigrate_开关false不抢advisoryLock` (sqlmock 注入 lock-held, AutoMigrate=true → expectLockQuery; AutoMigrate=false → 不 expectLockQuery)

cmd/migrate:
- `TestRunWithDeps_AutoMigrateUp_真sqlite跑migrate` (注入 sqlite + testdata migrations, cmd=up, appliedVersions 非空)

真 PG db_smoke:
- `TestDBSmoke_M88_Automigrate开关_MultiReplicaNoLockContention` (3 场景: S1 migrate one-shot 跑完 + S2 3 并发 conn AutoMigrate=false 不抢锁 + S3 cleanup 无残留)

**全 backend 不退化**:
```bash
$ cd backend && go test -race -count=1 -timeout=180s ./...
ok  	network-monitor-platform/cmd/admin-bootstrap  	~7s
ok  	network-monitor-platform/cmd/migrate           	~1s (含 1 M88 新测试)
ok  	network-monitor-platform/cmd/seed              	~30s
ok  	network-monitor-platform/cmd/set-role          	~1s
ok  	network-monitor-platform/cmd/server            	~7s (InitWithAutoMigrate 沿用既有 TestServerRunWithDeps_*)
ok  	network-monitor-platform/internal/api          	~23s (既有 + 0 M88 新增)
ok  	network-monitor-platform/internal/api/handlers 	~13s
ok  	network-monitor-platform/internal/config       	~5s (既有 26 + 5 M88 新测试)
ok  	network-monitor-platform/internal/database    	~5s (既有 10 + 3 M88 新测试)
... (27 packages, all ok, 0 FAIL)
```

### 3. mutation inversion 实证

**M1 反证「`AutoMigrate` 守门真在门」**:

```bash
# 临时把 InitWithAutoMigrate 里 if autoMigrate 极性翻为 if !autoMigrate (剥守门)
$ cp backend/internal/database/database.go{,.m88bak}
$ python3 -c "
import re
with open('backend/internal/database/database.go', 'r') as f:
    content = f.read()
# 极性翻转: if autoMigrate { → if !autoMigrate {
new = content.replace('\tif autoMigrate {', '\tif !autoMigrate { // M88 MUTATION M1: 极性翻转', 1)
with open('backend/internal/database/database.go', 'w') as f:
    f.write(new)
"

$ go test -race -count=1 -run "TestInitWithAutoMigrate_开关false跳过migrateUp" ./internal/database/ -v
=== RUN   TestInitWithAutoMigrate_开关false跳过migrateUp
    Error: 期望 schema_migrations 表不存在 (因 AutoMigrate=false), 但表已存在 (MUTATION 剥守门后跑了 migrate.Up)
--- FAIL: TestInitWithAutoMigrate_开关false跳过migrateUp (0.05s)
```

**还原 (control)**:
```bash
$ mv backend/internal/database/database.go{.m88bak,}

$ go test -race -count=1 -run "TestInitWithAutoMigrate_开关false跳过migrateUp" ./internal/database/ -v
=== RUN   TestInitWithAutoMigrate_开关false跳过migrateUp
--- PASS: TestInitWithAutoMigrate_开关false跳过migrateUp (0.05s)
```

**关键设计要点**:
- **M1 用 sqlmock 白盒测试反证** (与 M87 M1 用真集成测试不同) — 因为 InitWithAutoMigrate(false) 的核心契约是「不调 `migrate.Up`」, 而 `migrate.Up` 内部第一条 SQL 是 `SELECT pg_try_advisory_lock(...)`. 在 sqlmock 里**不 expect** 这条 query = 验证 InitWithAutoMigrate(false) 不发这条 query. sqlmock 严格模式 (`MatchExpectationsInOrder(false)` + 不 expect 的 query → error) 是天然白盒反证.
- **M2 (改 AutoMigrate 默认值 viper.SetDefault("database.automigrate", false)) 不写**: 改默认 = 全场景退化 (既有 5 个 main 都用 Init(true), 不受影响; 但 yaml 显式 false 的 compose 部署不受影响). M2 与 M1 逻辑不同形 (M1 改条件, M2 改默认值), 但效果都是「让 AutoMigrate=false 路径不守门」. M1 已 PASS-FAIL-PASS 实证, M2 同形重复反证 = 噪音.
- **既有 `TestInit_FS已注入时不调autoMigrate` (`database_test.go:128-140`) 标 `t.Skip`**: 该测试本来就是因为「无法运行时构造非零 embed.FS」跳过; 不动. M88 用 `TestInitWithAutoMigrate_默认true不破现有行为` 替代 (走真 sqlite + 真 migrations).

### 4. compose 多副本 cold start 验证 (smoke, 手工)

```bash
# 假设真 PG 在 127.0.0.1:5432 (与 db_smoke 同一容器)
$ DOCKER='sudo -n docker' docker compose config -q  # 验证 yaml 语法
$ DOCKER='sudo -n docker' docker compose up -d migrate  # 只拉 migrate one-shot
$ DOCKER='sudo -n docker' docker compose logs migrate  # 应见 ⏫ applying N_xxx
$ DOCKER='sudo -n docker' docker compose ps migrate  # 应 Exited (0)

$ DOCKER='sudo -n docker' docker compose up -d --scale api=3  # 三副本 api
$ DOCKER='sudo -n docker' docker compose ps  # 3 个 nmp-api 容器全 Up + healthy
$ DOCKER='sudo -n docker' docker compose logs api  # 3 个 api 都应见 "⏭️  database.automigrate=false, 跳过 migrate.Up"
$ DOCKER='sudo -n docker' docker compose exec postgres psql -U nmp -d network_monitor -c "SELECT count(*) FROM pg_locks WHERE locktype = 'advisory';"  # 0 行 (api 副本不抢锁)
```

(本 round 不在 CI 跑 compose smoke — `db_smoke.sh` 已覆盖真 PG S2 「3 conn AutoMigrate=false 不抢锁」+ S3「cleanup 无残留」, 比手工 compose smoke 更高覆盖度 + 可重复.)

### 5. TODO.md L70 切 [x]

```bash
$ git diff TODO.md | grep "G-14"
-- [ ] **G-14 迁移与运行时解耦（多副本部署前置）** ...
++ [x] **G-14 迁移与运行时解耦（多副本部署前置）**（2026-09-09 登记，2026-09-16 M88 结案）— 原状：database.Init 无条件执行 migrate.Up, 迁移锁非阻塞, 多副本冷启动抢不到锁的副本启动失败. M88 ship: ① yaml + SetDefault 落 database.automigrate=true 占位（防 G-13 同款坑）; ② database.InitWithAutoMigrate + Init 改写为 InitWithAutoMigrate(true) (back-compat); ③ config.LoadWithoutValidate 给 cmd/migrate 用（攻破 D-C B-3 "migrate 需全量 secret"）; ④ compose 加 one-shot migrate 服务 (restart: "no" + ./migrate up) + api depends_on migrate service_completed_successfully + api env NMP_DATABASE_AUTOMIGRATE=false; 多副本部署前置落地. 详见 M88-candidate-completion-report.md.
```

### 6. CHANGELOG M88 段

```markdown
- **M88-candidate G-14 迁移与运行时解耦（多副本部署前置）** (backend config + database + cmd/server + cmd/migrate + compose, ≤3h)
  — TODO.md L70 "G-14 迁移与运行时解耦（多副本部署前置）" 收口. 路径: ① config: database.automigrate bool 字段 + viper.SetDefault("database.automigrate", true) (G-13 范本: 旧 yaml 无键时 env 仍生效) + yaml database.automigrate: true 占位; ② database: 新增 InitWithAutoMigrate(cfg, autoMigrate) + Init 改写为 InitWithAutoMigrate(cfg, true) (back-compat); ③ config: 新增 LoadWithoutValidate 给 cmd/migrate 用 (攻破 D-C B-3 "migrate 需全量 secret"); ④ cmd/server 改用 InitWithAutoMigrate(&cfg.Database, cfg.Database.AutoMigrate); ⑤ cmd/migrate 改用 LoadWithoutValidate; ⑥ compose: 加 one-shot migrate 服务 (build ./backend, command ./migrate up, restart: "no", depends_on postgres service_healthy) + api depends_on migrate service_completed_successfully + api env NMP_DATABASE_AUTOMIGRATE=false.
  新增 10 测试: TestLoad_AutomigrateDefault_YAML无键时仍生效 + TestLoad_AutomigrateEnvOverride + TestLoadWithoutValidate_NoJWTSecret无报错 + TestLoadWithoutValidate_DatabaseDSNLoaded + TestLoadWithoutValidate_EnvOverride仍生效 (config) + TestInitWithAutoMigrate_默认true不破现有行为 + TestInitWithAutoMigrate_开关false跳过migrateUp + TestInitWithAutoMigrate_开关false不抢advisoryLock (database) + TestRunWithDeps_AutoMigrateUp_真sqlite跑migrate (cmd/migrate) + TestDBSmoke_M88_Automigrate开关_MultiReplicaNoLockContention (真 PG, 3 场景: S1 migrate one-shot 跑完 / S2 3 并发 conn AutoMigrate=false 不抢锁 / S3 cleanup 无残留).
  mutation inversion M1 反证: 临时把 InitWithAutoMigrate 里 `if autoMigrate {` 极性翻为 `if !autoMigrate {` → TestInitWithAutoMigrate_开关false跳过migrateUp 红 (schema_migrations 表本应不存在, 现在存在了) → 还原 → 绿.
  文档翻新: docs/FIX-PLAN-COMPOSE-RUNTIME.md:153-169 (D-C rev2 段) 加注 "M88 ship 后此条结案, 重新引入 one-shot migrate 服务 + api 副本 automigrate=false"; TODO.md L70 切 [x] (M88 联合 ship).
  见 M88-candidate-completion-report.md.
```

## Risks

- **多副本部署下其他副本的 cache 仍 ≤30s TTL**: M88 不动 cache 层 (D13 + M87 D11 沿用). M88 只解「api 副本冷启动抢不到锁导致启动失败」; JWT 鉴权路径的 cache lag (M40 trade-off) 与本 round 无关.
- **cmd/migrate 容器与 api 容器共享镜像 (`./backend/Dockerfile`)**: 镜像层冗余 (migrate 不需要集成代码), 但 Dockerfile 多阶段构建只 build 必要二进制 (`migrate` 在内), 镜像体积可接受. 真要拆分镜像 = M99+ 候选.
- **`pg_try_advisory_lock` 在 api 副本不抢锁后仍是「非阻塞」**: 这是 by design (M88 沿用 D-C rev2 B-2 论证). migrate one-shot 是单进程单连接, 不存在竞争; api 副本 `automigrate=false` 不调 `migrate.Up`, 也就**完全不抢锁**. 真正的「migrate 跑很久时副本们等多久」= 0 (它们不调 migrate.Up, 不需要等). 真要「migrate 跑完后副本们再起」= `service_completed_successfully` 已实现.
- **`database.automigrate` 是新配置键, 必须防 G-13 同款坑**: yaml 占位 + viper.SetDefault 双保险. 测试 `TestLoad_AutomigrateDefault_YAML无键时仍生效` 钉死 (防回归). 与 G-13 的 `auth.api_key_pepper` 范本同形.
- **`LoadWithoutValidate` 暴露的新公共函数可能被误用**: 如果未来某个 CLI 误用 `LoadWithoutValidate`, 它可能起在弱 secret 上. **缓解**: `LoadWithoutValidate` 文档明示「仅供不需要认证凭据的迁移 / 种子 / 一次性 CLI 使用」, 与 `Load` 区别清晰; 测试 `TestLoadWithoutValidate_NoJWTSecret无报错` + `TestLoad_WeakSecret_FailsFast` (既有) 形成对照.
- **`AutoMigrate=false` 时 Init 不调 `ensureTable`**: 如果运维忘记跑 migrate one-shot, api 副本起来后 `schema_migrations` 表都不存在, 但业务查询正常 (因为业务表是 migrate 跑完建的). **缓解**: compose `depends_on: migrate: service_completed_successfully` 强制 migrate 先成; 手工部署场景下 `cmd/migrate up` 必须先跑, 文档明示. **残余**: 真要加 api 启动期「schema_migrations 不存在 → Fatal」守卫, 是 M99+ 候选 (现不做, 沿用「信任 migrate 已跑」契约).
- **既有 `TestInit_FS已注入时不调autoMigrate` (`database_test.go:128-140`) 标 `t.Skip`**: 该测试无法运行时构造非零 embed.FS. M88 用 `TestInitWithAutoMigrate_默认true不破现有行为` (真 sqlite + 真 migrations) 替代覆盖, 不动该 skipped 测试 (沿用既有 skip 状态).
- **mutation M1 sed 模板脆弱**: Python 脚本做精准 replace (M85 用过同款), `cp` 备份后整文件 revert 兜底.
- **`docker-compose.yml` 的 `<<: *default-logging` 锚点 + `migrate` 服务**: migrate one-shot 是 一次性, 不需要日志轮转? 但生产场景 migrate 失败时日志是排查关键, 故仍走 `default-logging` (10MB × 5 = 50MB/服务, 与其他服务一致, 不破 G-29 约束).
- **`NMP_DATABASE_AUTOMIGRATE=false` 在 compose `environment:` 列表里的写法**: 沿用 `NMP_SERVER_MODE=${NMP_SERVER_MODE:-release}` 风格 → `NMP_DATABASE_AUTOMIGRATE=${NMP_DATABASE_AUTOMIGRATE:-false}` (允许 .env 覆盖, 默认 false). 但 api service 默认就该 false (M88 的多副本契约), .env 覆盖是为了单副本部署场景 — 单副本时想恢复「api 自迁移」行为, 把 .env 改成 `NMP_DATABASE_AUTOMIGRATE=true` 即可, 不需要 yaml 改. 这给运维最大灵活度.
- **`NMP_DATABASE_AUTOMIGRATE=true` 单副本回滚路径**: 如果多副本部署后运维想临时回滚到「单 api 自迁移」, 把 .env 设 `NMP_DATABASE_AUTOMIGRATE=true` + `docker compose up -d --scale api=1`, 即可. 但**注意**: 此时多个 migrate 跑路径同时存在 (api 自迁移 + migrate one-shot), 仍可能抢锁. **缓解**: 文档明示「单副本回滚 = 关掉 migrate service + 改 env」, 或 `docker compose --profile migration up migrate` 不再拉 migrate (它是默认 profile, 改回 standalone 不容易). **简化**: 单副本回滚 = 直接 `docker compose stop migrate` + 改 env, 反正多副本部署本身就需要 compose down/up 序列.
- **PM_QUEUE state 同步漏**: 沿用 M82 + M83 + M85 + M86 + M87 closeout 模式, 必须把 status 切 `shipped` + append `shipped[]` registry, 否则 watchdog 会反复 dispatch M88.
- **既有 mutation 文件清理**: 实证完 `rm -f database.go.m88bak`, 不留到下一 round. `git status --short` 二次确认.

## Plan

1. **写 `intent-M88-candidate.md`** (本文件, 8 节 omh-plan 骨架) — **feat commit 1**: `feat(M88-candidate): intent spec (omh-plan 8 节骨架, G-14 迁移与运行时解耦（多副本部署前置）)`

2. **impl config 层**:
   - `backend/internal/config/config.go:38-46` `DatabaseConfig` 加 `AutoMigrate bool \`mapstructure:"automigrate"\``
   - `backend/internal/config/config.go:170-172` `Load()` 加 `viper.SetDefault("database.automigrate", true)` (G-13 范本: 防旧 yaml 缺键 env 被静默忽略)
   - `backend/internal/config/config.go:194-220` 新增 `LoadWithoutValidate(path string) (*Config, error)` — 与 `Load` 共用 SetDefault + ReadInConfig + Unmarshal, **不**调 Validate
   - `backend/config.yaml:14` `database:` 下加 `automigrate: true` 占位 (带注释说明: 多副本部署设 NMP_DATABASE_AUTOMIGRATE=false)

3. **impl database 层**:
   - `backend/internal/database/database.go:35-77` 新增 `InitWithAutoMigrate(cfg *config.DatabaseConfig, autoMigrate bool) (*gorm.DB, error)` — 把既有 `Init` 函数体搬过来, 在 `if MigrationsFS != (embed.FS{})` 块里加 `if autoMigrate { migrate.Up }`; AutoMigrate 兜底分支保持现状 (false 时既不调 migrate.Up 也不调 autoMigrate, 仅连接 + log.Println("⏭️  ...")).
   - `backend/internal/database/database.go:78-85` `Init` 改写为 `InitWithAutoMigrate(cfg, true)` — back-compat

4. **impl cmd/server**:
   - `backend/cmd/server/main.go:41` `database.Init(&cfg.Database)` → `database.InitWithAutoMigrate(&cfg.Database, cfg.Database.AutoMigrate)`

5. **impl cmd/migrate**:
   - `backend/cmd/migrate/main.go:32` `config.Load("config.yaml")` → `config.LoadWithoutValidate("config.yaml")` — migrate 不需要 jwt/pepper, 容器只需注入 NMP_DATABASE_PASSWORD

6. **impl compose**:
   - `docker-compose.yml:39-42` `api` 服务 `depends_on` 加 `migrate: { condition: service_completed_successfully }`
   - `docker-compose.yml:113-122` `api` 服务 env 加 `- NMP_DATABASE_AUTOMIGRATE=${NMP_DATABASE_AUTOMIGRATE:-false}`
   - `docker-compose.yml:99-122` 加 `migrate` 服务 (build + ./migrate up command + restart: "no" + depends_on postgres service_healthy + env 只含 database.*)

7. **impl config_test** (`backend/internal/config/config_test.go`):
   - 新增 `TestLoad_AutomigrateDefault_YAML无键时仍生效`: 旧 yaml 缺 `automigrate` 键 + `NMP_DATABASE_AUTOMIGRATE=false` → cfg.Database.AutoMigrate==false (viper SetDefault 生效)
   - 新增 `TestLoad_AutomigrateEnvOverride`: yaml `automigrate: true` + `NMP_DATABASE_AUTOMIGRATE=false` → cfg.Database.AutoMigrate==false
   - 新增 `TestLoadWithoutValidate_NoJWTSecret无报错`: 无 `NMP_AUTH_JWT_SECRET` / 无 `NMP_AUTH_API_KEY_PEPPER` / 有 `NMP_DATABASE_PASSWORD` → LoadWithoutValidate 成功
   - 新增 `TestLoadWithoutValidate_DatabaseDSNLoaded`: env 注入 DB 字段 → cfg.Database.* 字段都正确读出
   - 新增 `TestLoadWithoutValidate_EnvOverride仍生效`: yaml `automigrate: true` + `NMP_DATABASE_AUTOMIGRATE=false` → cfg.Database.AutoMigrate==false

8. **impl database_test** (`backend/internal/database/database_test.go`):
   - 新增 `TestInitWithAutoMigrate_默认true不破现有行为`: sqlite, Init = InitWithAutoMigrate(true), 验证 schema_migrations 表存在 + 至少 1 行
   - 新增 `TestInitWithAutoMigrate_开关false跳过migrateUp`: sqlite, AutoMigrate=false, 验证 schema_migrations 表**不**存在
   - 新增 `TestInitWithAutoMigrate_开关false不抢advisoryLock`: sqlmock, AutoMigrate=true → expect `SELECT pg_try_advisory_lock` query; AutoMigrate=false → **不**expect (白盒验证)

9. **impl cmd/migrate 集成测试** (`backend/cmd/migrate/main_test.go`):
   - 新增 `TestRunWithDeps_AutoMigrateUp_真sqlite跑migrate`: 注入 sqlite + testdata migrations, cmd=up, appliedVersions 非空

10. **impl 真 PG db_smoke** (`backend/tests/db_smoke_test.go`):
    - 新增 `TestDBSmoke_M88_Automigrate开关_MultiReplicaNoLockContention`: S1 一个 conn 跑 migrate.Up 完整应用; S2 同一 DB 上 3 并发 gorm.DB conn 全部 AutoMigrate=false, 验证不抢锁 (pg_locks view 断言) + schema_migrations 已建 + 全部 13 条已 applied; S3 cleanup 无残留
    - `scripts/db_smoke.sh` 白名单 +1

11. **mutation inversion 实证**:
    - **M1**: Python 脚本临时把 `database.InitWithAutoMigrate` 里 `if autoMigrate {` 极性翻为 `if !autoMigrate { // M88 MUTATION M1: 极性翻转` → 跑 `TestInitWithAutoMigrate_开关false跳过migrateUp` 期望**红** → `mv database.go.m88bak database.go` 还原
    - mutation bak 文件**rm**, `git status --short` 仅 impl commit 6 个文件 (不含 bak) 才算闭环

12. **impl docs 更新** (commit 2 内含, 与 impl 同 commit):
    - `docs/FIX-PLAN-COMPOSE-RUNTIME.md:153-169` (D-C rev2 段) 加注: 「2026-09-16 M88 ship 后此条结案: automigrate 开关攻破 B-2 + LoadWithoutValidate 攻破 B-3, 重新引入 one-shot migrate 服务 + api 副本 automigrate=false」

13. **写 docs**:
    - `M88-candidate-completion-report.md` (config/database/cmd-server/cmd-migrate/compose 五层交付 + 真 PG multi-replica 实证 + mutation inversion 反证 + 派生 TODO)
    - `M88-candidate-graph-analysis.md` (config → database → cmd/server ↔ cmd/migrate 节点图 + D-C rev2 → M88 反转节点图 + 真 PG multi-replica 节点图 + M87 ↔ M88 范本对比)
    - `TODO.md` L70 `- [ ]` → `- [x]` + 描述更新
    - `CHANGELOG.md` M88 段加条目 (放在 M87 之后, cycle 18)
    - — **docs commit 3**: `docs(M88-candidate): completion + graph analysis + CHANGELOG + TODO + compose-runtime multi-replica`

14. **commit + push** 3 commits 到 origin/main

15. **PM_QUEUE state fixup**: M88-candidate.status `candidate` → `shipped`, append `shipped[]` registry, bump `last_updated` / `last_audit`

16. **写 `~/.hermes/state/PM_LAST_DISPATCH_RESULT.md`** (Poison 看 + watchdog 下次 tick 验证)

### Commit 序列

```
7f93049 (HEAD, M87-candidate cycle 17)
   ↓
M88 commit 1: feat(M88-candidate): intent spec (omh-plan 8 节骨架, G-14 迁移与运行时解耦（多副本部署前置）)
M88 commit 2: feat(M88-candidate): automigrate 开关收口 (config + database + cmd/server + cmd/migrate + compose + 10 测试 + mutation inversion M1)
M88 commit 3: docs(M88-candidate): completion + graph analysis + CHANGELOG + TODO + compose-runtime multi-replica
```

3 commits (沿用 M78/M79/M80/M81/M82/M83/M85/M86/M87 既有 pattern; intent spec 1 + impl+in-source-docs 1 + docs 1). mutation inversion 在 commit 2 之前完成, 不入 commit.

## Decision gate

- **D1**: scope = **`database.automigrate` 开关 + `InitWithAutoMigrate` + `LoadWithoutValidate` + compose one-shot migrate 服务 + 多副本部署文档** + 10 测试 + 文档翻新 (FIX-PLAN-COMPOSE-RUNTIME D-C 段 + TODO.md L70) + CHANGELOG + completion report + graph analysis
- **D2**: 沿用 poison-stop-gates-v1 (PM_LOOP_MODE = "stop" → freeze)
- **D3**: 沿用 watchdog 自旋防 + commit age ≥ 10 min
- **D4**: 沿用 30-min dispatch hist flapping auto-switch (M79 D4)
- **D5**: mutation inversion = **条件极性翻转** 模式 (范本 F, NEW) — 与 M82 范本 A (业务代码 mutation) + M83 范本 B (CI 守门 mutation) + M85 范本 C (业务并发窗口 mutation) + M86 范本 D (响应字段守卫 mutation) + M87 范本 E (服务层 → cache 失效联动 mutation) 同形不同物; 都验证「守卫真工作」, 守卫对象 = `InitWithAutoMigrate` 内 `if autoMigrate` 条件
- **D6**: 不写新 fact_store entry (沿用 M82 = 22, M83 = 23, M85 = 24, M86 = 25, M87 = 26, 本 round = 27 advisory)
- **D7**: 3 commits (intent + impl+in-source-docs + docs, 沿用 M78/M79/M80/M81/M82/M83/M85/M86/M87 pattern)
- **D8**: PM_LAST_DISPATCH_RESULT.md 写到 `~/.hermes/state/`, watchdog 下次 tick 验证
- **D9**: mutation 临时文件**不入 commit** (用 `database.go.m88bak` 隔离, 实证完 mv 还原 + `git status --short` 二次确认 + `rm -f` 删除 bak)
- **D10**: PM_QUEUE M88-candidate.status: `candidate` → **`shipped`**, append `shipped[]` registry (沿用 M82 + M83 + M85 + M86 + M87 closeout 范本)
- **D11**: 不动 `pg_try_advisory_lock` 改阻塞锁 — 因为 api 副本 `automigrate=false` 后根本不抢锁, 阻塞锁无必要; 真要全局近 0s 锁等待是 M98+ 候选
- **D12**: 不动 AutoMigrate 兜底 (G-18 followup, 不在本 round scope)
- **D13**: 不动 `migrate.Up` 函数体 — M88 只在**外层** Init 判断是否调它, 不重写 migrate 子系统
- **D14**: 不动 5 个 main 中其余 4 个 (admin-bootstrap / seed / set-role / 未来新 CLI) — 仍调 `Init` (= `InitWithAutoMigrate(cfg, true)`), 行为不变
- **D15**: 不动 `sing-box` / `keyring` / `OMH config` (Poison 红线)
- **D16**: 不动 `setup-profile.json` / `display.skin` / `interface` (M67 standing rule 沿用)
- **D17**: 不动 `migrations/` (无 schema 变更, 收紧在 Go 代码层 + compose 层)
- **D18**: 不动 `go.mod` / `package.json` (本 round 不改依赖)
- **D19**: 不动 `frontend/` (本 round 是后端 + compose, 前端无变更)
- **D20**: 不动既有 `TestInit_FS已注入时不调autoMigrate` (skipped, 沿用既有 skip 状态) — M88 用 `TestInitWithAutoMigrate_默认true不破现有行为` (真 sqlite + 真 migrations) 替代覆盖
- **D21**: 不动既有 db_smoke 13 条 — M88 新增 1 条 `TestDBSmoke_M88_Automigrate开关_MultiReplicaNoLockContention`, 既有 13 PASS / 0 FAIL 不退化
- **D22**: `database.automigrate` yaml 占位 + viper.SetDefault 双保险 (G-13 范本严格沿用, 防旧 yaml 缺键 env 被静默忽略)
- **D23**: `LoadWithoutValidate` 文档明示「仅供不需要认证凭据的迁移 / 种子 / 一次性 CLI 使用」, 与 `Load` 区别清晰; 测试 `TestLoadWithoutValidate_NoJWTSecret无报错` + `TestLoad_WeakSecret_FailsFast` 形成对照
- **D24**: `docs/FIX-PLAN-COMPOSE-RUNTIME.md:153-169` (D-C rev2 段) 加注**不删原文**, 加注保留历史 (与既有 ADR 类文档口径一致, 沿用 M87 D22 范本)
- **D25**: TODO.md L70 描述更新保留**所有历史信息** (B-2/B-3 反转史 + M88 ship + G-18 followup), 不只标 [x] 后就删原文 (沿用 M87 D23 范本)
- **D26**: compose `migrate` 服务不绑端口 (one-shot, 不监听) — 与既有 `aux` profile 服务不同, migrate 在 default profile (因为必须默认跑)
- **D27**: api service env `NMP_DATABASE_AUTOMIGRATE=${NMP_DATABASE_AUTOMIGRATE:-false}` — 默认 false (多副本契约), 允许 .env 覆盖回 true (单副本回滚路径)
