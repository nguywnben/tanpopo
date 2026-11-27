# CP54 — `FINOPS_VIEWER` RBAC boundary and fresh DEF-080 reproduction

**Frozen source:** `737079ddb847d97dbcf832f610ed0048556cb65b`
**Runtime:** exact-source local Linux/amd64 binary, SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`; PostgreSQL 16 + three-node Valkey 8.1; Control Plane `127.0.0.1:18151`.
**Identity/browser:** Chromium, real Owner and synthetic enrolled `FINOPS_VIEWER` in QA005 CP52 Recovery / Recovery Project / dev.
**Disposition:** accepted role-boundary checks **PASS (scoped)**; DEF-080 **FAIL, independently reproduced**; PG-001 evidence expanded.

## Workflow

In the Owner Console, opened Access & Keys → Members & Roles → Add member and invited `qa005-cp53-finops@example.invalid`. The form initially selected Developer; selecting FinOps Viewer left both roles selected. The Owner used Edit roles to clear Developer, saved `FINOPS_VIEWER` only, then redeemed the local one-time invitation and set a synthetic password through the real enrollment form. The activation request returned HTTP 200. Screenshots show the role selection, saved pure role, and activated account; the one-time enrollment token and passwords are excluded.

To establish the counterfactual for DEF-080, the Owner created an environment-scoped PROVIDER Virtual Key named `CP54 DEF-080 inventory counterfactual`. The Owner inventory showed one active key in the same environment, last four `KeMa`. No matching provider route existed, and no provider request was made. The Owner then signed out and the actual `FINOPS_VIEWER` session signed in.

## Results

- FinOps was usable. The real Budgets & Holds screen loaded; its usage-summary and budgets API requests returned HTTP 200. Direct `GET /admin/v1/budgets` also returned 200.
- The direct API calls `GET /admin/v1/traces`, `GET /admin/v1/traces/{requestId}`, `GET /admin/v1/traces/{requestId}/debug-payload`, `GET /admin/v1/kill-switches`, `GET /admin/v1/virtual-keys`, and `GET /admin/v1/members` returned HTTP 403 `insufficient_role_permission`. The trace detail used a deliberately nonexistent synthetic request ID; authorization denied it before revealing resource existence. Traces and Security direct UI routes showed an explicit “Access restricted” state. This is a scoped PASS for the relevant accepted `TP-IMP-0030` / `DEC-050` RBAC invariant. No mutation endpoint was attempted.
- **DEF-080 reproduced:** the viewer's Virtual Key API read returned HTTP 403, while the screen showed that red permission alert alongside `Environment Virtual Keys 0` and `No Virtual Keys in this environment`. The Owner had just verified a same-scope key was active. The UI therefore presents an authoritative empty inventory despite both a denied read and a known existing key. The data-plane key secret was not displayed or recorded. DEF-080 remains Low / FAIL: this is misleading presentation; the authorization boundary held and no protected key row was disclosed.
- After capture, the Owner revoked the synthetic key through the real Console. The success state said `0 active streams severed`, and the row showed `Revoked`. No QA key remains active from this test.
- The preselected Developer role in Add Member required an extra explicit role-edit step to provision a pure FinOps Viewer. This expands existing [PG-001](../PRODUCT-GAP-REGISTER.md); absent an accepted default-role requirement, it remains a product-gap observation rather than an implementation defect.

Detailed sanitized results and screenshot paths are in [CP54 JSON](../evidence/CP54-finops-viewer-boundary-and-def080.json). The invitation token, passwords, and one-time key secret are excluded. No new defect ID was assigned; historical defect counts are unchanged. QA continues; **GATE-REL-07 remains BLOCKED**.
