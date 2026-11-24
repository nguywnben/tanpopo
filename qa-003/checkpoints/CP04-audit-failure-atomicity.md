# CP04 — Audit append failure and provisioning atomicity

**Baseline:** RC5 `3423d4f523ef0ac77469a1201b8b753d1d31c902`
**Status:** Synthetic SQLite failure-injection cases executed; full DEF-007 scope and QA campaign remain open. `GATE-REL-07` remains **BLOCKED**.

## Setup and execution

To keep failure injection away from the primary QA runtime, create an encrypted CLI backup of its synthetic database, restore it to a separate private `/tmp` directory, then install test-only SQLite triggers in that clone that abort inserts into `audit_events` for `organization.created`, `project.created`, and `environment.created`. No application code was changed. Start a separate server on `127.0.0.1:18105`.

Through the real Operations → Organizations UI, submit a synthetic organization create while the organization audit insert is forced to fail. The UI displayed `failed creating organization`, the Admin API returned HTTP 500 `internal_error`, the organization was absent from the Admin API, and direct read-only DB inspection showed no partial organization row and no create event. The visually inspected screenshot records the populated create form and inline failure: [screenshot](../evidence/CP04-audit-failure-org-create.png). Sanitized response/UI evidence: [JSON](../evidence/CP04-audit-failure-injection.json).

Then, in the same isolated clone, submit project and environment creates through the authenticated Admin API using the browser session and CSRF token. The injected audit failures returned HTTP 500 (`failed creating project` / `failed creating environment`). Follow-up list reads showed neither object persisted; project/environment counts remained one each (the seeded defaults). See [project/environment failure evidence](../evidence/CP04-audit-failure-project-env.json).

## Disposition

- **DEF-007 — PARTIAL.** Successful org/project/environment/key audit events verified in CP02, and injected audit append failures for organization, project and environment creation all rolled back the corresponding business records on the isolated SQLite runtime. The UI reported failure for organization creation. Additional mutation types, low-level disk-full/IO failures, other database backends, concurrent create races and audit-sink failures outside these event types remain unverified.
- No product defect confirmed by this phase. A failure response with no partial business row is the expected transactional outcome.
- The injected trigger and cloned DB are private `/tmp` artifacts and are excluded from the branch.
