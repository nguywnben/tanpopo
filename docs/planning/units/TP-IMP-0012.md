# `TP-IMP-0012` — Tier-1 `Declarative Adapter` Runtime Engine & 4-Entity Provider Catalog (`SM-02`)

| Property | Value |
|---|---|
| **Unit ID** | `TP-IMP-0012` |
| **Milestone / Epic** | `M1` (`Foundation & Vertical Slice`) · `EP-02` |
| **Status** | `BLOCKED` *(Depends on `TP-IMP-0004`, `TP-IMP-0010`, `TP-IMP-0011` — `GAP-01, 02`)* |
| **Size** | `L` (`internal/extension/declarative/*`, `internal/provider/catalog/*`, `internal/config/snapshot/*`, `db/migrations/sqlite/0005_providers_models.sql`) |
| **Review Tier** | **`REQUIRES_STRONG_REVIEW`** *(Turing-incomplete expression interpreter defending against ReDoS/OOM & 4-entity catalog boundaries)* |
| **Technical Owner** | `Staff Platform / Runtime Engineer` |

---

## 1. Intent
Implement:
1. **Unambiguous 4-Entity Provider Hierarchy (`SPEC-EXT-02`):** `ProviderAdapter` (transformation package) → `ProviderConnection` (endpoint identity, `allowed_hosts`, `allow_private_ip`, secret slot bindings via `connection_secret_bindings` & canonical `SM-02` state machine) → `LogicalModel` (unified model alias carrying `DECLARED / TESTED / VERIFIED` capability assurance tiers and `micro-USD` pricing tables) → `ModelDeployment` (physical routing target carrying geographic `region`, `idle_chunk_timeout_ms`, and routing weight).
2. **Tier-1 `Declarative Adapter` Runtime Engine:** Implements the shared `extension/port.ProviderRuntime` contract (`TP-IMP-0010`) over the finite non-Turing-complete operator set locked in `POC-02` (`TP-IMP-0004`), targeting `< 2 ms p95` gateway overhead (`SLO-02` `TARGET / UNVERIFIED`) with zero arbitrary code execution surface.

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `internal/provider/catalog/repository.go` (CRUD & queries for `ProviderAdapter`, `ProviderConnection`, `ConnectionSecretBinding`, `LogicalModel`, `ModelDeployment`, appending to `config_changelog`, and implementing `broker.CredentialBindingResolver`).
  - `internal/config/snapshot/providers_view.go` (extend `ConfigSnapshot` with `O(1)` lookup indexes across the 4-entity hierarchy).
  - `internal/provider/catalog/connection_health.go` (drive canonical `SM-02` transitions: `DRAFT -> VALIDATING -> ACTIVE <-> DEGRADED | AUTH_EXPIRED_REAUTH_REQUIRED | SUSPENDED_BY_ADMIN | REVOKED | ARCHIVED` via two-phase `BuildValidationProbe` / `VerifyValidationResult`).
  - `internal/extension/declarative/engine.go` (implement `extension/port.ProviderRuntime` with hard caps of JSON nesting depth `<= 32`, `<= 256` operator steps per message, and zero backtracking `ReDoS` regexes).
  - `db/migrations/sqlite/0005_providers_models.sql` (`provider_adapters`, `provider_connections` enforcing `CHECK (billing_mode IN ('COMMERCIAL_API', 'SELF_HOSTED_INFRA'))`, `connection_secret_bindings`, `logical_models`, `model_deployments`).
