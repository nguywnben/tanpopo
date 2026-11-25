# CP42 — Extension quarantine, traffic containment, and recovery

**Status:** FAIL for lifecycle recovery and documented error semantics; PASS for immediate traffic containment in the tested paths. Overall coverage remains PARTIAL.
**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`.
**Product source:** unchanged.

## Scope and setup

Used the actual Chromium Console and authenticated instance Operator / Org Owner in the isolated test-root runtime at `127.0.0.1:18124`. The test quarantined its existing synthetic Ollama extension with reason `QA-004 adapter quarantine acceptance probe`; no production extension, credential, or signing key was involved. The test-signed extension is not an authentic Tanpopo release-signed package. Runtime `18128` remains a separate healthy test instance for follow-on testing; `18124` is intentionally preserved in QUARANTINED state as reproduction evidence.

## Results

1. **Pre-quarantine dispatch — PASS, narrow slice.** Cookie-free authenticated native `/native/ollama/api/generate` reached the isolated fixture once and returned success. No real provider expenditure occurred.
2. **Quarantine interaction — PASS, narrow slice.** The real Extensions page showed a confirmation modal identifying the affected project and connection, required a reason and exact package ID, and confirmed that the action would immediately disable the extension and sever active streams. After confirmation, the installed row showed `Quarantined` and a success banner stated active streams were severed.
3. **Containment — PASS.** After quarantine, both cookie-free `/native/ollama/api/generate` and `/v1/chat/completions` returned HTTP 503 before the synthetic fixture. The fixture call count during the probes was zero, deployments were not selected, and persisted traces showed zero usage and cost. The product failed closed.
4. **Error contract — FAIL (DEF-059).** Both endpoints returned `no_compliant_deployment_available` with `residency_unavailable_error` and `retryable:true`; the API compatibility table defines HTTP 503 `extension_quarantined`, type `extension_error`, and `retryable:false` when the backing extension is quarantined. Traces persist the generic code as well. See the DEF-059 report and sanitized response/trace evidence.
5. **Recovery — FAIL (DEF-058).** The business entity state machine defines `QUARANTINED -> INSTALLED_DISABLED` as audited unquarantine by OPERATOR / ORG_OWNER. The quarantined row’s real action menu contained Review permissions, Upgrade package, and Uninstall package, with no unquarantine action. An authenticated POST to the candidate `/admin/v1/extensions/{id}/unquarantine` returned 404 `resource_not_found`; trying the available approval operation returned 409 `invalid_state_transition`. Uninstall returned 409 `extension_referenced_by_active_connections`, reporting two dependent connection records, and left the installation quarantined. The state remained `QUARANTINED`, version 3.
6. **Audit — PASS, scoped.** Quarantine emitted `extension.quarantined` into the instance `SYSTEM` audit partition with the operator principal, installation target, previous state and supplied reason. The SECURITY/OWNER audit UI is scoped to the organization chain by default; querying `chain_partition=SYSTEM` as instance Operator found the event. System-chain verification returned `intact:true` for four events. The initial organization-partition read was not used to infer a missing event.

## Evidence

- `evidence/CP42-quarantine-confirm.png` and `CP42-quarantine-confirm-filled.png` — actual quarantine confirmation, visually inspected.
- `evidence/CP42-quarantined-state.png` and `CP42-quarantined-actions.png` — persisted state and actual action menu; visually inspected. No recovery action appears.
- `evidence/CP42-quarantine-rejection.json`, `CP42-quarantine-traffic-details.json`, and `CP42-recovery-probes.json` — sanitized native/provider responses, fixture dispatch count, traces, state and rejected recovery attempts.
- `evidence/CP42-system-audit.json` — SYSTEM-partition `extension.quarantined` event and successful chain verification.
- `evidence/CP42-quarantine-failure-traces.png` and `CP42-quarantine-failure-trace-detail.png` — actual Traces screen showing persisted 503s and generic error code; visually inspected.
- Frozen-source inspection: `web/console/src/pages/extensions/ExtensionsPage.tsx` conditionally omits Quarantine for quarantined rows but provides no Unquarantine action; `internal/admin/http/server.go` registers no unquarantine route; lifecycle transition table permits the recovery edge. Source inspection is corroboration, not substituted for runtime evidence.

## Coverage and limits

This verifies one manual quarantine transition, two fail-closed dispatch paths, one missing recovery path, and one SYSTEM audit entry. Auto-quarantine from signed revocation feeds, Wasm fault thresholds, stream abortion under a deliberately active SSE request, multi-node propagation, alternative lifecycle starting states, rollback, and a completed post-recovery re-enable/reprobe sequence remain UNVERIFIED. The quarantine remains in the isolated 18124 test DB; it does not affect the separate 18128 runtime. No database mutation or direct state bypass was used. QA-004 continues; **GATE-REL-07 remains BLOCKED**.
