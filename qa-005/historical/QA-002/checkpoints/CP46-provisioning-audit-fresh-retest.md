# CP46 — Fresh provisioning audit-event and chain retest

- **Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169` (frozen RC4)
- **Result:** PASS for exact Console-created project, environment, and member audit events and the current chain verification; broader DEF-007 coverage remains open.
- **Environment:** isolated SQLite runtime, synthetic ORG_OWNER, real Chromium Console session.
- **Product source changes:** none. No automated tests were run.

## Procedure and outcomes

1. Opened **Security → Audit Log** in the real Console and visually inspected the current list.
2. Filtered the action field separately for `project.created`, `environment.created`, and `member.created`.
3. The project filter returned two persisted events, including CP43 secondary project at sequence 35. The environment filter returned two persisted events, including CP43 secondary environment at sequence 36. The member filter returned two events, including CP45 synthetic Team Admin principal at sequence 52.
4. For the CP43 project/environment events, the displayed actor matched the active ORG_OWNER principal and targets matched the persisted project/environment IDs. The CP45 member event showed the same owner as actor and the newly created principal as target.
5. Clicked **Verify hash chain**. Console reported “Hash chain verified” and “Cryptographic RFC 8785 JCS chain intact across 55 events.” This verification operation did not mutate application resources.
6. The synthetic CP45 Team Admin had already been suspended and its temporary password hash cleared after the authorization test. No credentials appear in evidence.

## Evidence

- `../evidence/CP46-security-audit-all.png`
- `../evidence/CP46-security-audit-project.png`
- `../evidence/CP46-security-audit-environment.png`
- `../evidence/CP46-security-audit-member.png`
- `../evidence/CP46-audit-chain-verified.png`

Each screenshot was opened and visually inspected during the retest.

## DEF-007 conclusion and limits

**PASS for the tested successful Console create paths**: project, environment, and member resources emitted expected audit events with visible actor/target attribution, and the current 55-event chain verified. This independently corroborates prior DEF-007 scoped passes; it does not prove atomicity under injected storage failure, standalone organization-create failure/delete/update coverage, or every provisioning path. Those remain UNVERIFIED and the severity/impact should not be downgraded beyond the tested acceptance slice.

## Consolidated status

- **DEF-001–015:** DEF-001–004 and DEF-006–015 verified fixed only in their recorded QA-002 test slices; DEF-005 partial with DNS rebinding, redirects, mapped notation, alternate runtimes and adapter-specific coverage still open. DEF-007 gained this fresh successful-path retest.
- **Open confirmed:** DEF-016–025; full per-defect acceptance remains in [register](../DEFECT-RETEST-REGISTER.md) and [status summary](../STATUS-SUMMARY.md).
- **Open observations:** QA-OBS-001 and QA-OBS-003–008; QA-OBS-008 still concerns labeling of organization-wide MCP rows, with the scoped Team Admin permission check passing.
- **Coverage:** audit create paths named above PASS; failure injection, other create/update/delete paths and org-create negative controls UNVERIFIED. No blocker prevented this phase.
- **Recommended handoffs:** retain current DEF-016–025 remediation set; DEF-007 requires resilience/atomicity validation before declaring that finding closed comprehensively.

`GATE-REL-07` remains **BLOCKED**. QA-002 remains in progress.
