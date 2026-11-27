# CP23 — DEF-002 failed-provider promotion guard

**Source/runtime:** exact RC7 source `737079ddb847d97dbcf832f610ed0048556cb65b`, local binary SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`; isolated SQLite runtime at `127.0.0.1:18145`; Chromium real UI; local HTTP 401 fixture at `127.0.0.1:19091`; synthetic owner and project. QA-only `@tanpopo/ollama` approval used the ephemeral test trust root already documented in environment identity. No external provider or credential.

## Result: PASS (scoped guard)

1. Created a real provider connection through Providers with explicit `127.0.0.1/32` private-address consent. Initial state was `CREATED_UNVERIFIED`.
2. Used its real Probe connection action. The local fixture returned HTTP 401 for `/v1/models`; the API response was HTTP 200 with `valid:false`, `auth_failed`, and sanitized `upstream returned HTTP 401`. Persistent UI state remained Unverified.
3. Registered a synthetic logical model and deployment through Models & Deployments against that failed connection. Deployment creation returned HTTP 201; the Console showed `PROVISIONED` and `Connection: Unverified`. The deployment action menu contained Disable and Delete, with no Activate action.
4. Independently attempted `PATCH /admin/v1/deployments/{id}` with the exact current `If-Match: "v1"` and `state: ACTIVE_HEALTHY`, using the authenticated QA owner session. The server returned HTTP 422 `connection_not_active`. Readback stayed `PROVISIONED`, `CREATED_UNVERIFIED`, version 1. UI and failure-state screenshots were visually inspected.
5. Deleted the synthetic deployment and failed connection through the real Console. Stopped the fixture. The synthetic logical model declaration remains without a deployment; no model-delete action is offered in the Console.

**Expected:** a provider failing its live verification stays unverified; a deployment created against it cannot become Active/Healthy by UI or API. **Actual:** this exact failed-probe path is guarded. This fresh RC7 result independently supports the scoped QA-004 PASS and does not resolve the earlier QA-001 conflicting evidence outside this Ollama/provisioning path.

Sanitized evidence: `evidence/CP23-provider-promotion-guard.json`. Visually inspected screenshots: `CP23-provider-401-created.png`, `CP23-provider-401-probed.png`, `CP23-failed-deployment-created.png`, `CP23-failed-deployment-actions.png`, `CP23-failed-deployment-after-block.png`, and `CP23-provider-test-cleanup.png`.
