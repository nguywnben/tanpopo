# Encrypted Backups, Data Retention, Disaster Recovery & Support Matrix — Tanpopo

> **SRE & Data Durability Specification (Phase ASTRA A06 — Updated in `TP-IMP-0003` Spec Closure Batch C)**
> - **Document ID:** `SPEC-OPS-02` | **Version:** `0.2.0-step03-closure` | **Last Updated:** `2026-11-14`
> - **Authored By:** Principal SRE & Security Architect
> - **Governance Status:** **Accepted Operations Specification (`ACCEPTED` — `CR-06, CR-07, CR-09, CR-10` & `COND-PO-01..05`)** — synchronized with [`data-model.md`](../architecture/data-model.md) (`ARCH-DATA-01`), [`deployment-topology.md`](../architecture/deployment-topology.md) (`ARCH-OPS-01`), and [`secrets-and-tenant-isolation.md`](../security/secrets-and-tenant-isolation.md) (`SPEC-SEC-03`). **All `RTO`, `RPO`, `< 5-second` cache-eviction, and `< 60-second` deletion/rollback figures in this document carry the `TARGET / UNVERIFIED` label until empirically validated in `TP-IMP-0008`, `TP-IMP-0032`, `TP-IMP-0040`, and `TP-IMP-0043` (`COND-PO-01`, `COND-PO-02`, `COND-PO-03`).**
> - **Requirements Traceability:** `GAP-04`, `GAP-05`, `GAP-06`, `GAP-13`; `WF-12`, `WF-13`, `OPS-05`, `OPS-06`, `OPS-09`, `TP-FR-1003`, `TP-NFR-0501`, `TP-NFR-0502`, `TP-NFR-0601`, `TP-NFR-0602`, `TP-NFR-0701`, `TP-NFR-0901`, `TP-NFR-0902`.

---

## 1. Zero-Plaintext Backup Architecture (`OPS-05` & `CR-06`)

### 1.1. Golden Rule: Strict Separation Between `.tpop.enc` Backups and `Master KEK`

Every sensitive record persisted by Tanpopo is protected by the **three-tier `AES-256-GCM` envelope encryption hierarchy (`Master KEK -> Org DEK -> Project DEK`)**:
- `org_encryption_keys` stores each organization's `256-bit` `Org DEK` wrapped under `Master KEK`.
- `project_encryption_keys (org_id, project_id)` stores each project's `256-bit` `Project DEK` wrapped under its parent `Org DEK`.
- All sealed records bind one of **two exact canonical `AEAD AAD` strings**:
  1. `Org`-scoped secret (`secret_credentials.owner_project_id IS NULL`, under `Org DEK`): `"tanpopo:v1:cred:" + org_id + ":" + credential_id + ":" + secret_kind`
  2. `Project`-scoped secret, opt-in debug payload blob, or opt-in cache entry (under `Project DEK`): `"tanpopo:v1:proj:" + org_id + ":" + project_id + ":" + record_kind + ":" + record_id`

Consequently:
- **Database backups NEVER contain the `Master KEK` (`TP-FR-1003`):** `tanpopo backup create` inspects its staging manifest and aborts immediately if any `Master KEK` file or environment dump is present (`TP-IMP-0032`). An attacker who exfiltrates `.tpop.enc` or a PostgreSQL backup from S3/NAS lacks the root `Master KEK` required to unwrap any `Org DEK` or `Project DEK`.
- **Custody Hazard Upon `Master KEK` Loss:** Conversely, if an operator loses both the host and `TANPOPO_MASTER_KEY` (without Cloud KMS / Vault custody), routes, policies, `virtual_keys` (`SHA-256` digests), `usage_settlements`, and `audit_events` remain `100%` restorable, whereas sealed `secret_credentials` must be re-entered. `SCR-01` and `SCR-16` display a persistent warning `Banner` until `Master KEK` backup custody is acknowledged (`UX-GR-05`).
- **Local Admin Lockout Recovery (`Break-Glass` — `CR-09`):** If a standalone self-hoster loses their local admin password or misconfigures OIDC SSO after `/setup` seals (`410 bootstrap_already_completed`), host/container shell access recovers administrative control via **`tanpopo admin reset-local-owner`**, appending a tamper-evident entry to the `'SYSTEM'` chain in `audit_events`.

