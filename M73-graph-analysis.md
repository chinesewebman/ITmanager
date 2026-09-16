# M73 Graph Analysis — OMH Loop 第 5 cycle

## 双轨状态

### graphify (多视图多仓库诊断)

| 阶段 | 节点 | 边 | 异常 |
|---|---|---|---|
| M72 (loop cycle 4) | 7143+ | 14690+ | 0 |
| M73 (loop cycle 5) | 7143+ | 14690+ | 0 |

M73 = config-only (OMH decisions/ + project-rules.md + skill frontmatter), ITmanager 项目代码不动, graphify 视图不变. 0 anomaly ✓.

### codegraph (本地 index, `codegraph index .`)

M73 同样不动 ITmanager 仓库代码, codegraph 节点数不变.

### 三处冗余 (M67 standing rule)

| 位置 | 内容 |
|---|---|
| `~/.omh/project-rules.md` | "model 选择 / 输出 shape: see decisions/model-calibration.md" |
| `~/.omh/skills/planner/omh-plan/SKILL.md` | frontmatter `see_also: ["~/.omh/decisions/model-calibration.md"]` |
| `~/.omh/skills/operator/omh-decide/SKILL.md` | frontmatter `see_also: ["~/.omh/decisions/model-calibration.md"]` |
| MEMORY.md (this session) | **不动** (memory 2200 char 预算已满, calibration 长尾归 fact_store) |
| fact_store | fact_id=13: M73 model calibration paragraph shipped |

## 边界

- OMH 自带 `cognitive_surrender: warning` 持续 — M73 用 cross-link + decisions/ 隔离 model 选择, 不 inject OMH routing config, 沿用 M67 standing rule
- Poison stop gates (poison-stop-gates-v1) 未触发 — M73 config-only, 不动 keyring / sing-box / OMH config
- OMH doctor 退 1 (pre-existing yaml module miss) — 与 M73 无关, 不归本 round

## 下轮候选 (cycle 6)

- M74 = intent-spec-author skill 骨架升级 (≤2h PM-direct)
- M75 = PII 脱敏 (Users.tsx TODO L21, ≤2h frontend)
- M77 = G-19 web 容器最小权限 (≤2h docker)
- M78 = G-15 release 校验解耦 (≤2h backend)

PM-direct 自起 ≤1h 极小 round 折衷沿用.
