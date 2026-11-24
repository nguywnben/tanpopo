# QA-OBS-017 — Google adapter probe cannot use managed proxy egress

- **Classification:** Environmental/provider-connectivity observation; not a product defect on available evidence
- **Severity:** Informational / blocked integration acceptance
- **Baseline:** QA-003 frozen RC5 source `3423d4f523ef0ac77469a1201b8b753d1d31c902`
- **Runtime:** isolated SQLite clone `127.0.0.1:18116`, Chromium, synthetic connection `QA003 Google CP24`
- **Historical reference:** QA-002 CP112 (`qa/qa-002-regression` commit `1052ecb25a51e7ea94045946ede865ee062d4419`), which recorded the same direct-vs-proxy differential; QA-001's older `probe_runtime_unavailable` remains a separate unresolved historical condition.

## Reproduction and evidence

1. Use the cloud-configured `QA_GOOGLE_AI_STUDIO_API_KEY` in memory to request `GET https://generativelanguage.googleapis.com/v1beta/models?pageSize=10` through the environment-configured HTTPS proxy, with the key in the `x-goog-api-key` header. Google returns HTTP 200 and model metadata. No generation method is called.
2. Issue the same non-generative model-list request from Node's default `fetch`, which does not use the configured proxy in this runtime. It fails with `ECONNREFUSED` before an HTTP response.
3. In Tanpopo's own Console, stage `@tanpopo/gemini`, inspect its permissions (host `generativelanguage.googleapis.com`, `api_key` slot, WASM sandbox tier), explicitly check consent, and activate it. Seal the configured credential through the write-only Vault Secrets flow, then create a normal Google provider connection with endpoint `https://generativelanguage.googleapis.com` and that credential.
4. Use the connection's real **Probe connection** action. Admin API returns HTTP 200 for the probe operation but `valid=false`, `failure_code=upstream_unreachable`, `sanitized_message=upstream connection could not be reached`; the UI leaves it Unverified. No inference is sent.

Sanitized direct comparison: [result JSON](../evidence/CP24-google-egress-result.json). UI evidence visually inspected: [staged permission review](../evidence/CP24-google-stage-review.png), [pre-consent aria-disabled readback](../evidence/CP24-google-setup-result.json), [consent checked](../evidence/CP24-google-consent-checked.png), [credential metadata](../evidence/CP24-google-credential-metadata.png), [connection configuration](../evidence/CP24-google-connection-ready.png), and [probe outcome](../evidence/CP24-google-probe-result.png). After the probe, the synthetic connection and write-only secret were deleted and the Gemini extension was quarantined/uninstalled; final readback showed the temporary extension switch cleared. [Cleanup readback](../evidence/CP24-google-cleanup-result.json). No key material is stored in evidence.

## Classification

The exact same authenticated, non-generative Google model-list operation succeeds through the configured Cloud proxy and direct transport fails before HTTP. Tanpopo's guarded provider path produced `upstream_unreachable` after activation and credential binding. The evidence supports the managed Cloud direct-egress restriction as the cause of this current failure. It does not support a Gemini adapter transform defect, invalid credential, or product networking defect. Tanpopo's guarded client intentionally does not inherit ambient proxies because doing so could alter network/SSRF guarantees; no proxy bypass or security relaxation was attempted.

This QA-003 run did **not** reproduce `probe_runtime_unavailable`: the Gemini package staged, permission consent succeeded, the adapter activated, and the provider probe executed and returned an upstream connectivity failure. The historical `probe_runtime_unavailable` occurred in a different older runtime/configuration and remains UNVERIFIED for RC5 until that environment/configuration can be recreated or logs can establish its cause. The QA-002 CP112 direct-vs-proxy result is corroborating historical context only, not an inherited QA-003 PASS.

## Limits and next infrastructure needed

One active Gemini connection and one SQLite clone were exercised. Provider inference, streaming, auth rejection, adapter-specific response normalization, the remaining runtimes under matching provider configuration, and successful direct-egress probing remain **UNVERIFIED/BLOCKED**. To close provider acceptance, supply an isolated QA runtime/network where Tanpopo's guarded direct dial to Google's documented endpoint is authorized, or a supported first-party egress path that preserves the application's SSRF controls. Do not use client-side proxy routing as a substitute; do not widen private-IP allowlists.
