# `TP-IMP-0045` — `Air-Gap Bundle` Offline Supply-Chain Tooling (`bundle export/verify/import`) & `Multi-Org GA` Console Governance

| Attribute | Value |
|---|---|
| **Unit ID** | `TP-IMP-0045` |
| **Milestone / Epic** | `M5` (`v1.0 GA Hardening, Docs & Release`) · `EP-09` |
| **Status** | `BLOCKED` *(Depends on `TP-IMP-0035`, `TP-IMP-0040`, `TP-IMP-0041` — `GAP-01, 12`)* |
| **Size** | `M` (`internal/cli/bundle/*`, `web/console/src/shell/OrgSwitcher.tsx`, `web/console/src/pages/orgs/*`) |
| **Review Tier** | **`REQUIRES_STRONG_REVIEW`** *(Offline supply-chain signature verification `CT-31` & Multi-Organization UI context isolation)* |
| **Technical Owner** | `Staff Enterprise Platform & Frontend Lead` |

---

## 1. Intent
Deliver the two enterprise scale-out capabilities scheduled for `v1.0 GA` per [`release-scope.md`](../../product/release-scope.md), [`installer-upgrades.md`](../../operations/installer-upgrades.md), and `CR-04`:
1. **Zero-Egress `Air-Gap Offline Bundle` Tooling (`tanpopo bundle export / verify / import`, `CT-31`, `OPS-AC-11`, `GATE-REL-06`):** Enable infrastructure teams in a connected staging zone to package binaries/container images, digest-pinned `.tpkg` extensions, `index.json` + `revocations.json` feeds, `Trust Root` public keys, and `SBOM` manifests into a single cryptographically signed `.tpbundle` archive; transfer it across an air-gap diode into a defense/financial enclave **with zero outbound Internet or external DNS access**; and verify signatures offline to install and operate at `100%` parity.
2. **Full `Multi-Organization Console UX` on Shared Clusters (`BA-OQ-001`):** Activate the top-bar `Organization Switcher` inside `AppShell` (progressive disclosure: rendered only when the principal belongs to `> 1` organization or holds the instance-scoped `OPERATOR` role, preserving a clutter-free header for default single-org installs), flushing all client-side query caches instantaneously on every `Org` context switch.

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `internal/cli/bundle/export.go`, `verify.go`, `import.go` (offline archive bundling, zero-socket cryptographic signature verification, and local extension catalog seeding).
  - `web/console/src/shell/OrgSwitcher.tsx` & `web/console/src/pages/orgs/OrganizationsAdminPage.tsx` (scaffolded from Astryx templates, invoking `queryClient.clear()` and resetting `Project / Env` context atomically upon switching `Org`).
  - Network-disabled container integration test (`docker run --network=none`) verifying installation, Web Console rendering (backed by local `Figtree` font assets from `TP-IMP-0026`), and live inference against an enclave-local model server.
- **Out-of-Scope:** Any net-new feature additions beyond the locked `v1.0 GA` scope.

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0307`, `TP-FR-0501`, `BA-OQ-001`, `CT-31`, `OPS-AC-11`, `UX-AC-10`, `CR-04`.
- **Dependencies:** `TP-IMP-0035`, `TP-IMP-0040`, `TP-IMP-0041`.
- **Design Refs:** [`installer-upgrades.md`](../../operations/installer-upgrades.md) (`SPEC-OPS-01` Section 6), [`information-architecture.md`](../../ux/information-architecture.md) (`SPEC-UX-01`), [`contract-test-matrix.md`](../../quality/contract-test-matrix.md) (`CT-31`).

## 4. API, Wire & Data Contracts
- **100% Offline Verification Invariant (`CT-31`):** `tanpopo bundle verify` and `tanpopo bundle import` execute under strict zero-network-socket guards — all certificate chains, public keys, and `revocations.json` advisories required for signature verification are self-contained inside `.tpbundle` and validated against the host's pinned trust root.
- **UI State Isolation Across `Org` Switches:** Switching from `Org A` to `Org B` immediately purges all in-memory browser query caches, scoped URL parameters, and active `Org A` ephemeral `Playground` session keys.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1` (`CT-31` & `OPS-AC-11` True Zero-Egress Air-Gap Install):** Inside a fully network-isolated enclave with zero external routes, running `tanpopo bundle verify ./release.tpbundle && tanpopo bundle import ./release.tpbundle` boots the Gateway, loads the Web Console cleanly (`0` external font/asset errors), and serves enclave-local inference at `100%`.
- **`AC-2` (`Multi-Org Progressive Disclosure`):** Single-`Org` installations retain a minimal header; users belonging to both `Org A` and `Org B` can switch seamlessly between organizations with `0` cached cross-org data remnants in the UI.
- **`NEG-1` (`Tampered Air-Gap Bundle`):** Flipping 1 byte inside an embedded `.tpkg` within `.tpbundle` causes `tanpopo bundle verify` to detect the digest/signature mismatch immediately and abort import (`exit code 22`).
- **`NEG-2` (`UX-AC-10`):** Running with network interfaces completely disabled confirms `0` external browser requests, and `npx @astryxdesign/cli doctor --built dist` passes with `0 fail`.

## 6. Test Commands & Required Evidence
- Pure Go (`CGO_ENABLED=0`): `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -v ./internal/cli/bundle/...` *(PowerShell: `$env:CGO_ENABLED='0'; $env:TANPOPO_TEST_HERMETIC='1'; go test -v ./internal/cli/bundle/...`)*
- Race detector (`-race`): `go test -v -race ./internal/cli/bundle/...`
- `cd web/console && npm run typecheck && npm run test && npm run build && npx astryx doctor --built dist`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Provisioning new `Organization` tenants on a shared cluster is restricted strictly to instance-level `OPERATOR` principals (or declarative GitOps config), preventing single-tenant admins from spawning arbitrary organizations.
- **Rollback:** Clusters can be locked to single-organization mode via `tenancy.multi_org_enabled = false`.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0035`, `TP-IMP-0040`, and `TP-IMP-0041` are complete.
- **DoD:** `CT-31`, `OPS-AC-11`, and `UX-AC-10` passing, `astryx doctor --built dist` at `0 fail`, and `REQUIRES_STRONG_REVIEW` sign-off achieved.
