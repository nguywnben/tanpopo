# CP01 — Exact-source build and fresh-install journey

Date: 2026-11-25 (Asia/Saigon)

## Build provenance

- Source: RC5 `3423d4f523ef0ac77469a1201b8b753d1d31c902`.
- No RC5 release artifact was available in the GitHub release listing; original artifact verification remains **UNVERIFIED**.
- Built an isolated static Linux binary with Go `1.26.11` in the official Go container using the RC5 `go.mod`/`go.sum`, `-mod=readonly`, and `-trimpath`. The provided Cloud session CA was mounted read-only to permit verified HTTPS module resolution. No source files or dependency declarations were changed.
- Injected metadata: version `0.1.0-dev` (source default), exact RC5 commit, and the RC5 commit timestamp. `tanpopo version --output json` returned all three expected values.
- Built binary SHA-256: `c04227449313e9e9befaa9e02952f9dd15edbc8e24a368fe678e4720e467cec1` (temporary binary under `/tmp`, not published).
- First container dependency fetch without the session CA failed TLS verification. Retrying with the provided CA read-only succeeded with TLS verification enabled. This was an isolated container trust-store gap.

## Isolated runtime

- New local SQLite runtime at `127.0.0.1:18103`, backed by `/tmp/qa003-runtime/data/tanpopo.db` and a newly generated private Master KEK file. No existing QA runtimes were modified.
- `/healthz` returned HTTP 200. Cloud QA provider credentials were not used; aggregate external-provider spend remains USD 0.

## Real browser journey

Chromium 156, 1440×1000 viewport, separate clean browser contexts:

1. Opened `/setup`; visually inspected the initial Setup screen and captured [initial Setup screenshot](../evidence/CP01-setup-initial.png).
2. Completed owner/bootstrap fields and proceeded through the optional provider, custody, and review steps. The provider step accurately showed that no provider adapters were enabled; visually inspected [provider empty-state screenshot](../evidence/CP01-setup-provider-empty.png).
3. Submitted Setup, reached Overview, and captured [completed Setup screenshot](../evidence/CP01-setup-complete-overview.png).
4. Started a separate browser context, signed in with the newly created synthetic owner, hard-reloaded Overview, and confirmed the session and default organization/project/environment context persisted. Captured and visually inspected [fresh-login screenshot](../evidence/CP01-fresh-login-overview.png).
5. On the initial Setup view, the four visible step labels (Owner account, First connection, Key custody, Review) appeared in each step button’s accessible name. This independently passes that narrow live-browser criterion for DEF-001 on RC5. Original test-runner cases remain to be rerun; DEF-001 stays PARTIAL.

Machine-readable observations and screenshot index: [`CP01-fresh-install.json`](../evidence/CP01-fresh-install.json). Screenshot files contain no bootstrap token or entered credentials. Synthetic owner credentials and runtime keys remain outside the repository with restrictive permissions.

## Scope disposition

- **PASS, scoped:** exact source build/metadata; service health; local fresh install; optional-provider empty state; synthetic owner bootstrap; separate-session login and reload; visible step text included in stepper accessible names.
- **PARTIAL:** DEF-001, SCR-01, WF-01. Original static test cases, alternate browser engines/viewports, adapter activation, provider connections, user roles, other workflows, persistence beyond the observed session, and operational recovery remain open.
- **UNVERIFIED:** RC5 release artifact identity; browser Firefox/WebKit; full feature/screen/workflow matrix; all remaining defect acceptance criteria.
- No new defect is asserted from this phase. GATE-REL-07 remains **BLOCKED**.
