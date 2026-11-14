# ADR-0001: Select Go (`1.24+`) Role-Separable Modular Monolith for Tanpopo Core & CLI

## Status
**`PROPOSED`** *(Awaiting Product Owner approval at Gate G3)*

## Date
`2026-11-11`

## Context
Tanpopo must simultaneously satisfy three architectural constraints:
1. Ultra-lean single-node self-hosted ergonomics (`TP-FR-1001`, `TP-NFR-0103`): startup in `< 3 seconds`, idle RAM `< 128 MB`, and distribution as a single cross-platform executable (`linux/amd64`, `linux/arm64`, `darwin/arm64`, `windows/amd64` — `TP-NFR-0901`).
2. High-throughput hot-path performance (`TP-NFR-0101`..`0102`, `TP-NFR-0301`): `< 2 ms` (`p95`) proxy overhead for Declarative Bundles, `< 1 ms` (`p95`) per-SSE-chunk forwarding latency, and `>= 1,000` concurrent SSE streams within `2 vCPU, 2 GB RAM`.
3. Rich multi-plane domain capabilities (`Unified/Native LLM`, `Plugin Sandbox`, `MCP Hub`, `A2A`, `OAuth 2.1`, `FinOps Ledger`, `Admin API`, `CLI`) that still scale horizontally without state affinity in enterprise clusters (`TP-FR-1005`).

## Decision
1. Adopt **Go (`1.24+`, statically compiled with `CGO_ENABLED=0`)** as the sole implementation language for **Tanpopo Core (`tanpopod`)** and the **`tanpopo` CLI**.
2. Structure the codebase as a **Plane-Separated Modular Monolith**: a single repository enforcing strict unidirectional internal package boundaries (`ingress`, `iam`, `policy`, `routing`, `finops`, `extension`, `security`, `telemetry`, `storage`), compiled into a single binary supporting `--role=all` (standalone default), `--role=data-plane`, and `--role=control-plane` (for HA cluster deployments).

## Alternatives Considered

### Option B: Rust (`Tokio` + `Axum` + `Wasmtime`)
- *Pros:* Maximum raw throughput, zero garbage-collection (GC) pauses, and fastest native Wasm Component Model runtime.
- *Cons:* Substantially slower development velocity across complex Control Plane workflows (OAuth 2.1, RBAC, Admin CRUD, MCP/A2A coordination, financial reconciliation); higher barrier to open-source community contributions; and more complex C++ static linking during multi-OS cross-compilation compared to Go's `CGO_ENABLED=0`.
- *Why not selected:* Velocity cost across `v0.1`–`v0.3` is disproportionately high when Go comfortably clears the `< 2 ms` (`p95`) latency target.

### Option C: Full-Stack TypeScript (`Node.js 22+` or `Bun`)
- *Pros:* 100% language uniformity with the Web Console; abundant web and MCP SDK ecosystem.
- *Cons:* Single-threaded event loop suffers `p99` tail-latency spikes when parsing large multimodal JSON payloads concurrently with thousands of active SSE streams; standalone single-executable bundles are heavy (`~90–120 MB`), straining the `< 128 MB` idle RAM target; and creates dangerous temptation to load untrusted JS plugins directly in-process.
- *Why not selected:* Inferior fit for lean self-hosted infrastructure packaging and predictable tail latency under heavy streaming load.

### Option D: Python (`FastAPI` / `uvloop`) or Upfront Multi-Service Microservices
- *Cons:* Python's interpreter overhead and GIL bottleneck proxy throughput under load; decomposing into microservices at `v0.1` forces every self-hoster to operate an unnecessary fleet of internal network services.
- *Why rejected:* Directly violates both hot-path latency targets and pragmatic Day-1 self-hosting ergonomics.

## Rationale
Go with `CGO_ENABLED=0` (paired with the pure-Go `wazero` WebAssembly runtime and a pure-Go SQLite driver) achieves the highest weighted score (`9.05/10` in `system-design.md`): it compiles to a single static binary that runs everywhere while embedding Web Console static assets via `embed.FS`, multiplexes thousands of SSE connections over lightweight goroutines, and remains highly readable, maintainable, and approachable for open-source infrastructure contributors.

## Consequences
- **Positive:** Single-binary installation; small memory footprint; clean `--role=data-plane` / `--role=control-plane` separation in HA mode without maintaining fragmented microservice repositories.
- **Negative / Trade-offs:** Introduces a two-language boundary inside the monorepo (`Go` for Backend/CLI and `TypeScript` for the Web Console + one Plugin SDK target); engineers must enforce allocation discipline on the hot path (`sync.Pool`) to prevent GC tail pauses.

## Reversibility & Migration Implications
- **Reversibility:** **Hard to reverse (*One-Way Door*)** once `v0.1` Core is implemented. However, because both the `Extension Contract` (`SPEC-EXT-01`) and external `API Contracts` (`SPEC-API-01`) are strictly language-agnostic, community extension packages and the Web Console remain unaffected even if internal Core components evolve.
- **Current Repository Migration Impact:** Zero files are moved during Phase A05; once implementation is explicitly approved, initialize `go.mod` and establish clean `cmd/`, `internal/`, and `web/console/` directories.
