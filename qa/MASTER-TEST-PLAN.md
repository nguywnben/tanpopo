# Tanpopo Independent QA Plan

## Checkpoint 2026-11-23

Independent validation at baseline `a771311107c4e4c2c7d9b53a3f5a0926f114b799` (branch `work`). No product source, tests, dependencies, or lockfiles are modified. This workspace contains QA artifacts only.

## Goals and boundaries

Validate observable behavior across product journeys, UX, security, business rules, and operations. Report only exercised behavior as tested. Use disposable state for destructive tests and never use production data. Real model-provider tests require the configured QA credentials; credential values must not be read into logs, prompts, reports, or screenshots.

## Coverage strategy

1. Phase 0: establish baseline, map repository requirements, screens, APIs, CLI, integrations, CI, and existing evidence; identify contradictions and inaccessible requirements.
2. Phase 1: install and execute end-to-end workflows against an isolated local instance; use existing automated tests as evidence, not substitutes for user journeys.
3. Phase 2: exercise UI in a real browser, capture and inspect screenshots across representative roles, states, locales, themes, and widths.
4. Phase 3: audit security, tenant boundaries, persistence, finance, lifecycle and failure behavior with isolated data.
5. Phase 4: rerun confirmed defects and release-relevant regression coverage, publish a traceable handoff.

## Environment and evidence rules

Record exact commands, exit statuses, environment blockers, and sanitized outputs. Keep screenshots and logs under `qa/evidence/`. Every defect gets stable ID, severity, user impact, expected/observed behavior, exact reproduction, evidence, root-cause hypothesis, and regression risk. Retests append outcomes; never overwrite earlier results.

## Known environment constraints at checkpoint

- Cloud runtime configuration now reports running/connected; Docker daemon is available. The earlier unavailable state is resolved for shell work.
- Node v24.22.3 and npm 11.19.1 are available. `npm ci --ignore-scripts` succeeded with `npm_config_cache=/tmp/tanpopo-npm-cache`.
- `/usr/bin/go` remains an unrelated/non-Go binary (`go version` returns `Go: Unknown option: version`). Official Go 1.27.4 was downloaded to `/tmp` and verified against the SHA-256 in go.dev release metadata. `go mod download` still fails because signed module artifact downloads redirect to `storage.googleapis.com`, which returns network 403.
- Docker Engine 29.9.2, Chromium, and Playwright are available. Vite starts on port 4173. The Go backend has not been built/launched, so the browser sees Admin API 500.
- Provider runtime bindings `QA_META_MODEL_API_KEY` and `QA_GOOGLE_AI_STUDIO_API_KEY` are ready per environment metadata; values remain uninspected. Google API egress is not currently enabled in the running configuration. The Meta provider's endpoint/host is not specified by repository docs or request text found so far.
- On 2026-11-23, saved a restricted-network draft adding `storage.googleapis.com` and `generativelanguage.googleapis.com`. Draft save is confirmed; user must save/publish it for new egress to apply. Existing repository membership and runtime credential bindings were preserved.

## Current status after checkpoint 21

- Cloud environment revision 9 is current, network is unrestricted/enforced, provider bindings are reported ready, outbound HTTPS to `proxy.golang.org` returned 200, and Go 1.27.4 resolves `go list -deps ./...`. The prior full Go suite passed with `umask 0022`.
- The isolated Tanpopo runtime remains healthy on 8080/8081 with its test SQLite database; a separately restored copy now runs on 8082 and passed owner login. Real Chromium/Firefox/WebKit automation is available in the official Playwright container. No product files were changed; reports/evidence are under `qa/`.
- No successful Tanpopo-to-Google inference exists. App-mediated Google calls remain blocked by the provider transport's direct-egress/proxy incompatibility despite direct Google API success and ready credential bindings.
- The original nine findings remain preserved; DEF-010 was added after A2A Virtual Key authorization was reproduced on an enabled isolated runtime. Audit verification passes for existing events, but tenant creation events are absent. Backup verify/restore fail for an empty-chain QA organization; after adding legitimate member/login audit events, CLI create/verify/restore and a restored-runtime login succeeded in a separate environment. Eight tenant resource collections and two nested environment reads return generic 404 under the other org when a non-operator Team Admin scopes the request there. GATE-REL-07 remains blocked.
- Continue from completed checkpoints rather than rerunning them: broaden role/tenant API isolation and policy/key lifecycle checks; test kill-switch, streaming/cancellation/failover, budget enforcement/settlement, MCP/A2A positive paths, backup recovery after audit requirements are satisfied, deletion/retention, and per-feature responsive/cross-engine interactions. Resolve or document each coverage cell with evidence before assessing the gate.

## Immediate next steps at the initial environment checkpoint

Continue actual Alpha journeys in the isolated SQLite instance: key issuance and plane separation, model/deployment/route persistence and invalid transitions, inference failure behavior, APIs/CLI, and recovery. Exercise remaining per-screen interactions at desktop/mobile widths, themes and available locales. Then execute business/security/operational cases and regression. Do not treat this checkpoint as exhaustive Alpha/Beta completion or release approval.

## Checkpoint 3 — runtime and real-browser Alpha (2026-11-23)

- Published environment is `running`, network `unrestricted/enforced`; direct HTTP requests through the environment network path reach Google AI Studio. Go modules are downloaded. Tanpopo built to `/tmp/qa-tools/tanpopo` and is running loopback-only on `127.0.0.1:8080` with an isolated SQLite DB under `/tmp/qa-tanpopo/data` and exact allowed origin `http://127.0.0.1:8080`.
- `/healthz`, `/readyz`, `/setup`, bootstrap status all returned 200. First-run setup, owner creation, login, and dashboard completed via Chromium. State persisted as initialized and owner session returned expected Operator + Org Owner roles.
- Captured and visually inspected real app screenshots for all 12 main navigation pages at 1440px and 375px; no external runtime requests or JS errors observed in that sweep. Some pages required several seconds to leave loading state, so initial snapshots of transient loading are not used to judge final state. Per-page interaction coverage is not yet complete.
- Setup first-run invalid blank submit returned an inline “Cannot continue” message; token/account flow advanced, skipped provider, required master-key custody confirmation, then completed with a persisted owner. Screenshots: `setup-first-run-1440.png`, `setup-validation-empty.png`, `setup-step2-1440.png`, `setup-step3-1440.png`, `setup-review-1440.png`, `setup-complete-1440.png`.
- RBAC: created temporary Security Reviewer principals in the isolated DB. Used the local recovery command and a temporary Org Owner binding to create a second local identity, then removed its Org Owner binding. As Security Reviewer, `/security/approvals` rendered, Operations nav was hidden, and direct `/ops/health` showed “Access restricted”. Evidence: `access-reviewer-least-privilege.png`, `security-reviewer-approvals.png`, `security-reviewer-ops-denied.png`. A member added by email alone could not locally authenticate until a local owner recovery password was issued; external OIDC is not configured.
- Extension lifecycle: staged the official Gemini adapter; UI showed `Origin trust: Official`, `Publisher signature: Unsigned`, Tier-2 WASM, and permission additions (Google host, `api_key` slot, provider hooks). Review required an explicit consent checkbox. Second reviewer consented and activated it; API `POST .../approve-permissions` returned 200; UI state became Active. Evidence includes stage, consent, and activated screens.
- Google provider: credential environment binding was ready, so one tiny direct GenerateContent request to model `gemini-3.5-flash-lite` returned HTTP 200/STOP/“OK” (1 output token, 10 total tokens). The key was entered only in a masked field and sealed through the app; plaintext was not logged or screenshotted. Created a Gemini connection with the exact API hostname and secret slot. Connection probe returned `valid:false`, `failure_code:upstream_unreachable` twice (~2.1s), and stayed `Created Unverified`. The same credential and host work over environment proxy using both `x-goog-api-key` and direct API requests. Tanpopo’s SSRF-guarded egress transport explicitly has no ambient HTTP proxy (`Proxy: nil`); cloud outbound is proxy-mediated. Therefore this probe result is an environment-path limitation pending a supported direct egress route; it is not yet assigned as a product defect. No successful inference through Tanpopo has been achieved.
- Full Go suite first had one failure because task umask `0077` made requested test files `0644` actually `0600`; isolated retest failed 3/3 under that umask. With `umask 0022`, targeted permission test passed 3/3 and `go test ./...` passed. The first result is an execution-environment artifact, not an implementation finding.
- Previous finding `DEF-001` remains intact. GATE-REL-07 remains blocked.


