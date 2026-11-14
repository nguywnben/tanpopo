# `TP-IMP-0003` — Spec Closure Batch C: Complete DDL for `11` Missing Tables, `Project DEK`, `(org_id, id)`/`FORCE RLS`, `Audit` Hash-Chain & `Valkey {org_id}`

| Property | Value |
|---|---|
| **Unit ID** | `TP-IMP-0003` |
| **Milestone / Epic** | `M0` (`Spec Closure & Spikes`) · `EP-00` |
| **Status** | **`DONE`** *(Completed & verified on `2026-11-14` under Product Owner Stage 1 M0 approval `DEC-009` — closed `GAP-04, 05, 06`)* |
| **Size** | `M` (`8` data-architecture, security & ops files + reference SQL schemas in `db/spec-check/*`) |
| **Review Tier** | **`REQUIRES_STRONG_REVIEW`** *(Touches envelope encryption hierarchy, multi-tenant RLS isolation & monetary ledger)* |
| **Technical Owner** | `Principal Data Architect + Cryptography/Security Lead` |

---

## 1. Intent
Close every database, cryptographic, and distributed-cache gap identified in Audit A08 (`AUD-003`, `004`, `010`, `026`, `029`) so `ARCH-DATA-01` and `ARCH-OPS-01` achieve `100%` structural parity across `SQLite WAL` and `PostgreSQL 16+` without requiring any storage engineer to invent table names, composite keys, or `AEAD AAD` binding formulas.

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - Reference SQL files for automated syntax/schema validation: `db/spec-check/sqlite_schema.sql`, `db/spec-check/postgres_schema.sql`.
  - Architecture, security & operations docs: `docs/architecture/data-model.md`, `docs/architecture/deployment-topology.md`, `docs/architecture/system-design.md`, `docs/security/secrets-and-tenant-isolation.md`, `docs/security/plugin-sandbox-and-supply-chain.md`, `docs/operations/backups-recovery.md` (update in-memory `DEK` cache eviction in `DR-03`).
  - Traceability & gap log: `docs/requirements/traceability.md`, `docs/reviews/unresolved-gaps.md`.
- **Out-of-Scope:** Executable Go migration runner code (owned by `TP-IMP-0007` and `TP-IMP-0039`).

## 3. Requirements, Dependencies & Design References
- **Requirements & Gaps Refs:** `GAP-04`, `GAP-05`, `GAP-06`; `TP-FR-0204`, `0501`, `0704`, `0801`, `0802`, `1003`, `1005`, `TP-NFR-0402`, `0404`, `0501`, `0502`.
- **Dependencies:** `TP-IMP-0001`, `TP-IMP-0002`.
- **Design / Audit Refs:** [`unresolved-gaps.md`](../../reviews/unresolved-gaps.md) (`CR-06`, `CR-07`), [`ADR-0003`](../../architecture/adr/0003-dual-mode-persistence-and-cluster-state.md), [`ADR-0005`](../../architecture/adr/0005-two-phase-budget-ledger-and-commercial-billing-separation.md).

## 4. API, Wire & Data Contracts
- **Complete DDL for All 11 Missing Tables (`26` Canonical Tables System-Wide):**
  `config_changelog`, `distributed_leases`, `usage_rollups_hourly`, `oauth_authorization_sessions`, `connection_secret_bindings`, `org_extension_policies`, `extension_revocations`, `a2a_tasks`, `webhook_subscriptions`, `project_encryption_keys`, and `valkey_hold_checkpoints` (`1s` periodic budget-hold checkpoint table syncing Valkey holds to PostgreSQL per `CR-07` with `(org_id, ledger_id, node_id, active_hold_micro_usd, updated_at_ms)`).
- **`UUIDv7` Identifier Normalization & Multi-Tenant Constraints:**
  - Primary key IDs use `<prefix>_<26_chars_crockford_base32_uuidv7>` (total length `<= 32` characters, fitting cleanly inside `VARCHAR(40)`).
  - Permit `extension_installations.active_version_id` to be `NULL` while awaiting capability approval; permit `approval_requests.org_id` / `project_id` to be `NULL` for `Instance`-scoped actions; add a partial unique index `WHERE org_id IS NULL` on `principals`.
  - Enforce composite `(org_id, id)` keys on every tenant-owned child table paired with `ENABLE` + `FORCE ROW LEVEL SECURITY`.
  - Add `version BIGINT` to `environments`, `mcp_tools`, `agent_endpoints`, `budget_ledgers`, and `approval_requests`; `owner_project_id` (nullable for `Org`-level secrets) on `secret_credentials`; `owner_principal_id` & `allowed_models_json` on `virtual_keys`; `idle_chunk_timeout_ms` on `model_deployments`; `waterfall_stages_json` on `request_traces_meta`; `project_id` on `debug_payload_blobs`; standardize timestamps on `BIGINT epoch_ms`.
