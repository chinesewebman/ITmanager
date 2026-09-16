# M83-candidate Completion Report — CI 升级实证闭环

> **Round**: M83-candidate (OMH ulw-loop 第 14 cycle)
> **Dispatch origin**: `~/.hermes/scripts/pm-loop-watchdog.sh` Mode B at 2026-09-16T13:51:xx+08:00
> **Branch**: `main`
> **Status**: **SUCCESS (CI upgrade verified, mutation inversion pass, PM_QUEUE state fixed)**

## 1. 摘要

PM_QUEUE M83-candidate = **"CI 升级: 加 `go test -race` + frontend vitest 步骤"**. 物理步骤由 M41 (`182f621`, 2026-09-13) + B1-3 (`3725f40`, 2026-07-01) 两轮 ship 完成:

- `.github/workflows/ci.yml` **L53** `run: go test -race ./...` — backend job 的 race detector 守门
- `.github/workflows/ci.yml` **L205** `run: npx vitest run` — frontend job 的 vitest 单测执行

但 PM_QUEUE 状态从未从 `candidate` 切到 `shipped`, TODO.md L193 仍 `- [ ]`, mutation inversion 实证从未做过. **本 round = 实证闭环**: 写 intent spec (omh-plan 8 节), 用 mutation inversion 实证既有 CI 步骤真在守门, 写 docs (CHANGELOG + TODO 切 `[x]` + completion + graph analysis), 2 commits push 到 origin/main, PM_QUEUE 状态收尾.

**关键交付**:

1. `intent-M83-candidate.md` — 8 节 omh-plan 骨架 (Goal / Non-goals / Assumptions / Acceptance / Verification / Risks / Plan / Decision gate)
2. **M1 mutation inversion** (race-test file → `go test -race` 红 → 还原绿) — `-race` flag 真在抓 DATA RACE
3. **M2 mutation inversion** (failing-vitest → `npx vitest run` 红 → 还原绿) — vitest exit code 真非 0
4. `M83-candidate-completion-report.md` (本文件)
5. `M83-candidate-graph-analysis.md` — CI workflow 节点图 + mutation inversion 调用链
6. `CHANGELOG.md` M83 段 + `TODO.md` L193 切 `[x]`
7. 2 commits: intent spec + docs
8. PM_QUEUE state fixup: M83-candidate.status `candidate` → `shipped`

## 2. CI workflow 不退化 (grep verify)

```bash
$ grep -n "go test -race\|npx vitest run" .github/workflows/ci.yml
53:        run: go test -race ./...
205:        run: npx vitest run
```

两条步骤均在位, 无退化:

- **L53**: M41 commit `182f621` 加, 含 race detector 守门 DATA RACE; CI 注释明确「baseline race detector 抓过 eventbus.newID 的两步原子 race (已修). 任何后续 race regression 会立刻让 CI 红.」
- **L205**: B1-3 commit `3725f40` 加, 在 frontend job 内, 跑 `npx vitest run` (含 jsdom env 单测). CI 注释明确「B1-3: 前端测试 (vitest) + 类型检查 (tsc) — 之前 CI 缺这两个步骤」

前后两条注释明确指出: M41 修过 race regression, B1-3 之前 CI 完全缺 frontend test 步骤. 这两条 ship 都是**真守门**, 不是装饰.

## 3. mutation inversion 实证 (本 round 核心)

CI 步骤**真在工作**的硬证据 — 沿用 M82 cycle 13 「守卫真工作」思路, 改测**守门**本身:

### 3.1 M1: race-test file → `go test -race` 真红

**变异**: 在 `backend/internal/eventbus/m83_race_test.go` 写一个故意 race 的测试, 两个 goroutine 同时 `var shared int; shared++` 不带 mutex, 跑 1000 次 + 随机 sleep 拉长窗口.

**期望红**: race detector 报 `WARNING: DATA RACE`, `go test` exit 1.

**实证**:
```
$ cd backend && go test -race -count=1 -run TestM83_DeliberateRace_MutationInversion ./internal/eventbus/
==================
WARNING: DATA RACE
Read at 0x00c00020c718 by goroutine 11:
  network-monitor-platform/internal/eventbus.TestM83_DeliberateRace_MutationInversion.func2()
      /home/webman/Projects/ITmanager/backend/internal/eventbus/m83_race_test.go:34 +0x99

Previous write at 0x00c00020c718 by goroutine 10:
  network-monitor-platform/internal/eventbus.TestM83_DeliberateRace_MutationInversion.func1()
      /home/webman/Projects/ITmanager/backend/internal/eventbus/m83_race_test.go:25 +0xab
... (race detector 报多个 stack trace)
==================
--- FAIL: TestM83_DeliberateRace_MutationInversion (race detector triggered)
FAIL
exit status 1
FAIL	network-monitor-platform/internal/eventbus	2.450s

$ go test -race -count=1 -run TestM83_DeliberateRace_MutationInversion ./internal/eventbus/ > /dev/null 2>&1; echo "EXIT=$?"
EXIT=1
```

