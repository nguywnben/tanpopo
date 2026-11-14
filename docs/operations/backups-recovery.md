# Encrypted Backups, Data Retention, Disaster Recovery & Support Matrix — Tanpopo

> **SRE & Data Durability Specification (Phase ASTRA A06 — Updated in A08 Audit | `DRAFT / READY_FOR_REVIEW`)**
> - **Document ID:** `SPEC-OPS-02` | **Version:** `0.1.1-step08-audit` | **Last Updated:** `2026-11-14`
> - **Authored By:** Principal SRE & Security Architect
> - **Governance Status:** **Proposed Operations Specification (`PROPOSED — NO CODE`)** — directly bound to the physical schema in [`data-model.md`](../architecture/data-model.md) (`ARCH-DATA-01`) and deployment architecture in [`deployment-topology.md`](../architecture/deployment-topology.md) (`ARCH-OPS-01`). **All `RTO`, `RPO`, and `< 60 second` deletion window figures in this document carry the `TARGET / UNVERIFIED` label (not yet empirically benchmarked).**
> - **Requirements Traceability:** `WF-12`, `WF-13`, `OPS-05`, `OPS-06`, `OPS-09`, `TP-FR-1003`, `TP-NFR-0501`, `TP-NFR-0502`, `TP-NFR-0601`, `TP-NFR-0602`, `TP-NFR-0701`, `TP-NFR-0901`, `TP-NFR-0902`.

---

## 1. Zero-Plaintext Backup Architecture (`Zero-Plaintext Backup Architecture` — `OPS-05`)

### 1.1. Golden Rule: Strict Separation Between Data Backups and `Master KEK`
Every sensitive credential stored in Tanpopo (upstream provider `API Keys`, `OAuth Tokens`, Webhook signing secrets) is protected via envelope encryption (`Envelope Encryption`: `AES-256-GCM` under a per-tenant `Org DEK`, where each `Org DEK` is wrapped by the root `Master KEK`). Consequently:
- **Database backups NEVER contain the `Master KEK`:** Even if a backup archive stored on S3 or NAS is exfiltrated, an attacker cannot decrypt a single upstream provider API key.
- **Irrecoverable Secret Hazard Upon Master Key Loss (`Master KEK Loss Hazard`):** Conversely, if an operator loses both the primary host and the `Master KEK` (without using an auto-backed Cloud KMS), routes, policies, and `micro-USD` financial ledgers remain `100%` restorable, whereas all `Provider Connections` will require fresh credential entry. Therefore, `SCR-01` and `SCR-16` persistently display a system `Banner` until `Master KEK` custody in an independent password manager or Vault is acknowledged (`UX-GR-05`).

### 1.2. Backup Scope & Methods by Deployment Mode