### 1.2. Backup Scope, Methods & `SQLite -> PostgreSQL` Promotion

| Component to Back Up | Single-Node Mode (`SQLite WAL`) | Clustered HA Mode (`PostgreSQL 16+` & `Valkey`) | Recommended Cadence & `RPO` (`TARGET / UNVERIFIED`) |
|---|---|---|---|
| **1. Primary Relational Store** (Config, `org_encryption_keys`, `project_encryption_keys`, `micro-USD` Ledgers, `audit_events` Chains) | Online non-blocking CLI: `tanpopo backup create --output backup.tpop.enc` (executes `VACUUM INTO` snapshot + `zstd` compression + `SHA-256` manifest verification). | Continuous streaming backup via **`pgBackRest` / `WAL-G`** (daily base backups + continuous `WAL` archiving to Object Storage for Point-in-Time Recovery `PITR`). | Single-node: Every `15m` – `1h` (shipped off-host).<br/>Clustered HA: Continuous (`RPO <= 1–5m TARGET / UNVERIFIED`, within `< 15m` `TP-NFR-0601`). |
| **2. Lockfile & `.tpkg` Artifact Store** (`tanpopo.lock` + pinned `.tpkg`/`.wasm` bundles) | Bundled automatically inside `backup.tpop.enc` (and tracked in GitOps). | Replicated in internal S3 / Private OCI Registry with `Object Versioning` enabled. | On every extension install, upgrade, or rollback. |
| **3. Server Bootstrap Config** (`tanpopo.yaml` — `${ENV:...}` refs, `0` plaintext secrets) | Versioned in Git + included inside `backup.tpop.enc`. | Managed via Helm `values.yaml` / Terraform in Git. | Per infrastructure commit. |
| **4. `Valkey` Fast-State Cluster** (`tp:v1:{<org_id>}:*`) | *(Not applicable — in-process Go RAM)* | **No persistent backup required:** rate limits are ephemeral, while `budget_ledgers`, `1s` `valkey_hold_checkpoints`, and key revocations reside in PostgreSQL and rebuild via `tanpopo admin verify-state` (`< 10s TARGET / UNVERIFIED`). | Reconstructed from PostgreSQL on recovery (`COND-PO-03`). |
| **5. Root Key (`Master KEK`)** | Stored offline in an independent secret manager (1Password, Vaultwarden, hardware safe). | Stored and rotated in multi-region **AWS KMS / GCP KMS / Azure Key Vault / HashiCorp Vault**. | Quarterly access-verification drill. |

- **Engine Promotion (`SQLite -> PostgreSQL` — `CT-32`, `CR-10`):** When promoting a standalone host to HA mode, run `tanpopo admin freeze-writes --reason "Migrate to PostgreSQL"`, execute **`tanpopo db migrate-engine --from sqlite:///var/lib/tanpopo/tanpopo.db --to postgres://... --verify`** (which pre-creates all historical and 3-month lookahead monthly `RANGE` partitions on `usage_settlements`, `request_traces_meta`, and `audit_events` before copying rows and verifying row counts, `micro-USD` totals, `VerifyAuditChain`, and trial `DEK` unwrapping), switch nodes to PostgreSQL + Valkey, and run **`tanpopo admin unfreeze-writes`**.

---

## 2. Tiered Retention Policy & Tenant Crypto-Shredding (`C1..C4`)

### 2.1. Default Privacy-Preserving Retention Windows (`TP-NFR-0501`, `TP-BR-0802`)

