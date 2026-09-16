# M85-candidate — `cmd/set-role` 并发窗口收口 (SELECT FOR UPDATE 串行化, OMH ulw-loop 第 15 cycle)

> **Loop cycle**: 15 of `itmanager-grit-2026q3`
> **Loop mode**: B (watchdog Mode B auto-dispatched M85-candidate after M84 cycle 13 ship, 10-min commit-age gate 沿用 M79 D3)
> **Dispatch origin**: `~/.hermes/scripts/pm-loop-watchdog.sh` Mode B at 2026-09-16T16:06:58+08:00 (PM_QUEUE M85-candidate = `cmd/set-role` 并发窗口, derived from TODO.md L59 by `pm-loop-derive-candidates.py` M84 ship)
> **Scope**: `backend/cmd/set-role/` + 4 new tests, ≤3h estimated (auto-derived)
> **Prerequisite**: M83-candidate (`35dc9d6`, 2026-09-16) CI 实证闭环 ship; M41 (`182f621`) + B1-3 (`3725f40`) CI 在位; user_service.applyUserUpdate 已用 `clause.Locking{Strength: "UPDATE"}` 范本 (`internal/service/user_service.go:150-156`)

## Goal

PM_QUEUE M85-candidate = **"`cmd/set-role` 并发窗口"** 的实证 + 修复:

- **诊断**: TODO.md L59 钉死「防自锁检查与写入已同事务，但两个并发进程仍可能各自通过（TOCTOU）；该命令是人工运维操作，暂不修」
- **核心缺陷**: `cmd/set-role/main.go:84-105` 的事务**只读目标用户 row 一次**（事务外 L73），事务内 count "其他 admin" (`WHERE role='admin' AND id<>?`) **不加锁**。两个并发进程:
  - Tx A (demote A): 看到 B 是 admin → count others=1 → 通过 → 改 A
  - Tx B (demote B): 看到 A 是 admin → count others=1 → 通过 → 改 B
  - 结果: 0 admin, 系统自锁, 只能 SQL 直连救
- **关键设计要点**: 「排除目标」`AND id<>?` 让两个并发 demote 不同 admin 的事务**锁不到共同行** — 这是「读-判-写」TOCTOU 的典型反模式
- **目标交付**:
  1. **修复 TOCTOU**: 把 count query 改为 `WHERE role='admin'` (锁**所有** admin 行, 不排除目标), 加 `clause.Locking{Strength: "UPDATE"}` → 两个并发 demote-admin 事务在 PG 上**串行**, 第二个事务读到 commit 后状态, count 才能反映「剩余 admin 数」
  2. **白盒 SQL 契约测试** (新增): 用 GORM `Session{DryRun: true}` 抓生成 SQL, 断言包含 `FOR UPDATE` 子句 (沿用 `user_service_test.go:421-447` `TestUserService_Update_持锁读旧值` 范本, 但改用 sqlite `:memory:` + DryRun 而不是 sqlmock, 与 cmd/set-role 既有测试基座一致)
  3. **mutation inversion 实证** (范本 C — 业务并发窗口): 写一个并发 goroutine 测试 + 一个故意剥离 `clause.Locking` 的 mutation 测试, 实证「无锁时 SQL 不带 FOR UPDATE, 有锁时 SQL 带 FOR UPDATE」
  4. **既有契约测试不退化**: 13 个现有 set-role 测试 (`main_test.go`) 全绿
  5. **TODO.md L59 切 `[x]`** + CHANGELOG M85 段 + `M85-candidate-completion-report.md` + `M85-candidate-graph-analysis.md`

## Non-goals

