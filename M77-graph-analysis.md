# M77 Graph Analysis — OMH Loop 第 8 cycle

## 双轨状态

### graphify

| 阶段 | 节点 | 边 | 异常 |
|---|---|---|---|
| M75 (loop cycle 7) | 7143+ | 14690+ | 0 |
| M77 (loop cycle 8) | 7143+ | 14690+ | 0 |

M77 = docker + docs, 不动 ITmanager 项目代码, graphify 视图不变. 0 anomaly ✓.

### codegraph

M77 不动 Go / TS 代码, codegraph 节点不变.

## 累计 shipped (8 cycle)

| Cycle | Round | 范围 | mutation red |
|---|---|---|---|
| 1 | M69 | G-Asset-IpConflictGuard-v6 (backend) | 1 |
| 2 | M70 | G-Asset-IpConflictAudit (backend) | 2 |
| 3 | M71 | Audit Sidebar 入口 (frontend) | 5 |
| 4 | M72 | IPv4-mapped IPv6 validator (frontend) | 2 |
| 5 | M73 | Model Calibration paragraph (config-only) | 0 |
| 6 | M74 | intent-spec-author skill (config-only) | 0 |
| 7 | M75 | PII 脱敏 (frontend) | 12 + 4 = 16 |
| 8 | M77 | G-19 web 容器最小权限 (docker) | 6 (compose) |

**总 mutation red (8 cycle)**: 32 case 真红实证. 0 false-green.

## 边界

- OMH `cognitive_surrender: warning` 持续 — M77 用 docker 改动 + assertion 钉死, 不动 OMH config
- Poison stop gates (poison-stop-gates-v1) 未触发 — M77 docker-only, 不动 keyring / sing-box / OMH config
- omh doctor 退 1 (pre-existing yaml module miss) — 与 M77 无关
- **docker 引擎未跑**: PM-direct sandbox 验证仅 `docker compose config` + pytest assertion. 实际跑通 (`docker compose up`) 留给下次 smoke

## 下轮候选 (cycle 9)

- **M78** = G-15 release 校验解耦 (≤2h backend) — `integrations.netbox.token` / `glpi.*_token` 改 URL 配置了才校验, compose 默认翻 release
- M76 = G-14 多副本 compose (≤4h 留 future, 复杂)
- 候选可能再扩 (Poison "继续优化" 后 4+1 项)

PM-direct 自起 ≤1h 极小 round 折衷沿用.
