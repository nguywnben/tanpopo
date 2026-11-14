# `TP-IMP-0040` — `Valkey` Cluster Coordination (`tp:v1:{org_id}:*` Lua), `< 5s` 3-Layer Cluster Revocation, `> 10s` Stale-Node Circuit Breaker & Role-Split Helm Chart

| Attribute | Value |
|---|---|
| **Unit ID** | `TP-IMP-0040` |
| **Milestone / Epic** | `M4` (`v0.3 Clustered HA, Enterprise Security & A2A`) · `EP-08` |
| **Status** | `BLOCKED` *(Depends on `TP-IMP-0033`, `TP-IMP-0039` — `GAP-01, 06`)* |
| **Size** | `L` (`go.mod`, `internal/cluster/valkey/*`, `internal/config/snapshot/cluster_sync.go`, `internal/cli/admin/verify_state.go`, `deploy/helm/tanpopo/*`) |
| **Review Tier** | **`REQUIRES_STRONG_REVIEW`** *(Zero `CROSSSLOT` errors on sharded clusters, `< 5s` cluster-wide revocation propagation & `> 10s` partitioned-node fail-closed circuit breaking `FMEA-07`)* |
| **Technical Owner** | `Principal Distributed Systems / SRE Architect` |

---

## 1. Intent
Implement the distributed state coordination plane for **Clustered Scale-Out HA (`TP-FR-1005`, `ADR-0006`, `ARCH-OPS-01`, `CR-07`)**:
1. **`Cluster Hash-Tag`-Safe Atomic `Valkey / Redis` Scripts (`tp:v1:{<org_id>}:*` — `CR-07`):** Co-locate all rate-limit keys (`RPM`, `TPM`, `Concurrency`), `Idempotency-Key` locks, and fast budget-hold reservations belonging to the same `Organization` under the `{<org_id>}` hash tag so multi-key `Lua` scripts never trigger `CROSSSLOT` errors on `Valkey Cluster`, paired with `1-second` `valkey_hold_checkpoints` flushes to PostgreSQL and the post-PITR reconciliation command **`tanpopo admin verify-state --check-audit-chain --reconcile-orphaned-holds --rebuild-valkey-state` (`DR-02`)**.
2. **3-Layer Key Revocation & Kill-Switch Propagation Within `< 5 seconds` Cluster-Wide (`SLO-07`, `FMEA-07`, `INC-07`):** Combine (Layer 1) `Valkey Pub/Sub`, (Layer 2) `PostgreSQL LISTEN/NOTIFY`, and (Layer 3) a `2-second` polling loop tailing the canonical **`config_changelog`** table. If any Data Plane node loses connectivity to both PostgreSQL and Valkey for more than **`10 seconds` (`Stale-Snapshot Circuit Breaker`)**, it immediately transitions `/readyz -> 503` and **refuses Virtual Key traffic (`Fail-Closed`)**.
3. **Production Role-Split `Helm Chart` (`--role=data-plane` vs `--role=control-plane`):** Provide `HPA`, `PodDisruptionBudget`, `NetworkPolicy`, and zero-drop 3-node rolling restarts with zero severed SSE streams (`OPS-AC-06`).

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `go.mod`, `go.sum` (adding pure-Go `github.com/valkey-io/valkey-go`).
  - `internal/cluster/valkey/ratelimit_lua.go` & `budget_hold_lua.go` (atomic Lua scripts using `tp:v1:{<org_id>}:rl:*` and `tp:v1:{<org_id>}:budget:*` keys + `valkey_hold_checkpoints` checkpointing + safe PostgreSQL fallback when Valkey degrades).
  - `internal/config/snapshot/cluster_sync.go` (3-layer `config_changelog` synchronizer + `max_staleness = 10s` partitioned-node circuit breaker).
  - `internal/cli/admin/verify_state.go` (`tanpopo admin verify-state --check-audit-chain --reconcile-orphaned-holds --rebuild-valkey-state` supporting `DR-02`).
  - `deploy/helm/tanpopo/*` (`Chart.yaml`, `values.yaml`, dedicated `data-plane` and `control-plane` Deployments, `PDB`, `NetworkPolicy`, `ServiceMonitor` validated with `helm lint` & `kubeconform`).
