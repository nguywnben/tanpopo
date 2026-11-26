# CP91 — A2A task cards at mobile width

**Baseline:** exact frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`
**Outcome:** PASS (scoped) — the persisted CP90 canceled task and its expandable lifecycle details remained readable at a 390×844 viewport with no horizontal overflow. No defect assigned.
**Gate:** GATE-REL-07 remains BLOCKED. Product source unchanged.

## Real browser interaction and visual inspection

Signed in as the isolated test Org Owner in Chromium at 390×844 and opened MCP Hub → A2A Agents & Active Tasks. The CP90 input-required task, now Canceled, appeared as the first task card. The UI adapts to cards at this width; the list remains vertically scrollable. I opened the first task's `Detail (3)` disclosure using the visible rendered control. It displayed hops (1 / 5), sub-budget hold ($0.000000), and settled cost ($0.000000). The task card and adjacent records remained readable.

Both actual viewport screenshots were visually inspected: [mobile task cards](../evidence/CP91-a2a-mobile-390.png) and [expanded task details](../evidence/CP91-a2a-mobile-390-detail-expanded.png). Measured `innerWidth`, document scroll width, body scroll width, and main scroll width were all 390 pixels before/after the disclosure interaction. This is one Owner/mobile-width layout slice only; keyboard navigation, text scaling, other widths, roles, browsers, long values, and broader MCP/A2A flows remain UNVERIFIED.

## Evidence and disposition

The screenshot shows 3 registered A2A agents and 11 recent tasks, including canceled, completed, and failed states. Expanding the CP90 card exposes the expected persisted zero-dollar settlement details. No clipping, horizontal overflow, or new visual defect was observed in this slice. No product state was changed by this check.

QA remains ongoing. GATE-REL-07 remains BLOCKED.