| Data Tier (`C1..C4`) & Physical Table | Day-One Default Posture | Configurable Retention Bounds | Automated Lock-Free Purge Mechanism |
|---|---|---|---|
| **`C4` — Opt-In Debug Payloads (`debug_payload_blobs`)** | **DISABLED BY DEFAULT (`0 bytes` written to disk)** | When explicitly enabled per project: **`TTL <= 72 hours`** (enforced by SQL `CHECK`). | Sealed under `Project DEK`; rolling micro-batch purge (`expires_at_ms <= now_ms`) every `10 minutes`. |
| **`C2` — Technical Request Metadata (`request_traces_meta`)** | **ENABLED** (`request_id`, HTTP status, `waterfall_stages_json`, token counts — `0` prompt bytes) | **`7 days` – `90 days`** (default `30 days`). | PostgreSQL: zero-lock `DETACH PARTITION CONCURRENTLY` -> `DROP TABLE request_traces_meta_YYYY_MM`; SQLite: yielded micro-batches. |
| **`C3` — Financial Settlement Ledger (`usage_settlements`)** | **ENABLED** (immutable via `BEFORE UPDATE/DELETE` trigger; powers `Chargeback`) | **`>= 365 days`** (or statutory `1–7 years`). | Read-only monthly partitions (`PARTITION BY RANGE (settled_at_ms)`); cold Parquet/CSV export prior to partition detach. |
| **`C3` — Security Audit Log (`audit_events`)** | **MANDATORY ALWAYS-ON** (immutable per-`org_id` + `'SYSTEM'` `RFC 8785 JCS` hash chains — `TP-BR-0803`) | **`>= 365 days`** (`UPDATE`/`DELETE` aborted by DB trigger). | Signed export to immutable WORM storage (`S3 Object Lock`) prior to detaching aged monthly partitions. |

### 2.2. Organization & Single-Project Erasure Workflow (`< 60s TARGET / UNVERIFIED` — `TP-NFR-0502`, `OPS-AC-09`)

When an `ORG_OWNER` confirms permanent deletion of a single `Project` or an entire `Organization` (`WF-13`):
1. **Target Phase 1 (`0–5s TARGET / UNVERIFIED` — `Freeze & Stream Abort`):** Transitions `projects.status` or `organizations.status` to `PENDING_ERASURE`, appends an emergency `SHRED` epoch to `config_changelog`, broadcasts on `Valkey Pub/Sub` + `PG LISTEN/NOTIFY` to block new admissions and abort active SSE streams (`< 1s` standalone / `< 5s` cluster `TARGET / UNVERIFIED`), and finalizes in-flight `budget_reservations`.
2. **Target Phase 2 (`5–15s TARGET / UNVERIFIED` — `Cryptographic DEK Destruction & RAM Eviction`):**
   - *Single-Project Shred:* Deletes the target row in **`project_encryption_keys (org_id, project_id)`** (cascade-deleting live project-owned `secret_credentials`, `debug_payload_blobs`, and `response_cache_entries`) and zeroizes `dek:proj:<org_id>:<project_id>:*` across `BlindBroker` RAM caches on all nodes, while leaving sibling projects and the parent `Organization` `audit_events` hash chain intact.
   - *Full-Organization Shred:* Deletes **`org_encryption_keys`** (cascading all child `project_encryption_keys`), zeroizes `dek:org:<org_id>:*` across all nodes, and purges `tp:v1:{<org_id>}:*` keys in Valkey.
3. **Target Phase 3 (`15–45s TARGET / UNVERIFIED` — `Relational Purge & Anonymized Receipt`):** Purges mutable `virtual_keys`, `provider_connections`, `route_policies`, `policies`, and `request_traces_meta` for the target scope, recording an anonymized compliance receipt (`tenant.crypto_shredded`, `0` PII).

> [!IMPORTANT]
> **Product Owner Verification Condition (`COND-PO-02` — Cold-Backup Undecipherability Is `TARGET / UNVERIFIED` Until Empirically Proven):**
> Destroying `project_encryption_keys` or `org_encryption_keys` rows in SQL design **does NOT by itself prove** that historical `.tpop.enc` or PostgreSQL PITR backups are undecipherable or that erasure completes in `< 60 seconds`. Both **`< 60s` Crypto-Shredding** and **cold-backup undecipherability** remain **`TARGET / UNVERIFIED`** until full key-lifecycle tests, live backup/restore drills, multi-node RAM `DEK` cache eviction and zeroization assertions, SQLite WAL/page residue checks, and post-shredding non-recoverability tests pass in **`TP-IMP-0008`** and **`TP-IMP-0032`** (and multi-node HA drills in **`TP-IMP-0043`**).

