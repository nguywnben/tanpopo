# Tanpopo Web Console Guidelines (`ADR-0007`, `DEC-086`)

Read [`../../AGENTS.md`](../../AGENTS.md), [`../../CONSTRAINTS.md`](../../CONSTRAINTS.md), and [`../../docs/architecture/adr/0007-tanpopo-console-v2-design-system.md`](../../docs/architecture/adr/0007-tanpopo-console-v2-design-system.md) before modifying Web Console files.

## Architecture & Directory Layout

- `src/styles/` — Layered semantic CSS (`@layer tokens, base, layout, components`):
  - `tokens.css`: Warm Daylight Precision (light) + Obsidian Dark (`[data-theme="dark"]`) semantic tokens. Must remain LF-encoded.
  - `base.css`, `layout.css`, `components.css`: Structural layout & accessible component primitives.
  - `theme.tsx`: Light/dark theme state provider (`localStorage` + `prefers-color-scheme`).
- `src/ui/` — Reusable accessible primitives (`primitives.tsx`, `DataTable.tsx`, `Overlays.tsx`).
- `src/router/` — Zero-dependency HTML5 History SPA router (`RouterContext.tsx`).
- `src/api/` — OpenAPI 3.1 generated types (`generated.ts`), security policy (`operation-policy.generated.ts`), and CSRF/If-Match-enforcing `openapi-fetch` clients (`client.ts`, `catalogClient.ts`, `managementClient.ts`).
- `src/shell/` — 4-domain operational navigation (`routes.ts`, `ConsoleShell.tsx`, `WorkspaceRouter.tsx`).
- `src/pages/` — Domain workspaces covering Setup, Login, Overview, Traces, FinOps, Providers, Models, Routes & Policies, Extensions, MCP Hub, Playground, Access & Keys, Security, and Operations.

## Mandatory Quality & Responsive Rules

1. **Zero Page-Level Horizontal Overflow:** Every primary screen must maintain `document.documentElement.scrollWidth <= document.documentElement.clientWidth` at `320`, `375`, `768`, `1024`, `1440`, and `1920` CSS px — including long data strings, open sidebars, open modals/drawers, and long logs/JSON. Never apply `overflow-x: hidden` or `overflow-x: clip` to `html` or `body`.
2. **Operational Ergonomics:** No nested cards, no decorative badges (counts and semantic status indicators only), scannable responsive tables (stacked key-value rows below `768px`), and slide-over drawers for secondary details and forms.
3. **Security & Offline Operation:** Preserve mandatory `X-CSRF-Token` and `If-Match` OpenAPI guards, RBAC route/action visibility, write-only credential handling, English-only UI copy (`ISS-008`), local `Figtree` font assets (`0` external runtime requests), and `npm ci --ignore-scripts`.
4. **Verification Gate:** Run inside `web/console/`:
   ```bash
   npm run typecheck
   npm test
   npm run build
   npm run sync:embed
   ```
   Then run `go run ./scripts/ci/check.go --all` and `go test ./internal/webui/... ./scripts/ci/...` from the repository root.
