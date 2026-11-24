# CP49 — Workspace selection and scoped FinOps state after refresh

**Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`
**Result:** QA-OBS-009 recorded; contract unresolved, not classified as a defect.
**Product source changes:** none.

## Work performed

In a fresh authenticated Chromium context, the organization owner landed in the runtime's secondary project/environment. The owner selected the first project through the actual workspace menu and visited FinOps. Before refresh the page showed that project's two persisted budget rows (project and environment). A full page refresh restored the secondary project/environment and its “No budgets configured” state. The screenshots were visually inspected. No budget or other business data was changed.

The fresh login and post-refresh scope matched the runtime's initial workspace, while the user's manual selection did not survive this refresh. The selected scope and resulting FinOps dataset changed together; the first project's persisted rows were not deleted. QA-OBS-009 records the behavior pending an explicit preference contract. This does not resolve DEF-024, because the organization budget remains hidden from the project-scoped list.

## Evidence

- `../evidence/CP49-finops-before-refresh.png`
- `../evidence/CP49-finops-after-refresh.png`
- `../evidence/CP49-workspace-refresh-state.json`

## Coverage disposition

- **PASS:** persisted project1 rows displayed in project1 scope; after refresh, project2 scope and its empty local-budget state were shown consistently.
- **UNVERIFIED:** whether last-selected workspace is intended to persist across reload, session, new browser, and device; no preference contract was identified.
- **FAIL still open:** DEF-024, applicable organization budget hidden from the project-scoped FinOps list.

`GATE-REL-07` remains **BLOCKED** and QA-002 continues.
