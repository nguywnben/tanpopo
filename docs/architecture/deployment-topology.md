# Deployment Topology, Scale, High Availability & SRE Architecture — Tanpopo

> **Infrastructure & SRE Architecture Document (Phase ASTRA A05 — Gate G3: `READY_FOR_REVIEW` | `PROPOSED — PRE-APPROVAL`)**
> - **Document ID:** `ARCH-OPS-01` | **Version:** `0.1.0-step05-draft` | **Last Updated:** `2026-11-11`
> - **Authoring Roles:** Principal SRE & Platform Architect
> - **Governance Status:** **Proposed Infrastructure Design (`PROPOSED`)** — coupled to [`ADR-0001`](./adr/0001-core-runtime-and-modular-monolith.md), [`ADR-0003`](./adr/0003-dual-mode-persistence-and-cluster-state.md), and [`ADR-0006`](./adr/0006-immutable-config-snapshots-and-fast-revocation.md).
> - **Requirements Traceability:** `TP-FR-0702`, `TP-FR-0705`, `TP-FR-1001`..`1005`, `TP-NFR-0101`..`0302`, `TP-NFR-0404`, `TP-NFR-0601`, `TP-NFR-0602`, `TP-NFR-0901`.

---

## 1. Three Standardized Deployment Topologies (`Progressive Self-Hosted Topologies`)

Tanpopo's architecture lets an engineering team start with a single command on a laptop or VPS and progressively scale into a multi-AZ high-availability cluster (`>= 99.9%`) **without changing API contracts, rewriting GitOps manifests, or repackaging Plugins**:

| Deployment Mode | Target Audience | Active Runtime Components | Resource Footprint & Availability (*Target Goals*) |
|---|---|---|---|
| **Mode A: `Standalone Zero-Dependency`** *(Default `v0.1`)* | Solo Developers, AI Power Users, small evaluation teams, or lightweight edge/on-prem setups. | **1 `tanpopod` binary (or 1 Docker container)** embedding Web Console SPA + SQLite WAL + In-RAM counters + Secret Vault wrapped by keyfile/env `TANPOPO_MASTER_KEY`. | Idle RAM `< 128 MB`, ready to serve in `< 3 seconds` (`TP-NFR-0103`). Fits `<= 200–500` concurrent SSE streams on a single host. |
| **Mode B: `Team Compose Stack`** *(Recommended mid-scale production)* | Dev Teams (10–100 engineers) running on 1–2 internal or cloud VMs. | `docker compose up -d`: `2x tanpopod` (behind Caddy/Nginx/Traefik) + `PostgreSQL 16` + `Valkey 8` + optional `OTel Collector`. | Survives single-container restarts with zero service interruption; sustains `>= 1,000` concurrent SSE streams/node (`TP-NFR-0301`). |
| **Mode C: `Clustered Multi-AZ HA`** *(Enterprise standard `v0.3`/`v1.0`)* | AI Platform Teams (50–500+ engineers) running continuous SLA-backed production traffic. | Kubernetes cluster: split `tanpopo-dataplane` Deployment (stateless `HPA 3..N pods`) + `tanpopo-controlplane` (`2 pods`) + `PgBouncer` + `PostgreSQL Multi-AZ` + `Valkey Sentinel/Cluster` + `Cloud KMS / Vault` + `Isolated Sidecar Pods` (when Tier-3 is enabled). | Target Gateway-tier availability **`>= 99.9%`** (`TP-NFR-0201`), linear horizontal scaling with zero `sticky sessions` (`TP-FR-1005`). |

### High-Availability Cluster Topology Diagram (`Mode C — Multi-AZ Production HA`)

