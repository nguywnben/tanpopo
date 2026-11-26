# CP22 — Operator role scope boundary

**Outcome:** PASS for preventing an organization member workflow from granting the instance `OPERATOR` role; dedicated instance-Operator behavior remains UNVERIFIED. No defect confirmed. GATE-REL-07 remains BLOCKED.

## Reproduction

The Org Owner opened Access & Keys → Members & Roles → Add member in the isolated canary project. The real role picker offered Org Owner, Team Admin, Security Reviewer, FinOps Viewer, and Developer; it did not offer Operator. A CSRF-protected request then attempted to add a synthetic `.invalid` member with `roles: ["OPERATOR"]` and the canary `project_id`. The server returned HTTP 403 `role_escalation_denied` with “OPERATOR role cannot be bound inside an organization member.” A fresh member list returned HTTP 200 and confirmed no member with that address was created. Evidence is `evidence/CP22-operator-role-options.png` and sanitized `evidence/CP22-operator-scope-boundary.json`.

The real screenshot was visually inspected. The 5-role list is consistent with the backend's instance-scope boundary: OPERATOR is not an organization/project grant. We do not classify its absence from this organization-member selector as a defect. Source inspection corroborates the route's denial and is explanatory evidence only; the disposition is based on the observed UI/API result.

## Coverage limits

This does not test a dedicated instance-Operator identity or role-specific Operations, organization provisioning, system audit partition, backup, restore, or multi-org session flows. The available bootstrap identity combines OPERATOR with ORG_OWNER; the supported member form does not provision a standalone instance Operator. Those behaviors remain UNVERIFIED and require an isolated instance-role provisioning path or a dedicated pre-provisioned Operator principal. No test member was created and no product data was changed.
