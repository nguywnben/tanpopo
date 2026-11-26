# CP119 — Native Ollama multimodal image dispatch

**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1` (artifact run 45581179274)
**Result:** PASS (scoped) for a cookie-free, PROVIDER-plane native `/api/chat` request forwarding one synthetic image to an isolated Ollama-compatible endpoint. FAIL (continued evidence for DEF-051, DEF-052, and DEF-057) for usage accuracy reconciliation and logical-model display-name persistence/readback. No new defect ID was assigned. Temporary VKEYs were gracefully revoked. Real-provider egress/spend was zero; no production code or signing material was changed or used.

## Reproduction and observed behavior

On the exact-RC6 test-root listener `127.0.0.1:18128`, in the real Chromium Console, the Org Owner switched to the default project / dev environment and registered a logical model with human display text `QA004 CP119 Ollama Vision Canary`, slug `qa004-cp119-vision-canary`, and declared `streaming` and `vision` capabilities. The create operation returned 201 and authenticated list/readback returned 200. The visible catalog used the slug in place of the entered display name, further reproducing existing DEF-057; this was not assigned a new ID.

The Owner created a self-hosted, zero-rate deployment (`dpl_01m8b4352ve0087hze9p4z40qe`) backed by the synthetic `QA004 synthetic Ollama` connection on loopback port 18125. Readback reported `ACTIVE_HEALTHY`. The Owner created a single-target failover route `qa004-cp119-vision`, then issued two temporary PROVIDER-plane VKEYs limited to `/native/ollama/*` and this model. The credentials remained in process memory and were not captured in screenshots or evidence.

A 69-byte synthetic 1×1 red PNG (SHA-256 `b1ff9c8ea3a780bad09b346c423d2d0e46815926879b18e841d928376a946640`) was sent in one cookie-free JSON request to `POST /native/ollama/api/chat`, with `stream:false`. The instrumented loopback fixture recorded `/api/chat`, the expected model, one user message, `image_count: 1`, `image_base64_chars: 92`, and no Authorization header. It returned HTTP 200 and `Synthetic fixture accepted the image request`. The fixture recorded no prompt, image bytes, or credential value, and no external provider was contacted.

A prior attempt used the original fixture before the instrumented fixture had replaced it. It also produced an HTTP 200 trace, but because that attempt lacked the image-aware sanitized log, it is excluded from the scoped forwarding conclusion. The instrumented attempt is the only one counted as verified image forwarding.

## Cross-screen and persistence results

- Traces API readback returned HTTP 200. The two attempt rows for this canary showed HTTP 200, no error, `usage_accuracy: EXACT`, and `total_cost_micros: 0`. The real Traces screen displayed both as “Exact”; the instrumented request is `trc_01m8b4en9ne00aa2cjnbf6m5hf`.
- The real Chargeback screen grouped by Virtual Key showed one estimated request and zero exact requests for each temporary key, both at a zero total amount because this synthetic self-hosted deployment had a zero rate. This conflicts with the successful trace row’s Exact label and extends DEF-052 into native image input. The synthetic fixture returned Ollama `prompt_eval_count: 8` and `eval_count: 7`; FinOps still classified the request as estimated, extending DEF-051 to this multimodal `/api/chat` journey.
- These screens and readbacks are not evidence of a nonzero-price settlement; this route was intentionally zero-rate. No claim is made about dollar charge accuracy in CP119.
- The Console’s Access & Keys view used “Revoke (graceful drain)” for both temporary keys. It showed both rows as Revoked and stated “0 active streams severed.” Authenticated `GET /admin/v1/virtual-keys?limit=100` confirmed both IDs in state `REVOKED`, version 2. No plaintext key was retained. The model, deployment, route, request and trace remain in the isolated QA database for reproducibility.
- The image-aware fixture was stopped after the attempt and the original QA Ollama mock was restarted and returned HTTP 200 from `/api/tags`.

## Evidence

- `../evidence/CP119-vision-model-registered.png` and `CP119-vision-deployment-created.png` — real Console model/deployment setup; screenshots visually inspected.
- `../evidence/CP119-vision-route-created.png` — route setup.
- `../evidence/CP119-vision-traces.png` — two 200 rows, visually inspected.
- `../evidence/CP119-vision-chargeback-vkey.png` — per-key 0 exact / 1 estimated request rows, visually inspected after resizing a temporary review copy; original evidence image is unchanged.
- `../evidence/CP119-vision-keys-revoked.png` — both keys visibly Revoked after graceful drain, visually inspected.
- `../evidence/CP119-native-vision-results.json` — sanitized response, synthetic image digest/size, fixture receipt, trace readback, FinOps readback, and revocation metadata. It contains no credential or prompt/image content.

## Limits and remaining coverage

This is one valid static PNG in a non-streamed native Ollama chat on one local self-hosted route. It does not establish image understanding quality, multi-image ordering, URL image behavior, malformed or oversized media handling, MIME mismatch behavior, streaming multimodal frames, video/audio, SDK interoperability, other Ollama native operations, OpenAI-compatible image forwarding, another runtime role, multiple targets, failure/retry behavior, budget enforcement, or nonzero-priced multimodal usage. Anthropic/Gemini remain blocked by authentic release-signature readiness; this test does not bypass that policy. No source was changed. QA continues; **GATE-REL-07 remains BLOCKED.**
