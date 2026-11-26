# CP45 — DEF-072 exact-source split-role inventory

**Fresh classification:** PARTIAL (historical RC6 omission does not reproduce with both RC7 roles alive; missing expected-node visibility remains unresolved)  
**Source:** `737079ddb847d97dbcf832f610ed0048556cb65b`  
**Binary:** `/tmp/qa005-bin/tanpopo`, SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`  
**Environment:** fresh exact-source Control Plane `127.0.0.1:18151` and Data Plane `:18152`, sharing a disposable PostgreSQL 16 database/app role and Master KEK; Chromium; synthetic Owner. No Valkey was configured in this run: the available local Valkey service was standalone, not cluster-enabled.

## Healthy split-role behavior

1. Provision an isolated PostgreSQL database and application role.
2. Start the exact-source Control Plane and complete first-time setup in the real Console.
3. Start a separate Data Plane using the same PostgreSQL database and Master KEK.
4. Verify each listener's `/readyz`, attempt unauthenticated Data Plane model discovery, then inspect authenticated `GET /admin/v1/ops/cluster-status` and Operations → Health & Cluster.

Both processes returned HTTP 200 readiness. Unauthenticated `GET /v1/models` at the Data Plane returned HTTP 401 `invalid_virtual_key`, consistent with the ingress boundary. The authenticated inventory returned two distinct rows, including the ready Data Plane and Control Plane, both at config epoch 1 with health `ok`. The visually inspected Operations screen showed `Cluster nodes 2`, the distinct IDs/roles, heartbeat ages, epoch and `OK` state. This reverses the narrow RC6 reproduction where a ready split Data Plane was omitted.

## Node-loss follow-up and uncertainty

I terminated only the disposable Data Plane and waited for heartbeat expiry. The Control Plane continued to return HTTP 200 from `/readyz`. After refresh, the API returned one row (the live Control Plane) and the Console showed `Ready` and `Cluster nodes 1`; the vanished Data Plane row was removed, with no incomplete, missing or unreachable indicator. The screenshot was visually inspected.

The scenario had no configured desired replica count/topology. The API reports live heartbeat members; whether an expired process remains an “active” node or should be surfaced as missing is therefore unresolved in this run. Because the original active-node omission now passes but expected-node loss is not demonstrably covered, DEF-072 is **PARTIAL**, not PASS. Historical RC6 FAIL evidence remains preserved.

Evidence includes the [healthy inventory screenshot](../evidence/screenshots/CP45-split-ops-inventory.png), [post-loss screenshot](../evidence/screenshots/CP45-split-ops-after-dataplane-stop.png), and [sanitized machine-readable record](../evidence/CP45-def072-split-node-inventory.json). Fresh two-node Valkey Cluster, configured expected topology, deliberately stale-but-live heartbeats, restart/rejoin, multi-control-plane, epochs after a real config mutation, authorization matrix, and production node-loss behavior remain UNVERIFIED.
