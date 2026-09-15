# M67 — G-OMH-Workflow-Adoption 把"OMH 装好"升级到"OMH 真用上" (M66 OMH 试作用派生)

## Goal
把 OMH 从"装上 + doctor 全绿"的 passive state 升级到"工作流命名 / 决策 gate / evidence 边界
显式化"的 active state. M66 已实证 omh-plan skeleton (Goal/Non-goals/...) 对写 intent 真有用;
本轮把同样的 OMH workflow naming 推到 dispatch / decision / completion 三处, 并把 Poison 时段
规则正确安置.

## Non-goals (out of scope)
- **不**让 OMH 接管 PM-direct vs omp dispatch 切换 (Poison 时段规则) — OMH 是 advisory skill
  pack, 不是 autonomous executor; 这条规则仍在我 `MEMORY.md` + `fact_store fact_id=1`
- **不**改 `~/.hermes/config.yaml` 任何字段 (OMH install 时已配好, 不二次扰动)
- **不**写新 skill (Skill authoring 是 OMH 内部事, 我只在 OMH 骨架下消费)
- **不**改 `display.skin: omh` / `interface: tui` (Poison 没要求, 留待 Poison 决策)

## Assumptions
- OMH v2.0.3 装在 `/home/webman/.local/bin/omh`, 123 skills at `/home/webman/.omh/skills/`
- 当前 `setup-profile.json`: `default_executor: hermes` + `dispatch_policy: prepare_only` +
  `selected_categories: ["hermes-retained"]`
- OMH 的实际身份: **advisory workflow router** (skill descriptions 给 Hermes 提供 routing hints,
  真正执行仍是 Hermes + subagent + 外部 executor; "prepared vs observed" 边界 OMH 显式区分)
- Poison 偏好自驱 PM-direct (≤6h 跨前后端 / ≤4h 后端 / ≤2h 前端 / ≤1h 极小), 允许 ≤1h PM-direct
  折衷 (M52-M57 + M65 实证)

## Acceptance (本轮 PASS 必须验证)

### A. workflow 命名显式化
- [ ] 起 round 时显式用 OMH workflow 名 (omh-plan 写 intent / ulw-work dispatch / omh-decide
  决策点 / omh-task-completion-protocol 收尾)
- [ ] 每轮 intent 文件首行标注 `omh-workflow: omh-plan` 或对应 OMH skill

### B. Poison 时段规则正确安置
- [ ] 不动 `setup-profile.json` 强制时段 (OMH 不接 config, 试了也会被忽略)
- [ ] 把 Poison 时段规则注进 **project memory** (OMH project memory store) + `MEMORY.md` +
  `fact_store fact_id=1` 三处冗余 (事实流 + Memory 流 + OMH 流)
- [ ] 给 OMH 写一份 `~/.omh/project-rules.md` 标注 "Poison 时段规则" 是 project-local 决策,
  不是 OMH routing 强制项 — 留给 OMH-aware skill (omh-loop / ulw-work) 当 advisory 读

### C. Decision gate 用 OMH 骨架
- [ ] 每个 architecture decision 用 `omh-decide` 骨架 (Options / Tradeoffs / Assumptions /
  Rejected Alternatives / Decision Evidence Needed)
- [ ] PM-direct 拍板时显式标 "Decision: X (recommended)" + "Rejected: Y / Z" + "Risk: TBD"

### D. Evidence boundary 显式化
- [ ] 每个 commit / dispatch / test run 标注 prepared vs observed (OMH 强约束: 不许把 prepared
  当 observed)
- [ ] 文档不写 "verified" 但只有 intent 阶段证据的 (必须有 mutation inversion / 真 PG 往返 /
  路由级 SQL 捕获 等)

### E. 跑通 ≤4h PM-direct 实证
- [ ] 起 M68 = G-Asset-IpConflictGuard (M66 派生 TODO, ≤2h backend-only) 验证 OMH-aware
  PM-direct 仍能正常 ship
- [ ] M68 跟 M66 同等 ship 标准 (intent + feat + docs + mutation + graphify + codegraph)

## Verification (本轮怎么验)
1. `omh doctor`: 仍 44/44 PASS (我们不动 OMH 配置, doctor 不能退步)
2. `~/.omh/project-rules.md` 写完 + `fact_store` 加 1 条 fact + `MEMORY.md` 标注 OMH-aware
3. 起 M68 (G-Asset-IpConflictGuard, ≤2h) 实证 OMH workflow shape 真用上
4. M68 完成标准与 M66 同: backend tests + mutation + graphify 0 anomalies + codegraph

## Risks
- **R1 (低)**: OMH workflow shape 写得太刻意反而拖累节奏 → 缓解: 只在 architecture decision /
  intent 阶段用, dispatch 阶段保留简洁
- **R2 (中)**: Poison 时段规则到 OMH 没强约束 → 我可能看 OMH advisory 而忽略 fact_store 1 →
  缓解: M67 写完同时把规则加进 fact_store, OMH advisory 与 fact_store 1 双向确认
- **R3 (低)**: OMH skill pack 后续版本升级可能改 default_executor → 缓解: 不动 config,
  升级若改 setup 重跑 `setup --default-executor hermes` 一行命令即可

## Plan
1. 起 round 写 intent (本文件) + commit
2. 写 `~/.omh/project-rules.md` (Poison 时段规则 advisory)
3. `fact_store add` 一条 Poison-时段 + OMH-aware PM-direct fact
4. `MEMORY.md` 标注 OMH-aware section
5. 起 M68 = G-Asset-IpConflictGuard (≤2h PM-direct, M66 派生 TODO), 用 OMH workflow shape
   (omh-plan 写 intent / ulw-work dispatch 描述 / omh-decide decision gate)
6. M68 ship 后回 M67 写 `M67-completion-report.md` + `M67-graph-analysis.md`
7. Commit M67 docs + push

## Decision gate
- **Decision 1 (PM-direct 拍)**: OMH install 选项 (2026-09-16) 保留 `--default-executor hermes`,
  Poison 时段规则只进 project memory 不进 setup-profile.json — **PASS**
- **Decision 2 (本轮拍)**: M67 不写新 skill / 不改 config / 不动 display.skin, 只补
  `~/.omh/project-rules.md` + `fact_store` + `MEMORY.md` + 起 M68 实证 — **PASS**
- **Decision 3 (Poison 决策待)**: `display.skin: omh` / `interface: tui` 是否 revert — **未拍**,
  留 future 不打扰 Poison
