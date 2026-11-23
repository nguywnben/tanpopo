# QA-002 — RC4 independent Alpha/Beta regression

**Workspace:** branch `qa/qa-002-regression`, based on `40bba5b55c2da6fc00b107b315c9b3167fb20169` (2026-11-24).
**Scope:** independent regression; do not modify Tanpopo implementation.
**Release gate:** `GATE-REL-07` remains **BLOCKED**.

This workspace is separate from QA-001. Historical evidence is reference material only; no QA-001 PASS is inherited. QA-001 remains the immutable record at branch `qa/qa-001-handoff-cp65`, commit `d994f82128f0e78a4e7bb7dd4ecd705cee60d85f` in the sibling checkout `/workspace/tanpopo`.

## Current records

- [Baseline and environment CP00](checkpoints/CP00-baseline.md)
- [Coverage matrix](COVERAGE-MATRIX.md)
- [Historical defect retest register](DEFECT-RETEST-REGISTER.md)
- `evidence/` — sanitized command outcomes, API/state snapshots, and visually reviewed screenshots.
- `reports/` — phase reports, new defect findings (DEF-016 onward), and explicitly unclassified QA observations.

Each checkpoint records the tested revision/runtime, actions, evidence, and PASS / FAIL / BLOCKED / UNVERIFIED result. A PASS is limited to the exact scenario evidenced. No test or artifact may be called PASS based only on source inspection, HTTP success, or an automated test.

## Evidence hygiene

Use disposable local databases and synthetic provider/integration fixtures. The configured provider keys may only be passed to the intended provider request from the runtime environment; do not print, persist, or include them in screenshots or reports. Sanitize response headers and logs. Keep databases, credentials, key material, and raw runtime logs outside version control. Store reproducible summaries and screenshots here.

## Checkpoint status

| Checkpoint | Scope | Result |
| --- | --- | --- |
| CP00 | Exact baseline, artifact inventory, environment/tooling diagnosis | PARTIAL PASS; no published RC4 build/release artifact found; see checkpoint |
| CP01 | Fresh-install Setup and first authenticated session | PASS for the executed setup/login path; broader persistence, roles, and features remain open |
| CP02 | Release linker provenance and full Go race suite | Linker settings verified; Hydrogen artifact UNVERIFIED; race suite PASS (86 packages) |
| CP03 | Complete fresh setup on A2A-enabled runtime; live axe and visual inspection | PASS for scoped Chromium wizard path; DEF-001 verified fixed in tested path; broad coverage continues |
| CP04 | Built CLI config show/help/reconcile using sanitized local HTTP fixture | PASS for command routing and fail-closed validation; real Tanpopo Admin API interaction remains UNVERIFIED |
| CP05 | First FinOps project budget from empty state; new-session persistence check | PASS for tested workflow; other scopes and budget policy remain open |
| CP06 | Bootstrap and standalone provisioning audit events, duplicate rejection, chain verification | PASS for tested creation paths; mutation/error paths remain open |
| CP07 | Real WebKit 26.5 webhook drawer at 375×844 using local HTTPS | PASS for scoped layout; registration requires a synthetic HMAC secret and remains untested |
| CP08 | Authorized A2A discovery, input-required task API/Console lifecycles, provider-only denial, key revocation | PASS for scoped lifecycle and key scenarios; MCP path, concurrent drain, budgets, and other roles remain open |
| CP09 | MCP private endpoint CIDR form, wildcard rejection and persistence | PASS for registration form and saved CIDR; actual MCP sync/network gating remain UNVERIFIED |
| CP10 | Ollama extension permission review, provider defaults, persistence and synthetic live probe | PASS for scoped Ollama lifecycle; other adapters and model/deployment inference remain open |
| CP11 | Exact no-budget provider settlement, FinOps headline, selected model grouping, CSV/JSON exports | PASS for scoped reconciliation; quota policies and other group dimensions remain open |
| CP12 | Local 401 provider probe, deployment promotion guard, route health and fail-closed inference | PASS for scoped rejection/readiness path; Google and other failure modes remain open |
| CP13 | Console encrypted backup, CLI verification, secret-decryption restore, restored startup/login | PASS for SQLite path; cluster and damaged-archive cases remain open |
| CP14 | Project-scoped Team Admin member creation, list refresh and role-boundary retest | CREATE/BOUNDARY PASS; UI list refresh FAIL (DEF-016), separately recorded |
| CP15 | Authorized A2A task paused at synthetic endpoint during Console graceful key revocation | Revocation PASS; dispatched task returned 400 `context canceled` and persisted Failed. A2A task drain contract UNVERIFIED; not classified as defect. |
| CP16 | Project/environment create, project teardown, audit attribution and chain verification | PASS for successful synthetic lifecycle and audit chain; standalone org/fault paths remain open. |
| CP17 | Fresh isolated bootstrap, separate-session persistence, Google provider boundary and empty-model Playground recovery | Fresh setup/login PASS for scoped path; Google model validation BLOCKED in-app by proxy-only egress; no-provider Playground correctly surfaces HTTP 503. Wider setup/platform and provider coverage remain open. |
| CP18 | Standalone organization provisioning, duplicate rejection and tenant audit isolation | PASS for new-organization audit attribution and one-event chain verification; duplicate slug rejected with no second organization. Fault injection, delete and cross-tenant access remain open. |
| CP19 | DEF-001 exact original Setup test retest on the new source baseline | Both original targeted Vitest cases PASS on 40bba5b; QA-001 failures on the pre-engineering baseline remain historical evidence, while CP03 provides separate live-browser evidence. Broader accessibility remains open. |
| CP20 | Firefox browser provisioning and launch diagnosis | UNVERIFIED: Firefox 153 installed in temporary `/tmp` cache, but Playwright launch timed out before app navigation or screenshots due read-only sandbox UID mapping and software compositor failures. Chromium/WebKit remain usable for their already-scoped cases. |
| CP21 | Fresh SQLite runtime clean restart, readiness and persisted Console state | PASS for startup probes, separate-session login, workspace persistence and retained failure trace; clustering and crash/fault recovery remain open. |
| CP22 | Fresh-runtime provider install → connection → deployment → route → streaming Playground inference and trace correlation | PASS for scoped synthetic provider setup/inference and exact-usage persistence; FAIL DEF-017 because Playground's trace card remains pending on repeated 404s despite a persisted trace. Google and real-provider behavior remain BLOCKED/UNVERIFIED; further product and platform coverage continues. |
| CP23 | Rerun interrupted full Go race suite using verified Go 1.26.11 | PASS: 86 test-bearing packages, 22 packages without tests, zero failures/race reports; source diff from baseline empty. Automated result does not replace product coverage. |
| CP24 | Streamable HTTP MCP governance and data-plane lifecycle | PASS for the synthetic read-only flow: registration/discovery, exact-tool key grant, JSON-RPC call, ungranted and cross-plane denial, drift lock/reapproval, and revoked-key 401. Broader MCP risk/role/transport coverage remains open; see checkpoint. |
