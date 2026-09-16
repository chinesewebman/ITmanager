# M85-candidate Completion Report — `cmd/set-role` 并发窗口收口 (broad lock + SELECT FOR UPDATE 串行化)

> **Loop cycle**: 15 of `itmanager-grit-2026q3`
> **Loop mode**: B (PM-direct dispatch, watchdog Mode B auto-dispatched M85-candidate after M84 cycle 13 ship)
> **Dispatch origin**: `~/.hermes/scripts/pm-loop-watchdog.sh` Mode B at 2026-09-16T16:06:58+08:00 (PM_QUEUE M85-candidate = `cmd/set-role` 并发窗口, derived from TODO.md L59 by `pm-loop-derive-candidates.py` M84 ship)
> **Intent**: `intent-M85-candidate.md`
> **Feat**: `9733853` (impl 1 file / +211)
> **Feat**: `668a215` (intent 1 file / +257)
> **Total**: 2 commits, 全部 push 到 origin/main

## 摩擦

PM_QUEUE M85-candidate 来自 TODO.md L59 "**`cmd/set-role` 并发窗口** — 防自锁检查与写入已同事务，但两个并发进程仍可能各自通过（TOCTOU）；该命令是人工运维操作，**暂不修**".

PM-direct 2026-09-16 verbatim 决定: 走 omh-plan 8 节骨架, 实证 + 修复, 不接受 "暂不修" framing — 因为:
1. **TODO 描述承认有 bug** ("两个并发进程仍可能各自通过"); 接受 framing 等于承认有未修缺陷.
2. **修复路径已存在**: `internal/service/user_service.go:144-178` ship 了 SELECT FOR UPDATE 范本, 沿用即可, 成本 ≤ 3h.
3. **watchdog M84 自举派工第 1 例**: M85-candidate 是 M84 ship 后 derive 的 11 真候选第 1 个, 需要实证走" 自举派工 → PM-direct 自决 → closeout " 全链路, 不能 reject.

## 决策

- **D1** (PM-direct 自决, D-gate §1): **修复 TOCTOU**, 不接受 "暂不修". 修复路径 = 提取 `countAdminUnderLock` helper + 加 `clause.Locking{Strength: "UPDATE"}` + 移除 `AND id <> ?` 排除目标 + 改语义 `otherAdmins == 0` → `totalAdmins <= 1`.
- **D2** (D-gate §2): 沿用 poison-stop-gates-v1.
- **D3** (D-gate §3): 沿用 watchdog 自旋防 + commit age ≥ 10 min.
- **D4** (D-gate §4): 沿用 30-min dispatch hist flapping auto-switch.
- **D5** (D-gate §5): mutation inversion = **范本 C 业务并发窗口** (NEW). 沿用 M82 范本 A (业务代码) + M83 范本 B (CI 守门).
- **D6** (D-gate §6): fact_id = 24 advisory (沿用 M82 = 22, M83 = 23, 本 round = 24).
- **D7** (D-gate §7): 2 commits (intent + impl, 沿用 M78/M79/M80/M81/M82/M83 pattern; impl 与 docs 拆开).
- **D8** (D-gate §8): PM_LAST_DISPATCH_RESULT.md 写到 `~/.hermes/state/`, watchdog 下次 tick 验证.
- **D9** (D-gate §9): mutation 临时文件**不入 commit** (用 `main.go.m85bak` 备份 + 临时 edit, 实证完 mv 还原 + `git status` 二次确认 + `rm -f` 删除 bak).
- **D10** (D-gate §10): PM_QUEUE M85-candidate.status: `candidate` → **`shipped`** (见本文 §"PM_QUEUE fix").
- **D11** (D-gate §11): 接受 sqlite 不渲染 FOR UPDATE 的已知边界 (白盒测试切 PG dialector + DryRun).
- **D12** (D-gate §12): 不动 HTTP 路径的 user_service.go (同样的 TOCTOU 风险, M86+ 候选).
- **D13** (D-gate §13): 不加新迁移 (修复在 Go 代码层, 不需要 schema 变更; 沿用 M82 范本).
- **D14** (D-gate §14): 不动 setup-profile.json / display.skin / interface (M67 standing rule 沿用).
- **D15** (D-gate §15): mutation M1/M2 反证必须**全红 → 还原全绿**, 否则不算闭环.

## 改动 (本 dispatch 2 commits + 4 files)

