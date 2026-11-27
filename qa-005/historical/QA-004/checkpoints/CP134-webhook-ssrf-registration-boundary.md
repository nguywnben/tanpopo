# CP134 — Webhook SSRF registration boundary

**Baseline:** exact frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`  
**Runtime:** isolated SQLite runtime `127.0.0.1:18128`  
**Actor:** synthetic QA Org Owner in the isolated canary project  
**Browser:** real Chromium at 1440×1000; screenshots were visually inspected  
**Product source modified:** no

## Result

**PASS (scoped security negative cases).** The actual Operations → Webhooks form submitted two private targets using a newly sealed synthetic `WEBHOOK_HMAC_SECRET`. RC6 rejected both registrations with HTTP 400 `ssrf_blocked_private_ip` before a subscription was persisted:

- `https://127.0.0.1:16379/` → `endpoint_url violates egress SSRF policy`.
- `https://169.254.169.254/latest/meta-data/` → `metadata and link-local addresses are unconditionally blocked`.

The authenticated subscription list remained at zero. The synthetic credential was deleted through Providers → Vault Secrets (HTTP 204); a refreshed page and API readback both returned zero matching credentials. The API never returned the plaintext secret. No webhook delivery or external provider call was made, and no defect was assigned.

This verifies registration-time rejection only. It does not verify successful public HTTPS delivery, HMAC/timestamp validation by a receiver, redirects, retries, DNS rebinding, or project-scoped webhook authorization. Full webhook behavior remains **PARTIAL**.

## Reproduction

1. Sign in to exact RC6 as the QA Org Owner and select `QA-004 Test Root Org / QA-004 Isolated Canary`.
2. In Providers → Vault Secrets, create a random, masked `WEBHOOK_HMAC_SECRET`. The metadata response omitted `secret_value`.
3. In Operations → Webhooks, retain the default synthetic event selection and submit the loopback HTTPS URL above. Observe HTTP 400 and the SSRF policy error in the form.
4. Replace the URL with the link-local metadata target and submit again. Observe HTTP 400 and the unconditional metadata/link-local block message.
5. Read `/admin/v1/webhooks?limit=100`; it returns HTTP 200 with zero subscriptions.
6. Delete the temporary credential from the Vault Secrets row. Refresh the screen and read the scoped secrets API again; the matching row is absent.

## Evidence

- [Sanitized result data](../evidence/CP134-webhook-ssrf-results.json)
- [Credential creation and one-time secret notice](../evidence/CP134-webhook-ssrf-secret-created.png)
- [Loopback rejection](../evidence/CP134-webhook-ssrf-loopback-rejected.png)
- [Metadata endpoint rejection](../evidence/CP134-webhook-ssrf-metadata-rejected.png)
- [No subscription persisted](../evidence/CP134-webhooks-no-subscription.png)
- [Credential deleted and vault empty after readback](../evidence/CP134-webhook-ssrf-secret-cleanup.png)

The campaign still needs a controlled public HTTPS receiver and an authorized outbound path to verify successful delivery, signature bytes, anti-replay timestamp, acknowledgement and retry behavior. CP69's earlier public endpoint attempts ended in transport failures; no delivery correctness is inferred here. `GATE-REL-07` remains **BLOCKED**.