- **Out-of-Scope:** Sustained `1,000`-SSE-stream soak benchmarking (covered in `TP-IMP-0043` in `M5`).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-1005`, `TP-NFR-0201`, `TP-NFR-0301`, `TP-NFR-0302`, `TP-NFR-0404`, `SLO-01`, `SLO-04`, `SLO-06`, `SLO-07`, `ADR-0006`, `CR-07`, `FMEA-07`, `DR-02`, `OPS-AC-06`, `INC-07`.
- **Dependencies:** `TP-IMP-0033`, `TP-IMP-0039`.
- **Design Refs:** [`ADR-0006`](../../architecture/adr/0006-immutable-config-snapshots-and-fast-revocation.md), [`deployment-topology.md`](../../architecture/deployment-topology.md) (`ARCH-OPS-01`), [`nonfunctional-slo.md`](../../quality/nonfunctional-slo.md) (`FMEA-07`), [`unresolved-gaps.md`](../../reviews/unresolved-gaps.md) (`CR-07`).

## 4. API, Wire & Data Contracts
- **Canonical `Valkey` Keyspace Convention (`CR-07`):** `tp:v1:{<org_id>}:<subsystem>:<entity_id>` — strictly prohibiting any `EVAL`/`MGET` command from touching keys belonging to two distinct `org_id` values in a single call.
- **Degraded Behavior When `Valkey` Fails While `PostgreSQL` Remains Healthy:** Rate limiting falls back to per-node local token buckets divided by estimated replica count (`Local Fallback Quotient`), whereas budget reservations and key-revocation checks fall back directly to `PostgreSQL` so hard budgets are never breached and revoked keys never slip through.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1` (`Cluster-Wide < 5s Revocation SLA`):** Running 3 `data-plane` instances + 1 `control-plane` instance backed by PostgreSQL + Valkey, issuing an emergency revoke of `Virtual Key K1` on `control-plane` causes all 3 `data-plane` nodes to reject `K1` (`401`) and sever active `K1` SSE streams within **`< 1 second`** (and within **`< 3 seconds`** even with `Valkey Pub/Sub` disabled to force fallback propagation).
- **`AC-2` (`FMEA-07` Partitioned Node Fail-Closed > 10s):** Network-partitioning 1 `data-plane` node from both PostgreSQL and Valkey causes that node to transition `/readyz -> 503` and reject authenticated requests after `10 seconds` (`FakeClock` / fault-injection proxy) rather than serving traffic against a stale snapshot.
- **`AC-3` (`CR-07` Valkey Cluster Hash-Slot Safety & `DR-02` `verify-state`):** Running the Lua script suite against a 3-shard `Valkey Cluster` confirms `0` `CROSSSLOT` errors; flushing Valkey and executing `tanpopo admin verify-state --check-audit-chain --reconcile-orphaned-holds --rebuild-valkey-state` reconstructs cluster state from PostgreSQL at `100%`.
- **`AC-4` (`OPS-AC-06` Zero-Drop 3-Node Rolling Restart Under 100 Active SSE Streams):** Under 100 active SSE streams across 3 `data-plane` nodes, performing a sequential rolling restart (`SIGTERM` + graceful drain) produces `0` `502/503` client errors.
- **`NEG-1`:** Rendering and validating the Helm chart with `helm template` + `kubeconform` confirms zero `root` containers and zero overly permissive `NetworkPolicy` rules.

## 6. Test Commands & Required Evidence
- Pure Go (`CGO_ENABLED=0`): `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -v ./internal/cluster/... ./internal/config/snapshot/...` *(PowerShell: `$env:CGO_ENABLED='0'; $env:TANPOPO_TEST_HERMETIC='1'; go test -v ./internal/cluster/... ./internal/config/snapshot/...`)*
- Race detector (`-race`): `TANPOPO_TEST_HERMETIC=1 go test -v -race ./internal/cluster/... ./internal/config/snapshot/...` *(PowerShell: `$env:TANPOPO_TEST_HERMETIC='1'; go test -v -race ./internal/cluster/... ./internal/config/snapshot/...`)*
- `helm lint deploy/helm/tanpopo`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Plaintext `SecretCredential` material and prompt payloads are never written to `Valkey`; production configurations enforce `TLS` on both `Valkey` and `PostgreSQL` connections.
- **Rollback:** Single-node deployments (`--storage=sqlite`) remain completely unaffected by `internal/cluster/valkey`.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0003` (locking the `Valkey {org_id}` keyspace per `CR-07`), `TP-IMP-0033`, and `TP-IMP-0039` are complete.
- **DoD:** 4-node cluster tests, `FMEA-07`, 3-shard `Valkey Cluster` suite, and `helm lint` passing, with `REQUIRES_STRONG_REVIEW` sign-off achieved.
