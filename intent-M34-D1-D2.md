---
id: INTENT-M34-D1-D2
title: Close residual G-25 ticket-number hole and finalize D-1 tickets schema
status: accepted
author: hermes@local (PM)
created: 2026-09-12
outcomes:
  - A POST /tickets request without `ticket_number` field generates a unique T-YYYYMMDD-NNNN label
  - 25 same-day ticket inserts produce 25 distinct labels (A..Z, then AA) with no collision
  - Two concurrent inserts producing the same label result in exactly one row committed (UNIQUE index + retry)
  - Schema columns are aligned with model fields (ticket_type NOT NULL removed, all 24 model fields insert-clean)
  - Hand-built ticket numbers from external integrations (GLPI external_id) bypass the auto-generator
acceptance:
  - id: AC-M34-1
    given: a fresh PG instance with migrations 000013 and 000028 applied
    when: POST /tickets with all 24 model fields populated, omitting ticket_number
    then: ticket_number is auto-assigned T-YYYYMMDD-A (first of day); INSERT succeeds; row visible
  - id: AC-M34-2
    given: same fresh PG instance
    when: 25 same-day ticket INSERTs run, each without ticket_number
    then: 25 distinct labels generated, no two the same; covers A..Z + AA
  - id: AC-M34-3
    given: tickets table has UNIQUE INDEX on ticket_number (from migration 000013)
    when: a manual INSERT tries to add a duplicate ticket_number
    then: PG returns 23505 unique_violation; application retry path catches it
  - id: AC-M34-4
    given: model.Ticket.TicketType has gorm:\"size:20\" (no NOT NULL)
    when: INSERT a ticket with TicketType=\"\" (empty string)
    then: row commits without 22001 NOT NULL violation (DDL has DROP NOT NULL)
  - id: AC-M34-5
    given: GLPI sync sends a ticket with external_id=\"GLPI-12345\" and ticket_number=\"\" (empty)
    when: AssignTicketNumbers is called before CreateInBatches
    then: each row gets a distinct assigned label; pre-filled ticket_numbers (if any) skip the assignment