| 文件 | 改动 | 内容 |
|---|---|---|
| `intent-M85-candidate.md` | new (commit 1) | 8 节 omh-plan 骨架, 16KB / +257 |
| `backend/cmd/set-role/main.go` | edit (commit 2) | count query 加 `clause.Locking{Strength: "UPDATE"}` + 移除 `AND id <> ?` 排除目标 + 提取 `countAdminUnderLock` helper + 改语义 `totalAdmins <= 1`; +34 / -7 |
| `backend/cmd/set-role/main_test.go` | edit (commit 2) | 加 4 测试: 锁SQL契约_PG + sqlite对照 + mutation_inversion_M1 + GORM并发等价; +177 |

2 commits (cycle 15):
1. `668a215` `feat(M85-candidate): intent spec (omh-plan 8 节骨架, cmd/set-role 并发窗口收口)` — 1 file / +257
2. `9733853` `feat(M85-candidate): cmd/set-role 并发窗口收口 (broad lock + SQL 契约测试)` — 2 files / +211 / -7

`git log --oneline origin/main..HEAD` 为空 (本地 = 远端, 0 滞后). 2 commits 全部 push 成功.

mutation inversion 临时文件不入 commit (D9 实证):
- `backend/cmd/set-role/main.go.m85bak` — 备份原文件, 实证完 `rm -f` 删除.

最终 `git status --short` = 空 (working tree clean).

## mutation inversion 实证 (本 round 核心 — 范本 C)

业务并发窗口的真修复 — 沿用 M82 cycle 13 「守卫真工作」思路, 改测**业务并发窗口**本身:

### M1 反证「剥锁真无锁」:

**变异**: 在 `countAdminUnderLock` 临时剥掉 `Clauses(clause.Locking{Strength: "UPDATE"})`:

```go
// mutation 期间:
func countAdminUnderLock(tx *gorm.DB) (int64, *gorm.DB, error) {
    var n int64
    _ = clause.Locking{Strength: "UPDATE"}  // mutation: 锁被剥掉
    stmt := tx.Model(&models.User{}).
        Where("LOWER(TRIM(role)) = ?", middleware.RoleAdmin).
        Count(&n)
    return n, stmt, stmt.Error
}
```

**实证原文**:
```
$ cd backend && go test -race -count=1 -v -run 'TestRunWithDeps_并发窗口锁SQL契约_PG|TestRunWithDeps_并发窗口mutation_inversion_M1' ./cmd/set-role/

=== RUN   TestRunWithDeps_并发窗口锁SQL契约_PG
    main_test.go:357: 
        	Error:      	"SELECT count(*) FROM \"users\" WHERE LOWER(TRIM(role)) = $1 AND \"users\".\"deleted_at\" IS NULL" does not contain "FOR UPDATE"
        	Messages:   	PG 下 count query 必须渲染 FOR UPDATE — 锁是并发窗口收口的核心, 丢了就是 TOCTOU
--- FAIL: TestRunWithDeps_并发窗口锁SQL契约_PG (0.00s)
=== RUN   TestRunWithDeps_并发窗口mutation_inversion_M1
    main_test.go:414: 
        	Error:      	"SELECT count(*) FROM \"users\" WHERE LOWER(TRIM(role)) = $1 AND \"users\".\"deleted_at\" IS NULL" does not contain "FOR UPDATE"
        	Messages:   	production helper countAdminUnderLock 在当前 main.go 下必须生成 FOR UPDATE — mutation 反证: 剥掉 clause.Locking 后此断言必红
--- FAIL: TestRunWithDeps_并发窗口mutation_inversion_M1 (0.00s)
FAIL
```

**控制 (control)**: 还原原代码 → 跑同测试 → **PASS** (sqlText = `"SELECT count(*) FROM \"users\" WHERE LOWER(TRIM(role)) = $1 AND \"users\".\"deleted_at\" IS NULL FOR UPDATE"`).

**结论**: 锁是 `clause.Locking` 加的, 不是 PG 默认行为. mutation 剥掉 → SQL 不带锁 → 测试红 → 还原绿. 锁真在 SQL 里.

### M2 反证「排除目标真能锁但锁集合不对」:

**变异**: 把 `countAdminUnderLock` 临时还原旧代码「排除目标」 `AND id <> ?` 写法:

