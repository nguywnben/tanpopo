# Security and Reliability Report

## Checkpoint 2026-11-23

No penetration, tenant-isolation, secret-handling, chaos, concurrency, restart, migration, backup/restore, or performance test was executed. Avoid production data and use disposable instances when resumed. Configured QA provider variable names are present; values were not read or logged and provider connectivity is unverified.

## Checkpoint 3

- Verified positive and negative RBAC path for Security Reviewer (Security visible; Operations hidden and direct route denied). Other roles and tenant isolation remain untested.
- Google credential was sealed to the local SQLite-backed app; the UI exposed only an opaque vault fingerprint. One direct Google request succeeded. Tanpopo’s provider probe did not connect; app reports generic upstream unreachable. No bypass of SSRF, TLS, or trust enforcement was attempted.
- Setup stored a generated master key with mode 0600 in the isolated `/tmp/qa-tanpopo` environment. No backup/recovery drill has run. Deletion of temporary org/project/account data, secret shredding, backup/restore, and audit-chain tamper checks remain open.


## Checkpoint 4

- E2E isolated local inference request without a valid virtual key returned 401 `invalid_virtual_key`; an unauthenticated admin GET and state-changing POST both returned 401 `unauthenticated`. These checks show the requests were rejected before a success response; deeper auth/session/CSRF testing is outstanding. Untrusted-origin preflight returned 405 and no `Access-Control-Allow-Origin`.
- A complete local fake-provider request traversed the route, returned HTTP 200, and produced a trace visible in the UI. This does not establish live provider resilience. Direct Google access works from the environment but the app's guarded egress fails upstream reachability because direct app TCP cannot use the cloud proxy; no guard bypass was attempted.
- Existing multi-org create/switch passed, but cross-tenant resource enumeration and mutation checks are still untested. Secret is sealed and masked from UI evidence; rotation/revocation, audit integrity, budget accounting, cancellation, concurrency, kill switch, backup restore and restart resilience remain open.
- Firefox/WebKit runtime dependencies absent; exact missing libraries are recorded in the master plan. Performance metrics emitted by current E2E harness are invalid due to its observer init callback and cannot substantiate latency/reliability claims.


## Checkpoint 5 — readiness failure boundary

The resolver failed closed when the only target had an unverified connection, returning 503 `no_compliant_deployment_available` before upstream egress. However, the console/admin activation path allowed the deployment to be labeled `ACTIVE_HEALTHY`, and the route UI reported `Healthy`; see DEF-002. This creates a material operational reliability risk without evidence of cross-boundary data leakage. Test key revoked and temporary route/deployment removed.


## Checkpoint 8 — financial control setup

A $100 monthly organization ceiling with 50/80/100 thresholds was created through the authenticated Admin API in isolated test data, then raised to $120 through the Console. Negative input produced inline validation and no mutation. This establishes configuration persistence only; no budget hold, concurrent exhaustion or 429 enforcement was tested. DEF-003 notes that the empty Console has no create control.

## Checkpoint 13 — scoped read authorization

A test-only principal with Developer and Team Admin roles (no Operator role) could list two projects in its bound organization (HTTP 200). Supplying the second organization's ID in `X-Tanpopo-Org-ID` returned HTTP 404 `resource_not_found` with a generic message and no data. This provides positive and negative evidence for one read-only tenant boundary. No cross-org writes, resource-ID substitution across endpoints, cursor replay, or other scoped data families were tested. Evidence: `evidence/phase13-nonoperator-cross-org-authorization.json`.

## Checkpoints 14–15 — Team Admin privilege boundary

The non-operator Team Admin did not see Operations or Security links; a direct Operations route showed a role-specific denial. `GET /admin/v1/organizations` returned 403. A visible Add member form produced 403 `role_escalation_denied` because it omitted required project IDs; no member was created (DEF-006). Evidence: `evidence/phase14-team-admin-authorization.json` and `evidence/phase15-team-admin-add-member-journey.json`. No cross-org writes, project ID substitution, or role self-escalation were attempted.

## Checkpoint 16 — tenant audit and backup verification failure

In the canonical runtime, creating `qa-boundary-org`, `qa-isolation-20261123`, and `qa-sandbox` produced no corresponding org/project/environment audit events; the new org had zero chain entries. This is DEF-007. CLI backup creation generated an encrypted archive, but verification rejected it because the initialized org had no verifiable audit history. The Console backup handler separately returned 503 because its required config path is unset by canonical serve wiring (DEF-008). At this checkpoint, no archive restore into the active DB or KEK recovery had been attempted. Evidence: `evidence/phase16-audit-and-backup-verification.json`.

## Checkpoint 17 — integrity check and isolated recovery attempt

