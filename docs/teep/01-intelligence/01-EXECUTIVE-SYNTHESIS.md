# 01 — Executive Synthesis: Evolving Tanpopo into a Sovereign Enterprise AI Infrastructure Platform

- **Document ID:** `TEEP-01-DOC-01`
- **Research Execution Date:** `2026-11-27`
- **Evaluated Baseline:** Frozen RC9 `main` (`ac9ae87a68f095203de82968ac5d08729e33a531` — *Unsigned RC9, `BASELINE_STABLE`*)

---

## 1. Strategic Thesis: Why Deferring `v1.0.0` Is the Right Product Decision

Primary-source intelligence gathered on `2026-11-27` across **20+ competing platforms, 8 provider API families, 4 model metadata catalogs (`18,000+` model records), 6 protocol standards (`MCP 2026-07-28`, `A2A v1.0.1`, `OAuth 2.1`/`RFC 8693`, `OTel GenAI`), and every subsystem of Tanpopo RC9 (`344` Go files, `56` SQL tables, `15` Console modules)** validates the Founder's decision to defer `v1.0.0` for the **Enterprise Product Expansion Program (`TEEP`)**.

### 1.1 A Rare Market Window Created by 2026 Consolidation & Security Failures (`[VERIFIED_FACT]`)
1. **Independent Open-Source Attrition:** Within 16 months (`2025-07` to `2026-10`), **Portkey** and **Protect AI** (`protectai/llm-guard` archived `2026-07-09`) were absorbed into Palo Alto Networks **Prisma AIRS**; **TensorZero** shut down and archived read-only (`2026-06-12`); **Helicone** relicensed its Rust gateway to copyleft **`GPL-3.0`** and entered maintenance mode under Mintlify (`2026-03-03`); **Smithery** shut down its hosted MCP proxy (`2026-10-31`, breaking ~218 Official MCP Registry entries); **Langfuse** (ClickHouse), **Promptfoo** (OpenAI), **Lakera** (Check Point), **CalypsoAI** (F5), and **Arize** (Dynatrace, non-OSI `ELv2`) consolidated into larger commercial stacks; and **LiteLLM**, **Bifrost**, **Kong**, **Traefik Hub**, and **Gravitee** locked SSO/SCIM, RBAC, Vault, audit logs, and MCP/token budgets behind proprietary enterprise paywalls ([`02-MARKET-LANDSCAPE.md`](./02-MARKET-LANDSCAPE.md)).
2. **Crown-Jewel Supply-Chain & Sandbox Crisis:** **LiteLLM** suffered two 2026 PyPI/Docker credential-harvesting compromises (`1.82.7/8` Mar 24 & `1.107.0` Oct 21) plus CISA KEV `CVE-2026-40217` (`co_code` RCE) and `GHSA-w4c7-34wj-hqm8` (`CVSS 10.0` master-key read), while **IBM ContextForge** suffered SSRF `CVE-2026-110194` and IDE marketplaces suffered **GlassWorm**. Enterprises urgently want a **memory-safe, single-binary, `100% Apache-2.0`, air-gap-native platform** whose plugin sandbox (`wazero`) and credential broker (`BlindHostCredentialBroker`) make third-party secret theft structurally impossible.
3. **Rapid Mid-2026 Protocol Evolution:** Since Tanpopo's M0 specifications were frozen, OpenAI shifted flagship reasoning+tool workloads (`GPT-5.4+`, `GPT-6.2`) onto `/v1/responses`, Gemini 3/4 enforced multi-turn cryptographic `thoughtSignature` replay, Anthropic split `5m`/`1h` prompt caching and `defer_loading` tool search, **MCP `2026-07-28`** transitioned to **Stateless Streamable HTTP**, and **Linux Foundation A2A `v1.0.1`** standardized `/.well-known/agent-card.json` and `/message:send` ([`05-PROTOCOL-AND-TECHNOLOGY-INTELLIGENCE.md`](./05-PROTOCOL-AND-TECHNOLOGY-INTELLIGENCE.md)).

---

## 2. Candid Assessment of Tanpopo RC9 (`ac9ae87a`): Crown Jewels vs Critical Gaps

### 2.1 Architectural Crown Jewels to `KEEP` (`10` Core Primitives)
Tanpopo RC9 has **already solved** security and financial invariants that competitors cannot easily retrofit:
- Pure-Go (`CGO_ENABLED=0`) static binary + zero-ambient-authority `wazero` (`16 MiB`, `50ms`, `14.57 µs/op`) extension sandbox (`CAP-111`).
- `BlindHostCredentialBroker` (`0` raw secrets in Wasm, `DEC-004` Cookie ban, stream secret scrubber) + connect-time `SSRFGuardDialer` (`CAP-064`, `CAP-065`).
- 3-tier `AES-256-GCM` envelope Vault, PostgreSQL 16 `FORCE ROW LEVEL SECURITY`, gapless `RFC 8785 JCS` audit hash chains, and 128-bit `u128` two-phase FinOps holds + `SM-09` A2A sub-budget carving (`CAP-037`, `CAP-094`).
- Air-gap-first Astryx Neutral Web Console (`0` external requests, `0` `axe` violations, 15 offline locales).

