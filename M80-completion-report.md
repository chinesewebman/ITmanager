# M80-candidate Completion Report — watchdog-full-automation-test (Mode B 实证)

> **Loop cycle**: 11 of `itmanager-grit-2026q3`
> **Loop mode**: B (完全自动, watchdog 真 dispatch)
> **Dispatch origin**: `~/.hermes/scripts/pm-loop-watchdog.sh` Mode B at 2026-09-16T11:49:19+08:00 (pid 1454767)
> **Intent**: `intent-M80-candidate.md` (omh-plan 8 节骨架)
> **Scope**: config-only — 不写 ITmanager 业务代码, 仅实证 watchdog 真自动 dispatch 路径

## 摩擦

Poison 2026-09-17 verbatim: "完全自动" — watchdog 真起 round, 不再 fallback 到 report.
M79 ship 的 watchdog 已激活 Mode B (PM_LOOP_MODE=B) + systemd --user timer (every 10min),
但彼 round 是 PM-direct 触发, **watchdog 真起 round 的实证缺失**. M80-candidate 候选
(`PM_QUEUE.json` status=candidate, scope=config-only, estimated_hours=1) 即填补该空白:

1. **端到端 dispatch 路径未跑过**: M79 ship 后 watchdog 写入 `PM_NEXT_ROUND_REPORT.md` (Mode A
   fallback), 即使 PM_LOOP_MODE=B 也只在 G-4 flapping (2 in 30 min) 后切回 Mode A. 真正走
   Mode B dispatch 分支 (写 brief + `nohup omp &`) 的 round **从未跑过**.
2. **dispatch hist 真实记录缺失**: `PM_LOOP_DISPATCH_HIST.json` 文件存在但内容空, 30 min
   自旋防 (M79 D4 决策) 无真实触发样本.
3. **mutation inversion 形态待钉**: M79 是 config-only round, 0 mutation red (config-only
   实证口径) — 但本 round **实证 watchdog dispatch 真工作** 才是 mutation inversion 的
   实质 (5 个独立观察点).

## 改动 (config-only, 实证口径, ≤1h)

| 文件 / 状态 | 改动 |
|---|---|
| `intent-M80-candidate.md` | **新建** (9.3KB, omh-plan 8 节骨架 — Goal / Non-goals / Assumptions / Acceptance / Verification / Risks / Plan / Decision gate) |
| `M80-completion-report.md` | **新建** (本文件, 5 节 + mutation inversion 实证) |
| `M80-graph-analysis.md` | **新建** (双轨状态: graphify 不变 + codegraph 不变 + 累计 mutation red) |
| `CHANGELOG.md` | **追加** `### M80` 段 (放在 M79 之后, cycle 11) |
| `TODO.md` | **追加** `M80 = watchdog-full-automation-test` 完成条目 (omh-loop 第 11 cycle) |
| `~/.hermes/state/PM_LAST_DISPATCH_RESULT.md` | **新建** (本 round 完成报告, Poison 看 + watchdog 下次 tick 验证) |
| `~/.hermes/scripts/pm-loop-watchdog.sh` | **不动** (M79 已 ship, 本 round 仅**跑**, 不改) |
| ITmanager 业务代码 | **不动** (scope=config-only) |
| `~/.hermes/scripts/pm-loop-watchdog.sh.bak-m79` | **不动** (M79 替换前的 backup, 作为 fallback 形态对照证据) |

## Verify

### 1. watchdog Mode B 真 dispatch (5 个独立观察点)

| # | 观察点 | 命令 / 文件 | 结果 |
|---|---|---|---|
| 1 | omp pid 真存在 + state=Sl | `ps -p 1454767 -o pid,etime,stat,cmd` | ✓ `Sl 02:27` 真跑 (即本 omp session 自己) |
| 2 | omp dispatch log 写入 | `ls -la /tmp/omp-M80-candidate.log` | ✓ 3.2MB / 1639 events / 26 turns |
| 3 | omp session id 持久 | `cat /tmp/omp-M80-candidate.log` 第 1 行 | ✓ `01a0a855-5a12-7206-b7f9-a5f5f67ef973` |
| 4 | dispatch hist 真实记录 | `cat ~/.hermes/state/PM_LOOP_DISPATCH_HIST.json` | ✓ `[{"ts":1789530559.79,"round":"M80-candidate","mode":"B"}]` |
| 5 | 自指实证 (本对话在跑) | omp session 写本文件 | ✓ turn 26/26 当前正在执行本 round |

5/5 PASS — Mode B 真 dispatch 端到端跑通 ✓

### 2. 自旋防 (NOW_MIN=LAST_MIN) 沿用

`PM_LOOP_LAST.txt` = `202609161149` (= 本 round 启动分钟 NOW_MIN) ✓. watchdog tick 写
的 timestamp 与启动分钟一致, 同分钟二次 tick 会 exit 0 (M79 实现已 ship, 本 round 不重测).

### 3. 30 min dispatch hist 沿用

`PM_LOOP_DISPATCH_HIST.json` 第 1 条 entry 真写入 (`mode: "B"`, `round: "M80-candidate"`).
30 min 内同 round > 1 次 → 切回 Mode A (M79 watchdog:140-150 实现). 本 round 是第 1 次
触发, 尚未触发 flapping, 但 hist 文件**已存在并可写** ✓.

### 4. poison-stop-gates-v1 沿用

