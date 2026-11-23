# QA-001 defect retest register — QA-002

Baseline: `40bba5b55c2da6fc00b107b315c9b3167fb20169`. The acceptance criteria are from the QA-001 register and standalone handoffs at `qa/qa-001-handoff-cp65@d994f82128f0e78a4e7bb7dd4ecd705cee60d85f`. Historical PASS results are not inherited. **All entries start UNVERIFIED for RC4 until new evidence is recorded below.**

| ID | Historical finding | Original priority | QA-002 status | Current checkpoint/evidence |
| --- | --- | --- | --- | --- |
| DEF-001 | Setup progress button accessible name omitted visible text | Major | VERIFIED FIXED (scoped) | CP03: all four setup steps in real Chromium, accessible name includes visible step text; axe 0 violations each; screenshots visually inspected. Other browsers/shared components remain open. |
| DEF-002 | Failed/unverified provider could be promoted to Healthy | Major | UNVERIFIED | Pending controlled failed-probe activation and dependent route test |
| DEF-003 | FinOps first budget could not be created in Console | Moderate | VERIFIED FIXED (scoped) | CP05 real owner journey created project budget from zero; new authenticated session/list API showed the persisted v1 record and expected 2,500,000 micro-USD limit. Other scopes/enforcement unverified. |
| DEF-004 | CLI `config show` shadowed by duplicate command group | Moderate | VERIFIED FIXED (scoped) | CP04 built baseline binary resolves `config show` and `config reconcile`; synthetic endpoint summary rendered, missing desired file rejected fail-closed. Live Tanpopo Admin API/auth and valid reconcile remain unverified. |
| DEF-005 | MCP/A2A private endpoint forms omitted required CIDR allowlist | Moderate | PARTIAL PASS | CP08 A2A registration rejected wildcard CIDR and accepted explicit loopback /32; MCP path and endpoint gating remain open. |
| DEF-006 | Team Admin member form could not submit project scope | Moderate | UNVERIFIED | Pending non-operator role journey |
| DEF-007 | Org/project/environment creates were missing audit events | Major | VERIFIED FIXED (scoped) | CP06: bootstrap and standalone project/env create events present with actor, target, tenant partition and metadata; duplicate create 409 produced no partial row/event; chain intact. Standalone org, write faults, update/delete, and cross-tenant paths remain open. |
| DEF-008 | Console backup action failed to reach configured backup runtime | Moderate | UNVERIFIED | Pending encrypted backup verify/restore on eligible state |
| DEF-009 | WebKit 375px webhook drawer observed offscreen (conflicting history) | Moderate | VERIFIED FIXED (scoped) | CP07 authenticated HTTPS WebKit 26.5 @ 375×844: drawer heading/form/footer visible, document width 375, no overflow. HMAC credential missing so registration not tested. Conflicting QA-001 evidence preserved. |
| DEF-010 | Supported Virtual Key provisioning could not grant A2A | Major | VERIFIED FIXED (scoped) | CP08 UI issued active A2A-only `/a2a/*` key; discovery succeeded, provider-only key remained 403, then revoked key became 401. Other scopes/roles and rotation remain open. |
| DEF-011 | A2A `input-required` task could not resume | Major | VERIFIED FIXED (scoped) | CP08 three task lifecycles exercised; Console resumed same input-required task ID to completed with settlement. API resume, cancel, terminal transition rejections, and persistence verified. Other task states/roles remain open. |
| DEF-012 | FinOps headline ignored usage settlement without budget rows | Major | UNVERIFIED | Pending no-budget exact settlement reconciliation |
| DEF-013 | Ollama form defaulted to commercial API/US | Moderate | UNVERIFIED | Pending adapter-selected default and persisted metadata check |
| DEF-014 | Console CSV export parsed CSV as JSON | Moderate | UNVERIFIED | Pending actual CSV download and valid file inspection |
| DEF-015 | Exports ignored selected group-by dimension | Moderate | UNVERIFIED | Pending multi-entity grouping and CSV/JSON reconciliation |

## Retest evidence log

| Checkpoint | Defects | Result, evidence, and limits |
| --- | --- | --- |
| CP00 | All 15 | No defect behavior tested yet. Baseline/toolchain only; see `checkpoints/CP00-baseline.md`. |
| CP03 | DEF-001 | Not reproduced on this source baseline across the complete live Setup wizard. Four axe scans returned 0 violations; wizard persisted and initialized. Historical QA-001 failures remain preserved; scope limits are explicit in `checkpoints/CP03-setup-accessibility.md`. |
| CP04 | DEF-004 | Duplicate command/shadowing did not reproduce in the built binary; see `checkpoints/CP04-cli-config.md` for synthetic fixture limits. |
| CP05 | DEF-003 | No-budget UI exposed create flow; created project budget persisted across reload/new login and matched Admin API state. See `checkpoints/CP05-finops-first-budget.md`. |
| CP06 | DEF-007 | Historical missing provisioning events did not reproduce in current bootstrap or separate project/environment creation. Negative duplicate create left state/audit count unchanged; audit verifier intact. See `checkpoints/CP06-provision-audit.md`. |
| CP07 | DEF-009 | Offscreen drawer did not reproduce in authenticated HTTPS WebKit 26.5 at 375px; screenshots visually inspected. Plain-HTTP Secure-cookie behavior was isolated and corrected by HTTPS test proxy. See `checkpoints/CP07-webkit-webhook-mobile.md`. |
| CP08 | DEF-005, DEF-010, DEF-011 | A2A registration accepted an explicit loopback CIDR after rejecting wildcard CIDR; least-privilege A2A key successfully performed discovery and three same-ID task lifecycles (input-required→completed, input-required→canceled, and Console resume→completed). Provider-only key was denied; revoked A2A key was denied. Audit and synthetic upstream state corroborate UI/API. See `checkpoints/CP08-a2a-authorized-lifecycle.md`. |

## Acceptance criteria sources

Consult the original detailed register and targeted handoffs: `qa/DEFECT-REGISTER.md`; `qa/engineering-handoffs/DEF-005-private-endpoint-cidr-input.md`; DEF-010 through DEF-015 handoffs. QA-002 retests should use the expected/actual, reproduction, business impact, and acceptance criterion recorded there. For DEF-001 and DEF-009 preserve both sides of historical conflicting evidence. For DEF-010 do not weaken authorization. For DEF-002 isolate local fixture behavior from Google Cloud egress. For DEF-007 verify attribution and atomicity, not just event count. For DEF-008 verify a complete encrypted restore workflow, not merely UI state.