| Component to Back Up | Single-Node Mode (`SQLite WAL`) | Clustered HA Mode (`PostgreSQL 16+` & `Valkey`) | Recommended Cadence & `RPO` Target |
|---|---|---|---|
| **1. Primary Database** (Configuration, Wrapped Keys, `micro-USD` Ledger, `audit_events` Hash Chain) | Online non-blocking CLI command: `tanpopo backup create --output backup.tpop.enc` (executes consistent `VACUUM INTO` snapshot followed by `zstd` compression + `SHA-256` manifest signing). | Continuous streaming backup via **`pgBackRest` / `WAL-G`** (daily full base backups + continuous `WAL` archiving to Object Storage for Point-in-Time Recovery `PITR`). | Single-node: Every `15 minutes` – `1 hour` (shipped off-host).<br/>Clustered HA: Continuous (`RPO < 5 minutes`, exceeding the `< 15 minute` requirement of `TP-NFR-0601`). |
| **2. Lockfile & Extension Artifact Store** (`tanpopo.lock` + pinned `.tpkg`/`.wasm` bundles) | Bundled automatically inside `backup.tpop.enc` (or tracked in the team's GitOps repository). | Replicated in internal S3/OCI Object Storage with `Object Versioning` enabled. | After every extension install or upgrade mutation. |
| **3. Server Bootstrap Configuration** (`tanpopo.yaml` — zero plaintext secrets) | Versioned in Git (GitOps) + included inside the backup archive. | Managed via Helm `values.yaml` / Terraform in Git. | Per infrastructure commit. |
| **4. `Valkey / Redis` Cache** | *(Not applicable — in-process RAM)* | **No backup required** for durability: rate-limit counters are ephemeral, while authoritative ledger balances and key revocation sets reside in PostgreSQL and rehydrate automatically on boot. | Reconstructed automatically from PostgreSQL in `< 10 seconds`. |
| **5. Root Key-Encryption Key (`Master KEK`)** | Stored offline independently inside an organizational secret manager (1Password, Vaultwarden, physical safe). | Stored and rotated inside multi-region **AWS KMS / GCP KMS / Azure Key Vault / HashiCorp Vault**. | Quarterly access-recovery drill. |

---

## 2. Tiered Retention Policy & Sub-60-Second Tenant Deletion (`Retention & Crypto-Shredding`)

### 2.1. Default Privacy-Preserving Retention Windows (`TP-NFR-0501`, `TP-BR-0802`)

| Data Classification | Day-One Default Posture | Configurable Retention Bounds | Automated Lock-Free Purge Mechanism |
|---|---|---|---|
| **Prompt / Completion Body Text (`Debug Payloads`)** | **DISABLED BY DEFAULT (`0 bytes` written to disk)** | When explicitly enabled per project for debugging: **`TTL <= 72 hours`** (hard ceiling; longer values rejected). | Rolling micro-batch deletion every `10 minutes` + wrapped under ephemeral short-lived keys. |
| **Technical Metadata Request Traces (`request_traces`)** | **ENABLED** (`request_id`, latency, model, error code, token counts, `accuracy`) | **`7 days` – `90 days`** (default `30 days`). | On PostgreSQL: instant monthly partition drop `DROP TABLE request_traces_YYYY_MM`; on SQLite: off-peak `LIMIT 5000` micro-batches. |
| **Budget Settlement Ledger (`usage_settlements`)** | **ENABLED** (supports financial reconciliation & `Chargeback`) | **`>= 365 days`** (or corporate statutory accounting windows of `1–7 years`). | Read-only monthly partitions; supports cold export to Parquet/CSV prior to detaching aged partitions. |
| **Security Audit Log (`audit_events` hash-chained)** | **MANDATORY ALWAYS-ON (Immutable — `TP-BR-0803`)** | **`>= 365 days`** (manual deletion or mutation via API/Console is strictly prohibited). | Permits only signed archival export to immutable storage (`S3 Object Lock WORM`) prior to rotating expired partitions. |

### 2.2. Permanent Organization / Project Deletion in `< 60 Seconds` (`Tenant Crypto-Shredding` — `TP-NFR-0502`)
When an `ORG_OWNER` confirms permanent deletion of a `Project` or an entire `Organization` (`WF-13`):
1. **Seconds `0–5` (`Freeze & Stream Abort`):** Transitions tenant state to `DELETING`, broadcasts a cluster-wide revocation pulse (`< 5s`) blocking all new requests and severing active SSE streams for that scope, and settles any in-flight budget holds.
2. **Seconds `5–15` (`Cryptographic Key Destruction`):** Permanently destroys the dedicated `Tenant/Project DEK` row in the key table and purges in-memory caches inside the `Blind Broker` across every node — rendering all encrypted credentials and sensitive records belonging to that tenant (including copies residing inside unexpired historical backups) mathematically irrecoverable ciphertext noise.
3. **Seconds `15–45` (`Relational Purge & Anonymized Receipt`):** Purges virtual keys, connections, routes, policies, and metadata traces scoped to that tenant, retaining **strictly 1 anonymized compliance receipt** (`tenant.crypto_shredded`, containing zero PII) to prove regulatory erasure.

---

## 3. Four Step-by-Step Disaster Recovery Runbooks (`Disaster Recovery Runbooks` — `RTO < 15 Minutes`)

Every runbook below follows a strict safety invariant: **always restore and verify in local maintenance mode (`--maintenance-only` bound exclusively to `127.0.0.1`) before opening production ingress traffic**.

### `DR-01`: Standalone Host Total Loss (`Standalone Host Total Loss`)
- **Objective:** Provision and restore a replacement host from `backup.tpop.enc` + `TANPOPO_MASTER_KEY` in **`< 10 minutes`**.
- **Execution Steps:**
  1. Provision a clean host and install the exact `tanpopod` binary version recorded in backup metadata.
  2. Inject `TANPOPO_MASTER_KEY` from the organization's secret vault.
  3. Verify integrity and restore the encrypted archive:
     ```bash
     tanpopo backup verify ./backup-latest.tpop.enc
     tanpopo backup restore ./backup-latest.tpop.enc --data-dir /var/lib/tanpopo --verify-secrets-decrypt
     ```
     *(The `--verify-secrets-decrypt` flag unwraps `Org DEK` envelopes and validates the `audit_events` hash chain without printing any secret material to stdout/stderr).*
  4. Boot locally via `tanpopod serve --listen 127.0.0.1:8080`, run `tanpopo doctor` to confirm `/readyz = 200` and verify all packages in `tanpopo.lock` are loaded, then re-enable Reverse Proxy ingress.

### `DR-02`: Clustered PostgreSQL Database Corruption or Accidental Infrastructure Deletion (`Clustered DB PITR`)
- **Objective:** Restore an HA cluster to the exact second preceding the incident (`RPO < 5 minutes`, `RTO < 15 minutes`).
- **Execution Steps:**
  1. Enable maintenance mode at the Ingress layer and halt writes from `tanpopo-controlplane`.
  2. Restore a fresh PostgreSQL cluster to the target recovery timestamp (`pgbackrest --type=time "--target=2026-11-11 14:29:50+00" restore`).
  3. Run post-restore state verification and cache reconstruction:
     ```bash
     tanpopo admin verify-state --check-audit-chain --reconcile-orphaned-holds --rebuild-valkey-state
     ```
     *(Releases any orphaned `RESERVED_HOLD` budget reservations interrupted mid-flight at crash time and repopulates the full key-revocation set into Valkey prior to accepting traffic).*
  4. Roll restart `controlplane` and `dataplane` Pods, verify `/readyz = 200`, and reopen production ingress.

### `DR-03`: Suspected Master Key Exposure (`Master KEK Compromise & Envelope Re-Wrap`)
- **Objective:** Transition immediately to a fresh `Master KEK` with zero downtime, followed by prioritized rotation of upstream provider credentials.
- **Execution Steps:**
  1. Provision a next-generation master key (`KEK_v2`) in KMS/Vault while temporarily retaining `KEK_v1` in decrypt-only posture.
  2. Execute online envelope re-wrapping (`Envelope Re-Wrap` — re-encrypts only the compact set of `Org DEK` envelopes so execution completes in `< 2 seconds` without re-encrypting thousands of individual database rows):
     ```bash
     tanpopo vault rotate-kek --old-kek-id kek_v1 --new-kek-id kek_v2
     ```
  3. Permanently disable `KEK_v1` in KMS; if an adversary may have captured both a database snapshot and `KEK_v1` simultaneously, rotate all upstream provider API keys and revoke OAuth grants via `SCR-04`.

### `DR-04`: Severe Core or Extension Upgrade Regression (`Bad Release Rollback`)
- **Objective:** Roll the entire cluster back to the prior stable release in **`< 60 seconds`** (`SM-08`).
- **Execution Steps:**
  1. Because upgrade pre-steps enforce backward-compatible *Expand-Only* SQL migrations, no cold database restore is required.
  2. If the regression stems from a newly upgraded extension package: run `tanpopo plugin rollback <slug>` (or trigger via `SCR-07`) → completes in `< 5 seconds`.
  3. If the regression stems from a new Core binary: run `helm rollback tanpopo` (or revert the container image tag in `docker-compose.yml` and run `up -d`) → previous-version nodes seamlessly read the current schema and pinned `tanpopo.lock` within `< 60 seconds`.

---

## 4. Platform, Browser & Contract Compatibility Matrix (`Maintenance & Support Matrix`)

| Support Dimension | Official Tier-1 Support (`Tier-1 Supported`) | Best-Effort Compatibility (`Best-Effort`) | Deprecation & Lifecycle Maintenance Policy (`Deprecation Policy`) |
|---|---|---|---|
| **Host OS & CPU Architecture** (`TP-NFR-0901`) | Linux `x86_64` & `arm64` (kernel `>= 5.15`), standard OCI Containers, macOS `14+` (`arm64`), Windows Server `2022+` / Windows `11` (`x86_64`). | FreeBSD, `riscv64`, Windows `arm64`. | Aligned with upstream OS Long-Term Support (`LTS`) lifecycles. |
| **Databases & Cache** | Embedded SQLite (compiled into binary), **PostgreSQL `16`, `17`**, **Valkey `7.2`, `8.x`** / Redis `7.2`. | Managed cloud PostgreSQL/Redis-compatible services verified for Lua scripting & `LISTEN/NOTIFY`. | Supports at least the 2 most recent major PostgreSQL releases; provides `6 months` advance notice before raising minimum versions. |
| **Web Console Browsers** (`TP-NFR-0701`) | Latest `2` stable releases of **Chromium / Chrome / Edge**, **Firefox**, and **Safari** (macOS & iOS `>= 17`). | Other enterprise Chromium-based browsers. | Excludes Internet Explorer and environments disabling ES2022 or CSS Cascade Layers (`@layer`). |
| **Extension Contract & SDK** (`TP-NFR-0902`, `BA-OQ-005`) | Active `tanpopo.extension.v1` protocol and current `Manifest Schema`. | Immediately preceding version within the deprecation window. | - Pre-`1.0` (`0.x` series): Preserves compatibility with at least **`N-1` minor releases**.<br/>- From `v1.0` onward: Guarantees backward compatibility for at least **`12 months` (or the entire `1.x` major line)**, surfacing deprecation warnings in Console/CLI at least `2` releases before removing any legacy field. |
| **Security Patches** | Active stable minor branch (`Current Minor`) plus preceding minor (`N-1 Minor` post-`v1.0`). | Policy / `Quarantine` mitigation guidance for older branches. | Confirmed `Critical` vulnerabilities (sandbox escape, secret disclosure, cross-tenant bypass) target emergency patch release and revocation advisory publication within `< 48 hours`. |
