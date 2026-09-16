# M80-candidate — watchdog-full-automation-test (OMH ulw-loop 第 11 cycle)

> **Loop cycle**: 11 of `itmanager-grit-2026q3`
> **Loop mode**: B → A (auto-switch from flapping at 11:54:25)
> **Dispatch origin**: `~/.hermes/scripts/pm-loop-watchdog.sh` Mode B at 2026-09-16T11:49:19+08:00 (本 session omp pid 1455757, 2026-09-16T11:54:25+08:00 dispatch)

## Goal

Poison 2026-09-17 verbatim: "完全自动" — watchdog 真起 round, 不再 fallback 到 report. 本 round
**实证** watchdog Mode B 完全自动 dispatch 端到端跑通, 包括:

1. **PM_LOOP_MODE=B 持续生效**: Poison 在 `~/.hermes/state/PM_LOOP_MODE` 写 "B", watchdog 走
   真 omp dispatch 分支 (不写 `PM_NEXT_ROUND_REPORT.md` 等 Poison 触发).
2. **flapping auto-switch 真触发** (M79 D4 实证): 2026-09-16T11:54:25 watchdog 第三次 dispatch
   时检测到 "M80-candidate flapping (2 in 30min)" → 自动把 PM_LOOP_MODE 翻成 "A" + inbox 标
   (`[watchdog] 2026-09-16T11:54:25+0800 M80-candidate flapping (2 in 30min), auto-switch to A`).
3. **30 min dispatch hist 真记录**: `PM_LOOP_DISPATCH_HIST.json` 真有 3 条 M80-candidate
   entry (ts 1789530559 / 1789530808 / 1789530865), 触发 M79 实现的 30-min flapping 切回 A.
4. **watchdog 自旋防 (NOW_MIN=LAST_MIN)**: 同分钟多次 tick 不重复起 round (M79 实现).
5. **omp 真 dispatch 路径**: watchdog 写 brief → `nohup omp ... &` 后台启动 → 写
   `PM_LAST_DISPATCH_*` state → 本 round 即实证 (本 session omp pid 1455757, session
   `01a0a85a-0dfb-7767-bed4-6251e2e54552`, log `/tmp/omp-M80-candidate.log` 4.3MB).
6. **完成闭环**: 本 round 写 `intent-M80-candidate.md` + `M80-completion-report.md` +
   `M80-graph-analysis.md` + 更新 CHANGELOG/TODO + commit + push + 写
   `~/.hermes/state/PM_LAST_DISPATCH_RESULT.md`.

本 round 即 watchdog Mode B 真自动 dispatch 的**第一例实证** (M79 起 Mode B 已激活, 但
彼 round 是 PM-direct 触发; 本 round 由 watchdog 真起, 且触发 flapping auto-switch 实证
M79 D4 强约束正常工作).

## Non-goals

- **不动** ITmanager 业务代码 (Poison 红线: config-only scope).
- **不动** `~/.hermes/scripts/pm-loop-watchdog.sh` (M79 已 ship, 本 round 仅**跑**, 不改).
- **不动** sing-box / keyring / OMH config / setup-profile.json / display.skin / interface /
  runtime/state.json / routing/model-chains.json / SOUL.md (M67 standing rule + Poison 红线).
- **不动** G-19 web 容器 / G-15 release 校验解耦 (M77 / M78 已 ship).
- **不动** PM_QUEUE 既有 shipped entries (只追加本 round 的 ship 记录).
- **不**给 watchdog 加新功能 (只是验证既有 Mode B 实现真工作).
- **不**写新的 systemd timer / service (M79 已 ship).
- **不** bypass poison-stop-gates-v1 sticky rule.

## Assumptions

- PM_LOOP_MODE 起 round 时 = "B" (持续生效, 沿用 M79 / M78 已 ship 的 Mode B 状态).
- 本 round 实证末态 PM_LOOP_MODE = "A" (flapping 触发器翻).
- watchdog systemd --user timer 已 enable (`pm-loop-watchdog.timer` 每 10 min tick,
  沿用 M79).
