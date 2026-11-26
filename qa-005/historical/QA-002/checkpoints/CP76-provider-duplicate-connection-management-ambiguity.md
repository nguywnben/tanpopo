# CP76 — Duplicate provider connection management ambiguity

- **Baseline:** frozen RC4 `40bba5b55c2da6fc00b107b315c9b3167fb20169`; product source unchanged.
- **Result:** FAIL, new Low defect DEF-032 confirmed for the observed duplicate-record state.
- **Runtime:** isolated A2A-enabled runtime `127.0.0.1:18084`; real Chromium; authenticated Org Owner.

## Journey and evidence

Opened Providers → Connections and inspected the two persisted Google Gemini rows. Sanitized Admin API output shows distinct record IDs in the same project. The records share the same name, endpoint, adapter, state, billing mode, region and masked secret fingerprint. Each row menu offered Probe, Edit, Disable and Delete. Opening each Edit form showed the same fields and values and no visible scope, record ID or creation metadata. I visually inspected the table, both action menus and both edit forms.

Evidence: `../evidence/CP76-provider-duplicates-table.png`, `CP76-provider-duplicate-1-menu.png`, `CP76-provider-duplicate-2-menu.png`, `CP76-provider-duplicate-1-edit.png`, `CP76-provider-duplicate-2-edit.png`, and sanitized `CP76-provider-duplicate-comparison.json`.

No probe, credential rotation, disable, delete, or external provider request was made. This finding is limited to inability to distinguish duplicate records before an action. It does not assert that duplicate creation itself is invalid, and does not reclassify Google probe/runtime failures.

## Consolidated status

- **Historical DEF-001–015:** CP76 did not retest their original acceptance criteria; preserve the scoped statuses from prior checkpoints. DEF-005 remains PARTIAL with its DNS/rebinding/redirect/alternate-runtime gaps.
- **Other new defects:** DEF-016–031 remain as recorded; no reclassification in CP76.
- **New finding:** DEF-032 Low, indistinguishable provider records in the observed duplicate state; see `../reports/DEF-032-provider-connections-indistinguishable-when-duplicate.md`.
- **Coverage completed:** real UI inspection and sanitized API comparison of duplicate provider connection rows, action menus and edit forms.
- **Blocked:** none for CP76. Google provider egress/probe was not attempted. Existing webhook/operator and RC4 artifact limitations remain.
- **Remaining:** determine duplicate-name policy and verify selected-record targeting with an authorized QA-owned duplicate fixture; do not test against Google. Continue independent historical defect retests, workspace scope consistency across remaining Console surfaces, integrations, roles, browsers and operations.

`GATE-REL-07` remains **BLOCKED**. QA-002 remains in progress.
