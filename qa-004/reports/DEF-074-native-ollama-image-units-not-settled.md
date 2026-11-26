# DEF-074 — Native Ollama image inputs are not charged by image-unit price cards

**Severity:** Major  
**Status:** FAIL — independently reproduced on frozen RC6 with a healthy synthetic upstream and non-zero image-unit pricing.  
**Affected workflows:** Native Ollama `/api/chat` and `/api/generate` multimodal inference, image-unit pricing, usage settlement, budgets, Chargeback, and customer/project cost attribution.

## Reproduction

On isolated exact-RC6 runtime `127.0.0.1:18128`, an Org Owner used the real Chromium Console in `QA004 Test Root Org / default / dev` to:

1. Register a `vision:true` logical model `qa004-cp121-priced-vision`.
2. Create a healthy self-hosted loopback deployment on the synthetic `QA004 synthetic Ollama` connection, with token rates set to zero and `Image unit (nano-USD)` set to `1,000,000`. The deployment API readback retained `image_unit_nano_usd: 1000000` and `self_hosted_zero_rate: false`.
3. Create the single-target route `qa004-cp121-priced-vision` to that deployment and issue a PROVIDER-plane VKEY restricted to `/native/ollama/*` and that model.
4. Send one cookie-free `POST /native/ollama/api/chat` containing exactly two valid synthetic PNG base64 values in `messages[0].images`.

The local Ollama-compatible fixture recorded one `/api/chat` call, the requested model, two images in order, and the expected distinct SHA-256 digests. It returned synthetic HTTP 200 with text and Ollama `prompt_eval_count: 8`, `eval_count: 7`. It recorded no Authorization header, prompt, image bytes, or credentials. No external provider was contacted.

## Expected versus actual

The deployed rate card charges 1,000,000 nano-USD per image. Two images therefore represent 2,000,000 nano-USD = **2,000 micro-USD ($0.002)**, before any token charges (which were intentionally zero).

Actual authenticated `GET /admin/v1/usage/summary?group_by=key` readback and the real FinOps → Chargeback screen report for this VKEY:

- `exact_micro_usd: 0`
- `estimated_micro_usd: 0`
- `exact_request_count: 0`
- `estimated_request_count: 1`
- `unmetered_request_count: 0`

The successful request trace `trc_01m8b5pv3ve008emhjergfc88e` is labeled `EXACT` with `total_cost_micros: 0`; this accuracy/cost disagreement is also within existing DEF-052. The independent defect here is the missing **image-unit charge** on a successful request, even with a non-zero persisted image price. This is not merely DEF-051's token-counter issue: token rates were zero and image pricing alone should have generated $0.002.

## Evidence

- [CP121 checkpoint](../checkpoints/CP121-native-ollama-image-unit-settlement.md)
- `../evidence/CP121-native-image-pricing-results.json` — sanitized request, rate-card, fixture, trace, persistent chargeback, and revocation readback.
- `../evidence/CP121-priced-image-deployment-form.png` — real pricing form shows image unit 1,000,000 nano-USD and zero other rates; visually inspected.
- `../evidence/CP121-priced-image-deployment-created.png` and `CP121-priced-vision-route-created.png` — actual UI-created deployment and route.
- `../evidence/CP121-priced-image-traces.png` — request 200, visually inspected.
- `../evidence/CP121-priced-image-finops.png` — VKEY row shows estimated $0.000000 and 0 exact / 1 estimated request, visually inspected.
- `../evidence/CP121-priced-image-key-revoked.png` — graceful key revocation, visually inspected; API confirms `REVOKED` version 2.

## Project-budget boundary retest (CP122)

CP122 repeated the image request with an existing Monthly project hard limit of 100 micro-USD, of which 50 micro-USD had already been settled. A correct pre-egress hold for two images at the configured 1,000 micro-USD/image is 2,000 micro-USD and must be rejected before dispatch. Exact RC6 returned HTTP 200; the synthetic fixture received both images. Persistent budget readback remained at exact 36 + estimated 14 micro-USD with zero hold, and VKEY chargeback recorded 0 charge / 1 estimated request. This confirms DEF-074 also allows native image requests to pass a hard project-budget boundary without reserving their image cost. See [CP122](../checkpoints/CP122-image-priced-request-project-budget-boundary.md) and its screenshots/results.

## Root-cause hypothesis (not confirmed)

Read-only inspection of RC6's native request media estimator suggests it traverses `messages`, recognizes singular image fields such as `image`, `image_url`, and `image_base64`, but does not appear to recognize Ollama's plural `messages[].images` array of base64 strings. CP125 shows the same missing charge for `/api/generate` top-level `images[]`; the estimator’s handling of that request body remains a separate code-path hypothesis, not confirmed by runtime evidence. The actual FinOps results establish missing charges on both tested endpoints but do not alone prove one shared code path is the sole cause. Confirm request media extraction and the settlement fallback with the responsible engineering owner.

