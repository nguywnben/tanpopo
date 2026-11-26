# CP112 — Google provider probe and Cloud egress path retest

**Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`  
**Runtime:** isolated RC4 QA Console on 18084  
**Result:** **Google API via configured proxy PASS; Tanpopo probe FAIL with `upstream_unreachable`; classified as managed Cloud direct-egress limitation, not a product defect**  
**Product implementation modified:** No  
**Release gate:** `GATE-REL-07 BLOCKED`

## Real connection probe

In Chromium, opened **Providers → Connections**. The screen showed two visually identical `QA002 Google Gemini` rows, both `Unverified`; this remains the separate DEF-032 identity ambiguity. Chose the first visible row and used **Probe connection**. The actual request was `POST /admin/v1/connections/con_01m8530p3we00828f2x8mp6vs2/test`, HTTP 200, `valid=false`, `failure_code=upstream_unreachable`, latency 2068 ms, and safe message “upstream connection could not be reached”. The row remained Unverified and no model inference was sent. The result screenshot was visually inspected and retained.

## Independent network/API controls

- With ambient proxies explicitly disabled, a direct request to `generativelanguage.googleapis.com` timed out after six seconds before TCP connect/TLS (`HTTP 000`, `time_connect=0`).
- Through the environment's configured proxy, an authenticated read-only `GET /v1beta/models?pageSize=1` to Google returned HTTP 200 with one model in the requested page. The QA key was read from its managed environment binding, never printed, retained, or included in evidence.
- Frozen RC4 Gemini adapter source builds the same `GET /v1beta/models` probe and injects the key through the host-blind `x-goog-api-key` header. It does not put the key in a query string. No source change was made.

The paired results support the existing QA-OBS-017 classification: the Cloud environment supplies a working proxy path, but no-proxy/direct TCP egress is blocked; Tanpopo's guarded broker intentionally avoids ambient proxies to preserve host/CIDR/SSRF enforcement. This supports Cloud egress incompatibility as the cause of the observed `upstream_unreachable`; it does not prove all broker internals. The bound vault key's equality to the separately managed environment key was not established. No product defect is assigned, and no SSRF boundary was bypassed.

The historical QA-001 `probe_runtime_unavailable` result remains a different runtime-acquisition/configuration failure and remains **UNVERIFIED on RC4**. CP112 does not claim successful Tanpopo-to-Google inference or a complete multi-runtime comparison.

## Coverage disposition

- **PASS, scoped:** direct no-proxy failure reproduced; configured proxy path authenticated against the exact Google model-list endpoint; actual Console provider probe and unchanged persisted Unverified state observed; adapter's probe path/key transport read from frozen source.
- **BLOCKED/UNVERIFIED:** Google app-mediated inference/connection verification in an environment that supports guarded direct egress; stored-vault-key to environment-key equality; matching Google adapter/connection probes on other runtimes; historical `probe_runtime_unavailable` on RC4.
- **No product defect:** current evidence supports an environment/network path limitation. Do not weaken SSRF/CIDR policy or route the broker through an ambient proxy to make this test pass.

## Consolidated status

- **Historical DEF-001–015:** preserve scoped QA-002 outcomes only; DEF-005 remains PARTIAL and DEF-007 remains incomplete for fault atomicity/erasure proof. No QA-001 PASS is inherited.
- **Open defects:** DEF-016–038 remain preserved/open; CP112 changes no finding disposition and adds no defect.
- **Coverage completed:** real Chromium provider inventory/probe interaction; explicit proxy-vs-direct egress differential; same authenticated Google endpoint; visual screenshot inspection.
- **Coverage remaining:** same-config runtime comparison, app inference, provider adapter/credential comparison and all previously open product/platform/security/browser cases.
- **Recommended handoffs:** keep QA-OBS-017 environmental. Preserve DEF-038/007 and existing DEF-021/023/032 and cross-scope handoffs; do not relax host/CIDR authorization.

QA-002 remains in progress. `GATE-REL-07` remains **BLOCKED**.
