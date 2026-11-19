# AGENTS.md

Project-specific guidance for AI coding agents.

<!-- ASTRYX:START -->
Astryx v0.7.16 · 174 components
CLI: run every command as `npx astryx <cmd>` (shown below as `astryx ...`).

SETUP (once, in app entry e.g. main.tsx) — without these, components render unstyled:
  import "@astryxdesign/core/reset.css";
  import "@astryxdesign/core/astryx.css";

WORKFLOW — start every page from a template, never from scratch:
1. `astryx build "<idea>"` — names the template to scaffold from and the parts it lacks.
2. `astryx template <name> <path>` — scaffold it; keep its frame, gap and padding; replace the content.
3. `astryx template <Block>` for parts it lacks; `astryx component <Name>` to read props before using one.
Changing an existing page? Keep it: skip step 2 and add blocks/components inside its sections.
Before you deliver a build, run `astryx doctor --built <dir>`; don't deliver it if it fails.

RULES:
- No <div> — components handle all layout and spacing.
- Read `astryx docs layout` before changing a template's frame.
- Dense data = rows (Table, List/Item), not Card-wrapped lists. Badge = counts only.
- Style with component props first, then tokens (var(--color-*|--spacing-*|--radius-*)). No raw hex/px. No StyleX/Tailwind compiler here — don't use xstyle/utility classes directly. Some templates use StyleX and need a compiler plugin; see `astryx docs styling-overview`.
- THEMES: palettes and custom colors go through the theme, never :root overrides. To make or change a theme (colors, fonts, corners, spacing, a look from a picture), prefer the experimental theme editor over hand-written theme files: `astryx experimental theme editor new <name> --like neutral`, then `editor set <name> <thing> <value>` or `editor how "<what you want>"`; wire it with `astryx theme add <name>`; after each change look at it with `astryx experimental theme editor show <name> --png` and open the PNGs it writes (changes nothing in the app); a theme change is not done until you have looked at the pictures. In code, extend an imported theme to customize it; `theme eject <slug>` only forks source. See `astryx docs theme`.
- SELF-CHECK: re-read the file; replace any raw <div>/<span>, imported .css/@apply, or hardcoded #hex/px with the component or a token.

`astryx help` lists every command. Key ones beyond the workflow:
  search "<query>"   find any component / hook / doc / template
  discover <words>   integrations you could add
  docs <topic>       getting-started, principles, tokens, theme … (`astryx docs` lists all)
  docs cli           commands and API reference
  docs authoring     build an integration package, and every field it holds
  upgrade --from <old version> --apply   run after a dependency bump
<!-- ASTRYX:END -->

<!-- TANPOPO-HANDOFF:START -->
## Tanpopo Cold-Start Agent Navigation & Governance Gate (Gate G4 Stage 2 Approved)

**Current Product Owner directives (`DEC-060`, `DEC-072`):** **GPT-6 Luna** completed Units `TP-IMP-0031`–`0035` under `DEC-060`; `TP-IMP-0035` is DONE on `origin/main` `0a264c40ebb7288d849df14a466931cae2b1fab5` with post-merge CI `44468902234` and artifact/trusted-main attestation `44468902362` green. Under `DEC-072`, Luna is delegated technical decisions, implementation, independent review coordination, sequential acceptance, governance synchronization, and PR merge for Units `TP-IMP-0036` → `0037` → `0038`. Unit 36 is READY; Units 37–38 unlock only after the prior unit is DONE on `main` with green acceptance CI and their own DoR passes. Use persistent `develop` → per-unit PR → review at the spec tier + four required CI checks → merge → verify main SHA. No force-pushes, tags, or releases (`GATE-REL-07`).

If you are starting a fresh session with no prior chat context (e.g., executing a `TP-IMP-xxxx` unit):
1. **Read [`docs/governance/execution-baseline.md`](./docs/governance/execution-baseline.md) FIRST** (`EXEC-BL-001-APPROVED`).
   - Current Delivery Handoff Verdict: **`GO`** (`0001`..`0035` DONE; `0036` READY under `DEC-072`; `0037`..`0047` BLOCKED). Preserve `snapshot.Manager` `CheckServingReady()` and explicit `BlindHostCredentialBroker` `Config.PolicyResolver` wiring for credentialed connections.
   - Execute only the single current unit marked `READY`; use `develop` and PRs as specified by the active delegation (`DEC-060` or `DEC-072`). Accept units only within the active delegation and only after the required reviews and main CI pass. Never tag or publish releases (`GATE-REL-07`).
2. **Master Unit Index & Specs:** [`docs/planning/implementation-index.md`](./docs/planning/implementation-index.md) → `docs/planning/units/TP-IMP-xxxx.md`.
3. **Contracts & Spike Evidence (`M0`):** [`proto/tanpopo/extension/v1/runtime.proto`](./proto/tanpopo/extension/v1/runtime.proto), [`openapi/`](./openapi), [`db/spec-check/`](./db/spec-check), and [`docs/architecture/spike-results.md`](./docs/architecture/spike-results.md).
4. **Audit, Gap & Documentation Hubs:** [`docs/reviews/execution-readiness.md`](./docs/reviews/execution-readiness.md), [`docs/reviews/unresolved-gaps.md`](./docs/reviews/unresolved-gaps.md), [`README.md`](./README.md) & [`docs/README.md`](./docs/README.md).
<!-- TANPOPO-HANDOFF:END -->

## Repository Workspace Paths & Quality Contract (`TP-IMP-0005`)

- Read [`CONSTRAINTS.md`](./CONSTRAINTS.md) before writing code. Do not weaken it to make a change pass.
- Web Console package root: [`web/console/`](./web/console/) (`package.json`, `package-lock.json`, `astryx-themes.js`, `AGENTS.md`, `GEMINI.md`, `src/`). Run Node/Astryx commands inside `web/console/` after `npm ci --ignore-scripts` via `npm run astryx -- <cmd>` (unified local CLI alias per `@astryxdesign/cli@0.7.16` *Working with AI* guide; e.g. `npm run astryx -- doctor`) or `npx --no -- @astryxdesign/cli <cmd>` (on Windows PowerShell: `cmd /c npx --no -- @astryxdesign/cli doctor` or `npm run astryx -- doctor`). Keep `neutral` unmodified (never `theme eject`), zero `.css` files in `web/console/src/` (place local `Figtree` font CSS/assets outside `src/` under `web/console/assets/fonts/` in `TP-IMP-0026`), and `0` external runtime requests.
- Cross-platform quality gate: run `go run ./scripts/ci/check.go --all` and `go test -v ./...` (`CGO_ENABLED=0`), plus `go test -v -race ./...` in environments with a C toolchain (`ubuntu-24.04`, `macos-15`).

