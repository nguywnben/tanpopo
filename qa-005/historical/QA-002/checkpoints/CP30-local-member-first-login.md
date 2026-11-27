# CP30 — Local member creation to first login

**Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`
**Runtime:** isolated SQLite Tanpopo at `127.0.0.1:18086`, Chromium, standalone local-auth tenant
**Release gate:** `GATE-REL-07` remains **BLOCKED**.

## Result — FAIL DEF-021

- From a fresh browser session as the owner, created `qa002-cp30-local-member@example.invalid` as a `HUMAN` with `DEVELOPER`. The role picker visibly selected Developer; submission returned HTTP 201; the Console showed “Member added.” and the member list persisted the role.
- In an isolated browser context, attempted the new member's normal local sign-in. The actual `POST /admin/v1/auth/login` returned HTTP 401 `invalid_credentials`; the screen showed “Sign-in failed.” No invite, password setup, reset, or sign-up action was visible. No OIDC provider was configured.
- Product sources place six-role/local authentication in v0.1 Must and enterprise OIDC in v0.3 Could; however, the precise onboarding contract is incomplete. DEF-021 is limited to the confirmed local-only member workflow and records that uncertainty explicitly.
- The throwaway password was never issued to the new member and is not in evidence. No product source was changed.

## Evidence

See `../reports/DEF-021-member-added-without-supported-local-enrollment.md` and:

- `../evidence/CP30-member-create-form.png`
- `../evidence/CP30-member-created.png`
- `../evidence/CP30-member-first-login-form.png`
- `../evidence/CP30-member-first-login-denied.png`
- `../evidence/CP30-member-provisioning.json`
- `../evidence/CP30-member-isolated-login.json`

## Coverage update

WF-01 setup of the bootstrap owner remains PASS for its prior scoped scenarios. New-member local enrollment now FAILS. Existing role permission checks remain separately scoped; newly added account lockout, invalid/expired invite, SSO enrollment, and service-account onboarding remain UNVERIFIED.