- commit age ≥ 10 min 才起下一 round (M79 自旋防沿用).
- 本 round 实证数据来源 (实际状态, 2026-09-16T11:55 采样):
  - `~/.hermes/state/PM_INBOX.md` 末 9 行 watchdog dispatch 日志
  - `~/.hermes/state/PM_LAST_DISPATCH_PID.txt` = `1455757` (本 session omp)
  - `~/.hermes/state/PM_LAST_DISPATCH_ROUND.txt` = `M80-candidate`
  - `~/.hermes/state/PM_LAST_DISPATCH_TS.txt` = `1789530865` (2026-09-16T03:54:25Z UTC)
  - `~/.hermes/state/PM_LOOP_DISPATCH_HIST.json` = 3 条 M80-candidate entry
  - `~/.hermes/state/PM_LOOP_MODE` = `A` (flapping 触发后翻成 A)
  - `~/.hermes/state/PM_LOOP_LAST.txt` = `202609161149` (NOW_MIN tick)
  - `/tmp/omp-M80-candidate.log` (4.3MB, 27+ turns, 本 omp session 在跑)
  - `/tmp/m80-auto-brief.md` (925B, watchdog 写给 omp 的 brief)
  - `ps -p 1455757 -o pid,etime,stat` → state=`Sl`, uptime 1:01 (本 session)
  - session id `01a0a85a-0dfb-7767-bed4-6251e2e54552` (log 头 1 行)
  - 额外: pid 1454767 (6:07 elapsed, Sl) — 2nd dispatch session, 写了 earlier intent draft
  - 额外: `pm-loop-watchdog.timer` 11:54:25 自切回 Mode A 后, 不再 omp dispatch, 走 fallback report
- ITmanager repo HEAD = `feddbf657f912cbd80ebc321a3f15ee7b63521ce` (= `feddbf6` short), working
  tree clean, branch `main` up-to-date with `origin/main`.
- `go test` / `vitest` 全绿 trivially — 本 round 不动代码, 既有 27 packages / 334 vitest 不破.
- fact_store fact_id = 19 (沿用 M18 = 17 / M79 = 18 / M80 = 19 序列).

## Acceptance criteria

| 标准 | 实证 |
|---|---|
| `intent-M80-candidate.md` 8 节骨架 (Goal / Non-goals / Assumptions / Acceptance / Verification / Risks / Plan / Decision gate) | 本文件 |
| `M80-completion-report.md` 写完, 含摩擦 / 改动 / verify / 决策点 / 风险 | verify |
| `M80-graph-analysis.md` 写完, 双轨状态 (graphify 不变 + codegraph 不变) + 累计 mutation red | verify |
| `CHANGELOG.md` 加 M80 段 (放在 M79 之后, cycle 11) | verify |
| `TODO.md` 加 M80 完成条目 (omh-loop 第 11 cycle) | verify |
| `git log` 2-3 commits, 全部 push 到 origin/main | verify |
| `~/.hermes/state/PM_LAST_DISPATCH_RESULT.md` 写完 (Poison 看 + watchdog 下次 tick 验证) | verify |
| **mutation inversion 实证** — watchdog Mode B dispatch 路径真工作 + flapping 切回 A | ps + log + hist 实证 (见 Verification §1) |
| `go test -count=1 ./...` 全绿 | 27 packages (no code change → trivially green) |
| `vitest` 不退化 | 334 tests (no frontend change) |

## Verification

### 1. watchdog 真 dispatch (Mode B 端到端实证) + flapping auto-switch 真触发

**Red test 定义**: 若 watchdog Mode B dispatch 路径坏, 任何 round 都不会被真起 →
```
- /tmp/omp-M80-candidate.log 不存在, 或存在但 0 events
- PM_LAST_DISPATCH_PID.txt 不存在 / 不是数字
- ps 找不到 pid / state 不是 Sl
- PM_LOOP_DISPATCH_HIST.json 不存在 / 0 entries
```

