# Execution Baseline & Agent Handoff Contract — Tanpopo

> **MANDATORY COLD-START ENTRYPOINT FOR EVERY NEW EXECUTION SESSION (`ZERO PRIOR CHAT MEMORY REQUIRED`)**
> - **Baseline ID:** `EXEC-BL-001-DRAFT`
> - **Version:** `0.2.0-en` | **Last Updated:** `2026-11-14` | **Pre-Closure Reference Commit:** `bb3ffb0`
> - **CURRENT DELIVERY HANDOFF VERDICT (`HANDOFF_VERDICT`):** **`NO_GO`** *(Code execution is NOT unlocked!)*
> - **CURRENT `GATE G4` STATUS:** **`BLOCKED` / `READY_FOR_REVIEW`** *(Awaiting explicit Product Owner sign-off on `Gate G1`, `Gate G2`, `Gate G3`, `CR-01`..`CR-10`, and `Gate G4` via [`owner-approval-sheet.md`](./owner-approval-sheet.md) and [`decisions.md`](./decisions.md)).*
> - **CURRENT READY UNITS (`READY_UNITS`):** **`NONE` (`0 / 47` `READY`, `47 / 47` `BLOCKED`)**

---

## 0. Mandatory Start-of-Session Gate Check for the Engineering Agent ("Luna")

If you are an Engineering Coding Agent (e.g., **Luna**) starting a fresh session with **zero prior conversation history** and instructed to *"Implement `TP-IMP-xxxx`"*, you **MUST** execute these 4 gate checks before creating or modifying any file:

1. **Verify `HANDOFF_VERDICT` at the top of this file and the Stage-Gate table in [`docs/governance/decisions.md`](./decisions.md):**
   - While `HANDOFF_VERDICT` is **`NO_GO`** or `Gate G4` is not explicitly marked **`APPROVED`** by the Product Owner: **STOP IMMEDIATELY (`ABORT SAFELY`)**, report that the unit is `BLOCKED` by human governance gates, and point the caller to [`docs/governance/owner-approval-sheet.md`](./owner-approval-sheet.md). **Never infer approval from the mere existence of specification or planning files!**
2. **Verify the unit's own status and upstream `Dependencies` in [`docs/planning/implementation-index.md`](../planning/implementation-index.md):**
   - Execute `TP-IMP-xxxx` ONLY when its status in `implementation-index.md` is **`READY`** and `100%` of the units listed in its `Dependencies` field in [`docs/planning/units/TP-IMP-xxxx.md`](../planning/units/TP-IMP-0001.md) are marked `DONE`.
3. **Load Selective, Focused Context Only (Prevent Context Window Flooding):**
   - Read in strict order: (a) [`AGENTS.md`](../../AGENTS.md) → (b) this `execution-baseline.md` → (c) the target unit spec [`docs/planning/units/TP-IMP-xxxx.md`](../planning/units/TP-IMP-0001.md) → (d) **only** the specific sections cited under Section 3 (`Design Refs`) of that unit.
4. **Hard Public-Release Boundary:** No AI Agent may ever run `git tag`, `git push --tags`, `gh release create`, public `docker push`, or `npm publish` without explicit written sign-off from the Product Owner at [`TP-IMP-0047`](../planning/units/TP-IMP-0047.md) (`GATE-REL-07`).

---

## 1. Confirmed Owner Decisions (`CONFIRMED_BY_OWNER`) vs. Pending Approvals

