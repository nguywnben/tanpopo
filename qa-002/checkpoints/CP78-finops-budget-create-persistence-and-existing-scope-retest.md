# CP78 — DEF-003 fresh budget, persistence, and existing-scope create behavior

- **Baseline:** frozen RC4 `40bba5b55c2da6fc00b107b315c9b3167fb20169`; product code unchanged.
- **Runtime/browser:** isolated `127.0.0.1:18084`, authenticated Org Owner, real Chromium.
- **Results:** PASS for first project-budget creation and post-refresh persistence (DEF-003 scoped). FAIL DEF-033 Moderate for Create budget on a scope that already has a matching budget. QA-OBS-014 records a separate unresolved empty-state/session discrepancy.

## Fresh project: DEF-003 independent retest

1. Created `QA002 CP78 Fresh Budget` through the real scope switcher **New project** flow. It had no previous budget records.
2. Opened FinOps → Budgets & Holds. The page showed `Budget ceilings 0` and “No budgets configured”; the Create drawer defaulted to Project and the selected project, with MONTHLY cadence, 50/80/95 warning thresholds and an integer micro-USD limit.
3. Entered 2,500,000 and submitted through **Create budget**. The UI showed “Created budget ceiling” and one project row.
4. Hard refreshed in a separate real-browser login, reselected the project and reopened FinOps. The row remained. Authenticated API reads returned HTTP 200 with ID `bgt_project_prj_01m868q2jte00abmhf75asqts3_monthly`, PROJECT scope, the new project ID, MONTHLY period, limit 2,500,000 and version 1. Before/after screenshots were visually inspected.

Evidence: `../evidence/CP78-new-project-form.png`, `CP78-fresh-budget-empty.png`, `CP78-fresh-budget-form.png`, `CP78-fresh-budget-created.png`, `CP78-fresh-budget-before-refresh.png`, `CP78-fresh-budget-refresh-default.png`, `CP78-fresh-budget-after-refresh.png`, and sanitized `CP78-fresh-budget-journey.json` / `CP78-fresh-budget-refresh.json`. The first runner's expected post-reload Overview heading was incorrect because Tanpopo stayed on FinOps; the separate login/reload check completed the persistence verification. No provider request or inference was made.

## Existing project/period: DEF-033

A matching budget row existed for the fresh project at 2,500,000 before opening Create. The real Create form was then submitted at 3,000,000. It changed that same row to 3,000,000/version 2 and showed “Created budget ceiling.” A repeat submission at 2,800,000 sent `If-Match: "v1"` to the existing ID and returned HTTP 409 `optimistic_lock_conflict` because current revision was v2. The error appeared behind the open drawer and remained visible after Cancel dismissed it. I restored the ceiling via the row's Save action to 2,500,000/version 3; final API read and screenshot corroborate restoration. No usage or holds existed.

Evidence: `../evidence/CP78-existing-budget-before-create.png`, `CP78-existing-budget-create-form.png`, `CP78-existing-budget-after-create.png`, `CP78-budget-duplicate-conflict.json`, `CP78-budget-duplicate-conflict-drawer.png`, `CP78-budget-duplicate-conflict-dismissed.png`, and `CP78-budget-duplicate-restored.png`. Full reproduction/acceptance criteria: `../reports/DEF-033-finops-create-budget-reuses-stale-revision.md`.

## Conflicting exploratory state

The earlier A2A-project sequence showed an empty budget screen once, then a matching row in a later pre-submit screenshot. No API snapshot was saved between those states; the resulting row stayed at 1,000,000 micro-USD with zero project usage. Preserve this uncertainty as QA-OBS-014 (`../reports/QA-OBS-014-finops-empty-state-session-discrepancy.md`); do not use that sequence as the DEF-003 first-budget pass.

## Consolidated status

- **Historical DEF-001–015:** CP78 independently retested DEF-003 and passed the fresh project/monthly create and persistence slice. Other historical defects retain their prior scoped dispositions; original acceptance criteria remain incomplete overall. DEF-005 stays PARTIAL.
- **New findings:** DEF-033 Moderate confirmed for repeated Create on a matching budget scope/period. DEF-032 remains confirmed. QA-OBS-014 is unresolved and not classified as a defect.
- **Coverage completed:** fresh project creation, empty FinOps state, budget form, creation, same-scope persistence after reload, existing-budget collision, optimistic-lock error, and cap restoration; screenshots visually inspected and API state compared.
- **Blocked / remaining:** no CP78 blocker. Organization/environment/Virtual-Key budget creation, warning delivery, period rollover, estimate/unmetered cases, concurrency and cap precedence remain open. Continue DEF-001–015 retests, all other features/roles/platforms and integration gaps. The separate A2A-project empty-state discrepancy needs request-logged reproduction.

`GATE-REL-07` remains **BLOCKED**; QA-002 remains in progress.
