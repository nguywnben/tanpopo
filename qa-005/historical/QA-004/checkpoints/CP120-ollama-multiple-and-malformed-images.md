# CP120 — Multiple and malformed native Ollama image inputs

**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1` (artifact run 45581179274)
**Result:** PASS (scoped) for ordered two-image forwarding to a synthetic native Ollama upstream and propagation of that endpoint's malformed-image HTTP 400 as `upstream_bad_request`. FAIL (additional evidence for DEF-052) because the trace for both the successful and rejected requests is labeled Exact/$0. No new defect ID assigned. The single test VKEY was gracefully revoked and confirmed `REVOKED` version 2; the original loopback fixture was restored.

## Reproduction

On isolated exact-RC6 runtime `127.0.0.1:18128`, the real Chromium Owner session selected the `QA004 Test Root Org / default / dev` scope and issued one PROVIDER-plane VKEY limited to `/native/ollama/*` and the model `qa004-cp119-vision-canary`. The human-visible issue form verified both the route pattern and single allowed model.

A temporary local-only Ollama-compatible fixture on `127.0.0.1:18125` logged request path, model, image count, base64 validity, ordered SHA-256 hashes of decoded images, and whether the provider request carried an Authorization header. It did not save prompt, image bytes, or any credential. Through the cookie-free Data Plane:

1. `POST /native/ollama/api/chat` with a user message containing two PNG base64 images (synthetic 1×1 red then 2×1 green) returned HTTP 200. The fixture recorded `image_count: 2`, two valid images in the submitted order, and hashes `b1ff9c8e…46a946640` then `9d9e18e5…4208cc00`. The request body contained no real or sensitive data.
2. A second request with one malformed base64 image string reached the fixture, which returned synthetic HTTP 400. The Data Plane trace returned status 400 with `upstream_bad_request`; the invalid payload did not trigger external egress. This checks propagation at the synthetic integration boundary and does not establish application-side base64 validation.

Authenticated trace readback and the actual Traces UI showed both requests associated with the canary route: one 200 and one 400 `upstream_bad_request`. Both trace records report `usage_accuracy: EXACT` and `total_cost_micros: 0`, including the rejected request. This extends the existing DEF-052 trace-projection defect; the test deployment has zero pricing, so no claim is made about charge amount. The real Traces screenshot was visually inspected.

The real Access & Keys UI then used graceful drain to revoke the test key, showed the success banner and Revoked row, and authenticated API readback confirmed `REVOKED`, version 2. The scoped fixture was stopped and the prior QA Ollama fixture restarted; `/api/tags` returned HTTP 200.

## Evidence

- `../evidence/CP120-native-image-boundary-results.json` — sanitized request/fixture/trace/revocation facts; no prompt or image bytes.
- `../evidence/CP120-multi-image-key-scope.png` — real UI key route/model restrictions.
- `../evidence/CP120-multi-image-traces.png` — real Traces rows for successful and rejected requests, visually inspected.
- `../evidence/CP120-multi-image-key-revoked.png` — real graceful-revoke state and banner, visually inspected.

## Limits

This verifies two small inline PNGs in one non-streamed Ollama chat and one malformed base64 error from a synthetic upstream. It does not verify native multi-image comprehension, URL image inputs, payload size/count boundaries, MIME mismatch, streaming images, alternate media formats, OpenAI-compatible image translation, image billing at a nonzero price, or another adapter/runtime. No source or release-signing material was modified or used. QA continues; **GATE-REL-07 remains BLOCKED.**
