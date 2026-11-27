# CP68 — A2A private registration and input-required task lifecycle

**Result:** PASS for the scoped end-to-end flow on the enabled isolated runtime `18084`; other A2A states/platforms remain unverified.  
**Baseline:** frozen RC4 source `40bba5b55c2da6fc00b107b315c9b3167fb20169`.  
**Release gate:** `GATE-REL-07` remains **BLOCKED**.

## Method and result

On the A2A-enabled isolated runtime at `http://127.0.0.1:18084`, the synthetic Org Owner registered a local mock agent through the real Console with private access enabled and explicit `127.0.0.1/32`. The API returned 201, persisted the agent as Active with the exact allowlist, and the agent appeared in the Console.

The Console’s new-key form initially selected PROVIDER by default. A first exploratory key created after adding A2A therefore had both planes; it was revoked immediately in the real UI before any request was made with it. A second key was issued with A2A as the only plane and route `/a2a/*`. The tested request to `/v1/models` returned 403 `scope_or_plane_denied`, confirming the key did not authorize the Provider plane.

Using the A2A-only key, `GET /a2a/agents` returned 200 and included the registered synthetic agent; the gateway `.well-known/agent.json` response listed the same registry. A task was submitted and persisted as `input-required` with a 10,000-micro sub-budget hold. The Console displayed the exact task ID and state before and after a hard refresh. I visually inspected the refreshed screen. The same task was resumed in the Console with the requested approval text, returned 200 as `completed`, and persisted a 3,000-micro settled cost. I visually inspected the completed state and success notice.

The isolated fixture logged two POSTs with the same task ID: initial request without the approval marker and resumed request with it. Both included the trace-root and hop-count metadata; the Virtual Key Authorization header was not forwarded to the fixture. The A2A-only key was revoked in the Console; replaying A2A discovery then returned 401 `invalid_virtual_key`.

The 18445 runtime remains A2A-disabled by configuration; this CP verifies functionality only on enabled runtime 18084 and does not relabel that runtime/configuration blocker as a product defect.

## Evidence

- `../evidence/CP68-a2a-register-form.png`, `CP68-a2a-agent-registered.png`, and `CP68-a2a-agent-register.json` — real registration, explicit CIDR payload, 201 response, and persisted UI row.
- `../evidence/CP68-a2a-key-form.png`, `CP68-a2a-key-created.png` — initial exploratory broader-plane issuance; the key was revoked immediately without use.
- `../evidence/CP68-a2a-only-key-form.png`, `CP68-a2a-only-key-created.png` — least-privilege A2A-only key creation and UI record.
- `../evidence/CP68-a2a-discovery.json` — registry discovery and gateway agent-card response containing the test agent.
- `../evidence/CP68-a2a-input-required.png` and `CP68-a2a-input-required-after-refresh.png` — same persisted task ID/state across navigation and hard refresh; the latter was visually inspected.
- `../evidence/CP68-a2a-resume-form.png`, `CP68-a2a-completed.png` — real Console resume and completed task; completed screenshot visually inspected.
- `../evidence/CP68-a2a-lifecycle.json` — task IDs/states, hold and settlement, Provider-plane denial, key revoke and replay denial.
- `../evidence/CP68-a2a-fixture-calls.json` — sanitized synthetic endpoint calls, task identity and metadata; no secrets.
- `../evidence/CP68-a2a-key-revoke-confirm.png` — key revocation confirmation flow.

## Coverage update

The scoped private A2A registration, discovery, A2A-only authorization, input-required persistence, refresh recovery, Console resume, settlement, revocation, and replay denial passed on this enabled runtime. Cancellation after refresh, in-flight revocation, multi-hop and cross-region tasks, malformed CIDR classes, Firefox, and the disabled-runtime UX remain unverified or blocked. DEF-005 remains PARTIAL overall because other SSRF resolver/transport scenarios remain open. No source code or runtime configuration was changed. The mock listener was stopped after testing.
