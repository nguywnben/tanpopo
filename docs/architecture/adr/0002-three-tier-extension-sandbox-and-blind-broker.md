# ADR-0002: 3-Tier Isolated Extension Execution Model & `Blind Host Credential Broker`

## Status
**`PROPOSED`** *(Awaiting Product Owner approval at Gate G3 & verification via `SPIKE-01`, `SPIKE-02`)*

## Date
`2026-11-11`

## Context
A defining differentiator of Tanpopo is allowing operators to install `Provider Adapters` and other extensions from the Marketplace, Git, or private registries without cutting a new Core release (`DEC-002`). However, loading third-party code directly into Core's address space means a single compromised community package could read every plaintext API Key in memory or crash the entire gateway (`RSK-001`, `TP-NFR-0401`). Conversely, forcing authors to write complex compiled code for every straightforward OpenAI-like provider would throttle ecosystem growth (`RSK-005`).

## Decision
1. **Strict Prohibition on Unisolated In-Process Code (`Zero In-Process Untrusted Code`):** Dynamic shared libraries (`plugin.Open`/`dlopen`), host-privileged script interpreters, and unisolated dynamic module imports are unconditionally banned.
2. **Adopt a 3-Tier Isolated Extension Architecture (`SPEC-EXT-01`):**
   - **Tier 1 — `Declarative Adapter` (Static YAML/JSON):** Executed directly by Core's safe, non-Turing-complete mapping interpreter for `~75–80%` of standard providers (`0` arbitrary code execution surface).
   - **Tier 2 — `Sandboxed Wasm Hooks` (executed on pure-Go `wazero`, default-deny `0` FS/Env/Socket privileges):** Serves `~15–20%` of providers requiring custom stream framing or bespoke request signing, enforcing hard CPU fuel/epoch deadlines (`fuel/epoch`) and linear memory caps (`16–64 MB`).
   - **Tier 3 — `Isolated Sidecar` (isolated container/process communicating over `ConnectRPC / Protobuf` via UDS or `mTLS`):** Deployable exclusively by an `Operator` for the `~5%` of workloads requiring native C/HSM/ML libraries or `stdio` MCP bridges.
3. **Mandatory `Blind Host Credential Broker`:** Extensions never access the master secret vault; for all standard authentication schemes and Host-supported HMAC signers, raw secret material is injected or signed strictly at the Host network egress boundary and exclusively for explicitly approved `allowedHosts`.

## Alternatives Considered

### Option B: In-Process Dynamic Libraries (`Go plugin` / `dlopen` / In-process JS VM)
- *Why rejected outright:* Completely destroys the security boundary (`TP-NFR-0401`), prevents clean memory reclamation during version rollbacks, crashes the entire Gateway process if a plugin panics, and breaks cross-platform portability (`Go plugin` lacks full Windows and static-build support).

### Option C: Dedicated OS Subprocess per Extension (`go-plugin` Subprocess for 100% of Adapters)
- *Cons:* Installing 20 Provider Adapters spawns 20 background OS processes consuming hundreds of MB of RAM on a developer laptop; enforcing uniform network sandboxing on non-containerized Windows/macOS hosts is fragile; and distributing per-OS/CPU native binaries from Git is operationally heavy.
- *Why not selected:* Inferior to Wasm for single-artifact cross-platform portability and far heavier than Tier 1 for standard providers.

### Option D: YAML-Only Tier 1 Declarative Engine (No Wasm or Sidecar)
- *Cons:* Cannot accommodate enterprise-internal AI gateways with proprietary envelope framing or custom cryptographic signatures, forcing teams back into forking Core source code.

## Rationale
The 3-tier model aligns execution cost and blast radius directly with integration complexity: `0` malware execution risk and `< 2 ms` overhead for the majority of standard providers (Tier 1); single-file cross-platform `.wasm` portability with deterministic resource caps for custom wire protocols (Tier 2); and an explicit out-of-process escape hatch for specialized native integrations (Tier 3), paired with the `Blind Broker` to eliminate credential exfiltration by design.

## Consequences
- **Positive:** Adding new AI providers requires zero Core recompilation or process restarts; raw upstream API keys remain protected even if JSON transformation logic inside a `.wasm` guest contains a bug or vulnerability.
- **Negative / Trade-offs:** The platform team must build and maintain the golden contract test runner (`Cassette Harness`) and empirically validate the Wasm SSE chunk-streaming ABI (`POC-01` / `SPIKE-02`).

## Reversibility & Migration Implications
- **Reversibility:** **Medium during `0.x`** (`N-1` contract compatibility window), transitioning to **Hard from `v1.0` onward** (`>= 12-month` compatibility commitment).
- **Pre-Freeze Gate:** `SPIKE-01` and `SPIKE-02` must complete after Product Owner authorization prior to freezing the Wasm linear-memory ABI (`POC-01`).
