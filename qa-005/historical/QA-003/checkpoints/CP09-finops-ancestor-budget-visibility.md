# CP09 — Organization budget visibility in project FinOps scope

**Baseline:** frozen RC5 `3423d4f523ef0ac77469a1201b8b753d1d31c902`
**Status:** DEF-024 is verified fixed for the tested Org Owner/project view with simultaneous organization and project ceilings. Broader role and enforcement coverage remains open. `GATE-REL-07` remains **BLOCKED**.

## Procedure and result

1. In a separate authenticated Chromium session, created an organization/monthly budget of 20,000,000 micro-USD through FinOps → Create budget. The UI form explicitly showed Organization scope and the active organization ID; the pre-existing project/monthly limit remained 2,500,000. Form screenshot: `../evidence/CP09-org-budget-form.png`.
2. Reauthenticated in a fresh browser context and opened FinOps while the active workspace remained `Default Organization / default / dev`.
3. Visually inspected `../evidence/CP09-org-budget-in-project-view.png`: the table shows two distinct rows, `ORGANIZATION` with 20,000,000 and `PROJECT` with 2,500,000, each with its own scope ID and period.
4. Captured the actual Console request `GET /admin/v1/budgets?project_id=<default-project-id>&include_ancestors=true&limit=100`; it returned both rows. An unfiltered GET also returned the same two persisted rows. Sanitized readback and request evidence are in `../evidence/CP09-org-budget-scope-readback.json`.

The UI therefore does not hide the applicable organization ceiling in this project view. The initial evidence runner had a DOM accessor error after the organization-budget submission; the independent fresh-session read above verifies the persisted write and rendered state. A scope-only screenshot at No project/No env also showed both rows in [this screenshot](../evidence/CP09-finops-org-only-scope.png), but the automated follow-up back to project context hit a locator ambiguity; that attempted scope round trip is not counted as a pass.

## DEF-024 disposition

**VERIFIED FIXED for the tested Owner/project view.** The ancestor organization limit is returned by the project-filtered API and rendered separately from the project's own limit. This is fresh RC5 evidence, not an inherited QA-002 result.

Remaining: confirm the exact no-local-project-budget case, other roles and project-level permissions, edit authorization for inherited rows, refresh and repeated scope switching, environment/Virtual Key ancestors, and actual simultaneous enforcement after usage settlement. This checkpoint does not assert that those paths pass.

## Coverage update

- **PARTIAL:** FT-13 budgets/settlement/chargeback; SCR-12 FinOps; WF-08 quota/budget accounting. The two configured ceilings are visible and persisted; no usage or enforcement was exercised.
- No new defect confirmed. Release readiness and GATE-REL-07 remain blocked.
