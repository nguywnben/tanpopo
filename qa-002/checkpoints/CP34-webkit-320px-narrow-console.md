# CP34 — WebKit Console at 320×720

**Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`
**Runtime:** disposable SQLite Tanpopo on `127.0.0.1:18086`, local HTTPS proxy, WebKit 26.5, synthetic QA state
**Release gate:** `GATE-REL-07` remains **BLOCKED**.

## User actions and results

- Signed in as the Org Owner at 320×720 and visually inspected the settled Overview, responsive navigation drawer, Security → Approval Queue, Security → Audit Log, and Operations → Health & Cluster.
- On Overview, the four summary KPIs stack into one column; request count, 53.85% error rate, latency, spend, alert count, and nominal copy remained readable. No horizontal document overflow: viewport/document/body widths were all 320.
- The navigation drawer fills nearly the full width and scrolls vertically; its lower menu items are below the first viewport. Selecting Operations from that scrollable drawer succeeded and loaded the Operations screen.
- On Security, the three-tab strip is wider than its viewport (365px scroll width vs 296px client width), with Audit Log initially mostly clipped. Selecting Audit Log through the real browser interaction moved the strip to the selected tab and loaded 100 persistent audit-event rows; the selected view remained within the 320px document width. The Audit Log and Operations tab strips likewise expose only part of a subsequent tab at rest. No horizontal-scroll affordance is obvious in the screenshots; discoverability by touch is not yet evaluated and remains a UX question, not a confirmed defect.
- Operations showed readiness “Ready / Serving traffic,” SQLite WAL as the active backend, one standalone cluster node and health OK. API reads for session, workspace and Security audit/approvals returned 200.
- The Overview again showed 13 requests, 53.85% internal/upstream errors, zero active alerts, and “All systems nominal.” This further reproduces QA-OBS-003 but uses accumulated negative-test traffic; alert semantics remain unresolved.
- All listed screenshots were visually inspected after settling. The first Overview capture was a transient “Loading Overview…” frame and was discarded; the retained Overview screenshot is the settled state.

## Evidence

- `../evidence/CP34-webkit-overview-settled-320.png`
- `../evidence/CP34-webkit-drawer-320.png`
- `../evidence/CP34-webkit-security-320.png`
- `../evidence/CP34-webkit-security-audit-320.png`
- `../evidence/CP34-webkit-operations-320.png`
- `../evidence/CP34-webkit-operations-320.json`
- `../evidence/CP34-webkit-320.json` (sanitized viewport, tab dimensions, visible text and API outcomes)

## Limits

This is a narrow-width, owner-role pass over the listed screens. Touch gestures, keyboard-only tab traversal, other roles, provider error states, 320px form submission, remaining routes, locales/themes and other browser engines remain UNVERIFIED. No product code was changed.
