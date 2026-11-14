# `TP-IMP-0032` — Single-Node Self-Host Lifecycle: `/healthz` vs `/readyz`, `tanpopo doctor`, Declarative GitOps, Online Backup, `Crypto-Shredding < 60s` & `Upgrade Preflight`

| Attribute | Value |
|---|---|
| **Unit ID** | `TP-IMP-0032` |
| **Milestone / Epic** | `M2` (`MVP v0.1 Core Product`) · `EP-06` |
| **Status** | `BLOCKED` *(Depends on `TP-IMP-0008`, `TP-IMP-0011`, `TP-IMP-0031` — `GAP-01, 05`)* |
| **Size** | `L` (`internal/ops/health/*`, `doctor/*`, `gitops/*`, `backup/*`, `erasure/*`, `upgrade/*`) |
| **Review Tier** | **`REQUIRES_STRONG_REVIEW`** *(Online backup separated from `Master KEK`, `< 60s` cryptographic shredding & `SM-08` upgrade safety)* |
| **Technical Owner** | `Principal SRE / Storage Reliability Engineer` |

---

## 1. Intent
Deliver the complete SRE-grade production lifecycle toolkit for single-node self-hosted deployments (`TP-FR-1001`..`1004`, `SPEC-OPS-01`..`03`):
1. **Liveness/Readiness Separation (`/healthz` vs `/readyz`) & Graceful Connection Drain (`Graceful Drain`):** `/healthz` verifies process liveness; `/readyz` verifies database availability, `Master KEK` health, and `ConfigSnapshot` readiness — upon `SIGTERM`, `/readyz` immediately transitions to `503 draining` while allowing in-flight SSE streams to finish cleanly within `drain_timeout` (`OPS-AC-02`).
2. **8-Check `tanpopo doctor` Diagnostics & Declarative `GitOps` Reconciliation:** Validate key-file permissions (`0400/0600`), free disk space, `SQLite WAL` mode, system clock skew, and port availability, and atomically reconcile declarative `tanpopo.yaml` + immutable `tanpopo.lock` manifests (`OPS-AC-01`).
3. **Online `.tpop.enc` Backup & Verified Restore (`tanpopo backup create / verify / restore`):** Consistent hot backup (`VACUUM INTO` for SQLite producing canonical **`.tpop.enc`** archives) **never bundling the `Master KEK` inside the backup artifact** (`SPEC-OPS-02`), paired with post-restore checksum and audit-chain verification (`SLO-08`, `DR-01`, `OPS-AC-07`).
4. **Project / Organization Cryptographic Erasure Under `< 60 seconds` (`Crypto-Shredding` — `DR-03`, `OPS-AC-09`):** Permanently destroy the target `Project DEK` or `Org DEK` (`TP-IMP-0008`), zeroize in-memory caches, and purge associated records.
5. **Upgrade Preflight (`tanpopo upgrade preflight` — `SM-08`, `CT-24`, `DR-04`, `OPS-AC-05`) & Full 10/10-Step `EXT-ACCEPT-01` Harness:** Expand `tests/integration/ext_accept_01_test.go` (from the `M1` vertical slice) to execute all **10 mandatory steps** against a live `tanpopo serve` process with invariant `PID` and `sha256sum(tanpopo)` (covering `EU` data-residency routing, \$1.00 `SETTLED_EXACT`/`ESTIMATED` ledger accounting, `UPDATING_PENDING_RECONSENT` permission-escalation gating, `Rollback`, `< 1s` `Quarantine`, and `Blast-Radius` querying).

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `internal/ops/health/probes.go` & `internal/ops/lifecycle/drain.go`.
  - `internal/ops/doctor/checks.go` (8 environmental preflight checks).
  - `internal/ops/gitops/reconciler.go` (atomic reconciliation of declarative configuration using `${ENV:SECRET_NAME}` references).
  - `internal/ops/backup/sqlite_backup.go` (`create`, `verify`, `restore` for `.tpop.enc` archives).
  - `internal/ops/erasure/shredder.go` (`tanpopo tenant shred --project|--org` completing in `< 60 seconds`).
  - `internal/ops/upgrade/preflight.go` (`SM-08`: evaluating `coreVersionRange` `N-1` compatibility matrices across all packages in `tanpopo.lock` — `CT-24`).
  - `tests/integration/ext_accept_01_test.go` (completing all 10/10 `EXT-ACCEPT-01` steps).
