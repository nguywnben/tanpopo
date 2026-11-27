# CP20 — Security Reviewer scope and emergency actions

**Baseline:** RC5 `3423d4f523ef0ac77469a1201b8b753d1d31c902`
**Status:** Scoped Security Reviewer permissions passed on an isolated SQLite clone using real Chromium UI and same-origin Admin API interactions. Broader role matrix remains incomplete. QA-003 remains in progress; `GATE-REL-07` remains **BLOCKED**.

## Test environment and role provisioning

1. Created a transactionally consistent clone of the restored SQLite database and started the RC5 binary on `127.0.0.1:18116`. Org Owner signed in through the Console.
2. In Access & Keys → Members & Roles, added synthetic HUMAN principals with only the `Security Reviewer` role and activated them through the one-time enrollment flow. Enrollment tokens and passwords remained in memory and were not saved to screenshots, logs, or this branch. The invite form and post-activation role scope are shown in [invite form](../evidence/CP20-reviewer-invite-form.png) and [pending activation](../evidence/CP20-reviewer-pending-activation.png).
3. Using an independent signed-in Security Reviewer session, visited Access & Keys → Members & Roles. The Console returned the organization member list and omitted the Add member action. A same-origin `POST /admin/v1/members` with a valid session CSRF token returned HTTP 403 `insufficient_role_permission`; no canary member was created. Readback showed HTTP 200 for member listing. Evidence: [reviewer member view](../evidence/CP20-reviewer-members-view.png) and [sanitized authorization result](../evidence/CP20-reviewer-authorization-result.json).
4. Reviewed Security → Audit Log as the Security Reviewer; the list rendered and `GET /admin/v1/audit-events` returned HTTP 200. The actor identity and audit rows are visible in [audit log screenshot](../evidence/CP20-reviewer-audit-log.png).

## Authorized emergency actions

1. Security Reviewer opened Security → Kill Switches, reviewed the project blast-radius preview, triggered a Project-level switch on the synthetic `default` project, then cleared it through the Console with an audit reason. Trigger and clear succeeded; the final view showed zero active switches. See [security center](../evidence/CP20-reviewer-security-center.png), [blast-radius preview](../evidence/CP20-reviewer-killswitch-preview.png), [active switch](../evidence/CP20-reviewer-killswitch-triggered.png), and [cleared state](../evidence/CP20-reviewer-killswitch-cleared.png).
2. Org Owner issued an environment-bound synthetic Virtual Key with only `/v1/*` allowed. The one-time secret modal was not captured in a screenshot. The Security Reviewer saw key metadata and selected **Emergency security revoke (abort active streams)**. After exact key-name confirmation and a reason, the UI showed the key as Revoked and “0 active streams severed.” Read-only SQLite metadata showed `state=REVOKED` and `revocation_mode=EMERGENCY_SECURITY`. See [visible metadata](../evidence/CP20-reviewer-vkey-visible.png), [confirmation](../evidence/CP20-reviewer-vkey-emergency-confirm.png), [revoked state](../evidence/CP20-reviewer-vkey-revoked.png), and [sanitized result](../evidence/CP20-reviewer-vkey-revoke-result.json).

## Test-harness credential handling incident

During an earlier attempt to create a synthetic key, a local Playwright diagnostic printed the full one-time key modal text into tool output. That synthetic key was immediately revoked by the Org Owner using the emergency revocation UI; readback confirmed `REVOKED` / `EMERGENCY_SECURITY`. The modal was never captured as an evidence image, the secret was not written to a file or committed, and the later Security Reviewer revocation used a separately generated key without printing or storing its plaintext. The incident was caused by the QA harness logging an entire secret-bearing UI modal, not by Tanpopo. It does not establish a product defect; this record is included to preserve the QA evidence trail without reproducing the credential.

## Expected versus actual and defect disposition

- **Documented contract:** SCR-13 describes `SECURITY_REVIEWER` as read-only for audit and emergency key revoke; SCR-14 authorizes reviewer kill-switch actions within scope. The implementation role map explicitly allows `ActionMemberRead`, `ActionVKeyRead`, `ActionVKeyRevoke`, `ActionAuditReadVerify`, and `ActionKillSwitchTrigger`, but not `ActionMemberWrite` ([RBAC role map](../../internal/iam/rbac/authorizer.go#L286-L301)).
- **Actual:** Read routes returned 200, member creation returned 403 `insufficient_role_permission` after CSRF validation, the UI omitted Add member, and the documented kill-switch and emergency-revoke actions succeeded and persisted. Read-only access to the full organization member list is also available.
- **Contract ambiguity:** The role map permits member-list reads, while SCR-13 and the business entity map describe audit inspection and emergency actions without explicitly stating whether reviewer access to member emails and role assignments is intended. This is a documentation gap, not a confirmed authorization defect: no contract explicitly denies the read, and the action-level role map allows it. Product/engineering should state the intended data visibility and add it to the role acceptance matrix.
- **Disposition:** **PASS for the exercised action-level authorization paths; no new Tanpopo defect confirmed.** This does not close the broader six-role matrix, project/team scope variants, non-owner key lists, graceful drain, active-stream termination, or alternate persistence engines.

## Coverage updates

Extends **PARTIAL** coverage for FT-09, FT-12, SCR-13, SCR-14, WF-10, and WF-12, and Chromium 156 desktop/Linux. It verifies one organization-scoped Security Reviewer path only. Operator, Team Admin, Developer, and FinOps Viewer boundaries; cross-organization isolation; project-scoped reviewer bindings; active stream termination; and PostgreSQL/Valkey behavior remain open. Historical defect totals are unchanged: 17 scoped VERIFIED FIXED, 3 PARTIAL, 19 UNVERIFIED. Existing DEF-040 and DEF-041 remain open. QA-003 remains incomplete.
