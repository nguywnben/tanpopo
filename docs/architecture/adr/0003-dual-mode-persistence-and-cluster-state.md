# ADR-0003: Dual-Mode Persistence via Abstract Ports (`SQLite WAL` Standalone & `PostgreSQL + Valkey` HA Cluster)

## Status
**`ACCEPTED`** *(Approved by Product Owner on `2026-11-14` via `GOV-OAS-01` Track B + `CR-06, CR-07`; subject to `COND-PO-01..03`, `05`)*

## Date
`2026-11-11`

## Context
Tanpopo's target adopters span individual engineers and small teams demanding a 60-second zero-external-database launch (`TP-FR-1001`) all the way to enterprise platform teams operating multi-replica stateless clusters at `>= 99.9%` availability (`TP-FR-1005`, `TP-NFR-0201`) backed by an ACID financial ledger and immutable audit logs retained for `>= 365 days` (`TP-NFR-0501`).

## Decision
1. Isolate all data persistence and ephemeral cluster state behind two internal port interfaces (`RelationalStore` and `FastStateCoordinator`).
2. **Default Single-Node Mode (`Standalone Mode` — `v0.1`):** Use **embedded SQLite in `WAL` mode (`busy_timeout=5000`, `synchronous=NORMAL`, `foreign_keys=ON`)** paired with atomic in-process RAM counters and reservation locks.
3. **High-Availability Cluster Mode (`Clustered HA Mode` — `v0.3+`):** Use **PostgreSQL `16+`** (with time-range table partitioning for `usage_settlements`, `request_traces_meta`, and `audit_events`, plus `org_id`-scoped `Row-Level Security`) paired with **Valkey `8+` / Redis `7.2+`** for atomic Lua rate-limiting scripts, low-latency budget holds, and `Pub/Sub` revocation broadcasts.
4. **Zero Mandatory ClickHouse / Kafka in Core:** Rely on incremental SQL rollup tables (`usage_rollups_hourly`) + standard **OpenTelemetry (`OTLP`)** export so large enterprises can route telemetry into their existing analytical data warehouses.

## Alternatives Considered

### Option B: Require PostgreSQL + Redis for Every Installation (Including Local Dev)
- *Pros:* Eliminates SQLite dialect compatibility testing.
- *Cons:* Destroys single-binary evaluation ergonomics (`tanpopo serve`), dramatically increasing Day-1 onboarding friction.

### Option C: Embedded Raft-Replicated SQLite (`rqlite` / `dqlite`) for HA Clusters
- *Cons:* Streaming continuous financial settlement writes and trace metadata through embedded Raft consensus creates write-throughput bottlenecks and far harder disaster-recovery operations compared to standard PostgreSQL tooling that enterprise SRE teams already master.

### Option D: Hardwire ClickHouse + Kafka as Mandatory Log & Ledger Stores
- *Cons:* Pushes minimum RAM requirements into multi-gigabyte territory, turning self-hosting into a heavyweight infrastructure burden (*over-engineering*).

## Rationale
Dual-mode persistence behind clean abstract ports fulfills the promise of *"effortless on Day 1, enterprise-hardened on Day 100"*. By enforcing the six schema portability rules in [`data-model.md`](../data-model.md) (string `UUIDv7` keys, `BIGINT micro-USD` currency, zero business-logic reliance on vendor-specific stored procedures), the cost of maintaining both SQL adapters stays tightly bounded.

## Consequences
- **Positive:** `0` external runtime dependencies in `v0.1`; industry-standard horizontal scale in `v0.3+`; lock-free retention pruning via `DROP PARTITION` on PostgreSQL.
- **Negative / Trade-offs:** Every schema-touching PR must execute automated CI tests against both SQLite and a containerized PostgreSQL instance to catch SQL dialect drift early (`RSK-013`).

## Reversibility & Migration Implications
- **Reversibility:** **Medium** (all SQL queries are encapsulated inside `internal/storage/{sqlite,postgres}`).
- **Cross-Mode Upgrade Tooling:** Ship `tanpopo storage migrate-to-postgres --from sqlite://... --to postgres://...` so teams outgrowing standalone mode can promote full configuration state and historical ledger records into an HA cluster with zero data loss.
