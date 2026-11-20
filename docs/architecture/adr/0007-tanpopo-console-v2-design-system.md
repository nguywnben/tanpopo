# ADR-0007: Tanpopo Console v2 Custom Design System & Responsive Operational Architecture

- **Status:** `ACCEPTED` *(Authorized by Product Owner under `DEC-086`, 2026-11-20; supersedes the Astryx/StyleX UI layer of [`ADR-0004`](./0004-web-console-stack-astryx-evaluation-and-repo-layout.md))*
- **Date:** `2026-11-20`
- **Owner / Author:** Gemini 4 Neon (under Product Owner delegation `DEC-086`)
- **Scope:** `web/console/`, `internal/webui/dist/`, `scripts/ci/check.go`, `.github/workflows/ci.yml`

---

## Context

`ADR-0004` selected `@astryxdesign/core` v0.7.16 + `@stylexjs/stylex` as the initial Web Console UI layer. Through `TP-IMP-0026`..`0038`, operational workflows expanded across Setup, Overview, Traces, FinOps, Providers, Models, Routes/Explain, Policies, Extensions, MCP Hub, Playground, Access/Keys, Security/Approvals/Kill-Switches, and Operations.

During operational review after `TP-IMP-0038`, the legacy Astryx + StyleX stack exhibited structural constraints:
- Rigid `no <div>`/`no <span>` rules and inflexible table/form wrappers forced awkward raw `<select>` controls next to component wrappers and created horizontal layout overflow on narrow viewports.
- Heavy card stacking, redundant copy, and full-page `window.location.assign` reloads reduced operational scanability and responsiveness.
- `@stylexjs/rollup-plugin` and Astryx CLI tooling added build complexity and ~94 extra npm packages without improving operator ergonomics.

Under **`DEC-086`**, the Product Owner authorized a dedicated blank-slate UX/UI overhaul of the Tanpopo Web Console with full frontend architecture authority, while preserving all backend APIs (`/admin/v1/*`, `/v1/*`, `/readyz`), RBAC, CSRF/OCC security contracts, URL compatibility, offline/air-gap operation, `<= 250 KB` gzip initial JS bundle budget, and WCAG 2.2 AA accessibility.

---

## Decision

1. **Retire Astryx and StyleX completely:**
   - Remove `@astryxdesign/core`, `@astryxdesign/theme-neutral`, `@astryxdesign/cli`, `@stylexjs/stylex`, `@stylexjs/rollup-plugin`, `web/console/astryx-themes.js`, and legacy StyleX stylesheets.
2. **Adopt Tanpopo Console UI v2 (`React 19 + TypeScript 5.9 + Vite 7 + Layered Semantic CSS + lucide-react`):**
   - Author a bespoke, zero-bloat operational design system under `web/console/src/styles/` (`@layer tokens, base, layout, components`) and `web/console/src/ui/` (`primitives.tsx`, `DataTable.tsx`, `Overlays.tsx`).
   - Visual identity: **Warm Daylight Precision** (light default with hairline dividers, Warm Stone canvas, Ink primary actions, Tanpopo Ochre active indicator, and an integrated Obsidian Dark theme toggle).
   - Zero nested cards, zero decorative badges (counts and semantic status indicators only), and monospace `tabular-nums` for metrics, integer micro-USD / nano-USD figures, hashes, and request IDs.
   - Local self-hosted `Figtree` `.woff2` fonts (`web/console/assets/fonts/`) and tree-shaken `lucide-react` SVG icons (ISC license, Apache-2.0 compatible) with **0 external runtime network requests**.
3. **4-Domain Operational Navigation & Client-Side History Router:**
   - Organize all workflows into 4 concise groups (**Monitor**, **Gateway**, **Ecosystem**, **Governance**) with tab sub-navigation, a `Ctrl+K` quick-jump palette, real `POST /admin/v1/auth/logout` sign-out, and 100% backward URL compatibility for every route and alias up through `TP-IMP-0038`.
4. **Structural Zero-Horizontal-Scroll Guarantee (No `overflow-x: hidden` Hacks):**
   - Enforce `document.documentElement.scrollWidth <= document.documentElement.clientWidth` at `320`, `375`, `768`, `1024`, `1440`, and `1920` CSS px across all primary screens (including long table strings, open drawers/modals, and long logs/JSON).
   - Desktop tables convert automatically into scannable stacked key-value operational rows below `768px`; secondary detail (trace waterfalls, adapter capabilities, audit metadata, create/edit forms) lives in slide-over drawers.
   - `scripts/ci/check.go` (`--ui-layout`) explicitly forbids `overflow-x: hidden` / `overflow-x: clip` on `html` or `body` so horizontal overflow must always be solved structurally.
5. **Embedded Production Pipeline:**
   - `npm run build` generates OpenAPI types & CSRF/If-Match security policy, builds `web/console/dist/`, writes `.gz` siblings for static assets, and enforces the `<= 250 KB` gzip initial JS budget.
   - `npm run sync:embed` synchronizes `web/console/dist/` into `internal/webui/dist/` for `go:embed` single-binary delivery.

---

## Consequences

- **Positive:** Initial gzipped JS drops to ~74 KB (`< 30%` of the 250 KB budget), total CSS is ~5 KB gzipped, 94 legacy frontend packages are removed, client-side navigation is instant, and responsive overflow is eliminated structurally across all viewports (`320px–1920px`).
- **Trade-offs:** UI primitives are maintained in-repo under `web/console/src/ui/`; strict TypeScript, Vitest + `axe-core` WCAG 2.2 AA tests, and `scripts/ci/check.go --ui-layout` guard regressions continuously in Stage 0/1 CI.
- **Preserved from `ADR-0004`:** Apache-2.0 monorepo layout under `web/console/`, `npm ci --ignore-scripts`, single `package-lock.json`, OpenAPI-generated client with mandatory `X-CSRF-Token` / `If-Match` middleware, `go:embed` static serving with strict CSP, and air-gapped zero-external-request execution.
