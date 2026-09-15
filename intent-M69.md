# M69 — OMH ulw-loop Activation + G-Asset-IpConflictGuard-v6 (Loop 第 1 cycle)

> **Loop**: `itmanager-grit-2026q3` (2026-09-15T23:35:04Z 启)
> **Permission**: `execute_with_gates` (allowed: 11 / blocked: 3 external merge)
> **Sticky rule**: `poison-stop-gates-v1` (per-5-heartbeat restate)

## Goal

把 ITmanager 从 M68 ship 状态推到 production-ready, 闭合 M62-M68 派生的 4 条 TODO + OMH 启发 4 项 + 持续 ship cadence.

## Non-goals

- 不动 OMH config (`setup-profile.json` / `display.skin` / `interface`)
- 不动 sing-box / proxy / keyring
- 不动 ITmanager 之外的工作目录
- 不 bypass Poison stop gates

## Assumptions

- Poison 2026-09-12 standing rule: "不要什么都来问我" + "全权负责这个项目的推进"
- Poison 2026-09-14 reinforcement: "继续优化" + "graphcode 和 graphify 辅助分析" + "安排 omp 去干活"
- Poison 2026-09-16 verbatim: "好的，都同意" — loop boundary + stop gates + north-star 全 PASS
- OMH v2.0.3 已装 (44/44 doctor PASS), advisory routing guidance layer (skill pack not autonomous executor)
- 累计 22 round ship (M47-M68), 派生 TODO 剩 4 条 + OMH 启发 4 项
- 当前时段: 周二 23:35 CST = 已超 18:00 omp 窗口 (工作日 18:00→次日 09:00 omp), 现在应 PM-direct 起手 (loop 启动本身是 PM-direct 工作, 不调用 omp)

## Acceptance criteria (observed completion = loop success criterion LC001)

每个 round 都满足:
1. omh-plan 8 节 intent 写 + push (intent commit)
2. impl + 真测 (mutation inversion red → restore green)
3. graphify 0 anomalies + codegraph diff
4. fact_store fact_id 真写 (truth stream)
5. CHANGELOG + TODO + completion + graph analysis docs commit + push
6. PM_QUEUE shipped count +1

## Verification (per round)

- inner_loop_checks: `go build ./...` 0 err / `go test -count=1 ./...` 全绿 / `tsc --noEmit` 0 err / targeted vitest 全 PASS
- outer_loop_checks: graphify 0 anomalies / codegraph query 命中预期节点 / mutation inversion red 实证

## Risks

- **cognitive_surrender**: OMH 自带 warning, "loop can prepare broad actions; refresh human-owned judgment" — 我通过 fact_store truth stream + sticky rule per-5-heartbeat restate 防自旋
- **verification_gap**: 每 round 必 mutation inversion red 实证, 防 false-green
- **comprehension_debt**: 每 round 必 completion report 写, 不堆 prepared-not-observed
- **≤30 round 边界**: OMH loop 是 advisory, 30 round 后必重新授权才能续 (Poison "revert" gate)

## Plan

**M69 = Loop 第 1 cycle = G-Asset-IpConflictGuard-v6**:
- G-Asset-IpConflictGuard-v6 是 M68 派生: 跨资产同 IPv6 守卫 (M68 只查 v4, v6 没查; 业务上 v6 也可能冲突, 同 IP 守卫应覆盖)
- 范围 ≤2h, PM-direct backend-only
- 复用 M68 的 ErrIPConflict sentinel + 同 SELECT pattern (按 ipv6_address)

**M70 (loop 第 2 cycle) = G-Asset-IpConflictAudit**:
- 历史数据巡检 — 找现存跨资产同 IP 数据, 报告出来不自动修 (Poison 拍)

**M71 (loop 第 3 cycle) = Audit sidebar 入口** (≤1h frontend, T-73 派生)

**M72 (loop 第 4 cycle) = G-UI-AssetIpValidatorParity-Mapped** (M65 派生, ≤2h frontend)

**M73 (loop 第 5 cycle) = OMH model calibration paragraph** (≤2h, minimax + deepseek 各 1 段)

**M74 (loop 第 6 cycle) = intent-spec-author skill 骨架升级** (≤2h)

(剩余候选清单: 同 IP v6 守卫 / 历史巡检 / Audit sidebar / IP validator mapped / OMH model calibration / skill 升级 — 6 round 即收)

## Decision gate

**Loop activation 决策 (本 round 拍)**:
- D1: 立即起 M69 = G-Asset-IpConflictGuard-v6 (PM-direct ≤2h, v6 守卫复用 M68 SELECT pattern) → **PASS** (Poison "都同意" + M68 v4 实证)
- D2: 不接受 "loop 自动跑完全部 30 round" — 每 round 后 Poison 仍可触 "stop" / "revert", OMH 认知 warning cognitive_surrender 持续 require human-owned judgment refresh → **PASS** (Poison 4 stop gates 已拍)
- D3: Loop advisory 不接管 PM-direct cadence, 仍 PM-direct 自起 + omp dispatch 时段规则 → **PASS** (Poison 时段规则 verbatim verbatim 不变)
- D4: fact_store + sticky rule 双锁 stop gates → **PASS** (cognitive_surrender 应对)

## Stop conditions (Poison 触发)

1. "stop" → 立即 freeze loop + 等指令
2. "revert" → 走 M67 回退路径 (`omh loop stop` + 删 `~/.omh/project-rules.md` 关联 + 删 `~/.omh/decisions/omh-workflow.md`)
3. 连续 3 round 无 observed completion (commit + 真测 + fact_store fact_id 真写) → 自动 pause + 报告 Poison
4. 重要决策请示: 架构选型 / wire 破坏性 / reset / 不熟 e2e 工具首次 / 动 OMH config/sing-box/keyring/external merge
