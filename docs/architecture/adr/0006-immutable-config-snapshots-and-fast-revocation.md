# ADR-0006: Immutable Copy-on-Write Config Snapshots, Dual-Channel Fast Revocation & Tiered Failure Posture

## Status
**`ACCEPTED`** *(Approved by Product Owner on `2026-11-14` via `GOV-OAS-01` Track A; `< 1s` / `< 5s` revocation SLAs remain `TARGET / UNVERIFIED` until verified under load per `COND-PO-01, 05`)*

## Date
`2026-11-11`

## Context
On Tanpopo's inference hot path, every request must evaluate multiple configuration domains (`Virtual Key`, `Org/Project/Env`, `Policy`, `Route`, `Model Deployment`, `Extension Permissions`, `MCP Schema Hash`) within a combined sub-millisecond budget (`< 1 ms`). Simultaneously, security requirement `TP-NFR-0404` mandates that emergency revocation of a leaked key or quarantine of a malicious extension must take effect in **`< 1 second`** in standalone mode and **`< 5 seconds`** cluster-wide—including immediate termination of active in-flight SSE streams (`BA-OQ-003`, `RSK-011`).

## Decision
1. **Immutable In-RAM Copy-on-Write Snapshot (`Immutable Copy-on-Write ConfigSnapshot`):** Every Data Plane node holds the entire active configuration graph in an immutable read-only structure accessed lock-free via `atomic.Pointer[ConfigSnapshot]`, paired with an `ActiveStreamRegistry` mapping active keys, connections, and plugins to stream `context.CancelFunc` handles.
2. **Triple-Redundant Revocation Propagation (`Triple-Redundant Revocation Propagation`):**
   - *Layer 1:* **`Valkey/Redis Pub/Sub`** fast bus (`< 100 ms`).
   - *Layer 2:* **`PostgreSQL LISTEN/NOTIFY`** direct primary-store fallback when Valkey is degraded (`< 300 ms`).
   - *Layer 3:* Lightweight **`2-second`** `config_epoch` heartbeat poll paired with a self-isolating stale-node circuit breaker if disconnected for `> 10 seconds`.
3. **Explicit Tiered Failure Posture (`Tiered Failure Posture`):**
   - **Strict `FAIL_CLOSED`:** Authentication, tenant isolation, `Data Residency`, extension permissions, hard budget ceilings, and `destructive` MCP tool approvals.
   - **Bounded `FAIL_OPEN`:** External `OpenTelemetry` export (drop-oldest bounded ring buffer with dropped-batch metrics) and graceful degradation of `RPM/TPM` rate counters to conservative per-node local limits (`limit / N_nodes`) during transient Valkey outages.

## Alternatives Considered

### Option B: Direct PostgreSQL / Valkey Lookups on Every Request (No RAM Snapshot)
- *Cons:* Incurs multiple network round-trips per request, jeopardizing the `< 2 ms` (`p95`) overhead budget and turning the database into a single point of contention under traffic bursts.

### Option C: Passive Local Time-to-Live Caching (`Passive TTL Cache = 30–60 seconds`)
- *Cons:* Directly violates `TP-NFR-0404` (`< 5 seconds`) and leaves already-open SSE streams running unchecked after a credential compromise.

## Rationale
Pairing lock-free `ConfigSnapshot` reads with dual-channel push invalidation + a `2-second` epoch poll delivers static-compiled read speed while guaranteeing sub-`5-second` cluster-wide emergency revocation even if an intermediate message bus fails.

## Consequences
- **Positive:** Near-zero configuration lookup latency (`< 0.1 ms`), complete shielding of the relational database from Data Plane read amplification, and deterministic enforcement of emergency revocation SLAs.
- **Negative / Trade-offs:** Per-node RAM usage scales with active configuration size (for `10,000` virtual keys and `1,000` routes, the resident snapshot consumes `~15–25 MB` RAM—well within budget).

## Reversibility & Migration Implications
- **Reversibility:** **Easy to reverse internally** because the mechanism lives entirely inside the Control Plane–to–Data Plane synchronization boundary with zero impact on external APIs or persistent schemas.
