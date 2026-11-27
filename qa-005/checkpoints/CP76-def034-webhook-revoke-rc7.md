# CP76 — DEF-034 webhook subscription revoke retest

**Source:** frozen unsigned RC7 `737079ddb847d97dbcf832f610ed0048556cb65b`; Linux/amd64 runtime binary SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`; disposable SQLite clone `127.0.0.1:18162`; Chromium 156; authenticated synthetic Org Owner; `QA005 Org`. No Tanpopo source/specification changes and no provider use.

**Disposition:** **PASS (scoped QA-005 retest)** for registration, UI/API delete, subscription removal, audit retention/verification and signing-secret cleanup. This reconciles the RC4 missing-delete failure with the RC6 lifecycle pass and the current RC7 behavior, without deleting historical findings.

## Reproduction and evidence

1. Operations → Webhooks initially showed zero rows. Providers → Vault Secrets was empty. Sealed a newly generated synthetic `WEBHOOK_HMAC_SECRET` named `qa005-cp76-webhook-hmac`. The value was generated in memory and never printed, captured in screenshots, or committed.
2. Attempted `https://hooks.qa005-cp76.invalid/receiver`. The real API returned HTTP 400 `endpoint_url violates egress SSRF policy`; no subscription was created. The SSRF rule was not weakened. This hostname rejection is a security control, not a webhook lifecycle defect.
3. Registered the synthetic subscription for `kill_switch.triggered` and `mcp.approval.pending` to reserved placeholder origin `https://example.com`. The real Console returned HTTP 201, displayed the origin and Enabled state, and the authenticated list API returned one row at version 1. No matching event was triggered and no request was sent to the receiver.
4. The actual subscription row exposed an Actions menu with **Disable webhook** and **Delete webhook**. Selected Delete. The confirmation named the receiver origin and subscribed event types and stated queued deliveries would be canceled. Confirming invoked authenticated `DELETE /admin/v1/webhooks/{id}`, returned HTTP 200, displayed “Webhook deleted,” and left the Console empty. A subsequent `GET /admin/v1/webhooks?limit=100` returned HTTP 200 with zero rows.
5. The real Audit Log showed `webhook.registered` and `webhook.deleted` for the same subscription. Its JCS verification returned HTTP 200 and `intact:true` across 618 events. The final screenshot visibly shows both actions and the verified chain.
6. Deleted the synthetic HMAC credential through Providers → Vault Secrets. The UI returned HTTP 204 and a metadata-only list read confirmed the synthetic credential was absent; the test subscription list remained empty.

Actual Chromium screenshots were visually inspected: [registered row](../evidence/screenshots/CP76-webhook-registered.png), [row actions](../evidence/screenshots/CP76-webhook-actions.png), [explicit delete confirmation](../evidence/screenshots/CP76-webhook-delete-confirm.png), [deleted/empty state](../evidence/screenshots/CP76-webhook-deleted.png), [registered/deleted audit events and intact chain](../evidence/screenshots/CP76-webhook-audit.png), and [deleted synthetic Vault credential](../evidence/screenshots/CP76-webhook-secret-cleaned.png). Sanitized exact results are in [CP76 evidence JSON](../evidence/CP76-def034-webhook-revoke.json).

## Reconciliation and limitations

This exact RC7 slice confirms a discoverable, confirmed, auditable delete path; the prior QA-002 no-action/404 failure is not reproduced on this later baseline, and the QA-004 RC6 pass is consistent with it. DEF-034 is **PASS scoped**, not globally closed. Disable/re-enable version behavior, delivery cancellation when work is pending, disabled-event enqueue suppression, project scoping, other roles, concurrency, and actual receiver response/signature/retry were not tested. The placeholder domain received no event; no successful external delivery is claimed. `GATE-REL-07` remains BLOCKED.
