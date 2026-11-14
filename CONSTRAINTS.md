# Constraints (`CONSTRAINTS.md`)

> Last reviewed: `2026-11-14` · Governs all Tanpopo Core (`Go >= 1.24`), Web Console (`web/console/`), and CI changes (`SPEC-QA-04`, `TP-IMP-0005`).
> Do not weaken rules, remove checks, or add unapproved suppressions to make a change pass.

## Floor (always enforced, `scripts/ci/check.go`)

- No new suppression comments or security bypasses: `@ts-ignore`, `@ts-expect-error`, `@ts-nocheck`, `eslint-disable`, `biome-ignore`, `//nolint:gosec`, `nosemgrep`, `# gitleaks:allow`, `Stryker disable`, `istanbul ignore`
- No skipped or isolated tests without an approved ticket: `t.Skip(`, `.skip(`, `.only(`, `xit(`, `xdescribe(`, `@pytest.mark.skip`
- No unfinished stubs or empty catches: `throw new Error("not implemented")`, `panic("not implemented")`, empty `catch {}`
- No `http.DefaultClient`, `http.DefaultTransport`, `http.Get`/`Post`/`PostForm`/`Head`, `&http.Client{}` without non-nil `Transport`, `&http.Transport{}` without non-nil `DialContext`, bare `net.Dial`/`DialTimeout`/`DialTCP`/`DialUDP`/`DialIP`, `tls.Dial`/`DialWithDialer`, `&tls.Dialer{}` without non-nil `NetDialer`, `&net.Dialer{}` without non-nil `Control`, or zero-value `var`/`new()` allocations of `http.Client`/`http.Transport`/`net.Dialer`/`tls.Dialer` inside `cmd/`, `internal/`, or `tests/` outside the strictly scoped `internal/testkit/netguard` exceptions (`E3`/`E4`) (all network egress must route through injected SSRF-guarded / `netguard`-guarded clients and dialers)
- No imports from `github.com/nguywnben/tanpopo/tools/spikes` inside `cmd/` or `internal/`, and zero production imports of `internal/testkit` outside `_test.go`
- No raw HTML `<div>` or `<span>` elements, custom `.css` files, `:root` overrides, `theme eject` edits to `web/console/astryx-themes.js`, or non-English copy inside `web/console/src/`
- No secrets or canary tokens in source files (`sk-tp-canary-*`, `sk-tp-live-*`, private PEM keys) and never log matched secret values
- No competing JavaScript lockfiles (`yarn.lock`, `pnpm-lock.yaml`, `bun.lock`, `bun.lockb`) and never run `npm ci` without `--ignore-scripts`
- No copyleft (`GPL`, `AGPL`, `SSPL`) dependencies (`.agents/plugin.json` metadata is explicitly allowlisted under its upstream `MIT` license compatible with `Apache-2.0`)
- This file (`CONSTRAINTS.md`) does not get weakened to make a change pass

## Enforced with numbers (`Stage 0 / Stage 1 CI` & local verification)