### 2.2 Eight Critical RC9 Gaps That Would Undermine a Premature `v1.0.0` Launch (`[VERIFIED_FACT]`)
Direct source inspection ([`04-CURRENT-TANPOPO-PRODUCT-AUDIT.md`](./04-CURRENT-TANPOPO-PRODUCT-AUDIT.md), [`12-ARCHITECTURAL-DEBT-AND-RESIDUAL-RISKS.md`](./12-ARCHITECTURAL-DEBT-AND-RESIDUAL-RISKS.md)) proved:
1. **Broken Self-Hosted Day-0 Onboarding:** Clean `/setup` sees `available_adapters: []` (skipping quickstart & `FirstKeyDialog`), and source/unsigned builds hit **`403 org_trust_policy_denied`** when creating a connection because `PUT /organizations/{id}/extension-policies` has **zero Console UI and zero CLI command** (masked in CI by test-only `e2e/signer`). Even when signed, first token takes `7` screens, `8` forms, and manual `$/1M` → `nano-USD/token` arithmetic.
2. **Unwired Production Subsystems & `404` OpenAPI Drift:** `rollup.NewAggregator` (`DEF-078`), `vault/kms` (`AWS`/`GCP`/`Vault`, contradicting `README.md:34`), `telemetry/ttl.Cleaner`, `CreateDebugCaptureSession` (`debug_payload_blobs` permanently `0` rows!), `broker.NewWorkloadIdentityExchanger`, `ServerConfig.OIDC`, `ServerConfig.TokenVerifier` (**breaking 100% of remote CLI commands with `401`**, `DEF-004`), `EffectivePolicy.GuardrailHooks`, non-provider `ExtensionKind`s, and `request_timeout_ms` are **unwired at runtime**; `4` OpenAPI Admin routes return `404`; and `17` Admin operations lack Console UI.
3. **Throughput Ceiling (`109.48 req/s` SQLite; `56.86 req/s` 2-vCPU Docker HA):** Caused by `DisableKeepAlives: true` per request in `blind_broker.go:885` and synchronous per-request SQL `Reserve`/`Settle` serialization (`sqlite/store.go:104`).
4. **Multi-Tenant Control-Plane Scaling Wall (`ARCH-OBS-01`):** `18` sequential `O(N_orgs)` RLS loops (notably `7 * N_orgs` queries per pod on every `Snapshot.Refresh` and `2 * N_orgs` on admin `loadBindings` / failed login), `7` missing PG indexes, unpruned `O(M_partitions)` `audit_events` tip scans, and a non-atomic `TX1 + TX2` global quarantine split lacking direct Valkey `publishChange`.
5. **Newly Discovered Vault KEK Retirement Bug (`DEF-072`):** `handleRetireMasterKEK` (`ops_lifecycle.go:196`) runs `SELECT COUNT(*) FROM org_encryption_keys` under `current_org_id=''`, which PG `FORCE RLS` evaluates to **`0` unconditionally**, bypassing `409 org_deks_not_rewrapped`! Meanwhile `TANPOPO_MASTER_KEY_PREVIOUS` mislabels key IDs as `"kek_prev_0"` and `5` ops CLI commands fail on PostgreSQL.
6. **Protocol & Adapter Gaps:** Missing top-level `/v1/messages` & `/v1beta/models/*`, stateful/multi-vendor `/v1/responses`, `/v1/{batches,files,audio,images,ocr,realtime}`, MCP `2026-07-28` stateless HTTP (plus RC9 runs 4 upstream RPCs per `tools/call`), LF A2A `v1.0.1`, and fixes for `aws-bedrock` (`connections.go:798` blocking `AWS_SIGV4` + dropped multimodal blocks) and Ollama `:cloud` residency leaks.
7. **Model Intelligence & Sub-Nano Precision Gap:** `1,664` token-cost fields in `litellm_model_prices.json` (`7,193` models) are fractional at `1 nano-USD/token`, proving Tanpopo must replace `RateCard` v2 with `RateCard` v3 **`nano_usd_per_1m_tokens`** ($10^{-15}$ USD/tok) backed by a 4-layer catalog + discovery engine.
8. **Canonical `.tpkg` Signing Reproducibility (`RR-001`):** Divergent file sets between `builtin.NewCatalog` (`2` files) and `plugin pack` (`10` files), Windows `CRLF` checkout drift, runtime `compress/gzip` Go-version variance, and unpinned `build-wasm.sh` (`omitting aws-bedrock`) block cross-OS deterministic signing.

