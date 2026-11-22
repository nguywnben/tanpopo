# Tanpopo Web Console Guidelines (`ADR-0007`, `DEC-086`, `DEC-087`)

Read [`../../AGENTS.md`](../../AGENTS.md), [`../../CONSTRAINTS.md`](../../CONSTRAINTS.md), and [`../../docs/architecture/adr/0007-tanpopo-console-v2-design-system.md`](../../docs/architecture/adr/0007-tanpopo-console-v2-design-system.md) before modifying Web Console files.

## Architecture & Directory Layout

- `src/astryx-themes.ts` — CLI-generated Astryx Neutral theme registry (`npm run astryx -- theme add neutral`). Must remain LF-encoded.
- `src/styles/` — Astryx Neutral token-bridged layered CSS (`@layer reset, astryx-base, astryx-theme, tokens, base, layout, components`):
  - `tokens.css`: Neutral surface/border/ink aliases sourced directly from `@astryxdesign/theme-neutral` (light & dark synchronized via `light-dark()`). Must remain LF-encoded and contain zero retired beige/ochre hex literals.
  - `base.css`, `layout.css`, `components.css`: Structural responsive shell, flat hairline metric strip, responsive operational tables, and utility stacks.
  - `theme.tsx`: Root Astryx `<Theme theme={themes[defaultThemeSlug]} mode={theme}>` + light/dark mode persistence (`localStorage` + `prefers-color-scheme`) covering both pre-login (`/setup`, `/login`) and post-login screens.
- `src/ui/` — Thin Tanpopo adapters over official `@astryxdesign/core/*` components (`primitives.tsx`, `SelectField.tsx`, `MultiSelectField.tsx`, `RouteTypeahead.tsx`, `Overlays.tsx`) plus responsive desktop-table / mobile-stacked-row `DataTable.tsx`.
- `src/router/` — Zero-dependency HTML5 History SPA router (`RouterContext.tsx`).
- `src/api/` — OpenAPI 3.1 generated types (`generated.ts`), security policy (`operation-policy.generated.ts`), and CSRF/If-Match-enforcing `openapi-fetch` clients (`client.ts`, `catalogClient.ts`, `managementClient.ts`).
- `src/shell/` — 4-domain operational navigation (`routes.ts`, `ConsoleShell.tsx`, `WorkspaceRouter.tsx`).
- `src/pages/` — Domain workspaces covering Setup, Login, Overview, Traces, FinOps, Providers, Models, Routes & Policies, Extensions, MCP Hub, Playground, Access & Keys, Security, and Operations.

## Mandatory Quality & Responsive Rules

1. **Astryx Neutral Standard:** Use official `@astryxdesign/core/*` components everywhere suitable, and use Astryx Neutral tokens for domain-specific layouts. Keep active states strictly neutral (no colored decorative stripes/bars); reserve semantic colors for warnings, errors, success indicators, and destructive actions.
2. **Zero Page-Level Horizontal Overflow:** Every primary screen must maintain `document.documentElement.scrollWidth <= document.documentElement.clientWidth` at `320`, `375`, `768`, `1024`, `1440`, and `1920` CSS px — including long data strings, open sidebars, open modals/drawers, and long logs/JSON. Never apply `overflow-x: hidden` or `overflow-x: clip` to `html` or `body`.
3. **Operational Ergonomics:** No nested cards, no decorative badges (counts and semantic status tokens only), scannable responsive tables (stacked key-value rows below `768px`), and Astryx `Drawer` / `Dialog` / `AlertDialog` overlays for secondary details, forms, and confirmations.
4. **Security & Offline Operation:** Preserve mandatory `X-CSRF-Token` and `If-Match` OpenAPI guards, RBAC route/action visibility, write-only credential handling, English-only UI copy (`ISS-008`), Vite/Vitest local font aliasing (`@astryxdesign/theme-neutral/fonts.css` -> `assets/fonts/local-figtree-fonts.css` for `0` external runtime requests), and `npm ci --ignore-scripts`.
5. **Verification Gate:** Run inside `web/console/`:
   ```bash
   npm run typecheck
   npm test
   npm run build
   npm run sync:embed
   ```
   Then run `go run ./scripts/ci/check.go --all` and `go test ./internal/webui/... ./scripts/ci/...` from the repository root.

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
