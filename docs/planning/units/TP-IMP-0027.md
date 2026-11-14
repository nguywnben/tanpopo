# `TP-IMP-0027` — Onboarding & Overview UI: First-Run Setup Wizard (`SCR-01` Local GPU Opt-In & `TTFGR < 3m`), Login & `Overview` Dashboard (`SCR-02`)

| Attribute | Value |
|---|---|
| **Unit ID** | `TP-IMP-0027` |
| **Milestone / Epic** | `M2` (`MVP v0.1 Core Product`) · `EP-05` |
| **Status** | `BLOCKED` *(Depends on `TP-IMP-0025`, `TP-IMP-0026` — `GAP-01, 10, 12`)* |
| **Size** | `M` (`web/console/src/pages/setup/*`, `login/*`, `overview/*`) |
| **Review Tier** | **`REQUIRES_STRONG_REVIEW`** *(First-admin onboarding flow, explicit security opt-in for local GPU loopback `CR-10` & show-once secret display)* |
| **Technical Owner** | `Lead Product / Frontend Engineer` |

---

## 1. Intent
Deliver the **Zero-to-First-Governed-Request Under 3 Minutes (`UJ-01` — `TTFGR < 3m`)** journey across the first 3 console views built from canonical Astryx templates:
1. **Login & First-Run `Bootstrap` Gate (`SCR-01`):** Exchange the `Bootstrap Token` for the initial `ORG_OWNER` account and guide the operator through a streamlined 4-step wizard (Create first Provider Connection -> Declare Logical Model -> Mint a show-once `Virtual Key` with one-click copyable `curl` / Python / TS SDK snippets -> Fire first live verification request). Explicitly resolves `GAP-10` / `CR-10`: when the operator selects a local `Ollama` / `vLLM` runtime, the Wizard renders an explicit security confirmation checkbox granting `allow_private_ip = true` scoped strictly to the target endpoint (e.g., `127.0.0.1:11434`) without weakening the global `SSRFGuard` default.
2. **Operational `Overview` Dashboard (`SCR-02 Overview`):** Surface traffic health, `p50/p95` latency, error rate, budget burn, and prominent extension/incident alerts across all 5 canonical UI states (`Happy`, `Loading`, `Empty`, `Error`, `Offline`).

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `web/console/src/pages/setup/SetupWizardPage.tsx` (scaffolded from the Astryx onboarding/wizard template).
  - `web/console/src/pages/auth/LoginPage.tsx` (scaffolded from the Astryx login/auth template).
  - `web/console/src/pages/overview/OverviewPage.tsx` (scaffolded from the Astryx dashboard overview template).
  - `web/console/src/components/ShowOnceSecretDialog.tsx` (modal displaying a newly minted `Virtual Key` exactly once with an explicit warning that the plaintext secret can never be retrieved after dismissal — `UX-GR-01`).
- **Out-of-Scope:** Enterprise `OIDC SSO` login button (covered in `TP-IMP-0041`).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0902`, `UJ-01`, `SCR-01`, `SCR-02`, `UX-GR-01`, `UX-AC-01`, `UX-AC-04`, `GAP-10`.
- **Dependencies:** `TP-IMP-0025`, `TP-IMP-0026`.
- **Design Refs:** [`user-flows.md`](../../ux/user-flows.md) (`UJ-01`), [`screen-specifications.md`](../../ux/screen-specifications.md) (`SCR-01`, `SCR-02`), [`design-system-integration.md`](../../ux/design-system-integration.md) (`SPEC-UX-04`), [`unresolved-gaps.md`](../../reviews/unresolved-gaps.md) (`CR-10`).

## 4. API, Wire & Data Contracts
- **Client-Side Secret Hygiene Guardrail (`UX-GR-01`):** Provider API key inputs are masked by default and scrubbed from component state immediately upon `POST /admin/v1/secrets` completion; newly generated `Virtual Key` values are never persisted to URL history or browser storage.
- **Astryx Discipline:** `0` raw `div`/`span` elements; dense alert feeds rendered with `Table` / `List`; `Badge` reserved strictly for numeric counts.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1` (`UX-AC-01` Zero-to-First-Request Flow):** Given an uninitialized local server paired with `FakeLLMServer` (in both simulated cloud API mode and loopback `Ollama` mode with explicit opt-in enabled), When running the Playwright E2E flow completing `SCR-01` and landing on `SCR-02`, Then the entire flow completes smoothly in `< 60 seconds` automated (`< 3 minutes` human operator time).
- **`AC-2` (`UX-AC-04` Show-Once Enforcement):** After closing `ShowOnceSecretDialog` and reloading the page, zero DOM nodes or network JSON payloads contain the plaintext `vk_tp_live_...` secret.
- **`NEG-1`:** Selecting `Ollama (http://127.0.0.1:11434)` **without** checking the explicit private-address security acknowledgment keeps the continue action disabled with a clear SSRF protection explanation.
- **`NEG-2`:** Responsive layout verification across 3 viewport widths (`1280`, `768`, `375`) plus `axe-core` audit confirms `0` horizontal overflow issues and `0` critical/serious accessibility violations.

## 6. Test Commands & Required Evidence
- `cd web/console && npm run typecheck && npm run test && npm run build && npx astryx doctor --built dist`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** The `Bootstrap Token` on `SCR-01` is transmitted exclusively inside a `POST` JSON request body (never placed in URL query parameters where it could leak via `Referer` headers or browser history).
- **Rollback:** Self-contained page modules mounted inside `AppShell`.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0025` and `TP-IMP-0026` are complete.
- **DoD:** `UJ-01` E2E journey passing, `npx astryx doctor --built dist` reporting `0 fail`, and `REQUIRES_STRONG_REVIEW` sign-off achieved.
