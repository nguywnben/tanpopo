# CP74 — DEF-030 delayed route response after workspace switch

**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`  
**Runtime:** isolated RC6 browser runtime at `127.0.0.1:18128`  
**Role/browser:** synthetic Org Owner authorized for both same-organization projects; real Chromium at 1440×1000  
**Product source modified:** no

## Result

**DEF-030: PARTIAL on exact RC6.** The specific delayed-response variant did not reproduce. I held a real authenticated route-list response from `default/dev`, switched the active scope to the isolated canary project/environment, waited for its own zero-row response and empty page state, then released the old response. The page continued to show zero configured routes and no route rows/actions. This is not a PASS for the historical finding: the QA-002 reproduction had three foreign route rows, while this source fixture contains one synthetic route, and other timing/navigation variants remain open.

## Reproduction

1. Start the frozen RC6 test runtime on the isolated test database; sign in to Chromium as the test-root Org Owner.
2. Select `QA004 Test Root Org / default / dev` and open Routes & Policies.
3. Intercept and hold the response to `GET /admin/v1/routes?env_id=env_01m895k8pee008b5ah423mhysr&limit=100`. It returned HTTP 200 with one route, `qa004-loopback` (`rte_01m896capve00bxj3fmedpv16a`), belonging to the source environment.
4. While that response was held, switch the real workspace picker to `QA004 Test Root Org / QA-004 Isolated Canary / qa004-scope-canary-env`.
5. Verify its own route-list request returned HTTP 200 with zero rows and the UI showed `Configured routes 0` / `No routes in this environment`.
6. Release the held source response. Re-read the page and visible route rows/actions; it remained empty with no stale row or action menu.

Exact response and classification are in [sanitized readback](../evidence/CP74-def030-delayed-response.json). Visually inspected screenshots show the target empty state before [release](../evidence/CP74-target-before-release.png) and after [release](../evidence/CP74-target-after-release.png). The source screenshot was captured during a loading skeleton and is not treated as proof of source content; the authenticated response provides that evidence.

## Expected versus actual

- **Expected:** a response for a prior workspace scope must not populate route rows or actions after a different project/environment becomes active.
- **Actual:** in this single delayed-response test, the target environment stayed empty after the old response was released. No defect symptom was observed.
- **Limits:** the historical three-row scenario was not recreated. Refresh, in-app navigation, rapid repeated switching, alternate roles, target rows/actions, and other browser engines remain UNVERIFIED. No authorization bypass is implied by the historical same-organization state inconsistency.

QA continues; **GATE-REL-07 remains BLOCKED**.
