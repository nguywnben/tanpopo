# Alpha Functional Test Report

## Checkpoint 2026-11-23

No product runtime journeys completed. The environment lacks a functioning Go toolchain, so Tanpopo could not be built or started. Unit tests below are component evidence only.

### Executed

- `npm ci --ignore-scripts` in `web/console` with `npm_config_cache=/tmp/tanpopo-npm-cache`: PASS (365 packages, lockfile unchanged).
- `npm run typecheck`: PASS.
- `npm test`: FAIL, 38 passed / 2 failed across 40 tests. Both failures are accessibility audits in Setup workflow, `label-content-name-mismatch` on a step button.

### Not executed

Setup, API, CLI, persistence, provider integration, authorization, concurrency, recovery, upgrade, and deletion workflows require a working Go runtime and isolated service fixtures.

## Checkpoint 2 — environment recovery

- Installed official Go 1.27.4 under `/tmp/qa-tools/go-dist/go`; SHA-256 matched the official `go.dev/dl/?mode=json` metadata. The broken `/usr/bin/go` was left untouched.
- `go mod download` via the default proxy failed with 403 on signed artifact downloads from `storage.googleapis.com`. `goproxy.io` returned a Cloudflare 403 for the module archive. No checksum/integrity checks were disabled.
- `docker info` reports Engine 29.9.2. No images are preloaded. Tanpopo remains unbuilt because dependencies are blocked.
- A restricted egress draft requests `storage.googleapis.com` and `generativelanguage.googleapis.com`; it is saved but requires environment save/publish before the running machine can use those hosts.

## Checkpoint 3 — first real user journeys

- Fresh setup → bootstrap owner → authenticated dashboard: PASS in Chromium; persisted `initialized=true`; server health/ready endpoints both 200.
- Setup invalid blank submit: inline validation shown. Setup can advance with valid bootstrap token/email/password, skip first connection when no adapter is enabled, confirm master key custody, and complete.
- Extension: stage official Gemini adapter → inspect permission additions → explicit consent → activate: PASS (stage 202; approval 200; Active state persisted). Publisher is shown as Unsigned even though origin is Official.
- Vault: Google key sealed through masked UI, HTTP 201; success message says plaintext cannot be read again. Connection created and fingerprint displayed.
- Google AI Studio direct interoperability: minimal `gemini-3.5-flash-lite` GenerateContent returned HTTP 200, `STOP`, exactly `OK`; one generated token. No provider payload/key was retained in evidence.
- Tanpopo Gemini live probe: failed twice with generic `upstream_unreachable`, persisted connection remained `Created Unverified`. Direct authenticated Google requests from this same managed machine succeeded. Likely environment limitation: Tanpopo’s guarded transport deliberately does not use ambient HTTP proxy while cloud egress is proxy-mediated. End-to-end inference via Tanpopo is blocked and unverified; do not bypass SSRF guard to force it.
- RBAC direct route: Security Reviewer could access Security approvals, did not see Operations in navigation, and was denied direct `/ops/health` route with explicit Access restricted state.
- Go suite: initial failure `TestVaultCapabilityAndMasterKeyFailClosedNEG2` reproduced 3/3 under process umask `0077`; probe confirmed OS created `0644` file as `0600`. With umask `0022`, target test passed 3/3 and entire `go test ./...` passed. This validates an environment-dependent test assumption, not a product defect.


## Checkpoint 4 — successful isolated inference path

- Existing Playwright E2E (`web/console/e2e/console_e2e.py`, Chromium) passed in a fresh temporary app/database using a deterministic local fake upstream. The complete path activated `@tanpopo/openai-compatible`, probed the connection, registered a logical model, created a deployment and route, issued a one-time virtual key, invoked `/v1/chat/completions` successfully (200), then confirmed its trace in Observability. The test key lived only in the harness process and was not retained in report output.
- Multi-organization create/switch, keyboard command palette, and Vietnamese/Japanese strings passed. 61 route/theme/viewport axe/layout samples passed without recorded violations, overflow, console errors, or external requests. Details are in `evidence/phase4-chromium-e2e.json`.
- This local fake-provider path validates Tanpopo's gateway behavior but does not verify a commercial provider. The Google connection on the retained instance remains `Created Unverified`; its app probe has failed twice with generic upstream unreachable although direct Google requests from the host succeed. The likely guarded transport/proxy mismatch is unresolved.
- Safe negative probes: unauthenticated admin GET/POST and data-plane request without a virtual key all returned 401 with structured authentication errors; untrusted-origin preflight returned 405 and no allow-origin header.
- Browser reliability: performance observer values LCP/CLS/INP are invalid because the existing harness registers an uninvoked function; do not interpret zeros as a pass. Firefox/WebKit launch is blocked by missing host libraries listed in the master plan.


