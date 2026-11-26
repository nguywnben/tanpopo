# CP97 — CLI provenance, GitOps reconcile, and operational preflight

**Source baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`
**Product changes:** none; local toolchain/cache/binary and test config were placed under `/tmp`
**QA runtime:** `18084`, health and readiness returned HTTP 200

## Toolchain and build provenance

The shell's `/usr/bin/go` was a Go-board game, not the compiler. I downloaded Go 1.26.11 to `/tmp`, verified its SHA-256 against the official `go.dev` release metadata, and restored dependencies into `/tmp/qa002-go-mod-cache` (`go mod download` succeeded). A local CLI was built from the frozen source with explicit linker values for `version=0.1.0-dev`, commit `40bba5b55c2da6fc00b107b315c9b3167fb20169`, and build date `2026-11-24T10:23:56Z`. Go's automatic VCS stamping initially failed; building with `-buildvcs=false` and the explicit linker metadata succeeded. Running `version --output=json` returned those exact values.

This validates this QA-built source binary's provenance. It does not verify the separate RC4 artifact previously left in Hydrogen's DevBox.

## Actual CLI journeys

1. `tanpopo --help`, `admin --help`, and feature command help rendered the command tree and localized flags.
2. `config reconcile` applied the current empty QA config and lockfile to a temporary `/tmp` path. It returned `APPLIED` and left source, runtime database, and runtime lockfile untouched.
3. A malformed desired YAML was reconciled against a pre-existing temporary applied file containing a sentinel. CLI exited 14, returned no result payload, and reported that applied config/lockfile were left unchanged. The sentinel persisted unchanged.
4. Vietnamese `config show` with `TANPOPO_URL=http://127.0.0.1:18084` but no `TANPOPO_ADMIN_TOKEN` exited 14 with the localized missing-token message; no remote request was possible.
5. `doctor --root /tmp/tanpopo-qa002-data --output=json` returned exit 2: `clock-skew=warn`, `ports-available=fail`, `master-kek-ready=fail`, `storage-fs-lock=pass`, `cluster-cache=skipped`, `bundle-lockfile-integrity=pass`, `tls-posture=warn`, and `egress-dns-ssrf=pass`. Health/readiness remained 200. The port check overlaps already-running isolated services; the separate CLI shell has no service Master KEK binding. These are environment/context mismatches and are not classified as product defects.

Evidence: [sanitized CLI result](../evidence/CP97-cli-provenance-gitops-doctor.json). No credentials or database contents are included.

## Results and gaps

| Area | Result | Limits |
| --- | --- | --- |
| Source build metadata | PASS for local QA build | Does not validate the RC4 binary from Hydrogen's separate DevBox. |
| CLI help/version | PASS | Output formatting and other locales remain partial. |
| GitOps reconcile | PASS for empty config/lockfile and malformed-input atomicity | Non-empty resources/extensions, permission failures, process interruption and Windows remain UNVERIFIED. |
| `doctor` | BLOCKED / inconclusive in this shell context | Port/KEK findings reflect live services and missing shell credential bindings; not evidence that the serving process is unhealthy. |
| Remote Admin CLI | BLOCKED | Cloud runtime status exposes QA provider credentials but no `TANPOPO_ADMIN_TOKEN`; configured owner login is a browser session, not a CLI bearer credential. Live CLI reads remain UNVERIFIED. |

## Consolidated defect/status summary

- **Historical DEF-001–015:** CP97 did not retest them. Preserve limited scoped QA-002 outcomes: DEF-001–004, DEF-006, DEF-008–015 have passing paths; DEF-005 remains partial; DEF-007's successful audit paths do not close failure atomicity/populated erasure. No QA-001 PASS is inherited; full acceptance remains incomplete.
- **Historical open DEF-016–035:** remain open; no disposition changed.
- **New defects:** none.
- **Coverage completed:** Go toolchain repair, dependency resolution, source-build provenance, CLI help/version, safe local reconcile success/failure atomicity, localized missing-auth handling, and a diagnostic preflight compared with live health/readiness.
- **Coverage blocked:** remote CLI Admin API journeys need a supported QA `TANPOPO_ADMIN_TOKEN`; native passthrough still needs its enabled Data Plane ingress address/configuration. RC4 artifact provenance remains unverified in the unavailable DevBox.
- **Coverage remaining:** all prior open product/features/roles/platforms/integrations and defects; non-empty GitOps reconcile and recovery; live Admin CLI with a dedicated short-lived QA credential; native passthrough fidelity; full DEF-001–015 acceptance.
- **Recommended handoffs:** no new defect handoff. Keep DEF-035 with DEF-023/024/029/030, and prior DEF-034/033/005/007 priorities.
- **Release gate:** `GATE-REL-07` remains **BLOCKED**.