```mermaid
flowchart TB
    subgraph Edge["Edge & Load-Balancing Tier"]
        LB["L7 Load Balancer / Ingress (TLS 1.3, HTTP/2, SSE Buffering OFF)"]
        AdminLB["Internal Admin Ingress (mTLS / OIDC Protected)"]
    end

    subgraph K8sAZ["Multi-AZ Stateless Execution Cluster (AZ-1 & AZ-2)"]
        subgraph DPPods["Deployment: tanpopo-dataplane (--role=data-plane, HPA 3..N)"]
            DP1["Data Plane Pod #1 (RAM Snapshot + Wasm Pool)"]
            DP2["Data Plane Pod #2 (RAM Snapshot + Wasm Pool)"]
            DPN["Data Plane Pod #N (RAM Snapshot + Wasm Pool)"]
        end

        subgraph CPPods["Deployment: tanpopo-controlplane (--role=control-plane, 2 Replicas)"]
            CP1["Control Plane Pod #1 (Admin API + Leader Jobs + Embedded Console)"]
            CP2["Control Plane Pod #2 (Admin API + Standby)"]
        end
    end

    subgraph SharedState["Distributed State & Security Tier"]
        Valkey["Valkey / Redis HA (Rate Limits, Budget Holds, Fast Revocation Pub/Sub)"]
        PgPool["PgBouncer Transaction Pooler"]
        PG["PostgreSQL 16+ Primary + Sync/Async Standby (Multi-AZ)"]
        ObjStore["Shared Bundle Store (OCI Registry / S3 for .tpkg/.wasm bundles)"]
        KMS["Cloud KMS / HashiCorp Vault (Master KEK)"]
    end

    LB --> DP1
    LB --> DP2
    LB --> DPN
    AdminLB --> CP1
    AdminLB --> CP2

    DP1 <-->|"Lua RateLimit/Hold + Pub/Sub"| Valkey
    DP2 <--> Valkey
    DPN <--> Valkey
    CP1 -->|"Broadcast Invalidation < 100ms"| Valkey

    DP1 -->|"Batch Ledger & Audit Writes"| PgPool
    DP2 --> PgPool
    CP1 --> PgPool
    PgPool --> PG

    CP1 <--> ObjStore
    DP1 -->|"Pull Verified Bundle by SHA-256"| ObjStore
    CP1 <--> KMS
    DP1 <-->|"Unwrap Org DEK into Ephemeral RAM"| KMS
```

---

## 2. Immutable Config Snapshots & `< 1s` / `< 5s` Revocation SLA Enforcement (`ADR-0006`)

Executing SQL `SELECT` queries against PostgreSQL on every inference request to inspect `Virtual Key`, `Policy`, `Route`, and `Extension` state would inflate tail latency and overwhelm the database under traffic spikes. Conversely, relying on a naive passive TTL cache (`TTL = 60s`) would allow an attacker to keep exploiting a leaked key for a full minute (`RSK-011`).

Tanpopo resolves this cleanly via **Copy-on-Write `ConfigSnapshot` paired with Dual-Channel Invalidation**:

1. **Zero-Lock Hot Path Reads (`Zero-Lock Hot Path`):**
   - All active entities (`Organizations`, `Projects`, `Environments`, `Virtual Keys` hash tables, `Policies`, `Routes`, `Deployments`, `Extensions` grants, `MCP Tool Hashes`) are pre-compiled into an immutable read-only object graph in RAM accessed via `atomic.Pointer[ConfigSnapshot]`.
   - Every inference goroutine resolves configuration in `< 50 nanoseconds` with `0` `Mutex` contention and `0` SQL round-trips.
2. **Triple-Redundant Emergency Revocation Propagation (`SLA < 1s Standalone` / `< 5s Cluster` — `TP-NFR-0404`):**
   - **Layer 1 — Primary Fast Bus (`~10–100 ms`):** When an administrator triggers `Emergency Revoke` (or `Quarantine`), the Control Plane commits to the DB while incrementing `config_epoch`, then immediately publishes `{ epoch, revoke_type, target_id, emergency: true }` over **Valkey/Redis Pub/Sub**.
   - **Layer 2 — Valkey-Outage Backup (`~50–300 ms`):** Simultaneously broadcasts over **`PostgreSQL LISTEN / NOTIFY`** (`NOTIFY tanpopo_revocations`).
   - **Layer 3 — Hard Safety Backstop (`<= 2 seconds`):** Every Data Plane node runs a lightweight `SELECT max(config_epoch) FROM config_changelog` poll every **`2 seconds`**. Upon detecting a newer `epoch` than its resident snapshot (even if both Pub/Sub and NOTIFY dropped during a transient network blip), the node immediately fetches the configuration `delta`, swaps `ConfigSnapshot`, and invokes `ActiveStreamRegistry.Abort(target_id)` to sever in-flight streams.
   - **Stale-Node Circuit Breaker (`Stale-Node Circuit Breaker`):** If a Data Plane node loses connectivity to both PostgreSQL and Valkey for more than `10 seconds`, it flips `/readyz = 503` and refuses sensitive stream admissions (`Fail-Closed`) rather than serving traffic against a stale revocation list.

---

## 3. Caching Strategy, Distributed Rate-Limiting & DB Connection Governance

### 3.1. Explicit Cache Key & Staleness Matrix (`Explicit Cache Matrix`)