## Checkpoint 5 — provider readiness negative journey

- After a known failed Google connection probe (`Created Unverified`), the console allowed an owner to create and activate a deployment as `ACTIVE_HEALTHY`; the route screen then showed its target as `Healthy`. One virtual-key request to the route returned 503 `no_compliant_deployment_available`. The data plane failed closed before provider egress.
- This is recorded as `DEF-002`: control-plane readiness UI/API accepts and displays a healthy state that the data plane does not consider eligible. Test-only deployment and route were deleted, and the one-time key was revoked after capturing evidence. No provider inference or ledger settlement is claimed.

- Route Explain returned a verified dry-run with zero selected/rejected candidates for a logical model without a deployment; no upstream request occurred. This is recorded as partial routing coverage, not a defect.


## Checkpoint 8 — budget setup and edit

- Empty FinOps Console had no first-budget creation action. `DEF-003` records the missing UI workflow; Admin API creation was used as a workaround in the isolated environment. The Console then displayed the organization-level budget in `No project` workspace and saved an updated hard limit (PUT 200).
- Negative limit `-1` was blocked inline with no API request. Synthetic budget remains at $120/month. No inference or spend was generated. Budget holds, enforcement, concurrent reservations and reconciliation remain untested.

## Checkpoints 9–10 — CLI and private integration endpoint validation

- Verified outbound HTTPS to `proxy.golang.org` (HTTP 200), Go 1.27.4, and `go list -deps ./...` against the restored module cache. The retained isolated app and a second QA listener both returned healthy/ready; configured provider credential bindings are reported ready by the Cloud environment. Prior full Go suite results are retained and were not rerun in this continuation.
- Read-only CLI reproduction: `tanpopo config show --output json` exits 0 but returns local config help, not server JSON. Duplicate root `config` groups make remote inspection unreachable (DEF-004).
- Real Chromium Org Owner journeys submitted private MCP and A2A endpoint forms. MCP returned HTTP 400 `allowed_cidrs is required when allow_private_ip is true`; A2A returned HTTP 400 requiring the same field (DEF-005). No record persisted and no endpoint was fetched. Evidence and screenshots are in `evidence/phase9-cli-config-command-collision.json` and `evidence/phase10-private-integration-endpoint-journeys.json`.
- MCP tool discovery/schema pinning/grants, A2A AgentCard and task delegation, and private-endpoint API workaround remain untested. GATE-REL-07 remains BLOCKED.

## Checkpoint 11 — project/environment scoping

- An Org Owner created project `qa-isolation-20261123` and environment `qa-sandbox` through the real Console; create requests returned HTTP 201 and the scope selector moved into each new scope.
- The synthetic project and environment remain in the isolated QA SQLite DB for follow-on scope-boundary testing. No provider traffic or spend was generated. Cross-organization data isolation remains untested. Evidence: `evidence/phase11-workspace-provisioning.json` and its visually inspected screenshots.

## Checkpoint 12 — organization scope

- Operations created `QA Boundary Organization` (`qa-boundary-org`) through the real Console, HTTP 201. Switching to it displayed no projects or environments; the `qa-isolation-20261123` project remained scoped to Default Organization. Screenshots: `evidence/phase12-org-created.png`, `phase12-second-org-scope.png`.
- This operator-owner journey confirms UI organization scope separation only. A direct cross-org request by a non-operator Security Reviewer remains open: `qa-reviewer@example.invalid` failed one login attempt with the saved QA recovery password. No further password guesses were made; test recovery/access setup is still under investigation.

## Checkpoint 13 — non-operator organization boundary

- Created an isolated QA principal, issued its local recovery password with the supported CLI recovery path, then removed its temporary `ORG_OWNER` role through Access & Keys and left `DEVELOPER + TEAM_ADMIN` only.
- Through authenticated Chromium fetches, `GET /admin/v1/projects` with the principal's bound organization returned HTTP 200 and 2 project rows. The same request with the other QA organization's `X-Tanpopo-Org-ID` returned generic HTTP 404 `resource_not_found`; no target data was disclosed.
- Evidence: `evidence/phase13-nonoperator-cross-org-authorization.json`, `phase13-boundary-probe-team-admin.png`, `phase13-team-admin-session.png`. This tests one read route only; create/update/delete, pagination cursors, other resource IDs, and other role combinations remain open.

## Checkpoints 14–15 — Team Admin authorization and member workflow

