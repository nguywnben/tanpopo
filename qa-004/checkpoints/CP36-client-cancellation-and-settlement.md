# CP36 — Client cancellation propagation and settlement reconciliation

**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`  
**Status:** Partial; cancellation propagated; trace/FinOps reconciliation FAIL under DEF-052.  
**Product code:** unchanged.

## Journey

1. Temporarily replaced only the loopback test fixture on `127.0.0.1:18125` with a synthetic SSE responder that emits 24 slow chunks for the marked test request. The regular fixture source was preserved and restored after the test.
2. Used the real Access & Keys screen as Team Admin to issue a temporary PROVIDER-plane Virtual Key limited to `/v1/chat/completions`.
3. Used a cookie-free request from the real browser to `POST /v1/chat/completions`, streaming through the existing `qa004-loopback` route. After the first SSE chunk arrived, the browser `AbortController` cancelled the response.
4. The fixture observed the client disconnect after two upstream chunks. Tanpopo's trace for the request is HTTP 200, `mid_stream_abort`, `usage_accuracy=EXACT`, `$0`, 14ms. FinOps Chargeback for the same key records 0 exact requests/$0 and 1 estimated request/$0.000001.
5. Revoked the temporary key from Access & Keys; persisted state is `REVOKED` v2. Removed the one-time secret. The pre-existing route was not changed.
6. Restored `/tmp/qa004-ollama-mock.py` on port 18125 and verified the synthetic `/v1/models` endpoint responds again.

## Result

- **PASS, scoped:** browser cancellation reached Tanpopo and the upstream fixture; the fixture stopped its stream rather than sending the remaining 22 chunks.
- **FAIL:** per-request trace accuracy/cost still disagrees with FinOps settlement on client cancellation. This is another independent reproduction of DEF-052; no new defect ID assigned.
- **UNVERIFIED:** cancellation races before first byte, cancellation after later chunks/usage, native-dialect cancellation, multi-target fallback policy, concurrent cancellations, and runtime shutdown during cancellation.

## Evidence

- `evidence/CP36-client-cancel-results.json` — sanitized request, trace, fixture disconnect, FinOps screen, and key revocation state.
- `evidence/CP36-client-cancel-trace.png` — actual Traces screen, visually inspected.
- `evidence/CP36-client-cancel-finops.png` — actual Chargeback screen showing the test key's `0 / 1` exact/estimated requests and `$0.000001` estimated amount, visually inspected.
- `evidence/CP36-key-issue-form.png` — real restricted-key form before issuance; no one-time key secret is included.

The previously active test route and all historical records remain intact. GATE-REL-07 remains BLOCKED; QA-004 continues.

