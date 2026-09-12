---
id: INTENT-M34-G58
title: U7b must cross-check Go constants, not hardcode literals
status: draft
author: hermes@local
created: 2026-09-12
outcomes:
  - TestDBSmoke_ColumnWidthMatchesConstant reads column widths from truncate.go (live), not literals
  - If truncate.go's colAssetName drifts from 255, the test FAILS with a clear diff message
  - If the underlying DDL drifts (e.g. migration changes VARCHAR(255) → VARCHAR(200)), the test FAILS
  - All 9 columns verified both directions (DDL ↔ Go constant)
acceptance:
  - id: AC-G58-1
    given: truncate.go constants are 255, 100, 100, 100, 100, 500, 255, 255, 100
    when: TestDBSmoke_ColumnWidthMatchesConstant runs
    then: PASS (no false regression)
  - id: AC-G58-2
    given: colAssetName in truncate.go is changed from 255 to 200 (mutation M7b inverted)
    when: TestDBSmoke_ColumnWidthMatchesConstant runs
    then: FAIL — assertion message shows expected 255 vs actual 200 with table/column name
  - id: AC-G58-3
    given: a hypothetical ALTER TABLE assets ALTER COLUMN name TYPE VARCHAR(200) was run via migration
    when: TestDBSmoke_ColumnWidthMatchesConstant runs
    then: FAIL — assertion message shows expected 255 vs actual 200
  - id: AC-G58-4
    given: db_smoke test runs against real PG
    when: all 9 columns compared
    then: every column has a corresponding line tying the literal to the truncate.go constant name
edges:
  - Constants are unexported (colAssetName not ColAssetName) — test must be in `integration` package or constants must be exposed via a getter
  - If migrate-from-scratch path drops a column that truncate.go still references, test must surface a clear "missing column" error rather than a panic
not_goals:
  - Replacing U7a (the gorm tag reflection test) — both stay; this is additive
  - Migration 000028 follow-up to add new columns — out of scope
  - Exposing all of truncate.go's API to other packages
evidence:
  - "backend/tests/db_smoke_test.go:2671 — TestDBSmoke_ColumnWidthMatchesConstant uses literal 255/100/500 etc."
  - "backend/internal/integration/truncate.go:22-30 — 9 unexported constants"
  - "M33 step 6 mutation report (ad47ef4) §M7/M7b — U7b was expected to catch M7b mutation but only catches DDL drift, not Go-constant drift"
---

# INTENT-M34-G58: U7b must cross-check Go constants, not hardcode literals

## Context

`TestDBSmoke_ColumnWidthMatchesConstant` (U7b, db_smoke_test.go:2671) verifies that the 9 columns it cares about have widths matching the 9 `colAssetName`/`colAlertTriggerName`/etc constants in `backend/internal/integration/truncate.go`. But the test hardcodes the expected values as literals (255, 100, 500, etc.) and only documents the constant name in a comment.

This means:
- If `truncate.go`'s constant drifts (someone changes `colAssetName = 255` to 200), the test continues to pass — it checks DDL against the literal, not against the Go constant.
- The mutation-inversion report (M33 step 6, commit ad47ef4) explicitly flagged this: "U7b catches DDL drift, not Go-constant drift. U7a is the only catcher."

This intent closes that gap by making U7b read the live constants at runtime.

## Outcomes

- U7b becomes a **two-sided cross-check**: DDL ↔ Go constant. If either side drifts, the test fails.
- Mutation M7b (colAssetName 255 → 200) becomes catchable by U7b, not just U7a.
- The 9-column mapping stays explicitly documented in the test (readability is preserved).

## Acceptance Criteria

See `acceptance[]` frontmatter. Each AC maps to one assertion in the rewritten test.

## Edge cases

The constants are unexported (lowercase `colAssetName`). The cleanest fix is one of:

- (a) Move the test into `backend/internal/integration/` (same package, can read unexported).
- (b) Expose a getter `func ColumnWidths() map[string]int` in the integration package.
- (c) Use reflection in a separate `_test.go` file with the right build tag.

Recommended: **(b)** — exposing a small getter is the least intrusive change and keeps db_smoke_test.go's //go:build dbsmoke isolation intact.

## Operational constraints

- **Do NOT touch**: `redact/truncate.go`, `redact/redact.go` (different package, different constants).
- **Must preserve**: `//go:build dbsmoke` build tag on db_smoke_test.go.
- **Test gates**:
  - `go test ./internal/integration/... -count=1`
  - `DOCKER='sudo -n docker' scripts/db_smoke.sh` — must show ≥ 40 PASS

## Evidence

See `evidence[]` frontmatter. The mutation-inversion report explicitly flagged this gap, making it a tracked PM finding rather than a guess.

## E2E verification

Per `verify-e2e` skill: drive `scripts/db_smoke.sh`, observe TestDBSmoke_ColumnWidthMatchesConstant passes against current 9 columns. Then mutate one constant, re-run, observe FAIL. Then revert. Capture both stdout.

## Completion report

After dispatch: write `M34-R3-G58-completion-report.md` per `task-completion-protocol` skill.
