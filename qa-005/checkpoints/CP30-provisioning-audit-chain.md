# CP30 — Organization, project, and environment provisioning audit

**Baseline:** exact frozen source `737079ddb847d97dbcf832f610ed0048556cb65b`; local source-built Linux amd64 runtime `127.0.0.1:18145`, isolated SQLite; real Chromium 1440×1000; synthetic Operator/Org Owner. No product source changes.

## Result: PASS (scoped DEF-007 successful provisioning paths)

In the Console, switched to the QA-only secondary organization and created project `QA005 Audit Project` (`qa005-audit-project`) using the workspace-scope New project flow. The project was returned by HTTP 201, appeared in the active scope, and refreshed in the project list. With that project selected, created environment `QA005 Audit Sandbox` (`qa005-audit-sandbox`) through New environment. The API returned HTTP 201 and the active scope showed the new environment. Screenshots of the forms, selected secondary-organization context and final scope were visually inspected: `CP30-org-project-scope-menu.png`, `CP30-project-create-form.png`, `CP30-environment-create-form.png`, and `CP30-project-environment-created.png`.

Read-only SQLite audit inspection found correctly targeted `organization.created`, `project.created`, and `environment.created` entries. Each event has target type/ID aligned with its resource, the project and environment chain entries use the secondary organization as their chain key, and the environment metadata references the created project. The exact-source CLI chain verification reported valid across 218 events with zero orphaned holds. Sanitized readback is in [`CP30-provisioning-audit.json`](../evidence/CP30-provisioning-audit.json).

This is fresh evidence for only successful Org Owner/Operator provisioning on SQLite. Duplicate/conflicting slugs, partial storage failure/atomicity, other roles, PostgreSQL event partitioning, project/environment update/delete audit contracts, concurrent create races and downstream failure recovery remain UNVERIFIED. Historical DEF-007 records and QA-004 results remain preserved; this result does not close broader criteria.
