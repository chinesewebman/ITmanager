---
id: INTENT-M35-R2
title: vCenter VM 纳管 — ITmanager 从 NetBox 读 VM 资产 + 孤儿 dry-run runbook
status: draft
author: hermes@local
created: 2026-09-12
outcomes:
  - 02-资产管理.md §2.8 新增「vCenter VM 纳管」段落（ITmanager 从 NetBox 读 VM 数据的字段对照 + 渲染规则）
  - 03-监控采集.md §3.X 新增「vCenter 孤儿 VM 处理 runbook」（dry-run + 人工确认窗口）
  - docs/adr/0008-r3-vcenter-via-netbox.md ADR 收口「ITmanager 不直连 vCenter、走 NetBox-sync、单一 SoT」
  - docs/FIX-PLAN-R3-VCENTER-VIA-NETOBOX.md 需求/计划/验收/Risk
  - docs/IMPL-R3-VCENTER-VIA-NETOBOX.md 实施细节
  - CHANGELOG M35-R2 段
  - M35-R2-completion-report.md (5-section)
acceptance:
  - id: AC-R3-1
    given: v3 §3 R3 已 spec 但 ITmanager 0 行 vCenter 代码
    when: 02-资产管理.md §2.8 写好
    then: 段落含字段对照表 (NetBox VirtualMachine → ITmanager asset view) + 渲染规则 + 与 R2 (line) 的协调
  - id: AC-R3-2
    given: v3 §3 R3 Risk 条款明文「先 dry-run 一周 + 人工确认窗口」
    when: 03-监控采集.md §3.X 写好
    then: 含 dry-run 启用步骤 + 周观察期 checklist + 人工确认窗口流程
  - id: AC-R3-3
    given: ADR-0008 必须收口 R3 三条决策
    when: read docs/adr/0008-r3-vcenter-via-netbox.md 决策节
    then: 含 3 硬约束: ITmanager 不直连 vCenter / 单一 SoT=NetBox / 孤儿删除走人工确认
  - id: AC-R3-4
    given: docs/FIX-PLAN-R3-VCENTER-VIA-NETOBOX.md + IMPL 同名 .md 存在
    when: 两文件 cross-link + 与 ADR-0008 互链
    then: cross-link grep PASS
  - id: AC-R3-5
    given: 02 + 03 + ADR-0008 + FIX-PLAN + IMPL 都已写
    when: CHANGELOG.md M35-R2 段追加
    then: 含 commits 列表 + v3 §7 A-6 / A-7 (若适用) 状态变化
edges:
  - v3 §3 R3 风险条款明文: VM 频繁增删产生大量孤儿对象 (快照/克隆/临时机) → 必须 dry-run + 人工确认
  - 02-资产管理.md §2.8 必须与 R2 (lines) 协调: asset view 同时包含 line + VM, 不能双写
  - R3 不在 13 章 audit (R5 范围内), 但 R3 文档必须不破坏 R5 已落的版本号一致
  - NetBox `NetBox-synced` 标签是 bb-Ricardo/netbox-sync 项目的约定, ITmanager 文档必须引用而非自定义
not_goals:
  - 实际部署 vCenter 或 NetBox
  - 实际写 ITmanager 读 NetBox VM 的 endpoint handler (那是 stage 2 实施)
  - 改 schema / migrations (R3 资产视图渲染走现有 assets 表 + NetBox sync 维护的 VirtualMachine 标签)
  - 自己实现 vCenter → NetBox sync (那是 bb-Ricardo/netbox-sync 项目的事)
  - 改 02-资产管理.md 既有 §2.1-§2.7 (R2 已落, 不动)
evidence:
  - "docs/v3-架构优化需求.md §3 R3 line 83-89 — R3 需求源头"
  - "02-资产管理.md 已有 §2.7 专线 (R2 已落) — R3 §2.8 紧接其后"
  - "03-监控采集.md 现有结构 — R3 §3.X 插入位置待定"
  - "docs/adr/0007-r1-holmesgpt-toolset-边界.md — ADR 模板先例"
  - "docs/FIX-PLAN-R2-NETBOX-CIRCUITS.md — R3 文档结构对标 R2"
---

# INTENT-M35-R2: vCenter VM 纳管

## Context

v3 §3 R3 决策：vCenter → NetBox sync 走社区版 `bb-Ricardo/netbox-sync`，ITmanager **不直连 vCenter**，**从 NetBox 读 VM 资产**。理由是 P-4（上千台 VM 零纳管）+ NetBox 是 SoT（VM 必须进同一张资产图）。

本轮是 docs-only stage 0（类似 M35-R1）—— 不写 ITmanager 读 NetBox VM 的 endpoint handler（那是阶段 2 实施），只把下列文档落定：

1. **02-资产管理.md §2.8** — ITmanager 资产视图如何呈现 NetBox 来的 VM（字段对照 + 渲染规则）
2. **03-监控采集.md §3.X** — vCenter 孤儿 VM 处理 runbook（dry-run + 人工确认窗口，对应 v3 R3 Risk 条款）
3. **ADR-0008** — 收口「ITmanager 不直连 vCenter / 单一 SoT / 孤儿删除走人工确认」3 条决策
4. **FIX-PLAN-R3** + **IMPL-R3** — 项目标准文档
5. **CHANGELOG** + **completion-report** — 收口

后续阶段（不在本轮）：
- 阶段 1：实施 dry-run（用 `bb-Ricardo/netbox-sync` 的 dry-run 模式跑一周）
- 阶段 2：实施 ITmanager 端读 NetBox VM 的 endpoint handler + asset view 渲染
- 阶段 3：周观察期结束后，启用 NetBox-synced 标签的孤儿识别 + 人工确认窗口

## Outcomes

See `outcomes[]` frontmatter.

## Acceptance Criteria

See `acceptance[]` frontmatter. 与 M35-R1 类似，本轮是 docs-only，所以 AC 偏 content-existence + cross-link 而非 behavior。

## Edge cases

- **R3 风险条款明文**：「VM 频繁增删产生大量孤儿对象（快照/克隆/临时机）→ 先 dry-run 跑一周，确认删除策略再开自动清理；删除前必须有人工确认窗口」—— 这是 v3 §R3 Risk 的硬约束，03 §3.X 必须显式落实，不能含糊。
- **与 R2 协调**：02 §2.8 资产视图同时含 line + VM，不能让运维人员感到「两套数据在 ITmanager」。需在 §2.8 顶部明确说明 R2+R3 共用同一张 asset view。
- **NetBox-synced 标签**：bb-Ricardo/netbox-sync 项目约定，ITmanager 不重新定义，只引用。
- **R5 已落的版本号一致**：R3 文档不能改 README/CHANGELOG 已稳定的版本号段。

## Operational constraints

- Do NOT touch: 02-资产管理.md §2.1-§2.7（R2 已落）、03-监控采集.md 既有结构、routes.go、openapi.yaml、schema.sql、migrations、backend code。
- Must preserve: ADR 编号递增到 0008。
- No code gates; PM verifies docs exist + content matches intent.

## Evidence

See `evidence[]` frontmatter.

## E2E verification

Per `verify-e2e`, this round is docs-only. Probe = reviewer reads 02 §2.8 + 03 §3.X + ADR-0008 + FIX-PLAN + IMPL, 看到 R3 的字段切分 + dry-run runbook + 3 条决策完整。

## Completion report

After dispatch: write `M35-R2-completion-report.md` per `task-completion-protocol` skill.
