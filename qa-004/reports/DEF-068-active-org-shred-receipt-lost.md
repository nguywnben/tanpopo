# DEF-068 — Active-organization crypto-shred success receipt disappears

**Severity:** Moderate  
**Status:** FAIL (confirmed on frozen RC6)  
**Affected workflow:** Operations → Organizations → crypto-shred of the currently selected tenant  
**Baseline:** `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`

## Summary

When an Operator crypto-shreds the currently selected organization through the Console, Tanpopo completes the irreversible deletion and records its audit receipt, but the UI does not show the expected success receipt. The Console switches to a surviving organization after deletion; that scope change remounts the keyed workspace page and clears the `receipt` state before it can be reviewed.

The same workflow on an inactive synthetic organization keeps the active scope and visibly shows the receipt banner. This A/B control narrows the defect to the active-organization deletion path. The API, DEK-destruction receipt, metadata cleanup, and audit chain succeeded in the canary. The issue is the missing user-visible confirmation after an especially destructive action, not evidence that shredding failed.

## Reproduction

1. On the isolated exact-RC6 runtime, create a new synthetic organization through Operations → Organizations.
2. Switch into it; use the actual workspace picker to create a project and environment.
3. In Providers → **Seal secret**, seal a project-scoped API-key-kind credential with a clearly synthetic, non-routable value. Do not create a provider connection or send an inference request.
4. Refresh the Vault Secrets view. Confirm the metadata remains and no plaintext value is returned.
5. In Operations → Organizations, use that active organization row's **Crypto-shred** action. Enter a reason and the exact slug, then confirm.
6. Observe HTTP 200, the organization disappearing, and the Console switching to a surviving organization. The Operations page contains no success banner or destroy receipt.
7. Query the audit partition for the deleted organization. The `organization.crypto_shredded` event exists and the tenant hash-chain verifier returns HTTP 200 with `intact: true`.
8. Control: create a second empty synthetic organization while scoped elsewhere and crypto-shred its inactive row. The Console displays the success receipt and preserves the current scope.

The sanitized exact outcomes, IDs, and limitations are in [`CP67-crypto-shred-results.json`](../evidence/CP67-crypto-shred-results.json). Screenshots were captured during actual Chromium interactions and visually inspected.

## Expected versus actual

| Expected | Actual |
|---|---|
| After successful active-organization shredding, the operator sees a durable confirmation showing the destroyed DEK identifier and revoked-key count, as the Operations page does for inactive-organization shredding. | The target row disappears and scope changes; the receipt/banner is absent. The operator must query the audit log to confirm completion. |
| The user can distinguish a completed irreversible operation from an interrupted or failed one without leaving the workflow. | The UI reports no completion result for this active-target case even though the server returned HTTP 200 and persisted `organization.crypto_shredded`. |
| Other tenants remain unaffected. | This test preserved both surviving organizations; the root organization still returned its two projects. |

## Evidence and confirmed mechanics

- [Sanitized results and API readbacks](../evidence/CP67-crypto-shred-results.json)
- [Canary tenant and Crypto-shred action before deletion](../evidence/CP67-org-directory-before-shred.png)
- [Exact-slug confirmation dialog](../evidence/CP67-crypto-shred-confirmation.png)
- [Active organization removed, but no receipt/banner](../evidence/CP67-active-org-shred-no-receipt.png)
- [Inactive control: receipt visible and active scope preserved](../evidence/CP67-inactive-org-shred-receipt-visible.png)
- [Synthetic secret metadata after refresh; no plaintext present](../evidence/CP67-vault-secret-after-refresh.png)
- [Project/environment creation and resulting selected scope](../evidence/CP67-environment-created.png)

**Observed facts:** the active-canary delete response was HTTP 200 with `destroyed_dek_id` and zero revoked keys; the canary organization was absent from the subsequent organization list; its project and secret metadata reads returned empty lists; one targeted `organization.crypto_shredded` event remained; the five-event tenant chain verified intact. The inactive-control delete also returned HTTP 200 and the receipt was visible.

**Source-confirmed explanation:** `OrganizationsAdminPage.handleShredConfirm` sets receipt state and, when deleting the active organization, calls `onSelectOrg`. `ConsoleShell` changes `orgSwitchEpoch`, and `WorkspaceRouter` is keyed by `${activeOrgId}:${orgSwitchEpoch}`. The remount discards the page-local receipt state. This code path matches the observed UI result.

**Not established:** persistence of the receipt across a full browser reload, user-visible recovery for interrupted/202 deletion, or the `<60s` deletion SLA. Those remain unverified.

## Business impact

Operators lose immediate UI confirmation for a successful, irreversible tenant deletion. This creates avoidable uncertainty during an erasure or incident workflow and forces an extra Audit Log step to establish whether the organization DEK was destroyed and how many virtual keys were revoked. Server-side audit evidence remained intact in this test.

## Acceptance criteria

1. Deleting the active organization through the Console must display the successful crypto-shred receipt after the scope switches, including the destroyed DEK identifier and revoked-key count.
2. Preserve the result across the active-scope page remount (for example, keep operation feedback outside the keyed workspace subtree or transfer the receipt to the new page state).
3. Do not show a success receipt for a pending, failed, or ambiguous deletion. Present a clear pending/error state and a recovery path when finalization is incomplete.
4. Retain parity between active and inactive target behavior while ensuring the receipt does not expose secret plaintext or data belonging to another tenant.
5. Add a real-browser regression covering active and inactive target organizations and assert both the persisted audit receipt and the visible UI outcome.

No product implementation was changed. `GATE-REL-07` remains **BLOCKED**.