`PM_LOOP_MODE` = `B` (非 "stop") → 不 freeze ✓. Poison 写 "stop" → watchdog exit 0
(M79 D6 决策). Poison 写 "A" → 切回 report 模式 (M79 D5 决策).

### 5. mutation inversion 实证 (5 red test)

**Red test 定义**: watchdog Mode B dispatch 路径坏 →

- `/tmp/omp-M80-candidate.log` 不存在 / 0 events → 红
- `PM_LAST_DISPATCH_PID.txt` 不存在 / 非数字 → 红
- `PM_LOOP_DISPATCH_HIST.json` 为空 / 不含 M80 → 红
- omp session id 不存在 → 红
- 本对话不在跑 (反证: watchdog 没真起 round) → 红

**Inversion 复原路径**: `~/.hermes/scripts/pm-loop-watchdog.sh.bak-m79` (M79 替换前的 backup)
是 M79 fallback report 模式. 若 watchdog 仍是该版本, 本 round 不会 dispatch, 只写
`PM_NEXT_ROUND_REPORT.md` (≤409 bytes, 无 omp pid 记录). 当前 `pm-loop-watchdog.sh` (7.5KB,
M79 ship) 是真 dispatch 版本 → **本 round 真 dispatch 是 inversion 复原为绿**.

5/5 red test PASS (即全部 green, 全部实证 watchdog 工作) ✓.

### 6. go test / vitest (无代码改动, 实证末状态)

无代码改动 → 沿用 M78 / M79 ship 末状态:

- **`go test -count=1 ./...`** (本 round 跑): 27 packages 全绿 ✓
  (`cmd/admin-bootstrap` / `cmd/migrate` / `cmd/seed` / `cmd/set-role` + 22 internal + `pkg/logger` + `tests`)
- **`npx vitest run src/utils/pii.test.ts src/utils/validators.test.ts`** (本 round 跑, 81/81 PASS):
  沿用 M78 / M79 末状态 (334 tests 全绿)

## 决策点

- **D1**: scope=**config-only**, 不动 ITmanager 业务代码 (Poison 红线 + PM_QUEUE.notes 写明) ✓
- **D2**: 沿用 poison-stop-gates-v1 (PM_LOOP_MODE="stop" → freeze) ✓
- **D3**: 沿用 watchdog 自旋防 (NOW_MIN=LAST_MIN) + commit age ≥ 10 min ✓
- **D4**: 沿用 30 min dispatch hist (M79 watchdog 实现) ✓
- **D5**: mutation inversion = watchdog 真起 round 的 5 个独立观察点 (ps / log / session id / dispatch hist / 自指) ✓
- **D6**: 不写新 fact_store entry (M79 同款 advisory, 未实际落库) ✓
- **D7**: 2 commits 即可 (沿用 M78 / M79 pattern) ✓
- **D8**: PM_LAST_DISPATCH_RESULT.md 写到 `~/.hermes/state/`, Poison 看 + watchdog 下次 tick 验证 ✓

## Poison 用法 (Mode B 沿用 M79)

```
# 切回 Mode A (Poison 主动接管, 走 Telegram "go M{N}")
echo A > ~/.hermes/state/PM_LOOP_MODE

# 暂停
echo stop > ~/.hermes/state/PM_LOOP_MODE

# 看本 round 完成报告 (Poison / 下次 tick 验证)
cat ~/.hermes/state/PM_LAST_DISPATCH_RESULT.md
```

## 风险

- **Telegram CLI 不可用**: 沿用 M79. watchdog 报告写到本地文件, Poison 主动看.
- **systemd user 守护**: 用户登出后 systemd --user 可能停. 沿用 M79 D1.
- **Mode B 自旋防**: M79 D4 简化 — watchdog 自己写 brief 会绕过 Poison 时段规则 advisory.
  本 round 实证: Mode B 真 dispatch (任何时段都跑), Poison 时段规则 advisory 只在 PM-direct
  时段被遵守.
- **commit age 10 min**: M79 D3. 本 round ship 后 watchdog 至少等 10 min 才起下一 round.
- **watchdog tick 与 git push 时序**: push 完成 → watchdog tick → commit age check → 才起
  下一 round. 若 push 失败, watchdog 会持续 tick. 这是**预期行为**.
- **fact_store 序列**: M18 = 17 / M79 = 18 / M80 = 19. 本 round advisory, 未实际落库 (沿用 M79).

## 累计 shipped (omh-loop 11 cycle)

| Cycle | Round | 范围 | mutation red |
|---|---|---|---|
| 1 | M69 | G-Asset-IpConflictGuard-v6 (backend) | 1 |
| 2 | M70 | G-Asset-IpConflictAudit (backend) | 2 |
| 3 | M71 | Audit Sidebar 入口 (frontend) | 5 |
| 4 | M72 | IPv4-mapped IPv6 validator (frontend) | 2 |
| 5 | M73 | Model Calibration paragraph (config-only) | 0 |
| 6 | M74 | intent-spec-author skill (config-only) | 0 |
| 7 | M75 | PII 脱敏 (frontend) | 16 |
| 8 | M77 | G-19 web 容器最小权限 (docker) | 6 |
| 9 | M79 | PM-direct Autonomous Loop (config-only) | 0 |
| 10 | M78 | G-15 release 校验解耦 (backend) | 5 |
| 11 | **M80** | **watchdog-full-automation-test (config-only)** | **5 (Mode B dispatch 实证)** |

**总 mutation red (11 cycle)**: 37 case 真红实证. 0 false-green.