### 1.1. Locked Baseline Decisions (`DEC-001` .. `DEC-008`) & Permanently Rejected Directions (`REJ-001` .. `REJ-004`)
Only the following decisions are currently confirmed in the repository ([`decisions.md`](./decisions.md)):
- **`DEC-001`:** Tanpopo is an **open-source, self-hosted-first, extension-first, production-grade, scalable AI Gateway** spanning Unified API, routing, policies, observability, FinOps cost control, MCP tools, A2A connectivity, Web Console, and CLI.
- **`DEC-002`:** Users independently install **Provider Adapters and other extensions** from Marketplace indexes, Git repositories, URLs/`.tpkg` archives, or private registries **without rebuilding or re-releasing Tanpopo Core**.
- **`DEC-003`:** **Provider, Plugin, MCP, and A2A are 4 independent architectural layers** — conflating their schemas, wire protocols, or permission models is permanently rejected (`REJ-004`).
- **`DEC-004` (`Absolute Legal & Compliance Boundary`):** Official Adapters strictly target official commercial Provider APIs and provider-authorized access flows; **Tanpopo never builds, bundles, documents, or legitimizes converting personal consumer subscriptions (`ChatGPT Plus/Pro`, `Claude Pro/Max`, `Codex`, `Antigravity`, etc.) into general-purpose APIs, nor uses "accept-risk" disclaimers to excuse TOS violations (`REJ-003`)**.
- **`DEC-005`:** Canonical repository `https://github.com/nguywnben/tanpopo` (`main` branch), Git author `Nguyễn Công Ben <nben940665@gmail.com>`.
- **`DEC-006`:** Local installation and use of all 25 software engineering skills in [`.agents/skills/`](../../.agents/skills) (`skills-lock.json`).
- **`DEC-007`:** Web Console baseline uses `@astryxdesign/core@^0.7.13` + `@stylexjs/stylex@^0.19.1` with default theme **`neutral`** ([`astryx-themes.js`](../../astryx-themes.js)) — **never run `astryx theme eject`, never emit raw `div`/`span` HTML elements, and reserve `Badge` strictly for numeric counts**.
- **`DEC-008`:** Atomic Conventional Commits formatted as `<type>: step <NN> - <description>` (or `<type>(<module>): step <NN> - <description>`); no `a`-prefixed step numbers and no force-pushes on `main`.

### 1.2. Consolidated Pending Decisions Required to Flip `NO_GO` → `GO`
All pending approvals are consolidated with full trade-off and impact analysis in **[`docs/governance/owner-approval-sheet.md`](./owner-approval-sheet.md)**:
1. **`Gate G1`:** `SD-G1-01`..`05` ([`decisions.md`](./decisions.md) Part C).
2. **`Gate G2`:** `BA-OQ-001`..`05` & `TD-G2-01`..`06` ([`decisions.md`](./decisions.md) Parts E–F).
3. **`Gate G3`:** `ADR-0001`..`0006` and explicit authorization to run isolated offline PoCs `SPIKE-01`..`04` ([`decisions.md`](./decisions.md) Part H & [`release-readiness.md`](../quality/release-readiness.md) Section 4).
4. **`Audit A08 Change Requests`:** `CR-01`..`CR-10` ([`unresolved-gaps.md`](../reviews/unresolved-gaps.md)) to unlock spec-closure units `TP-IMP-0001`..`0003`.
5. **`Gate G4`:** Approval of this Execution Baseline (`EXEC-BL-001-DRAFT` → `EXEC-BL-001-APPROVED`) and confirmation of implementation unit commit tagging (`[TP-IMP-xxxx]`).

---

## 2. Canonical Zero-Guesswork Technical Dictionary for Engineering Agents

Once the candidate architecture (`ADR-0001`..`0006` + `CR-01`..`10`) is approved by the Product Owner, every implementation unit `TP-IMP-0001`..`0047` **MUST** adhere strictly to the following single canonical identifiers:

