# QA-001 defect retest register — QA-002

Baseline: `40bba5b55c2da6fc00b107b315c9b3167fb20169`. The acceptance criteria are from the QA-001 register and standalone handoffs at `qa/qa-001-handoff-cp65@d994f82128f0e78a4e7bb7dd4ecd705cee60d85f`. Historical PASS results are not inherited. **All entries start UNVERIFIED for RC4 until new evidence is recorded below.**

| ID | Historical finding | Original priority | QA-002 status | Current checkpoint/evidence |
| --- | --- | --- | --- | --- |
| DEF-001 | Setup progress button accessible name omitted visible text | Major | UNVERIFIED | Pending fresh reproduction in test and live browser states |
| DEF-002 | Failed/unverified provider could be promoted to Healthy | Major | UNVERIFIED | Pending controlled failed-probe activation and dependent route test |
| DEF-003 | FinOps first budget could not be created in Console | Moderate | UNVERIFIED | Pending fresh empty-budget journey |
| DEF-004 | CLI `config show` shadowed by duplicate command group | Moderate | UNVERIFIED | Pending built RC4 CLI interaction |
| DEF-005 | MCP/A2A private endpoint forms omitted required CIDR allowlist | Moderate | UNVERIFIED | Pending both real registration journeys and endpoint gating |
| DEF-006 | Team Admin member form could not submit project scope | Moderate | UNVERIFIED | Pending non-operator role journey |
| DEF-007 | Org/project/environment creates were missing audit events | Major | UNVERIFIED | Pending fresh provisioning, audit-chain and rollback/attribution checks |
| DEF-008 | Console backup action failed to reach configured backup runtime | Moderate | UNVERIFIED | Pending encrypted backup verify/restore on eligible state |
| DEF-009 | WebKit 375px webhook drawer observed offscreen (conflicting history) | Moderate | UNVERIFIED | Pending stable authenticated WebKit visual retest; CP49/65 invalid target state |
| DEF-010 | Supported Virtual Key provisioning could not grant A2A | Major | UNVERIFIED | Pending least-privilege issuance journey; preserve provider-only 403 |
| DEF-011 | A2A `input-required` task could not resume | Major | UNVERIFIED | Pending same-task input continuation, lifecycle and authorization |
| DEF-012 | FinOps headline ignored usage settlement without budget rows | Major | UNVERIFIED | Pending no-budget exact settlement reconciliation |
| DEF-013 | Ollama form defaulted to commercial API/US | Moderate | UNVERIFIED | Pending adapter-selected default and persisted metadata check |
| DEF-014 | Console CSV export parsed CSV as JSON | Moderate | UNVERIFIED | Pending actual CSV download and valid file inspection |
| DEF-015 | Exports ignored selected group-by dimension | Moderate | UNVERIFIED | Pending multi-entity grouping and CSV/JSON reconciliation |

## Retest evidence log

| Checkpoint | Defects | Result, evidence, and limits |
| --- | --- | --- |
| CP00 | All 15 | No defect behavior tested yet. Baseline/toolchain only; see `checkpoints/CP00-baseline.md`. |

## Acceptance criteria sources

Consult the original detailed register and targeted handoffs: `qa/DEFECT-REGISTER.md`; `qa/engineering-handoffs/DEF-005-private-endpoint-cidr-input.md`; DEF-010 through DEF-015 handoffs. QA-002 retests should use the expected/actual, reproduction, business impact, and acceptance criterion recorded there. For DEF-001 and DEF-009 preserve both sides of historical conflicting evidence. For DEF-010 do not weaken authorization. For DEF-002 isolate local fixture behavior from Google Cloud egress. For DEF-007 verify attribution and atomicity, not just event count. For DEF-008 verify a complete encrypted restore workflow, not merely UI state.