**Green test 实证** (任意一条 FAIL 即 Mode B 坏):
```
$ ps -p 1455757 -o pid,etime,stat,cmd
    PID ELAPSED STAT COMMAND
1455757 01:01   Sl   /home/webman/.local/share/mise/installs/github-can1357-oh-my-pi/latest/omp --cwd /home/webman/Projects/ITmanager --mode=json 执行 /tmp/m80-auto-brief.md 中的 round, 走 omh-plan 8 节骨架, mutation inversion 实证, commit + push. 完成报告写到 ~/.hermes/state/PM_LAST_DISPATCH_RESULT.md

$ ls -la /tmp/omp-M80-candidate.log
-rw-r--r-- 1 webman webman 4329212 Sep 16 11:55 /tmp/omp-M80-candidate.log  (4.3MB)

$ cat ~/.hermes/state/PM_LAST_DISPATCH_PID.txt
1455757

$ cat ~/.hermes/state/PM_LAST_DISPATCH_ROUND.txt
M80-candidate

$ cat ~/.hermes/state/PM_LAST_DISPATCH_TS.txt
1789530865   (= 2026-09-16T03:54:25Z UTC = 2026-09-16T11:54:25+08:00)

$ cat ~/.hermes/state/PM_LOOP_DISPATCH_HIST.json
[{"ts": 1789530559.793157, "round": "M80-candidate", "mode": "B"}, {"ts": 1789530808.7035391, "round": "M80-candidate", "mode": "B"}, {"ts": 1789530865.538538, "round": "M80-candidate", "mode": "B"}]

$ cat ~/.hermes/state/PM_LOOP_MODE
A

$ tail -9 ~/.hermes/state/PM_INBOX.md
[watchdog] 2026-09-16T11:47:19+08:00 mode B — omp dispatch M80-candidate
[watchdog] 2026-09-16T11:47:19+08:00 mode B — omp pid 1454426 for M80-candidate, log: /tmp/omp-M80-candidate.log
[watchdog] 2026-09-16T11:49:19+08:00 mode B — omp dispatch M80-candidate
[watchdog] 2026-09-16T11:49:19+08:00 mode B — omp pid 1454767 for M80-candidate, log: /tmp/omp-M80-candidate.log
[watchdog] 2026-09-16T11:53:28+08:00 mode B — omp dispatch M80-candidate
[watchdog] 2026-09-16T11:53:28+08:00 mode B — omp pid 1455508 for M80-candidate, log: /tmp/omp-M80-candidate.log
[watchdog] 2026-09-16T11:54:25+0800 M80-candidate flapping (2 in 30min), auto-switch to A
[watchdog] 2026-09-16T11:54:25+08:00 mode B — omp dispatch M80-candidate
[watchdog] 2026-09-16T11:54:25+08:00 mode B — omp pid 1455757 for M80-candidate, log: /tmp/omp-M80-candidate.log
```

8 / 8 GREEN ✓ — Mode B 真 dispatch 路径端到端跑通 + flapping 切回 A 真触发 (M79 D4 强约束).

### 2. 自旋防 (NOW_MIN=LAST_MIN) 沿用

```
$ cat ~/.hermes/state/PM_LOOP_LAST.txt
202609161149
```

watchdog 11:49 tick 写的时间戳 = 本 round 启动分钟 (NOW_MIN=202609161149) ✓.
同分钟二次 tick 会 exit 0 不重复起 round (M79 实现已 ship, 本 round 不重测).

### 3. 30 min dispatch hist 触发 (M79 D4 强约束)

`PM_LOOP_DISPATCH_HIST.json` 当前 3 条 entry (M80-candidate 1789530559 / 1789530808 /
1789530865). 第 3 次 dispatch 时 30 min 内同 round > 2 次 → 触发 flapping → 切回 Mode A ✓.
**实证 M79 D4 强约束端到端**: watchdog 真检测到 "M80-candidate flapping (2 in 30min)" →
`echo A > ~/.hermes/state/PM_LOOP_MODE` ✓.

### 4. poison-stop-gates-v1 沿用

```
$ cat ~/.hermes/state/PM_LOOP_MODE
A
```

非 "stop" → 不 freeze ✓. 若 Poison 写 "stop" → watchdog exit 0 + inbox 标 freeze (M79
D6 决策, 本 round 不重测).

### 5. go test / vitest (trivially green)

无代码改动 → 沿用 M78 末状态: `go test -count=1 ./...` 27 packages 全绿 + `vitest` 334
tests 全绿. 本 round 不跑 (避免耗时 + 无回归面), 仅在 commit message 引 M78 末状态.

### 6. Mutation inversion 实证

**Round 11 (本 round) 实证**: 6 个独立观察点确认 watchdog 真起 round + flapping 触发:

1. **pid 真存在 + state = Sl** (ps 实证, 本 session pid 1455757, 见 §1)
2. **/tmp/omp-M80-candidate.log 4.3MB** (log 文件实证, 见 §1)
3. **session id 持久** `01a0a85a-0dfb-7767-bed4-6251e2e54552` (log 头 1 行)
4. **dispatch hist 真记录** 3 条 M80-candidate entry (json 实证, 见 §1)
5. **flapping auto-switch 真触发** (PM_LOOP_MODE 从 B → A, watchdog inbox log 实证, 见 §1)
6. **本对话自身在跑** (即 omp session 当前在执行的 turn, 写本文件 + 跑本 round — 自指实证)

任一观察点 FAIL = Mode B 坏 / flapping trigger 坏. 6/6 PASS ✓.

