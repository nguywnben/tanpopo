# CP77 — Provider list across selected workspace scopes

- **Baseline:** frozen RC4 `40bba5b55c2da6fc00b107b315c9b3167fb20169`; no product implementation changed.
- **Result:** Organization-wide behavior observed; scope-label contract unresolved as QA-OBS-013. No confirmed defect and no authorization bypass.
- **Runtime/browser:** isolated A2A-enabled runtime `127.0.0.1:18084`; real Chromium; authenticated Org Owner.

## Journey and evidence

Opened Providers → Connections with `qa-002-a2a-project / dev` selected, then selected `QA002 Audit mwto47wa / stage` through the real workspace selector. The table continued to show the same five connection rows. The sanitized Admin API response for `GET /admin/v1/connections?limit=100` showed that all five were owned by `QA002 Audit mwto47wa`; none was owned by the initially selected A2A project. The visible table had no project context column.

I visually inspected both screenshots. `CP77-providers-scope-comparison.json` records the selected scopes and visible row text. `CP77-providers-scope-api.json` records sanitized IDs, owner project IDs, names, endpoints and states. No secret values are present. No provider operation or external call was performed.

The exact-baseline handler and UI client were read to classify the behavior: the endpoint allows an unfiltered organization-wide list for an Org Owner, and the Console calls it without `project_id`. This is consistent with an org-wide administrative registry; whether the UI should label each connection's owner/shared scope remains unresolved. See `../reports/QA-OBS-013-provider-connection-scope-labels.md`.

## Consolidated status

- **Historical DEF-001–015:** no original acceptance retest in CP77; preserve prior scoped results. DEF-005 remains PARTIAL with SSRF/redirect/rebinding/alternate-runtime cases open.
- **Confirmed new defects:** none in CP77. DEF-032 remains confirmed from CP76 for indistinguishable duplicate records. QA-OBS-013 is not a defect or security finding.
- **Coverage completed:** real user workspace switching on Providers and comparison with API ownership metadata; source contract read; screenshots visually inspected.
- **Blocked:** none in CP77. Google provider egress/probes were not attempted.
- **Remaining:** repeat with project-scoped Team Admin and resources in multiple projects; clarify expected list scope and project labeling. Continue untouched historical acceptance retests and broader feature, role, browser, integration and operational coverage.

`GATE-REL-07` remains **BLOCKED**. QA-002 remains in progress.
