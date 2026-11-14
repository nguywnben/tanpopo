# `TP-IMP-0028` — Catalog & Extensions UI: Adapters (`SCR-03`), Write-Only Connections (`SCR-04`), Models (`SCR-05`) & Plugin Permission Review/Rollback (`SCR-06, 07`)

| Attribute | Value |
|---|---|
| **Unit ID** | `TP-IMP-0028` |
| **Milestone / Epic** | `M2` (`MVP v0.1 Core Product`) · `EP-05` |
| **Status** | `BLOCKED` *(Depends on `TP-IMP-0025`, `TP-IMP-0026` — `GAP-01, 12`)* |
| **Size** | `L` (`web/console/src/pages/providers/*`, `connections/*`, `models/*`, `extensions/*`) |
| **Review Tier** | **`REQUIRES_STRONG_REVIEW`** *(Core security UX guardrails `UX-GR-01..04`: no blind one-click install & 3-axis trust separation)* |
| **Technical Owner** | `Lead Product / Security UX Engineer` |

---

## 1. Intent
Build the 5-screen AI catalog and extension lifecycle management suite (`SCR-03`, `SCR-04`, `SCR-05`, `SCR-06`, `SCR-07`) per [`screen-specifications.md`](../../ux/screen-specifications.md) (`SPEC-UX-03`), enforcing all 4 core security UX guardrails:
- **`UX-GR-01`:** Write-only secret forms on `SCR-04` (displaying only non-sensitive metadata fingerprints after creation).
- **`UX-GR-02` & `UX-GR-03`:** Transparent display of the 3 capability assurance tiers (`DECLARED` / `TESTED` / `VERIFIED`) on `SCR-05` and strict enforcement of compliant billing modes (`COMMERCIAL_API` / `SELF_HOSTED_INFRA`), never labeling or guiding personal consumer subscription abuse as supported (`DEC-004`).
- **`UX-GR-04`:** On `SCR-06` & `SCR-07`, visually separate the 3 orthogonal trust dimensions (`Origin`, `Publisher Trust`, `Execution Tier`) and **prohibit blind one-click install/upgrade** — always presenting an explicit permission review dialog (`Permission & Delta Review Modal`: `allowed_hosts`, `secretSlots`, Sandbox tier) requiring explicit administrator consent before transitioning a package to `ENABLED_ACTIVE`, paired with a one-step rollback action to the prior known-good version (`< 5s`).

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `web/console/src/pages/providers/AdaptersPage.tsx` (`SCR-03`), `ConnectionsPage.tsx` (`SCR-04`), `ModelsCatalogPage.tsx` (`SCR-05`).
  - `web/console/src/pages/extensions/ExtensionCatalogPage.tsx` (`SCR-06`) & `InstalledExtensionDetailPage.tsx` (`SCR-07` with `PermissionDiffTable` highlighting newly requested permissions $\Delta P$ during upgrades, plus `Rollback` / `Quarantine` actions).
  - All dense datasets rendered using Astryx `Table` / `List` components (never wrapping dense rows in `Card` lists).
- **Out-of-Scope:** Interactive `OAuth 2.1` redirect authorization button on `SCR-04` (wired in `TP-IMP-0034` / `0038` during `M3`).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0201`..`0204`, `TP-FR-0304`, `TP-FR-0306`, `TP-FR-0902`, `UJ-02`, `UJ-04`, `UJ-07`, `SCR-03`..`07`, `UX-GR-01`..`04`, `UX-AC-03`, `UX-AC-12`.
- **Dependencies:** `TP-IMP-0025`, `TP-IMP-0026`.
- **Design Refs:** [`user-flows.md`](../../ux/user-flows.md) (`UJ-02`, `UJ-04`, `UJ-07`), [`screen-specifications.md`](../../ux/screen-specifications.md) (`SCR-03`..`07`), [`design-system-integration.md`](../../ux/design-system-integration.md) (`SPEC-UX-04`).

## 4. API, Wire & Data Contracts
- **Upgrade Permission Delta Highlight ($\Delta P$):** When `GET /admin/v1/extensions/{id}/permission-diff` reports newly added network hosts or an execution-tier elevation from `Declarative` to `Wasm`, the UI highlights every expanded permission row and disables the confirmation button until an authorized `ORG_OWNER` / `OPERATOR` / `SECURITY_REVIEWER` explicitly acknowledges the review (`UX-AC-03`).
- **`409` Conflict Handling:** When updating a `Connection` or `LogicalModel` returns `409 optimistic_lock_conflict`, the UI presents a non-destructive reload prompt rather than discarding user edits.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1` (`UX-AC-03` No Blind One-Click Install/Upgrade):** Given an unverified community package (`unverified-external`), When an administrator initiates installation or upgrades a package with $\Delta P \neq \emptyset$ on `SCR-06`/`SCR-07`, Then the package never activates immediately and MUST pass through the 3-axis trust + explicit permission review modal; clicking `Rollback` cleanly restores the previous version.
- **`AC-2` (`UX-AC-12` Capability Assurance Visibility):** On `SCR-05`, operators can filter and clearly distinguish newly declared models (`DECLARED`) from cassette-verified models (`TESTED`/`VERIFIED`).
- **`NEG-1`:** A user with the `DEVELOPER` role opening `SCR-06`/`SCR-07` sees `Install`, `Approve Permissions`, and `Quarantine` actions hidden/disabled per the RBAC matrix (`SPEC-UX-01`).
- **`NEG-2`:** Running `npx astryx doctor --built dist` returns `0 fail` (`0` raw `div`/`span` elements, `Badge` used strictly for numeric counts).

## 6. Test Commands & Required Evidence
- `cd web/console && npm run typecheck && npm run test && npm run build && npx astryx doctor --built dist`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Every manifest metadata string authored by external package publishers (`description`, `author`, `changelog`) MUST be rendered exclusively as escaped React plain text — **never via raw HTML (`dangerouslySetInnerHTML`)** — preventing `Stored XSS` from malicious packages.
- **Rollback:** Independent catalog screen modules under `web/console/`.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0025` and `TP-IMP-0026` are complete.
- **DoD:** Component & `UJ-02, 04, 07` journey tests passing, manifest XSS prevention verified, `astryx doctor --built dist` at `0 fail`, and `REQUIRES_STRONG_REVIEW` sign-off achieved.