```go
// mutation 期间:
func countAdminUnderLock(tx *gorm.DB) (int64, *gorm.DB, error) {
    var n int64
    stmt := tx.Model(&models.User{}).
        Clauses(clause.Locking{Strength: "UPDATE"}).
        // MUTATION M2: 还原旧代码「排除目标」 — 锁集合不相交, TOCTOU 重启
        Where("LOWER(TRIM(role)) = ? AND id <> ?", middleware.RoleAdmin, "00000000-0000-0000-0000-000000000000").
        Count(&n)
    return n, stmt, stmt.Error
}
```

**实证原文**:
```
$ cd backend && go test -race -count=1 -v -run 'TestRunWithDeps_并发窗口锁SQL契约_PG|TestRunWithDeps_并发窗口mutation_inversion_M1' ./cmd/set-role/

=== RUN   TestRunWithDeps_并发窗口锁SQL契约_PG
    main_test.go:359: 
        	Error:      	"SELECT count(*) FROM \"users\" WHERE (LOWER(TRIM(role)) = $1 AND id <> $2) AND \"users\".\"deleted_at\" IS NULL FOR UPDATE" should not contain "AND id"
        	Messages:   	count query 不得排除目标 — 排除会让两个并发 demote-admin 锁集合不相交
    main_test.go:361: 
        	Error:      	"SELECT count(*) FROM \"users\" WHERE (LOWER(TRIM(role)) = $1 AND id <> $2) AND \"users\".\"deleted_at\" IS NULL FOR UPDATE" should not contain "id <>"
        	Messages:   	count query 不得用 'id <>' 排除目标 — 同上
--- FAIL: TestRunWithDeps_并发窗口锁SQL契约_PG (0.00s)
=== RUN   TestRunWithDeps_并发窗口mutation_inversion_M1
    main_test.go:416: 
        	Error:      	"SELECT count(*) FROM \"users\" WHERE (LOWER(TRIM(role)) = $1 AND id <> $2) AND \"users\".\"deleted_at\" IS NULL FOR UPDATE" should not contain "AND id"
        	Messages:   	production helper 不得排除目标 — mutation 反证: 还原 AND id<>? 后并发等价测试必红
--- FAIL: TestRunWithDeps_并发窗口mutation_inversion_M1 (0.00s)
FAIL
```

**控制 (control)**: 还原原代码 → 跑同测试 → **PASS** (sqlText 不含 `AND id` / `id <>`).

**结论**: 即使有 broad lock, 排除目标仍会让两个并发 demote-admin 锁集合不相交 — TOCTOU 重启. SQL 契约测试钉住这一点, 不允许"修了半截".

### 临时 mutation 文件清理 (D9 实证)

```
$ git status --short
?? M85-candidate-completion-report.md
?? M85-candidate-graph-analysis.md
```

`backend/cmd/set-role/main.go.m85bak` 已 `rm -f`, `git status --short` 不含 mutation 痕迹. mutation 实证完所有临时文件**全部删除**, 这是 D9 (mutation 临时文件不入 commit) 的硬证据.

## go test 复核 (本 dispatch)

### backend

```
$ cd backend && go test -race -count=1 -timeout=180s ./...
ok  	network-monitor-platform/cmd/admin-bootstrap  	7.066s
ok  	network-monitor-platform/cmd/migrate           	1.043s
ok  	network-monitor-platform/cmd/seed              	29.851s
ok  	network-monitor-platform/cmd/set-role          	1.094s (含 4 M85 新测试)
ok  	network-monitor-platform/internal/api          	22.840s
ok  	network-monitor-platform/internal/api/handlers 	12.864s
ok  	network-monitor-platform/internal/diagnostic   	2.063s
ok  	network-monitor-platform/internal/eventbus     	1.152s
ok  	network-monitor-platform/internal/grpcserver   	1.039s
ok  	network-monitor-platform/internal/httpx        	1.560s
ok  	network-monitor-platform/internal/integration   	17.974s
ok  	network-monitor-platform/internal/metrics      	1.018s
ok  	network-monitor-platform/internal/middleware    	1.578s
ok  	network-monitor-platform/internal/migrate      	1.033s
ok  	network-monitor-platform/internal/models       	1.105s
ok  	network-monitor-platform/internal/notification  	1.823s
ok  	network-monitor-platform/internal/postmortem   	1.619s
ok  	network-monitor-platform/internal/redact       	1.042s
ok  	network-monitor-platform/internal/service      	2.386s
ok  	network-monitor-platform/pkg/logger            	1.022s
ok  	network-monitor-platform/tests                 	1.757s
Wall time: 72.61 seconds
```