## Checkpoint 4 — full Chromium app path and screen audit (2026-11-23)

- The existing Chromium harness ran in a fresh temporary SQLite instance with a deterministic local fake provider. It passed official OpenAI-compatible extension activation → connection probe → logical model → deployment → route → virtual key issuance → `POST /v1/chat/completions` (HTTP 200) → trace visible in the console. It also created/switched organizations, used the keyboard command palette, and verified Vietnamese and Japanese localization strings. This supplies an actual Tanpopo inference journey without misrepresenting it as a live Google provider test.
- Completed 61 accessibility/layout samples across 27 unique routes: 42 desktop 1440px, 4 tablet 768px, 15 mobile 375px; 46 light and 15 dark. Axe recorded 0 violations in these samples; all had one non-empty H1 and no document-width overflow. No external requests, browser console errors, or page exceptions were recorded. Raw sanitized audit data: `evidence/phase4-chromium-e2e.json`. Visual screenshots from the prior real-browser journeys remain indexed in `evidence/README.md`; the existing harness did not save screenshots for this ephemeral run.
- The harness performance readings are invalid for LCP/CLS/INP: its `add_init_script` registers an arrow function without invoking it, leaving observer metrics at 0. TTFB/FCP values are retained as observations only and no performance pass is claimed. This is a QA harness limitation, not a Tanpopo defect.
- Firefox 153 and WebKit 26.5 downloads completed into `/tmp/qa-playwright-browsers`, but launch is blocked by missing host libraries: `libgtk-4.so.1`, `libgraphene-1.0.so.0`, `libharfbuzz-icu.so.0`, `libmanette-0.2.so.0`, `libhyphen.so.0`, `libGLESv2.so.2`. Chromium is the only verified browser engine.
- Unauthenticated API probes on the separate retained disposable instance returned 401 for `GET /admin/v1/models`, `POST /admin/v1/models` and chat inference with no virtual key; error envelopes were structured and did not indicate a state mutation. Untrusted-origin `OPTIONS /admin/v1/models` returned 405 with no CORS allow-origin header.
- Live Google direct API auth succeeds, but the app probe is still blocked by its guarded-egress transport not traversing the cloud proxy. No successful Tanpopo → Google inference is claimed. `DEF-001` is preserved. GATE-REL-07 stays BLOCKED.

## Current phase limits

Mission remains active. Multi-tenant isolation, role matrix beyond the reviewer denial, native protocol, streaming failure/cancellation, routing explain/failover, policy boundaries, budgets/ledger, MCP/A2A behavior, secret rotation, audit tamper evidence, kill switches, backup/restore, restart persistence, deletion/retention, all API/CLI surfaces and per-feature Firefox/WebKit interactions remain untested or partial.


## Checkpoint 5 — connection/deployment readiness mismatch (2026-11-23)

- On the retained disposable instance, two live Google connection probes remained `upstream_unreachable` and the connection UI remained `Created Unverified`. Creating a nonzero test-rate deployment returned 201/`PROVISIONED`. The owner’s Activate deployment action returned success and changed it to `ACTIVE_HEALTHY`; Models and Routes both displayed `Healthy`.
- One virtual-key request to the new route returned HTTP 503 `no_compliant_deployment_available` before provider egress. This confirms the resolver correctly excluded the unverified connection, while the management surfaces misleadingly marked it healthy. Added `DEF-002` in the defect register.
- Evidence screenshots and sanitized response are in `evidence/phase5-unverified-connection-health-mismatch.json`. The one-time key was revoked; the temporary route/deployment and sentinel rate card were removed. The logical model, sealed secret, and failing connection remain in isolated QA storage for follow-up.
- This separates a confirmed product state-reporting defect from the unresolved infrastructure path to direct Google inference. No Tanpopo → Google success is claimed.

- Route Explain UI dry-run for the retained logical model with no deployment returned HTTP 200, `dry_run=true`, zero selected and zero rejected candidates. Screenshot: `evidence/routes-explain-no-deployment.png`. This is a usability observation only; policy and failover simulations remain open.


## Checkpoint 6 — alternate browser engines (2026-11-23)

- To work around missing host GTK4/Graphene/HarfBuzz ICU/Manette/Hyphen/GLESv2 libraries, ran Firefox and WebKit in official `mcr.microsoft.com/playwright:v1.62.0-noble` image digest `sha256:baed2032d533817f3dbe6425de795788430ba345e819a1201337009ba17c9d07`. The image includes browser system dependencies; no product files changed.
- Both engines used the same HTTPS loopback TLS proxy (the app sets Secure session cookies). Each authenticated and completed axe/layout checks on all 27 product routes at 1440px: 0 axe violations, 0 horizontal overflow, one H1 each, 0 external requests, 0 console errors and 0 uncaught page errors. Five representative 1440px screenshots and five 375px screenshots per engine were captured; representative desktop/mobile screenshots were visually inspected. Mobile axe/layout was not run on Firefox/WebKit.
- Combined across Chromium, Firefox and WebKit: 115 desktop/tablet/mobile axe/layout samples already summarized by engine details in `evidence/phase4-chromium-e2e.json` and `evidence/phase5-cross-browser.json`. Chromium alone includes dark mode and tablet axe/layout; alternate engines were light desktop for audits.
- The host remains unable to launch Firefox/WebKit directly; the supported Docker alternative removes this as a QA blocker. This extends browser-rendering evidence only and does not imply every feature interaction was tested in each engine.


## Checkpoint 7 — corrected one-off performance observation (2026-11-23)

- Used a corrected page init script (registered observers directly rather than leaving an arrow function uninvoked) on `/overview` at 1440×900 over HTTPS. Single run: TTFB 14.9 ms, FCP 232 ms, LCP 512 ms, CLS 0.0019. Command palette open/dismiss emitted Event Timing entries; maximum observed duration 56 ms. Full data: `evidence/phase6-performance-observation.json`.
- This is one local observation only; the 56 ms maximum is not INP, these values are not percentile/stability measurements, and no performance threshold pass is claimed. The earlier harness performance zeros remain invalid and were not used.


