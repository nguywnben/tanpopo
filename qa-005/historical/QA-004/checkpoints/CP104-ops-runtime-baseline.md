# CP104 — Operations view across existing isolated runtimes

**Frozen baseline:** RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`
**Result:** scoped health/readiness PASS on 18128 and 18124; HA and backup/restore acceptance UNVERIFIED.

The authenticated Operations → Health & Cluster view and `/admin/v1/ops/cluster-status` were read on the healthy 18128 and 18124 instances. Both reported `sqlite_wal`, a single healthy node, and `/readyz` 200. The 18128 response also reported config epoch 221, WAL bytes 4,157,112, and `backup_supported=true`; the 18124 screenshot showed epoch 36. These are separate isolated singleton runtimes, not evidence of a distributed cluster or failover.

The 18121 status probe returned 401 because its old stored browser session was stale. This was an authentication-state limitation and no product result was inferred. Unauthenticated `/healthz` 200 responses on the other local ports only establish liveness, not shared state or authenticated readiness.

The 18124 Security screen also showed one active extension-installation kill switch with reason `QA-004 adapter quarantine acceptance probe`. The reason and target identify a pre-existing QA safety switch. It was preserved and not cleared; this does not represent a new defect.

## Evidence

- Visually inspected [18128 Operations overview](../evidence/CP104-operations-overview-ui.png), [18128 cluster API response](../evidence/CP104-ops-cluster-readback.json), and [18124 Operations overview](../evidence/CP104-runtime-18124-operations.png).
- No Tanpopo source was changed. No new defect was assigned.

## Coverage

- Operations health/readiness on two isolated SQLite runtimes: **PASS (scoped)**.
- Authenticated status on 18121: **BLOCKED** by stale session; refreshing credentials was not in this limited inventory step.
- PostgreSQL runtime, HA/failover, backup/restore, node-loss, and recovery: **UNVERIFIED** here (the PostgreSQL slice is CP105).

QA continues. **GATE-REL-07 remains BLOCKED.**
