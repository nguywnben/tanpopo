# CP44 — Org-only Console surfaces and deployment form recovery

- **Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169` (frozen RC4)
- **Result:** FAIL DEF-025 (low severity); remaining observations passed for the narrowly checked empty/organization states only.
- **Environment:** isolated SQLite server at `127.0.0.1:18086`, local HTTPS proxy at `127.0.0.1:18445`, real Chromium at 1440×900. The synthetic ORG_OWNER and org-only scope were used. Screenshot images were opened and visually inspected.
- **Product source changes:** none. No automated test was run.

## Scope and outcome

1. Selected `No project / No env` through the real top-bar workspace menu and visited Overview, Providers, Models, Extensions, Security, and the Models deployment form.
2. Visually reviewed each screen. Overview showed 0 requests/0% errors/0 alerts in this isolated tenant. Providers showed no configured connections. Models showed no environment deployments. Extensions showed one active declarative package in the org/instance context. Security queue showed 0 approvals. These are limited empty/context reads; they do not validate every organization-only contract or mutation.
3. Models **Add deployment** remained enabled without an active environment. Clicking opened a drawer without any scope selector; clicking **Save deployment** showed “Select an active environment, logical model, and provider connection” outside the drawer. No POST/PUT was emitted and no deployment persisted. This confirms [DEF-025](../reports/DEF-025-model-deployment-drawer-unavailable-without-environment.md).
4. Observed similar global/organization surface behavior in MCP Hub and Extensions, but did not classify it as a defect without a documented current-workspace filtering contract. MCP Hub remains QA-OBS-008.
5. No role changes, model/connection creation, extension mutation, policy edit, kill-switch action, or provider request occurred.

## Evidence

- `../evidence/CP44-org-only-overview.png`
- `../evidence/CP44-org-only-providers.png`
- `../evidence/CP44-org-only-models.png`
- `../evidence/CP44-org-only-extensions.png`
- `../evidence/CP44-org-only-security.png`
- `../evidence/CP44-org-only-add-deployment-drawer.png`
- `../evidence/CP44-org-only-add-deployment-validation.png`

## Consolidated defect status

- **Historical DEF-001–015:** QA-002 retests classify DEF-001–004 and DEF-006–015 as verified fixed only for their recorded scoped scenarios; DEF-005 remains partial, with redirects, DNS rebinding, mapped-notation support, alternate runtimes and full adapter-specific behavior open. No QA-001 pass is inherited.
- **Confirmed open defects:** DEF-016 through DEF-025. CP43 reproduced/expanded DEF-023 and confirmed DEF-024; CP44 confirmed low-severity DEF-025. See [status summary](../STATUS-SUMMARY.md) and [retest register](../DEFECT-RETEST-REGISTER.md).
- **Unresolved observations:** QA-OBS-001, QA-OBS-003–008; especially QA-OBS-008 global MCP list presentation. All original uncertainty remains preserved.
- **Recommended handoff:** keep DEF-016–024 on active engineering intake; add DEF-025 to Console UX cleanup. DEF-005 remains a security validation track, preserving existing fail-closed SSRF and authorization behavior.

## Coverage state

- **Completed here:** real-browser org-only navigation through Overview, Providers, Models, Extensions and Security; empty-state screenshot inspection; scoped deployment create attempt proving no network mutation; persistent state was unchanged for deployments.
- **Blocked:** none in this phase. This did not test organization-wide policy writes or any deployment creation with a real active environment.
- **Remaining:** repeat actions in project/no-environment and project/environment; test non-owner role and project authorization for deployment create; broader loading/error/recovery and keyboard/focus behavior; other Console surfaces and browsers; historical defect and integration regression gaps in [coverage matrix](../COVERAGE-MATRIX.md).

`GATE-REL-07` remains **BLOCKED**. QA-002 is ongoing.
