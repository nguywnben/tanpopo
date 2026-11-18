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

**Current Product Owner directive (`DEC-059` under `DEC-058`, accepting `TP-IMP-0030` + `CR-13` Option A):** `TP-IMP-0001`..`0030` are **`DONE` (`30 / 47`)** (`DEC-011`..`DEC-048`, `DEC-059`; Unit 30 final implementation SHA `38846e4a27cfd6f795656f248cee1bc1beee2944` on PR #3, PR CI run `44263375798` green across Stage 0 + all 3 OS runners, `code-reviewer` `APPROVE`, `security-auditor` `PASS`). **`TP-IMP-0031`..`0047` (`17 / 47`) remain `BLOCKED`**; Unit 31/32 are not opened and no tags or releases may be created (`GATE-REL-07`).

If you are starting a fresh session with no prior chat context (e.g., executing a `TP-IMP-xxxx` unit):
1. **Read [`docs/governance/execution-baseline.md`](./docs/governance/execution-baseline.md) FIRST** (`EXEC-BL-001-APPROVED`).
   - Current Delivery Handoff Verdict: **`GO`** (`TP-IMP-0001..0030` = `DONE`; **`0 / 47` `READY`**; **`0 / 47` `READY_FOR_REVIEW`**; **`17 / 47` [`TP-IMP-0031..0047`] remain `BLOCKED`** under existing authorization gates). Downstream Data Plane and Control Plane units MUST preserve authentication strictly via `snapshot.Manager` APIs enforcing `CheckServingReady()` rather than bypassing via `Current().AuthenticateKey(...)`, and explicitly wire `BlindHostCredentialBroker` `Config.PolicyResolver` alongside `Config.Resolver` for credentialed connections.
   - Execute **only** a unit explicitly marked **`READY`** in [`docs/planning/implementation-index.md`](./docs/planning/implementation-index.md); execution agents never self-accept units outside explicitly authorized active delegations (`DEC-058` / `DEC-059` covers solely `TP-IMP-0030`). Never tag/publish releases (`GATE-REL-07`).
2. **Master Unit Index & Specs:** [`docs/planning/implementation-index.md`](./docs/planning/implementation-index.md) → `docs/planning/units/TP-IMP-xxxx.md`.
3. **Contracts & Spike Evidence (`M0`):** [`proto/tanpopo/extension/v1/runtime.proto`](./proto/tanpopo/extension/v1/runtime.proto), [`openapi/`](./openapi), [`db/spec-check/`](./db/spec-check), and [`docs/architecture/spike-results.md`](./docs/architecture/spike-results.md).
4. **Audit, Gap & Documentation Hubs:** [`docs/reviews/execution-readiness.md`](./docs/reviews/execution-readiness.md), [`docs/reviews/unresolved-gaps.md`](./docs/reviews/unresolved-gaps.md), [`README.md`](./README.md) & [`docs/README.md`](./docs/README.md).
<!-- TANPOPO-HANDOFF:END -->

## Repository Workspace Paths & Quality Contract (`TP-IMP-0005`)

- Read [`CONSTRAINTS.md`](./CONSTRAINTS.md) before writing code. Do not weaken it to make a change pass.
- Web Console package root: [`web/console/`](./web/console/) (`package.json`, `package-lock.json`, `astryx-themes.js`, `AGENTS.md`, `GEMINI.md`, `src/`). Run Node/Astryx commands inside `web/console/` after `npm ci --ignore-scripts` via `npm run astryx -- <cmd>` (unified local CLI alias per `@astryxdesign/cli@0.7.16` *Working with AI* guide; e.g. `npm run astryx -- doctor`) or `npx --no -- @astryxdesign/cli <cmd>` (on Windows PowerShell: `cmd /c npx --no -- @astryxdesign/cli doctor` or `npm run astryx -- doctor`). Keep `neutral` unmodified (never `theme eject`), zero `.css` files in `web/console/src/` (place local `Figtree` font CSS/assets outside `src/` under `web/console/assets/fonts/` in `TP-IMP-0026`), and `0` external runtime requests.
- Cross-platform quality gate: run `go run ./scripts/ci/check.go --all` and `go test -v ./...` (`CGO_ENABLED=0`), plus `go test -v -race ./...` in environments with a C toolchain (`ubuntu-24.04`, `macos-15`).

