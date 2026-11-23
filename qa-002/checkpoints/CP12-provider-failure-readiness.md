# CP12 — Local failed-provider readiness and fail-closed route

**Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`  
**Runtime:** Isolated SQLite on `127.0.0.1:18084`; bundled Ollama adapter; synthetic loopback provider at port 19107 returns HTTP 401 on every request. No Google access, no source changes.

## DEF-002 independent retest

Created `QA002 Failing Ollama 401` through Providers → Connections, using the local synthetic endpoint and explicit loopback access. The live “Probe connection” action returned an Admin API success envelope with `valid=false`, while the UI showed the synthetic rejection message and left the connection `Unverified`. Screenshot: `../evidence/CP12-def002-probe-failed.png`.

Created a deployment against the existing QA logical model with the failed connection. It persisted as `PROVISIONED`. The real deployment action menu contained Disable and Delete only; no Activate action was offered. Screenshot: `CP12-def002-deployment-unverified.png` and visually inspected action menu `CP12-def002-provisioned-actions.png`.

As an additional backend guard check, issued an owner-authenticated, CSRF-protected Admin API `PATCH /admin/v1/deployments/dpl_01m850zp5qe0083acpb9f738e5` requesting `ACTIVE_HEALTHY` with the current ETag. The API returned HTTP 422 `connection_not_active`, explicitly identifying the backing connection as `CREATED_UNVERIFIED`. The deployment stayed PROVISIONED. Record: `../evidence/CP12-def002-api-rejection.json`; post-attempt UI screenshot `CP12-def002-api-activated.png` was inspected and still showed Provisioned.

Created route `qa002-failed-provider-route` through the real Routes UI with only this deployment. The route’s Target health displayed Provisioned, not Healthy (`CP12-def002-route-created.png`, visually inspected). An actual provider-key `POST /v1/chat/completions` against the route returned HTTP 503 `no_compliant_deployment_available`; sanitized response is `CP12-def002-dataplane.json`. No upstream call can succeed because the fixture deliberately rejects requests.

**Result: DEF-002 VERIFIED FIXED (scoped).** The failed connection cannot be promoted through either UI or Admin API; route readiness remains accurate and the gateway fails closed. This directly tests local adapter/auth failure and does not classify the separate Google `probe_runtime_unavailable` / `upstream_unreachable` history as product defect. The failure modes outside local Ollama, provider recovery after credentials/network repair, and multi-target failover remain UNVERIFIED.

GATE-REL-07 remains **BLOCKED**. QA-002 continues.