| Dimension | Single Canonical Standard | Prohibited Legacy / Drift Variants |
|---|---|---|
| **Root Go Module (`go.mod`)** | `module github.com/nguywnben/tanpopo` (`Go >= 1.24`) | `module tanpopo`, `tanpopo.io/...` |
| **Isolated M0 Spikes Module** | `tools/spikes/go.mod` (`module github.com/nguywnben/tanpopo-spikes`) | Mixing spike code into root `go.mod` or `internal/` |
| **Single Executable Binary** | `tanpopo` (`tanpopo serve --role=all\|data-plane\|control-plane`) | `tanpopod`, `tanpopoctl` |
| **Hot-Path Ingress Packages** | `internal/ingress/{http,sse,native,idempotency,blob,cache,mcp,a2a}` | `internal/gateway/*`, `internal/mcp/*`, `internal/a2a/*` |
| **Artifact File Extensions** | Extension package: **`.tpkg`** · Encrypted backup: **`.tpop.enc`** · Air-gap bundle: **`.tpbundle`** | `.tppkg`, `.tpbak` |
| **6 Canonical RBAC Roles (`CR-04`)** | `OPERATOR`, `ORG_OWNER`, `TEAM_ADMIN`, `SECURITY_REVIEWER`, `FINOPS_VIEWER`, `DEVELOPER` | `org_admin`, `project_admin`, `billing_viewer`, `instance_super_admin` |
| **Virtual Key Format & Hashing** | `vk_tp_<env>_<32_base62>` · Plain `SHA-256(rawKey)` in `active_key_hash` & `previous_key_hash` | `tpk_live_...`, ad-hoc salted string hashes |
| **Primary Key IDs (`UUIDv7`)** | `<prefix>_<26_chars_crockford_base32>` (`<= 32` chars inside `VARCHAR(40)`) | `prefix_` + 36-char hex overflowing 40 chars |
| **Security & Trace HTTP Headers** | CSRF: `X-CSRF-Token` · Request ID: `x-tanpopo-request-id` | `X-Tanpopo-CSRF`, `X-Request-ID` |
| **Critical HTTP Status & Error Codes** | Budget exhausted: **`429 project_budget_exceeded`** · Residency/Deployment unavailable: **`503 no_compliant_deployment_available`** · Native dialect mismatch: **`400 dialect_mismatch`** · Wasm trap/timeout: **`502 extension_execution_fault`** · A2A loop/hop limit: **`400 delegation_depth_exceeded`** · Bootstrap replay: **`410 bootstrap_already_completed`** | `402 budget_exceeded`, `503 all_compliant_deployments_unavailable`, `502 plugin_execution_trapped` |
| **Canonical Physical SQL Tables (`26`)** | Use exact names `config_changelog`, `budget_ledgers`, `budget_reservations`, `usage_settlements`, `usage_rollups_hourly`, `request_traces_meta`, `approval_requests`, `webhook_subscriptions`, `valkey_hold_checkpoints` | `config_revisions`, `budgets`, `usage_ledger_entries`, `request_traces`, `mcp_approval_requests`, `webhook_endpoints` |
| **`tanpopo` CLI Exit Codes (`SPEC-OPS-04`)** | `0` (OK), `2` (Validation/Args), `10` (Network/Timeout), `14` (Auth/Permission/Canary Leak), `22` (Contract/Hash/Integrity Fail), `30` (Internal), `31` (Dirty Migration) | Inventing exit codes `3` or `4` |
| **Cross-OS Go Test Execution** | **Always separate into 2 commands** (Go `-race` requires `CGO_ENABLED=1`):<br/>1. Pure-Go check: `CGO_ENABLED=0 go test ./...` *(PowerShell: `$env:CGO_ENABLED='0'; go test ./...`)*<br/>2. Race check: `go test -race ./...` *(or `go run ./scripts/ci/check.go`)* | Combining `CGO_ENABLED=0` and `-race` on one command (`go: -race requires cgo`) |

---

## 3. Source-of-Truth Document Precedence

