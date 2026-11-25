# CP15 — Security Reviewer key-level kill-switch and recovery lifecycle

**Outcome:** PASS for the tested one-Virtual-Key kill-switch lifecycle on the isolated RC6 clone. Other target levels, active-stream termination, latency and distributed propagation remain unverified.

## Controlled test scope

An Org Owner issued two fresh PROVIDER Virtual Keys through Access & Keys, each scoped to the test environment and synthetic route. The target key returned HTTP 200 against the local synthetic Ollama before triggering; the second key returned HTTP 200 while the first key's switch was active. A third initial test key was created during fixture setup, but an automation parser captured only its visible prefix; no inference used it. All three CP15 keys were later revoked through the Console, and the Owner readback confirms `REVOKED` / version 2. The plaintext secrets were retained only in mode-0600 `/tmp` files during the test and are absent from the repository.

## Trigger, isolation, and recovery

1. As a Security Reviewer, opened Security → Trigger kill switch, selected the `Virtual key` target level, entered the exact ID of the dedicated target key, previewed impact (`1 org · 1 project · 1 virtual key · 1 connection`), and confirmed. The API returned HTTP 200, config epoch 29, one affected entity, and zero severed streams.
2. A cookie-free native request using that key returned HTTP 401 `invalid_virtual_key` with “Virtual Key is suspended by emergency kill switch.” A different, newly issued provider key still completed local synthetic inference with HTTP 200. This confirms the tested key-level boundary; the connection and other key remained usable.
3. As Org Owner, opened the active switch in the real Console and cleared it with a reason. The API returned HTTP 200, `active: false`, version 2; the active-switch list returned zero. The target key then returned HTTP 200 to the local synthetic route.
4. Audit filters returned the `kill_switch.triggered` and `kill_switch.cleared` events. The UI showed the active switch and later “Kill switch cleared and logged” with no active switches. All three temporary test keys were then revoked in the real Console; post-revoke API readback showed the revoked state.

Safe response and state evidence are in `evidence/CP15-killswitch-*`; the actual UI screenshots show the scoped target, preview, confirmation, active state, clear outcome, and revoked test keys. Screenshots were visually inspected.

## Harness observation, not a product defect

An initial in-browser `fetch` included the authenticated Console cookie and received HTTP 401 `invalid_credential_plane` because `/v1/*` rejects admin session cookies by design. The result was not treated as kill-switch behavior. The valid cookie-free key request above was then run and its suspension state confirmed. Similarly, an early token parser captured only the visible key prefix; that invalid test credential was not used to infer a product defect and the key was revoked. Exact key material never entered the evidence branch.

## Remaining operational coverage

Project, provider-connection, and extension switch targets; concurrent SSE abort; MCP stream abort; role-specific clear permissions; multi-node propagation; epoch consistency after restart/crash; rollback under partial propagation; propagation latency; and switch/audit behavior on PostgreSQL/Valkey deployments remain `UNVERIFIED`.