- **不动** `cmd/set-role` 的事务边界 (`Transaction(func(tx *gorm.DB) error { ... })` 沿用, 不改成嵌套事务或显式 BEGIN/COMMIT)
- **不动** `user_service.applyUserUpdate` (`internal/service/user_service.go:144-178`) 的 FOR UPDATE 范本 — 它已用, 沿用为参考
- **不动** HTTP 路径 (`PUT /users/:id` 等) — 同样的 TOCTOU 风险在 `user_service.go` 也存在, 但**不在本 round scope** (M86+ candidate 候选, 沿用 M83 派生 TODO 模式)
- **不动** `migrations/` — 不加新迁移 (M85 fix 是 Go 代码层, 不需要 schema 变更; 与 M82 同款, 业务守卫放在代码层而非 DB 层)
- **不动** `setup-profile.json` / `display.skin` / `interface` (M67 standing rule)
- **不动** `sing-box` / `keyring` / `OMH config` (Poison 红线)
- **不动** `go.mod` / `package.json` (本 round 不改依赖)
- **不动** `internal/middleware/roles.go` 词表 / 能力矩阵 (词表未变, 只是 cmd/set-role 加锁)
- **不**新增 `db_smoke.sh` 真 PG 测试 (与 M82 同款: 真 PG 测试是 M86+ 候选, M85 单测基座沿用 sqlite `:memory:` + DryRun)
- **不**用 advisory lock 替代 SELECT FOR UPDATE (advisory lock 引入新概念, 范本不在本 round; SELECT FOR UPDATE 沿用 user_service.go 已 ship 范本, 更稳)
- **不**重构 syncUserRoles (L116-138) — 它不在并发窗口的 TOCTOU 路径上
- **不** mutation inversion 用「真 PG 并发 race detector 抓取」(M85 没有 race detector 路径 — race window 是「count-then-write」, 不是「共享变量无锁」; M83 范本 B 的 race detector 范本不适用)

## Assumptions

- ITmanager repo HEAD = `e2937dd` (M84 cycle 13 ship — watchdog 自举), working tree clean, branch `main` up-to-date with `origin/main`
- `backend/cmd/set-role/main.go` 当前实现 (L73 事务外读 user, L84-98 事务内 count 不加锁) 是已知缺陷面
- `backend/internal/service/user_service.go:144-178` 已 ship 的 SELECT FOR UPDATE 范本 (`tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&target, "id = ?", id)`) 是 M85 修复的对齐基线
- 临时 mutation 测试文件用 `m85_` 前缀 (与 M83 同款, 不撞既有测试命名)
- Go 1.25.14 (本地) 与 CI runner Go 1.25 (跟 go.mod) 一致, GORM 行为对齐
- sqlite `:memory:` 单连接, 不渲染 `FOR UPDATE` (driver 明说不支持行级锁, 沿用 user_service.go:141 注释) — 这是 M85 的**已知边界**: 白盒 DryRun SQL 断言**只在 PG dialector 下抓得到 `FOR UPDATE`**; sqlite 下 SQL 是 `WHERE role='admin'` 不带 lock 子句, 测试需切 dialector
- M85 接受这个边界: 白盒测试用 `gorm.Dialector` 切换到 `postgres` driver, 在 DryRun 模式下生成预期 SQL; 这是 GORM `Session{DryRun: true}` 的标准做法, 与 `user_service_test.go:437` 的 sqlmock ExpectQuery regex 模式本质相同 (都验 SQL 文本)
- fact_store fact_id = 24 advisory (沿用 M82 cycle 13 = 22, M83 cycle 14 = 23, 本 round = 24, 未实际落库)
- watchdog tick 时段: M85-candidate 由 Mode B 自动 dispatch, commit-age ≥ 10 min 才起下一 round (M79 D3 沿用)
- `~/.hermes/state/PM_LAST_DISPATCH_RESULT.md` 写 M85 closeout (Poison 看 + watchdog 下次 tick 验证)

## Acceptance criteria

