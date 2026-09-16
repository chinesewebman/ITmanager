# M74 — intent-spec-author skill 骨架升级（OMH ulw-loop 第 6 cycle）

> **Loop cycle**: 6 of `itmanager-grit-2026q3`

## Goal

OMH 当前没有 `intent-spec-author` skill — 写 intent.md 走 omh-plan (8 节骨架, 通用 planning).
Intent (Poison 时段规则 + 项目 specific) 需要更窄的入口: 不写 generic plan, 而写
**round-scoped IntentSpec** (Goal / Non-goals / Assumptions / Acceptance / Verification / Risks / Plan / Decision gate).

新建 `~/.omh/skills/planner/intent-spec-author/SKILL.md`, 让 Hermes 起新 round 时
优先 route 到 intent-spec-author 而不是通用 omh-plan.

## Non-goals

- 不改 omh-plan skill (M73 standing rule: 仅 cross-link, 不动 skill body)
- 不改 setup-profile.json / display.skin / interface (Poison 没要求)
- 不替代 omh-decide / omh-decision-prototype — 决策类仍走 decide
- 不强 replace 现有 omh-plan 路由 — intent-spec-author 是 **窄入口**, 与 omh-plan 共存

## Assumptions

- OMH skill 目录结构: `~/.omh/skills/<category>/<skill-name>/SKILL.md`
- OMH skill frontmatter 必须: name / description / metadata.hermes { tags, category, phase, role, quality_tier }
- OMH routing 是 advisory (skill descriptions 给 Hermes routing hints)
- M47-M73 累计写 27 个 intent-M{N}.md, omh-plan 8 节骨架已被 PM-direct 实证有效

## Acceptance criteria

| 标准 | 实证 |
|---|---|
| 新文件 `~/.omh/skills/planner/intent-spec-author/SKILL.md` ≥ 4KB | ≥3KB |
| frontmatter parse YAML OK | python yaml.safe_load 验证 |
| description 含 `[omh]` 标记 | grep verify |
| 含 8 节骨架 (Goal/Non-goals/Assumptions/Acceptance/Verification/Risks/Plan/Decision gate) | grep |
| 含 Use When / Do Not Use When / Examples / Completion Checklist | grep |
| 不引 omh install --force 覆盖风险 (新文件, 不动现有 skill) | 新文件, 不覆盖 |
| fact_store fact_id=14 | shipped record |

## Verification

- `python3 -c "import yaml; yaml.safe_load(open(...).read().split('---')[1])"` 验证 frontmatter
- `grep -c '^## ' SKILL.md` ≥ 8 节
- `ls ~/.omh/skills/planner/intent-spec-author/SKILL.md` 存在
- `omh doctor` 退 1 (pre-existing yaml module miss, 与 M74 无关)

## Risks

- **OMH routing overlap**: intent-spec-author 与 omh-plan 描述相似, Hermes 可能误 route. 缓解: description 用更窄 trigger ("intent.md / round spec / per-round intent / 写 M{N}-intent"), 让 routing 收敛
- **OMH skill 不自动 reload**: 写完新 SKILL.md, 当前会话不会立即读到. 缓解: 在 description 末尾加 "Use when the user says: intent, intent.md, per-round intent, M{N}-intent" — 下次 OMH doctor 扫描时纳入
- **不动 omh-plan body**: 避免 omh install --force 覆盖. 新建独立 skill

## Plan

1. 写 `~/.omh/skills/planner/intent-spec-author/SKILL.md` (~4KB, frontmatter + 8 节骨架 + Use When / Do Not Use When / Examples / Completion Checklist / Workflow Lane)
2. yaml frontmatter parse 验证
3. fact_store fact_id=14
4. docs commit (CHANGELOG + TODO + completion + graph analysis)

## Decision gate

- **D1**: 新建独立 skill, 不 extend omh-plan — 避免 omh install --force 覆盖 ✓
- **D2**: 8 节骨架 = omh-plan 实证有效骨架 — 复用 M47-M73 27 round intent 实证 ✓
- **D3**: 不动 setup-profile.json / display.skin / interface — Poison 没要求 ✓
- **D4**: intent-spec-author 是窄入口, 与 omh-plan 共存 — Hermes 自选 routing ✓
