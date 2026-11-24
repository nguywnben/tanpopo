# QA-002 consolidated status summary

**Frozen baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`  
**Latest checkpoint:** CP46
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
| DEF-019 | Moderate | Playground hides canonical project-budget rejection reason. |
| DEF-020 | Moderate | Security shell badges remain stale after clearing switches or deciding approvals until reload. |
| DEF-021 | Moderate | Local HUMAN member creation has no supported enrollment path for first login. |
| DEF-022 | Moderate | Session UI retains revoked role/navigation/actions until reload after server rejects the session. |
| DEF-023 | Moderate | Organization member list is falsely empty without both project and environment selected; CP43 expanded the repro. |
| DEF-024 | Moderate | Project FinOps view hides an applicable persisted organization ceiling and displays a false empty state. |
| DEF-025 | Low | Models enables Add deployment without environment scope; its form cannot select an environment and reports a page-level validation after submission. |

All details, reproduction steps, expected behavior, evidence and acceptance criteria are in the linked [defect reports](reports/).

## Open observations

- QA-OBS-001: A2A graceful key revocation during a dispatched task; contract unresolved.
- QA-OBS-003: overview presents “All systems nominal” despite substantial request failures.
- QA-OBS-004: MCP CIDR sync failure uses generic retryable diagnostics.
- QA-OBS-005: budget activation/backfill semantics.
- QA-OBS-006: trace and settlement identifiers differ; correlation contract unresolved.
- QA-OBS-007: IPv4-mapped private endpoint notation rejected; supported endpoint contract unresolved.
- QA-OBS-008: MCP Hub org-wide list shown without project labels; global vs selected-project presentation contract unresolved.

## Coverage

- **Completed in CP43–45:** real Chromium scope switching across organization-only, project/no-environment, project/environment and a second project; navigation/refresh; visually inspected Access, FinOps, Routes, Traces, Playground, MCP, A2A, Overview, Providers, Models, Extensions and Security states; API/persisted-state comparison for members and budgets; Models no-environment create attempt generated validation but no mutation; a real Team Admin session confirmed project chooser/MCP list filtering and 403 on a different project budget query.
- **Blocked:** no CP43–45 execution capability blocked. Organization-only policy editing was not attempted because the UI requires project/environment and its intended contract remains unclear. Google adapter/runtime failures and RC4 artifact verification remain as recorded in prior checkpoints.
- **Remaining:** broader independent retest of DEF-001–025 on additional roles/platforms; full non-owner cross-project authorization; workspace transitions under latency and failure; organization policy contract; multi-project MCP labeling/actions; effective budget display plus isolated spend/enforcement regression; remaining screens, business workflows, browser/platform modes, integrations and operational failures. See [coverage matrix](COVERAGE-MATRIX.md) for detailed PASS/FAIL/BLOCKED/UNVERIFIED entries.

## Recommended engineering handoffs

1. DEF-017, DEF-018, DEF-020, DEF-023 and DEF-024: prepare cross-screen state and business visibility fixes with end-to-end persistence regressions.
2. DEF-016 and DEF-021: agree the member enrollment/list contract and align UI success/error states with supported onboarding.
3. DEF-005: keep security boundaries fail-closed; engineering should define redirects and supported mapped-address notation before changing behavior. Do not weaken SSRF or authorization.
4. DEF-022: ensure live session role changes reconcile client state without exposing actions after server invalidation.

`GATE-REL-07` remains **BLOCKED**. No product implementation was changed, no release was approved, and QA-002 is not complete.