| 标准 | 实证 |
|---|---|
| `intent-M85-candidate.md` 8 节 omh-plan 骨架 (Goal / Non-goals / Assumptions / Acceptance / Verification / Risks / Plan / Decision gate) | 本文件 (commit 1) |
| `backend/cmd/set-role/main.go` count query 加 `clause.Locking{Strength: "UPDATE"}` + 移除 `AND id<>?` 排除目标 | verify (grep) |
| `backend/cmd/set-role/main.go` 注释明确说明 "锁全部 admin 行, 让并发 demote-admin 事务串行" | verify |
| `backend/cmd/set-role/main_test.go` 加 `TestRunWithDeps_并发窗口锁SQL契约`: 切换 PG dialector + DryRun 抓 SQL, 断言包含 `FOR UPDATE` + 不含 `AND id` | verify |
| `backend/cmd/set-role/main_test.go` 加 `TestRunWithDeps_并发窗口锁SQL契约_sqlite对照`: 在 sqlite dialector 下 DryRun, 断言**不**包含 `FOR UPDATE` (证 sqlite 不渲染锁, 接受 M85 已知边界) | verify |
| `backend/cmd/set-role/main_test.go` 加 `TestRunWithDeps_并发窗口mutation_inversion`: 临时剥离 `clause.Locking` 的 mutation 文件, 跑白盒测试 → **期望红** (mutation 文件 SQL 不带 FOR UPDATE) → 还原 mutation → 期望绿 | verify |
| `backend/cmd/set-role/main_test.go` 加 `TestRunWithDeps_并发窗口_GORM并发等价`: 模拟并发场景的 Go-level 模拟 — 用 2 个事务 (tx A + tx B) 各自 count admin, 断言在「broad lock (新代码)」下第二个事务 count 看到第一个事务的写入 (PG 串行语义) — 在 sqlite 下用 sequential simulation (因为 sqlite 是单连接) | verify |
| **mutation inversion 实证 — TOCTOU 窗口真存在 vs 真修复** | see Verification §3 |
| &nbsp;&nbsp; M1: 临时还原旧代码 (count 无锁, `WHERE id<>?`) → 白盒 SQL 契约测试**红** → 还原新代码 → 绿 | verify |
| &nbsp;&nbsp; M2: 临时还原 `AND id<>?` (broad lock 还在但排除目标) → 并发等价测试**红** (因为锁集合不相交) → 还原 → 绿 | verify |
| mutation 临时文件实证完**全部删除** (不入 commit) | verify (git status) |
| 13 个既有 set-role 测试全绿 | verify (`go test ./cmd/set-role/`) |
| `cd backend && go test -race -count=1 ./cmd/set-role/...` 全绿 | verify |
| `cd backend && go test -race -count=1 -timeout=180s ./...` 全绿 (28 packages, 0 退化) | verify |
| `M85-candidate-completion-report.md` + `M85-candidate-graph-analysis.md` 写完 | docs commit 2 |
| `CHANGELOG.md` M85 段加「并发窗口收口」条目 | docs commit 2 |
| `TODO.md` L59 `- [ ]` → `- [x]`, 标注「已 ship M85-candidate (broad lock + SQL 契约 + 并发等价测试)」 | docs commit 2 |
| git log 2 commits, 全部 push 到 origin/main | verify |
| `~/.hermes/state/PM_QUEUE.json` M85-candidate.status: `candidate` → **`shipped`** | state fixup |
| `~/.hermes/state/PM_LAST_DISPATCH_RESULT.md` 写完 | verify |

## Verification

### 1. cmd/set-role 不退化 (grep verify)

```bash
$ grep -n "clause.Locking\|FOR UPDATE\|id <>\|id<>" backend/cmd/set-role/main.go
89:                Clauses(clause.Locking{Strength: "UPDATE"}).
91:                Where("LOWER(TRIM(role)) = ?", middleware.RoleAdmin).
```

任一 FAIL = 修复未应用, 红测试。

### 2. 既有契约测试 (Green: 13 既有测试 + 4 新测试)

**既有**:
```bash
$ cd backend && go test -race -count=1 ./cmd/set-role/
ok  	network-monitor-platform/cmd/set-role  [13 PASS, 4 new PASS, 0 FAIL]
```

**全 backend 不退化**:
```bash
$ cd backend && go test -race -count=1 -timeout=180s ./...
ok  	network-monitor-platform/cmd/admin-bootstrap  	~7s
ok  	network-monitor-platform/cmd/migrate           	~1s
ok  	network-monitor-platform/cmd/seed              	~30s
ok  	network-monitor-platform/cmd/set-role          	~2s (含 4 M85 新测试)
ok  	network-monitor-platform/internal/api          	~23s
... (28 packages, all ok, 0 FAIL)
```

### 3. mutation inversion 实证 (范本 C — 业务并发窗口)

**M1 反证「无锁真无锁」**:

```bash
# 临时还原旧代码 (count 不加锁, 排除目标)
$ sed -i.bak 's|Clauses(clause.Locking{Strength: "UPDATE"}).|Model(&models.User{}).|' backend/cmd/set-role/main.go
$ sed -i.bak 's|Where("LOWER(TRIM(role)) = ?", middleware.RoleAdmin).|Where("LOWER(TRIM(role)) = ? AND id <> ?", middleware.RoleAdmin, user.ID).|' backend/cmd/set-role/main.go

$ cd backend && go test -run TestRunWithDeps_并发窗口锁SQL契约 ./cmd/set-role/
--- FAIL: TestRunWithDeps_并发窗口锁SQL契约
    Error: SQL "SELECT count(*) FROM "users" WHERE LOWER(TRIM(role)) = $1 AND id <> $2" 应包含 "FOR UPDATE" 守卫
FAIL
exit 1
```

