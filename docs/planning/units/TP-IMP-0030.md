# `TP-IMP-0030` — Observability, FinOps & Incident Response UI: Trace Waterfall (`SCR-11`), Budgets (`SCR-12`), Kill-Switch/Audit (`SCR-14`) & Operations (`SCR-16`)

| Attribute | Value |
|---|---|
| **Unit ID** | `TP-IMP-0030` |
| **Milestone / Epic** | `M2` (`MVP v0.1 Core Product`) · `EP-05` |
| **Status** | `BLOCKED` *(Depends on `TP-IMP-0025`, `TP-IMP-0026` — `GAP-01, 12`)* |
| **Size** | `L` (`web/console/src/pages/traces/*`, `finops/*`, `security/*`, `operations/*`) |
| **Review Tier** | **`REQUIRES_STRONG_REVIEW`** *(Stored XSS prevention on trace/audit payloads `CT-27`, strict separation of 3 cost-accuracy labels `UX-GR-05` & Emergency Kill-Switch UX)* |
| **Technical Owner** | `Lead Frontend / SRE UX Engineer` |

---

## 1. Intent
Deliver the 4 observability, FinOps, and incident-response views of `MVP (v0.1)` per [`screen-specifications.md`](../../ux/screen-specifications.md) (`SPEC-UX-03`):
1. **Trace Explorer & Debug Waterfall (`SCR-11 Traces`):** Filter by `request_id` / error code / model, render a per-hop execution waterfall (`Policy -> Route Attempt 1 (503) -> Failover Attempt 2 (200)`), and surface explicit privacy posture badges such as `Metadata-Only (0 prompt bytes stored)` (`UX-AC-09`).
2. **Budgets & FinOps Chargeback View (`SCR-12 Budgets & Chargeback`):** Strictly enforce **`UX-GR-05`** — render `EXACT`, `ESTIMATED`, and `UNMETERED_FLAGGED` request counts distinctly (never blending them ambiguously into a single "exact cost" figure), display pending reservations (`Active Hold`), and expose `CSV/JSON` chargeback export actions (`UX-AC-06`).
3. **Security Center, 4-Tier Emergency Kill-Switches & Audit Log (`SCR-14 Security & Audit`):** Activate/clear 4-tier Kill-Switches with live `Blast-Radius` impact previews, inspect immutable audit records, and run interactive `Verify Hash Chain` integrity checks (`UX-AC-02`).
4. **System Health, Backups & Upgrade Readiness (`SCR-16 Operations`):** Display `/readyz` probe health, WAL disk footprint, backup history, `tanpopo.lock` package pins, and upgrade preflight status (`UX-AC-05`).

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `web/console/src/pages/traces/TracesExplorerPage.tsx` (`SCR-11`).
  - `web/console/src/pages/finops/FinOpsBudgetsPage.tsx` (`SCR-12`).
  - `web/console/src/pages/security/SecurityAuditAndKillSwitchPage.tsx` (`SCR-14`).
  - `web/console/src/pages/operations/OperationsSettingsPage.tsx` (`SCR-16`).
- **Out-of-Scope:** `destructive` MCP tool approval queue tab on `SCR-14` (added in `TP-IMP-0038` during `M3 / v0.2`).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0704`, `TP-FR-0705`, `TP-FR-0802`, `TP-FR-0803`, `TP-FR-0902`, `UJ-08`, `SCR-11`, `SCR-12`, `SCR-14`, `SCR-16`, `CT-27`, `UX-GR-05`, `UX-GR-06`, `UX-AC-02`, `UX-AC-05`, `UX-AC-06`, `UX-AC-09`.
- **Dependencies:** `TP-IMP-0025`, `TP-IMP-0026`.
- **Design Refs:** [`user-flows.md`](../../ux/user-flows.md) (`UJ-08`), [`screen-specifications.md`](../../ux/screen-specifications.md) (`SCR-11`, `12`, `14`, `16`), [`contract-test-matrix.md`](../../quality/contract-test-matrix.md) (`CT-27`).

## 4. API, Wire & Data Contracts
- **Trace & Audit Stored XSS Prevention (`CT-27` Stored XSS Prevention):** Every model identifier, upstream error message, `User-Agent`, key label, or debug payload rendered on `SCR-11` and `SCR-14` MUST be 100% safely escaped — automated security tests inject XSS payloads into trace metadata and verify zero script execution in the browser.
- **Exact Currency Formatting:** Convert integer `micro_usd` values to formatted currency strings using exact integer/decimal string division on the frontend rather than IEEE-754 floating-point summation.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1` (`UX-GR-05` & `UX-AC-06` Honest FinOps Accuracy + Non-Color-Only Tokens):** Given ledger entries spanning `EXACT`, `ESTIMATED`, and `UNMETERED_FLAGGED` settlement states, When opening `SCR-12`, Then all 3 categories are displayed distinctly using `Token` components pairing `icon + label` (never relying on color alone — `UX-AC-06`).
- **`AC-2` (`UX-AC-02` Mobile Emergency Kill-Switch `375px` & `UX-AC-09` `200`-Row `< 100ms INP`):** On `SCR-14` across both desktop (`1280px`) and on-call mobile (`375px` — `UX-AC-02`) viewports, an operator can inspect `Blast-Radius` and trigger an emergency Kill-Switch without clipped controls; a 200-row trace table on `SCR-11` maintains interaction responsiveness at `INP < 100 ms` (`UX-AC-09`).
- **`NEG-1` (`CT-27` Stored XSS Payload in Trace/Audit):** Injecting malicious script payloads into `error_message` / `user_agent` trace fields renders strictly as inert plain text on `SCR-11`/`SCR-14`.
- **`NEG-2` (`UX-AC-05`):** `npx @astryxdesign/cli doctor --built dist` passes with `0 fail`, and `axe-core` passes cleanly across both Light and Dark themes.

## 6. Test Commands & Required Evidence
- `cd web/console && npm run typecheck && npm run test && npm run build && npx astryx doctor --built dist`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Principals with the `FINOPS_VIEWER` role can access `SCR-12` only (with zero access to debug payloads on `SCR-11` or `Kill-Switch` controls on `SCR-14`).
- **Rollback:** Independent 4-screen observability/operations module group.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0025` and `TP-IMP-0026` are complete.
- **DoD:** `CT-27`, `UJ-08`, and `UX-AC-02, 05, 06, 09` passing, `astryx doctor --built dist` at `0 fail`, and `REQUIRES_STRONG_REVIEW` sign-off achieved.
