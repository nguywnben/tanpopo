# TP-IMP-0034 Execution Record

## Current checkpoint

- Unit: `TP-IMP-0034`
- Governance state: `DONE` under `DEC-069`; Unit 35 remains blocked until the acceptance governance CI passes on main.
- Implementation branch: persistent `develop`, fast-forward synchronized to `origin/main` merge SHA `e4a6fd37564f06c131be9432c9a8f4c0643d175a` after PR #11 merged.
- Unit 33 predecessor: DONE on `origin/main`; Unit 34 implementation PR #11 merged at main SHA `e4a6fd37564f06c131be9432c9a8f4c0643d175a`.
- DoR: verified on `origin/main`: `TP-IMP-0003`, `TP-IMP-0010`, and `TP-IMP-0024` are DONE; `GAP-01` and `GAP-04` are CLOSED.
- Implementer and delegated acceptor: **GPT-6 Luna** under `DEC-060`.
- Review tier: `REQUIRES_STRONG_REVIEW`; final independent reviews passed on the exact PR #11 implementation head.
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
- The release artifact dry run exposed that a public listener without `TANPOPO_OAUTH_REDIRECT_URI` caused runtime startup to fail. **DEC-068** makes the OAuth coordinator optional until an explicit HTTPS callback is configured while preserving `503 oauth_unavailable` and fail-closed broker behavior; a runtime regression test covers public startup and OAuth unavailability.
- Final independent code review on PR #11 SHA `1d88da49a4279229602bd1c3dc24112397f45865`: **Turing** (**code-reviewer**) — **APPROVE**, no actionable findings. Final security review on the same SHA: **Noether** (**security-auditor**) — **PASS**, zero Critical/High/Medium/Low findings. Reviewers did not run tests; CI run `44430250831` passed all four required jobs and artifact dry-run `44430250788` completed successfully on the same SHA.
- **Popper** (**test-engineer**) contributed the OAuth refresh and concurrency test plan and tests; the final CI runs exercise those tests on the accepted diff.

## Contract reconciliation

- **DEC-064** assigns `0016_oauth_sessions.sql` as the next migration because `0011_routes.sql` and `0012`..`0015` are already used. Existing migration history remains immutable.
- Implement the OAuth start and callback routes declared by `openapi/admin-v1.yaml`; add an OpenAPI operation for RFC 8628 device polling, which the current API contract lacks.
- Keep upstream OAuth HTTP clients behind `SSRFGuardDialer`, bind state to the authenticated principal, organization, connection, and exact browser cookie session, and keep token plaintext out of persistence, API responses, redirects, and logs.
- **DEC-066** records final review remediations: persist grant type in Vault; require reauthorization for expired PKCE/device grants without refresh tokens; coordinate refresh with `oauth_token_leases`, reload token state after acquisition, and pass the expected RFC 8707 resource into token resolution; atomically claim and complete device polls with token persistence; reject bearer-authenticated browser handshakes; and test 200 concurrent broker calls plus audience mismatch.

## Scope and required evidence

Implement Authorization Code + PKCE S256, Client Credentials, and authorized RFC 8628 Device Flow; persist short-lived authorization state; seal token pairs in Vault; serialize token refresh per credential; bind tokens to the exact RFC 8707 resource; stop retrying `invalid_grant`, mark reauthorization required, and emit the required alert. Add migration, OpenAPI, HTTP, repository, concurrency, state-replay, audience-mismatch, and token-redaction coverage required by the unit spec (`CT-19`, `AUTH-09`, `AUTH-10`, `AUTH-16`, `FMEA-08`).

Windows CI exposed a SQLite file-handle teardown failure; the test now permits a bounded 30-second Windows teardown window and still fails if the runtime retains the database handle. The Vault coverage shortfall was resolved by adding focused validation cases for OAuth sessions and transactional token rotation. The public-listener artifact smoke failure was resolved under `DEC-068`; the runtime stays available without OAuth callback configuration while OAuth endpoints return `503 oauth_unavailable` and broker resolution fails closed. The final callback review also restricted HTTP callbacks to exact loopback addresses in hermetic tests only.

Final implementation evidence: PR #11 head `1d88da49a4279229602bd1c3dc24112397f45865`; four PR checks in run [`44430250831`](https://github.com/nguywnben/tanpopo/actions/runs/44430250831) and artifact dry-run [`44430250788`](https://github.com/nguywnben/tanpopo/actions/runs/44430250788) succeeded. **Turing** (**code-reviewer**) APPROVE and **Noether** (**security-auditor**) PASS with zero findings on the final diff. PR #11 merged to main at `e4a6fd37564f06c131be9432c9a8f4c0643d175a`; post-merge four-job CI [`44432591862`](https://github.com/nguywnben/tanpopo/actions/runs/44432591862) and artifact/attestation [`44432591826`](https://github.com/nguywnben/tanpopo/actions/runs/44432591826) succeeded. **GPT-6 Luna** implemented and accepted Unit 34; **Maxwell** and **Heisenberg** contributed earlier code/security reviews; **Popper** (**test-engineer**) contributed the concurrency test plan; **Turing** and **Noether** delivered final reviews. Unit 35 DoR is verified; its READY transition remains gated on this acceptance governance PR passing CI on main. No tag or release was created.
