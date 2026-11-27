# CP73 — DEF-042 organization-member role picker scope

**Source:** exact frozen RC7 `737079ddb847d97dbcf832f610ed0048556cb65b`; Linux/amd64 runtime binary SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`; disposable SQLite clone `127.0.0.1:18162`; Chromium 156; authenticated synthetic Org Owner; organization-only scope `QA005 Org / No project / No env`.
**Disposition:** **PASS (scoped QA-005 regression).** The org-member role picker omits instance-only Operator, while the server independently rejects an authenticated direct API attempt with 403. No unsupported member was created. QA-003's prior failure is preserved as historical evidence.

## Reproduction and result

1. Used the real Console scope picker to choose `QA005 Org`, `No project`, and `No env`, opened Access & Keys → Members & Roles, then opened **Add organization member** and expanded **Assign roles**.
2. The visible organization options were Developer, Org Owner, Team Admin, Security Reviewer and FinOps Viewer. Operator was absent. The captured real Chromium screenshot was visually inspected.
3. As a defense-in-depth check, sent an authenticated, CSRF-protected `POST /admin/v1/members` scoped with `X-Tanpopo-Org-ID` to the same organization and payload `{email: qa005-def042-operator@example.invalid, kind: HUMAN, roles: [OPERATOR], project_ids: []}`. The API returned HTTP 403 `role_escalation_denied`, with `OPERATOR role cannot be bound inside an organization member`.
4. An explicitly scoped list read returned HTTP 200 and four members both before and after the rejected attempt. The canary email was absent.

Sanitized results are in [CP73 evidence](../evidence/CP73-def042-org-member-role-scope.json); the role-picker capture is [here](../evidence/screenshots/CP73-org-role-picker.png).

## Classification and limitations

This resolves the reproduced UI/server mismatch on one exact-RC7 Owner organization-scope path and independently verifies server-side rejection. It is not a full RBAC certification: other roles' role-option lists, member role edits, multiple organizations, Team Admin/project scope, cross-tenant item-ID attempts, other browsers and PostgreSQL/Valkey remain open. No member, invitation, credential or provider request was created. No implementation/specification changed. `GATE-REL-07` remains BLOCKED.
