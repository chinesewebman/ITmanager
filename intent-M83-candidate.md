# M83-candidate — CI 升级 closeout: 实证 `go test -race` + `npx vitest run` 真在守门 (OMH ulw-loop 第 14 cycle)

> **Loop cycle**: 14 of `itmanager-grit-2026q3`
> **Loop mode**: B (PM-direct dispatch 沿用, watchdog Mode B auto-dispatched M83-candidate after M82-candidate cycle 13 ship, 10-min commit-age gate 沿用 M79 D3)
> **Dispatch origin**: `~/.hermes/scripts/pm-loop-watchdog.sh` Mode B at 2026-09-16T13:51:xx+08:00 (PM_QUEUE M83-candidate = G-CI-1, derived from TODO.md L193 "CI 升级: 加 `go test -race` + frontend vitest 步骤", 未 ship 实证闭环)
> **Scope**: `.github/workflows/ci.yml` + mutation inversion 实证 + docs, ≤2h estimated
> **Prerequisite**: M41 (`182f621`, 2026-09-13) added `go test -race` to CI workflow + B1-3 (`3725f40`, 2026-07-01) added `npx vitest run` to CI frontend job — **两条步骤均已在 `.github/workflows/ci.yml` 实证 present**, 但 TODO.md L193 仍 unchecked, PM_QUEUE 状态未切, mutation inversion 实证未做

## Goal

PM_QUEUE M83-candidate = **"CI 升级: 加 `go test -race` + frontend vitest 步骤"** 的 closeout 实证:
- TODO.md L193 已写明本项 scope, 但 CI workflow 早已在 M41 + B1-3 ship 后**物理存在**这两个步骤
- 本 round 不新增 CI 代码 (无新增 step, 无 flag 调整) — 实证既有步骤**真在工作**
- 沿用 M82-candidate cycle 13 的 closeout pattern: intent spec + mutation inversion 实证 + docs commit + PM_QUEUE state 收尾
- 关键交付:
  1. **mutation inversion 实证**: 写一个故意 race 的临时测试, 验证 `go test -race` 真抓 → 还原绿; 写一个故意失败的临时 vitest, 验证 vitest 真抓 → 还原绿
  2. **CI workflow 不退化**: `grep` 证明 `.github/workflows/ci.yml` L53 `go test -race ./...` + L205 `npx vitest run` 双双在位
  3. **TODO.md L193 切 `[x]`**, 标 `已 ship M41 + B1-3, M83-candidate 实证闭环`
  4. **CHANGELOG M83 段**: 记录「CI 升级实证闭环」, 引用 M41 + B1-3 commit hash

## Non-goals

- **不动** `.github/workflows/ci.yml` L53 / L205 已有步骤 (已 ship M41 + B1-3)
- **不动** setup-profile.json / display.skin / interface (M67 standing rule)
- **不动** sing-box / keyring / OMH config (Poison 红线)
- **不动** go.mod / package.json / package-lock.json (本 round 不改依赖)
- **不**新增 coverage 阈值门禁 (TODO.md L194 = 另一条独立 candidate, M84+)
- **不**改 frontend build / lint / type-check 步骤 (B1-3 已 ship, 不退化)
- **不**新写 backend unit test (M41 ship 时已加 `TestPublish_并发安全`, 沿用)
- **不**新写 frontend vitest (B1-3 ship 时已加首批 case, 沿用 49 个 test 文件)
- **不** bypass poison-stop-gates-v1
- **不**给 OMH 加新功能 / 不写新 systemd timer
- **mutation 临时文件不 commit**: 用 `git stash` / 写 `/tmp/` 不入 repo, 实证完即删

## Assumptions