- **Cryptography, Ledger & Audit Hash-Chain Normalization (`CR-06, 07`):**
  - **2 Canonical `AEAD AAD` Formulas Only:**
    1. `Org`-scoped secret (`Org DEK`): `"tanpopo:v1:cred:" + org_id + ":" + credential_id + ":" + secret_kind`.
    2. `Project`-scoped secret/debug-blob/cache-entry (`Project DEK`): `"tanpopo:v1:proj:" + org_id + ":" + project_id + ":" + record_kind + ":" + record_id`.
  - **Virtual Keys (`Virtual Key`):** Prefix `vk_tp_<env>_<32_base62>`, storing raw `SHA-256(rawKey)` digests in `active_key_hash` and `previous_key_hash`.
  - **Audit Hash-Chain (`audit_events`):** Independent hash chain per `org_id` **plus 1 dedicated `SYSTEM` chain for instance-level events where `org_id IS NULL`** (e.g., `Bootstrap` or `LOCAL_BREAK_GLASS_OWNER_RESET`), hashed over `RFC 8785 JCS(metadata_json)`.
  - **Valkey Cluster:** Key schema `tp:v1:{<org_id>}:...` mandating `{<org_id>}` hash-tag braces.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1`:** Given `db/spec-check/sqlite_schema.sql` and `db/spec-check/postgres_schema.sql`, When cross-checked against every table referenced in `data-model.md`, `system-design.md`, `deployment-topology.md`, `screen-specifications.md`, and `TP-IMP-0007..0042`, Then all `26/26` canonical tables match character-for-character (`0` orphan tables).
- **`AC-2`:** Given the 2 `AEAD AAD` formulas and `vk_tp_` key format, When verified across `data-model.md`, `system-design.md`, and `secrets-and-tenant-isolation.md`, Then `100%` byte-for-byte parity holds.
- **`AC-3`:** Given single-`Project` crypto-shredding (`OPS-AC-09`) and `DR-03`, When destroying the target row in `project_encryption_keys` and purging RAM caches, Then all project-scoped secrets and `debug_payload_blobs` in historical backups become permanently undecryptable without impacting sibling projects.
- **`NEG-1`:** Zero multi-key Valkey Lua scripts may omit the `{<org_id>}` hash-tag braces.
- **`NEG-2`:** `Org A`'s `audit_events` hash chain must never depend on hidden rows from `Org B` or `SYSTEM` events, preserving full chain verifiability under `FORCE RLS`.
- **`NEG-3`:** Never permit duplicate `usage_settlements` rows for the same `request_id` (`UNIQUE (request_id, settled_at)`).

## 6. Test Commands & Required Evidence
- Cross-platform Node.js table-presence and formula consistency check (complemented by live SQLite/PostgreSQL DDL execution in `TP-IMP-0007` and `TP-IMP-0039`):
  - `node -e "const fs=require('fs');['db/spec-check/sqlite_schema.sql','db/spec-check/postgres_schema.sql'].forEach(f=>{const s=fs.readFileSync(f,'utf8');['config_changelog','distributed_leases','usage_rollups_hourly','oauth_authorization_sessions','connection_secret_bindings','org_extension_policies','extension_revocations','a2a_tasks','webhook_subscriptions','project_encryption_keys','valkey_hold_checkpoints'].forEach(t=>{if(!s.includes(t))throw new Error(f+' missing '+t)})})"`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Multi-tenant isolation must hold simultaneously across SQL composite foreign keys, `PostgreSQL FORCE RLS`, `AEAD AAD` cryptographic binding, and Valkey `{<org_id>}` hash-tags.
- **Rollback:** Atomic `git revert` on the specification branch if empirical findings in `TP-IMP-0004` require adjustments.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0001` and `TP-IMP-0002` are complete; Product Owner has approved `CR-06` and `CR-07`.
- **DoD:** Closes all `11/11` `CRITICAL` A08 audit gaps in [`unresolved-gaps.md`](../../reviews/unresolved-gaps.md); ready for `GATE-M0` sign-off upon completion of `TP-IMP-0004`.
