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

## Tanpopo Web Console Rules (`DEC-007`, `CONSTRAINTS.md`, `SPEC-UX-04`)

- Read [`../../AGENTS.md`](../../AGENTS.md), [`../../CONSTRAINTS.md`](../../CONSTRAINTS.md), and [`../../docs/ux/design-system-integration.md`](../../docs/ux/design-system-integration.md) (`SPEC-UX-04`) before modifying Web Console files.
- **Unified Local CLI Invocation (inside `web/console/` after `npm ci --ignore-scripts`):** run `npm run astryx -- <cmd>` (per `@astryxdesign/cli@0.7.16` *Working with AI* pattern; e.g. `npm run astryx -- doctor`, `npm run astryx -- component <Name>`, `npm run astryx -- template <id>`) or `npx --no -- @astryxdesign/cli <cmd>`.
- **Preserve `neutral` Theme:** keep [`astryx-themes.js`](./astryx-themes.js) unmodified (`defaultThemeSlug = "neutral"`); never run `astryx theme eject` and never add `:root` overrides.
- **Zero Custom CSS Inside `src/` & Zero External Runtime Requests:** `scripts/ci/check.go` (`no-custom-css-in-src`) forbids any `.css` files under `web/console/src/` and `air-gap-no-google-fonts` forbids external font URLs. Local `Figtree` font aliasing (`@astryxdesign/theme-neutral/fonts.css` -> `web/console/assets/fonts/local-figtree-fonts.css` + local `.woff2` files), Vite + StyleX compiler wiring, and screen scaffolding are deferred to `TP-IMP-0026`.
- **English-Only UI Copy (`ISS-008`) & Strict Floor Guard:** zero raw `<div>`/`<span>` tags, zero TypeScript/lint suppressions, and `Badge` strictly for numeric counts.