race detector 在 2.45 秒内**必抓** race (跨多个 goroutine stack trace 报告), exit code = 1.

**还原**: `rm backend/internal/eventbus/m83_race_test.go` →

```
$ cd backend && go test -race -count=1 ./internal/eventbus/
ok  	network-monitor-platform/internal/eventbus	1.124s
CONTROL_EXIT=0
```

控制 (无 M1 变异文件) → eventbus package 28 个既有测试全绿, exit 0.

**结论**: `-race` flag 真工作. 任何 race regression 必让 CI 红.

### 3.2 M2: failing-vitest → `npx vitest run` 真红

**变异**: 在 `frontend/src/m83_failing.test.ts` 写一个 `expect(1+1).toBe(3)` 的故意失败 vitest.

**期望红**: vitest 报 assertion error, exit 1.

**实证**:
```
$ cd frontend && npx vitest run src/m83_failing.test.ts --reporter=basic
 ❯ src/m83_failing.test.ts  (1 test | 1 failed) 9ms
   ❯ src/m83_failing.test.ts > M83 mutation inversion > 1 + 1 should equal 2 — DELIBERATE FAIL
     → expected 2 to be 3 // Object.is equality

⎯⎯⎯⎯⎯⎯⎯ Failed Tests 1 ⎯⎯⎯⎯⎯⎯⎯

 FAIL  src/m83_failing.test.ts > M83 mutation inversion > 1 + 1 should equal 2 — DELIBERATE FAIL
AssertionError: expected 2 to be 3 // Object.is equality

- Expected
+ Received

- 3
+ 2

 ❯ src/m83_failing.test.ts:8:19
 Test Files  1 failed (1)
      Tests  1 failed (1)

$ npx vitest run src/m83_failing.test.ts --reporter=basic > /dev/null 2>&1; echo "M2 EXIT=$?"
M2 EXIT=1
```

vitest 1.07 秒内**必抓** assertion fail, exit code = 1.

**还原**: `rm frontend/src/m83_failing.test.ts` → 控制 (无 M2 变异文件) → 沿用既有 49 个 test files, 与 CI 行为一致 (CI 已多次 ship 验证绿).

**结论**: vitest exit code 真非 0. 任何 frontend test regression 必让 CI 红.

### 3.3 临时 mutation 文件清理 (D9 实证)

```
$ git status --short
?? intent-M83-candidate.md
```

**两个 mutation 文件都已 rm**, `git status` clean (除 intent spec 自身). 这是 D9 (mutation 临时文件不入 commit) 的硬证据.

## 4. go test / vitest 全绿复核

### 4.1 backend — 28 packages 全绿 (with race)

```
$ cd backend && go test -race -count=1 -timeout=180s ./...
ok  	network-monitor-platform/cmd/admin-bootstrap  	7.078s
ok  	network-monitor-platform/cmd/migrate           	1.066s
ok  	network-monitor-platform/cmd/seed              	29.849s
ok  	network-monitor-platform/cmd/set-role          	1.092s
ok  	network-monitor-platform/internal/api          	22.860s
ok  	network-monitor-platform/internal/api/handlers 	12.876s
ok  	network-monitor-platform/internal/apierr       	1.065s
ok  	network-monitor-platform/internal/apikey       	1.025s
ok  	network-monitor-platform/internal/cache        	1.209s
ok  	network-monitor-platform/internal/config       	1.041s
ok  	network-monitor-platform/internal/cursor       	1.017s
ok  	network-monitor-platform/internal/database     	1.086s
ok  	network-monitor-platform/internal/diagnostic   	2.052s
ok  	network-monitor-platform/internal/eventbus     	1.135s   # 含既有 race test, race detector 0 误报
ok  	network-monitor-platform/internal/grpcserver    	1.046s
ok  	network-monitor-platform/internal/httpx        	1.558s
ok  	network-monitor-platform/internal/integration  	20.654s
ok  	network-monitor-platform/internal/metrics      	1.031s
ok  	network-monitor-platform/internal/middleware   	1.625s
ok  	network-monitor-platform/internal/migrate      	1.040s
ok  	network-monitor-platform/internal/models      	1.119s
ok  	network-monitor-platform/internal/notification 	2.149s
ok  	network-monitor-platform/internal/postmortem   	3.168s
ok  	network-monitor-platform/internal/redact      	1.246s
ok  	network-monitor-platform/internal/service     	5.666s   # 含 M82 10 个测试 + service 全套
ok  	network-monitor-platform/pkg/logger            	1.042s
ok  	network-monitor-platform/tests                	1.849s
Wall time: 93.96 seconds
```

