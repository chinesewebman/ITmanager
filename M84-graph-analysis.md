# M84 Graph Analysis — OMH Loop 第 13 cycle

## 双轨状态

### graphify

| 阶段 | 节点 | 边 | 异常 |
|---|---|---|---|
| M80 (loop cycle 11) | 7143+ | 14690+ | 0 |
| M84 (loop cycle 13) | 7143+ | 14690+ | 0 |

M84 = config-only + script 升级 (不在 ITmanager repo 内), graphify 视图不变. 0 anomaly ✓.

### codegraph

M84 加 `pm-loop-derive-candidates.py` (110 行, OUTSIDE repo at `~/.hermes/scripts/`),
codegraph 节点不变 (ITmanager 仓库视图). watchdog `pm-loop-watchdog.sh` 在 `~/.hermes` 不被 codegraph 索引.

**codegraph 自用**: M84 derive 调 `codegraph query` 11 次, 6 次命中 (55%). 实证 codegraph 健康可查.

## 累计 shipped (13 cycle)

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
| 11 | M80 | watchdog-full-automation-test (config-only) | 6 |
| 12 | M81+M82+M83 | G-41 / G-39 / CI 升级 (auto-shipped by watchdog) | TBD |
| 13 | M84 | watchdog 自举 (codegraph + graphify) | 6 |

**总 mutation red (13 cycle)**: 49 case 真红实证. 0 false-green.

## 边界

- OMH `cognitive_surrender: warning` 持续 — M84 不动 OMH config
- Poison stop gates (poison-stop-gates-v1) 沿用
- `~/.hermes/scripts/pm-loop-watchdog.sh` 仍是 M79 ship 版本 + M80/M84 incremental patches
- 不动 sing-box / keyring / setup-profile.json (M67 standing rule)

## 下轮候选 (cycle 14)

M85-M95 (11 真候选 auto-derived). watchdog 下次 tick 自动 dispatch 最早 ≤4h 的一个.