edges:
  - Concurrent threads hitting the same day's prefix can race on the max+1 calc; resolved by UNIQUE constraint + retry
  - Hand-edited ticket_numbers with non-seqLabel suffix (e.g. \"TICKET-20260912-A1\") are parsed as garbage and skipped in max calc — does not block new ticket generation
  - Manual DELETE of a ticket mid-day leaves a hole; max+1 algorithm is immune
not_goals:
  - Cross-day ticket numbering (each day resets to A)
  - Distributed / multi-region ticket ID coordination
  - Migration 000028 also covers users.role / deleted_at (D-4) or audit_logs (D-5) — those are separate intents
  - Replacing the WHOLE schema (we use additive 000028 follow-on to 000013, never destructive)
evidence:
  - "docs/FIX-PLAN-D1-D7.md §3.1 — D-1/D-4/D-5 strategy: ADD COLUMN IF NOT EXISTS + DROP NOT NULL"
  - "TODO.md G-25 entry (line 92-95): original fix b58696f, residual boundary documented as \"删过工单后 count 回退\""
  - "commit b58696f (M26) — AssignTicketNumbers batch-path introduced; generateTicketNumber still count-based"
  - "commits 4496087 / 7fb2ca9 / 80be839 / 241408e — M34 actual delivery"
  - "scripts/db_smoke.sh — TestDBSmoke_TicketsSchemaRoundTrip + GenerateTicketNumberDayScoped + TicketNumberRetry"
---

# INTENT-M34-D1-D2: Close residual G-25 ticket-number hole and finalize D-1 tickets schema

## Context

The G-25 ticket-number collision bug was fixed in M26 (commit b58696f, 2026-09-10) for the **batch-insert** path via `AssignTicketNumbers(db, tickets)`. But the **single-row** path (`generateTicketNumber` called from `Ticket.BeforeCreate`) was left count-based — i.e., it counts the rows already in the table for today's prefix and uses `count + 1` as the next label. If a ticket is manually deleted mid-day, `count` rolls back and the next single-row INSERT may pick an already-used label, hitting the UNIQUE constraint and forcing `TicketService.Create`'s 5-retry loop to fail all 5 attempts (because each retry reads the same count).

This intent closes that residual boundary by switching `generateTicketNumber` to a used-set + max+1 algorithm, and uses the same algorithm in `AssignTicketNumbers` for consistency. Concurrently, it finishes D-1 schema alignment: a small idempotent migration `000028_tickets_schema_align` that drops NOT NULL on `tickets.ticket_type` (which the model declares as `gorm:"size:20"` without NOT NULL — so empty-string values would 22001 against the original DDL).

## Outcomes

Bullet list corresponds to `outcomes[]` frontmatter.

- **A POST /tickets request without `ticket_number` field generates a unique T-YYYYMMDD-NNNN label.** This is the user-visible behavior of the auto-numbering path. Operators never see a 500 from this path on a normal request.
- **25 same-day ticket inserts produce 25 distinct labels (A..Z, then AA) with no collision.** Confirms the algorithm scales past the 26-letter alphabet boundary.
- **Two concurrent inserts producing the same label result in exactly one row committed (UNIQUE index + retry).** Safety net even if used-set calc races.
- **Schema columns are aligned with model fields (ticket_type NOT NULL removed, all 24 model fields insert-clean).** The schema_drift_test stops complaining.
- **Hand-built ticket numbers from external integrations (GLPI external_id) bypass the auto-generator.** Backwards compatibility with M26's batch path.

## Acceptance Criteria

See `acceptance[]` frontmatter. Each AC maps to one test in `tests/db_smoke_test.go`:

- AC-M34-1 ↔ `TestDBSmoke_TicketsSchemaRoundTrip`
- AC-M34-2 ↔ `TestDBSmoke_GenerateTicketNumberDayScoped`
- AC-M34-3 ↔ `TestDBSmoke_TicketNumberRetry`
- AC-M34-4 ↔ implicit in AC-M34-1 (empty ticket_type = empty string allowed)
- AC-M34-5 ↔ covered by M26's `TestTicket_AssignTicketNumbers_已有号不覆盖` (sqlite unit test, untouched by M34)

## Edge cases

See `edges[]` frontmatter. Two are non-obvious:

- Concurrent threads race on `max+1` calc; UNIQUE index + 5-retry is the resolution (this is documented in `models/ticket.go:generateTicketNumber` comment, lines 138-148).
- Hand-edited ticket numbers with non-seqLabel suffix (e.g. `TICKET-20260912-A1`) are skipped in max calc — they are parsed as garbage by `parseSeqSuffix`, which returns `(_, false)`. This is intentional: the algorithm does not block on weird human edits, it just ignores them in the max pool.

## Operational constraints

- **Do NOT touch**: `migrations/000001_init.up.sql`, `migrations/000013_schema_align.up.sql`. M34 only adds 000028.
- **Must preserve**: existing UNIQUE index on tickets.ticket_number (created in 000013).
- **Lint/typecheck/test commands**:
  - `gofmt -l backend/`
  - `cd backend && go vet ./...`
  - `cd backend && go test ./internal/{redact,integration,models}/... -count=1`
  - `DOCKER='sudo -n docker' scripts/db_smoke.sh`

## Evidence

See `evidence[]` frontmatter. The chain is: M26 fix has documented residual → M34 intent.md captures the residual → M34 commits close the residual → db_smoke tests prove the closure.

## Completion report

`M34-R1-completion-report.md` — 5-section report per `task-completion-protocol` skill.

## E2E verification

Per `verify-e2e` skill, the user-visible flow is:

1. `POST /api/tickets` (HTTP, real surface) with empty ticket_number → expect 201 with auto-generated T-YYYYMMDD-NNNN
2. Bulk-insert 25 same-day rows → expect 25 distinct labels
3. Manual duplicate INSERT → expect 23505

Probe commands captured to `evidence/M34-R1/`.
