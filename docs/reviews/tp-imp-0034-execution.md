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

## Contract reconciliation

- **DEC-064** assigns `0016_oauth_sessions.sql` as the next migration because `0011_routes.sql` and `0012`..`0015` are already used. Existing migration history remains immutable.
- Implement the OAuth start and callback routes declared by `openapi/admin-v1.yaml`; add an OpenAPI operation for RFC 8628 device polling, which the current API contract lacks.
- Keep upstream OAuth HTTP clients behind `SSRFGuardDialer`, bind state to the authenticated principal, organization, and connection, and keep token plaintext out of persistence, API responses, redirects, and logs.

## Scope and required evidence

Implement Authorization Code + PKCE S256, Client Credentials, and authorized RFC 8628 Device Flow; persist short-lived authorization state; seal token pairs in Vault; serialize token refresh per credential; bind tokens to the exact RFC 8707 resource; stop retrying `invalid_grant`, mark reauthorization required, and emit the required alert. Add migration, OpenAPI, HTTP, repository, concurrency, state-replay, audience-mismatch, and token-redaction coverage required by the unit spec (`CT-19`, `AUTH-09`, `AUTH-10`, `AUTH-16`, `FMEA-08`).

Final implementation SHA, CI run links, reviewer names/verdicts, merge SHA, and post-merge acceptance CI will be added after those results are available. Unit 35 remains blocked until Unit 34 is DONE on main with green acceptance CI.
