# ADR-0004: Independent Evaluation of Embedded Static Web Console Stack (`React 19 + Vite + StyleX + Astryx neutral`) & Apache-2.0 Monorepo Layout

## Status
**`ACCEPTED`** *(Approved by Product Owner on `2026-11-14` via `GOV-OAS-01` Track A + `CR-10`; remaining Vietnamese UI strings in initial `src/App.tsx` placeholder MUST be replaced with English prior to completing `TP-IMP-0026` per `ISS-008`)*

## Date
`2026-11-11`

## Context
The repository contains the relocated [`web/console/package.json`](../../../web/console/package.json), [`web/console/astryx-themes.js`](../../../web/console/astryx-themes.js), [`web/console/src/App.tsx`](../../../web/console/src/App.tsx), and [`web/console/src/main.tsx`](../../../web/console/src/main.tsx) configured with `@astryxdesign/core@^0.7.13`, `@astryxdesign/theme-neutral`, `@astryxdesign/cli`, and `@stylexjs/stylex` (`DEC-007`), relocated under `web/console/` with `Apache-2.0` and English placeholder copy in `TP-IMP-0005`. However:
1. Architectural discipline requires **never treating pre-installed packages as a fait accompli**; the stack must be evaluated objectively across bundle footprint, self-hosted operational cost, `WCAG 2.2 AA` accessibility, and long-term maintainability.
2. Concrete baseline discrepancies existed pre-`M1` (`ISS-001`..`005`, `008`): `ISS-002..004, 008` were closed in `web/console/` during `TP-IMP-0005`, while Vite bundling, StyleX compilation, and template-scaffolded screens (`ISS-001`, `ISS-005`) are scheduled in `TP-IMP-0026`.

## Decision
1. **Approve upon independent technical evaluation (`PROPOSED`) the `TypeScript + React 19 + Vite + StyleX + Astryx v0.7.13 (theme neutral)` stack for the administrative Web Console**, compiled into static assets (`SPA Static Assets`) and **embedded directly into the Go binary (`embed.FS`)** so it is served from the Control Plane's admin listener with zero Node.js server runtime in production.
2. **Establish a clean Monorepo directory layout (executed only after implementation begins post-approval):**
   - Relocate all UI source and build configs under `web/console/` (with proper Vite + StyleX compiler configuration, explicit `react` & `react-dom` dependencies, `@astryxdesign/cli` moved to `devDependencies`, and views scaffolded properly via `npx astryx template ...` replacing the handwritten placeholder).
   - Standardize license metadata across every manifest file to **`Apache-2.0`** matching the root `LICENSE` (`OQ-007`, `RSK-012`).

## Alternatives Considered

### Option B: Migrate to `Next.js App Router (SSR)` Running a Dedicated Node.js Process
- *Cons:* An authenticated internal admin console requires zero SEO; forcing self-hosters to run a separate Node.js SSR container alongside Go Core doubles memory overhead and complicates network ingress.
- *Why rejected:* Adds operational complexity with zero product benefit.

### Option C: Replace Astryx with `Tailwind CSS + shadcn/ui`
- *Pros:* Broad community familiarity.
- *Cons:* Loses the automated build-time enforcement of `astryx doctor --built` (which reliably prevents AI-generated raw `div` soup, hardcoded hex colors, and dense-table anti-patterns) and discards the `neutral` design direction established in `DEC-007`.

### Option D: Server-Rendered `Go Templates + HTMX`
- *Pros:* Eliminates the Frontend JS build toolchain.
- *Cons:* Poor ergonomics for stateful, real-time interactive views such as side-by-side SSE streaming in `Playground`, multi-branch `Route Explain` decision trees, and interactive JSON Schema permission diff dialogs.

## Rationale
Combining **StyleX + Astryx** (compile-time atomic CSS with `0` runtime style-calculation overhead, enforced `Table`/`List` primitives for dense telemetry data, and built-in `WCAG 2.2 AA` compliance) with a **Vite SPA embedded into Go `embed.FS`** delivers a dual advantage: a modern, responsive operator UI for users while remaining **a single zero-Node.js Go binary on production hosts** from an SRE perspective.

## Consequences
- **Positive:** Cleanly resolves `ISS-001`..`005`; introduces zero extra runtime processes; enforces automated UI quality gates in CI via `npx astryx doctor --built`.
- **Negative / Trade-offs:** Release CI pipelines must run `npm ci && npm run build` inside `web/console/` prior to `go build` so `dist/` assets are embedded into the compiled binary.

## Reversibility & Migration Implications
- **Reversibility:** **Easy – Medium** because the Web Console communicates with the Control Plane exclusively over the versioned `/admin/v1/*` REST contract (`SPEC-API-01`) with zero direct coupling to internal database schemas.
- **Execution Note:** Existing root files remain untouched during Phase A05 (zero premature file moves prior to Product Owner Gate G3 approval and implementation kickoff).
