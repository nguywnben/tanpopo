# 00 — TEEP-02A Proposed Master Feature Catalog Index

- **Document ID:** `TEEP-02A-DOC-00`
- **Mission:** `TEEP-02A` — Proposed Master Feature Catalog (Tanpopo Enterprise Product Expansion Program)
- **Catalog Status:** **`PROPOSED — READY FOR TEEP-02B FOUNDER & COORDINATOR SCOPE REVIEW`** *(Scope and final architecture are intentionally NOT locked)*
- **Delivery Branch:** `planning/teep-02a-feature-catalog` (derived from `planning/teep-01-intelligence` `a9f8467ddfd10f2a7962aae8f213e7f3277f2bdd`; original `TEEP-01` branch preserved untouched)
- **Authoritative Implementation Baseline:** Frozen Unsigned RC9 `main` (`ac9ae87a68f095203de82968ac5d08729e33a531` — `BASELINE_STABLE`)
- **Release Gate Status:** `GATE-REL-07` remains **`BLOCKED`** (`0` production code edits, `0` spec mutations, `0` implementation units, `0` paid provider calls, `0` git tags/releases).

---

## 1. Executive Catalog Telemetry at a Glance

| Catalog Dimension | Proposed Count | Breakdown |
|---|---:|---|
| **Functional Domains (`DOM-01`..`DOM-26`)** | **`26`** | `25` retained from `TEEP-01` + `1` newly justified domain (`DOM-26: Multi-Instance Fleet & Release Lifecycle Management`). |
| **Proposed Product Features (`FEAT-001`..`FEAT-153`)** | **`153`** | User-Facing (`CAT-PROD`): `44` · Admin/Ops (`CAT-ADMIN`): `90` · Developer/Ecosystem (`CAT-ECO`): `19`. |
| **Feature Dispositions** | **`153`** | `KEEP`: `7` · `IMPROVE`: `60` · `REPLACE`: `4` · `ADD`: `82` (`REMOVE` handled via `FND-019` technical cleanup). |
| **Catalog Evolution vs `TEEP-01` (`CAP-001`..`161`)** | **`100%` Traced** | Retained `1:1`: `130` · Substantially Expanded: `8` · Consolidated (`6 CAPs -> 3 FEATs`): `3` · Newly Added in `TEEP-02A`: `12` · Promoted to `ARCH`/`FND`: `17` `CAP`s. |
| **Candidate Priority Distribution** | **`153`** | `P0-Foundational`: `42` · `P1-Enterprise-Core`: `70` · `P2-Differentiator`: `38` · `P3-Expansion`: `3`. |
| **Internal Architectural Enablers (`ARCH-001`..`018`)** | **`18`** | Separated from user-facing features (`CAT-ARCH`); algorithm/engine selections deferred to `TEEP-03`. |
| **Foundational Remediation & Debt (`FND-001`..`020`)** | **`20`** | Preserves `DEF-001..086` (`15` open `PARTIAL`s), `PG-001..014`, `RR-001..029`, `ARCH-OBS-01`, and `FND-001` (`P0` PG `KEK` `FORCE RLS` risk). |
| **Non-Functional Requirements (`NFR-001`..`018`)** | **`18`** | `12` reconciled `QAS-ENT-01..12` scenarios + `6` standing UI/portability/air-gap/license bars (`CAT-NFR`). |
| **Researched Product Innovations (`INN-01`..`12`)** | **`12`** | All `12` innovations mapped into concrete `FEAT-*` capabilities in [`07-INNOVATION-PORTFOLIO.md`](./07-INNOVATION-PORTFOLIO.md). |
| **Explicitly Rejected Anti-Patterns (`REJ-001`..`006`)** | **`6`** | Consumer ToS pooling, unsandboxed Console JS, native `.so`/`eval` plugins, mandatory ClickHouse, host `eval` tool glue, cloud-only doctor chatbot. |
| **Strategic & Technical Decisions (`DEC-CAND-01`..`22`)** | **`22`** | `13` queued for `TEEP-02B` Product Scope/Licensing Gate; `9` technical selections deferred to `TEEP-03`. |

---

## 2. Complete Package Map (`docs/teep/02-product/02a-feature-catalog/`)

