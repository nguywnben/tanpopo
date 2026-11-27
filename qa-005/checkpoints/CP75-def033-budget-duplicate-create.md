# CP75 — DEF-033 FinOps duplicate-create retest

**Source:** exact unsigned RC7 source `737079ddb847d97dbcf832f610ed0048556cb65b`; Linux/amd64 runtime SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`; isolated disposable SQLite clone `127.0.0.1:18162`; Chromium 156; synthetic Org Owner; `QA005 Org / QA005 No Budget Project`; monthly Project ceiling. No provider calls.

**Disposition:** **PASS (scoped QA-005 retest)** for duplicate Create rejection and persistence in this one Owner/SQLite/project/month scenario. Historical QA-002's silent-overwrite/stale-revision failure and QA-003's narrower duplicate-guard pass remain preserved.

## Reproduction and result

1. Opened FinOps in the active project scope. The visually inspected initial page showed five Virtual Key ceilings and no Project ceiling. See [initial FinOps screen](../evidence/screenshots/CP75-def033-empty-before.png).
2. Through the real Console, created a monthly Project ceiling of 2,500,000 integer micro-USD. The resulting project row is visible after reload in [persisted budget screen](../evidence/screenshots/CP75-def033-persisted-row.png).
3. Opened **Create budget** again for the same project/month, entered 3,000,000, and submitted. The Console presented: “A budget already exists for this scope and period. Update the existing limit instead of creating a duplicate.” The form offered a separate **Update existing budget** action. See the [duplicate form](../evidence/screenshots/CP75-def033-duplicate-form.png) and the [rendered duplicate rejection](../evidence/screenshots/CP75-def033-duplicate-create.png).
4. Did not activate the separate update action. Browser request capture saw no non-auth write request from the duplicate submission. An authenticated `GET /admin/v1/budgets?limit=100` returned HTTP 200; matching row remained `hard_limit_micros=2500000`, `version=1`, warnings `[50,80,95]`. The refreshed Console row also still displayed 2,500,000.
5. Visually inspected all four screenshots. The duplicate error is legible in the open drawer, and after closing it the table shows the project ceiling and unchanged 2,500,000 limit.

Sanitized API/UI metadata: [CP75 evidence JSON](../evidence/CP75-def033-duplicate-create.json).

## Scope and limitations

The exact-RC7 duplicate Create path did not reproduce the historical silent overwrite: the duplicate is rejected before a write, and the original row/version persist. Thus the tested scenario passes; this does not erase prior failure evidence or globally close the finding. Explicit **Update existing budget**, optimistic concurrency/stale-revision after an intentional update, concurrent editors, other roles/scopes/periods, and PostgreSQL/Valkey are unverified. The test-created synthetic ceiling remains in this disposable QA database and is not product/source data. No code, specification or release state changed. `GATE-REL-07` remains BLOCKED.
