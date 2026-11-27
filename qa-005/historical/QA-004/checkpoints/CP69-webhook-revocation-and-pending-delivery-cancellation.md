# CP69 — Webhook revoke lifecycle and queued retry cancellation

**Baseline:** exact frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`  
**Runtime:** isolated SQLite RC6 runtime `127.0.0.1:18128`  
**Browser:** Chromium, 1440×1000; actual Console actions and screenshots visually inspected  
**Product source modified:** no

## Outcome

- **PASS (scoped), DEF-034:** Independently retested on RC6 rather than inheriting QA-003's RC5 pass. In a new synthetic org, the real Console registered a webhook; Disable and Enable changed its persisted state and version; Delete returned HTTP 200, removed the subscription from scoped API readback, and retained audit history.
- **PASS (scoped):** A project kill-switch event created a webhook delivery row. The worker made four attempts, then left it in `RETRY_SCHEDULED` with no HTTP status and a generic transport failure. Disabling the subscription through the real Console returned HTTP 200, advanced it to version 4, displayed “Webhook disabled and queued deliveries canceled,” and removed the matching retry row. The Delete confirmation likewise explicitly said queued deliveries would be canceled.
- **PASS (scoped):** Audit readback retained `webhook.registered`, two `webhook.disabled` events, `webhook.enabled`, and `webhook.deleted`. The real Security → Audit Log page verified the RFC 8785 chain intact across 13 events.
- **BLOCKED / UNVERIFIED:** No receiver HTTP response, acknowledgement, signature verification, or successful delivery was observed. The row had `last_http_status: null` and `last_error: transport failure`; direct TCP probes to public port 443 were refused by this runner. This is insufficient evidence to classify a product delivery defect.
- **UNVERIFIED:** A second kill-switch event was triggered and cleared while the subscription was disabled, but the delivery table was not read before the subsequent subscription deletion. No pass is inferred for suppressing event enqueue while disabled. Other event types, project-scoped subscription, role boundaries, failure statuses and concurrent deliveries also remain open.

## Reproduction

1. Through Chromium, sign in as the isolated test operator/owner. Create `qa004-cp69-webhook-revocation`, switch into it at **No project / No env**, and create `qa004-cp69-webhook-project` through the workspace picker.
2. In Providers → Vault Secrets, seal a random synthetic `WEBHOOK_HMAC_SECRET`. Its input was masked; API metadata did not contain `secret_value`. A test automation retry produced two same-name synthetic credentials; both were later deleted through the Console (HTTP 204) and scoped metadata readback returned zero. No plaintext is included in evidence.
3. In Operations → Webhooks, register an org-scoped subscription to the placeholder origin `https://example.com` for `kill_switch.triggered` and `mcp.approval.pending`. The UI showed origin only and an Enabled row; registration returned HTTP 201. No user data or provider credentials were placed in the payload.
4. Disable and re-enable from the row Actions menu. Scoped Admin API readback showed `enabled:false, version:2`, then `enabled:true, version:3`. Captured the actual `PATCH` responses, notices and screens.
5. In Security → Kill Switches, preview the exact canary project, trigger a reasoned project switch, and confirm the exact project slug. Trigger returned HTTP 200 with one affected entity and zero severed streams. The targeted event produced one delivery row; read only its status metadata (not payload): four attempts, `RETRY_SCHEDULED`, null HTTP status, generic `transport failure`.
6. Return to Operations → Webhooks and disable while that retry was still queued. The Console returned HTTP 200, persisted `enabled:false, version:4`, showed the queued-cancellation success notice, and SQLite readback found zero matching delivery rows. Clear the switch through Security; the UI/API returned HTTP 200.
7. Trigger and clear a second project switch while the subscription was disabled (both HTTP 200). This tested the user path, but the database was not queried before the next cleanup step, so disabled-event suppression remains UNVERIFIED.
8. Delete the subscription through the row action and explicit confirmation. `DELETE` returned HTTP 200 `{deleted:true}`, the scoped subscription list returned zero, and five webhook lifecycle actions remained in the 13-event audit partition. The real Console verified the JCS chain intact.
9. Delete both synthetic HMAC credentials through the real Vault UI; readback returned zero. Switch back to the root organization, crypto-shred the now-inactive synthetic canary, observe the receipt, and confirm it no longer appears in the organization list. Two unrelated organizations remained.

Sanitized API and database metadata is in [CP69 results](../evidence/CP69-webhook-lifecycle-results.json). Actual screenshots were visually inspected: [empty webhooks](../evidence/CP69-webhooks-empty.png), [masked HMAC entry](../evidence/CP69-hmac-secret-form-masked.png), [registered row and actions](../evidence/CP69-webhook-registered.png), [kill-switch preview](../evidence/CP69-kill-switch-preview.png), [retry canceled while disabled](../evidence/CP69-webhook-pending-canceled.png), [delete confirmation](../evidence/CP69-webhook-delete-confirm.png), [deleted empty state](../evidence/CP69-webhook-deleted-empty.png), [13-event verified chain](../evidence/CP69-webhook-audit-chain-verified.png), and [synthetic-org cleanup receipt](../evidence/CP69-org-shred-receipt.png). Additional setup and cleanup screenshots are in the evidence directory.

## DEF-034 disposition

QA-003 previously marked this defect fixed on RC5. This CP69 RC6 retest independently confirms the scoped Console/API lifecycle that was missing historically: Disable and Enable persist; Delete is available through the row actions and Admin API, removes the subscription, cancels queued deliveries, and retains audit records. **DEF-034 is PASS (scoped) for this lifecycle.** It does not establish delivery correctness or all RBAC scopes.

No new product defect is assigned. Receiver behavior, delivery reliability, disabled-event suppression, delivery retry policy, PostgreSQL worker behavior, and other roles remain incomplete. QA continues; `GATE-REL-07` remains **BLOCKED**.