## Checkpoint 8 — FinOps budget configuration (2026-11-23)

- In the empty project workspace, the FinOps Console displayed zero budgets and no create action. The authenticated Admin API `PUT /admin/v1/budgets/{canonical ID}` successfully created a $100 synthetic monthly organization ceiling with 50/80/100 percent warning thresholds. Switching workspace to `No project` showed the organization budget; switching/editing in the Console changed the ceiling to $120 (HTTP 200, success banner).
- Negative hard-limit entry `-1` was rejected inline as “must be a non-negative safe integer in micro-USD”; zero budget PUTs were emitted and the stored $120 limit remained.
- Added DEF-003 for missing first-budget creation in the Console. No spend/inference was generated by this workflow. The record remains synthetic in the isolated database. Evidence: `evidence/phase6-finops-budget-journey.json`.
- Budget creation/configuration and input validation are now partial coverage; budget hold reservation, hard ceiling enforcement, concurrent burst behavior, carryover, settlement accuracy and exports remain open.

## Checkpoints 9–10 — remaining CLI and integration journeys (2026-11-23)

- Network configuration revision 9 is enforced; outbound module-proxy connectivity returned HTTP 200. Go 1.27.4 resolves `go list -deps ./...`. The isolated app returned 200 from health/readiness checks. Cloud reports both configured provider credential bindings ready; app-mediated Google egress remains unresolved.
- CLI `config show` was run read-only and returns local help with exit code 0 because the local reconcile and remote inspect groups share the root name `config` (DEF-004).
- Real Chromium Org Owner interactions inspected MCP/A2A empty states and attempted private endpoint registration on both forms. Each API returned HTTP 400 because the Console omitted required `allowed_cidrs`; no record was created and no endpoint was fetched (DEF-005). Screenshots and interaction details: `evidence/phase10-private-integration-endpoint-journeys.json`.
- Independent coverage continues. MCP discovery/schema grants/calls, AgentCard/task delegation, public integration flows, other CLI command groups, tenant isolation, streaming/failover/policy, budgets and operational recovery remain open. GATE-REL-07 remains BLOCKED.

## Checkpoint 11 — project/environment provisioning (2026-11-23)

- An Org Owner used the real scope switcher and create dialogs to provision synthetic project `qa-isolation-20261123` and environment `qa-sandbox` in the retained isolated DB. Both create endpoints returned 201 and the UI switched into each new scope. Screenshots and sanitized API summary: `evidence/phase11-workspace-provisioning.json`.
- These QA records remain available for subsequent scope-isolation work. No provider calls, spend, or destructive actions occurred. Organization membership/data isolation remains open. GATE-REL-07 remains BLOCKED.

## Checkpoint 12 — organization switching and data scope (2026-11-23)

- Using the isolated operator owner, created second organization `qa-boundary-org` (HTTP 201) and switched into it. The top scope became `QA Boundary Organization / No project / No env`; no project from Default Organization was listed in this scope. Evidence: `evidence/phase12-organization-scope-journey.json` and reviewed screenshots.
- This is a UI scoping check only. The bootstrap owner has global Operator privileges and cannot establish denial for a non-operator. A single stored-password attempt for the Security Reviewer failed; do not retry guesses. A supported local recovery or another test principal is needed for non-operator cross-tenant authorization checks. GATE-REL-07 remains BLOCKED.

## Checkpoint 13 — non-operator cross-org read authorization (2026-11-23)

- Added a test-only local human principal through Access & Keys, temporarily assigned Org Owner, issued a random local password via `tanpopo admin reset-local-owner`, then removed Org Owner and granted Team Admin. It has no Operator role; its shown roles are Developer and Team Admin.
- In a real authenticated Chromium session, GET projects in the bound org returned 200 with two rows. The same route targeting the other org via `X-Tanpopo-Org-ID` returned generic 404 `resource_not_found`. Evidence: `evidence/phase13-nonoperator-cross-org-authorization.json`; visual role evidence in two linked screenshots.
- This checks one resource-list read boundary. Mutating cross-org routes, resource-ID substitution, pagination cursors, role scope combinations and concurrent org switches remain open. GATE-REL-07 remains BLOCKED.

## Checkpoints 14–15 — Team Admin authorization and member management (2026-11-23)

- Team Admin navigation hides Operations/Security, direct `/ops/health` renders the role-specific Access restricted state, and `GET /admin/v1/organizations` returns 403 `insufficient_role_permission`. Structured result: `evidence/phase14-team-admin-authorization.json`.
- The Access & Keys page exposed Add member to Team Admin, but its form only collects email/roles. Submit returned 403 requiring project IDs; no member was created. This is DEF-006, Moderate, with screenshot/API evidence in `evidence/phase15-team-admin-add-member-journey.json`.
- Team Admin project-scoped member editing, other roles, virtual-key permissions, and cross-org write/ID-swapping probes remain open. GATE-REL-07 remains BLOCKED.

## Checkpoint 16 — backup and audit chain (2026-11-23)

- The Operations Console backup request returned 503 before and after a valid isolated `tanpopo.yaml` existed; the unified `serve` constructor omits `ConfigPath`, so the handler always sees it empty (DEF-008).
- CLI encrypted backup creation succeeded (83,501 bytes, mode 0600), but verification exited 14 because `qa-boundary-org` had no verifiable audit history. Read-only inspection also found zero organization-create events for that tenant and no `project.created`/`environment.created` events for the synthetic QA project (DEF-007).
- The audit gap and resulting backup-verification failure are recorded in `evidence/phase16-audit-and-backup-verification.json`. A restore attempt had not yet been made at this checkpoint. GATE-REL-07 remains BLOCKED.

## Checkpoint 17 — Operations journeys and isolated restore attempt (2026-11-23)

- Signed into the isolated runtime using the recovered QA owner and visually inspected the Operations health, upgrade preflight, webhook, and Security audit screens in Chromium. Readiness displayed Ready/Serving traffic, one SQLite node `OK`, epoch 23. The webhook list was empty; Add webhook showed the required sealed HMAC credential prerequisite and kept Register disabled. No webhook was created.
- Submitted the read-only compatibility preflight for Core `0.2.0`; the UI returned `Compatible` / `READY_TO_UPGRADE` (HTTP 200). This is a preflight result only; no upgrade or lockfile mutation occurred.
- Ran Security → Audit Log → Verify hash chain. The UI returned `Hash chain verified` across 172 events (HTTP 200). This global current-chain result does not reverse the missing tenant/project/environment creation events recorded in DEF-007.
- Attempted CLI restore of the encrypted QA archive into a new isolated destination with the correct Master KEK file and `--verify-secrets-decrypt`. Restore exited 14 because `qa-boundary-org` has no verifiable audit history; no destination directory/files were published. A first attempt without the KEK file failed earlier with a missing-key configuration error and is not treated as an archive defect.
- Evidence: `evidence/phase17-operations-routes-final.json`, `phase17-operations-interactions.json`, and the visually inspected `phase17-ops-*.png`, `phase17-audit-chain-verify.png`, `phase17-webhook-register-form.png`, `phase17-upgrade-preflight-result.png`. Mission remains active and GATE-REL-07 remains BLOCKED.