- ITmanager repo HEAD = `c3fde25` (M82-candidate cycle 13 docs ship), working tree clean, branch `main` up-to-date with `origin/main`
- `.github/workflows/ci.yml` L48-53 `go test -race ./...` (M41 ship 2026-09-13, commit `182f621`) 已在 backend job, 含 race detector 守门 DATA RACE
- `.github/workflows/ci.yml` L203-205 `npx vitest run` (B1-3 ship 2026-07-01, commit `3725f40`) 已在 frontend job, 含 vitest 单测执行
- 临时 mutation 测试文件 `backend/internal/eventbus/m83_race_test.go` + `frontend/src/m83_failing.test.ts` 实证完后即删, **不入 commit**
- Go 1.25.14 (本地) 与 CI runner Go 1.25 (跟 go.mod) 一致, race detector 行为对齐
- Node v26.8.1 (本地) 与 CI runner Node v22 (workflow L174 钉) 略有差异, vitest 1.6.1 在两者均能跑 (本地 `npm test` 验证过)
- fact_store fact_id = 22 advisory (沿用 M82 cycle 13 = 22 序列, 未实际落库)
- watchdog tick 时段: PM-direct dispatch, commit-age ≥ 10 min 才起下一 round (M79 D3 沿用)
- M82-candidate cycle 13 的 PM_LAST_DISPATCH_RESULT.md 已写 (`~/.hermes/state/PM_LAST_DISPATCH_RESULT.md`), 本 round 沿用同样 closeout 范本

## Acceptance criteria

| 标准 | 实证 |
|---|---|
| `intent-M83-candidate.md` 8 节 omh-plan 骨架 (Goal / Non-goals / Assumptions / Acceptance / Verification / Risks / Plan / Decision gate) | 本文件 (commit 1) |
| `.github/workflows/ci.yml` L53 `go test -race ./...` 仍在位 (M41 ship) | verify (grep) |
| `.github/workflows/ci.yml` L205 `npx vitest run` 仍在位 (B1-3 ship) | verify (grep) |
| `cd backend && go test -race -count=1 -timeout=180s ./...` 全绿 (28 packages) | verify |
| `cd frontend && npx vitest run` 全绿 (49 test files, ~338+ tests) | verify |
| **mutation inversion 实证 — CI 步骤真抓 bug** | see Verification §3 |
| &nbsp;&nbsp; M1 race: 写故意 race 临时测试 → `go test -race` 红 → 还原绿 | verify |
| &nbsp;&nbsp; M2 vitest: 写故意 fail 临时测试 → `npx vitest run` 红 → 还原绿 | verify |
| 临时 mutation 测试文件实证完**全部删除** (不入 commit) | verify (git status) |
| `M83-candidate-completion-report.md` + `M83-candidate-graph-analysis.md` 写完 | docs commit 2 |
| `CHANGELOG.md` M83 段加「CI 升级实证闭环」条目 | docs commit 2 |
| `TODO.md` L193 `- [ ]` → `- [x]`, 标注「已 ship M41 + B1-3, M83-candidate 实证闭环」 | docs commit 2 |
| git log 2 commits, 全部 push 到 origin/main | verify |
| `~/.hermes/state/PM_QUEUE.json` M83-candidate.status: `candidate` → **`shipped`** | state fixup |
| `~/.hermes/state/PM_LAST_DISPATCH_RESULT.md` 写完 | verify |

## Verification

### 1. CI workflow 不退化 (grep verify)

```bash
$ grep -n "go test -race\|npx vitest run" .github/workflows/ci.yml
53:        run: go test -race ./...
205:        run: npx vitest run
```

任一 FAIL = M41 / B1-3 ship 的步骤退化, 红 CI。

### 2. 既有契约测试 (Green: 28 packages backend + 49 frontend test files)

**backend (28 packages)**:
```bash
$ cd backend && go test -race -count=1 -timeout=180s ./...
ok  	network-monitor-platform/cmd/admin-bootstrap  	7.078s
ok  	network-monitor-platform/cmd/migrate           	1.066s
ok  	network-monitor-platform/cmd/seed              	29.849s
ok  	network-monitor-platform/cmd/set-role          	1.092s
ok  	network-monitor-platform/internal/api          	22.860s
ok  	network-monitor-platform/internal/api/handlers 	12.876s
... (28 packages, all ok, 0 FAIL)
```

**frontend (49 test files)**:
```bash
$ cd frontend && npx vitest run --reporter=json
{"numTotalTests": N, "numPassedTests": N, "numFailedTests": 0, "numTotalTestFiles": 49, "success": true}
```
(本地 Node v26 vs CI Node v22, vitest 1.6.1 行为对齐, 不引入 Node 版本敏感)

### 3. mutation inversion 实证 (M1 + M2 反证全红 → 还原全绿)

CI 步骤**真在工作**的硬证据:

