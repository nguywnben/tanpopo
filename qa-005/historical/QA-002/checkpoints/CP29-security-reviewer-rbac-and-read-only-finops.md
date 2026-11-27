# CP29 — Security Reviewer access and denied mutations

**Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`
**Runtime:** isolated SQLite Tanpopo at `127.0.0.1:18086`, Chromium, CP17 disposable tenant
**Release gate:** `GATE-REL-07` remains **BLOCKED**.

## Results

- **Role login and persistence — PASS for this synthetic principal.** A separate browser session authenticated as `qa002-cp28-reviewer@example.invalid`; session identified exactly `SECURITY_REVIEWER`. Credentials were provisioned out of band for QA because the Console Add member journey does not establish a local password or invitation. This does not count as onboarding/invitation coverage.
- **Allowed read surfaces — PASS for the tested scope.** Real navigation opened Security → Approval Queue, Access & Keys, and FinOps. Pending approvals, kill-switches, audit list, organization members, project/environment-scoped Virtual Keys, deployments, traces and budgets loaded successfully. The reviewer saw an empty approval queue, 18 scoped Virtual Key summaries (including revoked state, no plaintext), and 14 budget records. Screenshots were visually inspected.
- **Mutation boundaries — PASS for the tested negative paths.** The role's UI omitted Issue Key and budget-create controls; budget limit fields were `readonly` and `aria-disabled=true`. Direct same-session Admin API attempts used valid CSRF and valid payloads: `POST /admin/v1/virtual-keys` and `PUT /admin/v1/budgets/{id}` both returned `403 insufficient_role_permission`. Persistent checks after each attempt showed Virtual Key count and names unchanged at 18; budget count remained 14 and the first budget's ID, version 3 and 500,000-micro limit were unchanged.
- No new defect confirmed in the scoped Security Reviewer read/write boundary. Broader role coverage remains open; this is not evidence for FinOps Viewer, non-owner Operator, Developer or Team Admin behavior beyond earlier separately scoped cases.

## Evidence

- `CP29-reviewer-security.png` — approval queue and reviewer shell.
- `CP29-reviewer-access.png` — scoped key list and no issuance action.
- `CP29-reviewer-finops.png` — budget read view with visually disabled limits.
- `CP29-reviewer-rbac.json` — sanitized endpoint/status summary for successful reads; excludes session bodies and tokens.
- `CP29-reviewer-rbac-enforcement.json` — denied API status/codes plus before/after persisted counts and unchanged budget version/limit.

## Residual coverage

Approval decision and audit verification were exercised in CP28; CP29 retested readable Security surfaces without pending items. Still UNVERIFIED: cross-organization reviewer boundary, kill-switch trigger/clear by reviewer, virtual-key emergency revocation permissions, member role mutation denial, FinOps Viewer and other canonical roles, and normal invite/password/SSO onboarding. Source diff from the requested baseline remains empty; all state changes were confined to disposable test records and synthetic principal credential setup.
