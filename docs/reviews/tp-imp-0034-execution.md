# TP-IMP-0034 Execution Record

## Current checkpoint

- Unit: `TP-IMP-0034`
- Governance state: `READY` under `DEC-063`.
- Implementation branch: persistent `develop`, synchronized to `origin/main` at `c1dab09599c08250873b71cf8445ba9adc78de6b` after the Unit 33 acceptance PR merged.
- Unit 33 predecessor: DONE on `origin/main`; four required post-merge CI checks passed in run `44411945678`. Artifact/provenance verification is tracked in run `44411945857`.
- DoR: verified on `origin/main`: `TP-IMP-0003`, `TP-IMP-0010`, and `TP-IMP-0024` are DONE; `GAP-01` and `GAP-04` are CLOSED.
- Implementer and delegated acceptor: **GPT-6 Luna** under `DEC-060`.
- Review tier: `REQUIRES_STRONG_REVIEW`; final independent **code-reviewer** and **security-auditor** reviews required.
- No tag/release will be created (`GATE-REL-07`).

## Execution and independent review attribution

- Implemented by **GPT-6 Luna** under the active delegation in `DEC-060`; accepted by **GPT-6 Luna** after the required independent reviews and main CI gates pass.
- Initial code review on PR #11 SHA `e26d99d4b13cffd918e066ac1646017a44655969`: **Maxwell** (**code-reviewer**) — **APPROVE**, no required or optional findings.
- Initial security review on the same SHA: **Heisenberg** (**security-auditor**) — **PASS**, zero Critical/High/Medium findings; one Low observation remains that Go string copies of upstream token/client-secret values cannot be zeroized.
- Follow-up code review on PR #11 SHA `19497ac39c204d7a5be0bd1055c7cb18d6856add`: **Maxwell** (**code-reviewer** subagent; report signature **GPT-6 Luna**) — **REQUEST CHANGES** only to correct the stale review attribution in this record; no implementation or test findings.
- Follow-up security review on that SHA: **Heisenberg** (**security-auditor**) — **PASS**, zero Critical/High/Medium findings and the same Low observation. Reviewers did not run tests; execution evidence is tracked separately below.
- Final code review on PR #11 SHA `b7959c5abdce016ef3221da86a86e79ab5f52585`: **Maxwell** (**code-reviewer** subagent; report signature **GPT-6 Luna**) — **APPROVE**, no actionable findings.
- Final security review on that SHA: **Heisenberg** (**security-auditor**) — **PASS**, zero Critical/High/Medium findings; the same Low observation remains. Reviewers did not run tests; CI provides the test evidence below.
- Follow-up review of the added Vault regression tests on PR #11 SHA `73c52d99e486902e5f8389f76ce79d88eb317aee`: **Maxwell** (**code-reviewer** subagent; report signature **GPT-6 Luna**) — **APPROVE**, no actionable findings; **Heisenberg** (**security-auditor**) — **PASS**, zero Critical/High/Medium findings and the same Low observation. No implementation code changed in this test-only update.
- Follow-up review of the added Vault edge cases on PR #11 SHA `6d17c6d2f563708fd8681eac799527795ba552a6`: **Maxwell** (**code-reviewer** subagent; report signature **GPT-6 Luna**) — **APPROVE**, no actionable findings; **Heisenberg** (**security-auditor**) — **PASS**, zero Critical/High/Medium findings and the same Low observation. This update contains tests and execution-record text only; CI evidence is tracked below.
- The release artifact dry run exposed that a public listener without `TANPOPO_OAUTH_REDIRECT_URI` caused runtime startup to fail. **DEC-068** makes the OAuth coordinator optional until an explicit HTTPS callback is configured while preserving `503 oauth_unavailable` and fail-closed broker behavior; a runtime regression test covers public startup and OAuth unavailability. Final independent review and CI are pending on this updated diff.
- **Popper** (**test-engineer**) contributed the OAuth refresh and concurrency test plan and tests; the final CI runs exercise those tests on the accepted diff.

## Contract reconciliation

- **DEC-064** assigns `0016_oauth_sessions.sql` as the next migration because `0011_routes.sql` and `0012`..`0015` are already used. Existing migration history remains immutable.
- Implement the OAuth start and callback routes declared by `openapi/admin-v1.yaml`; add an OpenAPI operation for RFC 8628 device polling, which the current API contract lacks.
- Keep upstream OAuth HTTP clients behind `SSRFGuardDialer`, bind state to the authenticated principal, organization, connection, and exact browser cookie session, and keep token plaintext out of persistence, API responses, redirects, and logs.
- **DEC-066** records final review remediations: persist grant type in Vault; require reauthorization for expired PKCE/device grants without refresh tokens; coordinate refresh with `oauth_token_leases`, reload token state after acquisition, and pass the expected RFC 8707 resource into token resolution; atomically claim and complete device polls with token persistence; reject bearer-authenticated browser handshakes; and test 200 concurrent broker calls plus audience mismatch.

## Scope and required evidence

Implement Authorization Code + PKCE S256, Client Credentials, and authorized RFC 8628 Device Flow; persist short-lived authorization state; seal token pairs in Vault; serialize token refresh per credential; bind tokens to the exact RFC 8707 resource; stop retrying `invalid_grant`, mark reauthorization required, and emit the required alert. Add migration, OpenAPI, HTTP, repository, concurrency, state-replay, audience-mismatch, and token-redaction coverage required by the unit spec (`CT-19`, `AUTH-09`, `AUTH-10`, `AUTH-16`, `FMEA-08`).

Windows CI exposed a SQLite file-handle teardown failure in `TestDataPlaneRoleDoesNotMountControlPlaneOrMintBootstrapToken`; cleanup now allows a bounded 30-second Windows teardown window and still fails if the runtime retains the database handle. The required checks will be rerun on the final diff.

The final cross-platform CI run exposed a Vault package coverage shortfall; focused validation cases are being added for the new OAuth session and transactional rotation APIs. The artifact dry run is tracked on the current PR #11 head. The merge SHA and post-merge acceptance CI will be recorded after merge. Unit 35 remains blocked until Unit 34 is DONE on main with green acceptance CI.
