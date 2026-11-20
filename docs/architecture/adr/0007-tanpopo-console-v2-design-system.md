# ADR-0007: Tanpopo Console v2 Responsive Operational Architecture & Astryx Neutral Integration

- **Status:** `ACCEPTED` *(Initial v2 architecture authorized under `DEC-086`, 2026-11-20; amended under **`DEC-087`**, 2026-11-20 to adopt `@astryxdesign/core` + `@astryxdesign/theme-neutral` on top of the v2 IA and responsive architecture)*
- **Date:** `2026-11-20`
- **Owner / Author:** Gemini 4 Neon (under Product Owner directives `DEC-086` and `DEC-087`)
- **Scope:** `web/console/`, `internal/webui/dist/`, `scripts/ci/check.go`, `CONSTRAINTS.md`, `.github/workflows/ci.yml`

---

## Context

Under **`DEC-086`** (merged to `main` at `739397ab3f6ed4fb605523e853665e20a0a4b171`), Tanpopo Console v2 replaced the legacy flat multi-reload UI with a cohesive 4-domain operational architecture (**Monitor**, **Gateway**, **Ecosystem**, **Governance**), client-side History SPA routing, responsive stacked mobile tables, slide-over drawers, and a structural zero-horizontal-scroll guarantee across `320`–`1920` CSS px.

Following visual review of the merged `739397a` baseline, the Product Owner issued **`DEC-087`**:
- Preserve Console v2's 4-domain information architecture, navigation, responsive layout, API wiring, RBAC, offline execution, and zero-overflow guarantees.
- Replace the custom Warm Stone / beige surfaces, ochre accents, and decorative orange active bars (sidebar left stripe, tab underline, waterfall fill) with **Astryx Neutral** (`@astryxdesign/core` + `@astryxdesign/theme-neutral`, configured via `astryx theme add neutral`).
- Mount the Astryx `<Theme>` provider at the application root so both pre-login (`/setup`, `/login`) and post-login screens share synchronized neutral Light and Dark modes, using official Astryx components everywhere suitable.

---

## Decision

1. **Adopt Official Astryx Neutral (`@astryxdesign/core` + `@astryxdesign/theme-neutral`, `DEC-087`):**
   - Configure the Neutral theme with `@astryxdesign/cli` (`astryx theme add neutral` → `web/console/src/astryx-themes.ts`).
   - Mount `<Theme theme={themes[defaultThemeSlug]} mode={theme}>` inside the root `ThemeProvider` (`web/console/src/styles/theme.tsx`) so both pre-login (`/setup`, `/login`) and post-login (`ConsoleShell`, dialogs, drawers, error boundaries) receive synchronized Astryx Neutral Light (`#f1f1f1` canvas / `#ffffff` surface) and Dark (`#1b1b1b` canvas / `#262626` surface) palettes.
   - Replace duplicate custom UI widgets with official `@astryxdesign/core/*` components: `Button`, `IconButton`, `TextInput`, `TextArea`, `CheckboxInput`, `Banner`, `Badge`, `Token`, `TabList`/`Tab`, `SegmentedControl`, `Stepper`, `Dialog`/`DialogHeader`, `AlertDialog`, `Drawer`/`DrawerHeader`, `Layout`, `CodeBlock`, `EmptyState`, `Skeleton`, and `Kbd`.
   - Build specialized operational patterns (responsive desktop-table / `<768px` stacked-row `DataTable`, sticky collapsible sidebar rail, flat hairline `MetricStrip`, and proportional trace waterfall bars) directly on Astryx Neutral CSS tokens under `web/console/src/styles/` (`@layer reset, astryx-base, astryx-theme, tokens, base, layout, components`).
2. **Eliminate Warm Beige / Ochre & Decorative Colored Bars:**
   - Remove all warm-stone/beige and ochre hex values and remove all decorative accent bars/stripes on sidebar items, tabs, selected rows, and panels.
   - Use crisp neutral selected states (`--color-neutral` / `--color-accent`) in both Light and Dark modes; reserve semantic colors strictly for warnings, errors, success indicators, and destructive actions.
3. **Preserve Air-Gap / Offline Operation (`0` External Runtime Requests):**
   - Alias `@astryxdesign/theme-neutral/fonts.css` in `web/console/vite.config.ts` and `web/console/vitest.config.ts` to `web/console/assets/fonts/local-figtree-fonts.css`, bundling self-hosted `Figtree` `.woff2` weights (`400`, `500`, `600`, `700`) and tree-shaken `lucide-react` SVGs with zero external network requests.
4. **Preserve 4-Domain Navigation & Structural Zero-Horizontal-Scroll Guarantee:**
   - Enforce `document.documentElement.scrollWidth <= document.documentElement.clientWidth` at `320`, `375`, `768`, `1024`, `1440`, and `1920` CSS px across every primary route, including long strings, open mobile sidebar, open drawers, and open dialogs.
   - `scripts/ci/check.go` (`--ui-layout`) enforces Astryx Neutral registration (`astryx-neutral-theme-record`, `astryx-neutral-deps`), forbids retired beige/ochre palette literals (`no-legacy-ochre-palette`), forbids `overflow-x: hidden`/`clip` on `html` or `body` (`no-root-overflow-x-hidden`), and enforces air-gap asset hygiene and English-only UI copy (`ISS-008`).
5. **Embedded Production Pipeline:**
   - `npm run build` generates OpenAPI types & CSRF/If-Match security policy, builds `web/console/dist/`, writes `.gz` siblings for static assets, and enforces the `<= 250 KB` gzip initial JS budget.
   - `npm run sync:embed` synchronizes `web/console/dist/` into `internal/webui/dist/` for `go:embed` single-binary delivery.

---

## Consequences

- **Positive:** Every screen across pre-login and post-login workflows now shares cohesive official Astryx Neutral components and light/dark surfaces while keeping Console v2's 4-domain navigation, responsive mobile tables, `0` external runtime requests, `0` `axe-core` WCAG 2.2 AA violations, `0` page-level horizontal overflow across `320`–`1920` px, and an initial JS gzip bundle under `250 KB` (`~202 KB` gzip).
- **Trade-offs:** Initial gzipped JS increases from `~74 KB` (custom CSS primitives) to `~202 KB` (official `@astryxdesign/core` components) while remaining safely within the `<= 250 KB` budget via route-level lazy code splitting.
- **Preserved from `ADR-0004` / `DEC-086`:** Apache-2.0 monorepo layout under `web/console/`, `npm ci --ignore-scripts`, single `package-lock.json`, OpenAPI-generated client with mandatory `X-CSRF-Token` / `If-Match` middleware, `go:embed` static serving with strict CSP, and air-gapped zero-external-request execution.
