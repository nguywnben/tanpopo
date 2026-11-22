# Contributing to Tanpopo

Thank you for contributing to **Tanpopo**! Before opening a pull request, please read [`CONSTRAINTS.md`](./CONSTRAINTS.md), [`AGENTS.md`](./AGENTS.md), and the compliance rules below.

---

## Development Environment Setup

### Prerequisites
- **Go `1.26+`** (`CGO_ENABLED=0` pure-Go builds must always succeed; C toolchain required when running `go test -race`)
- **Node.js `22.x`** & `npm` (used inside `web/console/`)
- Optional for full cluster integration suites: Docker (for PostgreSQL 16+, 3-shard Valkey 8 Cluster, and `grafana/k6`)

### 1. Clone & Verify Baseline Gates

```bash
git clone https://github.com/nguywnben/tanpopo.git
cd tanpopo

# Cross-platform Floor Guard, DAG, license, canary, UI-layout & CGO_ENABLED=0 build check
CGO_ENABLED=0 go run ./scripts/ci/check.go --all

# Documentation, OpenAPI, CLI help & INC-01..07 drift check
CGO_ENABLED=0 go run ./scripts/docs/check_drift.go

# Go unit tests (hermetic offline mode, zero external API calls)
CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -short ./...
```

### 2. Race Detector & Integration Suites

```bash
# Go race detector (CGO_ENABLED=1 implicit default on Linux/macOS)
TANPOPO_TEST_HERMETIC=1 go test -race -short ./...
```

### 3. Web Console Workflow (`web/console/`)

All Web Console assets build cleanly offline and are embedded into the Go binary via `internal/webui/dist/`:

```bash
cd web/console
npm ci --ignore-scripts
npm run typecheck
npm run check:i18n
npm test
npm run build
npm run sync:embed
```

> **Always run `npm run build && npm run sync:embed` before committing Web Console changes** so `internal/webui/dist/` stays byte-synchronized with `web/console/src/`.

---

## Non-Negotiable Quality & Compliance Rules

1. **Pure-Go Portability & DAG Order:** Never introduce CGo requirements into default builds, and follow the 19-package acyclic DAG tiers enforced by [`scripts/ci/check.go`](./scripts/ci/check.go).
2. **Floor Guard (`CONSTRAINTS.md`):** Never suppress checks with `@ts-ignore`, `eslint-disable`, `//nolint:gosec`, `t.Skip()` workarounds, or empty `catch {}` blocks.
3. **Astryx Neutral & Accessibility (`AGENTS.md`):**
   - Keep Web Console copy in `web/console/src/i18n/locales/*.json` (15 languages, `en` canonical source).
   - Never apply `overflow-x: hidden` or `overflow-x: clip` to `html` or `body`.
   - Maintain `<= 250 KB` gzip initial JS budget and `0` runtime external CDN requests.
4. **Apache-2.0 Licensing (`GATE-REL-04`):** Only Apache-2.0 compatible dependencies (zero GPL/AGPL/SSPL crates or packages). Include the Apache-2.0 header on new source files.
5. **Commercial API & Self-Hosted Boundary (`DEC-004`):** Do not submit adapters, scripts, or docs that scrape or repurpose personal consumer subscriptions (`ChatGPT Plus/Pro`, `Claude Pro/Max`, etc.).
6. **Branch Workflow:** Target pull requests against `develop` (or follow active release unit governance when targeting `main` from `develop`). Ensure all local gates pass before pushing.
