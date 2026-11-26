# CP48 — FinOps workspace switch while budget response is delayed

**Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`
**Result:** PASS for one delayed project switch; no new defect identified.
**Product source changes:** none.

## Work performed

In real Chromium, the owner opened FinOps while in the CP43 secondary project/environment and switched to the first project using the workspace chooser. A browser-level test route delayed only the first project's budget GET by 1.8 seconds, then forwarded it unchanged to the isolated server. Other traffic and persisted state were not modified.

The visually inspected in-flight screenshot showed the new project/environment selected and table skeletons; it did not retain the previous project's rows under the new scope. After the delayed response settled, the page displayed the two budget rows persisted for project1: its environment budget and project budget. The request query parameters matched the selected project IDs.

## Evidence

- `../evidence/CP48-project-switch-loading.png` — visually inspected in-flight state.
- `../evidence/CP48-project-switch-settled.png` — visually inspected settled state and rows.
- `../evidence/CP48-finops-scope-switch-latency.json` — sanitized setup, request, scope, and outcome summary.

## Coverage disposition

- **PASS:** one owner FinOps budget list transition from project2 to project1 under 1.8-second response delay showed a loading skeleton, then only the selected project's rows.
- **UNVERIFIED:** project/environment switching during multiple concurrent requests; stale responses after navigation away/back; failed/aborted requests; refresh during a pending request; role/permission changes mid-flight; other Console pages and browsers. DEF-023 and DEF-024 remain confirmed open.

This is a narrow rendering regression pass, not a general workspace-state clearance. `GATE-REL-07` remains **BLOCKED**.