**还原**:
```bash
$ mv backend/cmd/set-role/main.go.bak backend/cmd/set-role/main.go
$ cd backend && go test -run TestRunWithDeps_并发窗口锁SQL契约 ./cmd/set-role/
ok  	network-monitor-platform/cmd/set-role  [1 PASS]
```

**M2 反证「排除目标真不能锁」**:

```bash
# 临时还原 broad lock 还在但 AND id<>? 排除目标
$ sed -i.bak 's|Where("LOWER(TRIM(role)) = ?", middleware.RoleAdmin).|Where("LOWER(TRIM(role)) = ? AND id <> ?", middleware.RoleAdmin, user.ID).|' backend/cmd/set-role/main.go

$ cd backend && go test -run TestRunWithDeps_并发窗口_GORM并发等价 ./cmd/set-role/
--- FAIL: TestRunWithDeps_并发窗口_GORM并发等价
    Error: 模拟并发事务: tx A count others 看到 1, tx B count others 也看到 1 → 0 admin 风险 (排除目标锁集合不相交)
FAIL
exit 1
```

**还原**:
```bash
$ mv backend/cmd/set-role/main.go.bak backend/cmd/set-role/main.go
$ cd backend && go test -run TestRunWithDeps_并发窗口_GORM并发等价 ./cmd/set-role/
ok  	network-monitor-platform/cmd/set-role  [1 PASS]
```

**关键设计要点**:
- **白盒 SQL 契约** (M85 mutation 范本 C 的核心): 切到 PG dialector + DryRun, 抓生成 SQL, 断言 `FOR UPDATE` 子句存在。这是与 sqlmock `ExpectQuery regex` (user_service_test.go:437) **同形不同物**: sqlmock 验「实际发出的 SQL 包含」, DryRun 验「GORM 在 PG dialector 下会生成」 — 都验证「锁真在 SQL 里」
- **并发等价测试** (Go-level 模拟): 用 sqlite 模拟「两个 sequential 事务」— sqlite 单连接天然串行, 但语义等价于 PG 上的两个并发事务被 broad lock 串行化。这捕获了「排除目标让锁集合不对」的设计错误, 是 mutation 反证 M2 的关键
- 两个 mutation 文件实证完**全部 rm**, `git status` clean 才算闭环
- 这是 cmd/set-role 独有的 mutation 范本 C: 不测业务代码 (M82 范本 A), 不测 CI 守门 (M83 范本 B), 测**业务并发窗口**的真修复

### 4. TODO.md L59 切 [x]

```bash
$ git diff TODO.md | grep "cmd/set-role.*并发"
-- [ ] **`cmd/set-role` 并发窗口** — 防自锁检查与写入已同事务，但两个并发进程仍可能各自通过（TOCTOU）；该命令是人工运维操作，暂不修
++ [x] **`cmd/set-role` 并发窗口** — 已 ship M85-candidate (broad lock `WHERE role='admin'` + `clause.Locking{Strength:"UPDATE"}` + SQL 契约测试 + 并发等价测试), 见 `M85-candidate-completion-report.md`
```

### 5. CHANGELOG M85 段

```markdown
- **M85-candidate `cmd/set-role` 并发窗口收口** (backend + tests + docs, ≤3h)
  — TODO.md L59 "`cmd/set-role` 并发窗口 — 防自锁检查与写入已同事务，但两个并发进程仍可能各自通过（TOCTOU）" 的实证 + 修复.
  修复路径: count query 改 `WHERE LOWER(TRIM(role)) = ?` (锁全部 admin 行, 不排除目标) + 加 `clause.Locking{Strength: "UPDATE"}`,
  让两个并发 demote-admin 事务在 PG 上**串行** — 第二个事务读到第一个 commit 后状态, count 才反映「剩余 admin 数」.
  范本沿用 `internal/service/user_service.go:144-178` 既有 SELECT FOR UPDATE 范本.
  新增 4 测试: 白盒 SQL 契约 (PG dialector + DryRun 抓 SQL 包含 `FOR UPDATE` + sqlite 对照不渲染 FOR UPDATE) + mutation inversion M1/M2 反证.
  见 `M85-candidate-completion-report.md`.
```

## Risks

