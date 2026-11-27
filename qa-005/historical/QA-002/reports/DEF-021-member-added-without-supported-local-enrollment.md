# DEF-021 — Added local human member has no supported enrollment path

**Severity:** Moderate
**Status:** Confirmed for a standalone local-auth deployment on baseline `40bba5b55c2da6fc00b107b315c9b3167fb20169`.
**Release gate:** `GATE-REL-07` remains **BLOCKED**.

## Summary

The owner can add a `HUMAN` principal and assign a role. The Console confirms “Member added.”, but the workflow provides no password enrollment, invitation, first-login setup, or recovery action. In a fresh local-auth browser context, the new member cannot authenticate and receives `401 invalid_credentials`. Enterprise OIDC is scoped as v0.3 in the product requirements, and no OIDC provider was configured in this v0.1-style standalone runtime.

This finding is about the unsupported local member onboarding contract. It does not prescribe setting or sending a plaintext password.

## Reproduction

1. Start from the isolated bootstrapped SQLite instance at `127.0.0.1:18086` on the QA-002 source baseline. Sign in as the owner.
2. Open **Access & Keys → Members & Roles → Add member**. Enter `qa002-cp30-local-member@example.invalid`; retain the selected `Developer` role and submit.
3. Observe HTTP `201`, the “Member added.” notice, and a persisted `HUMAN` member row with role `DEVELOPER`. See `../evidence/CP30-member-create-form.png`, `CP30-member-created.png`, and `CP30-member-provisioning.json`.
4. In a separate clean browser context, open `/login`. The page offers email/password sign-in and first-time setup for a new instance, with no invite, account-activation, password-reset, or new-account action. Enter the new member email and a throwaway synthetic password that was never given to that user. The real login request returns `401 invalid_credentials`; the page shows “Sign-in failed.” See `CP30-member-first-login-form.png`, `CP30-member-first-login-denied.png`, and `CP30-member-isolated-login.json`.
5. The member record remains listed with its assigned role. No OIDC provider was configured. The public local recovery command is `tanpopo admin reset-local-owner`, which is owner-specific; no supported local member enrollment/reset route was found in the user-visible login flow or Admin OpenAPI contract.

## Expected versus actual

- **Expected:** a standalone local-auth operator can complete the supported onboarding of an added `HUMAN` member, or the product clearly states that an external identity prerequisite must be configured before adding one. A first-login mechanism should use a time-bounded setup token or another secure credential enrollment path and preserve role boundaries.
- **Actual:** the API creates a role-bearing human principal and the Console reports success, but does not provide the principal with a usable local credential or next step. The only tested local login rejects, and the login page provides no recovery path. The assigned role is operationally unusable.

## Business impact

Teams cannot complete the local-only member onboarding journey or have the new Developer use the Console. Administrators may resort to direct database password-hash edits, which bypass the supported product workflow and its likely audit/security controls. This affects every newly added local human principal in the tested configuration, not the existing bootstrap owner.

## Facts and root-cause hypothesis

**Facts:** UI `POST /admin/v1/members` returned `201`; member list retained a `HUMAN` principal with `DEVELOPER`; member creation returned no invite or credential material; isolated login returned `401 invalid_credentials`; no recovery action was present on the login page; OIDC is marked v0.3 in `TP-FR-0504` while local authentication/RBAC are v0.1 Must features.

**Hypothesis:** member creation provisions the principal and RBAC row without a companion local credential enrollment state or notification flow. The existing recovery CLI may intentionally target only the bootstrap owner. This likely explains the gap but is not established by source inspection alone.

**Contract uncertainty:** `TP-FR-0502` / `TP-AC-F0502` require local admin authentication and six-role principals, while the endpoint contract only promises adding a principal and role binding. It does not state whether all HUMAN members must authenticate locally or how a local-only user should be enrolled. If member assignment intentionally supports only pre-existing external identities, the UI and release documentation currently do not communicate that prerequisite.

## Acceptance criteria

- In a standalone local-auth deployment, complete the member journey through a supported, audited, secure first-login enrollment mechanism; never return or email a long-lived plaintext password.
- Clearly communicate invite/setup-token expiry, reissue, and pending/active status; do not show “Member added” as an actionable completion if no supported identity path exists.
- The member can complete first login in a separate browser, receives only the assigned roles, and can access allowed screens while forbidden writes remain `403`.
- Invalid, expired, replayed, and cross-organization enrollment tokens fail closed and are audit-visible without leaking token material.
- If only external identities are supported in a mode, require/confirm that identity provider and show the prerequisite before principal creation; preserve `SERVICE_ACCOUNT` behavior separately.


## Independent runtime retest — CP110

CP110 repeated the local-only onboarding journey on the separate 18084 runtime and exact frozen baseline. The real Console form again offered only email, role, and scoped project; create returned 201 and “Member added.” with a HUMAN Developer role but no invitation or credential setup. In a fresh unauthenticated browser context the same new principal's local sign-in returned 401 and the login UI had no invite, activation, or password-reset path. The new first-login failure is independently reproducible; the intended local-vs-external identity contract remains uncertain as stated above.

CP110 also confirmed the existing DEF-023 false-empty state: after the successful create, organization-only/project-without-environment UI remained at zero while an explicit-organization members GET returned five; project + `stage` displayed those five persisted records. See [CP110](../checkpoints/CP110-local-member-enrollment-independent-runtime.md) and sanitized visual/request evidence `../evidence/CP110-local-member-onboarding.json` plus its linked screenshots. The test member is synthetic and remains in the isolated 18084 database because neither UI nor documented Admin API exposes a member-delete operation; no direct database edit was used.

## Evidence

- `../evidence/CP30-member-create-form.png` — actual member drawer with a selected role and no credential/invitation fields.
- `../evidence/CP30-member-created.png` — visually inspected success notice and persisted member list.
- `../evidence/CP30-member-first-login-form.png` — separate browser local login screen; no invite/reset/signup action.
- `../evidence/CP30-member-first-login-denied.png` — visually inspected failed local sign-in.
- `../evidence/CP30-member-provisioning.json` — sanitized creation and persistence summary.
- `../evidence/CP30-member-isolated-login.json` — isolated login `401 invalid_credentials`; no credential values.

QA-002 remains in progress. No release approval is implied.
