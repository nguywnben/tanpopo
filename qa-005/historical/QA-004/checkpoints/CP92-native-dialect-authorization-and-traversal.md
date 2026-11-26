# CP92 — Native dialect key boundary and encoded traversal

**Baseline:** exact frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`
**Outcome:** PASS (scoped) for native route-grant denial and encoded traversal rejection; BLOCKED for an actual Anthropic native dispatch. No new defect assigned.
**Gate:** GATE-REL-07 remains BLOCKED. Product source unchanged.

## Setup and interaction

In the isolated A2A-enabled RC6 clone at `127.0.0.1:18128`, the real Owner Console issued a temporary PROVIDER-plane key restricted to `/native/anthropic/*`. The Access & Keys inventory displayed the exact scope and Active state. The one-time secret was used only in this isolated probe and was not written to evidence. After requests, the key was revoked; authenticated API readback reports state `REVOKED`, version 2. The create form, active key inventory, and final revoked inventory screenshots were visually inspected: [key form](../evidence/CP92-native-dialect-key-form.png), [created key](../evidence/CP92-native-key-created.png), and [revoked key](../evidence/CP92-native-key-revoked.png).

## Results and classification

A cookie-free authorized `POST /native/anthropic/v1/messages` for the existing logical model returned HTTP 503 `no_compliant_deployment_available`. This key was bound to `QA-004 Isolated Canary`, which has no provider route; the only provider route in this runtime is in `default/dev`. Therefore this 503 is a workspace-scope setup result, not an Anthropic adapter or infrastructure failure, and it does not test the dialect-mismatch contract. CP95 repeats the request from the correct `default/dev` scope and verifies the expected HTTP 400 `dialect_mismatch`. Actual positive Anthropic upstream dispatch remains UNVERIFIED because no Anthropic deployment is configured.

The same key's request to `/native/ollama/api/chat` returned HTTP 403 `scope_or_plane_denied`, as required by its `/native/anthropic/*` grant. A raw encoded traversal probe to `/native/anthropic/v1/%2e%2e/organization/admin_api_keys` returned HTTP 403 `native_path_not_allowed`. These are scoped PASS results for path authorization and encoded traversal handling. This did not configure or contact an external provider. Sanitized request IDs and all probe classifications are in [results](../evidence/CP92-native-dialect-results.json).

## Coverage boundary

This does not verify Anthropic or Gemini SDK compatibility, lossless JSON/SSE, authentication brokerage, usage extraction, traces, fallback, or the same-dialect mismatch response when an incompatible target is selectable. Those remain UNVERIFIED; positive Anthropic/Gemini dispatch needs a correctly provisioned synthetic native deployment. The cross-dialect rejection itself now passes in CP95 without weakening trust or authorization. No defect was added. QA continues; **GATE-REL-07 remains BLOCKED**.
