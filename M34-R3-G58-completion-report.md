# M34-R3-G58 — Completion Report

## Delivered

U7b (`TestDBSmoke_ColumnWidthMatchesConstant`) now cross-checks live `integration.ColumnWidths()` against real PG `information_schema.character_maximum_length`, closing the G-58 gap where the test hardcoded literals and only caught DDL drift, not Go-constant drift.

## Changed

| File | Reason |
|------|--------|
| `backend/internal/integration/truncate.go` | +23/-1 — Added exported `ColumnWidths() map[string]int` keyed by `table.column`; also fixed a stale comment (`assets.asset_name` → `assets.name`) to match the actual schema column the test queries. |
| `backend/tests/db_smoke_test.go` | +33/-20 — Removed hardcoded `Expect int` field from the 9-row table; the expected value is now fetched live from `integration.ColumnWidths()` via a `table + "." + column` key. Added `require.Len` guard (want-table size must equal live map size) and `require.True(ok)` guard (column must be in live map). |

## Validation

| Gate | Command | Exit | Result |
|------|---------|------|--------|
| format | `cd backend && gofmt -l internal/integration/ tests/` | 0 | empty |
| vet    | `cd backend && go vet ./...` | 0 | clean (only the allowed sqlite3 C warning) |
| vet    | `cd backend && go vet -tags dbsmoke ./tests/` | 0 | clean |
| test   | `cd backend && go test ./internal/integration/... -count=1` | 0 | `ok network-monitor-platform/internal/integration 12.8s` |
| smoke  | `DOCKER='sudo -n docker' scripts/db_smoke.sh` | 0 | `✅ 迁移(全新+升级) + 冒烟断言全部通过` — U7b line: `✅ U7b 9 列宽全部 == truncate.go 常量（live ColumnWidths 读取，G-58 闭环）` |

## Mutation-inversion evidence

### AC-G58-2 — Go-constant drift (`colAssetName` 255 → 200)

Pre-mutation PASS line:
```
=== RUN   TestDBSmoke_ColumnWidthMatchesConstant
    db_smoke_test.go:2720: ✅ U7b 9 列宽全部 == truncate.go 常量（live ColumnWidths 读取，G-58 闭环）
--- PASS: TestDBSmoke_ColumnWidthMatchesConstant (0.04s)
```

Mutation FAIL (with `colAssetName = 200`):
```
=== RUN   TestDBSmoke_ColumnWidthMatchesConstant
        	Error Trace:	/home/webman/Projects/ITmanager/backend/tests/db_smoke_test.go:2714
        	Error:      	Not equal:
        	            	expected: 200
        	            	actual  : 255
        	Test:       	TestDBSmoke_ColumnWidthMatchesConstant
        	Messages:   	§7 M7/M7b 红点 / G-58：assets.name 的真实列宽(255) ≠ truncate.go colAssetName(200) —— 三者之一必漂：迁移 DDL、模型 tag、truncate.go 常量
--- FAIL: TestDBSmoke_ColumnWidthMatchesConstant (0.04s)
```
(Side note: two unrelated tests also failed — `TestDBSmoke_NetBoxFieldTruncation` and `TestDBSmoke_ThirdPartyFieldTruncation` — they hardcode 255 against the truncate constant which now reads 200. That's a separate signal the constant change actually happened; not a defect of U7b.)

Post-revert PASS: green, identical to pre-mutation.

### AC-G58-3 — DDL drift (`ALTER TABLE assets ALTER COLUMN name TYPE VARCHAR(200)`)

Baseline PASS:
```
=== RUN   TestDBSmoke_ColumnWidthMatchesConstant
    db_smoke_test.go:2720: ✅ U7b 9 列宽全部 == truncate.go 常量（live ColumnWidths 读取，G-58 闭环）
--- PASS: TestDBSmoke_ColumnWidthMatchesConstant (0.04s)
```

Mutation FAIL (after `ALTER ... TYPE VARCHAR(200)`):
```
=== RUN   TestDBSmoke_ColumnWidthMatchesConstant
        	Error Trace:	/home/webman/Projects/ITmanager/backend/tests/db_smoke_test.go:2714
        	Error:      	Not equal:
        	            	expected: 255
        	            	actual  : 200
        	Test:       	TestDBSmoke_ColumnWidthMatchesConstant
        	Messages:   	§7 M7/M7b 红点 / G-58：assets.name 的真实列宽(200) ≠ truncate.go colAssetName(255) —— 三者之一必漂：迁移 DDL、模型 tag、truncate.go 常量
--- FAIL: TestDBSmoke_ColumnWidthMatchesConstant (0.04s)
```

Post-revert PASS (after `ALTER ... TYPE VARCHAR(255)`): green, identical to baseline.

## Risk & Rollback

- **Edge: missing column in live map** — handled by `require.True(t, ok, "%s 在 integration.ColumnWidths() 里找不到 ...")`. If a future migration drops a column, the test surfaces a clear "missing column" message, no panic.
- **Edge: want-table drift vs ColumnWidths()** — handled by `require.Len(t, want, len(liveWidths), ...)`. Adding/removing a column without updating the want list is caught.
- **Rollback** — both changes are additive + a one-line removal of `Expect int`. `git revert` of the two commit hashes (`36cd221`, `8cbcac6`) returns the file to its `1fb333a` shape; no schema or migration changes.
- **Cross-package import** — verified no cycle: `internal/integration` does not import `backend/tests`; `tests` already imports `integration` (existing references at db_smoke_test.go:638/1305/1484/2241/2327/2348/2531/2739).

## Final state

```
8cbcac6 test(M34-G58): rewrite TestDBSmoke_ColumnWidthMatchesConstant to read live constants
36cd221 feat(M34-G58): expose ColumnWidths() getter in integration/truncate.go
1fb333a docs(M34) 步骤 6：intent-spec-author intent.md + task-completion-protocol 报告   ← base
```

Not pushed. `git status` clean.

## Status

**COMPLETE**