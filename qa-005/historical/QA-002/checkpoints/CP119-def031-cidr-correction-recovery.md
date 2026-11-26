# CP119 — A2A invalid-CIDR correction and persistence recovery

**Baseline:** frozen RC4 `40bba5b55c2da6fc00b107b315c9b3167fb20169`  
**Runtime:** isolated Tanpopo `18084`  
**Browser/role:** real Chromium mobile emulation, 375 × 844; QA Org Owner + Operator  
**Product implementation modified:** No  
**Release gate:** `GATE-REL-07 BLOCKED`

## Journey

Repeated the A2A registration flow in a new mobile Chromium context. Submitted `qa002-cp119-cidr-recovery` with private-endpoint authorization enabled and invalid `127.0.0.1/33`; API returned HTTP 400 `invalid_request` and the entered CIDR remained in the open drawer. Corrected the same field to `127.0.0.1/32` and resubmitted the same form. The API returned HTTP 201, closed the drawer, and displayed “A2A agent registered and published to discovery.” The UI showed the new Active row and the count increased from six to seven. A separate fresh browser context then loaded the same row; authenticated list API returned 200 and the row carried `agent_id=agt_01m86x65pne00byjz6vx33qm3a`, project `QA002 CP78 Fresh Budget`, and state `ACTIVE`.

No discovery or task action was initiated. The test did not grant a key or invoke the A2A agent. The synthetic private endpoint listener was not part of this recovery workflow; no task/tool call was made. There is no established supported deletion operation for the new synthetic row, so it remains in isolated QA data.

Visually inspected screenshots: `../evidence/CP119-a2a-cidr-corrected-before-submit.png` (corrected value in form), `CP119-a2a-cidr-recovery-result.png` (success banner and persisted row), and `CP119-a2a-cidr-recovery-new-session.png` (row persists in a new browser context after the transient banner disappears). Sanitized summary is `../evidence/CP119-a2a-cidr-recovery.json`.

## DEF-031 and recovery disposition

**PASS, scoped for recovery after correction:** the user can retain the form values after the invalid `/33` response, correct the CIDR, submit successfully, and see the new row persisted in an independent browser context. The invalid response itself remains a **FAIL for DEF-031 Low**: CP118's actual 375×844 screenshot showed its error text behind the drawer. CP119 verifies recovery despite that feedback issue; it does not make the initial failure message visible or close DEF-031. The success banner was visible in the submit context and was transient, while persisted row state remained after a fresh load.

The new row used an explicitly enabled private-endpoint option with exact `127.0.0.1/32`; it does not weaken the SSRF boundary. This tests registration and persistence only, not endpoint discovery, authorization via key, task lifecycle, or revoke/cleanup.

## Consolidated status summary

- **Historical DEF-001–015:** preserve limited QA-002 outcomes; no QA-001 PASS is inherited. DEF-005 remains PARTIAL; DEF-007 fault atomicity, storage faults, and independent erasure proof remain open.
- **Existing DEF-016–039:** preserved. DEF-031 Low remains confirmed from CP66/67/118; CP119 adds scoped recovery PASS but does not close it. No new defect ID.
- **Coverage completed:** invalid A2A CIDR rejection followed by same-drawer correction, HTTP 201, success notice, and persistence in a new Chromium mobile browser context. No task/discovery action.
- **Coverage blocked/remaining:** visual correction flow on WebKit/Firefox; keyboard/accessibility announcement; MCP recovery parity; full A2A endpoint/key/task/revocation lifecycle for this new row remains untested. Broader product coverage remains incomplete; current Firefox/WebKit limits remain recorded in CP111.
- **Recommended handoffs:** retain the DEF-031 field-level/visible error acceptance criteria; this demonstrates that the backend and correction path recover. Continue all open defect retests and do not treat this scoped pass as release readiness.

`GATE-REL-07` remains **BLOCKED**. QA-002 remains in progress.
