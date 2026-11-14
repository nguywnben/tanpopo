# `TP-IMP-0001` — Spec Closure Batch A: Remediate State Machines `SM-01..09`, Add Missing `AC`s & Align `v0.1` Gates

| Property | Value |
|---|---|
| **Unit ID** | `TP-IMP-0001` |
| **Milestone / Epic** | `M0` (`Spec Closure & Spikes`) · `EP-00` |
| **Status** | **`DONE`** *(Completed & verified on `2026-11-14` under Product Owner Stage 1 M0 approval `DEC-009` — closed `GAP-08, 09, 11`)* |
| **Size** | `M` (`11` business, requirements, test & release-readiness specification files) |
| **Review Tier** | **`REQUIRES_STRONG_REVIEW`** *(Touches security key-revocation state machines & release scope gates)* |
| **Technical Owner** | `Lead Business Analyst + Principal Requirements Engineer` |

---

## 1. Intent
Eliminate all dead-end states across lifecycle state machines (`SM-01`..`SM-08`), extract `SM-09` as a dedicated state machine for `A2A` tasks, formally add `TP-FR-*` requirements for `Tier-3 Sidecar`, `/v1/rerank`, `Webhooks`, `Opt-In Response Cache`, and `Admin Break-Glass` per Product Owner decisions on `CR-01..04, 08, 09`, and author all missing `TP-AC-*` acceptance criteria and `CT-*`/`AUTH-*` contract tests for `TP-FR-0701`, `TP-FR-0702`, and `TP-FR-0901` before any coding agent touches source code.

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `docs/requirements/business-entity-map.md`, `docs/requirements/business-rules.md`, `docs/requirements/srs.md`, `docs/requirements/acceptance-criteria.md`, `docs/requirements/traceability.md`.
  - `docs/product/release-scope.md`.
  - `docs/ux/user-flows.md` (align `SM-06` state names and purge deprecated state strings).
  - `docs/specifications/mcp-a2a-boundaries.md` (decouple `SM-09` cleanly from `SM-06`).
  - `docs/quality/contract-test-matrix.md` & `docs/quality/release-readiness.md` (add `CT-*`/`AUTH-*` cases for `GAP-11` and align `v0.1` exit criteria in `GATE-REL-06` with `CR-04`).
  - `docs/reviews/unresolved-gaps.md` (mark `GAP-08`, `GAP-09`, `GAP-11` closed with exact line citations).
- **Out-of-Scope:** Do not modify `api-contracts.md` or `data-model.md` (owned by `TP-IMP-0002` and `TP-IMP-0003`); strictly create zero `.go` or `.ts` source files.

## 3. Requirements, Dependencies & Design References
- **Requirements & Gaps Refs:** `GAP-08`, `GAP-09`, `GAP-11` (`AUD-006`, `007`, `008`, `011`, `012`, `013`, `014`, `023`, `024`); `TP-FR-0301`, `0304`, `0501`..`0504`, `0701`, `0702`, `0901`, `1004`.
- **Dependencies:** `NONE` *(First root unit unlocked immediately upon Product Owner sign-off on `GAP-01`).*
- **Design / Audit Refs:** [`integrated-audit.md`](../../reviews/integrated-audit.md), [`unresolved-gaps.md`](../../reviews/unresolved-gaps.md) (`CR-01`..`04`, `CR-08`, `CR-09`), [`decisions.md`](../../governance/decisions.md).

## 4. API, Wire & Data Contracts
- **Mandatory State-Machine Transition Normalization:**
  - `SM-01` (`Extension Lifecycle`): Lock canonical state names `DISCOVERED -> VERIFYING -> PENDING_PERMISSION_APPROVAL -> INSTALLED_DISABLED | ENABLED_ACTIVE -> UPDATING_PENDING_RECONSENT | DRAINING | ROLLED_BACK | QUARANTINED`; ensure every non-terminal state (`DISCOVERED`, `VERIFYING`, `PENDING_PERMISSION_APPROVAL`, `INSTALLED_DISABLED`, `ENABLED_ACTIVE`, `UPDATING_PENDING_RECONSENT`, `DRAINING`) has a valid direct transition edge to `QUARANTINED`.
  - `SM-02` (`Provider Connection`): Lock canonical state names `DRAFT -> VALIDATING -> ACTIVE <-> DEGRADED | AUTH_EXPIRED_REAUTH_REQUIRED | SUSPENDED_BY_ADMIN | REVOKED | ARCHIVED`; add `DEGRADED --> SUSPENDED_BY_ADMIN`, `DEGRADED --> REVOKED`, and `DRAFT --> ARCHIVED`.
  - `SM-04` (`Model Deployment`): Lock canonical state names `PROVISIONED -> ACTIVE_HEALTHY <-> THROTTLED_COOLDOWN | CIRCUIT_OPEN_UNHEALTHY -> HALF_OPEN_PROBING -> DISABLED` (permitting transitions to `DISABLED` from every active/degraded state).
  - `SM-05` (`Virtual Key`): Lock canonical state names `CREATED_ACTIVE -> ROTATING_DUAL_VALID -> QUOTA_PAUSED | SUSPENDED | REVOKED | EXPIRED`; add edges `QUOTA_PAUSED --> REVOKED`, `QUOTA_PAUSED --> SUSPENDED`, and `QUOTA_PAUSED --> EXPIRED`.
  - `SM-06` (`MCP HITL Approval`): Dedicated exclusively to MCP (`PENDING -> APPROVED -> CONSUMED | REJECTED | EXPIRED`).
  - `SM-07` (`Budget Hold`): Add `STREAMING_ACCRUING --> EXPIRED_RECLAIMED`.
  - `SM-08` (`Upgrade`): Add escape transition `BLOCKED_INCOMPATIBLE --> ABORTED_SAFE`.
  - `SM-09` (`A2A Task`): Extracted from `SM-06`, using canonical lowercase A2A wire states (`submitted -> working <-> input-required -> completed | failed | canceled`) mapped 1:1 to uppercase DB enums.
