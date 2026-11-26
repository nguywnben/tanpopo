# CP91 — Trace and Overview scope under project selection

**Baseline:** frozen RC4 `40bba5b55c2da6fc00b107b315c9b3167fb20169`  
**Runtime:** isolated QA runtime `http://127.0.0.1:18084`  
**Role/browser:** Organization Owner, real Chromium at 1440×900  
**Gate:** `GATE-REL-07 BLOCKED`

## Reproduction and comparison

1. Sign in as Org Owner and select `QA002 CP78 Fresh Budget` with `No env`.
2. Read the authenticated project and project-environment inventories, trace collection, and 24-hour overview summary.
3. Open Overview and Traces in the real Console and compare the visible values with those API responses.

The selected project had no environments. The sibling project `QA002 Audit mwto47wa` had environment `stage`. The two trace rows both carried the stage environment ID, but were shown while the workspace selector was on `QA002 CP78 Fresh Budget / No env`. The 24-hour Overview showed two requests, 50% errors, $0.012001 settled, zero active holds and zero alerts; the trace list contained the same two records, one 200 and one 503. UI values agreed with the API.

## Classification

**No defect assigned; QA-OBS-016 expanded.** The Org Owner has organization scope, and FT-14 explicitly allows trace inspection scoped to the caller's Project/Organization, so organization-wide trace visibility is consistent with authorization. The selected-project header remains prominent and Overview/Traces have no explicit “organization-wide” scope label. The mismatch can confuse users about which project the report represents. No cross-tenant access, authorization bypass, or stale async response was involved.

The view did not mutate state. The trace API returned metadata only and both rows had `has_debug_payload=false`, consistent with CP89’s narrow result.

## Evidence

- `evidence/CP91-overview-under-project-scope.png` — visually inspected; selected project header and organization aggregate.
- `evidence/CP91-traces-under-project-scope.png` — visually inspected; trace rows under same selected project.
- `evidence/CP91-trace-overview-workspace-scope.json` — sanitized projects, environment mapping, trace metadata and summary.
- `reports/QA-OBS-016-finops-chargeback-workspace-scope.md` — expanded with this cross-screen observation.

## Consolidated status

- **Historical DEF-001–015:** no new retests in CP91; scoped prior QA-002 statuses remain, DEF-005 is partial, independent acceptance remains incomplete, and no QA-001 PASS is inherited.
- **Confirmed open defects:** DEF-016–034 remain confirmed open; no status change. Preserve DEF-034 and the DEF-020 recurrence.
- **New findings:** no new defect. QA-OBS-016 expanded to cover the organization-wide Chargeback row (CP87) and organization-wide Overview/Traces under a selected project (CP91).
- **Coverage completed:** owner-selected empty project vs sibling project/environment mapping; organization-scoped Overview/Traces API values; UI/API consistency and screenshot inspection across both screens.
- **Blocked:** direct webhook delivery/HMAC by managed Cloud egress; runtime 18445 A2A by disabled config; RC4 artifact verification remains unverified in separate DevBox; full OTel collector-outage acceptance remains unverified for lack of known collector endpoint/access.
- **Coverage remaining:** independent DEF-001–015 acceptance; DEF-005 SSRF cases; DEF-007 failure atomicity/populated erasure; non-owner overview/trace scope roles; explicit product report-scope contract; open DEF-016–034 remediation/regression; all remaining feature/browser/platform/integration/accessibility/operations coverage.
- **Recommended handoffs:** unchanged—DEF-034, DEF-016/023/029/030, DEF-033, DEF-005 without weakening SSRF, DEF-007. Confirm whether owner global reports should visibly label organization scope.

QA-002 remains active and incomplete.
