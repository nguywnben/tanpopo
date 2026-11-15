# `@tanpopo/ollama`

Official Tier-1 `DeclarativeProviderAdapter` for local and LAN Ollama instances (`http://localhost:11434`, `ollama.internal`).

- **Billing Mode Default:** `SELF_HOSTED_INFRA` (paired with explicit administrator opt-in for loopback/LAN egress).
- **Auth Modes:** `NONE` (zero credential injection, `secret_slots: {}`) and `BEARER` (`Authorization: Bearer <slot:api_key>` for authenticated proxies).
- **Capabilities:** `/v1/chat/completions`, `/v1/embeddings`, `/v1/models`, and `/native/ollama/*`.
