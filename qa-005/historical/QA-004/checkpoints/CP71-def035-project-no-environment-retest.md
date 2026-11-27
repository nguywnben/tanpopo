# CP71 — DEF-035 project-only / no-environment retest

**Baseline:** exact frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`  
**Runtime:** isolated SQLite RC6 runtime `127.0.0.1:18128`  
**Role:** authenticated Org Owner/Operator  
**Browser:** Chromium, 1440×1000; screenshot visually inspected  
**Product source modified:** no

## Result

**DEF-035 remains PARTIAL on exact RC6.** In the real Console, the isolated canary project selected with **No env** showed zero deployments and no sibling row or action. The actual deployment request included `project_id=prj_01m89cf7vce008saenf6dyw8gk` and returned HTTP 200 with zero rows. This independently extends CP70, where the selected empty environment also showed zero and the owning `default/dev` view showed its two own deployments.

The organization-only UI/API contract remains unclear: the Console's “All environment deployments” heading accompanied an empty table while the unfiltered Org Owner deployment API returned two organization records. No defect is assigned from this contract ambiguity. No deployment actions or data mutations were performed.

## Reproduction and evidence

1. Sign in as the isolated root Org Owner in Chromium.
2. Select `QA004 Test Root Org / QA-004 Isolated Canary / No env` in the workspace scope picker.
3. Open **Models**. The page displays “All environment deployments — 0” and “No deployments found.”
4. Observe the authenticated request `GET /admin/v1/deployments?project_id=prj_01m89cf7vce008saenf6dyw8gk&limit=100`: HTTP 200, zero records.

See [visually inspected screenshot](../evidence/CP71-models-project-noenv.png) and [sanitized request/scope evidence](../evidence/CP71-def035-project-noenv-readback.json). CP70 has the owning and empty-environment controls.

## Acceptance coverage remaining

Other roles, refresh and rapid scope-switch races, pagination, ownership labels, destructive action authorization/lifecycle, and the org-only heading versus API contract remain UNVERIFIED or contract-unclear. QA continues; `GATE-REL-07` remains **BLOCKED**.