---

## 3. Six Strategic Pillars for the Expanded Enterprise Platform

| Pillar | Strategic Transformation | Anchor Capabilities & Innovations (`CAP-*` / `INN-*`) |
|---|---|---|
| **1. Frictionless Day-0 to Progressive Enterprise UX** | Collapse first-token setup from `20m` + `curl` hacks to `< 90s` (`HYPOTHESIS / UNVERIFIED`) while achieving `100%` Console/CLI/GitOps lifecycle parity. | Self-activating `/setup` & `tanpopo init` (`CAP-130`, `INN-09`), `100%` OpenAPI ↔ Console parity (`CAP-131`), remote CLI + Terraform + DB GitOps (`CAP-134`, `CAP-157`, `CAP-158`), Signed `SOLUTION_PACK`s (`CAP-160`, `INN-04`). |
| **2. Lossless Universal Multi-Protocol Gateway** | First-party OpenAI, Anthropic, Gemini, MCP (`2025`+`2026-07-28`), and LF A2A `v1.0.1` SDKs work natively with zero silent parameter downgrades. | Top-level `/v1/messages` & `/v1beta/*` (`CAP-002`), full `/v1/responses` & signature continuity (`CAP-003`, `CAP-005`), Batches/Files/Realtime/OCR (`CAP-137`..`141`), Dual-Stack MCP + OBO (`CAP-083`, `CAP-087`), A2A `v1.0.1` (`CAP-091`). |
| **3. 4-Layer Model Intelligence & Exact FinOps** | Combine signed offline catalogs, opt-in sync, BlindBroker account discovery, and sticky overrides with `~30 ns` pod budget leases and `FOCUS 1.1` chargeback. | `RateCard` v3 `nano_usd_per_1m_tokens` (`CAP-016`), Signed Packs + Account Discovery (`CAP-017`, `CAP-018`), Deprecation Advisor (`CAP-021`, `INN-02`), Escrow Leases + Signed Receipts + `FOCUS 1.1` (`CAP-038`..`044`, `INN-06`). |
| **4. `20,000+ RPS` Scale & `10,000+` Org Resilience** | Unlock `20x–100x` (`HYPOTHESIS / UNVERIFIED`) throughput and eliminate `O(N_orgs)` RLS bottlenecks while preserving every security invariant. | Pooled Epoch-Fenced Keep-Alive Broker (`CAP-046`), Group-Commit WAL/PG Batcher (`CAP-047`), Incremental Snapshot Delta (`CAP-048`), `ARCH-OBS-01` 7-Index + `O(1)` Audit Heads (`CAP-078`, `CAP-106`), Dual Prefix/KV Affinity (`CAP-024`, `INN-07`). |
| **5. Enterprise Zero-Trust IAM, Guardrails & Sovereign Privacy** | Substantive organizational governance for global banks, healthcare, and air-gapped defense networks. | 7-tier `org_units` (`CAP-074`), OIDC + SAML 2.0 + `< 5s` SCIM 2.0 (`CAP-069`, `CAP-070`), `cedar-go` ABAC (`CAP-071`), Secretless K8s/SPIFFE WIF (`CAP-072`), Speculative Guardrails (`CAP-060`..`063`), `HPKE` Flight Recorder & GDPR Salt-Shredding (`CAP-147`, `CAP-148`, `INN-08`). |
| **6. 11-Category Community SDK & TUF/OCI Marketplace** | Enable third-party innovation without Core releases, static publishing PATs, or memory-safety compromises. | Unified deterministic `.tpkg` packer (`CAP-117`), `runtime.proto` `v1.1` 11 categories + Go/Rust/TS SDKs (`CAP-112`, `CAP-114`), Keyless Sigstore + TUF + Harbor/ECR OCI `1.1` Marketplace (`CAP-118`..`123`), What-If & Chaos Studio (`CAP-152`, `CAP-154`, `INN-01`, `INN-05`). |

---

## 4. Governance Stop Gate

Per mission rules, `TEEP-01` establishes the complete empirical and architectural intelligence baseline (`161` candidate capabilities in [`13-PRELIMINARY-CAPABILITY-INVENTORY.md`](./13-PRELIMINARY-CAPABILITY-INVENTORY.md) and `16` decision points in [`14-OPEN-QUESTIONS-AND-DECISION-POINTS.md`](./14-OPEN-QUESTIONS-AND-DECISION-POINTS.md)) and **stops cleanly at the TEEP-01 Research Review Gate** ahead of `TEEP-02A` (Master Feature Catalog).
