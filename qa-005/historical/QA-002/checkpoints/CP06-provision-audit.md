# CP06 — Provisioning audit events and chain integrity

**Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`  
**Status:** PASS for bootstrap, standalone project/environment creation, duplicate-create fail-closed behavior, and hash-chain verification. Other provisioning failure and mutation paths remain open.

On the fresh 18084 SQLite instance, the bootstrap-created organization, project, and environment each had their expected creation event. In a new authenticated Chromium session, created a second project and an environment through CSRF-protected same-origin Admin API calls. Both returned 201; a fresh audit query and the live Security → Audit Log screen showed `project.created` and `environment.created` rows with the same principal as actor, the expected tenant partition, resource IDs, and project/slug metadata. Screenshot `evidence/CP06-audit-log.png` was visually inspected; it displays the organization, bootstrap project/environment, and the new project/environment with sequence numbers and actor attribution.

Posted a duplicate project slug. It returned 409 `resource_already_exists`; project/audit counts stayed unchanged. The documented audit verification endpoint returned HTTP 200 with `intact: true`, checked 10 events, and no broken sequence. The failure did not add a partial event. No source code was changed.

This reconciles the QA-001 finding with new evidence: CP65/48 historical evidence remains intact, but the specific missing-event symptom does not reproduce on this baseline for bootstrap and successful project/environment creation. This scoped retest does not cover standalone organization creation, denied/failed persistence paths, updates/deletes, or cross-tenant visibility.

Machine-readable sanitized results: `evidence/CP06-provision-audit.json`.
