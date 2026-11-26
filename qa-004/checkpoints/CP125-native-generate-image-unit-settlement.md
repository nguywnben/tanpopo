# CP125 — Native Ollama Generate image-unit settlement

**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1` (artifact run 45581179274)
**Result:** FAIL — extends Major DEF-074 from native `/api/chat` to native `/api/generate`. The temporary key was revoked, the original fixture restored, and the original project limit restored. GATE-REL-07 remains BLOCKED.

## Reproduction

The real Owner Console created a PROVIDER key restricted to `/native/ollama/*` and `qa004-cp121-priced-vision`. To allow this positive dispatch test after prior CP123/124 restored the project’s 100-micro limit over existing usage, I temporarily raised the project hard limit to 5,000 using the real FinOps Budgets & Holds UI; save and authenticated readback returned HTTP 200.

A cookie-free `POST /native/ollama/api/generate` sent the standard Ollama top-level `images` array with two valid synthetic PNGs. The request returned HTTP 200 and a completed synthetic response with prompt/eval counters 10/5. The local fixture received exactly two images in order and verified their distinct SHA-256 hashes. It received no Authorization header; no prompt/image bytes were retained and no external provider was contacted.

The route’s deployment had zero token prices, `image_unit_nano_usd: 1000000`, and `self_hosted_zero_rate:false`. Two images should cost 2,000 micro-USD ($0.002). The persistent trace `trc_01m8b7ajj1e009xtn3qwxptkte` says `EXACT` / zero cost. Authenticated per-key Chargeback instead reports zero exact/estimated micro-USD and one estimated request. This independently reproduces the native image omission on a second public native endpoint.

The project limit was returned to 100 through the real Console (HTTP 200). Budget readback showed exact 2,036 plus estimated 14 micro-USD, zero active holds, and version 152; no additional amount was settled by CP125. The temporary key is `REVOKED` version 2, the original QA fixture was restarted, and `/api/tags` returned HTTP 200. Screenshots of the key form, Traces, Chargeback, revoked key, and budget restoration were visually inspected.

## Defect and scope

CP121 proves native `/api/chat` `messages[].images[]` is forwarded but not charged. CP125 independently proves `/api/generate` top-level `images[]` behaves the same on the same vision model/deployment and non-zero image price. Combined with CP123/124’s normalized OpenAI controls, **DEF-074 is confirmed across both tested native Ollama endpoints** while the tested normalized `/v1/chat/completions` shape bills and obeys the same project limit.

No new defect ID was assigned. This does not establish other native adapters/dialects, streaming multimodal behavior, image-format/size/capability boundaries, concurrent budget holds, or distributed enforcement. Trace accuracy/cost still disagrees with the request’s Chargeback row, extending existing DEF-052.

See [`sanitized result`](../evidence/CP125-native-generate-image-pricing-results.json) and visually inspected evidence:

- `../evidence/CP125-native-generate-key-form.png`
- `../evidence/CP125-native-generate-traces.png`
- `../evidence/CP125-native-generate-chargeback.png`
- `../evidence/CP125-native-generate-key-revoked.png`
- `../evidence/CP125-native-generate-budget-increased.png`
- `../evidence/CP125-native-generate-budget-restored.png`

QA continues; **GATE-REL-07 remains BLOCKED.**