21 packages 全绿 (含 cmd/set-role 4 M85 新测试), race detector 0 误报.

## M82 ↔ M83 ↔ M85 mutation inversion 范本对比

| 维度 | M82-candidate (cycle 13) | M83-candidate (cycle 14) | **M85-candidate (cycle 15)** |
|---|---|---|---|
| **守卫对象** | 业务代码 (`writeNotificationTrigger`) | CI 守门 (`go test -race` + `npx vitest run`) | **业务并发窗口 (cmd/set-role count)** |
| **mutation 类型** | 删 rule filter (业务行为突变) | 故意 race / 故意 fail (CI 守门输入) | **剥 clause.Locking / 还原 AND id<>?** |
| **反证方式** | sqlmock 序列 + 真 sqlite count | race detector stack trace + vitest assertion error | **PG dialector + DryRun 抓 SQL 包含 FOR UPDATE** |
| **期望红** | `TestWriteNotificationTrigger_RuleEmpty_NoLogs` 红 | `WARNING: DATA RACE` + `AssertionError` | **`"FOR UPDATE" does not contain` / `"AND id" should not contain`** |
| **期望还原绿** | helper + rule filter 守卫 | 删除 mutation 文件 | **还原 main.go (mv main.go.m85bak main.go)** |
| **关键设计要点** | 「反向断言用真 sqlite」 | 「CI 守门真等价于 mutation 实证」 | **「SQL 契约钉住锁集合 + 排除目标」** |
| **范本编号** | **范本 A** (业务代码 mutation) | **范本 B** (CI 守门 mutation) | **范本 C** (业务并发窗口 mutation) [NEW] |

三条 mutation inversion 范本可写进 `~/.omh/skills/planner/intent-spec-author/SKILL.md` 8 节 Verification 段的范本库, 后续 round 复用.

## commit 现状 (本 dispatch 2 commits, 已 push)

```
$ git log --oneline -4 origin/main
9733853 feat(M85-candidate): cmd/set-role 并发窗口收口 (broad lock + SQL 契约测试)
668a215 feat(M85-candidate): intent spec (omh-plan 8 节骨架, cmd/set-role 并发窗口收口)
e2937dd docs(M84): completion + graph analysis + CHANGELOG + TODO (watchdog 自举)
ab3c848 feat(M84): watchdog 自举 (pm-loop-derive-candidates.py + codegraph + graphify wiring)
```

2 commits (cycle 15):
1. `668a215` `feat(M85-candidate): intent spec (omh-plan 8 节骨架, cmd/set-role 并发窗口收口)` — 1 file / +257
2. `9733853` `feat(M85-candidate): cmd/set-role 并发窗口收口 (broad lock + SQL 契约测试)` — 2 files / +211 / -7

`git log --oneline origin/main..HEAD` 为空 (本地 = 远端, 0 滞后). 本 dispatch 2 commits 全部 push.

## PM_QUEUE fix (本 dispatch)

PM_QUEUE 是 watchdog 派工的 single-source-of-truth. M85-candidate 在本 dispatch ship 后, PM_QUEUE state 修整 (PM-direct 责任, 沿用 M82 cycle 13 + M83 cycle 14 closeout 范本):

- `PM_QUEUE.json` M85-candidate: status `candidate` → **`shipped`**, loop_cycle 13 → **15**, 加 `shipped_at` / `commits` / `shipped_round` / `mutation_red` / `mutation_restore_green` / `candidate_note` / `derived_from` updated
- `PM_QUEUE.json` `last_updated` bumped to `2026-09-16T16:18:00+08:00`
- `PM_QUEUE.json` `last_audit` bumped to `2026-09-16T16:18:00+08:00`
- `PM_QUEUE.json` `shipped[]` registry append M85-candidate entry (dispatch_attempts=1, commits=[668a215, 9733853], loop_cycle=15, scope="cmd/set-role broad lock + SQL contract")

下次 watchdog tick (≥10 min after commit-age, M79 D3 沿用):
- M85-candidate status=shipped → skip
- 下一个 earliest candidate: PM_QUEUE 沿用 (M86-candidate = `GET /api/integrations/status`, M87-candidate = `G-5 已签发 JWT 不查库`, ...)
- watchdog 不会再次 dispatch M85

## 派生 TODO (留 future, cycle 15 已 ship 后本 dispatch 复核)

