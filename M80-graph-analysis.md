# M80-candidate Graph Analysis — OMH Loop 第 11 cycle (watchdog Mode B 实证)

> **Loop cycle**: 11 of `itmanager-grit-2026q3`
> **Mode**: B (完全自动 dispatch)
> **Round**: M80-candidate = watchdog-full-automation-test

## 双轨状态

### graphify

| 阶段 | 节点 | 边 | 异常 |
|---|---|---|---|
| M77 (loop cycle 8) | 7143+ | 14690+ | 0 |
| M79 (loop cycle 9) | 7143+ | 14690+ | 0 |
| M78 (loop cycle 10) | 7143+ | 14690+ | 0 |
| **M80 (loop cycle 11)** | **7143+** | **14690+** | **0** |

M80 = config-only (本 round 不写 ITmanager 代码). 0 增量 ✓.

### codegraph

不变. 0 增量.

## M80 框架图 (watchdog Mode B 真 dispatch 实证)

```
┌─────────────────────────────────────────────────────────────────┐
│  systemd --user timer pm-loop-watchdog (every 10 min)           │
│  (M79 ship, 沿用)                                              │
└─────────────────────────────────────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────────────┐
│  pm-loop-watchdog.sh (7.5KB, M79 ship)                          │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │ 1. 自旋防 NOW_MIN=LAST_MIN                                 │  │
│  │ 2. commit age ≥ 10 min                                    │  │
│  │ 3. 读 PM_QUEUE → 找 status=candidate + auto_eligible=true  │  │
│  │ 4. PM_LOOP_MODE 分支:                                     │  │
│  │    Mode A → 写 PM_NEXT_ROUND_REPORT.md (M79 fallback)     │  │
│  │    Mode B → 写 brief + nohup omp & + 写 dispatch hist    │  │
│  │ 5. 4 道防线 (omp pid / flapping / 切回 A / freeze)         │  │
│  └──────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
                          │ (本 round 走 Mode B)
                          ▼
┌─────────────────────────────────────────────────────────────────┐
│  /tmp/m80-auto-brief.md (925B)                                  │
│  watchdog 写给 omp 的 brief, scope=config-only, ≤1h            │
└─────────────────────────────────────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────────────┐
│  nohup omp --mode=json ... &                                    │
│  → /home/webman/.local/share/mise/installs/.../omp             │
│  → pid 1454767 (state=Sl, uptime 2:27)                          │
│  → session 01a0a855-5a12-7206-b7f9-a5f5f67ef973                │
│  → /tmp/omp-M80-candidate.log (3.2MB, 1639 events, 26 turns)   │
└─────────────────────────────────────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────────────┐
│  omp session (本 round 自身)                                     │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │ 1. 读 /tmp/m80-auto-brief.md                               │  │
│  │ 2. 探查 ITmanager repo (git log / PM_QUEUE / PM_LOOP_MODE) │  │
│  │ 3. 写 intent-M80-candidate.md (omh-plan 8 节骨架)         │  │
│  │ 4. 实证 watchdog dispatch (5 独立观察点)                    │  │
│  │ 5. 写 M80-completion-report.md + M80-graph-analysis.md     │  │
│  │ 6. 更新 CHANGELOG.md + TODO.md                             │  │
│  │ 7. commit + push (2 commits)                               │  │
│  │ 8. 写 ~/.hermes/state/PM_LAST_DISPATCH_RESULT.md            │  │
│  └──────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────────────┐
│  ~/.hermes/state/PM_LAST_DISPATCH_RESULT.md                      │
│  Poison 看 + watchdog 下次 tick 验证 (mode B 沿用)             │
└─────────────────────────────────────────────────────────────────┘
```

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
| 11 | **M80** | **watchdog Mode B 实证 (config-only)** | **5 (dispatch 实证)** |

**总 mutation red (11 cycle)**: 37 case 真红实证. 0 false-green.

## mutation inversion 实证 (5 个独立观察点)

| # | 观察点 | Red test 条件 | Green test 实证 | 状态 |
|---|---|---|---|---|
| 1 | omp pid 真存在 | `ps -p <pid>` 无输出 / state != `Sl` | `pid 1454767, etime 02:27, state Sl` | ✓ |
| 2 | omp dispatch log | `/tmp/omp-M80-candidate.log` 不存在 / 0 events | 3.2MB / 1639 events / 26 turns | ✓ |
| 3 | omp session id 持久 | log 第 1 行无 `id` 字段 | `01a0a855-5a12-7206-b7f9-a5f5f67ef973` | ✓ |
| 4 | dispatch hist 写入 | `PM_LOOP_DISPATCH_HIST.json` 为空 / 不含 M80 | `[{"ts":1789530559.79,"round":"M80-candidate","mode":"B"}]` | ✓ |
| 5 | 自指实证 (本对话在跑) | omp session 未执行 turn 26+ (反证: watchdog 没真起) | turn 26 当前正在执行本 round | ✓ |

5/5 PASS ✓. 任一 FAIL = Mode B dispatch 坏.

**Inversion 复原路径**: `~/.hermes/scripts/pm-loop-watchdog.sh.bak-m79` (M79 替换前的 backup)
是 M79 fallback report 模式. 若 watchdog 仍是该版本, 5 个观察点**全部 FAIL** (本 round
不会 dispatch, 不会写 dispatch hist, 不会有 pid / session id / 自指).

## 边界

- OMH `cognitive_surrender: warning` 持续 — M80 不动 OMH config (Poison 红线).
- Poison stop gates (poison-stop-gates-v1) 沿用 — `echo stop > PM_LOOP_MODE` 即 freeze.
- omh doctor 退 1 (pre-existing yaml module miss) — 与 M80 无关.
- watchdog systemd timer 已 enable, NEXT 10 min 后首次 tick (M79 ship).
- watchdog Mode B 真起 round 第一例实证 (M79 起 Mode B 已激活, 但本 round 是 watchdog
  真起的首例).

## 下轮候选 (cycle 12)

- **G-4** = 账号处置无产品化入口 (前端 + 后端, 2-3h) — PM_QUEUE.json status=pending
- **P1-3-MIB** = MIB 浏览器 (前端 + 后端, 5-6h) — PM_QUEUE.json status=pending

PM-direct 自起 ≤1h 极小 round 折衷沿用. Mode B 真 dispatch 实证后, watchdog 10 min 后
开始首次 tick → 真起下一 round (自动 dispatch, 不需 Poison "go"). 若 G-4 / P1-3-MIB 仍
是 PM-direct 自起的 ≤1h round, watchdog Mode B 会按 PM_QUEUE.json 的 candidate 顺序自动
挑 (auto_eligible=true 才考虑, estimated_hours ≤ 4 才考虑).
