# CP70 — DEF-035 workspace-scope retest for model deployments

**Baseline:** exact frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`  
**Runtime:** isolated SQLite RC6 runtime `127.0.0.1:18128`  
**Role:** authenticated Org Owner/Operator  
**Browser:** Chromium, 1440×1000; screenshots visually inspected  
**Product source modified:** no

## Result

**DEF-035: PARTIAL on RC6; prior RC4/RC5 results were not inherited.** In the two tested non-owning contexts (organization-only and an empty canary project's selected environment), the Console showed zero deployments and no sibling deployment action. In the actual owning `default / dev` scope, both deployments appeared and their `env_id` matched that environment. A project/environment-filtered Admin API read for the empty canary scope returned zero; the same filtered read for `default / dev` returned two.

One contract observation remains unresolved: an unfiltered Org Owner `GET /admin/v1/deployments?limit=100` returned both organization records while the organization-only Console displayed “All environment deployments” and zero rows. This may be an intentional scope filter; current reviewed SCR-05 text does not define the organization-only view. No defect is assigned from this ambiguity.

## Reproduction and evidence

1. Sign in as the isolated root Org Owner and switch to `QA004 Test Root Org / No project / No env`. Open Models. The actual Console shows **All environment deployments — 0 / No deployments found**. A separate authenticated org-only list call returns the two available organization deployment records, both bound to `default/dev`'s environment. See [org-only screenshot](../evidence/CP70-models-org-only.png).
2. Switch to `QA-004 Isolated Canary / qa004-scope-canary-env` and open Models. The real UI shows **Environment deployments — 0 / No deployments found** and no foreign row/actions. The canary project's environment mapping is confirmed by `GET /admin/v1/projects/{projectId}/environments`; an explicit query filtered by that project and environment returns zero. See [empty-project screenshot](../evidence/CP70-models-empty-project-environment.png).
3. Switch to the owning `default / dev` scope and open Models. The actual UI shows exactly two rows; both rows' `env_id` equals `env_01m895k8pee008b5ah423mhysr`, and an explicit project/environment-filtered Admin API read returns the same two IDs. See [owning-scope screenshot](../evidence/CP70-models-owning-project-dev.png).

Sanitized scope and API readbacks are in [CP70 evidence](../evidence/CP70-def035-scope-readback.json). No deployment was disabled, deleted, or otherwise mutated.

## Expected versus actual

- **Expected:** a selected environment's deployment table should show deployments belonging to that environment. If the Console provides an organization-wide view, its scope should be clear and each row should identify ownership before offering destructive actions.
- **Actual in this slice:** the empty canary environment showed no sibling row/action; the owning environment showed its two deployments. The organization-only UI was empty even though an unfiltered Org Owner API read returned both deployments. The contract does not specify whether this organization-only state should aggregate inventory, so it remains an open observation.

Other acceptance criteria remain UNVERIFIED: project selected with **No env**, Team Admin/project-limited and other roles, delayed/out-of-order scope responses, browser refresh and route-return races, pagination, ownership labels, and actual disable/delete authorization. QA continues; `GATE-REL-07` remains **BLOCKED**.