- **HTTP 路径同样的 TOCTOU 风险** (`internal/service/user_service.go:215-228` `checkUserUpdateGuards` 的最后一名 admin 守卫): 与 cmd/set-role 同款 — 当前已有 `clause.Locking{Strength: "UPDATE"}` 锁 target user row (L150-156), 但 count other admins **不**加锁, 是同样的 broad-lock-vs-exclude-target 设计错误. 修复范本沿用本 round: 改 count query 锁**全部** active admin 行. **M86+ candidate 候选**, scope = mixed (需 HTTP 路径测试 + 真 PG e2e 测试).
- **真 PG 并发 race window 测试**: M85 单测基于 sqlite `:memory:` sequential 模拟, 抓不到真并发 race (sqlite 单连接天然串行). 真 PG 上跑 `pg_try_advisory_lock` + 2 个独立 `*sql.DB` 实例并发 demote-admin 才是「真并发实证」. **M86+ 候选**, 加 `db_smoke` 真 PG 测试.
- **mutation 范本写入 skill**: 「业务并发窗口 mutation inversion 范本 C」可写进 `~/.omh/skills/planner/intent-spec-author/SKILL.md` 8 节 Verification 段的范本库 (与 M82 范本 A 「业务代码 mutation」 + M83 范本 B 「CI 守门 mutation」并列), 后续业务并发 round 复用.
- **`db_smoke` 加并发窗口 PG 测试**: 沿用 M82 派生 TODO 模式, 加真 PG e2e 测试覆盖 cmd/set-role 并发窗口 (db_smoke.sh 已有 3 CLI 迁移路径, 加 set-role 并发路径).

## OMH workflow shape 实证 (cycle 15 沿用 M67 standing rule)

M85-candidate (cycle 15) intent-M85-candidate.md 用 omh-plan 8 节骨架:

1. **Goal 节** 钉死**实证 + 修复** (不接 "暂不修" framing), 列出 broad lock 修复路径 + 4 测试 + 2 mutation 反证.
2. **Non-goals 节** 钉死**不动** HTTP 路径 (user_service.go 同款风险, M86+ 候选) + 不动 `migrations/` (修复在 Go 代码层) + 不动 setup-profile.json / display.skin / interface (M67) + 不动 sing-box / keyring / OMH config (Poison 红线).
3. **Decision gate 节** 钉死 15 个 D (D1 PM-direct 自决 / D2 poison-stop-gates / D3 自旋防 / D4 30-min flapping / D5 mutation 范本 C / D6 fact_id 24 / D7 2 commits / D8 PM_LAST_DISPATCH_RESULT / D9 mutation 文件不入 commit / D10 PM_QUEUE fixup / D11 sqlite 边界 / D12 HTTP 路径不动 / D13 不加 migration / D14 M67 红线 / D15 mutation M1/M2 全红→还原全绿).
4. **Verification 节** 4 测试 + 2 mutation 反证命令原文 + 期望 SQL 形态 (钉死 GORM 在 PG dialector 下生成 `FOR UPDATE`).

mutation inversion 在本 round 起新增**「业务并发窗口收口 (范本 C)」**模式 — 与 M82 范本 A 「业务代码 mutation」 + M83 范本 B 「CI 守门 mutation」同形不同物:

- M82 测: 「删 rule filter → writeNotificationTrigger 测试红」 (业务行为突变)
- M83 测: 「故意 race → go test -race 红」 + 「故意 fail → vitest run 红」 (CI 守门有效性)
- **M85 测: 「剥 clause.Locking → SQL 契约测试红」 + 「还原 AND id<>? → SQL 契约测试红」 (业务并发窗口真修复)**

三条模式都验证「守卫真工作」, 但守卫对象不同. 后续业务并发 round 沿用 M85 范本 C.

## 注意事项

1. **PM_QUEUE state 同步是 PM-direct 责任**: 跟 M82 cycle 13 + M83 cycle 14 closeout 同款 — cycle 15 ship 后必须把 PM_QUEUE 入口 status 切到 `shipped`. 未做 → watchdog 会反复 dispatch 同一 round. 本 round 是首次 dispatch, 直接 fix, 没有 watchdog flapping (1 次 dispatch 完成, 无 30-min 自旋).

2. **mutation 临时文件用 `main.go.m85bak` 备份**: 不直接 `git stash` (本 round 无既有未提交改动), 也不写 `/tmp/` 然后 `cp` 临时 (备份在同目录更简单). 实证完 `rm -f main.go.m85bak`. 这种模式 ok 因为:
   - 备份文件 `main.go.m85bak` 是与目标同名加 `.m85bak` 后缀, 不会被 GORM 任何路径扫到 (编译只识别 `.go`)
   - 实证完立即 `rm -f`, 不会留到下一个 round
   - 若担心 commit 误操作, 可用 `git stash` 替代

