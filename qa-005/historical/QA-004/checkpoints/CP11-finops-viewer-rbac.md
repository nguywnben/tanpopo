# CP11 — FinOps Viewer read/write boundary

**Outcome:** PARTIAL RBAC coverage; one current RC6 `FINOPS_VIEWER` role was independently provisioned and exercised. The six-role and tenant/scope matrices remain open.

## Real member journey

On the isolated RC6 clone, an Org Owner used Access & Keys → Members to assign the FinOps Viewer role to a synthetic `.invalid` address. The invite was activated with a generated test password, then the invited user signed in separately. No credential or invite token is retained in the QA branch. The actual invite form, member result, and FinOps Viewer screenshots are included in `evidence/CP11-finops-viewer-*`.

## Authorized and denied actions

- FinOps Viewer opened the real FinOps screen. `GET /admin/v1/budgets` returned HTTP 200 and one project budget. `GET /admin/v1/usage/summary?group_by=model` returned HTTP 200 with one synthetic model row.
- Attempted to update that existing budget using its current version. The API returned HTTP 403 `insufficient_role_permission`. A subsequent authorized read showed the hard limit remained 100 micro-USD at version 5; no write persisted.
- Direct `GET /admin/v1/members` returned HTTP 403 `insufficient_role_permission`.
- Navigated directly to the Security route. The Console displayed “Access restricted” and identified the assigned FinOps Viewer role. The Security and Members links were absent from the visible navigation.

Sanitized outcomes are in `evidence/CP11-finops-viewer-results.json`. No source change or external provider call was made.

## Visual evidence review

Visually inspected `CP11-finops-viewer-finops.png` and `CP11-finops-viewer-security-route.png`: the populated budget/chargeback view renders for the role, while the protected route presents an explicit denial with no data leakage. Invite form and pending-state screenshots show the synthetic test identity and selected role only.

## Remaining role/security coverage

Org Owner, Team Admin, Security Reviewer, Developer, and Operator remain unverified as role-specific journeys. FinOps Viewer project isolation, scope changes, exports, session revocation, and alternate-role composition remain open. No overall RBAC acceptance is claimed.
