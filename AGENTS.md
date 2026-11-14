# AGENTS.md

Project-specific guidance for AI coding agents.

<!-- ASTRYX:START -->
Astryx v0.7.13 · 174 components
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
- Palettes and custom colors go through the theme, never :root overrides. Run `astryx theme add <slug>` to import; `theme eject <slug>` only forks source. See `astryx docs theme`.
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
## Tanpopo Cold-Start Agent Navigation & Governance Gate (ASTRA A10)

If you are starting a fresh session with no prior chat context (e.g., executing a `TP-IMP-xxxx` unit):
1. **Read [`docs/governance/execution-baseline.md`](./docs/governance/execution-baseline.md) FIRST** (`EXEC-BL-001-DRAFT`).
   - Current Delivery Handoff Verdict: **`NO_GO`** (`Gate G1, G2, G3, G4` and `CR-01..10` await explicit Product Owner approval in [`docs/governance/owner-approval-sheet.md`](./docs/governance/owner-approval-sheet.md) and [`docs/governance/decisions.md`](./docs/governance/decisions.md); `0 / 47` units are `READY`).
   - Do **NOT** write application/PoC code, infer approval from document existence, or publish/tag releases while `HANDOFF_VERDICT` is `NO_GO`.
2. **Master Unit Index & Specs:** [`docs/planning/implementation-index.md`](./docs/planning/implementation-index.md) → `docs/planning/units/TP-IMP-xxxx.md`.
3. **Audit & Gap Logs:** [`docs/reviews/execution-readiness.md`](./docs/reviews/execution-readiness.md) (A10), [`docs/reviews/integrated-audit.md`](./docs/reviews/integrated-audit.md) & [`docs/reviews/unresolved-gaps.md`](./docs/reviews/unresolved-gaps.md) (A08).
4. **Repository Overview & Documentation Hub:** [`README.md`](./README.md) & [`docs/README.md`](./docs/README.md).
<!-- TANPOPO-HANDOFF:END -->