| Deliverable | Primary Audience & Contents |
|---|---|
| [`00-CATALOG-INDEX.md`](./00-CATALOG-INDEX.md) | Package navigation, separation-of-concerns summary, and governance posture. |
| [`01-PRODUCT-CAPABILITY-MAP.md`](./01-PRODUCT-CAPABILITY-MAP.md) | 26-domain architectural capability map, domain taxonomy, and beginner-to-enterprise progressive cohesion model. |
| [`02-MASTER-FEATURE-CATALOG.md`](./02-MASTER-FEATURE-CATALOG.md) | Unified catalog separating `CAT-PROD`, `CAT-ADMIN`, `CAT-ECO`, `CAT-ARCH` (`ARCH-001..018`), `CAT-NFR` (`NFR-001..018`), and `REJ-001..006`, with master index links into full 22-field specs. |
| [`03-FUNCTIONAL-DOMAINS/`](./03-FUNCTIONAL-DOMAINS/00-DOMAINS-INDEX.md) | Six deep domain volumes containing the complete 22-attribute Product Owner specification for every `FEAT-001`..`FEAT-153`. |
| [`04-CROSS-DOMAIN-WORKFLOW-MAP.md`](./04-CROSS-DOMAIN-WORKFLOW-MAP.md) | `18` end-to-end user, admin, publisher, notification, update/advisory, revision-rollback, and multi-instance fleet workflows (`WF-01..18`). |
| [`05-ENTERPRISE-CAPABILITY-MATRIX.md`](./05-ENTERPRISE-CAPABILITY-MATRIX.md) | Essential Self-Hosted/Enterprise vs Enterprise Governance vs Optional Ecosystem vs Long-Term Expansion classification across all `153` features. |
| [`06-CORE-AND-EXTENSION-CANDIDATES.md`](./06-CORE-AND-EXTENSION-CANDIDATES.md) | Candidate placement across Core Data Plane, Core Control Plane, Official `.tpkg`, Marketplace/Private `.tpkg`, and External Tooling + 7 non-overridable invariants. |
| [`07-INNOVATION-PORTFOLIO.md`](./07-INNOVATION-PORTFOLIO.md) | Concrete product realization, workflows, outcomes, limitations, and feasibility for all `12` innovations (`INN-01..12`). |
| [`08-FOUNDATIONAL-BACKLOG.md`](./08-FOUNDATIONAL-BACKLOG.md) | Foundational assurance & technical debt register (`FND-001..020`), highlighting `FND-001` (`P0` PG `KEK` `FORCE RLS` risk) and preserving `DEF-001..086`, `PG-001..014`, `RR-001..029`, and `ARCH-OBS-01`. |
| [`09-RELEASE-SCOPE-SCENARIOS.md`](./09-RELEASE-SCOPE-SCENARIOS.md) | Four coherent release-scope proposals (`SCEN-A`, `SCEN-B`, `SCEN-C`, `SCEN-D`) compared across 7 strategic, engineering, QA, and TCO dimensions. |
| [`10-FOUNDER-REVIEW-GUIDE.md`](./10-FOUNDER-REVIEW-GUIDE.md) | **Primary Executive Entry Point:** Plain-language review guide organized into 6 manageable review sessions with RC9 vs Proposed tables, examples, trade-offs, and decision checklists. |
| [`11-DECISION-REGISTER.md`](./11-DECISION-REGISTER.md) | `22` decision candidates (`DEC-CAND-01..22`) separating `TEEP-02B` scope/licensing choices from `TEEP-03` technical evaluations. |
| [`12-TRACEABILITY-AND-COVERAGE.md`](./12-TRACEABILITY-AND-COVERAGE.md) | `TEEP-01` reconciliation report, `100%` `CAP-001..161` traceability table, Added/Expanded/Consolidated/Rejected ledger, and completeness audit. |
| [`13-TEEP-02B-HANDOFF.md`](./13-TEEP-02B-HANDOFF.md) | Actionable review agenda and handoff contract for the `TEEP-02B` Founder & Coordinator Scope Gate. |
| [`data/`](./data/) | `8` machine-readable exports: `master-feature-catalog.json`, `master-feature-catalog.csv`, `architectural-enablers-and-nfrs.json`, `architectural-enablers-and-nfrs.csv`, `foundational-backlog.json`, `foundational-backlog.csv`, `decision-register.json`, and `cap-to-catalog-traceability.csv`. |
