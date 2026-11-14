# ADR-0002: 3-Tier Isolated Extension Execution Model & `Blind Host Credential Broker`

## Status
**`ACCEPTED`** *(Approved by Product Owner on `2026-11-14` via `GOV-OAS-01` Track B + `CR-02, CR-05`; subject to `COND-PO-01`, `05` and empirical `SPIKE-01`, `SPIKE-02` validation)*

## Date
`2026-11-11`

## Context
A defining differentiator of Tanpopo is allowing operators to install `Provider Adapters` and other extensions from the Marketplace, Git, or private registries without cutting a new Core release (`DEC-002`). However, loading third-party code directly into Core's address space means a single compromised community package could read every plaintext API Key in memory or crash the entire gateway (`RSK-001`, `TP-NFR-0401`). Conversely, forcing authors to write complex compiled code for every straightforward OpenAI-like provider would throttle ecosystem growth (`RSK-005`).

## Decision
1. **Strict Prohibition on Unisolated In-Process Code (`Zero In-Process Untrusted Code`):** Dynamic shared libraries (`plugin.Open`/`dlopen`), host-privileged script interpreters, and unisolated dynamic module imports are unconditionally banned.
2. **Adopt a 3-Tier Isolated Extension Architecture (`SPEC-EXT-01`):**
   - **Tier 1 — `Declarative Adapter` (Static YAML/JSON):** Executed directly by Core's safe, non-Turing-complete mapping interpreter (`<= 32` JSON depth, `<= 10,000` steps — verified in `SPIKE-01` across `9 / 10` representative providers, `POC-02`) with `0` arbitrary code execution surface.
   - **Tier 2 — `Sandboxed Wasm Hooks` (executed on pure-Go `wazero`, default-deny `0` FS/Env/Socket privileges):** Serves providers requiring custom stream framing or bespoke formatting, enforcing a hard **`16 MiB` (`256` Wasm pages)** linear memory cap (large multimodal payloads bypass Wasm via Host `blob_ref` handles per `CR-05`), **Linear-Memory Slice ABI `(ptr, len) -> packed u64`** (`POC-01`, verified at `953.9 ns/op` across `1,000` simultaneous SSE streams in [`spike-results.md`](../spike-results.md)), **`50 ms` chunk / `100 ms` body hard kill watchdogs** (separated from pool queue wait), and **mandatory tainted-module eviction/replacement** whenever `WithCloseOnContextDone(true)` interrupts a module.
   - **Tier 3 — `Isolated Sidecar` (`TP-FR-0308`, scheduled in `v0.3` per `CR-02` — isolated container/process communicating over `ConnectRPC / Protobuf` via `0600` UDS or `mTLS`):** Deployable exclusively by an `OPERATOR` for specialized native C/HSM/ML libraries or `stdio` MCP bridges (`POC-03`).
3. **Mandatory `Blind Host Credential Broker` (`CR-05`):** Tier-1 and Tier-2 extensions never access raw secret bytes (`scoped_secret_unseal` is removed from Tier-2 Wasm); raw secret material or Host-computed `HMAC-SHA256` / `AWS SigV4` / `RSA-JWT` signatures are injected strictly at the Host network egress boundary exclusively for approved `allowedHosts`, with **`Cookie` / `Set-Cookie` credential injection unconditionally banned (`DEC-004`)**.

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
- **Pre-Freeze Gate (`COMPLETED IN M0`):** `SPIKE-01` and `SPIKE-02` completed offline in `tools/spikes/` (`TP-IMP-0004`, documented in [`spike-results.md`](../spike-results.md)), locking `POC-01` and `POC-02` with zero architectural fallback CR required (`COND-PO-05`); end-to-end Gateway latency/memory targets remain `TARGET / UNVERIFIED` per `COND-PO-01` until measured in `TP-IMP-0035` / `0043`.
