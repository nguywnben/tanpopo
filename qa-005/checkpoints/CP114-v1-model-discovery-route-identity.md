# CP114 — authorized `/v1/models` discovery and route model identity

**Result:** PASS (scoped; no new defect).  
**Source:** `737079ddb847d97dbcf832f610ed0048556cb65b`; exact-source binary.  
**Environment:** isolated SQLite clone at `127.0.0.1:18164`, local OpenAI-compatible fixture at `127.0.0.1:19090`, Org Owner, Chromium 156. No paid provider calls.

## Steps and outcome

1. In the real Access & Keys Console, create a temporary PROVIDER-plane key scoped to `QA005 Org / QA005 No Budget Project / QA005 Budgetless Dev`, with route allowlist `*`.
2. Call `GET /v1/models` using that bearer credential. HTTP 200 returns only models authorized in the workspace, including the route’s logical model slug `qa005-playground-low`.
3. Send `POST /v1/chat/completions` with model `qa005-playground-low` and route `qa005-playground-openai`. The loopback provider returns HTTP 200 and the response identifies the logical model slug.
4. As a negative alias check, use the configured upstream provider model name `qa005-model` in the same request. It returns HTTP 503 `no_compliant_deployment_available`; this is expected for an identifier that is not the authorized logical model advertised by `/v1/models`.
5. Revoke the test key through the authenticated Admin session and verify HTTP 200.

The HTTP result and returned model list are preserved in [sanitized evidence](../evidence/CP114-model-discovery-routing.json). No credential secret or provider payload is included.

## Limits

One Owner, project/environment scope, route and SQLite backend were tested in Chromium. Other roles, multiple routes/models, pagination, all protocol adapters, SDK behavior, PostgreSQL/Valkey, alternative browsers and live-provider interoperability remain unverified. This is not a full `/v1/models` or authorization matrix pass.