- **sqlite 不渲染 FOR UPDATE**: `cmd/set-role` 测试基座是 sqlite `:memory:`, driver 不支持行级锁 (沿用 user_service.go:141 注释). M85 接受这个边界: 白盒测试用 PG dialector + DryRun, 真实 PG 上跑同代码即生效. **缓解**: 文档明确「白盒测试在 sqlite 下跳过 SQL 契约断言, 只跑 mutation 反证」, 抓得到 PG SQL 是 1× DryRun 调用, 0 性能影响
- **broad lock 误伤非并发路径**: 「锁全部 admin 行」会比旧代码锁更多行. 单事务场景下锁代价 = 锁 N 行 (N=admin 数, 一般 < 5) → 可忽略
- **mutation M1/M2 临时文件误 commit**: 与 M83 同款 — 用 `main.go.bak` 隔离 (sed 备份原文件), 实证完 `mv` 还原, `git status` 二次确认 clean
- **mutation 反证在 sqlite 下不直接抓 race window**: sqlite 单连接天然串行, 真实并发 race 抓不到. M85 的 mutation 反证是**白盒 SQL 契约** (锁在 SQL 里), 不是黑盒 race 检测 (与 M83 范本 B 不同). **关键设计**: 反证的是「设计正确性」 (锁集合覆盖两个并发 demote 的共同行), 不是「真并发执行」
- **PM_QUEUE state 同步漏**: 沿用 M82 cycle 13 + M83 cycle 14 closeout 模式, 必须把 status 切 `shipped` + append `shipped[]` registry, 否则 watchdog 会反复 dispatch M85
- **白盒 SQL 断言可能在 GORM 升级后漂移**: DryRun 抓的 SQL 跟 GORM 版本绑定. 当前 GORM 版本 (与 go.mod 锁定) 与 CI runner 一致. 若 GORM 升级后 SQL 文本变化, 白盒测试需同步更新. **缓解**: 测试注释明确 GORM 版本 + DryRun 的本质是「验证 clause 真的进了 SQL」, 不绑定具体 SQL 措辞
- **「GORM 并发等价」在 sqlite 下的模拟是 sequential**: 不是真并发, 是 sequential 模拟「两个事务先后看 count」 — 抓得到设计错误 (锁集合不对), 抓不到 race timing. **缓解**: 文档明确说明这是「白盒逻辑等价」测试, 不替代真 PG 并发测试 (M86+ 候选)
- **Telegram CLI 不可用**: 沿用 M79, watchdog 报告写到本地
- **watchdog 自旋防**: M85 由 Mode B 自动 dispatch (16:06:58), 本 round 是 PM-direct 收到派工后**自决**完成, 不走 inbox escalate

## Plan

1. **写 `intent-M85-candidate.md`** (本文件, 8 节 omh-plan 骨架) — **feat commit 1**: `feat(M85-candidate): intent spec (omh-plan 8 节骨架, cmd/set-role 并发窗口收口)`

2. **impl 修复**:
   - `backend/cmd/set-role/main.go:84-98` 改 count query:
     - 移除 `AND id <> ?` (避免锁集合不相交)
     - 加 `Clauses(clause.Locking{Strength: "UPDATE"})` 
     - 加 import `"gorm.io/gorm/clause"` (在 import 块)
   - 新增注释说明: 「锁全部 admin 行, 让并发 demote-admin 事务在 PG 上串行 — 排除目标会让锁集合不相交, 重新打开 TOCTOU」
   - 调整逻辑: `totalAdmins <= 1` 而不是 `otherAdmins == 0` (因为现在 count 是全部 admin, 包含目标)

3. **新增测试** (4 个):
   - `TestRunWithDeps_并发窗口锁SQL契约_PG`: 切 PG dialector, DryRun 抓 count SQL, 断言包含 `FOR UPDATE` + 不含 `AND id`
   - `TestRunWithDeps_并发窗口锁SQL契约_sqlite对照`: sqlite dialector 下 DryRun, 断言不包含 `FOR UPDATE` (证 sqlite 不渲染, 接受边界)
   - `TestRunWithDeps_并发窗口mutation_inversion_M1`: 故意调用**不带** clause.Locking 的 helper, 断言 SQL 不含 FOR UPDATE (mutation 期望红)
   - `TestRunWithDeps_并发窗口_GORM并发等价`: 在 sqlite sequential 模拟两个事务的 count, 断言 broad lock 下第二个事务 count 反映第一个事务 commit 后的状态 (这是 mutation M2 的反向 — 锁集合**对**时的预期行为)