| Cached Artifact | Storage Tier | Tenant-Safe Cache Key (`Tenant-Safe Key`) | Refresh Strategy & Max Tolerated Staleness |
|---|---|---|---|
| **`ConfigSnapshot` (Virtual Keys, Routes, Policies)** | Per-node RAM | Internally partitioned by `org_id -> project_id -> env_id` | Event-driven `Pub/Sub` + `LISTEN/NOTIFY` + `2s` epoch poll. Max staleness: **`< 1s` (standalone) / `< 5s` (cluster)**. |
| **Compiled native `.wasm` modules** | RAM + Read-only scratch disk | `wasm_compiled:<contract_ver>:<bundle_sha256>` | **Strictly immutable** (content-addressed by `SHA-256`); evicted via `LRU` once unreferenced by any active `Installation`. |
| **OIDC `JWKS` public signing keys** | Per-node RAM | `jwks:<issuer_url_sha256>` | `TTL = 15 minutes`, background `Single-Flight` refresh on unknown `kid` (rate-limited to `1/minute` to prevent IdP cache-busting floods). |
| **Unwrapped `Org DEK` / `OAuth Access Token`** | Private memory inside `Blind Broker` only | `dek:<org_id>:<kek_id>` | Short-lived in RAM (`TTL <= 5 minutes`), best-effort zeroized immediately upon connection revocation or `Organization` erasure. |
| **Hard Budget Balance & `destructive` Approval Flags** | **NEVER STALE-CACHED** | Atomic check directly against Ledger / Valkey | **Max staleness = `0`** (never served from passive TTL cache, preventing ceiling overspend or duplicate destructive tool execution). |

### 3.2. Distributed Rate-Limiting & Connection Pool Sizing (`Rate-Limiting & Connection Pools`)

1. **Multi-Dimensional Rate-Limiting Algorithm (`TP-FR-0702`):**
   - Simultaneously enforces three counters per `Virtual Key` (plus `Project` ceilings): **`RPM`** (smooth *GCRA / Sliding Window* avoiding boundary burst doubling), **`TPM`** (pre-debits estimated input tokens on admission, reconciles actual tokens at completion), and **`Max Concurrent Streams`** (atomic counter with self-healing TTL lease preventing leaks on node crash).
   - In HA clusters, all three checks execute inside **a single atomic Lua script on Valkey** (`1` network round-trip `< 0.5 ms`).
2. **Anti-Exhaustion Database Connection Pool Sizing (`Anti-Exhaustion Pool Sizing`):**
   - Data Plane nodes write strictly in asynchronous micro-batches (`AsyncLedgerAndTraceFlusher`) and **never** hold a DB connection while waiting on upstream LLM token generation (`5–60 seconds`!).
   - Hard sizing invariant: `data_plane_replicas × max_open_db_conns_per_pod` must remain below `70%` of PostgreSQL `max_connections`; deployments autoscaling beyond `> 10` replicas must route through **`PgBouncer` (`transaction pooling` mode)**.

---

## 4. Load-Balancer Requirements, Zero-Downtime Rolling Upgrades & Instant Rollback (`SM-08`)

1. **Load-Balancer Configuration for Long-Lived SSE Streams:**
   - Every L7 Reverse Proxy fronting Tanpopo (Nginx, Envoy, Cloudflare, AWS ALB) must enforce: **response buffering disabled (`X-Accel-Buffering: no` emitted automatically by Tanpopo)**, `HTTP/2` enabled client-side with persistent `HTTP Keep-Alive` / `HTTP/2` upstream pools toward Tanpopo, and `proxy_read_timeout >= 300s` to avoid prematurely severing deep-thinking `reasoning models`.
2. **Zero-Downtime Rolling Upgrade Procedure (`Zero-Downtime Rolling Upgrade` — `SM-08`):**
   - **Step 1 (`tanpopo upgrade preflight` — `TP-FR-1004`):** Verifies that every `ENABLED_ACTIVE` row in `extension_installations` has a `contract_version` within the new binary's supported window. Any mismatch halts immediately with `BLOCKED_INCOMPATIBLE` before touching live workloads.
   - **Step 2 (`Backup & Expand Migration`):** Captures a config/ledger snapshot and applies backward-compatible *Expand* SQL migrations.
   - **Step 3 (`Graceful Stream Drain`):** Upgrades Control Plane pods first, then rolls Data Plane pods sequentially: an outgoing pod receives `SIGTERM` → immediately flips `/readyz = 503` (so the Load Balancer stops routing new requests) → allows in-flight SSE streams to complete naturally within `drain_timeout` (default `90 seconds`) → flushes `100%` of queued cost settlements to the DB before exiting.
   - **Step 4 (`Instant Rollback`):** Because transitional schema migrations obey *Expand-Only* discipline and `tanpopo.lock` preserves prior pinned versions, if post-upgrade health checks fail, operators roll back simply by re-pointing the container image tag to the previous release (`< 60 seconds`) without restoring cold database backups.