## Checkpoint 18 — Operations mobile rendering across engines (2026-11-23)

- Ran live authenticated Chromium, Firefox 153 and WebKit 26.5 interactions at 375×844 for Operations health, webhook registration, and Security audit verification. All three had no recorded Admin API errors; each tested view's document width equaled the 375px viewport. Webhook Add opened and submit remained disabled without an HMAC signing credential.
- Chromium and Firefox visibly presented the mobile webhook drawer. WebKit included its heading and fields in the text tree but placed the heading at x=392px, outside the viewport; the screenshot remained on the underlying empty state and there was no horizontal scroll to reveal the drawer. Added DEF-009, Moderate. The defect is scoped to this tested WebKit/mobile state; other browser versions, devices, and drawer flows need further checks.
- Evidence: `evidence/phase18-ops-cross-engine-mobile.json`, `phase18-webkit-drawer-layout.json`, and visually inspected Chromium/Firefox/WebKit screenshots. No product code changed. GATE-REL-07 remains BLOCKED.

## Checkpoint 19 — broader read-only tenant boundary sweep (2026-11-23)

- In an authenticated Chromium session as the non-operator QA Team Admin, queried eight read-only resource collections under the principal's bound org and the second QA org. Projects, members, models, connections, routes, budgets, traces and secret metadata each returned HTTP 200 for the bound org and generic HTTP 404 `resource_not_found` for the other org. Only counts/status/error codes were retained; no resource rows were persisted to the report.
- This expands positive/negative tenant-read evidence beyond projects but does not cover cross-org write attempts, individual resource ID substitution, cursor replay, all resource families, or other roles. Evidence: `evidence/phase19-cross-tenant-read-sweep.json`. No product code changed. GATE-REL-07 remains BLOCKED.

## Checkpoint 20 — nested project environment reads (2026-11-23)

- As the same non-operator Team Admin, listed environments under both project IDs from the bound organization. Each returned 200 with one row in the bound org and generic 404 `resource_not_found` when the same project ID was combined with the other org ID. No identifiers or environment details were retained.
- This tests nested collection scoping for two project IDs. It does not establish safety for mutations, pagination, other nested endpoints, or resource families not exercised. Evidence: `evidence/phase20-cross-tenant-nested-read.json`; GATE-REL-07 remains BLOCKED.

## Checkpoint 21 — backup restore and service recovery (2026-11-23)

- Added a QA-only Org Owner to the second test organization and logged in once. This created a verifiable `member.created` and `auth.login_succeeded` chain for that org, but no retrospective org/project/environment provisioning audit events; DEF-007 remains open.
- CLI backup create and verify exited 0. Restore with secret-decryption verification exited 0 and published a seven-file snapshot into a separate directory. The restored SQLite database passed integrity check. Its files were staged into a distinct test runtime on port 8082; `/healthz` and `/readyz` returned `ok`/`ready`, and Chromium logged in as the restored tenant owner and loaded the Overview with successful Admin API responses. The original isolated DB on 8080/8081 was not replaced.
- The earlier empty-chain archive verification/restore failures remain valid and are retained at phases 16–17. Console backup still returns 503 (DEF-008). Evidence: `evidence/phase21-backup-successful-recovery.json`, `phase21-restored-runtime-smoke.json`, `phase21-test-tenant-owner-creation.json`, and visually inspected `phase21-restored-runtime-owner.png`. No product code changed; GATE-REL-07 remains BLOCKED.

## Checkpoint 22 — reversible emergency kill-switch journey

- From a real authenticated Chromium session using the configured HTTPS test origin, previewed a kill switch against a `CREATED_UNVERIFIED` QA provider connection. The UI estimated `0 orgs · 0 projects · 0 virtual keys · 0 connections`; the confirmation dialog identified the selected provider connection and warned that active streams would terminate.
- Trigger returned HTTP 200 and the UI reported epoch 24, one entity affected, zero streams severed. The active row appeared. Clear returned HTTP 200, wrote the cleared state, and the final UI showed zero active switches. This was a reversible isolated test action; no productive traffic was routed.
- The first attempt against a different test HTTPS origin received 403 `CSRF token or origin validation failed` before changing state. Retrying through the configured allowed origin completed the journey; this is a harness-origin mismatch and not a product defect. Initial response evidence is retained in `evidence/phase22-killswitch-partial.json`.
- Captured and visually inspected the preview, confirmation, active, and cleared states: `evidence/phase22-killswitch-preview.png`, `phase22-killswitch-confirm.png`, `phase22-killswitch-active.png`, `phase22-killswitch-cleared.png`. Full journey and HTTP outcomes: `evidence/phase22-killswitch-journey.json`.
- No new defect assigned. This validates one provider-connection trigger/clear path and its zero-impact preview only; other levels, nonzero blast radius, live-stream termination, concurrent propagation, and audit event detail remain untested. No product code changed. GATE-REL-07 remains BLOCKED.

## Checkpoint 23 — post-clear audit verification

- Recovered the QA Org Owner's local test credential using the supported local-only recovery command, authenticated through Chromium against the configured test origin, and opened Security → Audit Log after the CP22 kill-switch lifecycle.
- The UI displayed `kill_switch.triggered` followed by `kill_switch.cleared`; the verify action returned HTTP 200 and showed the RFC 8785 JCS chain intact across 189 events. This provides audit evidence for the tested kill-switch lifecycle only and does not address DEF-007's missing org/project/environment provisioning events.
- Evidence: `evidence/phase23-post-clear-audit-verify.json` and visually inspected `evidence/phase23-post-clear-audit-verify.png`. No new defect assigned; no product code changed. GATE-REL-07 remains BLOCKED.

## Checkpoint 24 — mobile cross-engine kill-switch drawer

- Authenticated Firefox 153 and WebKit 26.5 in the official Playwright 1.62.0 image at 375×844. Opened Security → Kill Switches → Trigger kill switch and inspected the form without submitting it.
- Both browsers rendered the drawer heading at x=73–323px inside the 375px viewport; document width remained 375px, active switch count remained zero, and no Admin API errors were observed. The required project confirmation slug was visible and Review & trigger stayed disabled while required fields were blank.
- Both screenshots were captured and visually inspected: `evidence/phase24-firefox-killswitch-375.png`, `phase24-webkit-killswitch-375.png`; sanitized measurements and responses: `evidence/phase24-killswitch-cross-engine-mobile-sanitized.json`.
- No new defect assigned. This does not establish parity for project/organization/environment scope submissions, mobile confirmation, or other mobile browsers. DEF-009 remains scoped to the WebKit Webhooks drawer. No product code changed; GATE-REL-07 remains BLOCKED.

## Checkpoint 25 — post-publication provider retest

