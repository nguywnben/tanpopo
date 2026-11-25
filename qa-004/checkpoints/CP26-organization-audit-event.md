# CP26 — Organization creation audit event

**Historical finding:** DEF-007, already PASS for the recorded setup and project/environment create paths. **CP26 result:** independent additional successful organization-provisioning path; no defect confirmed. Fault atomicity and other resource/role paths remain UNVERIFIED. GATE-REL-07 remains BLOCKED.

## Reproduction and evidence

The isolated instance's combined bootstrap Operator + Org Owner created `QA-004 Cross Tenant Boundary` through the real Operations → Organizations form. A separate synthetic `ORG_OWNER` then signed into that tenant and opened Security → Audit Log. Its audit API returned HTTP 200 with seven events. Exact UI action filter `organization.created` showed one entry with target type `organization` and target ID matching the newly created tenant. The event was recorded in the new tenant's own audit partition as sequence 1. The **Verify hash chain** action reported “Cryptographic RFC 8785 JCS chain intact across 7 events.”

Visually inspected screenshots are `evidence/CP26-org-created-audit-filter.png` and `evidence/CP26-org-audit-verified.png`. Sanitized target and verification details are `evidence/CP26-org-audit.json`.

## Classification and limits

This is positive fresh evidence for the successful organization-create audit path and is consistent with DEF-007's scoped PASS. The actor was a combined bootstrap identity; this does not isolate standalone OPERATOR attribution. Failure injection, storage/audit atomicity, duplicate/invalid requests, project and environment creation under other roles, and audit behavior during multi-org writes remain UNVERIFIED.