4. **mutation inversion 实证**:
   - **M1**: `sed` 临时还原 `Clauses(clause.Locking...)` + 还原 `AND id <> ?` → 跑白盒 SQL 契约测试 → 期望红 (SQL 不带 FOR UPDATE) → `mv main.go.bak main.go` 还原
   - **M2**: `sed` 临时还原 `AND id <> ?` (broad lock 还在) → 跑并发等价测试 → 期望红 (锁集合不对) → `mv main.go.bak main.go` 还原
   - mutation 临时文件**全部 rm**, `git status` clean 才算闭环

5. **既有用例复核**: `cd backend && go test -race -count=1 ./cmd/set-role/` → 17 个测试全绿 (13 既有 + 4 新); `go test -race -count=1 ./...` → 28 packages 全绿

6. **写 docs**:
   - `M85-candidate-completion-report.md` (TOCTOU 实证 + mutation inversion 反证 + commit 序列 + 派生 TODO)
   - `M85-candidate-graph-analysis.md` (cmd/set-role 节点图 + mutation inversion 调用链 + M82↔M83↔M85 范本对比)
   - `CHANGELOG.md` M85 段加条目 (放在 M83 之后, cycle 15)
   - `TODO.md` L59 `- [ ]` → `- [x]`
   — **docs commit 2**: `docs(M85-candidate): completion + graph analysis + CHANGELOG + TODO (cmd/set-role 并发窗口收口)`

7. **commit + push** 2 commits 到 origin/main

8. **PM_QUEUE state fixup**: M85-candidate.status `candidate` → `shipped`, append `shipped[]` registry, bump `last_updated` / `last_audit`

9. **写 `~/.hermes/state/PM_LAST_DISPATCH_RESULT.md`** (Poison 看 + watchdog 下次 tick 验证)

### Commit 序列

```
e2937dd (HEAD, M84 cycle 13)
   ↓
M85 commit 1: feat(M85-candidate): intent spec (omh-plan 8 节骨架, cmd/set-role 并发窗口收口)
M85 commit 2: feat(M85-candidate): cmd/set-role 并发窗口收口 (broad lock + SQL 契约测试 + 并发等价测试)
M85 commit 3: docs(M85-candidate): completion + graph analysis + CHANGELOG + TODO (cmd/set-role 并发窗口收口)
```

3 commits (沿用 M78/M79/M80/M81/M82/M83 既有 pattern; intent spec 1 + impl 1 + docs 1). mutation inversion 在 commit 2 之前完成, 不入 commit.

## Decision gate

- **D1**: scope = **`cmd/set-role` 并发窗口收口** (broad lock + SQL 契约测试 + 并发等价测试 + docs), 不动 HTTP 路径 (user_service.go 同样的 TOCTOU 风险, M86+ 候选)
- **D2**: 沿用 poison-stop-gates-v1 (PM_LOOP_MODE = "stop" → freeze)
- **D3**: 沿用 watchdog 自旋防 + commit age ≥ 10 min
- **D4**: 沿用 30-min dispatch hist flapping auto-switch (M79 D4)
- **D5**: mutation inversion = **业务并发窗口收口** 模式 (范本 C, NEW), 与 M82 (业务代码 mutation 范本 A) + M83 (CI 守门 mutation 范本 B) 同形不同物 — 都验证「守卫真工作」
- **D6**: 不写新 fact_store entry (沿用 M82 = 22, M83 = 23, 本 round = 24 advisory)
- **D7**: 3 commits (intent + impl + docs, 沿用 M78/M79/M80/M81/M82/M83 pattern)
- **D8**: PM_LAST_DISPATCH_RESULT.md 写到 `~/.hermes/state/`, watchdog 下次 tick 验证
- **D9**: mutation 临时文件**不入 commit** (用 `main.go.bak` 隔离, 实证完 mv 还原 + `git status` 二次确认)
- **D10**: PM_QUEUE M85-candidate.status: `candidate` → **`shipped`**, append `shipped[]` registry (沿用 M82 cycle 13 + M83 cycle 14 closeout 范本)
- **D11**: 接受 sqlite 不渲染 FOR UPDATE 的已知边界 (白盒测试切 PG dialector + DryRun)
- **D12**: 不动 HTTP 路径的 user_service.go (同样的 TOCTOU 风险, M86+ 候选)
- **D13**: 不加新迁移 (修复在 Go 代码层, 不需要 schema 变更; 沿用 M82 范本)
- **D14**: 不动 setup-profile.json / display.skin / interface (M67 standing rule 沿用)
- **D15**: mutation M1/M2 反证必须**全红 → 还原全绿**, 否则不算闭环