---

## 3. Four Step-by-Step Disaster Recovery Runbooks (`RTO < 15m TARGET / UNVERIFIED`)

Every runbook enforces a strict safety invariant: **restore and verify in local maintenance mode bound exclusively to `127.0.0.1` before reopening external ingress traffic**.

### `DR-01`: Standalone Host Total Loss (`RTO < 10m TARGET / UNVERIFIED`)
- **Execution Steps:**
  1. Provision a clean host and install the exact **`tanpopo`** binary version recorded in backup metadata.
  2. Supply `TANPOPO_MASTER_KEY` (or `0400`/`0600` keyfile) from the organization's external secret manager.
  3. Verify archive integrity and restore `.tpop.enc`:
     ```bash
     tanpopo backup verify ./backup-latest.tpop.enc
     tanpopo backup restore ./backup-latest.tpop.enc --data-dir /var/lib/tanpopo --verify-secrets-decrypt
     ```
     *(`--verify-secrets-decrypt` trial-unwraps `Org DEK` and `Project DEK` envelopes and runs `VerifyAuditChain` across every `chain_key` in `audit_events` without printing any secret material to `stdout`/`stderr`.)*
  4. Boot locally via `tanpopo serve --role=all --listen 127.0.0.1:8080`, run `tanpopo doctor` to verify `/readyz = 200` and `tanpopo.lock` package digests, then reopen Reverse Proxy ingress.

### `DR-02`: Clustered PostgreSQL Corruption or Infrastructure Loss (`RPO < 5m`, `RTO < 15m TARGET / UNVERIFIED`)
- **Execution Steps:**
  1. Enable maintenance mode at the Ingress layer and halt writes on `tanpopo-controlplane`.
  2. Restore PostgreSQL `16+` to the target recovery timestamp (`pgbackrest --type=time "--target=2026-11-14 08:00:00+00" restore`).
  3. Execute post-PITR audit, ledger, and Valkey reconstruction:
     ```bash
     tanpopo admin verify-state --check-audit-chain --reconcile-orphaned-holds --rebuild-valkey-state
     ```
     *(Verifies every per-`org_id` and `'SYSTEM'` `RFC 8785 JCS` audit chain, reclaims interrupted `RESERVED_HOLD` / `STREAMING_ACCRUING` reservations in `budget_reservations` alongside `valkey_hold_checkpoints`, and rebuilds `tp:v1:{<org_id>}:*` Valkey state prior to admitting traffic. Per **`COND-PO-03`**, full ledger consistency across Valkey/PostgreSQL failover and `verify-state` recovery remains `TARGET / UNVERIFIED` until proven under chaos testing in `TP-IMP-0021`, `TP-IMP-0040`, and `TP-IMP-0043`.)*
  4. Roll restart `tanpopo-controlplane` and `tanpopo-dataplane` pods, confirm `/readyz = 200`, and reopen production ingress.

### `DR-03`: Suspected `Master KEK` Exposure (`Online Envelope Re-Wrap & Mandatory Pre-Retirement RAM Cache Eviction` — `AUD-010`)
- **Objective:** Transition online to a fresh `Master KEK` with zero dropped SSE streams, ensuring **every Data Plane pod evicts its in-memory `DEK` cache before `KEK_v1` is retired**.
- **Execution Steps:**
  1. Provision `KEK_v2` in Cloud KMS / HashiCorp Vault (or staged keyring) while keeping `KEK_v1` enabled in decrypt-only posture.
  2. Run online `Org DEK` re-wrapping (re-encrypts only `org_encryption_keys` rows in `< 2s TARGET / UNVERIFIED` without rewriting `project_encryption_keys` or `secret_credentials`, atomically increments `config_changelog` with `mutation_type = 'KEY_ROTATE'`, and broadcasts invalidation over `Valkey Pub/Sub` + `PG LISTEN/NOTIFY`):
     ```bash
     tanpopo vault rotate-kek --old-kek-id kek_v1 --new-kek-id kek_v2
     ```
  3. **Mandatory Data Plane RAM `DEK` Cache Eviction Gate (`< 5s TARGET / UNVERIFIED` — `DO NOT SKIP`):** Before disabling `KEK_v1`, verify via `tanpopo doctor` or `GET /admin/v1/ops/cluster-status` that **`100%` of active Data Plane nodes report `config_epoch >= <rotation_epoch>`** (`max(tanpopo_config_snapshot_age_seconds) < 5`), confirming every node has zeroized cached `dek:org:<org_id>:kek_v1` buffers and re-unwrapped `Org DEKs` under `KEK_v2`. Drain or isolate any unreachable pod prior to proceeding.
  4. Only after `100%` cluster cache eviction is confirmed, permanently disable/retire `KEK_v1` in KMS/Vault; if an attacker may have obtained both a database copy and `KEK_v1`, rotate all upstream provider API keys and revoke OAuth grants via `SCR-04`.