## Business impact

Every accepted native Ollama image request can be undercharged or omitted from image-based budgets and chargeback when its deployment has a non-zero per-image rate. Operators see one successful request but no associated media charge, obscuring image workload cost and weakening budget controls. CP119/CP120 confirmed image forwarding with zero-rate cards; CP121 closes that gap and reproduces missing image pricing with a non-zero card.

## Acceptance criteria

1. For native Ollama `/api/chat`, count each accepted item in `messages[].images[]`, and for `/api/generate` count each accepted item in top-level `images[]`, when deriving request media usage, unless exact upstream `MediaUsage.image_count` is present and authoritative.
2. At the CP121/CP125 fixture and rate card, successful two-image requests through both `/api/chat` and `/api/generate` produce two image units and settle 2,000 micro-USD ($0.002) with zero token charge. At the CP122 100-micro project budget and 50-micro prior use, each such native request is rejected before fixture egress with `429 project_budget_exceeded`.
3. The settlement accuracy distinguishes request-observed image count from estimated token usage, and the response header, trace, durable ledger, Chargeback UI/API, and exports reconcile per request and in aggregate.
4. Invalid images or unsuccessful upstream calls follow documented charge/hold-release semantics; a rejected request must not be charged as a completed inference.
5. No image is counted twice when provider-reported media usage is present, and multi-image order/count, empty arrays, missing arrays, malformed base64, budget limits, streaming, and restart persistence have regression coverage.
6. Preserve the CP123 normalized OpenAI `image_url` positive control (two images settled 2,000 exact micro-USD on this same Ollama-backed model/deployment) while fixing native Ollama media extraction. Test every declared native adapter with its own request shape and preserve size/capability pre-egress checks.

No source was modified. No production credential or external provider was used. **GATE-REL-07 remains BLOCKED.**


## Normalized OpenAI image control and scope refinement (CP123)

CP123 used the same model, healthy Ollama-backed deployment, 1,000,000 nano-USD/image rate card, and two image files, but submitted standard OpenAI `image_url` content. After correcting a local fixture that initially returned a harness-generated 404 for `/v1/chat/completions`, a valid retry delivered both expected image SHA-256 values to the synthetic upstream. The request returned 200 and durable Chargeback recorded exactly 2,000 micro-USD, one exact request. This is a positive control: it narrows DEF-074 to the native Ollama `messages[].images[]` path for the tested shape and shows the same price/deployment can settle normalized OpenAI images. It does not establish all OpenAI media variants or other adapters. The same request Trace reads Exact/$0, separately reinforcing DEF-052. See [CP123](../checkpoints/CP123-normalized-openai-image-pricing-comparison.md) and its sanitized results/screenshots.


## Normalized-path hard-budget control (CP124)

With the project’s restored 100-micro-USD hard limit and settled usage already over that limit, CP124 sent a standard OpenAI two-image request through the same model/deployment. RC6 returned 429 `project_budget_exceeded` before the dedicated local fixture recorded any dispatch. The trace retained the rejection; Chargeback had no row; there was no hold or additional project settlement; and the project budget values/version remained unchanged. The temporary key was revoked and the original fixture restored. This provides a scoped normalized-path budget positive control and further narrows the observed budget bypass to native Ollama `messages[].images[]`; it does not cover other adapters, concurrent holds, or clustered enforcement. See [CP124](../checkpoints/CP124-normalized-openai-project-budget-boundary.md), its sanitized result, and visually inspected screenshots.


## Native Ollama Generate retest (CP125)

CP125 sent two valid ordered synthetic PNGs through native `POST /native/ollama/api/generate` using Ollama’s top-level `images` array. The same vision model, active loopback deployment, route and 1,000,000-nano-USD/image rate card were used. The local fixture received both exact image hashes and returned 200 with completion counters. The expected media charge is 2,000 micro-USD; persistent Chargeback instead reports zero exact and estimated micro-USD and one estimated request. The trace says Exact/$0. This independently reproduces the missing image-unit charge on `/api/generate` and extends the existing Trace/Chargeback discrepancy DEF-052. See [CP125](../checkpoints/CP125-native-generate-image-unit-settlement.md) and its sanitized evidence.

Together CP121 and CP125 establish the defect on the two tested native Ollama image request shapes. CP123/124 demonstrate that standard OpenAI `image_url` inputs on this same model/deployment were priced and rejected before egress when over the restored project ceiling. Do not generalize the positive controls to other adapters or media formats without testing them.
