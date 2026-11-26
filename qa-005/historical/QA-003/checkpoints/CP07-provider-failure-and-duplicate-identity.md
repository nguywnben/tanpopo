# CP07 — Provider failure guard, connection identity, and disable recovery

**Baseline:** frozen RC5 source `3423d4f523ef0ac77469a1201b8b753d1d31c902`
**Status:** fresh local failure-protection path passed for DEF-002; DEF-032 is partially remediated; one new lifecycle defect is recorded as DEF-040. Product source was not changed. `GATE-REL-07` remains **BLOCKED**.

## Environment and procedure

- Used the isolated SQLite runtime on `127.0.0.1:18103`, a real Chromium browser through the local HTTPS preview, the already activated official Ollama adapter, and a synthetic endpoint on `127.0.0.1:19187`.
- The fixture deterministically returned HTTP 401 with a sanitized `synthetic unauthorized` message. It received only the adapter's model-list probe; no model inference or external provider request occurred. Provider spend remains USD 0.
- Marketplace permissions were reviewed in the Console before enabling the adapter. The exact private endpoint was allowed only as `127.0.0.1/32`; SSRF controls were not weakened.
- Reproduction screenshots and sanitized API readbacks are in `../evidence/CP07-*`. They were inspected visually after capture.

## DEF-002 — failed provider cannot be promoted to healthy

1. From Extensions → Marketplace, stage and activate the official Ollama adapter after reviewing its declared permissions. See `CP07-ollama-marketplace-filtered.png`, `CP07-ollama-staged.png`, `CP07-ollama-permission-review.png`, and `CP07-ollama-activated.png`.
2. In Providers, create `QA003 RC5 Ollama 401` using the mock endpoint, Self-hosted billing, On-prem region, private access, and exact `127.0.0.1/32` allowlist. A browser locator timed out during the first automation attempt; the UI had in fact created the first record. A second identical test record was created by the retry. Both synthetic records are retained; this harness behavior is not classified as a product defect. See `CP07-ollama-connection-created.png`.
3. Select the first connection by visible suffix `0g0485nk` and use **Probe connection**. The fixture received `GET /v1/models` and returned 401. Admin API response was HTTP 200 with `valid=false`, `failure_code=auth_failed`, and `sanitized_message=synthetic unauthorized`. The UI displayed the failure and the connection remained `CREATED_UNVERIFIED`. See `CP07-ollama-probe.json` and `CP07-ollama-probe-failed.png`.
4. In Models & Deployments, register `qa003-synthetic-unauthorized`, then create a zero-rate Self-hosted deployment against that connection. The UI stated “Deployment registered in provisioned state.” The create API returned 201, `state=PROVISIONED`, and `connection_state=CREATED_UNVERIFIED`.
5. Using the authenticated browser session, attempt `PATCH /admin/v1/deployments/{id}` with `state=ACTIVE_HEALTHY`, the session CSRF token, and the returned ETag. The API rejected it with HTTP 422 `connection_not_active`. A fresh list read returned the same deployment as `PROVISIONED` at version 1. See `CP07-deployment-negative-state.json` and visually inspected `CP07-deployment-provisioned.png`.

**Disposition:** DEF-002 is **VERIFIED FIXED for this fresh RC5 local Ollama authentication-failure path**. This does not retest a routed inference result, successful recovery, other adapters, provider failover, or Google-specific failures. The observed behavior is independent of the earlier Cloud Google-egress limitation.

## DEF-032 — duplicate connection identity

The Providers table now displays a short stable ID suffix for each duplicate connection, and the edit drawer title repeats that suffix (`0g0485nk`). Opening the first row's action menu and disabling it changed only that ID; the second connection (`4hst80xy`) stayed `CREATED_UNVERIFIED`. This positively verifies targeted row action behavior in this repro. Screenshots: `CP07-provider-duplicate-edit.png`, `CP07-provider-duplicate-actions.png`, and `CP07-provider-disable-confirm.png` (the last shows the post-action state).

However, the real deployment form still shows two identical choices, both labeled `QA003 RC5 Ollama 401 (SELF_HOSTED_INFRA)`, without an ID or other scope. See `CP07-connection-selector-duplicates.png`. This leaves the provider identity issue open for model-to-connection binding. DEF-032 is **PARTIAL**, not closed.

