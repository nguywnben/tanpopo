# CP81 — Overview alert copy contract and time-window regression

- **Baseline:** frozen RC4 `40bba5b55c2da6fc00b107b315c9b3167fb20169`; no product implementation changes.
- **Runtime/browser:** isolated `127.0.0.1:18084`, Org Owner, real Chromium at 1440×900.
- **Result:** PASS for the tested 1h/24h/7d/30d KPI selection and UI/API consistency. QA-OBS-003 recurred; it remains an unresolved presentation-contract observation, not a confirmed defect.

## Real-browser flow and persistent API comparison

1. Signed in through the real Console and opened Overview. The initial 24h screen showed two requests, 50.00% internal/upstream error, 5.517 ms p95 proxy overhead, $0.012001 settled spend, zero holds, zero alerts, and “All systems nominal.”
2. Selected each visible radio option (`1h`, `24h`, `7d`, `30d`) through browser interaction. After each update, queried the same authenticated `/admin/v1/overview/summary?window=...` endpoint and compared the rendered KPI values.
3. At 1h, the UI/API both showed zero requests, 0% errors, 0 ms overhead, and zero spend/holds. At 24h, 7d, and 30d, both showed two requests, 50% errors, 5.517 ms overhead, and 12,001 micro-USD settled spend. All windows showed zero active alerts.
4. Read `/admin/v1/alerts/active` and the unhealthy deployment collection in the same session; both returned HTTP 200 with empty collections. No test request or other resource mutation occurred during CP81.
5. Visually inspected the actual 1h, 7d, and 30d screenshots; the 24h screen was also captured and inspected separately. The selected-window control and visible KPI values agreed with the corresponding API result.

Evidence:

- `../evidence/CP81-overview-1h-window.png`
- `../evidence/CP81-overview-24h-errors-no-alert.png`
- `../evidence/CP81-overview-7d-window.png`
- `../evidence/CP81-overview-30d-window.png`
- `../evidence/CP81-overview-alert-contract.json`
- `../evidence/CP81-overview-windows.json`

## QA-OBS-003 contract comparison

The documented Overview objective includes throughput, error rate, latency, spend, unhealthy deployments, and actionable alerts. The page’s “All systems nominal” card is rendered inside **Active alerts**, and its description says “No active operational alerts or pending remediation items.” The current alert collector documents budget utilization, unmetered spend, quarantined plugins, and OAuth reauthorization conditions; it has no request-error-rate threshold. The tested summary and active-alert APIs agree on the 50% error metric and zero alerts, and the unhealthy deployment list is also empty.

These facts support interpreting the card as alert posture, but they do not define whether “nominal” is appropriate when the dashboard simultaneously reports request errors. No explicit request-error alert threshold or wording requirement was found in the reviewed screen specification. Keep QA-OBS-003 unresolved; do not promote it to a defect or call the overall system healthy. The intentional negative-request history and small 2-request sample limit generalization.

## Consolidated phase status

- **Historical defects verified fixed:** no historical defect disposition changed. DEF-001–004, DEF-006–015 retain only their recorded scoped QA-002 passes; DEF-005 remains PARTIAL. QA-001 PASS results are not inherited.
- **Historical defects failing / unverified:** DEF-016–033 remain confirmed open. DEF-023 was reconfirmed in CP80. No CP81 finding changes those dispositions.
- **New findings:** none. QA-OBS-003 remains unresolved, and no new defect ID was assigned.
- **Coverage completed:** real UI selection and API comparison for all Overview time windows; active-alert and unhealthy-deployment empty-state checks; visual screenshot inspection.
- **Coverage blocked / remaining:** 18445 A2A plane remains configuration-disabled; webhook delivery still lacks usable Operator access and controlled receiver/signing setup; RC4 artifact remains UNVERIFIED. Healthy-vs-outage comparisons with a controlled provider, more roles/browsers/viewports, and wider operational coverage remain open.
- **Next work:** continue unexplored workflow and historical defect retests. Alert behavior for actual upstream outage and healthy recovery still requires a controlled integration path.

`GATE-REL-07` remains **BLOCKED**. QA-002 remains in progress.
