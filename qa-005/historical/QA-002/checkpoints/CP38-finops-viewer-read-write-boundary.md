# CP38 — FinOps Viewer UI and budget-write boundary

**Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`
**Runtime:** isolated SQLite Tanpopo at `127.0.0.1:18086`, real Chromium Console through local HTTPS proxy
**Release gate:** `GATE-REL-07` remains **BLOCKED**.

## Setup

- As Org Owner, created `qa002-cp38-finops@example.invalid` through Access & Keys → Members & Roles. The real POST `/admin/v1/members` returned 201 with only the `FINOPS_VIEWER` role.
- Normal local enrollment was unavailable as already documented in DEF-021. To exercise this role in a real session, QA temporarily set a production-format Argon2id password hash in the disposable database only. The authenticated session returned exactly `FINOPS_VIEWER`. After testing, the test principal was suspended and its password hash cleared; no password/hash is stored in evidence.

## Results

- **FinOps read — PASS for tested views.** The real session opened Budgets & Holds and Chargeback. The screen rendered 15 budget rows, exact/estimated/holds/unmetered values, and the chargeback aggregate. Screenshot inspection confirmed read access and the `FINOPS_VIEWER` identity.
- **Mutation boundary — PASS for one project budget.** The UI did not expose a Create budget action (`button` count 0) and rendered limit controls disabled. Direct same-session `GET /admin/v1/budgets` returned 200. A valid-CSRF, same-version PUT that attempted to save the existing project budget values returned 403 `insufficient_role_permission`. The project budget remained period MONTHLY, limit 10 micro-USD and version 20 before and after.
- **Out-of-scope data — PASS for one key collection.** The session's attempt to load `GET /admin/v1/virtual-keys` returned 403. No Virtual Key data appeared in the FinOps screen.
- The API outcomes, screenshots, exact role and unchanged before/after project budget state are recorded in the evidence files below. No product code changed.

## Evidence

- `../evidence/CP38-finops-member-form.png` — owner creation form with only FinOps Viewer selected.
- `../evidence/CP38-finops-member-after-submit.png` — visually inspected success and persisted member row.
- `../evidence/CP38-finops-member-create.json` — sanitized POST 201 and role.
- `../evidence/CP38-finops-member-state.json` — role binding and final suspended/hash-cleared state.
- `../evidence/CP38-finops-viewer-budgets.png` — read-only Budgets & Holds view.
- `../evidence/CP38-finops-viewer-chargeback.png` — read-only Chargeback view.
- `../evidence/CP38-finops-viewer-rbac.json` — session role, GET/PUT outcomes, UI action visibility, and unchanged project budget values.

## Limits

One non-owner role and one project budget were tested. Budget export actions were visible but were not exercised. Organization/environment/Virtual Key budget edits, chargeback filters/groupings, cross-tenant behavior, other mutation endpoints, roles and provider integrations remain UNVERIFIED. FinOps Viewer onboarding/password provisioning remains governed by DEF-021.