## DEF-040 — disabling an unverified connection is terminal without a recovery flow

This finding was discovered while verifying DEF-032 action targeting. It is separate from the duplicate-row defect.

### Reproduction and observed state

1. Create a provider connection that has never validated and is `CREATED_UNVERIFIED`.
2. From Providers, choose its row action **Disable connection**. The UI executes immediately, without a confirmation step or warning that the action is terminal, and the row becomes **Disabled**.
3. Try the row's **Probe connection** action. It returns HTTP 422 `invalid_state_transition`, message `cannot test connection in state ARCHIVED`; the fixture call count does not increase.
4. Attempt to update the same record to `ACTIVE` through the documented Admin API `PATCH /admin/v1/connections/{connectionId}`. It returns HTTP 422 `invalid_state_transition` (`cannot modify connection in state ARCHIVED`). The readback remains API state `DISABLED`, version 6. The UI exposes no Enable action.
5. A model deployment already bound to this connection remains persisted as `PROVISIONED`, with `connection_state=DISABLED`. No route or inference was sent in this phase.

Sanitized evidence: `CP07-provider-disable-reenable.json`, `CP07-probe-archived-fails.json`, and `CP07-provider-final-state.json`. Visually inspected screenshots: `CP07-provider-disable-confirm.png`, `CP07-probe-archived-fails.png`, and `CP07-final-models-state.png`.

### Expected, actual, and impact

- **Expected:** either provide a supported, clearly signposted recovery/reactivation flow for disabled connections, or tell the user before confirmation that disabling permanently archives an unverified connection and explain the replacement steps. Bound deployments should communicate that their provider is disabled and how to restore service.
- **Actual:** a one-step menu action permanently archives an unverified connection. The UI continues to call it Disabled; Probe and reactivation then fail, and the deployment remains attached and visually Provisioned even though its connection state is disabled.
- **Affected workflow:** Providers → disable/probe/edit and Models → provider binding/deployment recovery.
- **Severity:** Moderate. This can strand a configured connection and deployment and requires a replacement workflow; outage impact is bounded by the need for a user to invoke Disable. The exact supported remediation is unverified.

### Facts and hypotheses

**Observed facts:** UI action succeeds immediately; API and UI read back Disabled; API reactivation and UI Probe return 422 for internal state ARCHIVED; fixture receives no further request; a Provisioned deployment still references the disabled connection.
**Hypothesis, not established intent:** the product may intentionally treat Disable on an unverified draft as permanent archival. If that is the intended contract, the missing confirmation, warning, and replacement/recovery guidance remain the user-facing issue. Creating a replacement connection and deployment was not tested.

### Acceptance criteria

1. Document and expose the disable transition accurately, including whether it is reversible and the effect on dependent deployments.
2. If reversible, provide an authorized UI and API path that returns a disabled connection to a probeable state without bypassing validation; verify a successful probe before healthy deployment activation.
3. If terminal by design, require explicit confirmation with impact and replacement guidance, and prevent accidental archival from being presented as an ordinary reversible Disable action.
4. Ensure deployment status and recovery guidance remain clear when its backing connection is disabled or archived.
5. Regression-test both unverified and active connections, with persistent state and audit-event checks.

## Consolidated status after CP07

- Historical defects: **6 VERIFIED FIXED for scoped scenarios** (DEF-002, DEF-009, DEF-021, DEF-023, DEF-034, DEF-038); **2 PARTIAL** (DEF-007, DEF-032); **31 UNVERIFIED**. No prior result was inherited from QA-001/QA-002.
- New findings: **DEF-040 Moderate**, described above. No other new defect is confirmed in this checkpoint.
- Coverage expanded: FT-03, SCR-04, SCR-05, WF-03 (all still partial); the Ollama 401→Unverified→Provisioned denial path and duplicate display selection are explicitly covered.
- Still open: positive provider validation/activation and inference, post-disable recovery, route/failover behavior, connection picker identity, Google runtime/adapter separation, and all remaining feature, role, platform, integration and operational coverage.
- Firefox remains blocked by the container's `/proc/self/uid_map: EROFS` sandbox restriction. RC5 release artifact verification remains unverified. External provider spend remains USD 0.
- QA-003 remains in progress. `GATE-REL-07` remains **BLOCKED**.
