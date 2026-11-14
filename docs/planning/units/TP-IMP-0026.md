# `TP-IMP-0026` — Web Console Foundation (`web/console/`): Vite + StyleX + Astryx `neutral`, Air-Gapped Static `Figtree` Font Bundling, `AppShell` & Typed API Client

| Attribute | Value |
|---|---|
| **Unit ID** | `TP-IMP-0026` |
| **Milestone / Epic** | `M2` (`MVP v0.1 Core Product`) · `EP-05` |
| **Status** | `BLOCKED` *(Depends on `TP-IMP-0005`, `TP-IMP-0024` — `GAP-01, 12`)* |
| **Size** | `M` (`web/console/*`) |
| **Review Tier** | `STANDARD_REVIEW` *(Strict adherence to Astryx Design System v0.7.13 & zero-external-request air-gapped operation)* |
| **Technical Owner** | `Lead Frontend / Design System Engineer` |

---

## 1. Intent
Establish the production-grade frontend foundation for the **Tanpopo Web Console (`web/console/`)** in accordance with [`ADR-0004`](../../architecture/adr/0004-web-console-stack-astryx-evaluation-and-repo-layout.md) and [`design-system-integration.md`](../../ux/design-system-integration.md) (`SPEC-UX-04`):
1. Cleanly migrate root placeholder files into `web/console/` with `Vite + TypeScript strict + StyleX compiler + @astryxdesign/core v0.7.13`, retaining the Owner-selected **`neutral`** default theme (`DEC-007` — **`theme eject` is strictly prohibited**).
2. Permanently resolve the air-gap font conflict (`GAP-12` / `CR-10`): configure Vite aliases mapping font imports to locally bundled `Figtree` `.woff2` assets so that running in a completely offline network produces **`0` outbound requests to `fonts.googleapis.com` or any external CDN**.
3. Scaffold the `AppShell` navigation frame from official Astryx templates (`npx astryx template ...`), build the strongly typed API client generated from `openapi/admin-v1.yaml` (automatically attaching `X-CSRF-Token` + `If-Match`), wire the top-bar `Project / Environment` context switcher (hiding the multi-`Org` switcher in single-organization `v0.1` deployments per `BA-OQ-001`), and provide canonical state views (`Loading`, `Empty`, `Error`, `Offline`).

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `web/console/package.json` (`"license": "Apache-2.0"`), `tsconfig.json`, `vite.config.ts` (with StyleX compiler plugin & local `.woff2` font alias).
  - `web/console/astryx-themes.js` (inheriting the unmodified `neutral` theme configuration — no manual token edits or `:root` overrides).
  - `web/console/src/main.tsx` (importing `@astryxdesign/core/reset.css` followed by `@astryxdesign/core/astryx.css` in exact order).
  - `web/console/src/shell/*` (scaffolded from the Astryx dashboard shell template, routing across all 11 IA sections gated by the 6 canonical RBAC roles, keyboard shortcuts & `WCAG 2.2 AA` navigation).
  - `web/console/src/api/*` (strongly typed client generated from `openapi/admin-v1.yaml`).
- **Out-of-Scope:** Detailed domain implementations of individual screens `SCR-01`..`16` (covered in `TP-IMP-0027`..`0030` and `0038`).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0902`, `TP-NFR-0701`, `TP-NFR-0702`, `TP-NFR-0801`, `TP-NFR-1001`, `DEC-007`, `UX-AC-06`, `UX-AC-07`, `UX-AC-08`, `UX-AC-10`, `GAP-12`.
- **Dependencies:** `TP-IMP-0005`, `TP-IMP-0024`.
- **Design Refs:** [`ADR-0004`](../../architecture/adr/0004-web-console-stack-astryx-evaluation-and-repo-layout.md), [`information-architecture.md`](../../ux/information-architecture.md) (`SPEC-UX-01`), [`design-system-integration.md`](../../ux/design-system-integration.md) (`SPEC-UX-04`), [`AGENTS.md`](../../../AGENTS.md).

## 4. API, Wire & Data Contracts
- **Inviolable Astryx Design System Discipline (`AGENTS.md` Rules):**
  1. Scaffold every page frame via `npx astryx build` / `npx astryx template`, and inspect component props via `npx astryx component <Name>` before usage.
  2. **Strictly zero raw HTML `div` or `span` elements** (`UX-AC-07`), zero custom `.css` imports beyond Astryx's canonical stylesheets (`reset.css`, `astryx.css`, and `theme.css` + local font aliases in `astryx-themes.js`), and zero hardcoded `#hex` colors or `px` values.
  3. **`Badge` is reserved strictly for numeric counts (`counts only`)**; all textual state indicators (`ENABLED_ACTIVE`, `QUARANTINED`, `VERIFIED`) MUST use the `Token` component with `icon + label` so color-vision-deficient operators can distinguish states without relying on color alone (`UX-AC-06`).
  4. Prior to completion, `npx @astryxdesign/cli doctor --built dist` MUST pass with **`0 fail`**, and the initial JavaScript bundle MUST stay within `gzip <= 250 KB` (`UX-AC-08`).

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1` (`UX-AC-06`, `UX-AC-07` & `UX-AC-08`):** Given the `web/console/` source tree, When running `npm run typecheck && npm run build && npx @astryxdesign/cli doctor --built dist`, Then the build succeeds with **`0 fail`**, the initial compressed JS bundle is `gzip <= 250 KB` (`UX-AC-08`), and every status indicator pairs `icon + label` (`UX-AC-06`).
- **`AC-2` (`UX-AC-10` & `GAP-12` True Air-Gap Zero External Requests):** Given the static `dist/` build opened in a test browser blocking all network traffic outside `127.0.0.1`, When loading `AppShell`, Then the Network panel records **`0` requests to any external domain** (with crisp typography rendered from bundled local assets).
- **`NEG-1`:** Deliberately injecting a raw `div` element, a hardcoded `#ff0000` literal, or a `Badge` wrapping a textual status label in a CI gate test causes the lint/doctor gate to fail immediately.
- **`NEG-2` (`Offline Banner`):** Disconnecting `/admin/v1/*` surfaces a clear offline banner without triggering a blank-screen crash (`ErrorBoundary`).

## 6. Test Commands & Required Evidence
- `cd web/console && npm ci --ignore-scripts && npm run typecheck && npm run test && npm run build && npx astryx doctor --built dist`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Zero credentials or session tokens stored in `localStorage` / `sessionStorage` (`HttpOnly` cookies + in-memory CSRF token only); strict `Content-Security-Policy (CSP)` enforced as `default-src 'self'; connect-src 'self'; font-src 'self'; object-src 'none'`.
- **Rollback:** Isolated `web/console/` directory allowing clean single-commit revert.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** Product Owner approves `ADR-0004` & `CR-10` (authorizing relocation of root UI files into `web/console/`); `TP-IMP-0005` and `TP-IMP-0024` are complete.
- **DoD:** `npx astryx doctor --built dist` passes with `0 fail`, `0` external network requests in air-gap mode, and `axe-core` audit on `AppShell` reports `0` critical/serious violations.