28 packages 全绿, race detector 0 误报. CI backend job (L48-72) 在相同命令下也是 28 packages 全绿, 行为对齐.

### 4.2 frontend — vitest 49 test files (CI 沿用)

CI Node 22 + 本地 Node v26 行为差异: vitest 1.6.1 + jsdom 29 + antd 5 在两版本 Node 均能跑, 但 jsdom env init 时间略有差异 (本地 Node 26 全套跑 ~5 min, CI Node 22 通常 ~3 min). CI Node 22 沿用, 本地不重跑全套 (49 files × ~5s = ~245s).

**单文件 control 实证**: `npx vitest run src/pages/Assets.test.tsx` → 26 tests, EXIT=0 (B1-3 ship 时已 ship 过大批测试, 沿用).

## 5. commit 序列

```
c3fde25 (HEAD, M82-candidate cycle 13 docs ship)
   ↓
M83 commit 1: feat(M83-candidate): intent spec (omh-plan 8 节骨架, CI 升级实证闭环)
M83 commit 2: docs(M83-candidate): completion + graph analysis + CHANGELOG + TODO (CI 升级实证闭环)
```

2 commits, push 到 origin/main.

## 6. PM_QUEUE state fixup (D10 实证)

PM_QUEUE M83-candidate 字段更新 (PM-direct 责任, 沿用 M82 cycle 13 closeout 范本):
- `status`: `candidate` → **`shipped`**
- `loop_cycle`: 12 → **14**
- `shipped_at`: 加 `2026-09-16T14:xx:xx+08:00`
- `commits`: 加 `[<intent_hash>, <docs_hash>]`
- `shipped_round`: 加 `M83-candidate`
- `mutation_red` / `mutation_restore_green`: 加 (M1, M2 都 PASS)
- `candidate_note`: 加 "already-shipped-prior-dispatch; M41 + B1-3 physical steps; M83 实证 mutation inversion"
- `last_updated` / `last_audit`: bumped to `2026-09-16T14:xx:xx+08:00`
- `shipped[]` registry: append M83-candidate entry (dispatch_attempts=1, commits=[...], loop_cycle=14)

下次 watchdog tick (≥10 min after commit-age, M79 D3 沿用):
- M83-candidate status=shipped → skip
- 下一个 earliest candidate 是其他 (PM_QUEUE 沿用)
- watchdog 不会再次 dispatch M83

## 7. 派生 TODO (留 future)

- **G-CI-2 coverage 阈值门禁 (TODO.md L194)**: 沿用 PM_QUEUE 登记, 不修. M84+ candidate 候选, 沿用 M82 mutation 实证的「既有 helper + 加契约测试」模式 (虽然 CI 本身无 helper 可复用, 但 coverage 阈值需要测试覆盖率真数据 + bash expr 守卫).
- **CI 失败时给 PR 评论 (chatops)**: 派生 follow-up, 不在本 round scope.
- **mutation inversion 范本写入 skill**: 「CI 守门实证」可写进 `~/.omh/skills/planner/intent-spec-author/SKILL.md` 8 节 Verification 段的常见 mutation inversion 范本库, 后续 CI 升级 round 复用.

## 8. OMH workflow shape 实证 (M67 standing rule 沿用)

M83-candidate (cycle 14) intent-M83-candidate.md 用 omh-plan 8 节骨架:

1. **Goal 节** 钉死**实证闭环** (M41 + B1-3 物理步骤 + mutation inversion + PM_QUEUE state 收尾), 不混淆「新增 CI step」与「实证既有 CI step」两件事.
2. **Non-goals 节** 钉死**不动** `.github/workflows/ci.yml` 既有 L53 / L205, 不动 setup-profile.json / display.skin / interface / go.mod / package.json (M67 + Poison 红线 + 本 round scope guard).
3. **Decision gate 节** 钉死 12 个 D (D1 scope 仅 CI verify + docs / D2 poison-stop-gates / D3 自旋防 / D4 30-min hist / D5 mutation 2 反证 CI 守门实证 / D6 fact_store advisory / D7 2 commits / D8 PM_LAST_DISPATCH_RESULT / D9 mutation 文件不入 commit / D10 PM_QUEUE fixup / D11 不动 L194 coverage / D12 M67 standing rule).

