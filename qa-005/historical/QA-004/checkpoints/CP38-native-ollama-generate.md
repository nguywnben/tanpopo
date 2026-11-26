# CP38 — Native Ollama `/api/generate` dialect lifecycle

**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`  
**Result:** native dispatch/response PASS for this fixture; usage settlement FAIL under DEF-051/052.  
**Product code:** unchanged.

## Journey

- Issued a temporary PROVIDER-plane key from Access & Keys with `/native/ollama/*` route grant.
- Sent cookie-free `POST /native/ollama/api/generate` for logical model `qa004-loopback`, with `stream:false`. Tanpopo dispatched to the synthetic Ollama fixture's `/api/generate` path and returned HTTP 200 valid Ollama JSON, preserving `prompt_eval_count:6` and `eval_count:3`. Fixture received no authorization header.
- Response header reported `X-Tanpopo-Usage-Accuracy: ESTIMATED`. The trace row (`trc_01m89nyjake009aa209mbs3035`) reports Plane NATIVE, HTTP 200, `EXACT`, `$0`; FinOps for the same test key reports 0 exact / 1 estimated request and `$0.000001` estimated.
- The actual Traces and Virtual Key grouped Chargeback screenshots were visually inspected. Revoked the test key via Access & Keys; persisted key is `REVOKED` v2 and replay returned HTTP 401 `invalid_virtual_key`.
- No product code or route configuration changed. The temporary key secret was removed.

## Reconciliation

- **PASS, scoped:** authorized cookie-free Ollama native Generate dispatch and Ollama response fidelity for this synthetic fixture.
- **FAIL:** provider `prompt_eval_count` / `eval_count` are still estimated rather than consumed as exact usage (DEF-051). Trace and FinOps still disagree (`EXACT`/$0 vs `ESTIMATED`/$0.000001), extending DEF-052. No new ID assigned.
- **UNVERIFIED:** native Generate streaming, malformed/overflow counters, concurrent request behavior, and other Ollama native paths remain open.

## Evidence

- `evidence/CP38-native-generate-results.json`
- `evidence/CP38-native-generate-trace.png`
- `evidence/CP38-native-generate-finops.png`
- `evidence/CP38-native-key-form.png`
- `evidence/CP38-native-key-revoked.png`

QA-004 continues; GATE-REL-07 remains BLOCKED.

