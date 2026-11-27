# CP113 — PostgreSQL duplicate provisioning and audit atomicity

**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1` (artifact run 45581179274)
**Result:** PASS (scoped) for duplicate-slug rejection without duplicate success audit events; supports historical DEF-007.

## Real Console workflow

On the restored PostgreSQL database at control-plane port 18135, signed in as the synthetic Org Owner and attempted to create a project using the existing slug `qa004-cp107-pg-scope`. The real Console retained the new project name and duplicate slug, showed the specific message “project slug already exists in organization,” and kept the creation form open. The POST returned HTTP 409 `resource_already_exists`.

Then, in the existing CP107 project/environment, attempted to create an environment with the existing slug `qa004-cp107-qa`. The form retained both values and showed “environment slug already exists in project”; POST returned HTTP 409 `resource_already_exists`.

Authenticated list readbacks returned exactly one project matching the project slug and exactly one environment matching the environment slug. Before and after each conflict, filtered audit reads returned two `project.created` events and two `environment.created` events respectively; neither failed duplicate submission emitted an additional success event. These events correspond to the fresh setup and CP107 provisioning already present in the restored snapshot.

## Evidence

- [Before/after row and filtered audit readbacks with conflict responses](../evidence/CP113-pg-duplicate-audit-results.json).
- Visually inspected [project conflict form with retained fields](../evidence/CP113-pg-duplicate-project-error.png) and [environment conflict form with retained fields](../evidence/CP113-pg-duplicate-environment-error.png).

This is a scoped PostgreSQL backend confirmation of conflict behavior, not proof of audit atomicity for storage failures, concurrent races, or partially successful provisioning. DEF-007 remains PASS (scoped); broader failure recovery and mutation coverage remain open. The environment display-name mismatch (DEF-055) remains independently present: the existing environment’s readback uses its slug as the name. No new defect or source change resulted. **GATE-REL-07 remains BLOCKED.**
