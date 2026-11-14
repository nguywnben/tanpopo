# `TP-IMP-0033` — Cross-Platform Packaging (`linux`, `darwin`, `windows`), Rootless OCI Image, `systemd`/Compose Units, `SBOM` Generation & `Cosign` Signing

| Attribute | Value |
|---|---|
| **Unit ID** | `TP-IMP-0033` |
| **Milestone / Epic** | `M2` (`MVP v0.1 Core Product` — **Gate `GATE-M2`**) · `EP-06` |
| **Status** | `BLOCKED` *(Depends on `TP-IMP-0032` — `GAP-01`)* |
| **Size** | `M` (`deploy/docker/*`, `deploy/compose/*`, `deploy/systemd/*`, `.goreleaser.yaml`, `scripts/release/verify-artifacts.sh`) |
| **Review Tier** | **`REQUIRES_STRONG_REVIEW`** *(Core software supply chain, rootless `nonroot` OCI container & `MVP v0.1` exit gate)* |
| **Technical Owner** | `Release & Platform Security Engineer` |

---

## 1. Intent
Package the complete `MVP (v0.1)` product into deterministic, supply-chain-hardened distribution artifacts (`Reproducible Builds`) ready to run across all 3 target operating systems (`linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`, `windows/amd64`), including a minimal rootless OCI container image (`distroless/static:nonroot` with `readOnlyRootFilesystem`), hardened `docker-compose.yml` + `tanpopo.service` (`systemd`) templates, automated `CycloneDX SBOM` generation covering both Go and npm dependencies, and `Cosign` + `SHA256SUMS` cryptographic signing — closing **Gate `GATE-M2` (`MVP v0.1`)**.

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `.goreleaser.yaml` (`CGO_ENABLED=0 -trimpath -ldflags="-s -w"` cross-compilation producing reproducible `.tar.gz` / `.zip` archives across all 5 OS/Arch targets).
  - `deploy/docker/Dockerfile` (multi-stage build: compile `web/console` with `--ignore-scripts` -> compile static `tanpopo` binary -> copy into `nonroot` `UID 65532` base image containing no `/bin/sh` or package manager to eliminate post-exploitation shell surfaces).
  - `deploy/compose/docker-compose.standalone.yml` (`read_only: true`, `cap_drop: ["ALL"]`, `security_opt: ["no-new-privileges:true"]`, dedicated `/var/lib/tanpopo` volume).
  - `deploy/systemd/tanpopo.service` (`DynamicUser=yes` or dedicated service account, `ProtectSystem=strict`, `ProtectHome=yes`, `NoNewPrivileges=yes`).
  - `.github/workflows/release-dry-run.yml` & `scripts/release/verify-artifacts.sh` (`CycloneDX SBOM` generation, `govulncheck` + `trivy` vulnerability scanning at `0 Critical/High`, ephemeral `Cosign` test-key signing in CI — **strictly without publishing public releases**).
- **Out-of-Scope:** Multi-replica role-split Kubernetes `Helm Chart` for HA deployments (covered in `TP-IMP-0040` in `M4`); actual public release publishing (restricted exclusively to `TP-IMP-0047` upon explicit Product Owner sign-off).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-NFR-0103`, `TP-NFR-0901`, `SLO-05`, `SPEC-OPS-01`, `SPEC-SEC-02`, `GATE-REL-02`, `GATE-REL-04`.
- **Dependencies:** `TP-IMP-0032`.
- **Design Refs:** [`installer-upgrades.md`](../../operations/installer-upgrades.md) (`SPEC-OPS-01`), [`plugin-sandbox-and-supply-chain.md`](../../security/plugin-sandbox-and-supply-chain.md) (`SPEC-SEC-02`), [`release-readiness.md`](../../quality/release-readiness.md) (`GATE-REL-02`, `GATE-REL-04`).

## 4. API, Wire & Data Contracts
- **Cold-Start Resource Budget (`TP-NFR-0103` / `SLO-05` — `TARGET / UNVERIFIED`):** Smoke tests empirically measure packaged binary startup to `/readyz = 200` within `< 3 seconds` (`< 2s` target) and idle RSS memory footprint under `< 128 MiB`.
- **Rootless Container Invariant:** The OCI image defaults to `65532:65532` (refusing privileged `UID 0` execution in production mode) and operates cleanly with a read-only root filesystem (`--read-only`).

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1` (`GATE-REL-02` Multi-OS Cross-Build):** Running a local snapshot build (`goreleaser build --snapshot --clean`) produces all 5 Linux/macOS/Windows binaries without requiring any C compiler (`gcc`/`clang`).
- **`AC-2`:** Building the OCI image and launching with `docker run --read-only --cap-drop=ALL --security-opt=no-new-privileges:true` passes `/healthz`, `/readyz`, Web Console asset serving, and the `EXT-ACCEPT-01` smoke suite at `100%`.
- **`AC-3` (`GATE-REL-04` Apache-2.0, SBOM & Cosign):** Generates `sbom-go.cdx.json` + `sbom-web.cdx.json`, verifies zero copyleft `AGPL/GPL` dependency contamination, and passes `cosign verify-blob` verification.
- **`NEG-1`:** Attempting to spawn `/bin/sh` inside the production container image fails deterministically because the distroless static image ships no shell binary.

## 6. Test Commands & Required Evidence
- Cross-platform verification (Go driver + container check): `go run ./scripts/ci/check.go --verify-artifacts` *(or `bash scripts/release/verify-artifacts.sh` on Linux/macOS CI)*.

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** CI workflows in this unit perform `dry-run` artifact verification on PRs/branches only — **automated public GitHub Releases or official tag pushes are strictly prohibited prior to `TP-IMP-0047`**.
- **Rollback:** Immutable digest-pinned container images and archives per commit.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0032` and all `Milestone M2` units (`0016`..`0032`) are complete.
- **DoD:** All Gate `GATE-M2` (`MVP v0.1`) exit criteria satisfied, `0` `Critical/High` vulnerabilities, empirical cold-start & idle RSS measurements recorded, and `REQUIRES_STRONG_REVIEW` sign-off achieved.
