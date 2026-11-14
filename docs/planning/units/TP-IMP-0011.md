# `TP-IMP-0011` — Safe `.tpkg` Archive Extractor, Provenance/Signature Verifier, `SM-01` Capability Gate, Escalation Guard & `tanpopo.lock`

| Property | Value |
|---|---|
| **Unit ID** | `TP-IMP-0011` |
| **Milestone / Epic** | `M1` (`Foundation & Vertical Slice`) · `EP-02` |
| **Status** | `BLOCKED` *(Depends on `TP-IMP-0007`, `TP-IMP-0009` — `GAP-01, 04, 08, 09`)* |
| **Size** | `L` (`go.mod`, `internal/extension/installer/*`, `internal/extension/lockfile/*`, `db/migrations/sqlite/0004_extensions.sql`) |
| **Review Tier** | **`REQUIRES_STRONG_REVIEW`** *(Extension supply chain `TB-5`, Zip-Slip/Archive-Bomb defense & privilege-escalation guard)* |
| **Technical Owner** | `Platform Supply-Chain & Security Engineer` |

---

## 1. Intent
Build the installation, upgrade, rollback, and quarantine (`QUARANTINED`) pipeline for canonical **`.tpkg`** extension packages (reproducible `gzipped tar`, `<= 256` files, `<= 64 MiB`) sourced from local files, hash-pinned URLs, or immutable `40-hex SHA` Git commits **without recompiling Tanpopo Core**, enforcing the full `SM-01` state machine, separating the 3 orthogonal trust axes (`Package Origin ≠ Publisher Trust ≠ Runtime Safety`), enforcing organization minimum trust policies (`org_extension_policies` — `CT-20`), blocking silent upgrade capability escalation $\Delta P \neq \emptyset$ (`CT-18`), and recording immutable digests in `tanpopo.lock`.

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `go.mod`, `go.sum` (add license-vetted `Apache-2.0`/`MIT`/`BSD` pure-Go YAML / JSON Schema validation libraries).
  - `internal/extension/installer/archive_guard.go` (extract `.tpkg` enforcing hard caps of `<= 64 MiB` total uncompressed bytes, `<= 256` entries, `<= 50:1` compression ratio, rejecting `..` traversal, absolute paths, and `symlink`/`hardlink`/device entries; verify `filepath.EvalSymlinks` confinement inside `plugins_dir` before directory removal — `CT-04`).
  - `internal/extension/installer/manifest_validator.go` (validate `manifest.yaml` against canonical `ExtensionManifest` including `contractVersion`, `coreVersionRange`, `secretSlots`, `allowed_hosts`; reject unsupported `contractVersion` such as `"9.0"` — `CT-02`, enforce `N-1` compatibility window — `CT-24`, and block namespace impersonation matching `^@tanpopo(-[a-z0-9]+)?/` unless signed by the project root key — `CT-05`).
  - `internal/extension/installer/provenance.go` (verify `SHA-256` archive digest parity against `tanpopo.lock` — `CT-03` + offline `Ed25519` signature verification; classify `official`, `verified`, or `unverified-external` and enforce `org_extension_policies` allowlists — `CT-20`).
  - `internal/extension/installer/lifecycle.go` (drive canonical `SM-01` transitions: `DISCOVERED -> VERIFYING -> PENDING_PERMISSION_APPROVAL -> INSTALLED_DISABLED | ENABLED_ACTIVE -> UPDATING_PENDING_RECONSENT | DRAINING | ROLLED_BACK | QUARANTINED`, export `QuarantineInstallation` for `fault_guard` in `0013`, and retain at least 1 known-good prior version for `< 5s` rollback).
  - `internal/extension/lockfile/lockfile.go` (atomic `tanpopo.lock` reader/writer).
  - `db/migrations/sqlite/0004_extensions.sql` (`extension_packages`, `extension_versions`, `extension_installations` with nullable `active_version_id` while awaiting approval, `org_extension_policies`, `extension_revocations`).
