# CP03 — Organization-only membership and local enrollment

**Baseline:** RC5 `3423d4f523ef0ac77469a1201b8b753d1d31c902`
**Status:** Scoped journeys completed in the isolated SQLite runtime; campaign remains in progress. `GATE-REL-07` remains **BLOCKED**.

## Organization-only and project-scope consistency

Using Chromium 156, sign in as the synthetic organization owner and open Access & Keys → Members & Roles. Use the top scope picker to select Default Organization → No project → No env. At the initial state, the Console showed one organization member and Admin API `/admin/v1/members?limit=100` returned one. Add synthetic HUMAN Developer principals. After each successful add, the Console list refreshed without a page reload. With six total principals, organization-only UI count and Admin API count both read six; switching to `default / dev` showed the same six organization members. Evidence: [organization-only list](../evidence/CP03-members-org-only-current.png), [project/environment list](../evidence/CP03-members-project-scope.png), [API/UI count evidence](../evidence/CP03-members-org-only-current.json) and [project-scope evidence](../evidence/CP03-members-project-scope.json).

One earlier scope-picker capture showed the menu remains open after selecting No project until dismissed; Escape dismissed it and the subsequent Members tab interaction worked. This was a normal open menu state, so no defect is asserted from that test harness interaction.

## Enrollment lifecycle

From organization-only scope, Add member created a HUMAN Developer with `Pending activation` state. The Console exposed a one-time invite token and activation link in the confirmation panel. In a clean browser context, using that link/token and setting a password completed activation. A further fresh browser context signed in as the new member and loaded Overview. The organization members list/API then showed `has_password=true`, `enrollment_pending=false`, role `DEVELOPER`. Screenshots [after member login](../evidence/CP03-member-login-overview.png) and [enrollment outcome](../evidence/CP03-member-enrollment-result.png) were visually inspected. Sanitized readback is [here](../evidence/CP03-member-enrollment.json); invite tokens are not present in branch files.

## Defect dispositions

- **DEF-021 — VERIFIED FIXED for the tested Console invite flow.** RC5 provides an explicit one-time activation path for a HUMAN member: issue invite, set password, and sign in in a fresh browser context. Additional mail delivery, expiry/reissue, revoked invite, SSO/OIDC enrollment and role-specific onboarding paths remain unverified.
- **DEF-023 — VERIFIED FIXED for organization-only and project/environment scopes.** The membership UI showed the same six members as the explicit-organization API with no project/environment selected and after switching to `default / dev`. The prior reported zero-versus-populated mismatch did not reproduce on RC5.
- No new product defect confirmed in this phase.

## Coverage

FT-09, FT-15 and SCR-13 receive partial coverage for organization membership, one role and scope switching. Other role boundaries, project-scoped roles, revocation, member deletion, security edge cases, alternate browsers and locale/viewport combinations remain unverified. QA-002 outcomes are not carried forward as RC5 passes.
