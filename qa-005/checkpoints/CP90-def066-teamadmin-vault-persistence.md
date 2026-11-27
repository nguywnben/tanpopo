# CP90 — DEF-066 Team Admin Vault metadata persistence

**Source/runtime:** frozen unsigned RC7 source `737079ddb847d97dbcf832f610ed0048556cb65b`; Linux/amd64 binary SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`; exact-source isolated SQLite at `127.0.0.1:18162`; Chromium 156. Synthetic pure `TEAM_ADMIN`, assigned to `QA005 No Budget Project / QA005 Budgetless Dev`.

## Result

**PASS (scoped fresh QA-005 retest of DEF-066).** A project-bound Team Admin sealed a synthetic, non-provider API_KEY credential using the actual Console. The metadata appeared immediately, survived a hard refresh and was visible after signing in from a separate fresh browser context. The authenticated project-filtered API returned the same single metadata record with no plaintext. The intentionally unfiltered organization-wide list remained denied with HTTP 403 `insufficient_role_permission`. The Console delete action returned HTTP 204; a fresh filtered read and the UI showed zero rows. Historical QA-004 RC6 FAIL evidence remains preserved; this result only resolves the specified RC7 SQLite workflow.

## Reproduction

1. Sign in to the exact-RC7 Console as the pure Team Admin. Open Providers → Vault Secrets → Seal secret. Submit the synthetic name `QA005 CP90 fresh-login synthetic vault canary`, kind `API_KEY`, and an ephemeral fake value that is excluded from all logs and committed evidence. `POST /admin/v1/secrets` returned HTTP 201 with a project-owned metadata object and no plaintext field; the row appeared immediately.
2. Hard-refresh the Console, reopen Providers → Vault Secrets, and verify the same metadata row is present. The browser issued `GET /admin/v1/secrets?project_id=<assigned-project>&limit=100`, HTTP 200, one row, and no plaintext fields.
3. Open a new browser context and authenticate again as the same Team Admin. Providers → Vault Secrets again showed the persisted row. A direct filtered list returned HTTP 200 with one row and no plaintext; the unfiltered `GET /admin/v1/secrets?limit=100` returned HTTP 403 `insufficient_role_permission`. Connections and adapters remained visible in the same Providers view.
4. Delete the synthetic row through the real Console, confirm the action, and verify HTTP 204, the success notification, zero visible rows, and a subsequent filtered list with zero rows. No connection was created or bound to this canary; no provider request was made.

The test required a temporary loopback-only HTTPS proxy because Tanpopo correctly rejected the browser-origin/upstream-host mismatch on the first proxy (`csrf_validation_failed`, no state persisted). The test proxy on `127.0.0.1:19444` rewrote only its exact local browser Origin to the matching upstream authority; Tanpopo source/security settings were unchanged. CSRF-origin hardening across production reverse-proxy topologies is outside this checkpoint and remains separately unverified.

Visually inspected screenshots: [provider view before](../evidence/screenshots/CP90-teamadmin-providers-initial.png), [empty Vault before creation](../evidence/screenshots/CP90-teamadmin-vault-initial.png), [sealed row immediately](../evidence/screenshots/CP90-teamadmin-secret-immediate.png), [row after hard refresh](../evidence/screenshots/CP90-teamadmin-secret-after-refresh.png), [row after fresh login](../evidence/screenshots/CP90-teamadmin-secret-fresh-login.png), and [verified cleanup](../evidence/screenshots/CP90-teamadmin-secret-cleanup.png). Sanitized statuses, project binding, row counts and limitations: [CP90 JSON](../evidence/CP90-def066-teamadmin-vault-persistence.json). The fake plaintext value, cookies, CSRF token and session material were not retained.

## Scope limits

One project-bound Team Admin, one SQLite runtime, one synthetic API_KEY metadata row, one create/read/delete lifecycle and Chromium are covered. Other role boundaries, multiple assigned projects, cross-organization behavior, referenced credential deletion, credential binding/rotation, PostgreSQL/Valkey, Firefox/Safari and provider interoperability remain UNVERIFIED. Prior historical failures are not erased. No product source/specification changed; no paid provider usage occurred. `GATE-REL-07` remains BLOCKED.
