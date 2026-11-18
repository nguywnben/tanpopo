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
- The remediation rebuilds the Console from the locked npm graph before Go packaging, checks the live `EXT-ACCEPT-01` suite and Console from the nonroot container, verifies the file-backed secret under UID/GID `65532`, measures the Tanpopo process `VmRSS`, disables Cosign transparency-log uploads and signs/verifies a local SLSA provenance statement. CycloneDX now fails closed and reads the Console manifest explicitly. Building the UI also found and corrected the missing OpenAPI CSRF header declaration on `POST /admin/v1/provider-adapters`.
- Local checks after those changes: `npm --prefix web/console run build` passed (initial JS gzip `123.90 KB`); the live `TestEXTAccept01_` integration suite passed; CycloneDX npm generated a nonempty BOM with `262` components; GoReleaser config validation, shell syntax and `git diff --check` passed. Container secret/RSS smoke and final CI/reviews remain pending on the updated PR diff.