- Team Admin navigation omitted Operations/Security; direct `/ops/health` rendered a role-specific Access restricted banner. `GET /admin/v1/organizations` returned 403 `insufficient_role_permission`. Evidence: `evidence/phase14-team-admin-authorization.json` and screenshots.
- Access & Keys nevertheless exposed Add member. Submitting the email/default Developer role returned 403 `role_escalation_denied`: project-scoped admin must specify `project_ids`, but no project-scope control is present in the form. Member count stayed at 4. Added DEF-006 (Moderate); evidence `evidence/phase15-team-admin-add-member-journey.json` and linked screenshots.
- Other role combinations and Team Admin edits against project-scoped principals are still untested. GATE-REL-07 remains BLOCKED.

## Checkpoint 16 — backup and audit reliability

- Operations → Backups showed an enabled Create encrypted backup control but POST `/admin/v1/ops/backups` returned HTTP 503. Adding a valid test `tanpopo.yaml` did not change the result; canonical `serve` omits `ConfigPath` from `ServerConfig` (DEF-008). The UI shows the same error after retry.
- CLI `tanpopo backup create` succeeded with explicit QA config/lock/artifact paths and produced an encrypted 83,501-byte archive (mode 0600). `tanpopo backup verify` exited 14 because `qa-boundary-org` has no verifiable audit history.
- Read-only audit inspection found 0 events for the new organization, and no matching `project.created` or `environment.created` events for the QA project/environment, while the default organization had 167 events (DEF-007). The storage tenant repository has no runtime transactional audit sink wired; the server's direct audit logger does not cover those repository calls. Evidence: `evidence/phase16-audit-and-backup-verification.json` and Console screenshots. Archive was not restored.

## Checkpoint 17 — Operations, audit verification, and restore (2026-11-23)

- In authenticated Chromium, Operations health reported Ready / Serving traffic, SQLite WAL, and one healthy standalone node at config epoch 23. A Core `0.2.0` upgrade compatibility preflight returned HTTP 200 with `READY_TO_UPGRADE`; no upgrade was applied. Evidence screenshots and response summary: `evidence/phase17-ops-health.png`, `phase17-ops-upgrades.png`, and `phase17-upgrade-preflight-result.png`.
- The webhook list rendered an empty state. Opening Add webhook showed the write-only `WEBHOOK_HMAC_SECRET` requirement; with no eligible signing secret, Register webhook stayed disabled. No webhook was submitted. Evidence: `phase17-ops-webhooks.png`, `phase17-webhook-register-form.png`, `phase17-operations-interactions.json`.
- Security → Audit Log → Verify hash chain returned HTTP 200 and displayed “Hash chain verified” across 172 events. This confirms the currently stored chain only; it does not create missing tenancy events for the new org/project/environment (DEF-007). Screenshot: `phase17-audit-chain-verify.png`.
- Retried archive restore in a new isolated destination with the correct QA Master KEK file and secret-decryption check. Restore exited 14 on the same `qa-boundary-org` no-verifiable-audit-history error; no destination was published. The initial no-key attempt failed earlier at Master KEK configuration and was not counted as archive verification. JSON: `phase17-restore-attempt.json`.
- No product files were changed. No new defect is assigned from these checks; DEF-001 through DEF-008 remain open, and release readiness stays incomplete.

## Checkpoint 18 — Operations cross-engine mobile spot checks

- Authenticated Chromium, Firefox, and WebKit at 375×844 loaded live Operations health, Webhooks, and Security Audit screens. No Admin API errors or document-width overflow were recorded. The webhook form remained disabled without an HMAC credential in all three.
- Chromium/Firefox displayed the opened Webhooks drawer. WebKit kept the drawer content in its DOM but placed the heading at x=392px outside the viewport, confirmed against the mobile screenshot; recorded as DEF-009. Full screenshots and cross-engine response summary are indexed in `evidence/phase18-ops-cross-engine-mobile.json` and `evidence/phase18-webkit-drawer-layout.json`.
- Broad user journeys in Firefox/WebKit, browser/device support certification, and other UI states remain incomplete. No product code changed; GATE-REL-07 remains blocked.

## Checkpoint 19 — non-operator tenant collection reads

- From a real authenticated Chromium session as the QA `Developer + Team Admin`, issued read-only GETs for projects, members, models, connections, routes, budgets, traces, and secret metadata under both the bound org and a second QA org ID. All eight bound-org requests returned 200; all eight other-org requests returned generic 404 `resource_not_found`. Counts only were retained; resource records and secret metadata were not copied into the report.
- This extends the earlier single project-list boundary check. Cross-org item-ID substitution, writes, pagination replay and other roles remain open. Sanitized evidence: `evidence/phase19-cross-tenant-read-sweep.json`. GATE-REL-07 remains blocked.

