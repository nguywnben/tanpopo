# CP75 — Security Reviewer denial on Operator-only organization provisioning

**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`  
**Runtime:** A2A-enabled isolated RC6 test clone `127.0.0.1:18128`  
**Role/browser:** synthetic `SECURITY_REVIEWER`, real Chromium at 1440×1000  
**Product source modified:** no

## Result

**PASS (scoped)** for enforcement of the documented Operator-only organization provisioning boundary before resource creation. The authenticated Security Reviewer submitted a direct browser-origin request to `POST /admin/v1/organizations` with an empty JSON body. RC6 returned HTTP 403 `insufficient_role_permission`, rather than processing the body or creating a row. The authorized organization list remained at one synthetic test organization. The real Security page was opened and visually inspected; it showed the reviewer's approval queue, kill-switch controls, and audit-log navigation. No organization-create control was presented in the visible page.

This negative invalid-body request is a narrow authorization check. It does not establish behavior for every endpoint or a valid organization payload. The six-role × endpoint matrix remains substantially UNVERIFIED.

## Reproduction

1. Sign in to exact RC6 as the isolated synthetic Security Reviewer.
2. Open the real **Security** screen; the screenshot shows the active role, Approval Queue, Kill Switches, and Audit Log tabs.
3. From the same authenticated browser session, request `POST /admin/v1/organizations` with `Content-Type: application/json`, the session CSRF token, and body `{}`.
4. Read the response and then `GET /admin/v1/organizations?limit=100`.

Sanitized response and post-request inventory are in [CP75 results](../evidence/CP75-reviewer-operator-boundary.json). The [Security Reviewer screen](../evidence/CP75-reviewer-operator-boundary.png) was visually inspected. The reviewer account is synthetic and uses an `.invalid` address; no credential is included in evidence.

## Expected versus actual

- **Expected:** only an OPERATOR principal can provision tenant organizations; other roles receive a permission denial.
- **Actual:** the Security Reviewer received HTTP 403 `insufficient_role_permission`; no organization was returned or created, and the one-row authorized inventory was unchanged.
- **Limits:** valid-payload requests, other roles, other organization lifecycle endpoints, action-level endpoint coverage, and role changes/session revocation remain UNVERIFIED. No new defect is assigned.

QA continues; **GATE-REL-07 remains BLOCKED**.