- Rechecked the current Cloud status: network revision 9 is enforced and unrestricted; both configured QA provider credential bindings are ready. The earlier direct Google AI Studio test succeeded, but no credential value or payload is retained.
- On the active isolated runtime (configured browser origin), the authenticated Google connection Probe request returned HTTP 503 `probe_runtime_unavailable` with UI text `connection validation runtime unavailable`. Read-only metadata listed the Gemini adapter installation as `ENABLED_ACTIVE` / `sandboxed_wasm`; the connection remained `CREATED_UNVERIFIED`. This failure occurred during runtime acquisition, before the provider request, and the endpoint does not expose the factory cause.
- Compared against the separately restored runtime. Its TLS proxy origin returned 403 CSRF/origin rejection before mutation; the direct allowed test origin was used for the retry. The connection Probe then returned HTTP 200 with validation result `upstream_unreachable` / `upstream connection could not be reached`, and the connection remained `CREATED_UNVERIFIED`.
- The two runtime results isolate both a test-runtime setup issue and the still-failing Tanpopo-to-Google guarded egress path under the published network policy. No successful app-mediated inference is claimed. Evidence: `evidence/phase25-provider-egress-retest.json`, visually inspected `phase25-provider-probe-after-republish.png` and `phase25-restored-direct-provider-probe.png`, and sanitized adapter/runtime metadata in `phase25-runtime-capability-summary.json`.
- No new product defect assigned from these environment-specific responses; the root cause of the active runtime factory failure and guarded egress remains open. No product code changed. GATE-REL-07 remains BLOCKED.

## Checkpoint 26 — three-runtime provider comparison and egress classification

- Repeated the authenticated Provider → Probe user journey against all three isolated listeners using each listener's allowed origin. The shared active data on 8080 returned HTTP 200 with `valid=false`, `failure_code=upstream_unreachable`, message `upstream connection could not be reached`, and the connection stayed `CREATED_UNVERIFIED`. The separately restored runtime on 8082 produced the same result. Both failures took about 2.1 seconds; neither promoted the connection.
- Active listener 8081, through its configured HTTPS origin, returned HTTP 503 `probe_runtime_unavailable` / `connection validation runtime unavailable` before contacting Google. This differs from 8080/8082, which both acquire the adapter runtime and reach the guarded egress attempt. A direct HTTP-origin probe to 8081 was separately rejected 403 by CSRF/origin policy and was excluded from the comparison.
- The normal Cloud shell client, inheriting its configured proxy, queried Google AI Studio Models using the configured QA credential and received HTTP 200. A no-proxy direct request to the same service timed out (curl exit 28, HTTP 000). Cloud status is current: revision 9, unrestricted/enforced, credential bindings ready; retained Cloud network diagnostics contain no records.
- Source inspection of `internal/security/egress/ssrf_dialer.go` confirms Tanpopo deliberately does not use `http.ProxyFromEnvironment` for its SSRF-guarded transport. This security design prevents ambient proxy settings from bypassing the guard. In this Cloud environment direct sockets time out while the configured proxy works, so the `upstream_unreachable` result is an environment/transport compatibility limitation, not proof of invalid Google credentials or a Google outage. A safe, explicit guarded-proxy integration remains unverified; no successful Tanpopo-to-Google inference is claimed.
- `probe_runtime_unavailable` is isolated to listener 8081: the same adapter, connection state, binary family, and Cloud environment work through runtime acquisition on 8080/8082. Its exact per-process factory cause is not returned by the API and Cloud diagnostics have no record; classify as a listener/runtime configuration issue pending further isolation, not a confirmed general product defect.
- Captured and visually inspected all three browser states. Sanitized results: `evidence/phase26-provider-runtime-comparison.json`; screenshots: `phase26-active-8080-provider-probe.png`, `phase26-active-8081-provider-probe.png`, `phase26-restored-8082-provider-probe.png`. No app production state changed beyond ordinary QA login/audit records and probe audit/status fields. GATE-REL-07 remains BLOCKED.

## Checkpoint 27 — defect regression and independent interaction evidence (2026-11-23)

- Re-ran the Google provider probes and retained CP26’s three-runtime result. All three `/readyz` endpoints currently return `ready`; listener 8081 still fails provider runtime acquisition while 8080 and restored 8082 reach the guarded upstream attempt. No successful Tanpopo-mediated Google inference occurred. Shell access through the Cloud proxy succeeded and direct no-proxy access timed out; classify this as a Cloud proxy/direct-egress compatibility blocker. The 8081 discrepancy is instance-specific runtime/configuration failure with factory cause not exposed. Neither result alone proves a Google credential or provider defect.
- Revisited the open-defect set. DEF-003 (empty-scope first budget), DEF-004 (`config show` command collision), DEF-005 (MCP and A2A private endpoint CIDR omission), DEF-006 (Team Admin member form missing project scope), and DEF-008 (enabled Console backup action returns 503) reproduced in current isolated interaction. DEF-005 was confirmed on both forms; both API calls rejected before endpoint fetch or record creation. A targeted engineering handoff for DEF-005 is in `engineering-handoffs/DEF-005-private-endpoint-cidr-input.md`.
- DEF-009 did not reproduce in the current Playwright WebKit 26.5 interaction: at 375×844 the drawer heading stayed at x=73 and document width remained 375 across 0–1000ms. CP18 evidence remains intact and discrepancy is unresolved, so the historical finding remains open. DEF-001, DEF-002 and DEF-007 are preserved without a fresh retest this phase.
- All defect regression screenshots were visually inspected, including the A2A private registration/rejection and WebKit drawer at sampled animation times. Evidence: `evidence/phase27-defect-regression-summary.json`, `phase27-defect-regression.json`, `phase27-a2a-defect-retest.json`, `phase27-cli-config-defect-retest.json`, `phase27-finops-defect-retest.json`, `phase27-teamadmin-defect-retest.json`, and the referenced PNGs.
- This phase covers regression sampling, not exhaustive Alpha/Beta feature, role, platform or integration coverage. MCP tools/schema grants, A2A AgentCard/task delegation, positive public integrations, streaming/cancellation/failover, populated budget settlement/enforcement, additional role and mutation boundaries, deletion/retention, and broad CLI/API surfaces remain incomplete. No Tanpopo product code changed. GATE-REL-07 remains BLOCKED.

## Checkpoint 28 — isolated A2A backend registration smoke through live UI (2026-11-23)

- Started a deterministic local test fixture bound only to 127.0.0.1:19088 and opened the real A2A registration UI in Chromium on the isolated runtime. The UI payload contained `allow_private_ip: true` but no `allowed_cidrs`, confirming DEF-005 again. A Playwright-only request interceptor added the narrowly scoped `127.0.0.1/32` CIDR to the outbound test request; no Tanpopo files or runtime code were changed.
- With that explicit CIDR, the runtime returned HTTP 201 and the UI changed from zero agents to one Active agent row with a success banner; the screenshot was visually inspected. This exercises the backend's explicit private-endpoint registration contract and its visible post-submit state, but does not close DEF-005 because production UI itself still sends the incomplete request. The endpoint fixture was not contacted during registration; no A2A task was dispatched.
- The created `qa-cp28-local-mock` is isolated QA data in the disposable active SQLite test DB. No key, task, provider call, or spend was created. Evidence: `evidence/phase28-a2a-mock-ui-registration.json`, `phase28-a2a-mock-ui-form.png`, and `phase28-a2a-mock-ui-registered.png`. A separate direct API attempt without browser CSRF was rejected 403 and created no record; this harness-origin rejection is not a product finding.
- Remaining A2A task submission/status/cancel, caller grant boundaries, AgentCard fetch semantics, MCP discovery/tool calls, and broad per-role/per-browser coverage remain open. GATE-REL-07 remains BLOCKED.

