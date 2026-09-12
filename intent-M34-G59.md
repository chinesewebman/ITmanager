---
id: INTENT-M34-G59
title: Add worker tick-log assertion test (M11 mutation inverse)
status: draft
author: hermes@local
created: 2026-09-12
outcomes:
  - A test asserts that "tick ok: written=N, field_truncations=N" is logged after a successful sync tick
  - A test asserts that "tick error: ..." is logged when the sync fails (e.g., connect refused)
  - Both tests use a log capture mechanism that does not affect other tests
  - M11 mutation inverse (replace `log.Printf` with `_, _, _ = err`) makes both tests red
acceptance:
  - id: AC-G59-1
    given: MetricSyncWorker with stub Zabbix + DB returning N writes, 0 truncations
    when: 1 tick fires
    then: captured log contains "tick ok: written=N, field_truncations=0"
  - id: AC-G59-2
    given: MetricSyncWorker with Zabbix URL pointing to closed port (connect refused)
    when: 1 tick fires
    then: captured log contains "tick error:" + the connect refused message
  - id: AC-G59-3
    given: tick log lines are temporarily edited to no-op (e.g., `_, _ = written, ft`)
    when: both new tests run
    then: both fail with "expected tick ok line in log; got: <actual captured logs>"
edges:
  - log capture must not interfere with other tests (use t.Setenv or per-test logger swap)
  - worker tick interval is configurable; tests should use small interval (50ms) for speed
  - SQLite test DB may not match PG log content (e.g., field_truncations key); tests must work with sqlite
not_goals:
  - Replacing the worker (MetricSyncWorker is fine as-is)
  - Adding new fields to the log line (the format is the contract)
  - Migrating from log.Printf to slog (separate concern)
evidence:
  - "backend/internal/integration/metric_sync.go:112 + 118 — log.Printf call sites"
  - "backend/internal/integration/metric_sync_test.go:287-353 — existing Start/Stop/CtxCancel tests, none assert log content"
  - "M33 mutation-inversion report (ad47ef4) §M11 — mutation un-catchable because no log assertion test"
---

# INTENT-M34-G59: Worker tick-log assertion test (M11 inverse)

## Context

`MetricSyncWorker` calls `log.Printf` after each tick:
- `[zabbix metric sync] tick error: <err>` on failure (line 112)
- `[zabbix metric sync] tick ok: written=<n>, field_truncations=<n>` on success (line 118)

These log lines are the only signal an operator has that the worker is alive. The M33 step 6 mutation inversion report (`ad47ef4`) explicitly flagged this as **un-catchable by any existing test**: replacing `log.Printf("...", written, ft)` with `_, _ = written, ft` would compile cleanly and pass all unit tests — the test suite never asserts on log content.

This intent closes the gap by adding two log-capture tests, one for the success path and one for the error path.

## Outcomes

- **A test asserts "tick ok" log line on success.** Closes M11 success-path mutation.
- **A test asserts "tick error" log line on failure.** Closes M11 error-path mutation.
- **Both use isolated log capture.** No test bleed.
- **M11 mutation inverse verifiable**: edit log.Printf → no-op, run tests, both red, revert, both green.

## Acceptance Criteria

See `acceptance[]` frontmatter.

## Edge cases

The current code uses the stdlib `log` package which writes to `os.Stderr` by default. Tests need to redirect or buffer. Approaches:

- (a) `log.SetOutput(buf)` + restore in t.Cleanup. Simple but global state.
- (b) Inject a logger into `MetricSyncConfig`. Cleaner but touches prod code.
- (c) Capture stderr via `os.Pipe`. OS-level, brittle.

Recommended: **(a)** for first pass, with a clear note that (b) is the right long-term fix. Tests already use global state (the worker struct shares state), so the risk is bounded.

## Operational constraints

- Do NOT touch: the worker struct's public API (only its log calls).
- Must preserve: existing `TestMetricSyncWorker_StartStop幂等` + `_Stop未启动` + `_CtxCancel` tests still pass.
- Test gates:
  - `cd backend && go test ./internal/integration/... -count=1`
  - `cd backend && go test -race ./internal/integration/... -count=1`

## Evidence

See `evidence[]` frontmatter. The mutation-inversion report explicitly flagged this gap.

## E2E verification

Per `verify-e2e`: run the new tests, capture stdout. Manually apply the M11 mutation (replace log.Printf with `_, _, _ = ...`), re-run, capture FAIL output, revert, capture PASS.

## Completion report

After dispatch: write `M34-R4-G59-completion-report.md` per `task-completion-protocol` skill.