### `DR-04`: Core or Extension Upgrade Regression (`Instant Rollback < 60s TARGET / UNVERIFIED` — `SM-08`)
- **Execution Steps:**
  1. Because *Expand-Only* SQL migrations preserve backward compatibility for `N-1` binaries, no cold database restore is needed.
  2. If caused by an extension upgrade: run `tanpopo plugin rollback <slug>` (or via `SCR-07`) -> atomically swaps `active_version_id` back to `previous_pinned_version_id` in `< 5s` (`TARGET / UNVERIFIED`).
  3. If caused by a new `tanpopo` binary: run `helm rollback tanpopo` (or revert the image tag in `docker-compose.yml` and run `docker compose up -d`) -> nodes resume against the current schema and pinned `tanpopo.lock` in `< 60s` (`TARGET / UNVERIFIED`).

---

## 4. Platform, Browser & Contract Compatibility Matrix (`Support Matrix`)

| Support Dimension | Official Tier-1 Support | Best-Effort Compatibility | Deprecation & Maintenance Policy |
|---|---|---|---|
| **Host OS & CPU (`TP-NFR-0901`)** | Linux `x86_64` & `arm64` (kernel `>= 5.15`), standard `nonroot` OCI containers, macOS `14+` (`arm64` & `x86_64`), Windows `11` / Windows Server `2022+` (`x86_64` & `arm64`). | FreeBSD, `riscv64`. | Aligned with upstream OS Long-Term Support (`LTS`) lifecycles. |
| **Databases & Fast State** | Embedded pure-Go SQLite (`WAL` mode), **PostgreSQL `16`, `17`**, **Valkey `7.2`, `8.x`** / Redis `7.2` (with `{<org_id>}` cluster hash-tags & `LISTEN/NOTIFY`). | Managed cloud PostgreSQL / Valkey-compatible services supporting `FORCE RLS` & `EVALSHA`. | Supports at least the `2` most recent major PostgreSQL releases; `6 months` notice prior to raising minimum versions. |
| **Web Console Browsers (`TP-NFR-0701`)** | Latest `2` stable releases of **Chromium / Chrome / Edge**, **Firefox**, and **Safari** (macOS & iOS `>= 17`). | Other enterprise Chromium browsers. | Requires ES2022 and CSS Cascade Layers (`@layer`). |
| **Extension Contract & SDK (`TP-NFR-0902`, `BA-OQ-005`)** | Active `tanpopo.extension.v1` (`runtime.proto`) and current `ExtensionManifest` schema. | Preceding minor version within the `N-1` window (`0.x`) or `>= 12-month` SLA (`1.x`). | Pre-`1.0` (`0.x`): supports **`N-1` minor contract**. From `v1.0 GA`: guarantees **`>= 12 months`** backward compatibility across `1.x`, surfacing warnings in Console/CLI `>= 2` releases before field removal. |
| **Security Advisories** | Current stable minor + `N-1` minor (post-`v1.0`). | `QUARANTINED` / policy mitigations for older branches. | Confirmed `Critical` vulnerabilities target emergency patch release and signed `revocations.json` publication within `< 48 hours` (`TARGET / UNVERIFIED`). |
