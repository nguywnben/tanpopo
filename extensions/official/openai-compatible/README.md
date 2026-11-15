# `@tanpopo/openai-compatible`

Official Tier-1 `DeclarativeProviderAdapter` for OpenAI-compatible inference servers (`vLLM`, `SGLang`, `TGI`, `LM Studio`, internal OpenAI gateways).

- **Private Network / Self-Hosted Egress:** Supports explicit private/internal hostnames and configurable `endpoint_base_url` + private CIDR policies enforced by Host `SSRFGuard`.
- **CR-09 Telemetry:** Automatically injects `stream_options: {"include_usage": true}` on streaming requests so self-hosted vLLM/SGLang gateways emit exact terminal usage.
- **Auth Modes:** Supports `bearer_api_key` (`Authorization: Bearer <slot:api_key>`) and `none`.
