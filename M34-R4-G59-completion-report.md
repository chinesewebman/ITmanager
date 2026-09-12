# M34-R4 — Completion Report (per task-completion-protocol skill)

> Skill: `task-completion-protocol`
> Intent: INTENT-M34-G59
> Reporter: hermes@local (PM)
> Date: 2026-09-12

---

## Delivered

Two new test functions in `metric_sync_test.go` (`TestMetricSyncWorker_TickOkLogWritten` + `TestMetricSyncWorker_TickErrorLogWritten`) close the M33 §M11 mutation-inversion gap: replacing the worker's `log.Printf` calls with no-ops is now caught.

## Changed

```
8b90b96 test(M34-G59): add log-capture tests for MetricSyncWorker tick output (M11 inverse)
9ec65a1 docs(M34-R4): intent-M34-G59.md (intent-spec-author artifact)
```

| File | Reason |
|------|--------|
| `backend/internal/integration/metric_sync_test.go` | +88 lines — 2 new test funcs at L370 + L410, reuse existing `captureLog` helper from `truncate_test.go` (same package) |
| `intent-M34-G59.md` | +90 lines — intent-spec-author artifact (outcomes/AC/edges/not_goals/evidence) |

## Validation

| Gate | Command | Exit | Result |
|------|---------|------|--------|
| Lint | `cd backend && gofmt -l internal/integration/` | 0 | clean |
| Typecheck | `cd backend && go vet ./...` | 0 | clean (only sqlite3 C warning, pre-existing) |
| Target | `cd backend && go test ./internal/integration/... -count=1 -run TestMetricSyncWorker_Tick` | 0 | `ok 4.827s`, both tests PASS |
| Race | `cd backend && go test -race ./internal/integration/... -count=1 -run TestMetricSyncWorker_Tick` | 0 | `ok 4.359s` |
| Full regression | `cd backend && go test ./internal/integration/... -count=1` | 0 | `ok 14.689s` (existing tests unaffected) |

## Mutation-inversion evidence (AC-G59-3)

Captured by subagent in transcript. Summary:

- **FAIL state** (after `log.Printf → no-op` at `metric_sync.go:112 + 118`):
  - `TickOk`: assertion failed with `"expected tick ok line in log; got: ..."` (captured log contained only the field-truncation log, no `tick ok` substring)
  - `TickError`: assertion failed with `"expected tick error line in log; got: \"\""` (empty buffer — the worker did fire the tick but the no-op'd log.Printf wrote nothing)
- **PASS state** (after `git checkout -- metric_sync.go`):
  - `ok network-monitor-platform/internal/integration 1.829s` — both new tests green

PM independent re-verified the PASS state above. Did NOT re-run the mutation inverse (subagent already captured both states; doing it again risks drift if the captured output is non-deterministic).

## Risk

- **Global log state**: `captureLog` swaps `log.SetOutput` for the duration of each test. Both tests use `t.Cleanup` to restore. Tests do **NOT** call `t.Parallel()`. If a future test in this package adds `t.Parallel()` and also writes to stdlib `log`, captured output could mix. Documented in test comments; not blocking.
- **Re-used helper dependency**: the tests rely on `captureLog` from `truncate_test.go` (same package). If `truncate_test.go` ever deletes that helper, these tests break. The helper is general-purpose; risk is low.
- **No production code touched** — the mutation inverse is the only intentional change to `metric_sync.go`, and it's reverted.

Rollback: `git revert 8b90b96` (and `9ec65a1` if intent artifact needs to go too).

## Status

**COMPLETE**

Branch: `main`. Tip: `9ec65a1`. Author: hermes@local. Working tree clean. No push.

---

## Skill-application trace

1. **`intent-spec-author`** → wrote `intent-M34-G59.md` (4467 bytes, 5 outcomes / 3 AC / 3 edges / 4 not_goals / 3 evidence anchors)
2. **`omp-task-brief`** → rendered `/tmp/INTENT-M34-G59-brief.md` (4212 bytes, with 5-section report template, mutation-evidence required)
3. **`task-completion-protocol`** → this file (5 sections satisfied)
4. **`verify-e2e`** → mutation inverse evidence captured by subagent; PM independently re-verified PASS state
5. **`pre-flight graph audit`** → confirmed G-59 was a real gap (worker logs existed, no test asserted on them)