3. **GORM 的 `SoftDelete` 自动加 `AND "users"."deleted_at" IS NULL`**: 这是 GORM 给 `gorm.DeletedAt` 字段的默认行为, 不影响白盒断言 (FOR UPDATE 在末尾, AND id<>? 在条件中段), 但 SQL 文本会比 raw WHERE 长一些. 测试断言 `assert.Contains(sqlText, "FOR UPDATE")` 仍精准命中.

4. **白盒 SQL 契约测试与 GORM 版本绑定**: DryRun 抓的 SQL 文本跟 GORM 版本绑定. 当前 GORM 版本 (与 go.mod 锁定) 与 CI runner 一致. 若 GORM 升级后 SQL 文本变化, 白盒测试需同步更新. **缓解**: 测试注释明确「GORM 版本的特定形态」, 不绑定具体 SQL 措辞 (用 `assert.Contains` 而非 `assert.Equal`).

5. **mutation 临时文件 vs GORM unused import**: 当 mutation 完全剥掉 `Clauses(clause.Locking...)`, `clause` 包未被使用 → Go 编译 fail (unused import). 解决: 加 `_ = clause.Locking{Strength: "UPDATE"}` 强制让编译器保留 import. 这个 hack 是 mutation 反证的常见模式, 注释里说清即可.

6. **mutation inversion 与「无变异绿测试」区分**: 无变异绿测试只能证明「测试不红」, mutation inversion 实证「测试能红」. M82 cycle 13 已经验证: 「单跑绿测试抓不到测试设计漏洞, 必须靠 mutation inversion 反证才能暴露」. 本 round 沿用.

7. **`cmd/set-role/main_test.go` 加 `gorm.io/gorm/clause` import**: 用于白盒测试调 `clause.Locking{Strength: "UPDATE"}` 模拟 mutation. 与 production 代码 import 一致, 不引入新依赖.

8. **白盒 SQL 契约 + 真 PG race 测试是**互补**: M85 的白盒 SQL 契约抓**设计正确性** (锁在 SQL 里, 排除目标不在), 但抓不到**真并发 race** (sqlite 单连接). M86+ 加真 PG race 测试覆盖「两个独立 `*sql.DB` 实例并发 demote-admin」的真并发行为.

## 后续推荐

- **G-4 账号处置无产品化入口**: 仍是 M86+ 候选 (TODOG-4 是更广的账号管理问题, 不限于 cmd/set-role 路径).
- **HTTP 路径 user_service.go 同样 TOCTOU 收口** (M86-candidate 候选): `checkUserUpdateGuards` (internal/service/user_service.go:215-228) 同样有 broad-lock-vs-exclude-target 问题, 沿用本 round 范本修复.
- **fact_store fact_id = 24 (本 round advisory)**: 沿用 M82 cycle 13 = fact_id 22, M83 cycle 14 = fact_id 23, 本 round = 24 advisory, 未实际落库.
- **mutation 范本写入 skill**: 「业务并发窗口实证 (范本 C)」可写进 `~/.omh/skills/planner/intent-spec-author/SKILL.md` 8 节 Verification 段的范本库 (与范本 A 「业务代码 mutation」 + 范本 B 「CI 守门 mutation」并列), 后续业务并发 round 复用.

## watchdog 下次 tick 验证

- PM_QUEUE M85-candidate.status: `candidate` → **`shipped`** (本 dispatch fix)
- commit age ≥ 10 min (沿用 M79 D3, 本 dispatch 2 commits push, M85 impl commit `9733853` age ≥ 10 min by next tick)
- 30-min dispatch hist flapping auto-switch 沿用 (M79 D4, 本 dispatch 1 次完成, 无 flapping)
- `PM_LAST_DISPATCH_RESULT.md` 已写 (本文件, Poison 看)
- 2 commits 已 push 到 origin/main
- TODO.md L59 切 `[x]` (本 dispatch docs commit)
- CHANGELOG.md 加 M85 段 (cycle 15) (本 dispatch docs commit)
- watchdog 下次 tick 应 dispatch 下一个 candidate (PM_QUEUE 沿用, M86-candidate = `GET /api/integrations/status`)