- **Out-of-Scope:** Tier-2 `Wasm` sandbox pool (owned by `TP-IMP-0013` executing in parallel under `internal/extension/wasm/*`); multi-target route selection & failover state machine (owned by `TP-IMP-0020`).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0201`, `TP-FR-0202`, `TP-FR-0203`, `TP-FR-0301`, `TP-NFR-0101`, `SLO-02`, `TP-BR-0102`, `TP-BR-0401`, `SM-02`, `FZ-01`.
- **Dependencies:** `TP-IMP-0004`, `TP-IMP-0010`, `TP-IMP-0011`.
- **Design Refs:** [`provider-adapter.md`](../../specifications/provider-adapter.md) (`SPEC-EXT-02`), [`data-model.md`](../../architecture/data-model.md) Section 3.2, [`ADR-0005`](../../architecture/adr/0005-two-phase-budget-ledger-and-commercial-billing-separation.md).

## 4. API, Wire & Data Contracts
- **Hard Billing-Mode Boundary (`DEC-004` & `ADR-0005`):** `provider_connections.billing_mode` accepts exclusively `'COMMERCIAL_API'` or `'SELF_HOSTED_INFRA'`, rejecting any consumer-subscription mode at both JSON Schema and SQL `CHECK` layers.
- **Three Capability Assurance Tiers (`Capability Assurance Tier`):** Every capability flag (`streaming`, `tool_calling`, `vision`, `audio_input`, `document_input`, `strict_json_schema`, `reasoning`) carries an explicit tier badge: `DECLARED`, `TESTED`, or `VERIFIED`.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1`:** Given a `Tier-1 Declarative Adapter` `.tpkg` installed via `TP-IMP-0011`, When creating a `ProviderConnection` bound to a `SecretCredential` via `connection_secret_bindings` and registering `LogicalModel` + `ModelDeployment` pointing at `FakeLLMServer` through `BlindBroker` (`TP-IMP-0010`), Then connection validation advances `SM-02` `DRAFT -> VALIDATING -> ACTIVE` and transforms both synchronous payloads and SSE chunks accurately.
- **`AC-2`:** Given a self-hosted LAN model (`vLLM` / `Ollama`) configured with `billing_mode = 'SELF_HOSTED_INFRA'`, `allow_private_ip = true`, and `auth_mode = 'NONE'`, When registering a `ModelDeployment`, Then persistence and `ConfigSnapshot` resolution succeed without requiring a commercial API secret.
- **`NEG-1` (`FZ-01` Malicious Declarative Mapping):** Loading a declarative mapping containing recursive template references, nesting depth `> 32`, or pathological extraction patterns → rejected at load time or safely aborted in `< 1 ms` with zero panics or OOMs.
- **`NEG-2`:** Attempting to create a `ProviderConnection` with `billing_mode = 'CONSUMER_SUBSCRIPTION'` → rejected with `422 invalid_billing_mode` at both API validation and SQL constraint layers.
- **`NEG-3`:** Attempting hard deletion of a `ProviderAdapter` or `ProviderConnection` still referenced by an active `ModelDeployment` → blocked with `409 resource_in_use` (`port.ErrResourceInUse`).

## 6. Test Commands & Required Evidence
- Pure-Go verification (`CGO_ENABLED=0`):
  - POSIX: `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -v ./internal/extension/declarative/... ./internal/provider/catalog/...`
  - PowerShell: `$env:CGO_ENABLED='0'; $env:TANPOPO_TEST_HERMETIC='1'; go test -v ./internal/extension/declarative/... ./internal/provider/catalog/...`
- Data-race (`-race`) & fuzzing (`FZ-01`):
  - `go test -v -race ./internal/extension/declarative/... ./internal/provider/catalog/...`
  - `go test -fuzz=FuzzDeclarativeEngine -fuzztime=30s ./internal/extension/declarative/...`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** The Tier-1 declarative interpreter operates strictly on sanitized `NormalizedRequest` / `UpstreamResponse` payloads — zero filesystem, environment variable, or `vault` operators exist.
- **Rollback:** Additive schema `0005_providers_models.sql`; suspending a connection cleanly moves `SM-02` to `SUSPENDED_BY_ADMIN`.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0001` (`SM-02` remediation), `TP-IMP-0004` (`POC-02` operator lock), `TP-IMP-0010`, and `TP-IMP-0011` are complete.
- **DoD:** Passes unit tests, `FZ-01` fuzzing, in-memory frame transform benchmark `< 0.5 ms/op`, and `REQUIRES_STRONG_REVIEW`.
