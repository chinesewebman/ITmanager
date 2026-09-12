---
id: INTENT-M35-R1
title: HolmesGPT toolset integration spec — ITmanager as data source
status: draft
author: hermes@local
created: 2026-09-12
outcomes:
  - 09-AI辅助.md R1 段落有完整 toolset 端点清单 + 路径校验注
  - docs/adr/0007-r1-holmesgpt-toolset-边界.md 存在并显式收口「写操作不暴露」「只读 token」「最小 scope」
  - docs/FIX-PLAN-R1-HOLMESGPT.md 含端点契约 + 边界清单 + 实施文档
  - docs/IMPL-R1-HOLMESGPT.md 把 FIX-PLAN 收尾
  - CHANGELOG M35-R1 段 + TODO G-新登记（如有）
  - M35-R1 completion report (5-section per task-completion-protocol)
acceptance:
  - id: AC-R1-1
    given: 09-AI辅助.md 已写好 v3 段落
    when: grep "diagnostics/assets/{id}/timeline" 09-AI辅助.md
    then: at least one match with verification note against backend/internal/api/routes.go line ref
  - id: AC-R1-2
    given: docs/adr/0007-r1-holmesgpt-toolset-边界.md exists
    when: read 决策 section
    then: contains 3 硬约束: 只读 token + 写操作不暴露 + audit_logs 必走
  - id: AC-R1-3
    given: docs/FIX-PLAN-R1-HOLMESGPT.md + IMPL-R1-HOLMESGPT.md exist
    when: both files exist on main branch
    then: PASS (文档存在即可；本轮不写 endpoint 代码)
  - id: AC-R1-4
    given: CHANGELOG.md
    when: grep "M35" CHANGELOG.md
    then: M35-R1 section exists with bullet list of commits
  - id: AC-R1-5
    given: M35-R1-completion-report.md exists at repo root
    when: read top of file
    then: 5 sections present: Delivered / Changed / Validation / Risk / Status=COMPLETE
edges:
  - v3 §R1 风险 mitigation 已写进 09-AI辅助.md §9.3,但 ADR-0007 之前没有 — 必须新建
  - HolmesGPT toolset 配置示例（§9.4）已给 YAML，但实际 HolmesGPT 版本可能要求不同字段（version drift）；不阻塞本文档 spec
  - 09-AI辅助.md §9.2 五个端点的路径 line 44 已校对过 routes.go（2026-09-09 v3 修订时核对）；本轮不再重复校对
not_goals:
  - 实际写 endpoint handler 代码（5 个端点已在 routes.go 实现）
  - 实际部署 HolmesGPT 或写 HolmesGPT 配置
  - 实际签发只读 token / 配置 auth scope
  - 改 09-AI辅助.md §9.4 YAML 字段（已是 spec 形态）
  - 改 schema 或 migrations
evidence:
  - "docs/v3-架构优化需求.md §3 R1 line 66-72 + §7 A-6 line 130 — R1 需求"
  - "09-AI辅助.md §9.0-9.4 line 5-73 — 已有 v3 段落 + toolset 端点表"
  - "docs/adr/0001~0006 — 既有 ADR 风格模板"
  - "ADR-0003 grpc-作废与三层定位.md — 写边界的先例（决定 ITmanager 不暴露写操作给 AI）"
  - "backend/internal/api/routes.go:218 + 242 + 279 + 294 + 345 — 5 个端点真实路径"
---

# INTENT-M35-R1: HolmesGPT toolset integration spec

## Context

v3 R1 (in `docs/v3-架构优化需求.md` §3) commits ITmanager to be a **data source for HolmesGPT** rather than building its own LLM Q&A layer. The 09-AI辅助.md doc was substantially rewritten for v3 on 2026-09-09 — it already has the toolset endpoint table (§9.2), the safety boundary (§9.3), and the HolmesGPT-side YAML config example (§9.4).

What remains for M35-R1:

1. **ADR-0007** to formally record the toolset boundary decision (read-only token + write operations not exposed + audit_logs mandatory). ADR-0003 set the broader "三层定位" (SoT / 操作台 / AI 分析层), but the specific toolset-level contract hasn't been captured in its own ADR yet.
2. **FIX-PLAN-R1-HOLMESGPT.md** at the project-standard location (parallels FIX-PLAN-D1-D7.md / FIX-PLAN-R2-NETBOX-CIRCUITS.md etc.) — collects endpoint contracts, boundary rules, and references the existing 09-AI辅助.md instead of duplicating.
3. **IMPL-R1-HOLMESGPT.md** — concrete change list (which docs files to touch, what each section says) + verification gates.
4. **CHANGELOG M35-R1 section** + **TODO.md** update for any new G-class findings.
5. **Completion report** per task-completion-protocol.

This is a **docs-only round** — no code, no migrations, no openapi.yaml. The 5 toolset endpoints already exist in `routes.go`; we're capturing the spec, not building new functionality.

## Outcomes

See `outcomes[]` frontmatter.

## Acceptance Criteria

See `acceptance[]` frontmatter. AC-R1-3 is intentionally loose because the FIX-PLAN/IMPL files don't have hard validation gates — they're docs, and the gate is "exists + content matches intent".

## Edge cases

- **Path drift between docs and code**: 09-AI辅助.md §9.2 line 44 already cross-referenced routes.go line numbers (218, 242, 279, 294, 345) on 2026-09-09. If routes.go has shifted since, this round's completion report must call it out.
- **HolmesGPT version drift**: §9.4 YAML config example is illustrative. Real HolmesGPT may require different field names. Out of scope to verify.
- **Missing audit_logs guarantee**: ADR-0007 must require audit_logs entries for every toolset call. Existing audit_logs table (post-000013) supports this, but no code change is in scope.

## Operational constraints

- Do NOT touch: 09-AI辅助.md §9.0-9.4 (already complete), routes.go, any backend code, schema.sql, migrations, openapi.yaml.
- Must preserve: existing ADR numbering (next is 0007).
- Lint/typecheck/test commands: not applicable for docs-only. PM verifies docs exist + content matches intent.

## Evidence

See `evidence[]` frontmatter.

## E2E verification

Per `verify-e2e` skill, this round is docs-only — no HTTP surface to drive. The "user story" is "operator opens the repo, finds M35-R1 spec, sees ADR-0007 boundary + FIX-PLAN endpoint contracts + IMPL change list". Probe = `git log --oneline -5` shows the new commits + `cat docs/adr/0007-*.md` shows content.

## Completion report

After dispatch: write `M35-R1-completion-report.md` per `task-completion-protocol` skill.