During unit execution, resolve questions using this strict hierarchy (and if a genuine contradiction is discovered, stop and open a `CR` under Section 5 rather than guessing):
1. **Tier 1 (Supreme Governance):** [`AGENTS.md`](../../AGENTS.md) + [`docs/governance/decisions.md`](./decisions.md) (`CONFIRMED_BY_OWNER` / `APPROVED` entries) + this `execution-baseline.md`.
2. **Tier 2 (Machine-Readable Contracts — generated in `M0`):** `proto/tanpopo/extension/v1/runtime.proto`, `openapi/{dataplane-v1,admin-v1}.yaml`, `db/spec-check/{sqlite,postgres}_schema.sql`, `CONSTRAINTS.md`.
3. **Tier 3 (Requirements, Security & Verification Gates):** [`srs.md`](../requirements/srs.md), [`acceptance-criteria.md`](../requirements/acceptance-criteria.md), [`docs/security/*`](../security/threat-model.md), [`docs/quality/*`](../quality/contract-test-matrix.md).
4. **Tier 4 (Architecture, Deep Specs & UX/Ops):** [`docs/architecture/*`](../architecture/system-design.md) (`ADR-0001`..`0006`), [`docs/specifications/*`](../specifications/extension-contract.md), [`docs/ux/*`](../ux/screen-specifications.md), [`docs/operations/*`](../operations/installer-upgrades.md).
5. **Tier 5 (Unit Execution Spec & Index):** [`docs/planning/units/TP-IMP-xxxx.md`](../planning/units/TP-IMP-0001.md) & [`docs/planning/implementation-index.md`](../planning/implementation-index.md).

---

## 4. Dependency-Ordered Initial Execution Queue (Once Unblocked)

> **Live Status Today:** `0` units `READY` (`NO_GO`). Immediately upon Product Owner sign-off of [`owner-approval-sheet.md`](./owner-approval-sheet.md), units unlock in the following strict topological waves:

| Wave | Unit ID(s) | Execution Mode | Mandatory Review / Gate Checkpoint Before Next Wave |
|---|---|---|---|
| **Wave 0.1** | [`TP-IMP-0001`](../planning/units/TP-IMP-0001.md) | Sequential (`1` branch) | `REQUIRES_STRONG_REVIEW` — Closes `GAP-08, 09, 11`. |
| **Wave 0.2** | [`TP-IMP-0002`](../planning/units/TP-IMP-0002.md) | Sequential (`1` branch) | `REQUIRES_STRONG_REVIEW` — Extracts `runtime.proto` & `openapi/*.yaml` (`GAP-02, 03, 07, 10, 12, 13`). |
| **Wave 0.3** | [`TP-IMP-0003`](../planning/units/TP-IMP-0003.md) | Sequential (`1` branch) | `REQUIRES_STRONG_REVIEW` — Locks `26` SQL tables, `AEAD AAD`, `FORCE RLS`, `Valkey {org_id}` (`GAP-04, 05, 06`). |
| **Wave 0.4 / 1.1** | [`TP-IMP-0004`](../planning/units/TP-IMP-0004.md) **\|\|** [`TP-IMP-0005`](../planning/units/TP-IMP-0005.md) | **Parallel (`2` `git worktree`s, `0` file overlap)** | **`GATE-M0` (`Owner + Architect Sign-Off`)** after `0004` + `check.go` green on `0005`. |
| **Wave 1.2** | [`TP-IMP-0006`](../planning/units/TP-IMP-0006.md) **\|\|** [`TP-IMP-0007`](../planning/units/TP-IMP-0007.md) | **Parallel (`2` `git worktree`s)** | Hermetic `netguard` (`0006`) + `SQLite WAL` migration runner (`0007`). |
| **Wave 1.3** | [`TP-IMP-0008`](../planning/units/TP-IMP-0008.md) → [`TP-IMP-0009`](../planning/units/TP-IMP-0009.md) | Sequential | `REQUIRES_STRONG_REVIEW` — 3-tier Envelope Vault + `RBAC` / `Virtual Key` / `ConfigSnapshot`. |
| **Wave 1.4** | [`TP-IMP-0010`](../planning/units/TP-IMP-0010.md) **\|\|** [`TP-IMP-0011`](../planning/units/TP-IMP-0011.md) | **Parallel (`2` `git worktree`s)** | `REQUIRES_STRONG_REVIEW` — `SSRFGuard` + `BlindBroker` + `.tpkg` Installer `SM-01`. |
| **Wave 1.5** | [`TP-IMP-0012`](../planning/units/TP-IMP-0012.md) **\|\|** [`TP-IMP-0013`](../planning/units/TP-IMP-0013.md) → [`TP-IMP-0014`](../planning/units/TP-IMP-0014.md) → [`TP-IMP-0015`](../planning/units/TP-IMP-0015.md) | Parallel `0012\|\|0013`, then `0014 -> 0015` | **`GATE-M1` (`Vertical Slice Security & Architecture Gate`)** — Proves `EXT-SLICE-M1`. |

