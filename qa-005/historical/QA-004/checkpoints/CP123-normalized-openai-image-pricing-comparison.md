# CP123 — Normalized OpenAI image pricing comparison

**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1` (artifact run 45581179274)
**Result:** scoped PASS for normalized OpenAI image-unit settlement on the tested Ollama-backed route; **DEF-074 remains FAIL, narrowed to the native Ollama image shape.** DEF-052 is independently reinforced by the request trace and ledger disagreeing. No source was modified; GATE-REL-07 remains BLOCKED.

## Purpose and controlled setup

CP121/122 showed native Ollama `messages[].images[]` requests reaching the synthetic upstream without the configured image-unit amount being settled. This comparison sent the same two synthetic images, at the same 1,000,000 nano-USD/image price, through standard OpenAI `image_url` content on the same `qa004-cp121-priced-vision` model, healthy loopback deployment, and route.

A temporary PROVIDER key was created through the real Owner Console, restricted to the model and `/v1/*`. The cookie-free request was `POST /v1/chat/completions`. The local fixture recorded only path, model, image count, and image SHA-256 values. It confirmed two OpenAI image content blocks in the expected order, returned synthetic HTTP 200 with 10/5 token usage, and received no Authorization header. No external provider was contacted.

The first attempt was invalid as a fixture test: the upstream fixture implemented `/api/chat` only and returned its own 404 when the application called `/v1/chat/completions`. That temporary key was revoked. I added the correct OpenAI-compatible fixture response and image-block parser, then repeated the controlled request. Only the successful retry is used for the pricing conclusion.

## Result

Two images at 1,000,000 nano-USD each have an expected media price of 2,000 micro-USD ($0.002). The successful request returned HTTP 200 and authenticated persistent Chargeback readback reported 2,000 exact micro-USD, zero estimated micro-USD, and one exact request. The real FinOps Chargeback screenshot displayed Exact $0.002000 for the CP123 key. This shows the tested OpenAI `image_url` form is counted on this route and distinguishes the native Ollama media shape observed in DEF-074.

The per-request Trace API for the same `trc_01m8b6qppke00akgdd1jkkz6xg` still says `usage_accuracy=EXACT` and `total_cost_micros=0`. Durable Chargeback says 2,000 micro-USD exact. This is another direct reconciliation case for existing DEF-052; it does not create a duplicate finding or invalidate the separately verified Chargeback ledger settlement.

## Budget and cleanup

The project originally had a 100-micro-USD monthly hard limit, 36 exact plus 14 estimated micro-USD settled, and 50 micro-USD remaining. To permit the valid $0.002 OpenAI control request, the project hard limit was temporarily raised to 5,000 through the real Budgets & Holds UI. After the request, the original 100 limit was restored through the same UI (save HTTP 200), and authenticated readback verified 100. Readback then showed 2,036 exact, 14 estimated, zero active holds, and 1,950 micro-USD negative carryover. The 5,000 limit was not left configured. The real UI screenshot of the restored setting was visually inspected.

Both temporary CP123 keys are `REVOKED` version 2. The original QA Ollama fixture was restored and `/api/tags` returned HTTP 200. Screenshots of the restricted key form, Traces, Chargeback, revoked key inventory, and the budget before/after restoration were visually inspected. The fixture record contains only non-secret metadata and image digests; no prompt or image bytes were retained.

## Classification and remaining scope

- **DEF-074:** Native Ollama `messages[].images[]` settlement/budget bypass remains a Major FAIL. The normalized OpenAI `image_url` path passed this one same-deployment, two-image charge control; this does not establish parity for every dialect, adapter, media type, or request variant.
- **DEF-052:** The successful request's Trace projection is Exact/$0 while durable Chargeback is Exact/$0.002000. This extends the existing trace-versus-ledger defect.
- **New defect:** none.
- **Coverage still open:** native image budget and billing correction/regression, image/media variants, additional adapters, streaming multimodal requests, browser/provider breadth, all other product/security/operational coverage, and full DEF-052 reconciliation across exports and durable rows.

Sanitized machine readback is in [`CP123-openai-image-pricing-results.json`](../evidence/CP123-openai-image-pricing-results.json). Relevant real-browser evidence is listed below.

- `../evidence/CP123-openai-image-key-form.png`
- `../evidence/CP123-openai-image-traces.png`
- `../evidence/CP123-openai-image-chargeback.png`
- `../evidence/CP123-openai-image-key-revoked.png`
- `../evidence/CP123-image-budget-increased.png`
- `../evidence/CP123-image-budget-restored.png`

QA continues; **GATE-REL-07 remains BLOCKED.**
