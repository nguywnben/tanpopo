# TP-IMP-0033 Execution Record

## Current checkpoint

- Unit: `TP-IMP-0033`
- Governance state: `READY` under `DEC-062`
- Implementation branch: persistent `develop`
- DoR: verified; Unit 32 and all M2 units `0016`..`0032` are DONE on `main` with green acceptance CI.
- Implementer: **GPT-6 Luna**.
- Review tier: `REQUIRES_STRONG_REVIEW`; final independent **code-reviewer** and **security-auditor** reviews will be recorded here after implementation stabilizes.
- No release or tag will be created; artifact workflow is dry-run only under `GATE-REL-07`.

## Scope and evidence

- Implement the five specified Go binary targets, a rootless digest-pinned OCI image, hardened standalone Compose and systemd assets, Go and npm CycloneDX SBOM generation, vulnerability scanning, and ephemeral Cosign sign/verify.
- The PR must verify `/healthz` and `/readyz` from the read-only nonroot image, shell absence, Compose/systemd configuration, and empirical cold-start/RSS targets.
- CI run, implementation SHA, measured cold-start/RSS, review verdicts, and post-merge acceptance SHA/run will be added after those results are available.

## Review and CI checkpoint

- The first PR check run [`44383355668`](https://github.com/nguywnben/tanpopo/actions/runs/44383355668) passed all four required Stage 0/Stage 1 jobs on implementation SHA `d3e25e92546fd58c1878df5887600cba961e7041`. Release dry-run [`44383355865`](https://github.com/nguywnben/tanpopo/actions/runs/44383355865) exposed a real working-directory bug: CycloneDX npm was invoked from the repository root without `web/console/package.json`.
- Initial independent reviews on that checkpoint: **Descartes** (**code-reviewer**) requested changes for stale embedded Console assets, unreadable Compose secret ownership, missing Console/`EXT-ACCEPT-01` acceptance coverage and inaccurate RSS measurement; **Helmholtz** (**security-auditor**) identified possible Cosign transparency-log uploads, missing SLSA provenance, ignored npm SBOM errors, and the same stale UI build. These are remediation findings, not acceptance approvals.
- The remediation rebuilds the Console from the locked npm graph before Go packaging, checks the live `EXT-ACCEPT-01` suite and Console from the nonroot container, verifies the file-backed secret under UID/GID `65532`, measures the Tanpopo process `VmRSS`, disables Cosign transparency-log uploads, and delegates SLSA provenance creation to GitHub OIDC with workflow/digest verification. CycloneDX now fails closed and reads the Console manifest explicitly. Building the UI also found and corrected the missing OpenAPI CSRF header declaration on `POST /admin/v1/provider-adapters`.
- `TestEXTAccept01_LiveBootstrapAndExtensionInstallApproval` exercises bootstrap, login, install/approval, permission escalation/re-consent, rollback, quarantine, four-scope blast radius, and process/binary invariants on the live Go binary. `TestEXTAccept01_LiveEURoutingAndSettlement` covers the EU route and exact/estimated ledger paths. The older `TestEXTAccept01_FullLifecycleProcessContract` is only a route/process probe and is not the sole evidence for those behaviors; the artifact gate runs all `TestEXTAccept01_*` tests.
- Local checks after remediation: `npm --prefix web/console run build` passed (initial JS gzip `123.90 KB`); the live `TestEXTAccept01_` integration suite passed; CycloneDX npm generated a BOM with `262` components; CycloneDX Go with license detection generated `39` components; both BOMs passed the copyleft/unknown-license checker; Go quality gates, GoReleaser config validation, Compose override parsing, Cosign local signature verification, shell syntax and `git diff --check` passed. The image-backed secret/RSS smoke and GitHub OIDC attestation are pending the new PR run.

### Second review round

- **Arendt** (**code-reviewer**) approved the corrected UI build and generated API contracts, but requested more proof that the full ten-step `EXT-ACCEPT-01` suite and license scan are enforced by the artifact gate. The two live suites above provide the lifecycle/routing evidence; the release script invokes the full `TestEXTAccept01_*` selection and now license-checks every SBOM component.
- **Halley** (**security-auditor**) rejected the locally self-asserted SLSA statement because it did not authenticate the builder or inventory build inputs. That statement has been removed. The workflow now uses the pinned GitHub OIDC provenance action on `dist/SHA256SUMS` and verifies the attested subject and signer workflow. Final review on the resulting diff is pending.
