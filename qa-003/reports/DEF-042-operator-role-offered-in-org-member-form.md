# DEF-042 — Organization member form offers the instance-only Operator role

- **Severity:** Low
- **Status:** Confirmed Console UX/business-contract defect; backend authorization correctly rejects the unsupported binding
- **Baseline:** frozen RC5 `3423d4f523ef0ac77469a1201b8b753d1d31c902`
- **Affected workflows:** Access & Keys → Members & Roles → Add member; Operator tenant onboarding and role administration
- **Checkpoint:** [CP22](../checkpoints/CP22-operator-rbac-and-tenant-provisioning.md)

## Reproduction

1. Sign in as the synthetic bootstrap Operator/Org Owner and switch to the isolated synthetic tenant `qa003-operator-cp22`.
2. Open Access & Keys → Members & Roles → **Add member**.
3. Clear the default Developer role, open Assign roles, and select **Operator**. The role appears in the organization-member picker. The role list is visible in [picker options](../evidence/CP22-owner-role-menu.png), and the selected invalid choice is shown in the [role form](../evidence/CP22-operator-role-option-form.png).
4. Enter a synthetic `.invalid` email and submit.
5. `POST /admin/v1/members` returns HTTP 403 `role_escalation_denied`, message `OPERATOR role cannot be bound inside an organization member`. The drawer remains open with Operator selected and the error displayed. See [rejected form](../evidence/CP22-operator-role-rejected.png).
6. Read `/admin/v1/members?limit=100` with explicit `X-Tanpopo-Org-ID: <synthetic tenant id>`: HTTP 200, two rows, and no canary member. The initial unscoped read was recognized as querying the bootstrap account's Default Organization and is excluded as tenant evidence. Sanitized records: [UI/API behavior](../evidence/CP22-operator-role-ui-result.json), [explicitly scoped member count](../evidence/CP22-operator-owner-provisioning-result.json).

## Expected vs actual

- **Expected:** An organization-level member form offers only roles that can be bound at organization scope, or clearly explains any instance-scoped role before submit and points to its supported provisioning flow.
- **Actual:** The form presents Operator as an assignable organization role. The API rejects it only after submission with a clear but raw RBAC error. No role binding was created.

## Business impact and security boundary

This blocks an attempted workflow and gives a misleading role choice, causing wasted setup time and support confusion during tenant onboarding. The tested authorization boundary held: the server returned 403 and no member row appeared. This matches the explicit rejection of an Operator role in an organization member binding in [`rbac.go`](../../internal/admin/http/rbac.go#L926-L927); do not relax that boundary to accommodate the picker. This finding is **not** an authorization bypass and does not justify weakening the instance-scope restriction.

## Facts and root-cause hypothesis

**Facts:** The browser rendered Operator in the org-member role picker; a real submission returned 403 `role_escalation_denied`; the explicit-tenant member read stayed at two rows and did not contain the synthetic canary.

**Hypothesis:** The shared member form likely renders the full canonical role list without filtering by the selected binding scope. Confirm against the role-picker data source before changing behavior.

## Acceptance criteria

1. Hide or disable Operator when creating or editing an organization-scoped member, while retaining server-side scope validation.
2. If visible for discoverability, label it as instance-scoped before submission and identify the supported instance-level provisioning path.
3. Show only assignable roles for the current scope, and present a localized actionable message for any server-side rejection.
4. Add a real-browser regression for each canonical role at organization/project/instance scope, verifying UI choices, API authorization, and persisted bindings.

## Remaining limits

Only a synthetic Operator/Org Owner session, one SQLite tenant, Chromium 156 desktop, and the Add member creation flow were tested. Role editing, OIDC role mapping, instance-level provisioning, project-bound role assignment, alternate persistence engines, and other browsers remain open. The rest of QA-003 remains incomplete; `GATE-REL-07` remains **BLOCKED**.
