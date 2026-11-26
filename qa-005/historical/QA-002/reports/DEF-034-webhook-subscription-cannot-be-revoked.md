# DEF-034 — Webhook subscription cannot be revoked through Console or Admin API

- **Severity:** Moderate
- **Status:** CONFIRMED
- **Area:** Operations → Webhooks; Admin API; secret lifecycle
- **Baseline:** frozen RC4 `40bba5b55c2da6fc00b107b315c9b3167fb20169`
- **Reproduced:** CP86, isolated runtime `127.0.0.1:18084`, Chromium 1440×900, Operator + Org Owner

## Summary

After a valid public HTTPS webhook subscription is created, the Console list presents no disable, rotate, or delete action. An authenticated `DELETE /admin/v1/webhooks/{subscription_id}` returned 404 `resource_not_found`. The subscription was still enabled until QA-only cleanup. A tenant administrator therefore cannot use the product to revoke an external delivery destination or stop new event deliveries through the documented UI/API surface.

## Reproduction

1. Seal a disposable `WEBHOOK_HMAC_SECRET` in Providers → Vault Secrets.
2. In Operations → Webhooks, register a synthetic public HTTPS receiver for `kill_switch.triggered`. The Console returned HTTP 201 and showed the subscription as Enabled.
3. Inspect the actual table. It contains Receiver origin, Subscribed events, State, and Created columns, with no row actions or selection control.
4. From the same authenticated session, issue `DELETE /admin/v1/webhooks/{id}` with the session CSRF token. Tanpopo returned HTTP 404 `resource_not_found`. Source route registration also exposes only GET and POST for `/admin/v1/webhooks`.

The public receiver was a disposable synthetic endpoint; its token is excluded from the evidence and was deleted. No request reached it. CP86 records that separate delivery attempt failure as a managed Cloud direct-egress blocker, not as a Tanpopo delivery defect.

## Expected versus actual

- **Expected:** an authorized tenant administrator can disable or remove a registered destination using a supported, auditable workflow. Revocation stops new delivery immediately and defines how pending/retrying deliveries are handled. Bound receiver credentials can be safely retired after references are removed.
- **Actual:** a successful registration remains Enabled in the list; the UI offers no removal action; the API DELETE returns 404. The only exercised cleanup was a transaction against the isolated test database, followed by normal UI deletion of the two test vault credentials. No product-level subscription removal was available.

## Business and security impact

An administrator who changes receiver ownership, detects a compromised endpoint, or entered the wrong destination cannot revoke the active integration through the Console or Admin API. If network reachability later returns, future matching events may resume deliveries to that destination. Webhook payloads are designed to be redacted, but they still carry organization-scoped operational metadata. This is a Moderate lifecycle and least-privilege gap; no unauthorized delivery or data leak was observed in CP86.

## Facts and root-cause hypothesis

**Observed facts:** the real UI registration succeeded; the rendered list had no actions; authenticated DELETE returned 404; the API route table registers GET and POST only. The product source was not modified.

**Hypothesis:** the webhook feature implements registration/listing and background delivery but omitted the management lifecycle (disable/delete/rotation) in this release slice. This is a hypothesis about the implementation gap, not a confirmed design decision. The current product documentation reviewed during CP86 describes HMAC webhook delivery but does not define an unsubscribe contract.

## Acceptance criteria

1. Provide an authorized, discoverable Console action and corresponding Admin API operation to disable or delete a subscription; enforce tenant/project scope and CSRF/RBAC.
2. Require an explicit confirmation for permanent deletion, show the receiver origin and subscribed event types, and never expose the sealed full URL or HMAC plaintext.
3. Persist an attributable audit event and version/ETag conflict behavior for the lifecycle mutation.
4. Stop enqueueing new deliveries after disable/delete. Define and test cancellation or terminal handling of pending/retrying work without leaking data to a revoked destination.
5. Permit bound receiver-URL credentials and the signing secret to be removed after no active subscription references them; provide a safe rotation path.
6. Add end-to-end tests covering wrong tenant, insufficient role, concurrent update, in-flight delivery, retry queue, receiver recovery, and post-revocation event delivery denial.

## Evidence and limits

- [CP86 checkpoint](../checkpoints/CP86-webhook-delivery-and-subscription-lifecycle.md)
- `../evidence/qa002-cp86-webhook-registered.png` — visually inspected; displays public origin only.
- `../evidence/CP86-webhook-delivery-and-cleanup-sanitized.json` — statuses, delivery queue summary, cleanup and audit records; no receiver token, secret, raw request body, signature, database, or raw service logs.
- `../evidence/qa002-cp86-webhooks-clean.png` — visually inspected clean state after QA cleanup.

No successful outbound request or signature verification was observed: the delivery reached `DEAD_LETTER` after eight attempts with no HTTP status. The managed Cloud environment reports current enforced unrestricted policy, the managed shell proxy reached the public site, and direct no-proxy DNS timed out. Tanpopo's guarded egress client intentionally does not use ambient HTTP proxies. This supports an environment-specific direct-egress blocker; it is not evidence of a product delivery or HMAC defect. See QA-OBS-015.
