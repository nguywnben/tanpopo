# CP18 — Standalone organization provisioning and audit partition

**Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`  
**Runtime:** isolated SQLite server on `127.0.0.1:18084`; actual Operator/Org Owner Console session.  
**Implementation changes:** none.  
**Release gate:** GATE-REL-07 remains **BLOCKED**.

## Create and persist

From Operations → Organizations, we opened the real create dialog and entered a synthetic organization name/slug. The Console showed a success banner and the row persisted in the tenant list. The create form and result were visually inspected in `evidence/CP18-organization-create-form.png` and `CP18-organization-created.png`.

We switched into the new empty organization using its row's Switch action. The workspace header showed the new organization with no project or environment. Using the in-app Security → Audit Log navigation (SPA state preserved), we filtered `organization.created`. The row had sequence 1, the acting principal as actor, and the newly created organization ID as target. Verify hash chain returned HTTP 200 and “Cryptographic RFC 8785 JCS chain intact across 1 events.” Screenshots `evidence/CP18-standalone-org-audit-filter.png` and `CP18-standalone-org-audit-verified.png` were visually inspected.

A separate unique organization capture recorded create HTTP 201, followed by a duplicate slug attempt returning HTTP 409 `resource_already_exists`; the list contained one additional organization and no duplicate. This confirms the UI/API failure path does not create a second row. Evidence is in `evidence/CP18-standalone-organization-audit.json` and `CP18-organization-status-create.png`.

## DEF-007 retest reconciliation

This closes the previously untested successful standalone-organization creation slice for this baseline. Combined with CP06 bootstrap and project/environment create plus duplicate project rejection, and CP16 project/environment creation and crypto-shred, all three original provisioning classes now have current-baseline successful event evidence with expected actor/target and verifiable chains. QA-001's missing-event reports remain preserved as historical evidence; this QA-002 result does not erase or reinterpret their original environment.

**Result:** DEF-007 is **VERIFIED FIXED (scoped)** for bootstrap and successful org/project/environment create paths, plus the exercised duplicate create boundaries. Broad audit integrity is not PASS: storage-write failure injection, organization/project/environment delete/update paths, approval handling, and cross-tenant access are still UNVERIFIED.

## Reproduction/evidence details

The standalone organization ID was `org_01m8541pqse00a0twk7y54zckg`; its only audit event was `organization.created`, sequence 1, actor principal `prc_01m84xhbtwe00939kr14qg3khv`, target type `organization`, target ID equal to the new org ID, entry point `admin_api`. A read-only SQLite inspection independently confirmed the event belongs to the new organization's partition. The duplicate slug attempt returned 409 and did not create a second organization. No organization was deleted during CP18.

The larger CP18 response/UI screenshots also include synthetic IDs only. No credentials, database files, or raw logs were added to the repository. QA-001 remains untouched and QA-002 continues.