The Security UI verified the currently stored RFC 8785 JCS chain across 172 events with HTTP 200 and displayed a green intact-chain state. This does not negate DEF-007: the new organization's chain is empty and its project/environment create events are still absent. A valid Master KEK was supplied to a CLI restore attempt targeting a fresh isolated directory; with `--verify-secrets-decrypt`, restore exited 14 for the same no-verifiable-audit-history organization. The destination was not created and the active QA database was not overwritten. This is an unsuccessful recovery workflow, not evidence of archive corruption independent of the audit defect. Evidence: `evidence/phase17-restore-attempt.json`, `phase17-audit-chain-verify.png`.

The Operations health screen showed Ready / Serving traffic, SQLite WAL and one `OK` node; upgrade compatibility preflight returned `READY_TO_UPGRADE` without applying an upgrade. The webhook form left Register disabled because no sealed HMAC credential is present. No webhook, kill switch, key rotation, destructive restore, or audit tampering was performed. Detailed UI evidence is indexed in `evidence/README.md`.

## Checkpoint 19 — non-operator cross-organization collection reads

Authenticated as the isolated `Developer + Team Admin` principal in Chromium and issued read-only collection GETs under its bound organization and `qa-boundary-org`, varying `X-Tanpopo-Org-ID`. Projects, members, models, connections, routes, budgets, traces and secret metadata each returned HTTP 200 with collection counts for the bound org (2, 4, 1, 1, 0, 1, 1, 1 respectively), and generic HTTP 404 `resource_not_found` for the other organization. No row payloads were stored in evidence. This materially broadens the positive/negative evidence beyond the single project-list check at checkpoint 13; it does not cover cross-org item-ID substitution, pagination replay, writes, alternate roles, or all collection endpoints. Evidence: `evidence/phase19-cross-tenant-read-sweep.json`.

## Checkpoint 20 — project-ID nested collection scope

Using the same non-operator principal, read `/projects/{projectId}/environments` for each of the two projects returned in its own organization. Both nested-list requests returned HTTP 200 with one environment under the bound org ID; the same project IDs under the alternate org ID each returned generic 404 `resource_not_found`. Project IDs and row data were not retained. This tests two foreign-project-ID substitutions against a nested read route; it does not test project delete/update, other nested resource families, or cursor replay. Evidence: `evidence/phase20-cross-tenant-nested-read.json`.

## Checkpoint 21 — encrypted restore and recovered service smoke

With a valid local Master KEK, an encrypted QA archive restored successfully into a new directory after the second QA organization had acquired `member.created` and `auth.login_succeeded` audit entries. Secret envelope decryption was enabled. The restored SQLite DB passed integrity check; a separately staged restored runtime returned healthy/ready and accepted a Chromium login for the tenant owner. The active QA DB was not overwritten. This closes one happy-path restore and startup check only; the empty-chain restore failure remains reproducible, as does the underlying missing provisioning-event defect (DEF-007). Console backup remains blocked by DEF-008. Evidence: `evidence/phase21-backup-successful-recovery.json` and `phase21-restored-runtime-smoke.json`.

## Checkpoint 22 — bounded kill-switch trigger and recovery

The isolated provider-connection kill-switch path completed through real Chromium: preview estimated zero affected orgs/projects/keys/connections; POST trigger and DELETE clear each returned HTTP 200. UI reported epoch 24, one entity affected and zero streams severed, then returned to zero active switches after clear. This verifies one reversible low-impact path, not stream termination or propagation under load/nonzero scope. An initial 403 came from using the wrong HTTPS test origin; the configured-origin retry passed, so it is not classified as a product defect. Evidence: `evidence/phase22-killswitch-journey.json` and visually inspected preview/confirmation/active/cleared screenshots. No product code changed; GATE-REL-07 remains blocked.

## Checkpoint 23 — audit integrity after kill-switch lifecycle

Following the CP22 trigger and clear, authenticated Chromium displayed both immutable action events and verified the RFC 8785 JCS chain across 189 events (POST verification HTTP 200). This is a successful post-action integrity check; it does not establish tamper resistance through a deliberate tamper test, nor close DEF-007's missing provisioning audit records. Evidence: `evidence/phase23-post-clear-audit-verify.json` and visually inspected screenshot. GATE-REL-07 remains blocked.

## Checkpoint 24 — cross-engine mobile trigger guard

Real authenticated Firefox 153 and WebKit 26.5 sessions at 375×844 opened, but did not submit, the emergency kill-switch form. Both displayed required target/reason/project-confirmation inputs and disabled Review & trigger while blank; active-switch count stayed zero and no observed Admin API call failed. The drawer fit within the viewport in both engines. This checks the UI guard only, not authorization bypass, request tampering, confirmation, or propagation. Evidence: `phase24-killswitch-cross-engine-mobile-sanitized.json` plus visually inspected screenshots.