---

## 5. Plan & Contract Change Management Protocol (`Change Requests`)

No Engineering Agent may unilaterally modify files outside `In-Scope (Files Touched)`, rename public API fields/SQL tables, or weaken `SLO` / `CONSTRAINTS.md` thresholds. When an empirical blocker arises:
1. **Stop-the-Line:** Keep the working branch clean and append a structured `CR-<NN>` entry to [`docs/reviews/unresolved-gaps.md`](../reviews/unresolved-gaps.md) documenting: *Empirical Evidence → Options & Trade-Offs → Impacted `TP-IMP-*` Units*.
2. **Approval Authority:**
   - *Internal non-breaking structural refinements* (no public API, security, schema, or scope impact): Principal Architect reviews and updates `implementation-index.md`.
   - *Changes affecting product scope, `DEC-004` compliance, security isolation, persistence schemas, wire protocols, or `SLOs`*: **Mandatory Product Owner approval** recorded in [`decisions.md`](./decisions.md) before proceeding.

---

## 6. Cold-Start Engineering Agent Prompt & Mandatory Completion Report Contract

### 6.1. Canonical Single-Unit Prompt Template (Zero Prior Chat Context Required)
```text
Act as a Staff Implementation Engineer on Tanpopo. Execute solely unit: TP-IMP-xxxx.
Step 1: Read AGENTS.md, docs/governance/execution-baseline.md, docs/planning/implementation-index.md, and docs/planning/units/TP-IMP-xxxx.md.
Step 2: Verify governance gates. If HANDOFF_VERDICT is NO_GO or the unit's DoR / upstream dependencies are not satisfied, STOP immediately and report the exact blocker without writing code.
Step 3: Once READY, emit a concise 3-5 line plan confined to In-Scope (Files Touched), follow strict TDD (TANPOPO_TEST_HERMETIC=1, zero external network calls), implement the exact contracts using the canonical dictionary in execution-baseline.md, run all Section 6 verification commands (both CGO_ENABLED=0 and -race, plus astryx doctor --built where applicable), and produce the 7-part Unit Completion Report.
```

### 6.2. Mandatory 7-Part Unit Completion Report Contract
Every PR / completion report for a `TP-IMP-xxxx` unit **MUST** contain all 7 sections below:
1. **`Unit & Gate Check`:** `TP-IMP-xxxx` ID, confirmation of `DoR` satisfaction, and `Gate G4` status.
2. **`Scope Compliance`:** Exact `git status -s` / `git diff --stat` reconciled 1-to-1 against `In-Scope (Files Touched)`.
3. **`Acceptance & Negative Criteria Matrix`:** Pass/fail mapping from every `AC-1..n` and `NEG-1..n` to concrete automated `Test...` function names.
4. **`Hermetic & Floor-Guard Verification`:** Recorded exit codes and output summaries for: (a) `CGO_ENABLED=0` build/test, (b) `-race` test, (c) `CanaryScanner` (`0` secret leaks), (d) `Floor Guard` (`0` `@ts-ignore`/`nolint`/`t.Skip`/raw `div`-`span`), and (e) `npx @astryxdesign/cli doctor --built dist` (`0 fail`, for UI units).
5. **`Security & Rollback Check`:** Confirmation that Section 7 `Security Invariant` holds and rollback remains clean.
6. **`Review Checkpoint Routing`:** Units tagged **`REQUIRES_STRONG_REVIEW`** (`37/47`) require a fresh-context adversarial pass (`security-auditor` + `code-reviewer`) and Lead Engineer sign-off prior to merge.
7. **`Index & Traceability Sync`:** Status update of `TP-IMP-xxxx` to `DONE` in [`implementation-index.md`](../planning/implementation-index.md) and promotion of newly unblocked downstream units to `READY`.
