# CP37 — Local member enrollment, reissue, invalidation, and replay

**Baseline:** frozen RC5 `3423d4f523ef0ac77469a1201b8b753d1d31c902`  
**Runtime:** isolated SQLite at `127.0.0.1:18116`; real Chromium; synthetic Org Owner and synthetic HUMAN Developer accounts  
**Disposition:** DEF-021 **VERIFIED FIXED for the tested local invite/reissue flows**. Expiry, mail delivery, explicit administrative revocation, SSO/OIDC and other-role flows remain unverified. FT-09 and SCR-13 remain **PARTIAL**. `GATE-REL-07` remains **BLOCKED**.

## Real-user and API journey

1. As Org Owner in organization-only scope, created a synthetic HUMAN Developer using Access & Keys → Members & Roles. The Console showed a pending activation row. A fresh invitation was issued through the row’s **Issue invite token** action. Token values were used only in process memory and are absent from screenshots and source control.
2. Reissued a second invitation for the same pending member. The owner-side endpoint returned HTTP 201 with the same member ID and a new token/expiry. Redeeming the superseded first token returned HTTP 401 `invalid_enrollment_token`.
3. An unrelated invalid token also returned 401 `invalid_enrollment_token`. A weak password against the new invitation returned HTTP 400 `weak_password` and did not consume the invitation.
4. Redeemed the current token with a strong synthetic password. Enrollment returned HTTP 200 and established a session. Replaying that token returned HTTP 401 `invalid_enrollment_token`.
5. A separate clean Chromium context signed in using the enrolled member's credentials and reached Overview. A separate Org Owner session read back `has_password: true`, `enrollment_pending: false`, and role `DEVELOPER`.

Visually inspected the [pending member row](../evidence/CP37-reissue-initial-pending.png) and [fresh member Overview](../evidence/CP37-reissue-fresh-login.png). Sanitized status/metadata readback is in [reissue and replay result](../evidence/CP37-reissue-revocation-result.json). A second account also completed reissue, invalid-token rejection, successful enrollment, replay rejection and owner readback; its result is [here](../evidence/CP37-enrollment-result.json). No enrollment token or password is included in either file.

## Defect disposition and limits

- DEF-021's core local enrollment path is independently verified on RC5. Reissue invalidated the superseded token; invalid credentials and consumed-token replay were denied; weak password was rejected without consuming the valid invitation; a fresh member session and persisted activation state were confirmed.
- The issued token had an expiry timestamp roughly 24 hours after creation. The expiry boundary itself was not tested. The email address used `.invalid`, so external mail delivery was not exercised. No explicit revoke action was found or tested; reissue served as invalidation in this scenario. SSO/OIDC, unauthorized cross-organization activation, alternate roles and other browsers remain open.
- No authorization bypass or new defect was found. No provider endpoint was used and provider spend remained USD 0. QA-003 remains in progress; `GATE-REL-07` remains **BLOCKED**.
