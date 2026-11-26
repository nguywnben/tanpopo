# CP94 — Native idempotency replay and per-key isolation

**Baseline:** exact frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`
**Outcome:** PASS (scoped) — synchronous native requests replayed without a second upstream call, changed payload was rejected, and the same idempotency key under a second Virtual Key dispatched independently. No new defect assigned.
**Gate:** GATE-REL-07 remains BLOCKED. Product source unchanged.

## Correct scope and setup

The initial CP93 attempts were issued in `QA-004 Isolated Canary`, where no provider route exists. They returned 503 and made no fixture calls; this is retained in [CP93 setup evidence](../evidence/CP93-idempotency-results.json) and was excluded from the idempotency result. I used the real workspace picker to select `QA004 Test Root Org / default / dev`, where the configured route `qa004-loopback` has one `ON_PREM` deployment. The request used `X-Tanpopo-Data-Region: ON_PREM`.

The existing deterministic synthetic Ollama fixture at `127.0.0.1:18125` was already configured as the route target. Through the real Access & Keys UI, I issued two temporary PROVIDER keys, each restricted to `/native/ollama/*`. Requests were cookie-free. The fixture records only path, model, stream flag, authorization-header presence, body keys and content digest; no caller credential was present upstream.

## Request/replay lifecycle

With key A and one `Idempotency-Key`, the first native `/api/chat` request returned HTTP 200 and `Native synthetic response.`. Repeating the identical payload returned HTTP 200 with the same body SHA-256; the upstream request count stayed at one for this key. Reusing the same key with a modified payload returned 422 `idempotency_key_reused_mismatch`, with no additional fixture request.

Key B used the same `Idempotency-Key` and same request payload. It returned HTTP 200 and added exactly one independent upstream call, confirming idempotency scope is isolated by Virtual Key for this tested pair. A further identical replay on key A did not increase the count. Both keys were gracefully revoked (HTTP 200, persisted `REVOKED` v2); key A replay after revocation returned 401 `invalid_virtual_key`.

The actual default/dev Access & Keys screenshot was visually inspected; it shows the two key rows, their `/native/ollama/*` grants, and Revoked state: [key inventory](../evidence/CP94-idempotency-keys-revoked.png). The selected scope screenshot was also inspected: [default/dev scope](../evidence/CP94-default-dev-scope.png). Sanitized request IDs, body digests, response codes and fixture counts are in [results](../evidence/CP94-idempotency-results.json).

## Coverage boundary

This is one synchronous native route, one synthetic deployment, two Virtual Keys and one isolated SQLite runtime. Streaming idempotency, concurrent first claims, process restart, cross-node/distributed storage, key rotation during a claim, in-progress conflict behavior, retention expiry and broader route/role boundaries remain UNVERIFIED. Native response usage is labeled Estimated, consistent with existing DEF-051/052; this test did not reassess billing correctness. QA continues; **GATE-REL-07 remains BLOCKED**.
