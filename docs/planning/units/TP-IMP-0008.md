# `TP-IMP-0008` — Three-Tier Envelope Encryption Vault (`Master KEK -> Org DEK -> Project DEK`), `AEAD AAD` & Write-Only `SecretCredential`

| Property | Value |
|---|---|
| **Unit ID** | `TP-IMP-0008` |
| **Milestone / Epic** | `M1` (`Foundation & Vertical Slice`) · `EP-01` |
| **Status** | `BLOCKED` *(Depends on `TP-IMP-0007` — `GAP-01, 05`)* |
| **Size** | `M` (`internal/security/vault/*`, `db/migrations/sqlite/0002_vault.sql`) |
| **Review Tier** | **`REQUIRES_STRONG_REVIEW`** *(Cryptographic protection for crown-jewel assets `AST-01`, `AST-02`)* |
| **Technical Owner** | `Principal Security / Cryptography Engineer` |

---

## 1. Intent
Build the write-only secret vault (`Write-Only Secret Vault`) using three-tier `AES-256-GCM` authenticated envelope encryption (`Master KEK` → `Org DEK` → `Project DEK`) with hard-bound `AEAD AAD` context per [`secrets-and-tenant-isolation.md`](../../security/secrets-and-tenant-isolation.md) and `CR-06`, guaranteeing: (1) complete immunity to cross-organization and cross-project ciphertext transplant attacks, (2) zero external API/UI read-back methods for plaintext secrets, (3) online `Master KEK` rotation via lightweight `Org DEK` re-wrapping, and (4) cryptographic erasure (`Crypto-Shredding`) at both `Organization` and `Project` granularity.

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `internal/security/vault/kek.go` (`KEKProvider` interface + static `256-bit` key loader from `TANPOPO_MASTER_KEY` or local `0400`/`0600` keyfile).
  - `internal/security/vault/envelope.go` (`org_encryption_keys` and `project_encryption_keys` lifecycle, short-lived `TTL <= 5m` in-memory DEK cache with immediate `clear(buf)` zeroization on rotate/revoke/shred).
  - `internal/security/vault/store.go` (`SealSecret`, `RotateSecret`, `RewrapAllOrgDEKs`, `ShredProjectDEK`, `ShredOrgDEK`, and the unexported-field capability token `UnsealCapability` minted once in `cmd/tanpopo` exclusively for `internal/security/broker.NewBlindBroker` to call `UnsealWithCapability(cap, ...)` — mechanically preventing any other package in the repository from unsealing secrets).
  - `db/migrations/sqlite/0002_vault.sql` (`org_encryption_keys`, `project_encryption_keys`, `secret_credentials` with nullable `owner_project_id` for shared `Org`-level credentials).
- **Out-of-Scope:** Remote AWS/GCP KMS & HashiCorp Vault providers (owned by `TP-IMP-0041` in `v0.3`); OAuth 2.1 refresh brokering (owned by `TP-IMP-0034`).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0204`, `TP-FR-1003`, `TP-NFR-0402`, `TP-NFR-0502`, `TP-BR-0102`, `AST-01`, `AST-02`, `AUTH-14`, `OPS-AC-07`, `OPS-AC-09`.
- **Dependencies:** `TP-IMP-0007`.
- **Design Refs:** [`secrets-and-tenant-isolation.md`](../../security/secrets-and-tenant-isolation.md) Sections 1 & 4, [`data-model.md`](../../architecture/data-model.md) Sections 3.1 & 3.2, [`backups-recovery.md`](../../operations/backups-recovery.md) (`DR-03`).

## 4. API, Wire & Data Contracts
- **Two Canonical `AEAD AAD` Formulas (`CR-06` / `TP-IMP-0003`):**
  1. For `Org`-scoped secrets (`owner_project_id IS NULL`, wrapped under `Org DEK`):
     `AAD = []byte("tanpopo:v1:cred:" + orgID + ":" + credentialID + ":" + secretKind)`.
  2. For `Project`-scoped secrets/debug-blobs/cache-entries (wrapped under `Project DEK`):
     `AAD = []byte("tanpopo:v1:proj:" + orgID + ":" + projectID + ":" + recordKind + ":" + recordID)`.
  - Always generate a fresh 96-bit `nonce` from `crypto/rand` on every seal or rotation operation.
- **Control Plane Return DTO (`SecretMetadata`):** Contains only `{ ID, OrgID, OwnerProjectID, Name, SecretKind, BoundAudience, KeyFingerprint, Version, UpdatedAt }` — **never `Plaintext` or `Ciphertext`**.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1`:** Given canary secret `sk-tp-canary-DO-NOT-LEAK-9f8e7d6c`, When calling `SealSecret`, Then on-disk SQLite bytes contain only high-entropy ciphertext (scanning the raw `.db` file for the canary string yields `0` hits) and `GetSecretMetadata` returns only an irreversible fingerprint.
- **`AC-2`:** Given `KEK_v1` wrapping 10 `Org DEKs`, When invoking `RewrapAllOrgDEKs(KEK_v1 -> KEK_v2)`, Then all 10 organizations continue unsealing `SecretCredential` records seamlessly under `KEK_v2` without rewriting `secret_credentials`.
- **`AC-3`:** Given `Project P1` and `Project P2` inside `Org A`, When invoking `ShredProjectDEK(Org_A, P1)`, Then `P1`'s cached RAM `DEK` is zeroized immediately and all `P1`-scoped secrets become permanently undecryptable (even from SQLite backups captured prior to shredding), while `P2` continues operating `100%` normally.
- **`NEG-1` (`AUTH-14` Ciphertext Transplant):** Using raw SQL to copy `nonce` + `ciphertext` from `Org A` to `Org B` (or from `Project P1` to `Project P2`) → `UnsealWithCapability` fails immediately with a `GCM` authentication tag error (`cipher: message authentication failed`).
- **`NEG-2`:** Invoking `UnsealWithCapability` with a zero-value forged `UnsealCapability{}` or starting in production mode with `TANPOPO_MASTER_KEY` shorter than `32 bytes` / all-zero bytes → rejected immediately (`Fail-Closed`).
- **`NEG-3`:** Never reuse a `(DEK, nonce)` pair or include raw key bytes inside decryption error messages or logs.

## 6. Test Commands & Required Evidence
- Pure-Go verification (`CGO_ENABLED=0`):
  - POSIX: `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -v ./internal/security/vault/...`
  - PowerShell: `$env:CGO_ENABLED='0'; $env:TANPOPO_TEST_HERMETIC='1'; go test -v ./internal/security/vault/...`
- Data-race verification (`-race`, `CGO_ENABLED=1`): `go test -v -race ./internal/security/vault/...`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** `scripts/ci/check.go` performs AST verification ensuring `vault.MintUnsealCapabilityForBroker()` is invoked at exactly one call site inside `cmd/tanpopo`; transient plaintext byte slices are scrubbed via `defer clear(plaintext)` immediately after use.
- **Rollback:** Purely additive schema (`0002_vault.sql`), fully compatible with hot SQLite backups.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0003` (locking the 2 `AAD` formulas & `Project DEK`) and `TP-IMP-0007` are complete.
- **DoD:** Achieves `>= 90%` test coverage across `internal/security/vault/...`, passes `CanaryScanner`, and completes `REQUIRES_STRONG_REVIEW`.
