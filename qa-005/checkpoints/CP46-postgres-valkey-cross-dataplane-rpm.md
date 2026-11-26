# CP46 — PostgreSQL/Valkey shared RPM across two Data Planes

**Result:** PASS (scoped distributed Virtual Key RPM admission)  
**Source:** `737079ddb847d97dbcf832f610ed0048556cb65b`; exact-source Linux binary SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`  
**Environment:** fresh isolated PostgreSQL 16 database; three local cluster-enabled Valkey 8.1 instances (all reached `cluster_state:ok`, 16,384/16,384 slots, three known nodes); separate Control Plane `:18151` and Data Planes `:18152`/`:18153`; Chromium; synthetic Owner. No live providers or paid usage.

## User workflow and result

1. Complete fresh instance setup in Chromium on the Control Plane.
2. Use Access & Keys to issue an environment-bound PROVIDER Virtual Key for the test project/environment with wildcard model and route grants.
3. Set `rpm_limit=1` through the authenticated Admin API using the actual login CSRF token and resource version.
4. Send the first synthetic no-route completion request to Data Plane 18152, then an equivalent request to independent Data Plane 18153.
5. Inspect durable Traces and key metadata, then revoke the key in the real UI.

The first request passed key and RPM admission, then returned HTTP 503 `no_compliant_deployment_available` because this fixture intentionally had no provider route or deployment. The next request, through a distinct Data Plane process, returned HTTP 429 `rate_limit_exceeded`. The metadata-only Traces screen displays both outcomes and IDs; the API inventory confirms `rpm_limit=1`. This demonstrates that an exact-source RC7 request on one Data Plane was visible to RPM admission on a second process through the shared PostgreSQL/Valkey setup. No provider was contacted.

The synthetic key was revoked through Access & Keys. Final API readback reports state `REVOKED`, version 3, and only the ID/name/last four/limit metadata. The one-time plaintext is absent from saved evidence. The Operations screen, visually inspected after the run, listed the Control Plane and both Data Planes at the same config epoch with fresh heartbeats and `OK` health.

Visual evidence: [issue form](../evidence/screenshots/CP46-vkey-form.png), [two trace outcomes](../evidence/screenshots/CP46-rpm-traces.png), [revoked key inventory](../evidence/screenshots/CP46-vkey-revoked.png), [three-node inventory](../evidence/screenshots/CP46-three-node-inventory.png). Machine-readable details and limits are in [CP46 evidence](../evidence/CP46-postgres-valkey-cross-dataplane-rpm.json).

This is a narrow PASS for shared RPM enforcement over this no-route two-node path. TPM/budget fairness, concurrency races, Valkey outage/failover/rebuild, replica promotion, real provider traffic, other key policies, tenant boundaries and production scale remain UNVERIFIED. No product implementation or specification was changed.
