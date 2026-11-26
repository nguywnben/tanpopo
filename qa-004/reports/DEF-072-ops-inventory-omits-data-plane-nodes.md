# DEF-072 — Operations cluster inventory omits split data-plane nodes

**Severity:** Major
**Status:** FAIL — confirmed on exact RC6
**Affected workflow:** Operations health/epoch monitoring, split control-plane/data-plane deployment, Master KEK rotation and retirement readiness

## Summary

`GET /admin/v1/ops/cluster-status` and the real Operations → Health & Cluster screen return a single healthy `primary` row when a separate, ready `--role=data-plane` process is running against the same PostgreSQL database and Valkey cluster. The endpoint does not expose that data-plane node’s health or configuration epoch.

This leaves operators unable to establish the documented all-data-plane-node epoch condition for key rotation and retirement. It does not prove that any node is stale; it proves that the current inventory cannot show whether one is stale or unavailable.

## Reproduction

1. Restore the isolated CP105 PostgreSQL database using the external backup flow documented in [CP111](../checkpoints/CP111-postgresql-logical-backup-and-restore.md).
2. Start exact RC6 artifact run `45581179274` as a control plane on `127.0.0.1:18135`, with the restored PostgreSQL database and isolated three-node Valkey cluster.
3. Start the same artifact as a separate `data-plane` on `127.0.0.1:18136` with the same shared backends.
4. Verify `/readyz` on each listener. Both return HTTP 200.
5. Send unauthenticated `GET /v1/models` to the Data Plane. It reaches the native ingress and correctly returns HTTP 401 `invalid_virtual_key`.
6. Sign in as the synthetic Org Owner/Operator to the control plane, then call authenticated `GET /admin/v1/ops/cluster-status`. It returns HTTP 200 with only `nodes: [{id: "primary", config_epoch: 9, health: "ok", wal_bytes: 0}]`.
7. Open Operations → Health & Cluster. It shows `Ready`, `Cluster nodes 1`, and one `primary` row. There is no data-plane row. The split Data Plane’s admin cluster-status endpoint is not exposed (404).

## Expected versus actual

- **Expected:** the documented cluster-status API and SCR-16 Operations view enumerate each active Data Plane node and its identity, configuration epoch, lag and health. The view indicates an incomplete inventory rather than presenting a local control-plane health row as the full cluster when one or more nodes are unknown.
- **Actual:** the API returns a single row labeled `primary`; the UI accurately renders that API response as one healthy node, despite the independent Data Plane being ready and serving the public ingress guard.

## Evidence

- [CP112 split-role result and API/UI readback](../checkpoints/CP112-split-role-cluster-inventory.md)
- [Runtime and status JSON](../evidence/CP112-split-role-node-inventory.json)
- Visually inspected [Operations UI with one node](../evidence/CP112-split-role-ops-single-node.png)
- [CP111 restore setup](../checkpoints/CP111-postgresql-logical-backup-and-restore.md)

## Root-cause hypothesis

Facts are the split-role reproduction above and the exact-RC6 source behavior. `internal/admin/http/ops_lifecycle.go` chooses a fixed `standalone`/`primary` label from the local storage dialect and returns a one-element `nodes` slice populated with only the local config snapshot and readiness. The likely cause is singleton/local status construction where a cluster-wide node heartbeat/registration inventory is required; the intended registry/heartbeat architecture remains for engineering to confirm.

## Business and operational impact

Operators and release validation cannot verify that all Data Plane instances have applied sensitive configuration changes. In particular, the published DR-03 runbook uses this endpoint to confirm that **100%** of Data Plane nodes have passed a key-rotation epoch before retiring the old key. An omitted node can have unknown readiness or epoch while the Console displays a healthy one-node result. This creates a risk of an unsafe rotation decision, stale behavior, or later decryption/availability failure.

## Acceptance criteria

1. Return a cluster-wide inventory with a stable unique identity, role, config epoch, snapshot-age/lag, and health for every active Data Plane node.
2. Mark inventory incomplete/unhealthy if an expected node is missing, unreachable, or stale; do not equate a healthy local control-plane row with a healthy cluster.
3. Update the Console to present all rows and clear incomplete/stale states. Keep endpoint authorization restricted to the intended Operator role.
4. Add reproducible split-role integration tests covering multiple Data Planes, one stale epoch, one stopped node, recovery/rejoin, and singleton SQLite/PostgreSQL deployments.
5. Demonstrate the DR-03 all-node key-retirement precondition against the live node inventory.

Stale-node behavior, node loss recovery, multi-control-plane election, Valkey failover, load and multi-AZ behavior are not covered by this reproduction. No implementation code was changed. **GATE-REL-07 remains BLOCKED.**
