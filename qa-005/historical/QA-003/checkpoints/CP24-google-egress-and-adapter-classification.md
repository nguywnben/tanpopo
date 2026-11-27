# CP24 — Google Gemini adapter and managed Cloud egress classification

**Baseline:** RC5 frozen source `3423d4f523ef0ac77469a1201b8b753d1d31c902`
**Tested runtime:** isolated SQLite clone `127.0.0.1:18116`; real Chromium at 1440×1000; synthetic provider connection; configured Google QA credential used only in memory and the write-only vault
**Status:** current `upstream_unreachable` classified as an environmental direct-egress incompatibility on available evidence; no product defect confirmed. Tanpopo provider functionality remains PARTIAL/BLOCKED. `GATE-REL-07` remains **BLOCKED**.

## Environment and independent controls

- Google and Meta QA credential variables were reported ready by Cloud environment status. Their values were not printed or placed in evidence.
- An authenticated, non-generative `GET /v1beta/models?pageSize=10` request through the environment's HTTPS proxy returned HTTP 200 with ten model metadata rows. No inference/generation endpoint was invoked; spend remained USD 0. Evidence: [sanitized egress comparison](../evidence/CP24-google-egress-result.json).
- A Node default `fetch` request to the same Google origin without the ambient proxy failed with `ECONNREFUSED` before receiving HTTP. This makes direct and proxy-routed network paths observably different in this Cloud environment.
- The three target isolated runtime health endpoints `18114`, `18115`, and `18116` returned HTTP 200 at the start of this phase. The detailed app-level adapter repro below used 18116 only; matching provider configuration on 18114/18115 remains untested.

## Real Tanpopo Google adapter journey

1. In Extensions → Marketplace, visually inspect the official Google Gemini row and choose **Stage for review**. The actual install POST returned 202. The review sheet listed `generativelanguage.googleapis.com`, `api_key` slot, provider hooks, WASM sandbox tier, TLS requirement, disabled private access, request/response inspection, and resource limits.
2. Confirm the required-consent gate. Before checking consent, the Google-specific **Approve & activate** button had `aria-disabled=true`; Playwright click timed out because the action was disabled. After checking the explicit consent box, the attribute cleared and approval returned HTTP 200. The actual unchecked attempt did not activate the package. The interaction readback records `checked=false` / `aria-disabled=true` before consent and `checked=true` / enabled after consent; see [setup result](../evidence/CP24-google-setup-result.json). Visually inspected screenshots: [permission review](../evidence/CP24-google-stage-review.png) and [consent checked](../evidence/CP24-google-consent-checked.png).
3. In Providers → Vault Secrets, seal the configured `QA_GOOGLE_AI_STUDIO_API_KEY` through the write-only password field. The API returned 201. The screen exposed only kind, ID, fingerprint suffix and a “Plaintext cannot be read again” message. No key was emitted, read back, or captured. See [metadata screenshot](../evidence/CP24-google-credential-metadata.png) and [setup result](../evidence/CP24-google-setup-result.json).
4. Create a `COMMERCIAL_API`, US-region Google connection at `https://generativelanguage.googleapis.com` bound to the new secret. Creation returned 201 and state `CREATED_UNVERIFIED`; the Providers screen showed the fingerprint suffix. See [form](../evidence/CP24-google-connection-ready.png) and [created row](../evidence/CP24-google-connection-created.png).
5. Use **Probe connection** from the real row Actions menu. Tanpopo returned HTTP 200 for the probe operation with `valid=false`, `failure_code=upstream_unreachable`, and sanitized `upstream connection could not be reached`; the UI kept the connection Unverified and displayed that message. No provider model inference, streaming, or paid generation occurred. Afterwards, the synthetic connection and write-only credential were deleted (each DELETE 204); the Gemini installation was quarantined and uninstalled, and the resulting Extension Installation kill switch was cleared. Final authenticated readback showed no active kill switches and only the original Ollama installation. See [cleanup state](../evidence/CP24-google-cleanup-result.json) and [cleared switch screenshot](../evidence/CP24-google-switch-cleared.png). See [Actions menu](../evidence/CP24-google-connection-actions.png), [probe screenshot](../evidence/CP24-google-probe-result.png), and [sanitized response](../evidence/CP24-google-probe-result.json).

## Failure classification and historical reconciliation

- The exact model-list API works with the configured credential through the Cloud proxy; direct Node transport fails; Tanpopo's guarded provider probe also cannot reach Google. The available evidence supports managed Cloud direct-egress incompatibility for current `upstream_unreachable`. It does **not** establish an invalid key, broken Gemini transform, or product networking defect.
- The product adapter was staged and activated, Vault accepted the key, and the probe handler executed and returned a structured upstream failure. Therefore `probe_runtime_unavailable` was **not reproduced** in this RC5 configuration. That older QA-001 error arose in a different runtime/configuration and remains **UNVERIFIED** until its exact runtime/config can be restored or historical infrastructure logs identify the cause.
- QA-002 CP112 recorded the same configured-proxy success versus no-proxy/direct failure differential. It is corroborating historical context and is not counted as a QA-003 PASS. No SSRF guard was weakened and no proxy was forced into Tanpopo.
- No new product defect is filed. This is QA-OBS-017; see [classification report](../reports/QA-OBS-017-google-direct-egress.md).

## Limits and next needs

One Google adapter, connection and SQLite clone were exercised. Runtime-matched Google configuration on the other two services, inference/streaming, model-specific generation, error normalization after HTTP responses, invalid/rotated/revoked Google key behavior, failover and direct-egress acceptance remain open. A supported QA runtime/network path that allows Tanpopo's guarded direct connection to Google is required. This checkpoint does not establish production provider readiness.

Historical defect counts and the register are unchanged. New confirmed product defects remain DEF-040 through DEF-044. QA-003 remains in progress; `GATE-REL-07` remains **BLOCKED**.