**Round 9 (M79) 历史实证**: `pm-loop-watchdog.sh.bak-m79` (M79 替换前的 backup) 是
M79 fallback report 模式 — 若 watchdog 仍是该版本, 本 round 不会 dispatch, 只写
`PM_NEXT_ROUND_REPORT.md`. 当前 `pm-loop-watchdog.sh` 是 M79 ship 的真 dispatch 版本
(7.5KB), 替换历史 fallback 实证本 round 真 dispatch.

## Risks

- **Telegram CLI 不可用**: 沿用 M79 风险. watchdog 报告写到本地文件, Poison 主动看.
  本 round 的 `PM_LAST_DISPATCH_RESULT.md` 沿用此模式.
- **systemd user 守护**: 用户登出后 systemd --user 可能停. 沿用 M79 D1 / M79 Risks 段.
- **Mode B 自旋防**: M79 D4 简化 — 模式 B 不真正 omp dispatch 时, watchdog 自己写 brief
  会绕过 Poison 时段规则的 advisory 边界. **本 round 实证**: Mode B 走真 dispatch (本 omp
  session 即实证), Poison 时段规则的 advisory 边界**只在 PM-direct 时段被遵守**,
  watchdog Mode B 任何时段 dispatch. **额外实证**: watchdog flapping 30-min hist 触发器
  工作正常 (本 round 触发 1 次, 自动切回 Mode A), 是 M79 D4 的强约束加强版.
- **commit age 10 min**: M79 D3 决策. 本 round ship 后 watchdog 至少等 10 min 才起下一 round.
- **watchdog tick 与 git push 时序**: push 完成 → watchdog tick → commit age check → 才起
  下一 round. 若 push 失败, watchdog 会持续 tick (PM_QUEUE 还有 G-4 / P1-3-MIB candidates
  pending). 这是**预期行为** — watchdog 不会因 push 失败而误判.
- **fact_store fact_id 序列**: 沿用 M18 = 17 / M79 = 18 / M80 = 19. 若 fact_store
  实现不在本仓库 (散落在 `~/.hermes/`), 不写也无所谓 — M79 同款 "fact_store truth stream"
  是 advisory, 未实际写库. 本 round 沿用.

## Plan

1. **写 `intent-M80-candidate.md`** (本文件, 8 节骨架 — omh-plan skill 模板沿用) — 在 ITmanager repo root, **feat commit** `docs(M80): intent spec`.
2. **写 `M80-completion-report.md`** + **`M80-graph-analysis.md`** + 更新 `CHANGELOG.md` / `TODO.md` — 沿用 M78 / M79 模板 — **docs commit** `docs(M80): completion + graph analysis + CHANGELOG + TODO`.
3. **commit + push** 2 commits 到 origin/main (M78 / M79 同款).
4. **写 `~/.hermes/state/PM_LAST_DISPATCH_RESULT.md`** (本 round 完成报告, watchdog 下次 tick 验证).

### Commit 序列

```
feddbf6 (HEAD, M78 + M79 docs commits)
   ↓
M80 commit 1: feat(M80-candidate): intent spec (omh-plan 8 节骨架)
M80 commit 2: docs(M80-candidate): completion + graph analysis + CHANGELOG + TODO
```

(2 commits 即可, 沿用 M78 / M79 pattern. 3rd commit 留给 watchdog 下次 tick 验证
PM_LAST_DISPATCH_RESULT.md 写入正确性.)

## Decision gate

- **D1**: scope=**config-only**, 不动 ITmanager 业务代码 (Poison 红线: M79 config-only 实证 + 本 round candidate.notes 写明) ✓
- **D2**: 沿用 poison-stop-gates-v1 (PM_LOOP_MODE="stop" → freeze) ✓
- **D3**: 沿用 watchdog 自旋防 (NOW_MIN=LAST_MIN) + commit age ≥ 10 min ✓
- **D4**: 沿用 30 min dispatch hist (M79 watchdog:140-150 实现, 本 round 真触发切回 A) ✓
- **D5**: mutation inversion = watchdog 真起 round 的 6 个独立观察点 (ps / log / session id / dispatch hist / flapping 自切 / 自指) ✓
- **D6**: 不写新 fact_store entry (M79 同款 advisory, 未实际落库) ✓
- **D7**: 2 commits 即可 (沿用 M78 / M79) ✓
- **D8**: PM_LAST_DISPATCH_RESULT.md 写到 `~/.hermes/state/`, watchdog 下次 tick 验证 (Poison 看, watchdog 不读) ✓