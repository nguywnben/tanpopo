# QA-003 — Complete Alpha/Beta Regression

**Status:** In progress; not complete. **GATE-REL-07: BLOCKED.**

## Frozen baseline and immutable history

- RC5 source: `3423d4f523ef0ac77469a1201b8b753d1d31c902`
- QA-001 branch / commit: `qa/qa-001-handoff-cp65` / `d994f82128f0e78a4e7bb7dd4ecd705cee60d85f`
- QA-002 branch / commit: `qa/qa-002-regression` / `1052ecb25a51e7ea94045946ede865ee062d4419`
- Product source and tests are not changed by this branch. Only `qa-003/` documentation and sanitized QA evidence belong here.

## Current checkpoint

See [CP00 — baseline and environment inventory](checkpoints/CP00-baseline-environment.md), [CP01 — exact-source build and fresh-install journey](checkpoints/CP01-fresh-install-and-provenance.md), [CP02 — crypto-shred and restore](checkpoints/CP02-crypto-shred-and-restore.md), [CP03 — membership scope and enrollment](checkpoints/CP03-membership-scope-and-enrollment.md), [CP04 — audit failure atomicity](checkpoints/CP04-audit-failure-atomicity.md), [CP07 — provider failure, identity, and disable recovery](checkpoints/CP07-provider-failure-and-duplicate-identity.md), [CP08 — first budget and duplicate-create guard](checkpoints/CP08-finops-first-budget-and-duplicate-guard.md), [CP09 — organization budget visibility](checkpoints/CP09-finops-ancestor-budget-visibility.md), [CP10 — CLI config command resolution](checkpoints/CP10-cli-config-command-resolution.md), [CP11 — SSRF boundaries and MCP/A2A registration](checkpoints/CP11-ssrf-boundaries-and-agent-registration.md), [CP12 — MCP/A2A discovery and task lifecycles](checkpoints/CP12-mcp-a2a-discovery-and-task-lifecycles.md), [CP13 — SSRF defaults and controlled DNS rebinding](checkpoints/CP13-ssrf-defaults-and-dns-rebinding.md), and [CP14 — FinOps exports](checkpoints/CP14-finops-export-formats-and-dimensions.md). QA-003 independently retests all 39 historical defects; prior campaign results are reference material only.

CP05 adds the [webhook lifecycle retest](checkpoints/CP05-webhook-lifecycle.md). CP06 records [WebKit mobile coverage and browser runtime limits](checkpoints/CP06-webkit-mobile-and-browser-runtime.md).

## Latest consolidated status (CP14)

- Historical defects: **15 VERIFIED FIXED for scoped RC5 scenarios** (DEF-002, DEF-003, DEF-004, DEF-009, DEF-010, DEF-011, DEF-014, DEF-015, DEF-021, DEF-023, DEF-024, DEF-026, DEF-033, DEF-034, DEF-038); **3 PARTIAL** (DEF-005, DEF-007, DEF-032); **21 UNVERIFIED**. These labels do not imply full acceptance closure. No prior QA campaign PASS is inherited.
- New defects: **DEF-040 Moderate** — disabling an unverified provider terminally archives it without confirmation/recovery guidance; its attached deployment remains visually Provisioned while the backing connection is disabled. Product intent and replacement workflow remain unverified.
- Coverage: partial on FT-03, FT-04, FT-07, FT-08, FT-09, FT-11, FT-12, FT-13, FT-15, FT-16; SCR-01, SCR-02, SCR-03, SCR-04, SCR-05, SCR-10, SCR-12, SCR-13, SCR-14, SCR-16; WF-01, WF-03, WF-05, WF-08, WF-09, WF-13. WebKit mobile is partial; Firefox is blocked by container sandbox/filesystem restrictions; other platforms are unverified. CP11/12 rejected wildcard CIDRs and CP12 confirmed exact `/32` persistence for MCP/A2A. CP13 corrected the provider form default record: private/loopback access starts checked with `127.0.0.1/32`; submitting an untouched default created a `CREATED_UNVERIFIED` loopback connection. A controlled MCP DNS rebind made three requests to the explicitly allowed initial `/32`, then zero requests after resolution changed outside the allowlist. The UI surfaced a generic 502. This scoped run does not cover mixed answers, races, A2A/provider parity or other transports. CP12 exercised MCP discovery, schema approval, grants, tool call and drift lock, plus A2A Virtual Key provisioning/discovery, input-required UI resume, settlement and cancellation. CP14 reconciled populated `$0.002468` exact usage across Project, Environment, Virtual Key and Model views and independently downloaded valid CSV and JSON for each selected group, verifying the group key and totals. Date filters, multi-row aggregation and role coverage remain open. Advanced MCP/A2A roles, side effects and delegation remain open. Other coverage remains UNVERIFIED. RC5 release artifact verification remains UNVERIFIED because the artifact was stored in a separate DevBox not available here.
- Active environment: fresh and restored local SQLite runtimes, real Chromium interactions and visually inspected screenshots. Go build/dependency resolution succeeded in the container; the interrupted race suite package was rerun after installing its test runtime dependency and passed (CP01). External provider spend is USD 0.
- Remaining: independently retest 21 still-unverified historical defects and complete residual acceptance for partial findings, continue all feature/screen/workflow and platform coverage, and test other persistence backends, webhook delivery, and remaining DEF-007 paths. QA-003 is not complete; `GATE-REL-07` remains **BLOCKED**.

## Records

- [Historical defect retest register](DEFECT-RETEST-REGISTER.md)
- [Full product coverage matrix](COVERAGE-MATRIX.md)
- [CP11 — SSRF and MCP/A2A](checkpoints/CP11-ssrf-boundaries-and-agent-registration.md)
- [CP12 — MCP/A2A lifecycles](checkpoints/CP12-mcp-a2a-discovery-and-task-lifecycles.md)
- [CP13 — SSRF defaults and DNS rebinding](checkpoints/CP13-ssrf-defaults-and-dns-rebinding.md)
- [CP14 — FinOps exports](checkpoints/CP14-finops-export-formats-and-dimensions.md)
- [DEF-040 handoff](reports/DEF-040-unverified-provider-disable-is-terminal.md)
- Checkpoints and evidence are appended under `checkpoints/` and `evidence/`.
- New findings begin at DEF-040 in `reports/`.

## Operating constraints

Use isolated synthetic tenants/endpoints and keep cumulative real provider spend at or below USD 3. Credentials must not appear in docs, screenshots, logs, or source control. No implementation fixes, releases, tags, or human release approval are part of QA-003. Blocked and untested work remains explicitly labeled. QA continues past routine checkpoints.
