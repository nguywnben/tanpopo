# CP112 — Split-role cluster inventory and epoch observability

**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1` (artifact run 45581179274)
**Result:** FAIL — confirmed new Major DEF-072. The authenticated Operations cluster response omits an independently ready data-plane process.

## Reproduction

On the isolated PostgreSQL restore created in CP111, started the exact RC6 artifact with the same PostgreSQL database and three-node Valkey configuration as two split roles:

- Control plane: `127.0.0.1:18135`, `serve --role control-plane --offline`.
- Data plane: `127.0.0.1:18136`, `serve --role data-plane --offline`.

Both listeners returned HTTP 200 from `/readyz`. The data-plane listener also handled `GET /v1/models` at the native ingress boundary and correctly rejected a request without a Virtual Key with HTTP 401 `invalid_virtual_key`. The control plane’s authenticated `GET /admin/v1/ops/cluster-status` returned HTTP 200 but only one node: `id: primary`, `config_epoch: 9`, `health: ok`. The real Operations → Health & Cluster page displayed **Cluster nodes 1** and only that `primary` row. The data-plane process did not appear. Its admin cluster-status endpoint returned 404, as the split role does not expose the admin Console.

The observed one-node result persists with two distinct ready processes. This is reproducible against both the authenticated API and the real Console; the screenshots were visually inspected.

## Contract and impact

The OpenAPI summary for `GET /admin/v1/ops/cluster-status` says it inspects Data Plane node `ConfigSnapshot` epoch lag. SCR-16 says Operations monitors per-node epoch lag. The key-retirement runbook explicitly requires confirming that **100% of active Data Plane nodes** have reached a rotation epoch before retiring the old KEK.

With a healthy control-plane-local row and an omitted data-plane node, an operator cannot establish the required all-node condition. The UI can present a healthy one-node table while the independent Data Plane’s epoch and health are unknown. This undermines key-cache eviction verification and safe cluster operations. The issue does not establish that a Data Plane node is stale, only that this endpoint and Console do not report it.

## Facts and root-cause hypothesis

**Observed facts:** both split-role listeners were ready; the data-plane native ingress was reachable and enforced authentication; the control-plane API and UI returned one healthy `primary` row; the Data Plane did not appear in either inventory.

**Source fact:** `internal/admin/http/ops_lifecycle.go` assigns a fixed node label from the storage dialect (`primary` for PostgreSQL) and constructs `nodes` as a one-element array using only the local snapshot and local readiness check.

**Root-cause hypothesis:** the endpoint was implemented as a local/singleton snapshot even though its API, SCR-16 and operations runbook describe a cluster-wide Data Plane inventory. Confirm through the intended node-registration/heartbeat design during engineering remediation.

## Acceptance criteria

1. In a supported split control-plane/data-plane deployment, the authenticated cluster-status API and Operations UI enumerate every active node with a stable distinct identity, role, config epoch, snapshot lag and health.
2. Missing, unreachable, stale, or unregistered Data Plane nodes are surfaced as incomplete/unhealthy inventory. A local control-plane snapshot must not imply that all Data Plane nodes are ready.
3. The rotation/retirement gate can verify 100% of active Data Plane nodes at or beyond the required epoch; tests cover a healthy second node, a stale node, a stopped node, and recovery after it rejoins.
4. Singleton SQLite and PostgreSQL deployments retain correct, clearly labeled behavior and the endpoint remains appropriately restricted to Operators.

## Evidence and limits

- [Sanitized split-role runtime and cluster response](../evidence/CP112-split-role-node-inventory.json), with both ready probes, expected unauthenticated native ingress rejection, control-plane cluster-status result, and real UI text.
- Visually inspected [Operations UI showing one node](../evidence/CP112-split-role-ops-single-node.png).
- Contract references: `openapi/admin-v1.yaml` operation summary for `/admin/v1/ops/cluster-status`, `docs/ux/screen-specifications.md` SCR-16, and `docs/operations/backups-recovery.md` DR-03.

No data-plane lag or outage was manufactured; therefore behavior when a node is actually stale is still UNVERIFIED. Valkey application-level rate-limit and failover behavior, multi-control-plane election, active streams, load, and real multi-AZ fault domains remain UNVERIFIED. No product implementation was modified. QA continues; **GATE-REL-07 remains BLOCKED.**
