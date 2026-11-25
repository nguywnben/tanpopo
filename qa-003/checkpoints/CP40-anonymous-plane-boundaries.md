# CP40 — Anonymous Admin and Data Plane boundary probes

**Baseline:** frozen RC5 `3423d4f523ef0ac77469a1201b8b753d1d31c902`<br>
**Runtimes:** isolated SQLite `18116` and A2A-enabled SQLite `18107`<br>
**Disposition:** all sampled protected reads/calls rejected anonymous access; documented public health probes returned status only. QA-003 remains incomplete; `GATE-REL-07` remains **BLOCKED**.

## Reproduction and result

Sent requests without cookies, bearer tokens, or other credentials to three Admin API reads (`/admin/v1/organizations`, `/admin/v1/audit-events`, `/admin/v1/connections`), unified inference (`POST /v1/chat/completions`), A2A discovery (`GET /a2a/agents`), and MCP initialization (`POST /mcp`). The Admin reads returned HTTP 401 `unauthenticated`; inference returned 401 `invalid_virtual_key`; A2A returned 401 `invalid_credential_plane`; MCP returned 401 `invalid_virtual_key`. No tenant rows, audit data, provider configuration, or agent directory were returned. The synthetic inference prompt had no provider key and was rejected before execution.

Unauthenticated `/healthz` and `/readyz` returned HTTP 200 with only the documented top-level `status` field. Sanitized per-request statuses, media types, and error codes are in [CP40 evidence](../evidence/CP40-anonymous-boundary-result.json); no response bodies containing environment details are retained.

## Coverage and limits

- **PASS, scoped:** no-credential boundary for the sampled Admin, inference, MCP, and A2A routes; liveness/readiness expose a status-only envelope.
- **UNVERIFIED:** other endpoints/methods, malformed or expired credentials, CSRF, CORS, proxy/header spoofing, rate limits, all roles, and the complete OpenAPI auth matrix.
- No new defect was confirmed. This sample does not establish global authorization correctness.
