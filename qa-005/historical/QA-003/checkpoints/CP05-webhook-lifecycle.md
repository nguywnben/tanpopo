# CP05 — Webhook subscription lifecycle

**Baseline:** RC5 `3423d4f523ef0ac77469a1201b8b753d1d31c902`
**Status:** Registration, state transitions and deletion executed in the isolated SQLite runtime. `GATE-REL-07` remains **BLOCKED**.

## Journey and observed result

1. In a real Chromium session, inspect Operations → Webhooks in its empty state. Open the registration drawer and verify its HTTPS URL, signing credential, and event controls. The form describes that the complete receiver URL is sealed and metadata stores only the origin.
2. In Providers → Vault Secrets, seal a random synthetic `WEBHOOK_HMAC_SECRET`. The value was entered in a password field, was not saved in screenshots or output, and the API returned metadata without plaintext. Choose that credential in the Webhooks drawer and register a subscription for `https://example.com/qa003-webhook-synthetic` with `kill_switch.triggered` and `mcp.approval.pending`. The Console showed a success banner and an Enabled row; Admin API returned HTTP 201 and listed one subscription with origin only.
3. Do not trigger a subscribed event. Therefore no webhook delivery request or payload was sent to the public placeholder origin.
4. From the row Actions menu, disable the subscription, then re-enable it. The Console showed Disabled after the first action and Enabled before deletion. Audit readback contained `webhook.disabled` and `webhook.enabled`.
5. Open Delete webhook. The confirmation dialog states that the subscription and queued deliveries will be removed. Confirm in the UI. Admin API returned HTTP 200 with `deleted: true`; the next subscription list returned zero rows. The audit list retained `webhook.registered`, `webhook.disabled`, `webhook.enabled`, and `webhook.deleted`.
6. Remove the now-unused synthetic HMAC credential through Providers → Vault Secrets. The credential disappeared from the list and API readback; the unrelated synthetic API key remained. No delivery was queued or sent.

Visually inspected evidence: [empty state](../evidence/CP05-webhooks-empty.png), [registration form](../evidence/CP05-webhooks-form.png), [registered subscription](../evidence/CP05-webhook-registered.png), [disabled state](../evidence/CP05-webhook-disabled.png), [delete confirmation](../evidence/CP05-webhook-delete-confirm.png), [deleted state](../evidence/CP05-webhook-deleted.png), [credential cleanup](../evidence/CP05-webhook-secret-cleanup.png). Sanitized API details: [registration](../evidence/CP05-webhook-registration.json), [revocation](../evidence/CP05-webhook-revoke.json).

## Defect disposition

- **DEF-034 — VERIFIED FIXED for the tested Console/Admin API lifecycle.** RC5 presents disable/delete actions; disable and re-enable persisted; authenticated deletion returned 200, removed the subscription, and retained the deletion audit event. Historical Console omission/DELETE 404 did not reproduce.
- Delivery reliability, signature verification, retry/dead-letter behavior, stale ETag conflicts, queued-delivery cancellation with live pending rows, and cross-role authorization remain unverified. No delivery was triggered in this phase.
- No new defect confirmed.

## Environment and limits

The subscription target was a public placeholder origin used only for registration validation. No subscribed event was triggered and no request was sent to it. All generated credentials and QA data were synthetic; credential plaintext, database, and runtime logs remain outside the repository.
