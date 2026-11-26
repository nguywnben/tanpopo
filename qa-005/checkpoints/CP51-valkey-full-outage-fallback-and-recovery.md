# CP51 — Valkey full outage fallback and recovery

**Result:** **PASS (scoped documented degraded behavior)** for PostgreSQL-backed Data Plane service, conservative per-node RPM fallback, PostgreSQL-backed key revocation/replay denial, and cluster recovery. A separate Operations UI visibility observation is recorded as PG-007, not a defect.  
**Baseline:** exact unsigned source 737079ddb847d97dbcf832f610ed0048556cb65b; compiled Linux amd64 artifact SHA-256 62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9.  
**Runtime:** exact-source Control Plane :18151 and Data Planes :18152/:18153; PostgreSQL 16 remained available; three Valkey 8.1 cluster primaries at :17001–:17003; Chromium Owner. No real/paid provider.

## Documented behavior under test

Frozen docs state that while PostgreSQL remains healthy and Valkey is unreachable, rate limiting falls back to conservative per-node slices (limit divided by estimated node count), while key revocation uses PostgreSQL LISTEN/NOTIFY plus config-changelog polling. The readiness contract only requires /readyz to fail after a node is partitioned from both PostgreSQL and Valkey for more than 10 seconds. Evidence is in docs/operations/monitoring-incidents.md, docs/planning/units/TP-IMP-0040.md, and docs/operations/monitoring-incidents.md FMEA/INC-07 sections.

## Test procedure and actual result

1. Through the real Access & Keys UI, create an environment-bound PROVIDER key and set rpm_limit=6 through the authenticated, versioned Admin API (If-Match v1; successful PATCH advanced key version to v2). The test key last four was DVNF; its plaintext stayed in a mode-restricted /tmp file and was never recorded.
2. Stop all three isolated Valkey containers, leaving PostgreSQL and all Tanpopo processes running. Direct readiness probes returned HTTP 200 for CP, DP1 and DP2. The UI Operations screen continued to show Ready/Serving traffic and three healthy Tanpopo process rows.
3. Send four equivalent synthetic no-route requests to each Data Plane, sequentially, with the same key. On each Data Plane, requests 1–2 passed admission and returned HTTP 503 no_compliant_deployment_available because no model deployment/route exists; requests 3–4 returned HTTP 429 rate_limit_exceeded. The 2-request local allowance per Data Plane is consistent with a conservative 6/3 per-node fallback in this CP+2-DP process topology. Metadata-only Traces UI displayed all eight corresponding 503/429 records. No provider traffic occurred.
4. While all Valkey nodes remained stopped, revoke the same key in the real Control Plane Access & Keys UI. Revocation returned HTTP 200, key state REVOKED version 3. Subsequent credential replay on both Data Planes returned HTTP 401 invalid_virtual_key, demonstrating PostgreSQL fallback revocation under this topology; no numeric propagation SLA was measured.
5. Restart all Valkey containers. After cluster convergence, Valkey reported cluster_state=ok, 16,384 healthy slots, 3 known nodes/3 primaries; all three Tanpopo /readyz probes returned HTTP 200. Operations UI showed the Control Plane and two Data Planes with current heartbeats and OK health. The fallback key inventory showed Revoked.

## Product-gap observation, not defect

The visually inspected Operations view during complete Valkey unavailability still labels the service Ready/Serving and shows the three Tanpopo process nodes as OK; it does not show Valkey dependency/shard health. That is consistent with the documented readiness contract while PostgreSQL remains available, and the required fallback behaviors worked in this slice. No accepted requirement found in the baseline requires a Valkey-specific health panel, so this is recorded separately as PG-007 for TEEP-01 product/operations review. See [PG-007](../PRODUCT-GAP-REGISTER.md).

## Evidence and limits

- [Sanitized test record](../evidence/CP51-valkey-outage-fallback-and-recovery.json)
- Visually inspected screenshots: [key form](../evidence/screenshots/CP51-valkey-outage-key-form.png), [revoked key during outage](../evidence/screenshots/CP51-valkey-revoke-fallback.png), [Operations during Valkey outage](../evidence/screenshots/CP51-operations-during-valkey-outage.png), [trace outcomes](../evidence/screenshots/CP51-valkey-fallback-traces.png), and [recovered Operations inventory](../evidence/screenshots/CP51-valkey-recovered-ops.png), and [final inventory with all 10 campaign keys revoked](../evidence/screenshots/CP51-all-test-keys-revoked.png).
- One Owner, two Data Planes, three primary-only Valkey nodes, no replicas, no active budget ceiling, and one RPM key. No concurrent requests or 5-second revocation SLA timing was measured. Budget pre-hold safety margins, notification polling upper bound, emergency revocation and active-stream severance remain UNVERIFIED. Full database+Valkey partition >10 seconds and readiness fail-closed were not tested. A prior single-Valkey-node 7-second interruption yielded cluster_state=fail but runtime probes stayed Ready; it is not used to claim full-outage fallback or a defect.