| # | 变异 | 期望红命令 | 期望还原绿 |
|---|---|---|---|
| **M1** | 在 `backend/internal/eventbus/m83_race_test.go` 写一个故意 race 的测试 (两个 goroutine 同时 `x++` 不带 mutex), 跑 `go test -race` | `go test -race ./internal/eventbus/` **红** (race detector 报 `WARNING: DATA RACE`) | 删 `m83_race_test.go` → **绿** ✓ |
| **M2** | 在 `frontend/src/m83_failing.test.ts` 写一个 `expect(1).toBe(2)` 的故意 fail 测试, 跑 `npx vitest run` | `npx vitest run` **红** (`FAIL src/m83_failing.test.ts > 1+1 should equal 2`) | 删 `m83_failing.test.ts` → **绿** ✓ |

**关键设计要点**:
- M1 验的是 `-race` flag 的有效性 — 没有 flag 时, race 不一定可现; 有 flag 时 race detector 必然抓
- M2 验的是 vitest exit code — `npx vitest run` 默认 `bail` 关 (M2 写成单 test file, fail 不影响其他), 但 vitest 整体 exit code = 1 → CI 红
- 两个 mutation 文件都用 `/tmp/` + git 临时操作, 实证完**必须全部删除**, `git status` clean 才算闭环
- 这是 CI 升级 round 独有的 mutation 模式: 不测业务代码, 测**守门**本身是否真守门

### 4. TODO.md L193 切 [x]

```bash
$ git diff TODO.md | grep "CI 升级.*go test -race"
-- [ ] **CI 升级**：加 `go test -race` + frontend vitest 步骤
++ [x] **CI 升级**：加 `go test -race` + frontend vitest 步骤（已 ship M41 `182f621` + B1-3 `3725f40`, M83-candidate 实证闭环 — mutation inversion 见 `M83-candidate-completion-report.md`）
```

### 5. CHANGELOG M83 段

```markdown
- **M83-candidate CI 升级实证闭环** (`.github/workflows/ci.yml` + mutation inversion + docs)
  — TODO.md L193 "CI 升级: 加 `go test -race` + frontend vitest 步骤" 在 M41 (`182f621`, 2026-09-13) + B1-3 (`3725f40`, 2026-07-01) 已 ship
  物理步骤, 本 round **不新增 step**, 走 mutation inversion 实证既有 step 真在守门:
  M1 (race-test file → `go test -race` 红 → 还原绿) + M2 (failing vitest → vitest run 红 → 还原绿) = CI 守门真工作.
  见 `M83-candidate-completion-report.md`.
```

## Risks

- **mutation M1 race 时序非确定**: race detector 抓 race 是**概率性**的, 高竞争下必抓, 低竞争下可能漏. 故意 race 写法 (两个 goroutine 1000 次 `x++`, 加 `time.Sleep(rand)` 拉长窗口) 能保证 99%+ 抓到. 若仍漏, 加 `-race` flag 配 `-count=10` 强抓.
- **mutation M2 vitest exit code 在子 shell 下不传**: `npx vitest run` exit 1 → CI 红; 本地用 `npx vitest run 2>&1 | tail` 会丢 exit code. **验证**: 跑完看 `$?` 必须非 0.
- **临时 mutation 文件误 commit**: 用 `/tmp/` 路径或 `git stash` 隔离, 实证完用 `rm -f` + `git status` 二次确认 clean.
- **本地 Node v26 vs CI Node v22 行为差异**: vitest 1.6.1 + jsdom 29 + antd 5, 两版本 Node 都跑过 (CI 沿用, 本地新增验证). 若发现本地绿 CI 红, 立即 `nvm use 22` 复测.
- **PM_QUEUE state 同步漏**: 沿用 M82 cycle 13 closeout 模式, 必须把 status 切 `shipped` + append `shipped[]` registry, 否则 watchdog 会反复 dispatch M83.
- **mutation inversion 与 M82 同款反模式**: M82 用了真 sqlite 反向断言 (INSERT **不**发生), 本 round 沿用「真 DB 比 mock 更严」思路, 但 CI 守门验证不写 mock — 直接打真 vitest / 真 go test, 让守门失效立刻红.
- **Telegram CLI 不可用**: 沿用 M79, watchdog 报告写到本地.

## Plan