## Checkpoint 29 — non-operator cross-tenant write-boundary probe (2026-11-23)

- In authenticated Chromium as the non-operator Team Admin, sent `POST /admin/v1/projects` with an intentionally empty JSON body and a valid browser CSRF token. Under the principal's bound organization it returned 403 `insufficient_role_permission`; with the other organization's ID header it returned generic 404 `resource_not_found`. Empty body ensured no project could be created even if request routing changed. The Access & Keys screenshot showed the current principal and no virtual keys; evidence is `evidence/phase29-teamadmin-write-boundary.json` and its visually reviewed screenshot.
- This adds one non-operator write-route authorization/concealment comparison. It does not test authorized writes, object-ID substitution, other roles/routes, pagination, or concurrent scope changes. No data changed and GATE-REL-07 remains BLOCKED.

## Checkpoint 30 — read-only audit provisioning recheck (2026-11-23)

- Read the isolated active SQLite DB through a read-only connection. Both organizations had zero organization-create audit events; the retained QA project `qa-isolation-20261123` and environment `qa-sandbox` each had zero corresponding `project.created` / `environment.created` entries targeting their IDs. This re-confirms DEF-007 while later member/login/operations events remain present. Evidence: `evidence/phase30-audit-provisioning-recheck.json`.
- No data was modified. This does not test audit tamper detection or the behavior of new mutations after the existing history; GATE-REL-07 remains BLOCKED.

## Checkpoint 31 — fresh-instance Setup accessibility retest (2026-11-23)

- Started a separate disposable Tanpopo runtime on 8083 with a fresh SQLite DB and opened the first-run Setup page in real Chromium. Visually inspected the Owner account step screenshot. The step buttons show visible text such as “Owner account” and aria-labels such as “Go to step 1: Owner account”; the accessible name contains the visible wording. A real axe scan on the fresh Setup view returned zero WCAG violations, so DEF-001 did not reproduce in this browser state. The two prior Vitest failures remain unresolved against this result. Visible input labels were also associated.
- No setup submission or product mutation occurred. Evidence: `evidence/phase31-fresh-setup-step1.png`, `phase31-def001-fresh-setup-labeling.json` and `phase31-fresh-setup-axe.json`. The scan covered the fresh first Setup step only; it is not a full Setup journey or all-route accessibility pass. GATE-REL-07 remains BLOCKED.

## Checkpoint 32 — Virtual Key issuance controls (2026-11-23)

- Real Chromium opened Access & Keys → Issue key in the default organization/project/environment. Visually reviewed the drawer: it exposes key name, model allowance and MCP tool allowlist; no A2A plane or agent/route grant is offered. Evidence: `evidence/phase32-vkey-issue-form.json` and `phase32-vkey-issue-form.png`.
- This inspection alone does not establish the resulting authorization behavior; that was exercised after enabling the A2A runtime in CP34. GATE-REL-07 remains BLOCKED.

## Checkpoint 33 — A2A disabled-by-default runtime classification (2026-11-23)

- On the existing runtime with no `TANPOPO_A2A_ENABLED` binding, an A2A Data Plane request returned 404 `A2A plane is disabled`. A first probe also carried the browser's Admin session cookie and was rejected as `invalid_credential_plane`; this was a harness credential-plane error, not an A2A product result. Evidence: `evidence/phase32-a2a-vkey-scope-probe.json` and `phase33-a2a-enabled-vkey-probe.json`.
- The runtime source reads `TANPOPO_A2A_ENABLED` and defaults false. A second isolated listener was started on 8084 with the flag true, ready for CP34. The disabled result is classified as environment configuration; GATE-REL-07 remains BLOCKED.

## Checkpoint 34 — enabled A2A plane and Virtual Key authorization (2026-11-23)

- With `TANPOPO_A2A_ENABLED=true` on isolated listener 8084, created a normal environment-bound Virtual Key through the real UI and called `GET /a2a/agents` from a cookie-free client. The anonymous request returned 401 `invalid_credential_plane`; the issued key returned 403 `scope_or_plane_denied`, “Virtual Key is not permitted on A2A plane.” The key was revoked with HTTP 200; its secret was held only in process memory.
- Read-only database inspection confirmed each CP32–34 test key had `allowed_planes=PROVIDER` and `state=REVOKED`. The issue form and public API do not expose A2A plane or route grants. This is DEF-010, a confirmed product provisioning gap, distinct from the disabled-by-default runtime configuration. The issue form and A2A agent list screenshots were visually inspected.
- Evidence: `evidence/phase34-a2a-vkey-bearer-probe.json`, `phase34-vkey-a2a-scope-readback.json`, `phase34-a2a-enabled-agent-list.png`, and phase32 form evidence. Targeted engineering handoff: `engineering-handoffs/DEF-010-a2a-virtual-key-scope.md`. No product code changed. GATE-REL-07 remains BLOCKED.

## Checkpoint 35 — Virtual Key loading-state timing (2026-11-23)

- Reopened Access & Keys after key revocation and sampled the UI at 0, 300, 1000 and 2500ms. At the immediate sample it displayed “Loading Access & Keys…”; by 300ms the page rendered three revoked key rows and remained stable at later samples. The earlier CP32 screenshot captured the same brief load placeholder at 550ms after a page reload; a fresh navigation showed completion by 300ms. The prior capture does not establish a persistent stale-state defect.
- The stable screenshot was visually inspected and showed the three QA keys revoked; evidence: `evidence/phase35-vkey-loading-timing.json`, `phase35-vkey-access-initial.png`, `phase35-vkey-access-stable.png`. No new defect assigned. Broader delayed-network and concurrent mutation stale-state behavior remain open. GATE-REL-07 remains BLOCKED.

## Checkpoint 36 — Virtual Key rotation, grace and invalidation (2026-11-23)

- Created a disposable provider-plane key through the real Access & Keys UI and rotated it from its row action. The confirmation states both secrets remain valid for 24 hours and the replacement is shown once. Rotate returned HTTP 200 and version 2. From a cookie-free client, both the old and new bearer tokens returned HTTP 200 for `GET /v1/models` during the grace window.
- Revoked the rotated key, confirmed HTTP 200 / `REVOKED`, then confirmed both old and replacement bearer tokens returned 401 `invalid_virtual_key`. A fresh Access & Keys navigation displayed the revoked row. The old key was kept briefly in a mode-0600 temp file and deleted after testing; replacement remained in process memory only. No secret appears in evidence.
- Evidence: `evidence/phase36-key-lifecycle.json`, `phase36-key-lifecycle-confirm.png`, `phase36-key-lifecycle-revoked.png`, `phase36-key-lifecycle-revoked-ui.json`, and read-only final state `phase36-key-lifecycle-db-state.json`. The confirmation and final-state screenshots were visually inspected. GATE-REL-07 remains BLOCKED.

## Checkpoint 37 — Virtual Key graceful revoke through UI (2026-11-23)