## Checkpoint 25 — guarded provider egress and runtime acquisition

The published environment reports unrestricted network revision 9 and ready QA credential bindings. An active-runtime authenticated Probe returned 503 `probe_runtime_unavailable` during extension-runtime acquisition; adapter metadata was `ENABLED_ACTIVE`, and the connection remained `CREATED_UNVERIFIED`. In a separately restored runtime, the TLS proxy origin was rejected before mutation; the direct allowed local origin then produced HTTP 200 from the Probe endpoint but an `upstream_unreachable` validation result, preserving the unverified state. Direct provider API connectivity had succeeded earlier, but Tanpopo-mediated Google inference still has no success evidence. This is a current environment/runtime limitation pending root-cause isolation; no new product defect assigned. Evidence: `phase25-provider-egress-retest.json` and inspected UI captures.

## Checkpoint 26 — guarded egress versus Cloud proxy

Normal shell access through the Cloud proxy reached Google Models (HTTP 200 with the configured credential); no-proxy direct access timed out (HTTP 000, exit 28). Tanpopo's guarded egress dialer explicitly avoids ambient proxy configuration to keep SSRF controls authoritative. Runtime probes on 8080 and 8082 therefore reached a `valid=false/upstream_unreachable` result while 8081 failed earlier at runtime acquisition with 503. This is evidence of an environment/proxy compatibility boundary and one isolated runtime instance issue, not evidence that credentials are invalid or Google is unavailable. Safe explicit proxying that preserves SSRF controls remains an engineering/environment gap. No secret or provider payload is retained; no successful Tanpopo inference is claimed.

## Checkpoint 27 — provider path and regression retest

Three ready listeners were compared: 8080 and restored 8082 reached the Google probe attempt but returned `upstream_unreachable`; 8081 returned `probe_runtime_unavailable` before the upstream call. A Cloud-proxy request to Google succeeded while direct no-proxy connectivity timed out. This supports classifying `upstream_unreachable` as guarded-direct-egress incompatibility with proxy-only Cloud networking, not as invalid credentials or a confirmed Google outage. `probe_runtime_unavailable` is isolated to the 8081 runtime instance/configuration; the probe factory's underlying error is not surfaced, so root cause remains unresolved. Provider state stayed unverified and no live inference succeeded. Evidence: `evidence/phase26-provider-runtime-comparison.json` and the three screenshots.

CP27 repeated the MCP/A2A private endpoint request rejection (DEF-005), Team Admin member denial (DEF-006), and Console backup 503 (DEF-008). No records were created by rejected requests. Broader authorization mutations, protocol abuse, streaming, cancellation, failover and audit tampering remain open. GATE-REL-07 remains BLOCKED.

## Checkpoints 29–30 — write authorization and provisioning audit

A non-operator Team Admin's empty-body project-create request was denied 403 in its bound org and concealed as generic 404 under another org ID; no write was possible with the intentionally invalid body. A read-only SQLite recheck found no organization/project/environment creation events for the two orgs and retained QA project/environment, reconfirming DEF-007. Evidence: `phase29-teamadmin-write-boundary.json` and `phase30-audit-provisioning-recheck.json`. These checks do not establish full mutation isolation or tamper detection; GATE-REL-07 remains BLOCKED.

## Checkpoints 32–34 — A2A plane and key-scope boundaries

On the original runtime, A2A returned 404 because `TANPOPO_A2A_ENABLED` was not set. This is a deployment setting. An isolated listener with that flag enabled accepted the admin UI key creation but a cookie-free bearer request to `GET /a2a/agents` returned 403 `scope_or_plane_denied`; anonymous access returned 401. Read-only DB inspection showed all CP32–34 keys were provider-only, and the probe keys were revoked. This is DEF-010: the supported issuance surface lacks A2A plane/route grants, preventing normal A2A authorization. The default-deny behavior itself is appropriate; the missing grant path is the defect. Handoff: `engineering-handoffs/DEF-010-a2a-virtual-key-scope.md`. No task dispatch or agent endpoint call occurred. GATE-REL-07 remains BLOCKED.

## Checkpoints 36–37 — Virtual Key rotation/revocation

During the documented 24-hour grace window, both old and replacement tokens authenticated successfully. After revocation both failed closed with 401 `invalid_virtual_key`. The UI's graceful revoke path returned 200, refreshed the key row to Revoked and reported zero severed streams; its bearer also failed closed. Keys were synthetic, environment-bound and revoked after use. No secrets were retained in evidence. Emergency stream abort, live stream drain and race behavior remain untested; GATE-REL-07 remains BLOCKED.
