# CP41 — Native Ollama image-unit settlement

**Status:** PASS (scoped) for historical DEF-074  
**Source:** frozen RC7 `737079ddb847d97dbcf832f610ed0048556cb65b`; compiled binary SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`  
**Runtime/environment:** exact-source SQLite Console/Data Plane `127.0.0.1:18145`; Chromium; deterministic Ollama-compatible loopback fixture `127.0.0.1:19096`; one valid synthetic 1×1 image; no real provider or paid usage.

## Reproduction

1. Through the real Console, declared logical model `qa005-native-vision` with Vision capability, created an active deployment and route to the synthetic Ollama fixture, and set input/output rates to 1,000/2,000 nano-USD per token plus 100,000,000 nano-USD per image unit. A fresh Provider-plane key was restricted to `/native/ollama/*` and all test keys were revoked after the test.
2. Sent cookie-free `POST /native/ollama/api/chat` with `model: qa005-native-vision`, one synthetic image and `stream:false`.
3. The service returned HTTP 200 with `X-Tanpopo-Usage-Accuracy: EXACT`. The trace returned `EXACT`, 200, and 100,015 micro-USD. A read-only query of the isolated SQLite settlement identified `input_tokens=7`, `output_tokens=4`, `media_usage_json={"image_count":1}`, and the applied image-unit rate of 100,000,000 nano-USD. Arithmetic: `7×1,000 + 4×2,000 + 1×100,000,000 = 100,015,000` nano-USD; ceiling division to micro-USD = 100,015.
4. The FinOps model row independently showed exactly $0.100015, one exact request and zero estimated/unmetered requests. The trace and FinOps screenshots were visually inspected.

Sanitized request/response: [`CP41-native-image-call-sanitized.json`](../evidence/CP41-native-image-call-sanitized.json). Admin readback: [`CP41-native-image-readback.json`](../evidence/CP41-native-image-readback.json). Read-only durable row and arithmetic: [`CP41-native-image-settlement-readonly.json`](../evidence/CP41-native-image-settlement-readonly.json). Screenshots: `CP41-native-image-traces.png`, `CP41-native-image-finops.png`.

## Disposition and limits

DEF-074 is a scoped PASS on the native Ollama chat JSON path for one image and one exact response. It does not cover multiple images, larger/blob-backed image inputs, provider image-unit reporting, generated images, images on `/api/generate`, streaming image requests, other adapters, price migrations, or other databases. The provider fixture returned fixed synthetic 7/4 token counters and did not retain image bytes. `GATE-REL-07` remains BLOCKED.
