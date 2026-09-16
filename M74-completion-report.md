# M74 Completion Report — intent-spec-author Skill 骨架升级

> **Loop cycle**: 6 of `itmanager-grit-2026q3`
> **Feat**: `c548014`
> **Intent**: `intent-M74.md` (in same commit)

## 摩擦

OMH 当前没有 `intent-spec-author` skill. 起新 round 时 PM-direct 决策是一句话
(e.g. "起 M75 = PII 脱敏 (≤2h frontend)"), 但 spec 是 8 节 executable. 没有专用 skill
route, 走 omh-plan 太宽 (generic planning), 走手动写又无规范保证.

## 改动 (config-only, ≤2h)

新建 `~/.omh/skills/planner/intent-spec-author/SKILL.md` (7KB, 9 节):

| Section | 内容 |
|---|---|
| Frontmatter | name / description / metadata.hermes { tags, see_also, category, phase, role, quality_tier } |
| Why This Exists | 解释 intent-spec-author 与 omh-plan 的边界 |
| Do Not Use When | generic planning / decision trade-off / no round ID / multi-round → route 走别处 |
| Examples | Good: "起 M75 = PII 脱敏" / Bad: "Q4 产品方向" / Bad: "A/B/C 选哪个" |
| Completion Checklist | round ID + scope + budget + 8 节齐 + 可验证 acceptance + decision gate 区分 |
| Recovery Notes | 缺 round ID / vague acceptance / amend 已有 → 路径 |
| Workflow Lane | Intent -> plan -> execute 链路 |
| Use When | routing trigger signals (intent, M{N}-intent, round spec, 起 M{N}) |
| 8-Section Skeleton | 钉 8 节: Goal / Non-goals / Assumptions / Acceptance / Verification / Risks / Plan / Decision gate |
| Catalog Metadata | category / phase / role / quality_tier |

## **不改**

- `omh-plan/SKILL.md` body (避 omh install --force 覆盖) — 仅在 see_also cross-link
- `setup-profile.json` / `display.skin` / `interface` / `runtime/state.json` (Poison 没要求, M67 standing rule 沿用)
- `SOUL.md` / `config.yaml` (HERMES 一侧不动)

## Verify

- `python3 yaml.safe_load` parse frontmatter ✓ (name / description / metadata.hermes 全部识别)
- 9 ## sections (Why / Do Not Use / Examples / Completion / Recovery / Workflow / Use When / 8-Section / Catalog) ✓
- size 7KB ✓
- `omh doctor` 退 1 (pre-existing yaml module miss, 与 M74 无关)

## 决策点

- **D1**: 新建独立 skill, 不 extend omh-plan — 避免 omh install --force 覆盖 ✓
- **D2**: 8 节骨架 = omh-plan 实证有效骨架 — 复用 M47-M73 27 round intent 实证 ✓
- **D3**: 不动 setup-profile.json — Poison 没要求, 不擅自修改 ✓
- **D4**: intent-spec-author 是窄入口, 与 omh-plan 共存 — Hermes 自选 routing ✓
- **D5**: 见也加 model-calibration + omh-plan (双向 cross-link) — 让 routing 能找到关系 ✓

## 风险

- **OMH routing overlap**: intent-spec-author 与 omh-plan 描述相似, Hermes 可能误 route. 缓解: description 用更窄 trigger
- **OMH skill 不自动 reload**: 写完新 SKILL.md, 当前会话不会立即读到 (下次 OMH doctor 扫描时纳入)
- **不动 omh-plan body**: 新建独立 skill, 避免覆盖风险

## mutation inversion

不适用 (config-only, no code logic to mutate).