1. **写 `intent-M83-candidate.md`** (本文件, 8 节 omh-plan 骨架) — **feat commit 1**: `feat(M83-candidate): intent spec (omh-plan 8 节骨架, CI 升级实证闭环)`
2. **mutation inversion 实证**:
   - **M1 race**: 写 `backend/internal/eventbus/m83_race_test.go`, 内容为故意 race (两个 goroutine 同时 `var x int; x++` 不带 mutex, 跑 1000 次), 跑 `go test -race -run M83Race ./internal/eventbus/` → 期望红 (`WARNING: DATA RACE`); `rm` 文件 → 期望还原绿 (control: `go test -race ./internal/eventbus/` 绿).
   - **M2 vitest**: 写 `frontend/src/m83_failing.test.ts`, 内容为 `test('M83 mutation inversion', () => { expect(1).toBe(2) })`, 跑 `npx vitest run m83_failing` → 期望红; `rm` 文件 → 期望还原绿 (control: `npx vitest run` 沿用 49 files).
   - 两 mutation 文件实证完**全部 rm**, `git status` 必须 clean.
3. **既有用例复核**: `cd backend && go test -race -count=1 ./...` → 28 packages 全绿; `cd frontend && npx vitest run` → 49 files 全绿.
4. **写 docs**:
   - `M83-candidate-completion-report.md` (CI 守门实证 + mutation inversion 反证 + commit 序列 + 派生 TODO)
   - `M83-candidate-graph-analysis.md` (CI workflow 节点图 + mutation inversion 调用链)
   - `CHANGELOG.md` M83 段加条目
   - `TODO.md` L193 `- [ ]` → `- [x]`
   — **docs commit 2**: `docs(M83-candidate): completion + graph analysis + CHANGELOG + TODO (CI 升级实证闭环)`
5. **commit + push** 2 commits 到 origin/main.
6. **PM_QUEUE state fixup**: M83-candidate.status `candidate` → `shipped`, append `shipped[]` registry, bump `last_updated` / `last_audit`.
7. **写 `~/.hermes/state/PM_LAST_DISPATCH_RESULT.md`** (Poison 看 + watchdog 下次 tick 验证).

### Commit 序列

```
c3fde25 (HEAD, M82-candidate cycle 13)
   ↓
M83 commit 1: feat(M83-candidate): intent spec (omh-plan 8 节骨架, CI 升级实证闭环)
M83 commit 2: docs(M83-candidate): completion + graph analysis + CHANGELOG + TODO (CI 升级实证闭环)
```

(2 commits 即可, 沿用 M78 / M79 / M80 / M81 / M82 既有 pattern; feat 跟 docs 拆开, mutation inversion 在 commit 1 之前完成, 不入 commit.)

## Decision gate

- **D1**: scope = **CI workflow 不退化 + mutation inversion 实证 + docs**, 不动 `.github/workflows/ci.yml` 既有 step (M41 + B1-3 已 ship)
- **D2**: 沿用 poison-stop-gates-v1 (PM_LOOP_MODE = "stop" → freeze)
- **D3**: 沿用 watchdog 自旋防 + commit age ≥ 10 min
- **D4**: 沿用 30-min dispatch hist flapping auto-switch (M79 D4)
- **D5**: mutation inversion = **CI 步骤守门实证** 模式 (M1 race + M2 vitest fail), 与 M82 业务代码 mutation 同形不同物 — 都验证「守卫真工作」
- **D6**: 不写新 fact_store entry (沿用 M82 cycle 13 = fact_id 22 advisory 序列, 本 round = 23 advisory)
- **D7**: 2 commits 即可 (沿用 M78 / M79 / M80 / M81 / M82 pattern)
- **D8**: PM_LAST_DISPATCH_RESULT.md 写到 `~/.hermes/state/`, watchdog 下次 tick 验证
- **D9**: mutation 临时文件**不入 commit** (用 `/tmp/` 或 git stash 隔离, 实证完 rm)
- **D10**: PM_QUEUE M83-candidate.status: `candidate` → **`shipped`**, append `shipped[]` registry (沿用 M82 cycle 13 closeout 范本)
- **D11**: 不动 TODO.md L194 (coverage 阈值门禁) — 那是另一条独立 candidate
- **D12**: 不动 setup-profile.json / display.skin / interface (M67 standing rule 沿用)
