# M78 Graph Analysis — OMH Loop 第 10 cycle

## 双轨状态

### graphify

| 阶段 | 节点 | 边 | 异常 |
|---|---|---|---|
| M77 (loop cycle 8) | 7143+ | 14690+ | 0 |
| M78 (loop cycle 10) | 7143+ | 14690+ | 0 |

M78 = backend (config.go + test) + compose + docs, graphify 视图不变. 0 anomaly ✓.

### codegraph

M78 后端新增 `isPlaceholderToken` 函数, codegraph 节点小幅增加 (≤5).

## 累计 shipped (10 cycle)

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

**总 mutation red (10 cycle)**: 37 case 真红实证. 0 false-green.

## 边界

- OMH `cognitive_surrender: warning` 持续 — M78 不动 OMH config
- Poison stop gates (poison-stop-gates-v1) 沿用 — `echo stop > PM_LOOP_MODE` 即 freeze
- omh doctor 退 1 (pre-existing yaml module miss) — 与 M78 无关
- **Poison "auto 切换" 实证**: PM_LOOP_MODE=B 切换生效, 本 round 即 watchdog 模式 B 实证

## 下轮候选 (cycle 11)

- **M76** = G-14 多副本 compose (≤4h 留 future, 复杂) — 引入新键 `database.automigrate`, 需防 G-13 viper AllKeys 坑
- 候选可能再扩 (Poison "继续优化" 后 4+1 项)

PM-direct watchdog Mode B 已激活, 后续 round 由 watchdog 自主 dispatch + Poison 触发 (go/stop).