- Issued a disposable key through the UI, opened its row menu, selected Revoke key, and reviewed the modal's zero recent-traffic count and Graceful drain behavior. The UI revoke returned HTTP 200, displayed a success banner with zero active streams severed, and refreshed the row to Revoked without a manual reload. A cookie-free `GET /v1/models` using its bearer token then returned 401 `invalid_virtual_key`.
- Evidence: `evidence/phase37-ui-revoke.json`, `phase37-ui-revoke-confirm.png`, `phase37-ui-revoke-result.png`, and `phase37-qa-key-cleanup.json`; both screenshots visually inspected. Read-only cleanup check found all six CP32–37 test keys revoked and temporary key-secret files absent. This validates one graceful no-stream path; emergency abort, active-stream drain, concurrent key rotation/revocation and quota-pause transitions remain open. No new defect assigned; GATE-REL-07 remains BLOCKED.


## Checkpoint 38 — fresh-install journey on 8083

- Started from a fresh SQLite database with `initialized=false` and no enabled provider adapters. In real Chromium, opened the Setup wizard, tested the empty bootstrap-token validation, entered a synthetic owner and tenant, proceeded through the no-adapter state, acknowledged master-key custody, reviewed the owner/org/project summary, and submitted Complete setup.
- Bootstrap completed with HTTP 201. The bootstrap status then reported initialized, the authenticated session had `OPERATOR` + `ORG_OWNER`, and the browser reached Overview under the new QA organization, project, and `dev` environment. Before setup, `/v1/models` returned 401 `invalid_virtual_key`. No provider connection, route, or Virtual Key was created because the instance had no available adapter and the wizard offered the documented skip path.
- Captured and visually inspected all seven first-run screenshots from blank validation through Overview: `evidence/phase38-setup-step1-empty.png`, `phase38-setup-step1-validation.png`, `phase38-setup-step1-filled.png`, `phase38-setup-step2-no-adapters.png`, `phase38-setup-step3-custody.png`, `phase38-setup-step4-review.png`, and `phase38-setup-complete-overview.png`. Sanitized outcomes are in `evidence/phase38-fresh-install-journey.json`; owner password and bootstrap token are excluded.
- This passes owner/tenant bootstrap and its skip-provider path only. A usable initial route/key and first inference are still UNVERIFIED; this fresh instance must install/activate an adapter after setup before those steps can be exercised. No product code changed. GATE-REL-07 remains BLOCKED.

## Checkpoint 39 — A2A discovery and task lifecycle on 8084

- Used the enabled isolated 8084 runtime and its existing loopback synthetic agent. The UI/API-issued scoped QA key initially had only `PROVIDER`, reproducing DEF-010. To isolate-test the authorized data plane, the QA-only row's `allowed_planes` was set to `PROVIDER,A2A` in the disposable SQLite DB and a `config_changelog` invalidation was added; a SQLite backup was captured before the fixture mutation. No Tanpopo implementation code or authorization logic changed. Secret remains only in a mode-0600 temporary file and is scheduled for revocation/removal.
- Cookie-free requests then returned 200 from `/.well-known/agent.json` and `/a2a/agents` with the registered synthetic agent. A hop-count 6 task was rejected 400 `delegation_depth_exceeded` without upstream dispatch. A valid synchronous task reached the local fixture, persisted as `completed` (version 3), exposed its artifact on GET, and streamed a terminal SSE event. A `dispatch_async=true` task persisted as `submitted`, was readable by GET, then caller-canceled and remained `canceled` on re-read. The endpoint saw exactly the two synchronous valid calls; it received no caller `Authorization` header. The agent endpoint was loopback-only and the mock produced no external side effects.
- Visually inspected `evidence/phase39-a2a-agent-task-ui.png`; it showed the registered agent and the completed/canceled persisted task rows. Sanitized API summary: `evidence/phase39-a2a-lifecycle.json`; endpoint request evidence: `/tmp/qa-tanpopo/phase39-a2a-endpoint-requests.jsonl` (QA-local, not secret-bearing). This confirms core discovery, completion, cancellation, hop guard and bearer stripping, but does not close DEF-010 or establish budget settlement, nested delegation, permission matrix, or restart recovery. GATE-REL-07 remains BLOCKED.

## Checkpoint 40 — fresh-instance Marketplace review

- Opened Extensions → Marketplace on the fresh 8083 tenant in Chromium. Eight bundled providers were visible; all catalog entries displayed Official origin and `None` publisher signature. Opened the Ollama Local adapter's staged permission review: the candidate adds loopback/localhost/ollama.internal hosts, an `api_key` secret slot, provider hooks, and private-IP access. The review requires an explicit consent checkbox before approval/activation.
- Visually inspected the catalog at `evidence/phase40-fresh-marketplace.png` and the review dialog at `phase40-ollama-stage-dialog.png`; sanitized text is retained beside them. The package was staged for review; successful activation and the later connection/route/key path are UNVERIFIED at this checkpoint.

## Checkpoint 41 — A2A input-required resumption contract

- Changed only the local mock endpoint response to `input-required` and submitted an authorized synthetic task. The create response was HTTP 201, state `input-required`, version 3; GET returned the same persisted nonterminal state. The documented SM-09 contract says `input-required` resumes to `working` when caller input is provided. POST `/a2a/tasks/{id}/transition` with a resume state and input returned HTTP 404 `resource_not_found` / `A2A endpoint not found.` The implementation exposes only GET task and POST cancel for task subroutes.
- Visually inspected `evidence/phase41-a2a-input-required-console.png`; the Console showed the task indefinitely as “Input required” with no resume action or prompt. Sanitized repro and contract comparison: `evidence/phase41-a2a-input-required.json`. This is a confirmed defect and leaves multi-turn A2A workflows blocked; DEF-011 is appended below and a standalone handoff is in `engineering-handoffs/DEF-011-a2a-input-required-cannot-resume.md`.
- No product implementation was changed. The mock is isolated to 127.0.0.1:19088. GATE-REL-07 remains BLOCKED.

## Checkpoints 42–47 — provider marketplace and first successful fresh-install inference

- CP42: On isolated 8083, Org Owner explicitly reviewed and approved the staged bundled Ollama extension permission diff; it activated. This was a test-environment tenant action, not release approval. Evidence `phase42-ollama-activation.json` and visually inspected before/after screenshots.
- CP43: Providers UI created `qa-cp43-local-ollama` with explicit `127.0.0.1/32`, `SELF_HOSTED_INFRA`, `ON_PREM`; the isolated fixture probe returned valid and detected `llama3.2`. Default Ollama form state was separately observed as Commercial API / US, contrary to the bundled manifest default; DEF-013 added. Evidence `phase43-*`.
- CP44–45: UI registered logical model `llama3-2-qa-local`, added a healthy exact-rate deployment, then created a 100% priority route. Evidence `phase44-*`, `phase45-*`; visually inspected screenshots.
- CP46–47: Access & Keys issued a scoped key; cookie-free `/v1/models` and `/v1/chat/completions` returned HTTP 200 from the synthetic local fixture. Trace showed 13 input/10 output tokens. FinOps project row showed one exact 1-micro-USD settlement, while the headline stayed at zero because no budget ledger existed. DEF-012 added. Evidence `phase46-*`, `phase47-*`, plus CP51 DB reconciliation. Key later revoked in CP50.

