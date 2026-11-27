# CP73 — Independent DEF-002 provider failure and promotion-guard retest

- **Baseline:** frozen RC4 source `40bba5b55c2da6fc00b107b315c9b3167fb20169`; no implementation changes.
- **Result:** PASS for this independent local-provider failure scenario; DEF-002 remains VERIFIED FIXED (scoped).
- **Runtime:** isolated A2A-enabled RC4 runtime `127.0.0.1:18084`; real Chromium; synthetic loopback Ollama-compatible fixture on `127.0.0.1:19187`.

## Test flow

1. In Providers, created `QA002 CP73 Failing Ollama` using the enabled Ollama adapter, `SELF_HOSTED_INFRA` billing, `ON_PREM`, and explicit allowlist `127.0.0.1/32`. The connection persisted as `CREATED_UNVERIFIED`.
2. The fixture responds to every model-list request with HTTP 401. The real Console Probe connection action returned HTTP 200 with `valid=false`, `failure_code=auth_failed`, and sanitized message `upstream returned HTTP 401`. The fixture observed exactly one `GET /v1/models`. The UI displayed the failure and kept the connection Unverified.
3. Created deployment `qa002-cp73-failed-401` against that failed connection with zero-rate confirmation. The real deployment API returned HTTP 201 with `connection_state=CREATED_UNVERIFIED` and `state=PROVISIONED`; the Models table also showed Provisioned.
4. Opened the real deployment row action menu. It contained Disable deployment and Delete deployment only; no Activate action appeared.
5. Using the same owner session, attempted the documented administrative promotion via CSRF-protected `PATCH /admin/v1/deployments/{id}` with `If-Match: "v1"` and `state=ACTIVE_HEALTHY`. API returned HTTP 422 `connection_not_active`, naming the backing `CREATED_UNVERIFIED` connection. A post-read showed the deployment still `PROVISIONED`, connection still `CREATED_UNVERIFIED`, version 1.

## Evidence

- `../evidence/CP73-providers-initial.png` — visually inspected provider inventory before the fresh test.
- `../evidence/CP73-provider-form-ollama-allowlist.png` — visually inspected exact explicit loopback CIDR.
- `../evidence/CP73-provider-create.json` — sanitized Admin create/list responses.
- `../evidence/CP73-provider-created.png` — visually inspected new Unverified connection.
- `../evidence/CP73-provider-probe.json` — real Probe endpoint response.
- `../evidence/CP73-provider-fixture-calls.json` — fixture method/path evidence; one `GET /v1/models`.
- `../evidence/CP73-provider-probe-failed.png` — visually inspected auth error and Unverified state.
- `../evidence/CP73-deployment-form.png`, `../evidence/CP73-deployment-submit.png`, `../evidence/CP73-deployment-provisioned.png` — real deployment workflow and Provisioned result.
- `../evidence/CP73-deployment-actions.png` — visually inspected row menu; no activation action.
- `../evidence/CP73-def002-deployment-activation.json` — promotion rejection and post-read state.

## Test harness correction and boundaries

The first create attempt left the private-endpoint toggle unchecked. The API correctly rejected the private URL with HTTP 400 `ssrf_blocked_private_ip`; no connection was created, and no probe was run. The test was corrected by explicitly checking the control and entering only `127.0.0.1/32`. This is harness setup evidence, not a product defect, and no SSRF or authorization boundary was relaxed.

- `../evidence/CP73-provider-form-ollama.png` — inspected initial form with the private-endpoint toggle unchecked.
- `../evidence/CP73-provider-create-ssrf-rejection.json` — sanitized initial 400 and correction note.

This phase tests local Ollama adapter authentication failure only. It does not classify historical Google `probe_runtime_unavailable` or `upstream_unreachable` events, infer Cloud egress availability, or verify credential repair/recovery. Those remain separate environmental/configuration investigations. Deployment route dispatch and other provider failure statuses remain UNVERIFIED in CP73.

`GATE-REL-07` remains **BLOCKED**; QA-002 is in progress.
