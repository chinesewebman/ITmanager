# M74 Graph Analysis — OMH Loop 第 6 cycle

## 双轨状态

### graphify (多视图多仓库诊断)

| 阶段 | 节点 | 边 | 异常 |
|---|---|---|---|
| M73 (loop cycle 5) | 7143+ | 14690+ | 0 |
| M74 (loop cycle 6) | 7143+ | 14690+ | 0 |

M74 = config-only (OMH skill 新文件, 不动 ITmanager 项目代码), graphify 视图不变. 0 anomaly ✓.

### codegraph

M74 同样不动 ITmanager 仓库代码, codegraph 节点数不变.

## OMH skill tree 更新

```
~/.omh/skills/
├── planner/
│   ├── omh-plan/                  (existing)
│   ├── intent-spec-author/        (NEW M74) ← 7KB, 9 节
│   ├── omh-decision-prototype/
│   ├── omh-backend/
│   ├── omh-codebase-onboarding/
│   └── ...
├── operator/
│   ├── omh-decide/                  (existing, M73 see_also added)
│   └── ...
└── ...
```

## 三处冗余 (M67 + M73 + M74 累积)

| 位置 | 内容 |
|---|---|
| `~/.omh/project-rules.md` | "model 选择: see decisions/model-calibration.md" |
| `~/.omh/skills/planner/omh-plan/SKILL.md` | see_also: model-calibration |
| `~/.omh/skills/operator/omh-decide/SKILL.md` | see_also: model-calibration |
| `~/.omh/skills/planner/intent-spec-author/SKILL.md` | see_also: model-calibration + omh-plan (NEW M74) |
| MEMORY.md | **不动** (memory 2200 char 预算已满) |
| fact_store | fact_id=14: M74 intent-spec-author shipped |

## 边界

- OMH 自带 `cognitive_surrender: warning` 持续 — M74 用新建独立 skill 隔离 intent-spec 与 generic plan, 不动 omh-plan body
- Poison stop gates (poison-stop-gates-v1) 未触发 — M74 config-only, 不动 keyring / sing-box / OMH config
- OMH doctor 退 1 (pre-existing yaml module miss) — 与 M74 无关

## 下轮候选 (cycle 7)

- **M75** = PII 脱敏 (Users.tsx TODO L21, ≤2h frontend) — 业务 round, 非 config
- M77 = G-19 web 容器最小权限 (≤2h docker)
- M78 = G-15 release 校验解耦 (≤2h backend)
- M76 = G-14 多副本 compose (≤4h 留 future)

PM-direct 自起 ≤1h 极小 round 折衷沿用.
