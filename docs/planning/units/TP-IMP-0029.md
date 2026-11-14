# `TP-IMP-0029` — Orchestration & Access UI: Route Builder (`SCR-08`), Policy Hierarchy (`SCR-09`) & `Virtual Keys` / `RBAC` Management (`SCR-13`)

| Attribute | Value |
|---|---|
| **Unit ID** | `TP-IMP-0029` |
| **Milestone / Epic** | `M2` (`MVP v0.1 Core Product`) · `EP-05` |
| **Status** | `BLOCKED` *(Depends on `TP-IMP-0025`, `TP-IMP-0026` — `GAP-01, 12`)* |
| **Size** | `M` (`web/console/src/pages/routes/*`, `policies/*`, `access/*`) |
| **Review Tier** | `STANDARD_REVIEW` *(Destructive action confirmation guardrail `UX-GR-06` & monotonic policy inheritance visualization)* |
| **Technical Owner** | `Lead Frontend / Product Engineer` |

---

## 1. Intent
Build the 3 core `v0.1` routing, policy hierarchy, and access-control management screens using the Astryx Design System (`neutral`):
1. **Route & Fallback Builder (`SCR-08 Routes`):** Create/edit `RoutePolicy` configurations (`PRIORITY_FAILOVER`, `WEIGHTED_ROUND_ROBIN`, `LOWEST_COST`, `LOWEST_LATENCY_EWMA`), visually display `SM-04` circuit-breaker health states (`ACTIVE_HEALTHY`/`THROTTLED_COOLDOWN`/`CIRCUIT_OPEN_UNHEALTHY`/`HALF_OPEN_PROBING`) per target deployment, and surface inline form warnings whenever a fallback target lacks required capabilities or violates data-residency constraints.
2. **Hierarchical Policy Tree Manager (`SCR-09 Policies`):** Clearly distinguish non-relaxable inherited parent constraints (`Org -> Project -> Env`) from locally tightened limits (`RPM`, `TPM`, `MaxConcurrentStreams`, `Data Residency`, model allowlists).
3. **`Virtual Keys` & 6-Role `RBAC` Manager (`SCR-13 Access`):** Mint show-once `vk_tp_...` virtual keys, execute graceful dual-valid rotation (`ROTATING_DUAL_VALID`), clearly separate *Graceful Rotation* from stream-severing *Emergency Security Revoke* gated by a typed-name confirmation modal (`UX-GR-06`), and assign the 6 canonical RBAC roles to members.

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `web/console/src/pages/routes/RoutesPage.tsx` (`SCR-08`).
  - `web/console/src/pages/policies/PoliciesPage.tsx` (`SCR-09`).
  - `web/console/src/pages/access/AccessAndKeysPage.tsx` (`SCR-13`).
- **Out-of-Scope:** Interactive `Dry-Run Route Explain` drawer on `SCR-08` (covered in `TP-IMP-0038` in `M3`); `Enterprise OIDC` & `Four-Eyes` settings (covered in `TP-IMP-0041` / `0045`).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0502`, `TP-FR-0503`, `TP-FR-0601`, `TP-FR-0701`, `TP-FR-0902`, `UJ-05`, `SCR-08`, `SCR-09`, `SCR-13`, `UX-GR-06`, `UX-AC-11`.
- **Dependencies:** `TP-IMP-0025`, `TP-IMP-0026`.
- **Design Refs:** [`user-flows.md`](../../ux/user-flows.md) (`UJ-05`), [`screen-specifications.md`](../../ux/screen-specifications.md) (`SCR-08`, `SCR-09`, `SCR-13`), [`design-system-integration.md`](../../ux/design-system-integration.md) (`SPEC-UX-04`).

## 4. API, Wire & Data Contracts
- **Destructive Action Confirmation (`UX-GR-06`):** Emergency revocation of a `Virtual Key` with active traffic or deletion of a `RoutePolicy` MUST display the recent request volume affected and require explicit typed confirmation prior to submission.
- **`ETag` Concurrency Synchronization:** Every edit form across `SCR-08`, `SCR-09`, and `SCR-13` sends `If-Match` and handles `409 Conflict` gracefully without data loss.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1` (`Route Guardrails` & `UX-AC-11` `409 Conflict` Draft-Preserving Diff Modal):** Configuring route weights whose sum differs from `100` or selecting a fallback target missing `tool_calling` triggers an immediate inline warning; encountering `409 optimistic_lock_conflict` on `SCR-08`/`SCR-09` due to a concurrent administrator edit opens a two-version diff modal preserving the user's in-progress draft (`UX-AC-11`) with zero lost input.
- **`AC-2`:** When a `TEAM_ADMIN` views `SCR-09`, regions or models restricted at the `Org` level render as locked inherited controls (annotated with their `Org Policy` origin) and cannot be re-enabled.
- **`NEG-1`:** `npx astryx doctor --built dist` passes with `0 fail` (`0` raw `div`/`span` elements, `Table` used for virtual-key and member lists, `Badge` used strictly for numeric counts).

## 6. Test Commands & Required Evidence
- `cd web/console && npm run typecheck && npm run test && npm run build && npx astryx doctor --built dist`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Newly created `secret_key` values are never cached in global browser state; React state holding the plaintext secret is wiped immediately on dialog dismissal.
- **Rollback:** Independent 3-screen module group under `web/console/`.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0025` and `TP-IMP-0026` are complete.
- **DoD:** Component and `UJ-05` journey tests passing, `astryx doctor --built dist` at `0 fail`, and `axe-core` accessibility checks clean.
