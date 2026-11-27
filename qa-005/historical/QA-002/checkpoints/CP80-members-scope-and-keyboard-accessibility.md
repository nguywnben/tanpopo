# CP80 — Members scope consistency and keyboard tab interaction

- **Baseline:** frozen RC4 `40bba5b55c2da6fc00b107b315c9b3167fb20169`; product source unchanged.
- **Runtime/browser:** isolated `127.0.0.1:18084`, authenticated Org Owner, real Chromium at 1440×900.
- **Result:** FAIL DEF-023 reconfirmed in a fresh project/no-environment state. PASS for the tested keyboard tab navigation and activation slice. No new defect ID.

## DEF-023: current UI state versus persistent organization state

1. Opened Access & Keys → Members & Roles with scope `QA-002 A2A Org / QA002 CP78 Fresh Budget / No env`.
2. The real UI displayed the blue prompt to select a project and environment, then **Organization members 0** and **No members found**.
3. In the same authenticated browser session, an explicit `GET /admin/v1/members?limit=100` returned HTTP 200 with four persisted principals and `has_more: false`.
4. Switched through the real workspace selector to project `QA002 Audit mwto47wa` and environment `stage`. The same Members & Roles page then displayed the four member records, matching the API count.
5. No member, role, project, or environment was changed.

This is an independent reproduction of DEF-023’s project-selected/no-environment state. The page’s own refresh path had already returned early in this context; direct same-session API evidence confirms the authorized organization collection was not empty. No authorization bypass was involved.

Evidence:

- `../evidence/CP80-members-no-environment-empty.png`
- `../evidence/CP80-members-no-environment-api.json`
- `../evidence/CP80-members-project-environment-populated.png`
- `../evidence/CP80-members-project-environment-api.json`
- `../evidence/CP80-access-keys-members-keyboard-activated.png`
- `../evidence/CP80-access-keys-keyboard-tab-10.png`
- `../evidence/CP80-access-keys-tab-activation.json`

Screenshots were opened and visually inspected. The no-environment screenshot shows the zero count and empty state; the project/environment screenshot shows all four rows.

## Keyboard interaction slice

On Access & Keys, keyboard Tab focus reached the selected Virtual Keys tab with a visible focus outline. ArrowRight moved focus to Members & Roles; Enter activated it, changed `aria-selected`, and loaded that tab. ArrowLeft followed by Space returned to Virtual Keys. The member panel showed the same DEF-023 empty state when activated under No env. This confirms only the tested desktop Chromium tab interaction; broader keyboard, screen-reader, viewport, browser, and role coverage remains open. The manual tab behavior is consistent with focus navigation followed by explicit activation.

## Consolidated phase status

- **Historical defects verified fixed:** CP80 does not change the scoped status of DEF-001–015. DEF-005 remains PARTIAL; no QA-001 PASS is inherited.
- **Historical defects failing / unverified:** DEF-023 was independently reconfirmed. DEF-016–033 remain open as recorded; no other status changed.
- **New findings:** none. No new DEF or observation ID assigned.
- **Coverage completed:** same-session UI/API state comparison at project/no-environment and project/environment scopes; real workspace switching; keyboard focus, arrow navigation, Enter activation, Space activation; screenshot inspection.
- **Coverage blocked / remaining:** A2A runtime 18445 remains configuration-disabled; webhook delivery still lacks usable Operator access and a controlled receiver/signing setup; RC4 artifact verification remains UNVERIFIED. Organization-only refresh, other member roles, other screens, more browsers/viewports/locales, assistive technology, and broader product coverage remain incomplete.
- **Recommended handoff:** include this clean project/no-environment reproduction in DEF-023 acceptance. A fix must preserve the intended project-level authorization boundary and render errors separately from a genuinely empty collection.

`GATE-REL-07` remains **BLOCKED**. QA-002 remains in progress.
