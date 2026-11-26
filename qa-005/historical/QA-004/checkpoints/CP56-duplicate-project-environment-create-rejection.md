# CP56 — Duplicate project/environment create rejection

**Status:** PASS (scoped conflict handling and no partial duplicate resource); other create failure and recovery cases UNVERIFIED. **GATE-REL-07:** BLOCKED.
**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`; isolated artifact `45581179274` at `127.0.0.1:18132`.
**Product source:** unchanged.

## Real UI and API results

As the synthetic Org Owner, I selected the project created in CP55 and used the actual **New environment** form. Submitting a different name with its existing project slug returned HTTP 409 `resource_already_exists` (“environment slug already exists in project”). The form surfaced the specific conflict and retained both entered values. Project-scoped environment-list readback remained HTTP 200 with one existing record and one matching slug.

I then submitted **New project** with a distinct name and an existing organization project slug. It returned HTTP 409 `resource_already_exists` (“project slug already exists in organization”). The Console displayed the specific message and kept the inputs in the form. Authenticated project listing remained HTTP 200 with three records and exactly one record for the submitted slug.

Security → Audit Log filters after the rejected attempts still showed only the three successful `project.created` events and two successful `environment.created` events; no duplicate create rows appeared. Screenshots were visually inspected. These conflict checks support scoped no-partial-create behavior; they do not simulate storage outages, mid-transaction failures, races, or corrected resubmission.

## Evidence

- [Duplicate environment form error and retained input](../evidence/CP56-duplicate-environment-rejection.png)
- [Duplicate project form error and retained input](../evidence/CP56-duplicate-project-rejection.png)
- [Environment create audit filter after failure](../evidence/CP56-audit-environment-after-duplicate.png)
- [Project create audit filter after failure](../evidence/CP56-audit-project-after-duplicate.png)
- [Sanitized API, list, UI, and audit results](../evidence/CP56-duplicate-provisioning-results.json)
- [Successful create paths and DEF-055 retest](CP55-project-environment-provisioning-and-audit-retest.md)

No product code, credentials, provider data, or production tenant was touched.

## Coverage

- **PASS (scoped):** duplicate project and environment slugs return explicit 409 errors, show the server message in the corresponding form, preserve entered values, create no duplicate list row, and add no successful-create audit record.
- **UNVERIFIED:** correcting the same form after conflict, storage fault injection, concurrent creation, compensation/rollback behavior, permission-denial atomicity, invalid syntax, other roles/tenants, and other provisioning resource types.
