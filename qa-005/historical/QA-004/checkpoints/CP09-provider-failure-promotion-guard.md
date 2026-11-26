# CP09 — Failed synthetic provider remains unverified and cannot be promoted

**Outcome:** PASS for the tested RC6 Ollama connection/deployment promotion guard (historical DEF-002 scoped). Broader provider recovery, route selection/failover, other adapter types, and runtime roles remain PARTIAL/UNVERIFIED.

## Deterministic failure fixture

A QA-only local HTTP service bound to `127.0.0.1:18126` returned HTTP 401 for every request. The fixture log is outside this repository. No Google endpoint, Cloud egress, live provider, or credential was used. The real Console required explicit `127.0.0.1/32` private-address consent before creating the test connection.

## Real browser flow and persisted state

1. In Chromium, opened Providers and created `QA004 synthetic Ollama 401` using the bundled Ollama adapter and loopback endpoint. UI initially labeled it Unverified; API readback recorded state `CREATED_UNVERIFIED`, version 1.
2. Ran the real “Probe connection” action. Tanpopo displayed `upstream returned HTTP 401`; the connection remained `CREATED_UNVERIFIED`, version 3. The healthy fixture remained independently Active.
3. In Models, added `qa004-mock-failing` against the failed connection. Console reported “Deployment registered in provisioned state”; the visible row showed “Provisioned” and “Connection: Unverified.” Persistent API readback agreed (`PROVISIONED`, `CREATED_UNVERIFIED`).
4. Opened the deployment action menu: only Disable and Delete appeared; Activate was not offered.
5. Independently attempted the authenticated API state transition to `ACTIVE_HEALTHY` with the current entity version. RC6 returned HTTP 422 `connection_not_active`; a new API read showed the deployment still `PROVISIONED`, version 1.

This is a fresh RC6 retest; QA-001 historical failure evidence and later QA-002/003 guarded-path evidence are all preserved. The tested current paths agree with QA-002/003. The old QA-001 result remains a historical discrepancy outside this adapter/provisioning path, not silently erased.

## Evidence / visual inspection

- `evidence/CP09-provider-failure-form.png`, `CP09-provider-failure-created.png`, `CP09-provider-failure-probed.png` — real UI flow; screenshots visually inspected. The last shows the failure banner and connection still Unverified.
- `evidence/CP09-provider-failure-created.json` and `CP09-provider-failure-probe.json` — sanitized connection/probe state.
- `evidence/CP09-failed-deployment-form.png`, `CP09-failed-deployment-created.png`, `CP09-failed-deployment-readback.json` — deployment form/result and persisted state. Created screenshot was visually inspected.
- `evidence/CP09-provider-promotion-guard.json`, `CP09-provider-promotion-guard.png` — UI action list and API 422 plus unchanged state. Screenshot visually inspected; no Activate action shown.

## Limits

The failed deployment was not routed to a public route and no request was sent to the failing upstream. A probe test covers only HTTP 401; DNS, TLS, timeout, malformed responses, provider recovery, route eligibility, failover, and role-specific health transitions remain UNVERIFIED. This result is a scoped regression pass, not a claim that every historical QA-001 circumstance is reconciled.
