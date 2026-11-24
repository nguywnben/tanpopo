# CP79 — Fresh project provisioning audit event and chain verification

- **Baseline:** frozen RC4 `40bba5b55c2da6fc00b107b315c9b3167fb20169`; no Tanpopo implementation changes.
- **Runtime/browser:** isolated runtime `127.0.0.1:18084`, Org Owner principal, real Chromium interaction and screenshot inspection.
- **Result:** PASS for the tested successful project provisioning audit path and current organization chain. DEF-007 remains only scoped-pass; failure, mutation, and cross-tenant coverage remains open.

## Procedure and evidence

1. Opened Security → Audit Log in Chromium and filtered by `project.created`. The UI returned four matching events. The newest was sequence 300 for the CP78 project `prj_01m868q2jte00abmhf75asqts3`, attributed to the authenticated owner principal.
2. The independent authenticated audit API returned the same event with the organization chain partition and matching actor/target. Sequence 301 immediately followed with `budget.created` for the fresh project budget. Later budget updates at sequences 306 and 311 correspond to the CP78 cap collision and restoration.
3. Used the visible **Verify hash chain** action. The UI reported the RFC 8785 JCS chain intact across 315 events; the verification endpoint returned HTTP 200, `intact: true`, `checked_events_count: 315`, and no broken sequence.
4. Visually inspected the filtered event list and successful verification banner. No resource was mutated in CP79; the events were produced by CP78's already documented project/budget workflow.

Evidence:

- `../evidence/CP79-audit-project-created-filter.png`
- `../evidence/CP79-audit-chain-verified.png`
- `../evidence/CP79-audit-project-event-and-chain.json`
- Additional session context: `../evidence/CP79-audit-all.png` and `../evidence/CP79-security-initial.png`

## DEF-007 disposition

This independently corroborates a successful real-Console project create path with expected actor, target, partition, sequence, and a valid current chain. It does not establish failure atomicity or prove every provisioning mutation emits the required event. Standalone organization failure injection, update/delete paths, storage failures, and cross-tenant attribution remain **UNVERIFIED**. No defect status is downgraded or closed by this checkpoint.

## Consolidated phase status

- **Historical defects:** CP79 adds a successful-path DEF-007 result only. It does not change the status of DEF-001–006 or DEF-008–015; their acceptance criteria remain independently incomplete overall. DEF-005 remains PARTIAL.
- **New findings:** none. Existing findings DEF-016–033 and QA-OBS-001–014 remain preserved.
- **Coverage completed:** filtered audit event inspection, actor/target/partition cross-check, event ordering against persistent API evidence, and UI/API hash-chain verification across 315 events.
- **Coverage remaining / blocked:** DEF-007 failure-injection and mutation-path coverage remain open. A2A runtime 18445 remains configuration-disabled; webhook delivery still lacks usable Operator access and controlled receiver/signing setup; RC4 release artifact remains UNVERIFIED. Broad Console, integration, browser, role, and platform coverage remains incomplete.
- **Next work:** continue independent retesting and unexplored business workflows. Keep successful-path audit evidence separate from failure/atomicity acceptance.

`GATE-REL-07` remains **BLOCKED**. QA-002 remains in progress.
