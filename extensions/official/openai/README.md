# `@tanpopo/openai`

Official Tier-1 `DeclarativeProviderAdapter` for OpenAI (`api.openai.com`).

- **Supported Unified Routes:** `/v1/chat/completions`, `/v1/responses`, `/v1/embeddings`, `/v1/models`
- **Native Passthrough:** `/native/openai/v1/{chat/completions,responses,embeddings,models}`
- **CR-09 Telemetry:** Automatically injects `stream_options: {"include_usage": true}` when `stream: true`, capturing exact prompt/completion/cached/reasoning token counts while eliding trailing usage frames downstream when the caller did not request them.
- **Security:** Blind Host credential injection (`Authorization: Bearer <slot:api_key>`), zero wildcard egress, TLS enforced.