## Checkpoint 20 — nested project-scope read checks

- For both project IDs returned within the Team Admin's bound organization, GET `/admin/v1/projects/{projectId}/environments` returned 200 with one environment under the bound org header and generic 404 `resource_not_found` under the other QA org header. IDs/row content were not retained. This supplies two nested-resource scope checks without mutating state; writes, deletion, cursors and other ID-bearing endpoints remain untested.
- Evidence: `evidence/phase20-cross-tenant-nested-read.json`. No product code changed; GATE-REL-07 remains blocked.

## Checkpoint 21 — encrypted backup and full isolated restore

- Added a QA-only Org Owner to `qa-boundary-org` and authenticated it once, creating two legitimate audit events (`member.created`, `auth.login_succeeded`) while leaving the missing tenancy-provisioning events unresolved (DEF-007).
- In that state, CLI encrypted backup creation and verification both exited 0. Restore with the Master KEK and `--verify-secrets-decrypt` exited 0 into a separate destination with seven files. The restored SQLite database passed `PRAGMA integrity_check`; it retained 2 organizations, 5 principals and 195 audit rows at restore time.
- Staged the recovered files into a new isolated runtime on 127.0.0.1:8082. Health returned `ok`, readiness `ready`; real Chromium logged in as the restored tenant owner and loaded Overview, with all observed Admin API responses 200. Screenshot: `evidence/phase21-restored-runtime-owner.png`. Sanitized procedure and results: `evidence/phase21-backup-successful-recovery.json`, `phase21-restored-runtime-smoke.json`, `phase21-test-tenant-owner-creation.json`.
- This successful path depends on each organization having verifiable audit history; earlier empty-chain verify/restore failures remain valid. The active DB was not overwritten, Console backup still returns 503 (DEF-008), and missing create audit events remain DEF-007. GATE-REL-07 remains blocked.

## Checkpoint 22 — emergency kill-switch trigger and clear

A real authenticated Chromium session previewed and triggered a provider-connection kill switch against a QA-only unverified connection. Preview reported zero orgs, projects, virtual keys, or connections. Trigger returned HTTP 200 (epoch 24, one entity affected, zero streams severed); the active row was then cleared with HTTP 200, and the final active count returned to zero. The connection had no blast radius and no productive traffic was involved. A first attempt through a non-configured HTTPS test origin received CSRF/origin 403 without changing state; the configured origin retry succeeded. This is harness setup evidence, not a product defect. Journey: `evidence/phase22-killswitch-journey.json`; initial failure record: `phase22-killswitch-partial.json`; four visually reviewed screenshots are `phase22-killswitch-{preview,confirm,active,cleared}.png`. Other target levels, nonzero impact, active stream severance and propagation remain untested. GATE-REL-07 remains blocked.

## Checkpoint 23 — kill-switch audit records

After authenticating as the recovered QA Org Owner, the Security Audit Log showed the `kill_switch.triggered` and `kill_switch.cleared` events from CP22. Verify hash chain returned HTTP 200 and the UI reported the RFC 8785 JCS chain intact across 189 events. This validates audit visibility and chain verification for that action pair only; it does not resolve the distinct missing org/project/environment provisioning events (DEF-007). Evidence: `evidence/phase23-post-clear-audit-verify.json` and visually reviewed screenshot `phase23-post-clear-audit-verify.png`.

## Checkpoint 24 — mobile cross-engine kill-switch form

Authenticated Firefox 153 and WebKit 26.5 (Playwright 1.62.0 image) opened the trigger form at 375×844 without submitting. Both showed the heading and required Project confirmation slug within the viewport; document width equaled 375px, active count remained zero, blank required fields kept the action disabled, and observed Admin API calls had no errors. Screenshot review showed no WebKit offscreen placement on this surface. Evidence: `evidence/phase24-killswitch-cross-engine-mobile-sanitized.json` and the visually reviewed Firefox/WebKit captures. Submission semantics and other target levels remain untested.

## Checkpoint 25 — provider connection retest after network publication

