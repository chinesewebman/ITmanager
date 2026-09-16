# M73 — OMH model calibration paragraph（OMH ulw-loop 第 5 cycle）

> **Loop cycle**: 5 of `itmanager-grit-2026q3`

## Goal

钉两个 LLM provider (minimax + deepseek) 的默认输出 shape + 工具调用习惯 do/don't,
让 PM-direct / omp dispatch 时按当前实际跑的模型选 spec, 避免"按一个模型自然形状写,
另一个模型不识别"的往返漂移.

## Non-goals

- 不改 ~/.omh/setup-profile.json / display.skin / interface / runtime/state.json / routing/model-chains.json (Poison 没要求, M67 standing rule)
- 不改 OMH skill 内容 (omh-plan / omh-decide 仅 frontmatter 加 see_also 引用)
- 不改 SOUL.md / config.yaml (HERMES 一侧不动)
- 不 bypass poison-stop-gates-v1 sticky rule

## Assumptions

- Poison 偏好 keyring 存 LLM API key, 默认两个 provider:
  - **minimax** (Hermes 主用, 当前会话用此模型)
  - **deepseek** (omp dispatch 时段用, 工作日 18:00→次日 09:00 + 12:00-14:00 + 周末全天半价)
- Poison 2026-09-16 verbatim: "好吧，既然证明omh 好用，那就更好地使用 omh！" — OMH 工作流激活
- OMH 是 advisory skill pack (skill descriptions 给 Hermes routing hints, 不是 autonomous executor)
- 项目-rules.md 已 ship (M67), 三处冗余 advisory (project-rules.md / MEMORY.md / fact_store)

## Acceptance criteria

| 标准 | 实证 |
|---|---|
| 新文件 `~/.omh/decisions/model-calibration.md` 含 minimax + deepseek 各 1 段 | 3KB doc, 4 节 |
| project-rules.md 加 1 行 reference | "see decisions/model-calibration.md" |
| omh-plan/SKILL.md frontmatter 加 see_also | YAML parse 验证 ✓ |
| omh-decide/SKILL.md frontmatter 加 see_also | YAML parse 验证 ✓ |
| omh doctor 不引入新 regression | pre-existing yaml 模块缺失不变 |
| fact_store truth stream | fact_id=13 |

## Verification

- `python3 -c "import yaml; ..."` 验证两个 SKILL.md frontmatter 仍 parse
- `ls ~/.omh/decisions/model-calibration.md` 存在
- `omh doctor` 退 1 (pre-existing yaml miss, 不归 M73)

## Risks

- **YAML 解析**: `see_also: ["..."]` flow style 不带 trailing comma (已 fix)
- **OMH skill frontmatter 改**: omh install --force 会覆盖, 但项目-rules.md + decisions/ 不会被 omh install 触碰 (verified)
- **不动 setup-profile.json**: M67 standing rule 沿用
- **memory budget**: model calibration 细节不全压 MEMORY (keyring 节已满), 走 fact_store + decisions/

## Plan

1. 写 `~/.omh/decisions/model-calibration.md` (3KB, 4 节: 背景 / minimax / deepseek / 时段规则)
2. project-rules.md 加 1 行 reference
3. omh-plan + omh-decide skill frontmatter 加 see_also (不改 skill 内容)
4. fact_store fact_id=13
5. docs commit (CHANGELOG + TODO + completion + graph analysis)

## Decision gate

- **D1**: 不改 setup-profile.json / display.skin / interface (Poison 没要求) ✓
- **D2**: 走 decisions/ 目录 (project memory), 不动 SOUL.md / config.yaml ✓
- **D3**: 仅 cross-link (frontmatter see_also), 不动 skill 内容 (避免 omh install --force 覆盖) ✓
- **D4**: 不动 fact_store fact_id=1 (时段规则), 加新 fact_id=13 (calibration) ✓
