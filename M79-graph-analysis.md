# M79 Graph Analysis — OMH Loop 第 9 cycle

## 双轨状态

### graphify

| 阶段 | 节点 | 边 | 异常 |
|---|---|---|---|
| M77 (loop cycle 8) | 7143+ | 14690+ | 0 |
| M79 (loop cycle 9) | 7143+ | 14690+ | 0 |

M79 = config-only (watchdog + systemd + intent), ITmanager 项目代码不动. 0 anomaly ✓.

### codegraph

不变. 0 增量.

## M79 框架图

```
┌─────────────────────────────────────────────────────────┐
│  systemd --user timer pm-loop-watchdog (every 10 min)  │
└─────────────────────────────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────┐
│  pm-loop-watchdog.sh (6.5KB, bash + python helper)     │
│  ┌──────────────────────────────────────────────────┐  │
│  │ 1. 自旋防 NOW_MIN=LAST_MIN                        │  │
│  │ 2. commit age ≥ 10 min 才起下一 round             │  │
│  │ 3. 找 PM_QUEUE.json 中 status=candidate/pending   │  │
│  │ 4. 分支:                                          │  │
│  │    Mode A → 写 PM_NEXT_ROUND_REPORT.md            │  │
│  │    Mode B → inbox log "待 PM-direct 接 dispatch"  │  │
│  │ 5. 4 道防线 (omp pid / flapping / 切回 A / freeze)│  │
│  └──────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────┐
│  Poison 收到 (Telegram / 本地文件)                       │
│  - 模式 A: 回 "go M{N}" / "stop" / "auto"              │
│  - 模式 B: watchdog 自动跑, Poison 仅观察              │
└─────────────────────────────────────────────────────────┘
```

## 累计 shipped (9 cycle)

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

**总 mutation red (9 cycle)**: 32 case 真红实证. 0 false-green.

## 边界

- OMH `cognitive_surrender: warning` 持续 — M79 不动 OMH config
- Poison stop gates (poison-stop-gates-v1) 沿用 — `echo stop > PM_LOOP_MODE` 即 freeze
- omh doctor 退 1 (pre-existing yaml module miss) — 与 M79 无关
- watchdog systemd timer 已 enable, NEXT 10 min 后首次 tick

## 下轮候选 (cycle 10)

- **M78** = G-15 release 校验解耦 (≤2h backend) — `integrations.netbox.token` / `glpi.*_token` 改 URL 配置了才校验, compose 默认翻 release
- M76 = G-14 多副本 compose (≤4h 留 future, 复杂)
- 候选可能再扩 (Poison "继续优化" 后 4+1 项)

PM-direct 自起 ≤1h 极小 round 折衷沿用. Watchdog 10 min 后开始首次 tick.
