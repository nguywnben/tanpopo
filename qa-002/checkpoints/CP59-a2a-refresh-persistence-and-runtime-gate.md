# CP59 — A2A runtime gate, refresh persistence, and follow-up lifecycle

**Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`
**Result:** PASS for the A2A-enabled runtime’s input-required → navigation/refresh → same-task completion lifecycle. A second runtime was BLOCKED by its disabled A2A data-plane configuration. No new defect assigned.
**Release gate:** `GATE-REL-07 BLOCKED`.

## Runtime comparison and classification

The isolated Console at `https://127.0.0.1:18445` allowed an owner to register an agent and issue a virtual key. The first key form retained the default `PROVIDER` plane while A2A was added; that mixed-scope key was never sent and was revoked. A replacement key was explicitly created with `allowed_planes:["A2A"]`; it too was not used against this runtime. The data-plane discovery, well-known card, and task endpoints each returned 404 `resource_not_found`, “A2A plane is disabled,” with zero synthetic endpoint calls.

Source inspection in `internal/ingress/a2a/task_coordinator.go` confirms that the data-plane handlers return this exact 404 when `TaskCoordinator.Enabled()` is false; `Enabled()` reads `cfg.Enabled`, and the source error comment states that A2A is disabled by default. This supports classifying the 18445 result as a runtime/configuration gate, not an A2A authorization or lifecycle defect. The runtime’s effective config value was not independently read from its process configuration, so its exact config provenance remains UNVERIFIED. The Console still permitting agent registration and A2A-key provisioning while this data plane is disabled is retained as **QA-OBS-010** (control-plane/data-plane availability and UX contract question), not a confirmed product defect absent a documented requirement.

The separate isolated SQLite Console/runtime at `http://127.0.0.1:18084` was operational for A2A. It was freshly provisioned with a synthetic loopback agent on port 19184 and `127.0.0.1/32` explicitly allowlisted. An owner-issued key was verified as A2A-only, restricted to `/a2a/*`. Discovery and the well-known agent card both returned 200. With that exact key, task submission returned 201 and `input-required`; GET preserved the task ID and state. The synthetic endpoint records metadata only (task ID, part count, approval-marker presence, auth-header presence, trace-root presence and hop count); it does not retain input text or credentials.

## Real-user lifecycle and persistence

The A2A Console displayed the new task and its input-required state, including Resume and Cancel actions. I navigated to Overview, returned to MCP Hub, reloaded the browser, and confirmed the same task ID remained visible and resumable. I then submitted the required approval text through the Console. Resume returned 200; the same task ID reached `completed`, version 5. A fresh data-plane GET returned the same ID and terminal state with 3,000 micros settled. The fixture recorded exactly two calls for this ID, initial input then approval input; neither contained a forwarded Authorization header, both had a trace root, and both remained at hop 1. The virtual key was revoked through Access & Keys afterward; replay returned 401 `invalid_virtual_key`.

A separate post-revocation probe against the 18445 key returned the runtime’s A2A-disabled 404 before task access. No task or fixture egress occurred there. The proper A2A-only key issued in that runtime was also revoked without use. The working-runtime screenshots show the registered synthetic agent, the A2A-only key, the input-required task after refresh, and same-ID completion. I visually inspected these screenshots during the phase.

## Evidence

- [`../evidence/CP59-a2a-refresh-lifecycle.json`](../evidence/CP59-a2a-refresh-lifecycle.json) — sanitized task, resume, persistence, and metadata-only fixture outcomes.
- [`../evidence/CP59-18084-agent-registered.png`](../evidence/CP59-18084-agent-registered.png) — actual owner Console agent list and synthetic endpoint.
- [`../evidence/CP59-18084-key-created.png`](../evidence/CP59-18084-key-created.png) and [`../evidence/CP59-a2a-key-revoked.png`](../evidence/CP59-a2a-key-revoked.png) — A2A-only grant and final revoked state.
- [`../evidence/CP59-a2a-input-required-after-refresh.png`](../evidence/CP59-a2a-input-required-after-refresh.png) and [`../evidence/CP59-a2a-completed-after-refresh.png`](../evidence/CP59-a2a-completed-after-refresh.png) — same task visible after reload and after UI resume.
- [`../evidence/CP59-18086-a2a-key-revoked.png`](../evidence/CP59-18086-a2a-key-revoked.png) — unused A2A-only key revoked in the disabled runtime.
- Source gate: `internal/ingress/a2a/task_coordinator.go` (`Enabled`, disabled handler response, and `ErrA2ADisabled`).

The two extra agent registrations created during recovery from a UI row-selector timeout are isolated synthetic records in the disposable 18084 test database; they point only to the same local metadata-only fixture and were not used for dispatch. This is test-data residue, not a product defect or source change. Key material was removed from `/tmp` after revocation. No Tanpopo implementation was modified.

## Consolidated QA status

- **Historical DEF-001–015:** CP59 did not retest them; retain the current QA-002 disposition: DEF-001–004 and DEF-006–015 are verified fixed only for their scoped independent retests; DEF-005 remains partial with its unresolved SSRF/endpoint-contract coverage. Do not treat this as full regression clearance.
- **Open defects:** DEF-016–028 remain confirmed open. CP59 found no additional confirmed defect. DEF-023 remains a cross-screen workspace-scope issue requiring broader Console testing. DEF-026 and QA-OBS-004 last recurred in CP58.
- **Coverage completed:** A2A discovery, exact A2A-only authorization, synthetic input-required task, server-side persistence across navigation and hard refresh, UI resume, same-ID completion, exact synthetic settlement, trace/hop/header propagation, key revocation, and comparison against a runtime whose A2A gate is disabled.
- **Blocked/unverified:** A2A behavior on the 18445 runtime while A2A remains disabled; effective runtime config provenance; concurrent task drain; delegation depth and cross-role A2A authorization; budget/quota interactions; non-Chromium task lifecycle.
- **Other blockers:** CP53 public webhook delivery remains BLOCKED pending usable Operator credentials; RC4 Hydrogen DevBox artifact remains UNVERIFIED; historical Google provider failures remain runtime/integration-boundary cases as documented in CP17 and other checkpoints.
- **Next:** continue DEF-005 endpoint-resolution and transport cases, broader DEF-023 workspace contexts on every remaining screen, independent DEF-001–015 retests where not fresh, and unexplored product workflows/roles/platforms. Do not inherit QA-001 PASS.

QA-002 remains in progress. `GATE-REL-07` remains **BLOCKED**.