## Checkpoints 48–51 — provisioning audit, alternate-engine attempt, credential cleanup and FinOps reconciliation

- CP48 read-only SQLite query on the fresh bootstrap tenant found no `organization.created`, `project.created`, or `environment.created` events for the created org/project/dev IDs; 49 unrelated audit events show the audit store is otherwise active. `TP-IMP-0024` AC-1 and SCR-08 audit list document the provisioning contract. DEF-007 reconfirmed; `phase48-fresh-bootstrap-audit.json`.
- CP49 attempted DEF-009 in Playwright WebKit 26.5 at 375×844. Host lacks GTK/WebKit libraries; official Playwright Noble image launched, but the WebKit auth/navigation attempt ended back at `/login` (`Loading sign-in…`) and the script timed out waiting for Add Webhook. The target interaction was not reached. UNVERIFIED. Diagnostic: `evidence/phase49-def009-attempt.json`. Historical CP18 reproduction and CP27 non-reproduction remain unresolved; no new screenshot is claimed.
- CP50 gracefully revoked CP46's key in Access & Keys; the UI showed Revoked and zero active streams severed, then the same bearer returned HTTP 401 `invalid_virtual_key`. The one-time secret file was securely deleted after use. Screenshots `phase50-key-revoke-confirm.png`, `phase50-key-revoked.png`; result `phase50-key-cleanup.json`.
- CP51 reconciled FinOps UI to usage/budget DB tables: no budget rows, one exact settlement at 1 micro-USD, same-screen headline zero vs project row 1 micro-USD. Confirmed DEF-012. Evidence `phase51-finops-kpi-chargeback-mismatch.json` and phase47 screenshot.

## Checkpoints 52–57 — independent provider defect reproduction and cleanup

- CP52: Started an isolated loopback fixture on 127.0.0.1:19090 that returns HTTP 401. Created a private Ollama connection via the UI with 127.0.0.1/32; Probe returned HTTP 200, `valid=false`, `auth_failed`; no external network/provider was contacted.
- CP53–54: UI allowed a deployment on the failed-probe connection to be activated. API/UI reported `ACTIVE_HEALTHY`; the deployment row rendered Healthy while the connection probe was false and the connection remained in a draft/unverified state. This freshly confirms DEF-002's product-side mismatch. No inference was sent through this newly created route; CP5 historical HTTP 503 evidence remains the route/data-plane failure proof. See `phase52-*`, `phase53-*`, `phase54-*`.
- CP56–57: Deleted the synthetic failed deployment and connection through the UI; verified the original healthy local deployment remained. Stopped fixture processes. Cleanup evidence `phase56-*`, `phase57-*`.

Current classification: Google failures remain separate from DEF-002. CP26 found 8080/8082 `upstream_unreachable` consistent with proxy/direct-egress mismatch and 8081 `probe_runtime_unavailable` isolated to that listener. No Google inference succeeded. Local fixture `auth_failed` in CP52 is a controlled product-health test, not a Google/Cloud defect classification. No implementation changed. GATE-REL-07 remains BLOCKED.

## Checkpoints 58–60 — repeat checks for FinOps, CLI, and backup defects

- CP58 revisited the fresh 8083 FinOps Budgets tab as Org Owner: count 0, “No budgets configured,” no create/add budget control. The screenshot was captured after the empty state loaded and visually reviewed; `evidence/phase58-def003-empty-budget.png`, `phase58-def003-retest.json`. DEF-003 remains confirmed; Admin API creation remains the historical workaround.
- CP59 reran `/tmp/qa-tools/tanpopo config show --output json`: exit 0 but output was only the local `config` group help; `config reconcile --help` remained reachable. DEF-004 remains confirmed; `evidence/phase59-def004-retest.json`.
- CP60 opened Operations → Backups as Operator/Org Owner. The “Create encrypted backup” action was enabled; clicking it returned HTTP 503 `ops_backend_unavailable`, with an explicit missing `tanpopo.lock`, `tanpopo.yaml`, and extension artifact store message. Receipt count stayed zero. Visually inspected before/after screenshots `phase60-def008-backups-before.png`, `phase60-def008-backup-failure.png`; result `phase60-def008-retest.json`. DEF-008 remains confirmed.

The remaining DEF-006 latest interaction evidence is CP27's non-operator Team Admin form with an actual 403 and no membership mutation; CP57 did not repeat that role path. DEF-001 and DEF-009 current boundaries are recorded in the CP48–57 section above. GATE-REL-07 remains BLOCKED.

## Checkpoints 61–62 — chargeback groups and export formats

- CP61 selected Project, Environment, Virtual Key, and Model groupings in the real FinOps screen. Each group showed the same single exact 1-micro-USD settlement and one exact request after the associated key was revoked. Screenshots `phase61-chargeback-{project,environment,key,model}.png` were captured and inspected (key/model views visually confirmed).
- CP61 clicked Export CSV. Backend returned valid CSV with HTTP 200, but the UI showed a JSON parse error and no download. Confirmed DEF-014. Evidence `phase61-chargeback-export-csv.json` and visually inspected `phase61-export-csv-ui.png`.
- CP62 clicked Export JSON. The UI displayed “Exported JSON chargeback report”; downloaded JSON included the one exact row (`exact_micro_usd: 1`). Evidence `phase62-export-json.json` and visually inspected screenshot `phase62-export-json-ui.png`.

DEF-012 remains confirmed: all group rows and JSON export show the settled amount while the headline exact total remains zero. GATE-REL-07 remains BLOCKED.

## Checkpoint 63 — DEF-001 focused accessibility-test reproduction

Reran only the two original Setup Vitest cases (`hides unavailable presets…`, `offers only adapters…`). Both still fail the axe assertion with serious `label-content-name-mismatch`: the step 1 progress button's `aria-label="Go to step 1: Owner …"` excludes its visible button text. 11 unrelated tests in that file were skipped. Evidence: `phase63-def001-targeted-vitest.txt` and sanitized summary JSON. This confirms the test-side defect remains reproducible; it does not explain why CP31 live Chromium axe found no violation, so DEF-001 remains open. No implementation changed. GATE-REL-07 remains BLOCKED.

## Checkpoint 64 — export grouping consistency

With Group by set to Model, the table showed `llama3-2-qa-local`. Export JSON succeeded but requested `/admin/v1/usage/export?format=json&group_by=project`; downloaded `group_key` was `qa-fresh-project`. This confirms DEF-015; one-project totals match, but the exported aggregation dimension is wrong. Evidence: `phase64-*`, screenshots visually inspected. GATE-REL-07 remains BLOCKED.

## Checkpoint 65 — WebKit mobile drawer retest remained blocked before target

In the official Playwright Noble container, WebKit rendered the 375×844 login screen (visually inspected at `evidence/phase65-webkit-after-login.png`). The attempt returned to the empty sign-in form and never reached Operations or the webhook drawer. `phase65-def009-webkit-login-attempt.json` records the boundary. This is UNVERIFIED, not a product pass/fail; CP18/CP27 remain conflicting historical evidence. GATE-REL-07 remains BLOCKED.
