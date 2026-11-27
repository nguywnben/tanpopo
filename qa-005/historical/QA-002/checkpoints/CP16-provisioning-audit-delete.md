# CP16 — Project teardown audit and provisioning lifecycle

**Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`  
**Runtime:** isolated SQLite server on `127.0.0.1:18084`; authenticated Org Owner browser session. Only a newly created empty synthetic project was deleted; no product implementation changed.

## Actions and results

Using the real authenticated browser session's same-origin Admin API, QA created an empty synthetic project and environment. Both returned HTTP 201. The project was then deleted using the returned ETag, exact slug confirmation, and a test reason; deletion returned HTTP 200. Fresh project list state omitted the project, and read-only SQLite checks confirmed both the synthetic project and its environment rows were gone.

The Security → Audit Log screen was reopened after the action and allowed to finish loading. Actual screenshot `../evidence/CP16-audit-project-delete.png` was visually inspected. It displayed:

- `project.created` seq 125, actor principal and the synthetic project target.
- `environment.created` seq 126, same actor and the synthetic environment target.
- `project.crypto_shredded` seq 127, same actor and project target.

Read-only SQLite event records confirm the expected project/organization target and metadata for each action. Console Verify hash chain returned HTTP 200, `intact: true`, 127 events checked, and no broken sequence. Sanitized results: `../evidence/CP16-provisioning-audit-lifecycle.json`.

**Result:** the successful project/environment creation and project teardown paths emitted attributable audit events, and the resulting chain verified. This extends the scoped DEF-007 retest; it does not reverse or erase QA-001 historical evidence.

## Coverage limits

Standalone organization create/delete, injected failures between persistence and audit, duplicate/denied teardown, four-eyes approval, cross-tenant visibility, and restart persistence for deleted scope remain UNVERIFIED. Environment deletion is only covered by project cascade, not an independent action.

GATE-REL-07 remains **BLOCKED**. QA-002 continues.
