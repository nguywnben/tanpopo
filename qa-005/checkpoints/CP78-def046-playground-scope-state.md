# CP78 — DEF-046 Playground scope-switch stale-state retest

**Source:** exact unsigned RC7 `737079ddb847d97dbcf832f610ed0048556cb65b`; Linux/amd64 runtime binary SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`; isolated SQLite clone `127.0.0.1:18162`; Chromium 156; synthetic Org Owner; two populated scopes in `QA005 Org`.

**Disposition:** **PASS (scoped QA-005 retest)** for `Initial Project / dev` → `QA005 No Budget Project / QA005 Budgetless Dev` in the same Playground tab. The prior failure state did not remain visible after the workspace switch. Other workspace transitions and requests remain open.

## Reproduction and result

1. In the real Console, selected `QA005 Org / Initial Project / dev`, opened Playground, and sent synthetic prompt `QA005 DEF046 old-project trace state scope isolation.` to non-existent slug `qa005-no-such-route-cp78`.
2. Data Plane returned HTTP 503 `no_compliant_deployment_available` and trace ID `trc_01m8dk1zm0e00bf8vxr674ga3w`. Before switching, the UI visibly retained that error, user turn, route slug and trace card; see the visually inspected [source screenshot](../evidence/screenshots/CP78-def046-source-scope.png).
3. Without leaving the page, opened the workspace selector and chose `QA005 No Budget Project`; selected scope updated to `QA005 Org / QA005 No Budget Project / QA005 Budgetless Dev`.
4. After the switch, the Playground showed the default route, **No turns yet**, no prior error banner, no old prompt/assistant turn and no trace card. The session cap remained visible, but no request turn was displayed. The visually inspected [target screenshot](../evidence/screenshots/CP78-def046-target-scope.png) confirms the changed scope and clean conversation state.

Sanitized details: [CP78 evidence JSON](../evidence/CP78-def046-playground-scope-state.json).

## Limits

One same-tab project-to-project switch was tested for one Owner in Chromium/SQLite. Organization switching, environment A→B, environment→no environment, rapid/out-of-order switches, refresh/return-to-source, in-flight requests, other roles and browsers remain UNVERIFIED. No server-side cross-scope leakage or successful inference is claimed; the request intentionally failed before provider dispatch. Historical QA-003 stale-state failure remains preserved. No source or specification changed. `GATE-REL-07` remains BLOCKED.
