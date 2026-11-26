# CP87 — FinOps chargeback scope and date windows

**Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169` (frozen RC4 source)  
**Runtime:** isolated QA runtime `http://127.0.0.1:18084`  
**Role/browser:** Organization Owner, real Chromium at 1440×900  
**Gate:** `GATE-REL-07 BLOCKED`

## Actions and observed results

- Opened the real Console, selected `QA-002 A2A Org / QA002 CP78 Fresh Budget / No env`, and navigated FinOps → Chargeback. Visually inspected and retained both before/after API-query screenshots.
- The page displayed exact spend `$0.012001`, five exact requests, and a chargeback row for `QA002 Audit mwto47wa`, separate from the currently selected project. The UI exposes grouping and export controls, but no visible date/from/to controls.
- Read-only `GET /admin/v1/usage/summary` checks: `group_by=project` returned the same 12,001-micro, five-request organization row shown in the UI; a valid recent RFC3339 window returned 200 with zero rows; malformed and reversed windows returned 400 `invalid_request`.
- Adding the currently selected project ID did not narrow the report; the same organization-wide row remained. This agrees with the handler's organization scope and response data. No mutation was performed.
- FT-13 promises multi-dimensional chargeback but does not specify a date picker or selected-project filter. Recorded the unlabeled organization-wide scope as **QA-OBS-016**, an unresolved UX/business-contract question, not a defect. No evidence supports labeling the behavior a product failure.

## Evidence

- `evidence/qa002-cp87-chargeback-initial.png`
- `evidence/qa002-cp87-chargeback-api-windows.png`
- `evidence/CP87-finops-chargeback-scope-and-date-windows.json`
- `reports/QA-OBS-016-finops-chargeback-workspace-scope.md`

Both screenshots were visually inspected. The second confirms the selected project in the global header while the chargeback row names a different project.

## Result and limits

- **PASS:** UI and organization-scoped API data agree for the observed grouping; valid and invalid API windows are handled consistently.
- **UNVERIFIED:** whether organization-wide scope should be visibly labeled, whether `project_id` filtering is intentionally unsupported, and whether the UI should expose API date-window filtering.
- **No new defect.** This phase did not retest historical defects. No user data or product code was changed.

## Consolidated status

- **Historical DEF-001–015:** retain prior QA-002 scoped dispositions; DEF-005 remains partial. CP87 does not add a retest. QA-001 PASS results are not inherited.
- **Confirmed open defects:** DEF-016–034 remain open; CP86's DEF-034 and DEF-020 recurrence are preserved. No status change in CP87.
- **New findings:** no new defect; QA-OBS-016 documents report-scope labeling ambiguity.
- **Coverage completed:** one live owner Chargeback view, organization project grouping, valid/malformed/reversed API range checks, and selected-project query comparison.
- **Blocked:** direct webhook delivery remains blocked by QA-OBS-015 managed Cloud egress; runtime 18445 A2A remains disabled by configuration; RC4 artifact verification remains unverified in separate DevBox.
- **Remaining:** independent DEF-001–015 acceptance coverage remains incomplete; chargeback roles, pagination, locale, date-filter UI and project-scope contract; unresolved DEF-005 SSRF cases; DEF-007 failure atomicity/populated erasure; all open DEF-016–034 remediation and regression; broader products, browsers, platforms, integrations and operations.
- **Recommended handoffs:** no change to priorities; continue to prioritize DEF-034, DEF-016/023/029/030 state consistency, DEF-033 budget semantics, DEF-005 safe SSRF acceptance, and DEF-007 audit/erasure atomicity.

QA-002 remains in progress. Do not treat this checkpoint as completion or release approval.
