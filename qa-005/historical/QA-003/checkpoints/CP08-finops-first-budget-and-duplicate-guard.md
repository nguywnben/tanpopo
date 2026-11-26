# CP08 — First FinOps budget and duplicate-create guard

**Baseline:** frozen RC5 `3423d4f523ef0ac77469a1201b8b753d1d31c902`
**Status:** independent project-budget create/persistence and duplicate-create guard passed for the tested scope. `GATE-REL-07` remains **BLOCKED**.

## Journey and evidence

1. Signed in to the existing isolated SQLite runtime (`127.0.0.1:18103`) as the synthetic Org Owner and opened FinOps → Budgets & Holds. The empty state showed `Budget ceilings 0`, all usage/hold figures at zero, and **Create budget**. Authenticated `GET /admin/v1/budgets?limit=100&include_ancestors=true` returned an empty data set. Screenshot `../evidence/CP08-finops-initial.png` was visually inspected.
2. Opened the real Console form. It defaulted to Project, the active `default` project, Monthly, warning percentages 50/80/95, and a 100,000,000 micro-USD ceiling. Set the hard limit to 2,500,000 and submitted. The Console showed `Created budget ceiling`; the API returned HTTP 200 with a single project/monthly resource at the expected project ID, limit 2,500,000, warning thresholds `[50,80,95]`, and version 1. `../evidence/CP08-budget-create-form.png` and `../evidence/CP08-budget-created.png` were visually inspected.
3. Opened Create again for that same project/monthly scope, entered 2,800,000, and submitted. The form stayed open with an inline “A budget already exists for this scope and period” error and an explicit **Update existing budget** action. I canceled without updating. Admin API readback remained the same resource at 2,500,000/version 1. `../evidence/CP08-budget-duplicate-rejected.png` was visually inspected; machine-readable before/after data is `../evidence/CP08-budget-journey.json`.
4. Opened a separate browser context, authenticated independently, and loaded FinOps. The same budget ID, scope, period, amount and version were visible in the UI and returned by the Admin API. Screenshot `../evidence/CP08-budget-fresh-session.png` was visually inspected.

No inference or provider request was made. No budget limit was changed after creation.

## Defect dispositions

- **DEF-003 — VERIFIED FIXED for the first project/monthly budget journey.** This independently reproduces the original empty-to-created and persistent-read criteria. Organization, environment and Virtual Key scopes; enforcement and budget rollover remain unverified.
- **DEF-033 — VERIFIED FIXED for the same-scope duplicate-create scenario.** Duplicate Create is rejected inline and keeps the user from accidentally overwriting the existing limit. The explicit **Update existing budget** path was offered but not activated; its optimistic-lock, persistence and concurrent-user behavior remain open.
- **DEF-012 — UNVERIFIED.** This phase had no settlement rows or budget ledger and therefore did not exercise the historical usage-without-budget accounting discrepancy.

## Coverage update

- **PARTIAL:** FT-13 Budgets/reservations/settlement/chargeback; SCR-12 FinOps budgets/chargeback; WF-08 quota/budget accounting without exact usage.
- **Remaining:** organization/environment/Virtual Key ceilings, inherited scopes, warning thresholds firing, reservation and settlement, unmetered usage, period rollover, limit edits, concurrent updates, chargeback export/settlement reconciliation, and cross-screen budget enforcement.
- No new defect was confirmed in this phase. Previously reported QA-OBS-014 and QA-2 historical states are not treated as current evidence.
- QA-003 remains incomplete; no PASS is inherited from QA-001 or QA-002. `GATE-REL-07` remains **BLOCKED**.
