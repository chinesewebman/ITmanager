# M73 Completion Report — OMH Model Calibration Paragraph

> **Loop cycle**: 5 of `itmanager-grit-2026q3`
> **Feat**: `d4bbdb6`
> **Intent**: `intent-M73.md` (in same commit)

## 摩擦

Poison 默认两个 LLM provider: **minimax** (Hermes 主用, 当前会话) + **deepseek** (omp dispatch 时段用).

两个模型的工具调用习惯与默认输出 shape 不同:
- minimax: 短答立即动手, 不需要 preamble, 不需要 "我可以继续吗" 反复确认
- deepseek: 长 plan + 详细 brief, 能产多文件 / 多 commit 大 round

写 spec/intent/brief 时若按一个模型自然形状写, 另一个模型可能不识别或拒绝. 没有文档钉死, 每次 round 都会"按当前在用的模型重置口径", 浪费 PM-direct cadence.

## 改动 (config-only, ≤2h)

| 文件 | 改动 |
|---|---|
| `~/.omh/decisions/model-calibration.md` | **新建** 3KB doc, 4 节 (背景 / minimax / deepseek / 时段规则) |
| `~/.omh/project-rules.md` | 加 1 行 reference (新节"相关文档") |
| `~/.omh/skills/planner/omh-plan/SKILL.md` | frontmatter 加 `see_also: ["~/.omh/decisions/model-calibration.md"]` |
| `~/.omh/skills/operator/omh-decide/SKILL.md` | 同上 |

## **不改**

- skill 内容 (`omh-plan` / `omh-decide` 仅 frontmatter cross-link, 避免 omh install --force 覆盖)
- `setup-profile.json` / `display.skin` / `interface` / `runtime/state.json` / `routing/model-chains.json` (Poison 没要求, M67 standing rule 沿用)
- `SOUL.md` / `config.yaml` (HERMES 一侧不动)
- `~/.hermes/state/` PM_QUEUE / fact_store fact_id=1 (时段规则不变)

## Verify

- `python3 -c "import yaml; ..."` 验证两个 SKILL.md frontmatter 仍 parse ✓ (tags + see_also + category 各自识别正确)
- `ls ~/.omh/decisions/model-calibration.md` 存在 ✓
- `omh doctor` 退 1 (pre-existing yaml module miss, 与 M73 无关)

## 决策点

- **D1**: 走 decisions/ 目录 (project memory), 不动 SOUL.md / config.yaml — M67 standing rule 沿用 ✓
- **D2**: 仅 cross-link (frontmatter see_also), 不动 skill 内容 — 避免 omh install --force 覆盖 ✓
- **D3**: 不动 setup-profile.json — Poison 没要求, 不擅自修改 ✓
- **D4**: 不动 fact_store fact_id=1 (时段规则), 加新 fact_id=13 (calibration) — 长尾归 fact_store, memory 不动 ✓

## 风险

- **YAML 解析**: `see_also: ["..."]` flow style 不带 trailing comma (发现并已 fix)
- **OMH skill frontmatter 改**: omh install --force 会覆盖 skill 内容, 但**不改 skill body**, 仅加 frontmatter field; 即使覆盖 see_also 字段, project-rules.md 与 decisions/ 不被触碰
- **不动 setup-profile.json**: M67 standing rule 沿用, 不擅自修改

## mutation inversion

不适用 (config-only, no code logic to mutate).
