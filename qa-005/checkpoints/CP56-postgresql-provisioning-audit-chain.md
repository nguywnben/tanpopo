# CP56 — PostgreSQL provisioning events and audit-chain verification

**Frozen source:** `737079ddb847d97dbcf832f610ed0048556cb65b`
**Runtime:** exact-source Linux/amd64 binary SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`; isolated PostgreSQL 16 / three-node Valkey 8.1; Control Plane `127.0.0.1:18151`.
**Identity/browser:** synthetic Org Owner; Chromium, real Console.
**Disposition:** **PASS (scoped)** for fresh PostgreSQL successful-path DEF-007 provisioning audit and visible RFC 8785 JCS chain integrity.

## Procedure and evidence

On the fresh PostgreSQL database restored for CP52, the initial setup wizard had created one organization, its first project, and its `dev` environment. As the Owner, queried the Admin API audit list filtered separately for `organization.created`, `project.created`, and `environment.created`. Each returned HTTP 200 and exactly one event. Sequence numbers were 1–3; targets were respectively the expected organization, project, and environment IDs, with each event in the same organization chain partition.

Then opened Security → Audit Log and clicked **Verify hash chain**. The Console sent `POST /admin/v1/audit-events/verify`, which returned HTTP 200, `intact: true`, `checked_events_count: 55`, and no broken sequence. The page showed the green message “Cryptographic RFC 8785 JCS chain intact across 55 events.” The screenshot was visually inspected and matched the API result.

This is fresh independent PostgreSQL evidence complementing CP30's SQLite Org Owner provisioning pass. It keeps DEF-007 at **PASS (scoped)**; it does not establish atomicity under storage failure, concurrent provisioning, update/delete event contracts, tamper rejection, other role attribution, or audit export/telemetry integrity. No new defect was assigned. Sanitized event rows and verification result are in [CP56 JSON](../evidence/CP56-postgresql-provisioning-audit-chain.json); the screenshot is [here](../evidence/screenshots/CP56-postgres-audit-chain-verified.png). GATE-REL-07 remains **BLOCKED**.
