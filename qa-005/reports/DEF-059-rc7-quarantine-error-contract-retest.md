# DEF-059 — exact-RC7 quarantine error-contract retest

**QA-005 result:** PASS (scoped). The original historical FAIL remains preserved.

**Scope:** exact-source Linux/amd64 binary SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`; isolated SQLite clone `127.0.0.1:18164`; local Ollama-compatible fixture; synthetic Org Owner; Chromium 156.0.8078.79.

## Reproduction and result

1. Send an authorized native `/native/ollama/api/generate` request and provider-compatible `/v1/chat/completions` request to healthy routes. Both return HTTP 200 before quarantine.
2. Quarantine `@tanpopo/ollama` through Extensions → Installed, with the required audit reason and exact package-ID confirmation.
3. Resend both valid requests. Native returns HTTP 503 `extension_quarantined`, `extension_error`, `retryable:false`; provider-compatible returns the same contract.
4. Query each returned trace ID. Both persisted traces show HTTP 503 `extension_quarantined`, no selected deployment and zero cost. The actual Traces lookup and waterfall screenshots were visually inspected.
5. Unquarantine, review the unchanged permission set, approve and activate. Readback returns `ENABLED_ACTIVE`; an authorized native canary succeeds with HTTP 200. Temporary keys were revoked.

**Expected:** API compatibility contract HTTP 503 `extension_quarantined`, type `extension_error`, `retryable:false`; no traffic to quarantined adapter; clear correlated trace.  
**Actual:** matches on both tested ingress paths. The historical generic `no_compliant_deployment_available` response is not reproduced on RC7.

## Limits and business impact

This closes the diagnostic mismatch for these two adapter paths on one SQLite runtime. It does not verify the full failover-selection criterion, a separate residency-policy denial control, PostgreSQL/Valkey, other roles, adapters or deployments. A dedicated fixture call counter was unavailable, so dispatch prevention is supported by the 503, `deployment_id:null`, Policy-only trace and zero cost; no exact upstream call count is asserted. Historical RC6 behavior remains in the source report.

See [CP113](../checkpoints/CP113-def059-def075-def076-regression.md), [sanitized evidence](../evidence/CP113-def059-def075-def076.json), and the historical [DEF-059 record](../historical/QA-004/reports/DEF-059-quarantine-traffic-uses-generic-deployment-error.md). No product code was changed.