- **Out-of-Scope:** PostgreSQL cluster `pg_dump` / PITR orchestration and `SQLite -> Postgres` migration (covered in `TP-IMP-0039`).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-1001`, `TP-FR-1002`, `TP-FR-1003`, `TP-FR-1004`, `TP-NFR-0501`, `TP-NFR-0502`, `TP-NFR-0601`, `SLO-08`, `SM-08`, `CT-24`, `EXT-ACCEPT-01`, `DR-01`, `DR-03`, `DR-04`, `OPS-AC-01`, `OPS-AC-02`, `OPS-AC-05`, `OPS-AC-07`, `OPS-AC-09`.
- **Dependencies:** `TP-IMP-0008`, `TP-IMP-0011`, `TP-IMP-0031`.
- **Design Refs:** [`installer-upgrades.md`](../../operations/installer-upgrades.md) (`SPEC-OPS-01`), [`backups-recovery.md`](../../operations/backups-recovery.md) (`SPEC-OPS-02`), [`monitoring-incidents.md`](../../operations/monitoring-incidents.md) (`SPEC-OPS-03`).

## 4. API, Wire & Data Contracts
- **KEK Separation Invariant (`KEK Separation Invariant`):** A `.tpop.enc` archive contains the encrypted SQLite snapshot (ciphertext), `tanpopo.lock`, and a `SHA-256` manifest; if any environment variable dump or `Master KEK` file is detected in the archive staging list, the backup operation aborts immediately.
- **Ledger & Audit Preservation Across `Project` Erasure:** Executing `Crypto-Shredding` on a `Project` cryptographically destroys that `Project`'s credentials and debug payloads permanently within `< 60 seconds`, while keeping the `Org`-level `audit_events` hash chain intact and unbroken (`CR-06`).

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1` (`OPS-AC-07` & `DR-01` Online Backup `.tpop.enc` & Clean Restore Under Load):** While 20 concurrent goroutines continuously append ledger settlements and audit events, running `tanpopo backup create` followed by `tanpopo backup restore` into a fresh directory with the original `Master KEK` yields `PRAGMA integrity_check = ok` and `100%` valid `VerifyAuditChain`.
- **`AC-2` (`OPS-AC-09` & `DR-03` `< 60s` Crypto-Shredding Proof):** After creating backup `B1.tpop.enc` and executing cryptographic shredding for `Project P1`, the erasure completes in `< 60 seconds`, flushes RAM caches immediately, and renders `P1`'s encrypted secrets inside `B1.tpop.enc` permanently undecryptable while `Project P2` continues decrypting normally.
- **`AC-3` (`EXT-ACCEPT-01` Full 10-Step Suite & `OPS-AC-02` Graceful Drain):** All 10/10 steps of `EXT-ACCEPT-01` pass with invariant `PID` and `sha256sum(tanpopo)`; upon `SIGTERM`, `/readyz` returns `503` immediately while active SSE streams flush cleanly through `[DONE]`.
- **`NEG-1` (`OPS-AC-05` & `CT-24` Incompatible `N-2` Plugin Blocks Upgrade):** Simulating an `N-2` extension outside `coreVersionRange` during `tanpopo upgrade preflight` exits with a deterministic non-zero status blocking the upgrade (`BLOCKED_INCOMPATIBLE`).
- **`NEG-2` (`Insecure KEK File Permissions`):** Setting KEK file permissions to `0644` causes both `tanpopo doctor` and production server startup to refuse execution.

## 6. Test Commands & Required Evidence
- Pure Go (`CGO_ENABLED=0`): `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -v ./internal/ops/... ./tests/integration/...` *(PowerShell: `$env:CGO_ENABLED='0'; $env:TANPOPO_TEST_HERMETIC='1'; go test -v ./internal/ops/... ./tests/integration/...`)*
- Race detector (`-race`): `go test -v -race ./internal/ops/... ./tests/integration/...`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Declarative `GitOps` manifests reject raw inline API keys (requiring `${ENV:...}` or vault secret references) to prevent operators from committing plaintext credentials to Git.
- **Rollback:** `.tpop.enc` backup restoration and `< 60s` binary rollback (`DR-04`) are verified automatically within this unit.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0001` (`SM-08` alignment), `TP-IMP-0003` (`Project DEK`), `TP-IMP-0008`, `TP-IMP-0011`, and `TP-IMP-0031` are complete.
- **DoD:** All `OPS-AC-01, 02, 05, 07, 09` criteria passing under `-race`, and `REQUIRES_STRONG_REVIEW` sign-off achieved.