- **Out-of-Scope:** Remote Marketplace index synchronization & `Private OCI Registry` pulls (owned by `TP-IMP-0035`); offline `Air-Gap Bundle` export/import (owned by `TP-IMP-0045`).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0304`, `TP-FR-0306`, `TP-FR-0703`, `TP-FR-1002`, `TP-FR-1004`, `TP-NFR-0403`, `TP-NFR-0902`, `SM-01`, `CT-01`, `CT-02`, `CT-03`, `CT-04`, `CT-05`, `CT-18`, `CT-20`, `CT-24`, `CT-25`, `FZ-02`.
- **Dependencies:** `TP-IMP-0007`, `TP-IMP-0009`.
- **Design Refs:** [`extension-contract.md`](../../specifications/extension-contract.md) (`SPEC-EXT-01`), [`marketplace-lifecycle.md`](../../specifications/marketplace-lifecycle.md) (`SPEC-EXT-04`), [`plugin-sandbox-and-supply-chain.md`](../../security/plugin-sandbox-and-supply-chain.md), [`contract-test-matrix.md`](../../quality/contract-test-matrix.md) (`CT-01`..`05`, `CT-18`, `CT-20`, `CT-24`, `CT-25`, `FZ-02`).

## 4. API, Wire & Data Contracts
- **Capability Approval Gate & Escalation Algebra ($\Delta P$):**
  - Fresh installation requesting `allowed_hosts` network egress or `secretSlots` halts at `PENDING_PERMISSION_APPROVAL` (only `ORG_OWNER` / `OPERATOR` / `SECURITY_REVIEWER` may approve transitions to `INSTALLED_DISABLED` or `ENABLED_ACTIVE`).
  - Upgrade `v1.0.0 -> v1.1.0`: if $\Delta P = P_{\text{new}} \setminus P_{\text{approved}} \neq \emptyset$ (adding a new host, new `secretSlots`, or escalating from Tier-1 Declarative to Tier-2 Wasm), `v1.0.0` continues serving traffic uninterrupted while `v1.1.0` parks at `UPDATING_PENDING_RECONSENT` (`CT-18`).

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1`:** Given a valid `.tpkg` archive or `40-hex SHA` Git reference, When installed and capability set `P` is approved by `SECURITY_REVIEWER`, Then state progresses `VERIFYING -> PENDING_PERMISSION_APPROVAL -> ENABLED_ACTIVE`, `config_changelog` increments, and `tanpopo.lock` records the exact `sha256` and approved capability set `P`; invoking `Rollback` restores prior traffic routing in `< 5 seconds`.
- **`NEG-1` (`CT-01`, `CT-02`, `CT-24` Schema & Contract Range):** Malformed manifest syntax (`CT-01`), unsupported `contractVersion: "9.0"` (`CT-02`), or incompatible `coreVersionRange` outside the `N-1` window (`CT-24`) → rejected with canonical structured error codes.
- **`NEG-2` (`CT-03`, `CT-05`, `CT-20` Integrity, Namespace & Org Trust Policy):** Flipping 1 byte in `.tpkg` relative to `tanpopo.lock` (`CT-03`), third-party impersonation of `@tanpopo/openai` or `@tanpopo-official/x` without a valid root signature (`CT-05`), or installing `unverified-external` inside an `Org` restricted to `official`/`verified` (`CT-20`) → `100%` hard-blocked.
- **`NEG-3` (`CT-04` Archive Traversal / Symlink / Bomb):** Archive containing `../../etc/passwd`, outbound `symlink`, `> 256` entries, or a `10 KiB -> 200 MiB` decompression bomb → extraction aborted immediately with `0` escaped files.
- **`NEG-4` (`CT-18` & `CT-25` Escalation & Mutable Git Ref):** Upgrade silently adding a new egress domain parks at `UPDATING_PENDING_RECONSENT` (`CT-18`); supplying mutable Git refs `main`/`latest` instead of a `40-hex SHA` commit is rejected with `422 unpinned_git_reference` (`CT-25`).

## 6. Test Commands & Required Evidence
- Pure-Go verification (`CGO_ENABLED=0`):
  - POSIX: `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -v ./internal/extension/installer/... ./internal/extension/lockfile/...`
  - PowerShell: `$env:CGO_ENABLED='0'; $env:TANPOPO_TEST_HERMETIC='1'; go test -v ./internal/extension/installer/... ./internal/extension/lockfile/...`
- Data-race (`-race`) & smoke fuzzing (`FZ-02`):
  - `go test -v -race ./internal/extension/installer/... ./internal/extension/lockfile/...`
  - `go test -fuzz=FuzzArchiveAndManifest -fuzztime=30s ./internal/extension/installer/...`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Strictly never execute install-time lifecycle hooks (`postinstall` or shell scripts) when unpacking or registering `.tpkg` archives.
- **Rollback:** Atomic version rollback via `active_version_id` pointer swap in `extension_installations` + `config_changelog`.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0001` (`SM-01` remediation & `^@tanpopo(-[a-z0-9]+)?/` regex lock), `TP-IMP-0003` (5 extension DDL tables), `TP-IMP-0007`, and `TP-IMP-0009` are complete.
- **DoD:** `100%` green across `CT-01..05, 18, 20, 24, 25` + `FZ-02`; passes `REQUIRES_STRONG_REVIEW`.