- **Enum Normalization & SRS Additions (`CR-01, 02, 04, 09`):**
  - Lock the 6 canonical RBAC roles across all documents: `OPERATOR`, `ORG_OWNER`, `TEAM_ADMIN`, `SECURITY_REVIEWER`, `FINOPS_VIEWER`, `DEVELOPER`.
  - Lock the anti-impersonation namespace regex `^@tanpopo(-[a-z0-9]+)?/` and `Trust Tier` taxonomy per Owner decision on `CR-03`.
  - Add `TP-FR-0308` (`Tier-3 Isolated Sidecar` — `Could / v0.3`) and explicitly scope `/v1/rerank`, `Webhooks`, `Opt-In Response Cache`, and `tanpopo admin reset-local-owner` in `srs.md`.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1`:** Given all `SM-01`..`09` diagrams in `business-entity-map.md`, When inspecting every non-terminal state, Then every state of `Virtual Key`, `Connection`, `Deployment`, and `Extension` possesses a valid direct emergency transition to `REVOKED` / `DISABLED` / `QUARANTINED`.
- **`AC-2`:** Given `srs.md`, `acceptance-criteria.md`, and `contract-test-matrix.md`, When auditing all `36/36` `Must` requirements, Then explicit `Given/When/Then` scenarios and dedicated test IDs exist for: (a) `RPM`, `TPM`, and `Max Concurrent Streams` rate limits (`429 rate_limit_exceeded`), (b) monotonic child-policy relaxation rejection (`422 policy_relaxation_forbidden`), (c) Admin API cursor pagination (`limit`, `cursor`), and (d) the `v0.1` Official Adapters roster.
- **`AC-3`:** Given `release-scope.md` and `release-readiness.md`, When verifying `MVP (v0.1)` exit criteria, Then no `v0.3`/`v1.0` capabilities (`Air-Gap Bundle`, `< 5s` multi-node cluster revocation) erroneously block the single-node `v0.1` release.
- **`NEG-1`:** Strictly prohibited from unilaterally resolving `CR-01..04, 08, 09` prior to explicit Product Owner sign-off in `decisions.md`.
- **`NEG-2`:** Zero references to deprecated legacy state names remain anywhere inside normative specification directories (`docs/product`, `docs/requirements`, `docs/specifications`, `docs/architecture`, `docs/ux`, `docs/operations`, `docs/security`, `docs/quality`).
- **`NEG-3`:** Zero wording permitting consumer subscription credential harvesting or scraping (`DEC-004`).

## 6. Test Commands & Required Evidence
- Cross-platform verification (Node.js available on every workstation): `node -e "const fs=require('fs'),p=require('path');const dirs=['product','requirements','specifications','architecture','ux','operations','security','quality'];let hits=0;const walk=d=>fs.readdirSync(d,{withFileTypes:true}).forEach(e=>{const f=p.join(d,e.name);e.isDirectory()?walk(f):f.endsWith('.md')&&/PROBING_HALF_OPEN|APPROVED_ONCE/.test(fs.readFileSync(f,'utf8'))&&(console.error('Drift:',f),hits++)});dirs.forEach(d=>walk(p.join('docs',d)));process.exit(hits?1:0)"`
- Attach before/after reconciliation table closing `GAP-08`, `GAP-09`, `GAP-11` in the PR review.

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** `Emergency Revoke` must strictly preempt `QUOTA_PAUSED` so an attacker cannot wait until the next billing-period reset to reuse a compromised virtual key.
- **Rollback:** Markdown specification edits only — atomically revertible via a single `git revert` commit if adjustments are required.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** Product Owner has approved `Gate G1`, `Gate G2`, and Change Requests `CR-01..04, 08, 09` in [`decisions.md`](../../governance/decisions.md).
- **DoD:** All 11 in-scope specification files updated, Node drift check exits `0`, `GAP-08`, `GAP-09`, and `GAP-11` closed in [`unresolved-gaps.md`](../../reviews/unresolved-gaps.md), and `REQUIRES_STRONG_REVIEW` sign-off completed.
