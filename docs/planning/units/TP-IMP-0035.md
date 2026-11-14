# `TP-IMP-0035` — Signed Static Marketplace Index Reader, `Private OCI Registry` Pull, `revocations.json` Auto-Quarantine & Official `Bedrock` / `Azure` / `/v1/rerank`

| Attribute | Value |
|---|---|
| **Unit ID** | `TP-IMP-0035` |
| **Milestone / Epic** | `M3` (`v0.2 MCP Governance & Authorized OAuth 2.1`) · `EP-07` |
| **Status** | `BLOCKED` *(Depends on `TP-IMP-0011`, `TP-IMP-0018`, `TP-IMP-0034` — `GAP-01, 04, 09`)* |
| **Size** | `L` (`internal/extension/registry/*`, `internal/ingress/http/rerank.go`, `extensions/official/aws-bedrock/*`, `azure-openai/*`) |
| **Review Tier** | **`REQUIRES_STRONG_REVIEW`** *(Remote index signature verification, automated CVE package quarantine via `revocations.json` & `AWS SigV4` signing inside `BlindBroker`)* |
| **Technical Owner** | `Platform Ecosystem & Supply-Chain Lead` |

---

## 1. Intent
Expand the extension distribution ecosystem and enterprise cloud provider catalog for `v0.2` (`TP-FR-0307`, `TP-NFR-0602`, `CR-01`):
1. **Signed Static Marketplace Index & `Private OCI Registry` Artifact Pull (`OCI Distribution Spec v1.1`):** Synchronize `Ed25519/Cosign`-signed `index.json` catalogs, enable organizations to register internal OCI registries (`Harbor`, `GHCR`, `ECR`), and continue operating at 100% capacity from local caches whenever external Marketplace connectivity is unavailable (`Graceful Offline Degradation`).
2. **Signed Emergency Revocation Feed (`revocations.json` Auto-Quarantine):** Periodically poll (or manually import in air-gapped environments) signed revocation advisories for vulnerable or malicious extensions; if an installed package matches a `CRITICAL` revoked digest/version, automatically transition it to `QUARANTINED`, sever active streams, and emit an `INC-03` alert.
3. **Add `POST /v1/rerank` Ingress + 2 Official Enterprise Adapters (`@tanpopo/aws-bedrock` with `SigV4` signing in `BlindBroker` & `@tanpopo/azure-openai`).**

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `internal/extension/registry/index_sync.go` (verifying `index.json` signatures + defending against `Freeze / Rollback Attack` feed replays via strictly monotonic `serial` and `expires_at` enforcement).
  - `internal/extension/registry/oci_client.go` (pulling immutable `sha256:...` digest-pinned `.tpkg` artifacts from OCI Registries via `SSRFGuardDialer`).
  - `internal/extension/registry/revocation_watcher.go` (verifying `revocations.json` signatures and triggering automated quarantine).
  - `internal/ingress/http/rerank.go` (normalized `POST /v1/rerank` document reranking endpoint) & `AWS_SIGV4` request signing support inside `BlindBroker`.
  - `extensions/official/aws-bedrock/*` & `extensions/official/azure-openai/*` complete with offline replay cassettes.
- **Out-of-Scope:** Keyless cloud role federation via `AWS IRSA / GCP Workload Identity` (covered in `TP-IMP-0041` in `v0.3` — in `v0.2`, `@tanpopo/aws-bedrock` uses narrowly scoped IAM credentials stored in `vault` and signed via `SigV4` inside `BlindBroker`).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0307`, `TP-NFR-0602`, `SPEC-EXT-04`, `CT-20`, `CR-01`.
- **Dependencies:** `TP-IMP-0011`, `TP-IMP-0018`, `TP-IMP-0034`.
- **Design Refs:** [`marketplace-lifecycle.md`](../../specifications/marketplace-lifecycle.md) (`SPEC-EXT-04`), [`plugin-sandbox-and-supply-chain.md`](../../security/plugin-sandbox-and-supply-chain.md) Section 4, [`unresolved-gaps.md`](../../reviews/unresolved-gaps.md) (`CR-01`).

## 4. API, Wire & Data Contracts
- **Anti-Rollback Feed Protection (`Anti-Rollback Feed Protection`):** Both `index.json` and `revocations.json` MUST carry a strictly increasing positive integer `version_serial`; if a compromised remote mirror serves an older validly signed feed with `version_serial < last_seen_serial`, Tanpopo rejects the update and logs a security alert.
- **Graceful Offline Degradation Invariant (`TP-NFR-0602`):** Network failures while syncing `index.json` or `revocations.json` MUST **never** disrupt live inference traffic served by previously installed valid extensions.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1`:** Given `FakeRegistryServer` serving signed `index.json`, `revocations.json`, and an internal OCI repository, When syncing the catalog and pulling a package by `@sha256:...`, Then digest verification succeeds; shutting down `FakeRegistryServer` leaves live inference and local `tanpopo.lock` reinstalls operating at `100%`.
- **`AC-2` (`Auto-Quarantine on Signed Advisory`):** When `FakeRegistryServer` publishes a validly signed `revocations.json` marking `community/bad-adapter@1.0.0` as `CRITICAL`, the watcher automatically transitions the package to `QUARANTINED` and records a security audit event.
- **`AC-3`:** Running `tanpopo plugin test` across `@tanpopo/aws-bedrock` (including canonical `AWS4-HMAC-SHA256` signature verification inside `BlindBroker`), `@tanpopo/azure-openai`, and `/v1/rerank` passes at `100%`.
- **`NEG-1` (`Unsigned / Downgraded Revocation Feed`):** Injecting a forged `revocations.json` with an invalid signature (attempting mass DoS quarantine of legitimate plugins) or a regressed `version_serial` is rejected completely.
- **`NEG-2` (`Mutable OCI Tag Without Digest`):** Pulling from OCI using only a mutable `:latest` tag without a pinned `sha256` digest when strict digest pinning is enabled fails with `422 unpinned_oci_digest`.

## 6. Test Commands & Required Evidence
- Pure Go (`CGO_ENABLED=0`): `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -v ./internal/extension/registry/... ./internal/ingress/... ./extensions/official/...` *(PowerShell: `$env:CGO_ENABLED='0'; $env:TANPOPO_TEST_HERMETIC='1'; go test -v ./internal/extension/registry/... ./internal/ingress/... ./extensions/official/...`)*
- Race detector (`-race`): `go test -v -race ./internal/extension/registry/... ./internal/ingress/... ./extensions/official/...`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Registry pull credentials for `Private OCI Registry` endpoints are stored in `vault` under `AEAD AAD` and injected exclusively for the matching OCI registry hostname.
- **Rollback:** Setting `marketplace.sync_enabled = false` switches cleanly to pure offline operation.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0011`, `TP-IMP-0018`, and `TP-IMP-0034` are complete.
- **DoD:** OCI sync, signature verification, auto-quarantine, and both new enterprise provider packages passing all tests, with `REQUIRES_STRONG_REVIEW` sign-off achieved.
