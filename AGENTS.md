# AGENTS.md

Project-specific guidance for AI coding agents.

<!-- TANPOPO-CONSOLE:START -->
## Tanpopo Web Console v2 (`ADR-0007`, `DEC-086`)

- **Stack:** React 19 + TypeScript 5.9 (strict) + Vite 7 + Layered Semantic CSS (`@layer tokens, base, layout, components` under `web/console/src/styles/`) + `lucide-react` icons + OpenAPI-typed `openapi-fetch` client (`src/api/`). Legacy `@astryxdesign/*` and `@stylexjs/*` packages are retired.
- **Visual Direction:** Warm Daylight Precision (light default with hairline structure, Warm Stone canvas, Ink primary CTA, Tanpopo Ochre active indicator, and Obsidian Dark mode via `data-theme="dark"`).
- **Rules:**
  - Zero nested cards, zero decorative badges (`CountPill` for numeric counts, `StatusPill` for semantic icon+label status only), concise single `<h1>` per page, and `tabular-nums` monospace for IDs, hashes, durations, and integer micro-USD / nano-USD amounts.
  - **Zero horizontal scrollbar at page level** (`document.documentElement.scrollWidth <= document.documentElement.clientWidth`) across `320`, `375`, `768`, `1024`, `1440`, and `1920` CSS px. **Never** use `overflow-x: hidden` or `overflow-x: clip` on `html` or `body` (enforced by `scripts/ci/check.go --ui-layout`).
  - **Air-Gap / Offline:** Bundle local `Figtree` fonts (`web/console/assets/fonts/`) and tree-shaken SVG icons; `0` external runtime requests or CDN URLs.
  - **English-Only UI Copy (`ISS-008`) & Strict Accessibility:** Meet `WCAG 2.2 AA` (`0` `axe-core` violations), preserve all CSRF (`X-CSRF-Token`) and optimistic concurrency (`If-Match`) API guards, and keep initial JS bundle `<= 250 KB` gzip.
- **Commands (inside `web/console/` after `npm ci --ignore-scripts`):**
  - `npm run typecheck` — strict TypeScript check (`tsc --noEmit`)
  - `npm test` — Vitest + Testing Library + `axe-core` accessibility suite
  - `npm run build` — regenerate OpenAPI types/policy, Vite build, generate `.gz` siblings, and verify `<= 250 KB` gzip initial JS budget
  - `npm run sync:embed` — copy `web/console/dist/` into `internal/webui/dist/` for `go:embed` single-binary builds
<!-- TANPOPO-CONSOLE:END -->

<!-- TANPOPO-HANDOFF:START -->
## Tanpopo Cold-Start Agent Navigation & Governance Gate (Gate G4 Stage 2 Approved)

**Current Product Owner directives (`DEC-060`, `DEC-072`, `DEC-086`):** **GPT-6 Luna** completed Units `TP-IMP-0031`–`0038` (`TP-IMP-0038` is DONE on `origin/main` `5ac0293d1c25dd7c69a011037cd0fb3e05e1b527` under `DEC-085`). Under **`DEC-086`**, **Gemini 4 Neon** is authorized to perform a dedicated blank-slate Tanpopo Web Console UX/UI overhaul (`ADR-0007`) on `develop` → PR to `main`, covering all Console workflows through Unit 38 without starting Unit 39 or creating tags/releases (`GATE-REL-07`).

If you are starting a fresh session with no prior chat context (e.g., executing a `TP-IMP-xxxx` unit):
1. **Read [`docs/governance/execution-baseline.md`](./docs/governance/execution-baseline.md) FIRST** (`EXEC-BL-001-APPROVED`).
   - Current Delivery Handoff Verdict: **`GO`** (`0001`..`0038` DONE; `0039`..`0047` BLOCKED pending sequential DoR; dedicated Web Console v2 overhaul active on `develop` under `DEC-086`). Preserve `snapshot.Manager` `CheckServingReady()` and explicit `BlindHostCredentialBroker` `Config.PolicyResolver` wiring for credentialed connections.
   - Never tag or publish releases (`GATE-REL-07`).
2. **Master Unit Index & Specs:** [`docs/planning/implementation-index.md`](./docs/planning/implementation-index.md) → `docs/planning/units/TP-IMP-xxxx.md`.
3. **Contracts & Spike Evidence (`M0`):** [`proto/tanpopo/extension/v1/runtime.proto`](./proto/tanpopo/extension/v1/runtime.proto), [`openapi/`](./openapi), [`db/spec-check/`](./db/spec-check), and [`docs/architecture/spike-results.md`](./docs/architecture/spike-results.md).
4. **Audit, Gap & Documentation Hubs:** [`docs/reviews/execution-readiness.md`](./docs/reviews/execution-readiness.md), [`docs/reviews/unresolved-gaps.md`](./docs/reviews/unresolved-gaps.md), [`README.md`](./README.md) & [`docs/README.md`](./docs/README.md).
<!-- TANPOPO-HANDOFF:END -->

## Repository Workspace Paths & Quality Contract (`TP-IMP-0005`, `ADR-0007`)

- Read [`CONSTRAINTS.md`](./CONSTRAINTS.md) before writing code. Do not weaken it to make a change pass.
- Web Console package root: [`web/console/`](./web/console/) (`package.json`, `package-lock.json`, `AGENTS.md`, `GEMINI.md`, `assets/fonts/`, `scripts/`, `src/`). Always run `npm ci --ignore-scripts`, keep `0` external runtime requests, and run `npm run build && npm run sync:embed` before committing UI updates so `internal/webui/dist/` stays synchronized with `web/console/src/`.
- Cross-platform quality gate: run `go run ./scripts/ci/check.go --all` and `go test -v ./...` (`CGO_ENABLED=0`), plus `go test -v -race ./...` in environments with a C toolchain (`ubuntu-24.04`, `macos-15`).
