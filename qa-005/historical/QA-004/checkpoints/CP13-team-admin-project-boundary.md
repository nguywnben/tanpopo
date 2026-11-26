# CP13 — Team Admin project-scoped member workflow and escalation denial

**Outcome:** PASS for the tested same-project member grant and role-escalation boundary. Cross-project isolation and the full Team Admin lifecycle remain unverified.

## Real invitation and permitted project action

1. An Org Owner used the real Members form to assign only `TEAM_ADMIN` to a synthetic `.invalid` identity, scoped to project `default`. The role and scope selection are visible in `CP13-teamadmin-invite-form.png`.
2. Activated the invitation and signed in as Team Admin. The actual Add organization member form exposed only `Team Admin` and `Developer` as grantable roles, with `default` shown as its project scope. The screenshot was captured before any further action in the form.
3. Created a second synthetic Developer using that form and activated the invite. An Owner API readback shows both principals persisted under project `default` with the intended role and password enrollment complete (`CP13-teamadmin-persistence.json`).

## Denied role escalation

From the Team Admin session, attempted a credentialed, CSRF-protected `POST /admin/v1/members` assigning `ORG_OWNER` to a synthetic canary identity in the same project. The current RC6 API returned HTTP 403 `role_escalation_denied`. A subsequent Owner list read found no canary member. The request result and persistent-state check are in `CP13-teamadmin-results.json` and `CP13-teamadmin-persistence.json`.

No role boundary was relaxed and no upstream request or spend was involved. The enrollment token and synthetic passwords remain outside the repository.

## Visual evidence

Visually inspected the Owner assignment form and Team Admin member form. The latter visibly limits role choices to Team Admin and Developer and shows the project scope `default`. No one-time token was captured in the evidence.

## Remaining coverage

Cross-project and cross-org reads/writes, sibling project member scope, team admin removal/revocation, allowed policy/route changes, secrets/provider isolation, Security Reviewer and Operator boundaries, all-role refresh/scope switching, and concurrent role changes are still `UNVERIFIED`.