Cloud status reports revision 9 enforced/unrestricted and both QA credential bindings ready. The authenticated active-runtime Probe returned HTTP 503 `probe_runtime_unavailable` (`connection validation runtime unavailable`) before reaching the provider, despite the adapter installation being listed `ENABLED_ACTIVE`; the connection stayed `CREATED_UNVERIFIED`. On the separately restored instance, its TLS proxy origin was rejected by CSRF/origin validation, then the same UI journey through the direct allowed test origin returned HTTP 200 with `upstream_unreachable`; state remained unverified. The extension path ran in that restored process but still did not establish Google connectivity. Earlier direct Google AI Studio access succeeded, so no credential or external provider unavailability is inferred. No Tanpopo-mediated Google inference succeeded. Evidence: `phase25-provider-egress-retest.json`, `phase25-runtime-capability-summary.json`, and visually reviewed screenshots `phase25-provider-probe-after-republish.png` and `phase25-restored-direct-provider-probe.png`.

## Checkpoint 26 — three-runtime Google probe comparison

Across the three browser-reachable runtimes, 8080 and restored 8082 returned HTTP 200 with `valid=false`, `failure_code=upstream_unreachable`, and left the connection `CREATED_UNVERIFIED`; 8081 via its configured origin returned 503 `probe_runtime_unavailable` before provider egress. Google Models via the inherited Cloud proxy and configured QA credential returned HTTP 200, while the same host without a proxy timed out (HTTP 000, curl exit 28). Tanpopo's guarded egress source explicitly avoids `http.ProxyFromEnvironment` so ambient proxies cannot bypass SSRF protections. This points to Cloud's proxy-only egress boundary meeting Tanpopo's guarded direct transport; credential/service failure is not evidenced, and no app-mediated inference succeeded. The 8081 runtime failure is isolated and its factory cause is not exposed; it is treated as instance configuration until proven otherwise. Evidence: `phase26-provider-runtime-comparison.json`, three visually reviewed screenshots, and the runtime/net result in the master checkpoint.

## Checkpoint 27 — defect regression interactions

The real-browser regression pass reproduced the empty-budget first-action gap (DEF-003), CLI `config show` collision (DEF-004), private CIDR omission on both MCP and A2A forms (DEF-005), Team Admin member-flow mismatch (DEF-006), and enabled Console backup failure (DEF-008). Private endpoint requests returned 400 before any endpoint fetch or record creation. A2A screenshots and request evidence are in `evidence/phase27-a2a-defect-retest.json`; other retests are indexed by `phase27-defect-regression-summary.json`.

DEF-009 was not reproduced in current WebKit 26.5 at 375×844: the visible drawer remained at x=73 through 1000ms with no horizontal overflow. Prior CP18 evidence and the open finding are retained pending explanation of the discrepancy. DEF-001/002/007 remain open and unchanged; this phase did not re-test them. This is not completion of Alpha coverage. GATE-REL-07 remains BLOCKED.

## Checkpoint 28 — A2A private registration backend smoke

A deterministic fixture was bound to loopback. Real Chromium filled and submitted the A2A UI form. The unmodified payload omitted `allowed_cidrs` (DEF-005); a test-only Playwright interceptor supplied exactly `127.0.0.1/32`, after which the isolated backend returned 201 and the UI displayed one Active agent. Screenshots were visually inspected. The fixture was not contacted and no A2A task was run. This confirms the backend can accept explicit private scope; it does not make the production form usable or close DEF-005. Evidence: `evidence/phase28-a2a-mock-ui-registration.json`. A2A task lifecycle remains untested; GATE-REL-07 remains BLOCKED.

## Checkpoints 32–34 — A2A Virtual Key authorization

The real Issue Virtual Key drawer contains model and MCP tool controls but no A2A plane or route grants. The standard-created QA key was tested against an isolated listener with `TANPOPO_A2A_ENABLED=true`, using a cookie-free client: anonymous A2A discovery returned 401 and the issued key returned 403 `scope_or_plane_denied`. Read-only DB inspection confirmed `allowed_planes=PROVIDER`; the key was revoked and its secret was not persisted. The prior listener without the A2A flag returned 404, classified as configuration. The enabled-listener 403 is a new confirmed provisioning defect, DEF-010, with a targeted engineering handoff. A2A task lifecycle remains blocked/uncovered. GATE-REL-07 remains BLOCKED.

## Checkpoints 36–37 — Virtual Key lifecycle

Real Chromium issued a disposable key, rotated it through the row action, and showed the documented 24-hour dual-valid grace. Cookie-free provider model-list requests with both old and replacement tokens returned 200 during grace. After revocation both returned 401 `invalid_virtual_key`. A second key was revoked through the actual UI; the success banner reported zero active streams severed, the row immediately changed to Revoked, and its bearer then returned 401. Plaintext key material is absent from evidence and temporary probe copies were removed. Evidence: `phase36-key-lifecycle.json` and `phase37-ui-revoke.json`. Emergency abort and active-stream drain remain open; GATE-REL-07 remains BLOCKED.