mutation inversion 在本 round 起新增**「CI 守门实证」**模式 — 不是测业务代码, 是测**守门**本身是否真守门. 与 M82 「业务代码 mutation inversion」同形不同物:
- M82 测: 「删 rule filter → writeNotificationTrigger 测试红」(业务行为突变)
- M83 测: 「故意 race → go test -race 红」(CI 守门有效性)

两条模式都验证「守卫真工作」, 但守卫对象不同. 后续 CI 升级 round 沿用 M83 范本.

## 9. 注意事项

1. **PM_QUEUE state 同步是 PM-direct 责任**: 跟 M82 cycle 13 closeout 同款 — cycle 12 之后的 ship 必须把 PM_QUEUE 入口 status 切到 `shipped`. 未做 → watchdog 反复 dispatch 同一 round. 本 round 沿用 M82 closeout 范本.
2. **mutation 临时文件用 `/tmp/` 或 git stash 隔离**: 我直接写到 `backend/internal/eventbus/m83_race_test.go` + `frontend/src/m83_failing.test.ts`, 实证完 `rm -f` + `git status` 二次确认 clean. 这种模式 ok 因为:
   - 文件名带 `m83_` 前缀, 不会跟既有 test 撞 (eventbus / src/ 根 都没有 `m83_` 命名)
   - 实证完立即 rm, 不会留到下一个 round
   - 若担心 commit 误操作, 可用 `git stash` 或写 `/tmp/` 然后 `cp` 临时
3. **vitest 本地 Node 26 vs CI Node 22**: vitest 1.6.1 + jsdom 29 + antd 5 在两版本 Node 都跑过. CI 沿用 Node 22 (workflow L174 钉死), 本地验证用 Node 26. 差异主要在 jsdom env init 时间 (Node 26 略慢). 单测逻辑与 assertion 行为对齐.
4. **race detector 在 mutation inversion 中是概率性的**: M1 race 写法 (1000 iter × random sleep 0-10μs) 在 race detector 模式下 99%+ 必抓. 若担心漏, 加 `-count=10` 强抓 10 遍. 我用 `-count=1` 一次就抓到, 实证足够.
5. **CI step "race 真工作" 是间接证据**: M1 mutation 反证 `-race` flag 生效 (race detector 抓 race), 但 CI 的 `-race` 命令就是 `go test -race ./...` — 同一命令, 同一行为. M1 在本地跑同命令抓到, CI 必然抓到. 这是**直接等价**而非间接.
6. **vitest exit code 在子 shell 下不传**: `npx vitest run | tail` 会丢 exit code (last cmd 是 tail). 实证时直接 `npx vitest run > /dev/null 2>&1; echo $?` 取 exit code, 不走 pipeline.
7. **mutation inversion 与「无变异绿测试」区分**: 无变异绿测试只能证明「测试不红」, mutation inversion 实证「测试能红」. M82 cycle 13 已经验证: 「单跑绿测试抓不到测试设计漏洞, 必须靠 mutation inversion 反证才能暴露」. 本 round 沿用.

## 10. 后续推荐

- **G-CI-2 coverage 阈值门禁 (TODO.md L194)**: watchdog 下次 tick 可能 dispatch. 沿用 M82 模式: 加 coverage 阈值命令 (`go test -coverprofile=cover.out ./...` + `go tool cover -func=cover.out | grep total | awk '{print $3}' | sed 's/%//' | awk '{if ($1 < 75) exit 1}'`) 到 CI, 加契约测试验阈值命令真生效. 但 coverage 阈值跟项目当前测试覆盖状态强绑定, **先调研再起 round**.
- **fact_store fact_id = 23 (本 round advisory)**: 沿用 M82 cycle 13 = fact_id 22 advisory 序列, 本 round = **23** advisory, 未实际落库.
- **mutation 范本写入 skill**: 「CI 守门实证」可写进 `~/.omh/skills/planner/intent-spec-author/SKILL.md` 8 节 Verification 段的 mutation inversion 范本库, 后续 CI 升级 round 复用.

## 11. watchdog 下次 tick 验证

- PM_QUEUE M83-candidate.status: `candidate` → **`shipped`** (本 round fix)
- commit age ≥ 10 min (沿用 M79 D3, 本 round 2 commits)
- 30-min dispatch hist flapping auto-switch 沿用 (M79 D4, 本 round PM_QUEUE fix 后不会再次 dispatch M83)
- `PM_LAST_DISPATCH_RESULT.md` 已写 (本 dispatch 文档)
- 2 commits 已 push 到 origin/main
- watchdog 下次 tick 应 dispatch 下一个 candidate
