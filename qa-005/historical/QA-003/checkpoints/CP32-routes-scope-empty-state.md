# CP32 — Routes empty state and no-environment scope

**Baseline:** frozen RC5 source `3423d4f523ef0ac77469a1201b8b753d1d31c902`  
**Runtime:** isolated SQLite at `127.0.0.1:18116`; real Chromium 156; synthetic Org Owner  
**Disposition:** routes surface coverage **PARTIAL**. DEF-030 remains UNVERIFIED on RC5 because the available fixture has no route rows for a delayed-response test. No route was created or mutated. `GATE-REL-07` remains **BLOCKED**.

## User flow and observations

1. Selected `Default Organization / default / dev` and opened Routes & Policies → Routes. The page rendered an empty state and stated there were no routes in the environment. A visible Create route action was available. The screenshot was visually inspected: [default/dev routes](../evidence/CP32-routes-default-dev.png).
2. Authenticated `GET /admin/v1/routes?env_id=…&limit=100` returned HTTP 200 with zero routes. The environment inventory confirms this is the only environment with routes queried in the fixture.
3. Switched the workspace selector to `Default Organization / QA003 Empty Project DEF035 / No env`. Routes displayed the project/environment prerequisite and zero configured routes; the create action was disabled. The screenshot was visually inspected: [empty project scope](../evidence/CP32-routes-empty-project.png). The route page issued no environment-scoped request for the empty project.

## Limits and disposition

- This confirms only the zero-route screen and no-environment prerequisite. It does not exercise route creation, edit/delete, Route Explain, request dispatch, failover, or stale-response behavior with actual rows.
- DEF-030 requires a held non-empty route-list response and exposes stale Edit/Delete/Save actions. The RC5 fixture has no routes and the only existing deployment belongs to a disabled connection; no suitable healthy route target was available. Therefore DEF-030 stays **UNVERIFIED**, not fixed.
- FT-10 and SCR-08 move from UNVERIFIED to **PARTIAL** for their empty and scope-prerequisite screens.
- No new defect was confirmed; no route or deployment changed. QA-003 remains incomplete and `GATE-REL-07` remains **BLOCKED**.
