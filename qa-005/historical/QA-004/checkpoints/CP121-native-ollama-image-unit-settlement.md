# CP121 — Native Ollama image-unit settlement

**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1` (artifact run 45581179274)
**Result:** FAIL — new Major DEF-074. The native Ollama route forwarded a successful two-image request, but a persisted non-zero image-unit price produced zero image charge in per-key usage/Chargeback. Existing DEF-051/052 also remain evident for this call: provider token counters were ignored into estimated Chargeback, while Traces says Exact/$0. One temporary VKEY was gracefully revoked; the original loopback QA fixture was restored. No source changes or external provider calls.

## Test journey and result

The Org Owner used actual Chromium Console workflows in `QA004 Test Root Org / default / dev` to register a `vision:true` logical model `qa004-cp121-priced-vision`, create an `ACTIVE_HEALTHY` self-hosted loopback deployment, and add a single-target route. The deployment’s non-zero price card had all token/audio/document rates set to zero and `image_unit_nano_usd: 1,000,000`; `self_hosted_zero_rate` was false. The exact values were visibly entered in Models → Add deployment and preserved by authenticated deployment readback. A narrowly scoped PROVIDER-plane VKEY could invoke only `/native/ollama/*` and the test model.

A cookie-free native `POST /native/ollama/api/chat` sent two different valid synthetic PNGs in the Ollama `messages[0].images` array. The local fixture recorded both ordered SHA-256 hashes and no Authorization header, and returned synthetic HTTP 200 with `prompt_eval_count: 8` / `eval_count: 7`. No prompt/image bytes or credentials were retained by the fixture. No real provider was contacted.

At 1,000,000 nano-USD/image, the expected image-only charge is 2,000,000 nano-USD = 2,000 micro-USD = **$0.002**. Actual authenticated `GET /admin/v1/usage/summary?group_by=key` readback showed this key at zero exact micro-USD, zero estimated micro-USD, 0 exact / 1 estimated requests, and zero unmetered requests. The real per-key Chargeback screen showed the same zero-dollar Estimated row. The successful trace (`trc_01m8b5pv3ve008emhjergfc88e`) says `EXACT`, total cost zero. This is **DEF-074** (unbilled native image units) and additional DEF-052 evidence; the estimated token usage is consistent with DEF-051.

The temporary VKEY was revoked in the real Access & Keys UI; authenticated readback confirmed state `REVOKED`, version 2. The original QA Ollama fixture was restarted and `/api/tags` returned HTTP 200. The model, deployment, route, request and settlement aggregate remain in the isolated test DB for independent reproduction.

## Evidence

- `../evidence/CP121-native-image-pricing-results.json` — sanitized end-to-end result and API readback.
- `../evidence/CP121-priced-vision-model-form.png`, `CP121-priced-vision-model-created.png` — model declared through the Console.
- `../evidence/CP121-priced-image-deployment-form.png`, `CP121-priced-image-deployment-created.png` — non-zero image-unit price card and persisted healthy deployment.
- `../evidence/CP121-priced-vision-route-form.png`, `CP121-priced-vision-route-created.png` — routed deployment.
- `../evidence/CP121-priced-image-key-form.png` — route/model-restricted VKEY form.
- `../evidence/CP121-priced-image-traces.png` — actual trace list showing the successful request; visually inspected.
- `../evidence/CP121-priced-image-finops.png` — actual Chargeback UI showing zero-dollar estimated count; visually inspected.
- `../evidence/CP121-priced-image-key-revoked.png` — actual graceful-revocation result; visually inspected.

## Scope and limits

This proves the native Ollama `/api/chat` `messages[].images[]` shape is not charged by a persisted non-zero per-image rate in this exact-RC6 scenario. It does not prove all native adapters, OpenAI-compatible image formats, URL/blob image paths, pixel-based rates, non-image media schedules, mixed token+image rates, budget blocking, or streaming settlements. Root-cause analysis remains a hypothesis pending engineering review. QA continues; **GATE-REL-07 remains BLOCKED.**
