# ADR-0004: Independent Evaluation of Embedded Static Web Console Stack (`React 19 + Vite + StyleX + Astryx neutral`) & Apache-2.0 Monorepo Layout

## Status
**`PROPOSED`** *(Awaiting Product Owner approval at Gate G3)*

## Date
`2026-11-11`

## Context
The repository currently contains pre-existing [`package.json`](../../../package.json), [`astryx-themes.js`](../../../astryx-themes.js), [`src/App.tsx`](../../../src/App.tsx), and [`src/main.tsx`](../../../src/main.tsx) configured with `@astryxdesign/core@^0.7.13`, `@astryxdesign/theme-neutral`, `@astryxdesign/cli`, and `@stylexjs/stylex` (`DEC-007`). However:
1. Architectural discipline requires **never treating pre-installed packages as a fait accompli**; the stack must be evaluated objectively across bundle footprint, self-hosted operational cost, `WCAG 2.2 AA` accessibility, and long-term maintainability.
2. Concrete baseline discrepancies exist in the repo (`ISS-001`..`005`): no bundler is configured yet, root `package.json` declares `"license": "ISC"` conflicting with the root [`LICENSE`](../../../LICENSE) (`Apache-2.0`), and `src/App.tsx` is a handwritten placeholder rather than an Astryx-scaffolded template.

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