| Dimension | Rule | Checked by | Runs at |
|---|---|---|---|
| Pure-Go static binary | Builds with `0` errors under `CGO_ENABLED=0 -mod=readonly -trimpath` | `go run ./scripts/ci/check.go --pure-go-build` | every task end, Stage 1 CI |
| One-way package DAG | `0` import cycles; strict layer order `pkg <- storage <- security <- config <- domain <- edge <- cli <- cmd` | `go run ./scripts/ci/check.go --dag` | every task end, Stage 0 CI |
| Floor Guard & stubs | `0` banned suppressions, skips, unguarded `net/http`/`net.Dial`/`Dialer` defaults, `tools/spikes` imports, or raw HTML `div`/`span` | `go run ./scripts/ci/check.go --floor-guard` | every task end, Stage 0 CI |
| Licensing & lockfiles | Root `LICENSE` + `web/console/package.json` aligned to `Apache-2.0`; `0` GPL/AGPL/SSPL deps; single `package-lock.json` | `go run ./scripts/ci/check.go --license --lockfiles` | every task end, Stage 0 CI |
| Astryx UI baseline | `defaultThemeSlug = "neutral"` unmodified; `0` failures in doctor check; English UI strings | `go run ./scripts/ci/check.go --ui-layout` and `cd web/console && npx @astryxdesign/cli doctor` | every UI edit, Stage 0/1 CI |
| Canary & secret hygiene | `0` unredacted secret or canary leaks in `cmd/`, `internal/`, `scripts/`, `web/console/` | `go run ./scripts/ci/check.go --canary` | every edit, Stage 0 CI |
| Go unit tests (pure-Go) | `100%` pass rate with `CGO_ENABLED=0` and `TANPOPO_TEST_HERMETIC=1` | `go test -v ./...` | every task end, Stage 1 CI |
| Go race detector | `100%` pass rate with `-race` on C-toolchain environments (`ubuntu-24.04`, `macos-15`) | `go test -v -race ./...` | Stage 1 CI (Linux/macOS) |
| Machine-readable contracts | `runtime.proto` compiles (`0` errors); OpenAPI 3.1 `$ref`s resolve; SQLite DDL executes cleanly | `cd tools/spikes && go test -count=1 -run TestContract\|OpenAPI\|SQLite ./...` | Stage 0 CI |

## Targets (`TARGET / UNVERIFIED` — enforced progressively in `M1`–`M5` per `COND-PO-01`)

| Metric | Target Bar | Activating Unit |
|---|---|---|
| Security-critical package line coverage (`vault`, `sandbox`, `policy`, `ledger`, `mcp`) | `>= 85%` | `TP-IMP-0008` .. `TP-IMP-0044` |
| Web Console initial compressed JS bundle | `<= 250 KB` gzip | `TP-IMP-0026` |
| TypeScript strict check (`tsc --noEmit`) & Playwright `axe-core` `WCAG 2.2 AA` | `0` errors / `0` critical/serious violations | `TP-IMP-0026`, `TP-IMP-0044` |
| Live PostgreSQL 16 DDL, monthly partitioning & `FORCE RLS` | `0` DDL errors / `0` cross-tenant rows leaked | `TP-IMP-0039` |
| End-to-end Gateway `SLO-01`..`SLO-08` on `2 vCPU / 2 GiB RAM` | `p95 < 2 ms` (T1), `< 5 ms` (T2), `< 1 ms/chunk` | `TP-IMP-0043` |

## Exceptions

| ID | Rule | Path | Reason | Owner | Expires |
|---|---|---|---|---|---|
| `E1` | Upstream `MIT` metadata | `.agents/plugin.json` | Upstream `addyosmani/agent-skills` manifest (`MIT`, compatible with `Apache-2.0` — `ISS-007`). | `Platform Lead` | `None (tooling metadata)` |
| `E2` | Canary placeholder env literal | `.github/workflows/ci.yml` | Deliberately seeds `CANARY_FORBIDDEN_CI_KEY` env vars for `CanaryScanner` trap (`SPEC-QA-02`). | `CI/Security Lead` | `None (hermetic CI trap)` |
| `E3` | Scoped `http.DefaultTransport` / `http.DefaultClient` hardening | `internal/testkit/netguard/netguard.go` | Installs hermetic loopback wrapper onto `http.DefaultTransport` and `http.DefaultClient` under `TANPOPO_TEST_HERMETIC=1` (`DEC-012` / `CR-11`); `http.Get` & unguarded `net.Dial` remain banned here. | `Product Owner (`DEC-012`)` | `None (hermetic test guard)` |
| `E4` | Direct stdlib bypass negative tests (`NEG-1`) | `internal/testkit/netguard/netguard_default_test.go` | Dedicated test file verifying `netguard` intercepts `http.Get`, `http.DefaultClient`, bare `&http.Client{}`, and Linux `unshare -n` blocks raw-IP `net.Dial` (`DEC-012` / `CR-11`). | `Product Owner (`DEC-012`)` | `None (negative test suite)` |
