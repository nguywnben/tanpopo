# CP22 — Webhook SSRF rejection and Vault cleanup

**Source/runtime:** exact RC7 source `737079ddb847d97dbcf832f610ed0048556cb65b`; local compiled runtime SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`; isolated SQLite at `127.0.0.1:18145`. Real authenticated Chromium/Playwright UI flow as synthetic project owner.

## Result: PASS (scoped registration security control)

1. Operations → Webhooks showed zero subscriptions. Visually inspected the inventory empty state (`CP22-webhook-inventory-before.png`).
2. Created a synthetic `WEBHOOK_HMAC_SECRET` through Providers → Vault Secrets using the UI. The one-time plaintext was only sent to Tanpopo, never placed in a report, and the Console returned a trailing fingerprint. This credential existed only for the SSRF test.
3. Opened Add webhook, selected the synthetic credential and submitted `https://127.0.0.1:19443/qa005`. The API returned HTTP 400 `ssrf_blocked_private_ip`; the Console visibly showed “endpoint_url violates egress SSRF policy.” The webhook inventory remained zero. No webhook delivery occurred. The rejected-state screenshot was visually inspected.
4. Deleted the temporary Vault credential through the UI and reloaded its inventory. It showed zero credentials; cleanup screenshot visually inspected.

**Expected:** a loopback receiver must be rejected before registering a webhook or attempting delivery. **Actual:** rejection occurred and no subscription was created. This does not establish behavior for DNS rebinding, redirects, IPv6, other private ranges, public receiver delivery, HMAC verification, retries, or cleanup when a live webhook references a credential.

Sanitized result: `evidence/CP22-webhook-ssrf-results.json`. Screenshots: `CP22-webhook-inventory-before.png`, `CP22-webhook-add-form.png`, `CP22-webhook-loopback-rejected.png`, and `CP22-vault-cleanup.png`. The temporary secret plaintext and all runtime credentials remain outside the repository under `/tmp`.
