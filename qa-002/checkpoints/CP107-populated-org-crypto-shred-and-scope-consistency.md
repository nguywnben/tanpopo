# CP107 — Populated organization crypto-shred and scope consistency

**Baseline:** frozen RC4 `40bba5b55c2da6fc00b107b315c9b3167fb20169`<br>
**Runtime:** isolated QA server `18084`, SQLite WAL<br>
**Role/browser:** authorized Operator + Org Owner session, real Chromium at 1440 × 1000<br>
**Product implementation:** unchanged

## Scenario and results

Created a new synthetic tenant in the Operations Console and exercised its setup, member administration, key lifecycle and crypto-shred. The three pre-existing organizations were not deleted or modified.

1. In Operations → Organizations, used the real “Create organization” form to create `QA002 CP107 Populated Shred` with a unique test slug. The API returned HTTP 201, and the row appeared as the fourth organization. A fresh browser session switched into it through the organization list; the header showed the new organization and no project/environment.
2. Access & Keys → Members & Roles initially displayed `Organization members 0` / “No members found”. Added a synthetic HUMAN member `qa002-cp107-owner@example.invalid` with `Org Owner` through the Console. The API returned HTTP 201 and the UI said “Member added,” but the no-project table still showed zero. An authenticated `GET /admin/v1/members` with the selected organization header returned one persisted member. This reproduces **DEF-023 Moderate** in a new organization and dataset.
3. Opened the workspace selector and used New project to create `QA002 CP107 Disposable Project` (HTTP 201). Used New environment to create `stage` (HTTP 201). With the environment selected, Members & Roles displayed the same persisted owner member and role chips. The screenshots show the false-empty/populated contrast. The API `GET /admin/v1/projects`, project environments, members and virtual keys were checked with explicit `X-Tanpopo-Org-ID`; no cross-organization records were returned.
4. On Access & Keys → Virtual Keys, issued one environment-bound PROVIDER-only test key through the real Console. The list displayed it as Active; sanitized read-only API evidence stored ID, plane, environment and state but no secret field. The one-time secret was not persisted in evidence or used against a data-plane endpoint.
5. In Operations → Organizations, opened Crypto-shred for this synthetic organization. The dialog explicitly stated that the operation irreversibly destroys the organization DEK, purges secrets and keys, and cancels active sessions within 60 seconds. A wrong slug kept the action disabled; the exact slug plus a nonempty justification enabled it. Both states were visually inspected. Submitted the operation against only the new synthetic organization.
6. The DELETE returned HTTP 200 and `revoked_virtual_keys_count:1`; the UI removed the organization and the fresh list/API no longer contained its ID or slug. After reauthentication, requests explicitly scoped to the deleted organization returned HTTP 200 with empty project, member and virtual-key collections; the organization inventory contained the three pre-existing records only.
7. Queried the deleted organization’s audit partition with both `org_id` and `chain_partition`. Six events remained in sequence: `organization.created`, `member.created`, `project.created`, `environment.created`, `virtual_key.created`, and `organization.crypto_shredded`. The final event recorded one revoked key. The chain verifier returned `intact:true`, checked six events, and no broken sequence.

One initial audit query omitted `org_id` while selecting the deleted organization’s chain partition and returned HTTP 400. The matching verification request likewise returned 400. The request was corrected by explicitly supplying `org_id` and `chain_partition`; both the event list and chain verification then succeeded. These were harness query errors, not Tanpopo defects.

## Outcome and defect reconciliation

- **DEF-023 remains confirmed Moderate:** the persisted organization-wide member was hidden at org-only scope after a successful UI create; the same member appeared after project/environment selection. This independently extends CP39/43/71/80/100/101 evidence. No authorization bypass was observed.
- **DEF-007 advances to a populated success-path PASS:** a member, project, environment and active key existed before the real crypto-shred; one active key was revoked; the resource lists were empty after deletion; and all six create/delete events remained in an intact chain. DEF-007 remains partial/open because audit/storage failure atomicity, injected faults, backup/ciphertext destruction proof, session-cancellation timing, and broader cross-tenant cases were not tested.
- **DEF-021 was not retested:** creating the synthetic HUMAN owner does not establish a supported first-login enrollment path.
- **New defects:** none. The failed first audit query was corrected and is excluded from defect classification.

## Evidence

- `../evidence/CP107-operations-overview.png`, `CP107-organizations-list.png`, `CP107-org-create-dialog.png`, `CP107-org-created.png` — Operations state and new organization creation; visually inspected.
- `../evidence/CP107-neworg-members-org-only.png`, `CP107-neworg-add-member-form.png`, `CP107-owner-member-selected.png`, `CP107-neworg-member-added.png`, `CP107-member-visible-with-env.png`, `CP107-workspace-picker.png` — actual role/member/workspace UI states; visually inspected. The screenshot `CP107-neworg-member-added.png` shows success feedback alongside the false-empty count 0.
- `../evidence/CP107-project-create-filled.png`, `CP107-project-created.png`, `CP107-environment-create-filled.png`, `CP107-neworg-vkeys-empty.png`, `CP107-key-issue-dialog.png`, `CP107-populated-key-persisted.png` — project/environment creation and key state; visually inspected. No screenshot contains the one-time key secret.
- `../evidence/CP107-populated-org-before-shred.json` — exact pre-shred organization/project/environment/member/key state; no secret value.
- `../evidence/CP107-shred-wrong-confirmation.png` and `CP107-shred-exact-confirmation.png` — destructive confirmation disabled/enabled states; visually inspected.
- `../evidence/CP107-populated-shred-results.json` — sanitized HTTP/UI result, revoked key count, inventory and corrected-query note; no opaque DEK receipt value.
- `../evidence/CP107-populated-shred-audit.json` — six retained audit events and intact-chain verification.
- `../evidence/CP107-postshred-access.json` — explicit deleted-organization scoped reads after shred.
- `../evidence/CP107-populated-org-pre-shred-operations.png` and `CP107-populated-org-shredded.png` — real Operations inventory before and after; screenshots visually inspected. The success receipt is masked in the immediate post-action capture and absent from the clean post-reauthentication screenshot.

## Consolidated status summary

- **Historical defects verified fixed:** DEF-001–004, DEF-006 and DEF-008–015 retain scoped QA-002 outcomes; CP104 expanded DEF-011. DEF-005 remains PARTIAL. No QA-001 PASS is inherited.
- **Historical defects still open or unverified:** DEF-007 remains partial after this successful populated erase path; fault atomicity and physical/backup erasure proof remain UNVERIFIED. DEF-016–036 remain open; DEF-023 was reconfirmed; DEF-037 Low remains confirmed from CP105. No other disposition changed.
- **New defects:** none.
- **Coverage completed:** Operations tenant provisioning; organization switching; member role grant; project/environment creation; active key provisioning; wrong/exact slug guard; populated tenant crypto-shred; active-key revocation; organization/resource list absence; persisted audit and chain verification; real Chromium visual inspection.
- **Coverage blocked / remaining:** failure injection, backup restore implications, ciphertext/DEK destruction proof, active session cancellation timing, secrets/webhook resource deletion, four-eyes policy, and other roles/browsers remain unverified. No cross-tenant mutation was attempted.
- **Recommended engineering handoffs:** continue DEF-023 workspace-scope correction; retain DEF-007 audit/erasure fault and proof requirements; no change to DEF-005 SSRF or authorization boundaries.
- **Release gate:** `GATE-REL-07` remains **BLOCKED**.
