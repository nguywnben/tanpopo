# CP107 — DEF-055 retest on PostgreSQL

**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`
**Result:** project label persistence PASS (scoped); environment label persistence FAIL, confirming existing DEF-055 on PostgreSQL.

## Reproduction

Using the real Chromium Console on isolated PostgreSQL runtime 18133 as its synthetic Org Owner, created project `QA004 CP107 PostgreSQL Scope` / `qa004-cp107-pg-scope`, then created environment `QA004 CP107 Quality Assurance` / `qa004-cp107-qa` through the workspace picker.

The project POST returned HTTP 201 and its project list returned the exact distinct name and slug. The environment POST returned HTTP 201 with `name: QA004 CP107 Quality Assurance` and `slug: qa004-cp107-qa`. The subsequent project-scoped environment list returned HTTP 200 but `name: qa004-cp107-qa`, identical to its slug. After reloading the browser, the workspace scope picker displayed `qa004-cp107-qa`, not the submitted human-readable name. This reproduces DEF-055 on the PostgreSQL backend as well as the already documented SQLite-backed runs; it is additional backend coverage for the same defect, not a new defect.

The actual Security → Audit Log screen showed the targeted `project.created` and `environment.created` events and verified the RFC 8785 chain intact across 12 events. The project/environment remain as clearly labeled synthetic state in the isolated database because no cleanup flow was needed for this reproduction.

## Evidence

- Visually inspected [environment create form](../evidence/CP107-environment-form.png), [post-create state](../evidence/CP107-environment-created.png), [refreshed scope picker](../evidence/CP107-environment-scope-after-refresh.png), and [audit-chain UI](../evidence/CP107-audit-chain.png).
- [Sanitized create, list, scope, and audit readback](../evidence/CP107-postgres-project-environment-readback.json).

## Expected versus actual

- **Expected:** distinct environment name and slug persist through create, list readback, refresh, and workspace switching.
- **Actual:** create temporarily returns the supplied display name; persistent list and refreshed scope picker show only the slug.
- **Impact:** operators cannot use descriptive environment labels to distinguish staging/dev/prod during environment-scoped route, credential, budget, MCP/A2A, or governance work.

No Tanpopo implementation was modified. Existing [DEF-055](../reports/DEF-055-environment-display-name-not-persisted.md) remains open. QA continues; **GATE-REL-07 remains BLOCKED.**
