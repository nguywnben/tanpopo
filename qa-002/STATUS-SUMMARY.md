# QA-002 consolidated status summary

**Frozen baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`  
**Latest checkpoint:** CP53
**Release gate:** `GATE-REL-07 BLOCKED`  
**QA state:** In progress; coverage remains incomplete. QA-001 results are not inherited.

## Historical defects DEF-001–015

| Status | IDs | Notes |
| --- | --- | --- |
| Verified fixed for scoped post-baseline scenarios | DEF-001–004, DEF-006–015 | Each passes only the specific QA-002 retest scenarios recorded in [DEFECT-RETEST-REGISTER.md](DEFECT-RETEST-REGISTER.md). This is not a full role/platform regression. |
| Partial pass; residual security/support coverage open | DEF-005 | Explicit private-IP/CIDR positive/negative controls passed for the executed cases. Redirects were not followed; DNS rebinding, alternate runtimes, and IPv4-mapped endpoint support are not resolved. See QA-OBS-004/007 and CP25/40–42. |

## Confirmed open defects

| ID | Severity | Current summary |
| --- | --- | --- |
| DEF-016 | Moderate | Team Admin member creation succeeds, but unscoped member-list refresh is forbidden and UI reports failure/empty list. |
| DEF-017 | Moderate | Playground polls a request ID where trace lookup expects a trace ID; success card remains pending. |
| DEF-018 | Moderate | FinOps/API report Virtual Key lifetime budgets as monthly. |
| DEF-019 | Moderate | Playground hides canonical budget rejection reason; CP51 extends the generic-429 reproduction to organization enforcement on a child project/environment request. |
| DEF-020 | Moderate | Security shell badges remain stale after clearing switches or deciding approvals until reload. |
| DEF-021 | Moderate | Local HUMAN member creation has no supported enrollment path for first login. |
| DEF-022 | Moderate | Session UI retains revoked role/navigation/actions until reload after server rejects the session. |
| DEF-023 | Moderate | Organization member list is falsely empty without both project and environment selected; CP43 expanded the repro. |
| DEF-024 | Moderate | Project FinOps view hides an applicable persisted organization ceiling and displays a false empty state. |
| DEF-025 | Low | Models enables Add deployment without environment scope; its form cannot select an environment and reports a page-level validation after submission. |
| DEF-026 | Low | MCP sync banner reports zero schema changes requiring approval while a first-discovered schema is visibly locked pending review. |
| DEF-027 | Moderate | Routes & Policies cannot distinguish same-model deployments with identical region/health labels; provider and deployment identity are hidden from target choices and route summaries. |

All details, reproduction steps, expected behavior, evidence and acceptance criteria are in the linked [defect reports](reports/). CP47 adds a scoped loopback positive control and confirms Team Admin cross-project sync denial before fixture contact; DEF-026 captures the inconsistent first-review notice.

## Open observations

- QA-OBS-001: A2A graceful key revocation during a dispatched task; contract unresolved.
- QA-OBS-003: overview presents “All systems nominal” despite substantial request failures; CP53 recurred at 4 requests / 25% error / zero alerts.
- QA-OBS-004: MCP CIDR sync failure uses generic retryable diagnostics.
- QA-OBS-005: budget activation/backfill semantics.
- QA-OBS-006: trace and settlement identifiers differ; correlation contract unresolved.
- QA-OBS-007: IPv4-mapped private endpoint notation rejected; supported endpoint contract unresolved.
- QA-OBS-008: MCP Hub org-wide list shown without project labels; global vs selected-project presentation contract unresolved.
- QA-OBS-009: manual project/environment selection returns to the runtime-selected default after refresh; workspace persistence contract unresolved.

## Coverage

- **Completed in CP43–53:** real Chromium scope switching across organization-only, project/no-environment, project/environment and a second project; navigation/refresh; visually inspected Access, FinOps, Routes, Traces, Playground, MCP, A2A, Overview, Providers, Models, Extensions and Security states; API/persisted-state comparison for members and budgets; Models no-environment create attempt generated validation but no mutation; real Team Admin sessions confirmed project chooser/MCP filtering and 403 on different-project reads/sync. CP47 owner sync contacted only the allowlisted synthetic fixture, visually exposed the first-schema lock/status mismatch, and owner pin resolved it. CP48 delayed a project1 FinOps budget response during a real workspace switch; skeletons replaced old rows and the selected project rows appeared after settlement. CP49 confirmed refresh returns a manually selected project to the fresh-login/runtime default; affected rows changed with scope and remained persisted, leaving preference behavior as QA-OBS-009. CP50 exercised HTTP 302/303 MCP redirects; both failed closed before target contact with no tool persistence. CP51 exercised one synthetic child project/environment route under an organization budget: positive control settled 2 micro-USD; reduced org limit caused a generic Playground 429 with canonical trace reason and no upstream dispatch; limit restored and temporary Playground keys revoked. CP52 exercised a two-target priority-failover stream: first synthetic provider returned 503, backup returned 200 and the trace waterfall recorded both ordered deployment IDs; a temporary key was revoked. The route UI rendered three distinct deployments with identical labels (DEF-027); DEF-017 pending metadata recurred.
- **Blocked:** CP53 public webhook registration/delivery remains BLOCKED because no usable Operator credential is available for the isolated runtime. The external receiver token was deleted without receiving any data. Other test areas continue. Organization-only policy editing was not attempted because the UI requires project/environment and its intended contract remains unclear. Google adapter/runtime failures and RC4 artifact verification remain as recorded in prior checkpoints.
- **Remaining:** broader independent retest of DEF-001–027 on additional roles/platforms; full non-owner cross-project authorization; workspace transitions under latency and failure; organization policy contract; multi-project MCP labeling/actions and first-discovery/drift counts; HTTP redirects beyond CP41/50, HTTPS redirects, redirect chains, DNS rebinding/mixed DNS and alternate runtime behavior; scope races outside FinOps and delayed/error refresh recovery; workspace persistence preference contract; effective budget display plus isolated spend/enforcement regression (CP51 verifies one organization ceiling enforcement path, while DEF-024 display remains open); simultaneous ancestor/key budget caps and precedence; remaining screens, business workflows, browser/platform modes, integrations and operational failures. See [coverage matrix](COVERAGE-MATRIX.md) for detailed PASS/FAIL/BLOCKED/UNVERIFIED entries.

## CP53 consolidated coverage update

- **Historical defects:** no defect retested in CP53; existing scoped statuses are preserved.
- **Findings:** no new defect. QA-OBS-003 recurred.
- **Coverage completed:** exact Org Owner Operations navigation boundary; attempted alternate Operator credentials; disposable public receiver provisioned and deleted without any event payload.
- **Coverage blocked:** webhook registration, delivery, signature validation and retry until an Operator test credential is available.
- **Coverage remaining:** all unaffected regression areas continue; webhook blocker does not suspend other QA.

## CP52 consolidated defect update

- **Historical DEF-001–015:** no CP52 retest; preserve scoped statuses and limits. DEF-005 remains partial.
- **Open findings:** DEF-016–027 remain open. DEF-017 recurred after successful failover; DEF-027 is newly confirmed. DEF-019 retains the CP51 organization-budget expansion; DEF-024 remains a visibility failure.
- **Coverage completed:** real provider connection/probe, same-model second deployment, priority-failover route, synthetic 503→200 streaming response, persistent ordered trace and key revocation.
- **Coverage blocked:** none in CP52.
- **Coverage remaining:** multi-target all-fail/timeout/cancel/weights, route edit/reorder and provider-specific failures; see matrix.

## CP51 consolidated defect update

- **Historical DEF-001–015:** CP51 changed no historical retest disposition; preserve their scoped QA-002 results and residual limits as recorded above/register. DEF-005 remains partial.
- **Confirmed findings:** DEF-016–026 remain open. CP51 expands DEF-019 to organization-scope budget rejection and observes DEF-017 pending trace metadata again. DEF-024 remains a UI visibility failure despite the scoped backend enforcement pass. No new defect ID was added.
- **Coverage completed:** one full synthetic provider→model→healthy deployment→route→streaming Playground journey; organization cap positive/rejection/recovery; UI and DB verification; test-key revocation.
- **Coverage blocked:** none in CP51. Google credentials/provider adapter and RC4 artifact limitations remain as historical environment statuses.
- **Remaining:** simultaneous budget scope precedence, additional roles/browsers, provider-side 429 differentiation, and cross-screen recovery remain unverified; see matrix.

## Recommended engineering handoffs

1. DEF-017, DEF-018, DEF-019, DEF-020, DEF-023 and DEF-024: prepare cross-screen state and business visibility fixes with end-to-end persistence regressions.
2. DEF-027: disambiguate route target selection and fallback summaries by provider connection and deployment identity.
3. DEF-016 and DEF-021: agree the member enrollment/list contract and align UI success/error states with supported onboarding.
4. DEF-005: keep security boundaries fail-closed; engineering should define redirects and supported mapped-address notation before changing behavior. Do not weaken SSRF or authorization.
5. DEF-022: ensure live session role changes reconcile client state without exposing actions after server invalidation.
6. DEF-026: align sync result counts/copy with initial schema lock state while retaining the pin-before-use control.

`GATE-REL-07` remains **BLOCKED**. No product implementation was changed, no release was approved, and QA-002 is not complete.
