# CP72 — Team Admin model/deployment project boundary

**Baseline:** exact frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`  
**Runtime:** isolated SQLite RC6 runtime `127.0.0.1:18128`  
**Role:** authenticated Team Admin with one assigned project  
**Browser:** Chromium, 1440×1000; screenshot visually inspected  
**Product source modified:** no

## Result

**Scoped authorization boundary: PASS in the exercised reads. DEF-035 remains PARTIAL overall.** The Team Admin's Console scope picker exposed only `default` and `No project`; selecting No project showed an empty Models & Deployments table. Its sole allowed project list contained only `default`. An unfiltered deployment read returned two deployments, both in that assigned `default/dev` project. Explicit read attempts against the sibling canary project's deployments and environments each returned 403 `insufficient_role_permission`.

This slice found no sibling project deployment/environment disclosure and no new defect. It does not settle the Org Owner organization-only heading/API ambiguity or prove all supported role/scope combinations, refresh races, row actions, or other resource types.

## Reproduction and evidence

1. Open the exact-RC6 Console using the isolated Team Admin account.
2. Inspect the workspace picker: its Project options are `default` and `No project`; no sibling project is offered.
3. Switch to `No project / No env`, navigate to Models, and inspect the zero-row state in [the screenshot](../evidence/CP72-teamadmin-no-project-models.png).
4. Using the same authenticated browser session, read `/admin/v1/projects?limit=100`, `/admin/v1/deployments?limit=100`, and explicitly query the sibling canary project deployments and environment list. Sanitized status/count readback is in [CP72 evidence](../evidence/CP72-def035-teamadmin-boundary.json).

No deployment or project was mutated.

## Remaining coverage

DEF-035 remains **PARTIAL**. Org Owner org-only contract interpretation, delayed/out-of-order responses, refresh and scope-switch races, deployment pagination/ownership labeling, disable/delete controls, and other supported roles remain UNVERIFIED or contract-unclear. `GATE-REL-07` remains **BLOCKED**.
