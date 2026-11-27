# Product Coverage Matrix

Baseline `a771311107c4e4c2c7d9b53a3f5a0926f114b799`. Status: `I` = inventory started, `B` = blocked, `U` = not exercised, `P` = partial. Automated unit coverage is not equivalent to product journey coverage.

| Area | Source / evidence entry point | Status | Notes |
|---|---|---:|---|
| Setup and first run | Chromium UJ: bootstrap → owner → empty provider skip → key custody → initialize | P | Invalid blank submit and success flow persisted; optional connection skipped |
| Login, IAM, RBAC | Chromium local login, member role grant, Security Reviewer denial at Ops route, Team Admin session | P | Team Admin can read own-org projects; unbound-org request returns generic 404; Operations route is denied. Access & Keys exposes Add member, but submitting without project_ids returns 403 (DEF-006). Other roles and write scope tests remain open. |
| Organizations / tenancy | Operations → Organizations; workspace scope switcher; scoped Admin API | P | Created/switched a second tenant; non-operator Team Admin gets HTTP 200 for eight bound-org collections plus two project environment subcollections; matching other-org requests return generic 404. Other item-ID, pagination, write, and role probes remain open. |
| Providers / credentials | Gemini adapter activation, vault seal, connection create, real provider probe | P | Direct Google API works; Tanpopo probe fails `upstream_unreachable` due guarded transport vs proxy egress. Routed request fails closed with 503; management UI mismatch is DEF-002. |
| Models / routing / policies | Console Models, Routing; dataplane API | P | Logical model, deployment, route, activation and dry-run explain exercised. An unverified connection was displayed Healthy (DEF-002); failover and policy precedence remain open. |
| Marketplace / extensions | Console Extensions; `extensions/` | P | Two official adapters staged/activated after explicit permission review; community installation, upgrade and rollback remain open |
| MCP / A2A | Console MCP; runtime proto and APIs | P | Real browser reached both empty states. Private-endpoint registration fails for both Console forms because the UI omits required `allowed_cidrs` (DEF-005); no endpoint was fetched. MCP discovery, tool grants/calls, AgentCard registration and delegation remain open. |
| Inference / developer tools | Playground; CLI; dataplane API | P | Real `/v1/chat/completions` returned 200 through isolated deterministic fake provider, and trace appeared. Live Google inference, other ingress formats, streaming, native passthrough, Playground and CLI remain open. |
| Traces / observability | Console Traces, Overview | P | E2E inference trace persisted and appeared in Traces; detailed privacy/export/retention remains open. |
| FinOps / budgets / billing | Console FinOps; finance packages | P | API seeded an isolated organization ceiling; Console listed/edited it and rejected negative values. First-budget UI action missing (DEF-003); holds/enforcement/settlement/chargeback remain untested. |
| Security / audit | Console Security; security packages; scoped Admin API | P | Reviewer role denial, write-only secret sealing, unauthenticated 401s, one non-operator cross-org 404 and UI verification of the 172-event audit chain observed. Runtime organization/project/environment create events are absent (DEF-007); tamper detection and cross-scope routes remain open. |
| Operations / backup / recovery | Console Operations; CLI backup | P | Health/cluster page returned Ready with one OK SQLite node; `0.2.0` upgrade preflight returned READY_TO_UPGRADE. Webhook form correctly requires a sealed HMAC secret, but its drawer is offscreen in WebKit at 375px (DEF-009). Console backup endpoint returns 503 (DEF-008). CLI archive verify/restore fail while an org chain is empty, but create/verify/restore plus recovered-app login succeeded after that QA org had valid member/login audit events; missing provisioning events remain DEF-007. |
| Accessibility | Vitest and Chromium axe | P | E2E reported 0 violations across 61 screen samples; 2 existing Vitest failures remain DEF-001. |
| Setup and unavailable error states | Chromium setup and API fallback screens | P | Setup success and invalid-submit validation tested with live isolated backend; historical 500 fallback captured at 1440px and 375px |
| Responsive layout / locales / themes | Chromium screenshots + E2E axe/layout sweep | P | 27 routes audited across 1440/768/375, light/dark; command palette, multi-org, VI/JA strings passed. Phase 18 checked Operations health, webhook drawer and audit log at 375px in Chromium/Firefox/WebKit; WebKit drawer is offscreen (DEF-009). All locales and wider per-feature interaction parity in Firefox/WebKit remain open. |
| CLI and complete API surface | Binary built; health/setup/admin/data-plane paths exercised | P | Go suite passes with umask 0022; successful chat endpoint and unauthenticated 401 checks passed. `config show` is shadowed by duplicate root `config` groups (DEF-004). Full API/CLI contract coverage remains open. |

## User-specified baselines

Authoritative definitions exist in `docs/product/feature-catalog.md` (FT-01..16), `docs/ux/screen-specifications.md` (SCR-01..16), and `docs/requirements/workflows.md` (WF-01..13). The baseline identifiers are mapped as product areas below; all remain behaviorally untested unless separately noted above.

| Baseline | Scope | Status |
|---|---|---:|
| FT-01..16 | Product feature catalog, gateway through self-hosted operations | P |
| SCR-01..16 | First run/auth, overview, providers/connections, models, marketplace/extensions, routes/policies, MCP, traces/FinOps, tenancy/access, security, operations | P |
| WF-01..13 | Business workflows including first request, provider/plugin lifecycle, identity, routing, governance, FinOps/operations, recovery and deletion | P |

`I` means the source requirements are identified; it does not mean each requirement has been individually mapped to an executed test. Detailed per-ID traceability and any contradictions remain Phase 0 work.

## Checkpoint 4 feature-level traceability (2026-11-23)

| Feature | Status | Evidence / open coverage |
|---|---:|---|
| FT-01 Unified inference | P | Local fake-provider chat completion 200; streaming, multimodal, response/embedding formats, failover and cancellation open. |
| FT-02 Native passthrough | U | No native dialect journey. |
| FT-03 Adapter/connection/model/deployment | P | Adapter activation, secret/connection setup, model and deployment in isolated E2E; commercial provider validation blocked; confirmed readiness mismatch DEF-002. |
| FT-04 Vault/OAuth/federated identity | P | Write-only secret sealing and masked display observed; OAuth, rotation and refresh untested. |
| FT-05 Plugin sandbox/credential broker | P | Gemini/OpenAI-compatible adapter permission diff and explicit consent observed; sandbox adversarial behavior/secret broker not tested. |
| FT-06 Registry/update/rollback | P | Official marketplace package staged/activated; Git/archive/private registry, upgrade, signature verification and rollback untested. |
| FT-07 MCP governance | U | Route renders; tools, approval and schema pinning not exercised. |
| FT-08 A2A | U | Deep link loaded; delegation and policy behavior untested. |
| FT-09 Tenancy/RBAC/virtual keys | P | Multi-org create/switch, reviewer denial, key issuance/use observed; tenant isolation and lifecycle transitions open. |
| FT-10 Router/failover/explain | P | Fake-provider route returned 200; real unverified route was falsely shown Healthy but returned 503 (DEF-002). Dry-run explain ran; failover and policy constraints untested. |
| FT-11 Policy/rate limit/SSRF | U | No adversarial policy, quota, rate limit or SSRF test. |
| FT-12 Audit/kill switch | P | Security Reviewer access denial and UI hash-chain verification of 172 current events passed; no tamper-detection injection or kill-switch drill. Missing tenancy create events remain DEF-007. |
| FT-13 Budget ledger/chargeback | U | E2E trace exists, but no ledger accuracy/enforcement test. |
| FT-14 Telemetry/privacy | P | Trace visible after request; redaction, export, retention and privacy settings untested. |
| FT-15 Console/CLI/Admin API | P | Console browser journey and 401 API probes; CLI/full API contract untested. |
| FT-16 Startup/GitOps/backup/upgrade | P | Startup and health/readiness validated; upgrade preflight returned READY_TO_UPGRADE without mutation. CLI create/verify/restore and restored-app login succeeded after QA org audit history existed; verify/restore fail for the empty-chain state (DEF-007). Console endpoint is unavailable (DEF-008); live upgrade and lockfile lifecycle remain open. |

WF-01/02 setup, WF-03/04 adapter setup, WF-05 static key connection, partial WF-06 chat, and partial WF-07 single-target routing have evidence. WF-08..13 remain untested except UI presence. Browser: Chromium, Firefox and WebKit route rendering exercised; Firefox/WebKit ran in the official container because the host lacks GTK4, Graphene, HarfBuzz ICU, Manette, Hyphen and GLESv2 libraries. Per-feature alternate-engine workflows remain partial. GATE-REL-07 remains BLOCKED.

Checkpoint 5 expands FT-03/FT-10 negative readiness behavior and records DEF-002. No additional feature area is complete; GATE-REL-07 remains blocked.

Checkpoint 6 extends SCR-02..16 light desktop route rendering across Firefox/WebKit and captures five representative 375px views in each. Host library gaps were bypassed with the official matching Playwright image. Browser-specific interaction coverage beyond login/navigation remains partial.


## Checkpoint 8 feature trace update

FT-13/WF-08 is now `P`: synthetic organization budget creation through Admin API, UI listing/edit, and negative limit validation passed; the missing first-budget Console action is DEF-003. Hard-limit enforcement, request reservations, settlement, concurrent burst, alert thresholds and chargeback export remain untested.

## Checkpoint 9 — CLI configuration command reachability

`tanpopo config show --output json` exited 0 but displayed only local reconciliation help. Root help contains two separately registered `config` groups; remote `config show` is unreachable (DEF-004). This is a CLI usability/diagnostic finding, not a configuration-data mutation. CLI and complete API coverage stays `P`; GATE-REL-07 remains BLOCKED.

## Checkpoint 10 — private MCP and A2A registration journeys

Real Chromium interactions captured both empty states and submitted private endpoint registrations. MCP returned HTTP 400 `allowed_cidrs is required when allow_private_ip is true`; A2A returned HTTP 400 with the same requirement. The frontend sends the private-access flag without an allowlist (DEF-005). Neither registration persisted and no endpoint request was made. MCP/A2A coverage is `P`; tool discovery, grants, calls, AgentCard behavior and task delegation remain open. GATE-REL-07 remains BLOCKED.

## Checkpoint 11 — owner project and environment provisioning

An Org Owner created QA-only project `qa-isolation-20261123` and environment `qa-sandbox` through the real Console. Both create APIs returned HTTP 201; the scope switcher selected each new scope and the resulting Overview was captured. Evidence: `evidence/phase11-workspace-provisioning.json`. These records remain in the isolated QA DB; no provider calls or spend were generated. This is partial workflow coverage and does not verify cross-organization authorization isolation. GATE-REL-07 remains BLOCKED.

## Checkpoint 12 — organization creation and workspace boundary

The isolated QA operator created `qa-boundary-org` (HTTP 201) and switched into it. The scope picker displayed only `No project` / `No env`; `qa-isolation-20261123` remained in the Default Organization scope. Evidence: `evidence/phase12-organization-scope-journey.json`. This confirms UI scope separation under a global operator owner, not authorization isolation for a non-operator. The existing reviewer recovery password did not authenticate, so non-operator direct-org access checks remain open. GATE-REL-07 remains BLOCKED.

## Checkpoint 13 — non-operator cross-organization read boundary

A test-only human principal was added as `Developer + Org Owner`, issued a local recovery password, then updated through Access & Keys to `Developer + Team Admin` with no Operator binding. In a real Chromium authenticated session, its scoped `GET /admin/v1/projects` returned 200 and two projects for its bound organization. The same request with `X-Tanpopo-Org-ID` set to `qa-boundary-org` returned 404 `resource_not_found` with a generic “the requested resource was not found” message. Evidence: `evidence/phase13-nonoperator-cross-org-authorization.json`; visual role/session evidence is in `phase13-boundary-probe-team-admin.png` and `phase13-team-admin-session.png`. This is one successful isolation probe, not comprehensive tenant isolation coverage. GATE-REL-07 remains BLOCKED.

## Checkpoint 14 — Team Admin scope denial

For the non-operator Team Admin, the navigation hid Operations and Security; direct `/ops/health` showed “Access restricted” with the assigned roles. `GET /admin/v1/organizations` returned HTTP 403 `insufficient_role_permission`. Access & Keys remained visible. Evidence: `evidence/phase14-team-admin-authorization.json` and screenshots `phase14-team-admin-ops-denial.png`, `phase14-team-admin-members.png`. This adds one route-level denial and one operator-only API denial; broader role matrix remains open.

## Checkpoint 15 — Team Admin member creation dead end

The Team Admin's visible Add member form accepted email and role only, then received HTTP 403 because no `project_ids` were supplied. No member was created. Added DEF-006, Moderate. Evidence: `evidence/phase15-team-admin-add-member-journey.json`. Project-scoped member-management controls remain incomplete; GATE-REL-07 remains BLOCKED.

## Checkpoint 16 — tenancy audit events and backup recovery

Operations Console backup creation returned 503 before and after a valid QA `tanpopo.yaml` was added; the canonical server's `ConfigPath` remains empty (DEF-008). CLI `backup create` succeeded, producing an encrypted 83,501-byte mode-0600 archive, but `backup verify` exited 14 because `qa-boundary-org` had no verifiable audit history. Read-only database inspection found zero audit events for that organization and zero `project.created`/`environment.created` events for the QA project/environment (DEF-007). Evidence: `evidence/phase16-audit-and-backup-verification.json`.

## Checkpoint 17 — operational status, chain verification and recovery attempt

Real Chromium reported Ready / Serving traffic, one SQLite WAL node `OK`, and config epoch 23. Upgrade compatibility preflight for Core `0.2.0` returned `READY_TO_UPGRADE`; no upgrade was run. The webhook form was empty and correctly kept Register disabled until a write-only HMAC signing secret is available. The Security UI verified the existing audit chain intact across 172 events; this does not compensate for the missing tenancy-create events (DEF-007). An isolated CLI restore attempt with the correct KEK and `--verify-secrets-decrypt` exited 14 on the missing-history error; destination was not created and the active DB was untouched. Evidence: `evidence/phase17-operations-routes-final.json`, `phase17-operations-interactions.json`, `phase17-restore-attempt.json`, and visually reviewed screenshots. GATE-REL-07 remains BLOCKED.

## Checkpoint 19 — tenant collection read boundary

The non-operator Team Admin's read-only GETs to projects, members, models, connections, routes, budgets, traces and secret metadata returned 200 in its bound organization and generic 404 `resource_not_found` under the second org ID header. Response payloads were not saved. Evidence: `evidence/phase19-cross-tenant-read-sweep.json`. This broadens only collection-read coverage; cross-org writes, resource-ID substitution, pagination replay and alternate role combinations remain untested. GATE-REL-07 remains BLOCKED.

## Checkpoint 20 — nested environment collection boundary

GETs to each bound-org project’s environment subcollection returned 200 with one row. The same two project IDs under the alternate org header returned generic 404 `resource_not_found`. IDs/row data were not retained. Evidence: `evidence/phase20-cross-tenant-nested-read.json`. This is limited to nested environment reads. GATE-REL-07 remains BLOCKED.

## Checkpoint 21 — encrypted backup recovery through restored runtime

Created a QA-only Org Owner in `qa-boundary-org` and authenticated once, adding `member.created` and `auth.login_succeeded` chain entries; the missing org/project/environment creation events remain absent (DEF-007). With both org chains now verifiable, CLI encrypted backup create, verify (exit 0; 7 files), and restore with `--verify-secrets-decrypt` all succeeded into a new directory. Read-only restored SQLite check returned `integrity_check=ok`, 2 organizations, 5 principals and 195 audit rows. Staged the restored files into an isolated runtime on 8082 and confirmed health/readiness; Chromium login as the QA tenant owner and Overview loaded with HTTP 200 and no Admin API errors. Evidence: `evidence/phase21-backup-successful-recovery.json`, `phase21-restored-runtime-smoke.json`, `phase21-test-tenant-owner-creation.json`, and visually reviewed `phase21-restored-runtime-owner.png`. The earlier empty-chain failure remains separately documented; Console backup is still DEF-008. GATE-REL-07 remains BLOCKED.

## Checkpoint 22 — Security kill switches

| Capability / journey | Evidence-backed coverage | Status / remaining scope |
| --- | --- | --- |
| Provider-connection blast-radius preview | Authenticated Chromium showed zero orgs, projects, virtual keys, connections for the QA unverified connection; visual capture reviewed. | Partial; populated/nonzero impact and other target levels open. |
| Trigger and clear | Trigger POST 200, epoch 24, one entity affected, zero streams severed; clear DELETE 200; UI returned to zero active switches. | One reversible path passed; stream termination, propagation, concurrency and audit detail open. |
| Browser origin enforcement | Non-configured HTTPS origin was rejected 403 before state change; configured origin completed same journey. | Test harness origin corrected; no product defect assigned. |

Evidence: `evidence/phase22-killswitch-journey.json`, `phase22-killswitch-partial.json`, and visually inspected `phase22-killswitch-preview.png`, `phase22-killswitch-confirm.png`, `phase22-killswitch-active.png`, `phase22-killswitch-cleared.png`. This does not change release gate status; GATE-REL-07 remains BLOCKED.

## Checkpoint 23 — kill-switch audit coverage

| Capability / journey | Evidence-backed coverage | Status / remaining scope |
| --- | --- | --- |
| Audit visibility for trigger and clear | Audit Log displayed both events in sequence; verify action returned HTTP 200 and reported intact JCS chain across 189 events. | Partial; event detail payloads, tamper detection, export, pagination and missing provisioning events remain open. |

Evidence: `evidence/phase23-post-clear-audit-verify.json` and visually inspected screenshot `phase23-post-clear-audit-verify.png`. This does not close DEF-007 or GATE-REL-07.

## Checkpoint 24 — mobile cross-browser kill-switch UI

| Capability / journey | Evidence-backed coverage | Status / remaining scope |
| --- | --- | --- |
| Open Project-level kill-switch form | Firefox 153 and WebKit 26.5 at 375×844; heading/forms/footer visible, 375px document width, active count zero. | Partial; other mobile browser engines, form submissions, and confirmation flow open. |
| Required-field affordance | Blank target/reason/project-confirmation fields kept Review & trigger disabled in both engines; no API errors. | Client-side check only; tampered requests and server validation remain open. |

Evidence: `evidence/phase24-killswitch-cross-engine-mobile-sanitized.json`, `phase24-firefox-killswitch-375.png`, `phase24-webkit-killswitch-375.png`. DEF-009 remains specific to the WebKit Webhooks drawer tested in CP18.

## Checkpoint 25 — Google provider validation retest

| Capability / journey | Evidence-backed coverage | Status / remaining scope |
| --- | --- | --- |
| Provider adapter runtime acquisition | Active runtime returned 503 `probe_runtime_unavailable` while adapter metadata was `ENABLED_ACTIVE`; no connection state change. | Environment blocker; factory root cause unreported. |
| Provider validation result | Restored runtime allowed-origin Probe returned HTTP 200 with `upstream_unreachable`; connection stayed `CREATED_UNVERIFIED`. | Google egress remains unverified; no successful Tanpopo inference. |
| Test-origin enforcement | Restored HTTPS proxy origin received 403 before mutation; direct allowed origin permitted the probe. | Harness-origin mismatch documented separately from provider result. |

Evidence: `evidence/phase25-provider-egress-retest.json`, `phase25-runtime-capability-summary.json`, and visually inspected screenshots. Earlier successful direct Google request is retained at phase 3; credentials/payload are not retained.

## Checkpoint 26 — runtime and provider egress comparison

| Surface | Observed behavior | Classification / remaining scope |
| --- | --- | --- |
| Listener 8080 | Probe HTTP 200, `valid=false`, `upstream_unreachable`; connection remains `CREATED_UNVERIFIED`. | Guarded egress path blocked in Cloud environment. |
| Listener 8081 | Configured-origin Probe HTTP 503 `probe_runtime_unavailable`; no state promotion. | Instance-specific runtime configuration issue; exact factory cause unobserved. |
| Restored listener 8082 | Probe HTTP 200, `valid=false`, `upstream_unreachable`; connection remains unverified. | Same guarded egress boundary as 8080. |
| Shell proxy/direct comparison | Google Models via Cloud proxy HTTP 200; no-proxy request HTTP 000 / timeout. | Confirms proxy/direct path split; credentials and Google service are reachable through configured proxy. |

All three browser screenshots were visually inspected. Evidence: `evidence/phase26-provider-runtime-comparison.json` and its three screenshots. No app-mediated Google inference succeeded.

## Checkpoint 27 — regression coverage update

| Area | CP27 evidence | Status / remaining work |
| --- | --- | --- |
| Budget first-use UX | Default organization FinOps showed zero budgets and no create action. | DEF-003 reproduced; successful create/edit/threshold/ledger workflow remains open. |
| CLI server configuration inspection | `tanpopo --output json config show` returned local `config` reconcile help (exit 0). | DEF-004 reproduced; other CLI root commands remain partial. |
| MCP/A2A private registration | Both forms lack CIDR inputs; real Chromium submits returned HTTP 400; neither record persisted nor endpoint fetched. | DEF-005 reproduced on both surfaces; positive public MCP discovery/tools and A2A AgentCard/delegation remain open. |
| Team Admin member management | Form exposes email and role but no project IDs; backend rejects with 403, no member added. | DEF-006 reproduced; remaining member/role combinations open. |
| Console backups | Enabled create action returns 503 and no receipt appears. | DEF-008 reproduced; CLI backup recovery path has prior successful coverage, console path remains blocked. |
| WebKit mobile webhook drawer | Actual interaction at 375×844 showed panel inside viewport across 0–1000ms. | DEF-009 not reproduced in this run; CP18 evidence retained and issue stays open pending parity investigation. |
| Google provider probe runtime | 8081 returns `probe_runtime_unavailable`; 8080/8082 return `upstream_unreachable`; proxy shell request succeeds while direct egress times out. | Cloud proxy/direct-egress compatibility and one listener runtime discrepancy; app inference still unverified. |

DEF-001, DEF-002 and DEF-007 retain prior findings and were not freshly retested in CP27. Evidence index: `evidence/phase27-defect-regression-summary.json`. Coverage remains incomplete and GATE-REL-07 remains BLOCKED.

## Checkpoint 28 — A2A backend registration smoke

| Capability | Evidence-backed coverage | Status / remaining work |
| --- | --- | --- |
| Private A2A endpoint registry write | Real Chromium submitted through the registration UI; test-only interceptor supplied explicit `127.0.0.1/32`; backend returned 201 and the UI showed one Active row. | Backend registration contract exercised. Production form still omits `allowed_cidrs` (DEF-005). No task dispatch, endpoint fetch, or AgentCard discovery was tested. |
| Private endpoint request gating | Unmodified UI payload confirmed private opt-in without CIDR and was previously rejected with HTTP 400. | DEF-005 remains open; harness did not alter product code. |

Evidence: `evidence/phase28-a2a-mock-ui-registration.json` and visually inspected form/result screenshots. A2A SM-09 tasks, grant attenuation, hop boundaries, budget holds and lifecycle remain open. GATE-REL-07 remains BLOCKED.

## Checkpoints 29–31 — authorization, audit and Setup accessibility

| Area | Evidence-backed result | Status / remaining work |
| --- | --- | --- |
| Non-operator write boundary | Empty-body `POST /admin/v1/projects` as Team Admin returned 403 in bound org and generic 404 in alternate org. | One route is covered without side effects; other write endpoints and resource-ID swaps remain open. |
| Provisioning audit trail | Read-only SQLite query confirms zero org/project/environment provisioning events for retained synthetic tenants/resources. | DEF-007 reconfirmed; tamper and forward event behavior open. |
| Fresh Setup accessible names | Real Chromium shows accessible progress-button names containing their visible step wording; axe reports zero WCAG violations on the first Setup step. | DEF-001 did not reproduce in this browser state; prior Vitest failures remain unresolved. Full wizard and accessibility sweep remain open. |

Evidence: `evidence/phase29-teamadmin-write-boundary.json`, `phase30-audit-provisioning-recheck.json`, `phase31-def001-fresh-setup-labeling.json` and visually reviewed PNGs. Coverage remains incomplete; GATE-REL-07 remains BLOCKED.

## Checkpoints 32–34 — A2A key grant boundary and runtime flag

| Area | Evidence-backed result | Status / remaining work |
| --- | --- | --- |
| A2A runtime enablement | Listener without `TANPOPO_A2A_ENABLED` returned 404 `A2A plane is disabled`; separate listener with flag enabled returned ready. | Deployment configuration requirement; runtime operation paths remain open. |
| Virtual Key A2A authorization | Normal UI-issued key returned 403 `scope_or_plane_denied` to `GET /a2a/agents` on enabled listener; anonymous request returned 401. DB readback confirmed `allowed_planes=PROVIDER`; QA key revoked. | DEF-010 confirmed; supported provisioning for narrowly scoped A2A keys is absent. A2A task flows remain blocked. |
| Key issuance UX | Issue drawer exposes model and MCP controls only; no A2A plane or route/agent grants. | UX/API coverage supports DEF-010; broader key rotation/expiry/quota flows remain open. |

Evidence: `evidence/phase32-vkey-issue-form.json`, `phase33-a2a-enabled-vkey-probe.json`, `phase34-a2a-vkey-bearer-probe.json`, `phase34-vkey-a2a-scope-readback.json` and reviewed screenshots. DEF-010 handoff: `engineering-handoffs/DEF-010-a2a-virtual-key-scope.md`. GATE-REL-07 remains BLOCKED.

## Checkpoint 35 — post-mutation key list refresh

| Area | Evidence-backed result | Status / remaining work |
| --- | --- | --- |
| Access & Keys reload after revoke | Loading placeholder observed immediately; three revoked rows rendered by 300ms and stayed stable through 2500ms. | No persistent stale UI defect reproduced; delayed-network, concurrent mutation and other row actions remain open. |

Evidence: `evidence/phase35-vkey-loading-timing.json` and visually reviewed stable screenshot. GATE-REL-07 remains BLOCKED.

## Checkpoints 36–37 — Virtual Key rotation and revocation

| Capability | Evidence-backed result | Status / remaining work |
| --- | --- | --- |
| Key rotation and grace period | Real UI rotation returned 200/version 2; confirmation states 24-hour dual-valid grace; old and new bearer both returned 200 during grace. | One rotation path passed; expired-grace/restart/race behavior remains open. |
| Revocation after rotation | Revoke returned 200/REVOKED; old and replacement tokens both returned 401 `invalid_virtual_key`. | One API-driven cleanup path passed; see separate UI revoke journey below. |
| Graceful UI revoke | Real Access & Keys row action returned 200; UI showed zero streams severed and updated row to Revoked; subsequent bearer returned 401. | One no-active-stream path passed; emergency abort and active stream drain open. |

Evidence: `evidence/phase36-key-lifecycle.json`, `phase37-ui-revoke.json` and visually reviewed screenshots. GATE-REL-07 remains BLOCKED.


## Checkpoints 38–41 — fresh bootstrap and A2A live interactions

| Area | Evidence-backed result | Status / remaining work |
| --- | --- | --- |
| Fresh install / owner claim on 8083 | Bootstrap wizard submitted in Chromium from an uninitialized DB; HTTP 201; owner session and new Org/Project/dev scope reached Overview. Empty token rejected. | PASS for no-adapter skip path. Initial provider, deployment, default route, first Virtual Key, and inference remain UNVERIFIED. |
| A2A discovery on enabled 8084 | Authorized fixture key received 200 from `/.well-known/agent.json` and `/a2a/agents`; one loopback synthetic agent returned. | PASS for one Org Owner-owned project/region; other grants, tenant isolation, pagination and disabled state matrix remain open. DEF-010 still blocks supported user provisioning. |
| A2A task synchronous completion | Endpoint reached; task persisted `completed`, version 3, artifact readable by GET; terminal SSE event returned. Caller bearer header was not forwarded to endpoint. | PASS for no-cost local completion and caller-secret stripping. Budget settlement, failing endpoint, retries, downstream headers, nested delegation and restart recovery UNVERIFIED. |
| A2A async cancellation | Async task persisted submitted, then cancel and GET returned canceled. Hop 6 rejected before endpoint dispatch. | PASS for one cancellation/hop guard. Propagated cancellation to a running endpoint and budget-hold release remain UNVERIFIED. |
| A2A input-required resume | Endpoint returned input-required; task persisted and UI displayed it. Resume POST returned 404; exposed task routes offer no transition. | FAIL — DEF-011. Multi-turn tasks cannot resume through the documented API. |
| Marketplace trust review | Eight bundled provider adapters listed Official with no publisher signature; Ollama staged and permission diff reviewed for loopback/private-IP/API key/hooks. | PASS for discovery/stage/review display. Activation and provider install completion remain UNVERIFIED. |

Screenshots were visually inspected: `phase38-*`, `phase39-a2a-agent-task-ui.png`, `phase40-fresh-marketplace.png`, `phase40-ollama-stage-dialog.png`, and `phase41-a2a-input-required-console.png`. Checkpoint JSON is retained in `evidence/`. All other subsystem, role, browser, CLI, API and integration coverage remains incomplete; GATE-REL-07 remains BLOCKED.

## CP42–57 additive coverage (2026-11-23)

| Area / workflow | Result | Evidence and boundary |
| --- | --- | --- |
| Fresh Setup 8083: bootstrap, validation, owner, org/project/dev, no-adapter skip, key custody, review, completion | PASS for this one clean bootstrap journey | `phase38-*`, visually inspected at every step. Wizard intentionally skipped initial provider because no adapters were enabled. |
| Marketplace staging, permission review and activation for bundled Ollama | PASS for Org Owner path | `phase40-*`, `phase42-*`; only isolated synthetic tenant. Publisher signature displayed None/Unsigned; supply-chain provenance not assessed. |
| Private local provider connection and probe | PASS for explicit `/32` connection path | `phase43-*`; local fixture on 127.0.0.1:19089. Confirms provider form can capture CIDR, not MCP/A2A forms. |
| Ollama billing defaults | FAIL — DEF-013 | Form Commercial API/US vs manifest SELF_HOSTED_INFRA default. No incorrectly classified record was persisted; `phase43-new-connection-form.png`. |
| Logical model → deployment → route → scoped key → model list/chat → trace | PASS for one local synthetic successful request | `phase44-*`, `phase45-*`, `phase46-*`, `phase47-*`. One exact 1-micro-USD settlement; not external provider coverage. |
| FinOps headline vs project chargeback | FAIL — DEF-012 | Zero budget rows caused headline `$0.000000`; one exact settlement/chargeback row is `$0.000001`; `phase47-*`, `phase51-*`. |
| A2A discovery, sync completed task/artifact/SSE, async cancellation, hop-depth rejection | PASS for isolated authorized synthetic agent path | `phase39-*`; one org/project and one fixture. No Google; broader grant/tenant/retry/budget matrix open. |
| A2A `input-required` resumption | FAIL — DEF-011 | Persisted `input-required`; documented transition route returns 404; `phase41-*`. |
| A2A normal UI key issuance | FAIL — DEF-010 | Correct authorization rejects provider-only key with 403. Provisioning UI/API grants missing; do not weaken enforcement. |
| Fresh provisioning audit events | FAIL — DEF-007 | None for org/project/dev; 49 unrelated events exist; `phase48-fresh-bootstrap-audit.json`. |
| Failed provider probe → deployment activation | FAIL — DEF-002 | Controlled loopback HTTP 401 -> probe `valid=false/auth_failed`; UI/API still labels deployment ACTIVE_HEALTHY/Healthy; `phase52–54-*`. Historical CP5 provider request 503 preserved. Synthetic resources removed in CP56–57. |
| DEF-001 runtime setup label comparison | UNVERIFIED reconciliation | CP31 axe/runtime pass plus CP38 complete wizard; original two Vitest failures remain unresolved. |
| DEF-009 WebKit mobile webhook drawer | UNVERIFIED current checkpoint | CP49 failed before interaction; CP18 reproduced, CP27 did not. Preserve both. |
| Supported-browser/platform/API/CLI/permissions/security/operations full matrix | BLOCKED / UNVERIFIED by area | Existing evidence only covers enumerated paths; no completeness claim. |

No release gate or coverage completion is claimed. GATE-REL-07 remains BLOCKED.

## CP58–60 and defect status snapshot

| Finding | Latest status | Evidence / unresolved scope |
| --- | --- | --- |
| DEF-001 | UNVERIFIED reconciliation | CP38 completed full wizard; CP31 live axe/accessibility scan passed; original two Vitest failures still need exact test-state comparison. |
| DEF-002 | FAIL, confirmed | CP52–54 local 401 fixture -> `auth_failed` probe -> `ACTIVE_HEALTHY` deployment. Historical CP5 carries route HTTP 503. |
| DEF-003 | FAIL, rechecked CP58 | Empty fresh tenant has no UI budget create action; historical API-created budget/edit path exists. |
| DEF-004 | FAIL, rechecked CP59 | `config show` exit 0 prints local help only. |
| DEF-005 | FAIL, previously reproduced | MCP and A2A private forms omit CIDR. CP43 provider connection form accepts explicit CIDR; shared parity beyond tested surfaces remains open. |
| DEF-006 | FAIL, CP27 latest | Team Admin member form's missing project scope caused 403; not rerun after CP37. |
| DEF-007 | FAIL, rechecked CP48 | Fresh bootstrap org/project/dev have no provisioning audit events. |
| DEF-008 | FAIL, rechecked CP60 | Enabled Console backup button returns 503; no receipt added. |
| DEF-009 | UNVERIFIED current attempt | CP18 reproduced, CP27 did not; CP49 WebKit authentication/navigation returned to `/login` and did not reach the drawer; diagnostic `phase49-def009-attempt.json`. |
| DEF-010 | FAIL, preserved | Supported key provisioning still lacks A2A plane; valid 403 authorization remains enforced. |
| DEF-011 | FAIL, CP41 latest | `input-required` task cannot resume via exposed transition route. |
| DEF-012 | FAIL, confirmed | FinOps headline uses budget ledgers; zero budget rows hide a nonzero exact settlement. |
| DEF-013 | FAIL, confirmed | Ollama default form metadata conflicts with adapter manifest default. |

The defect retests above do not establish full feature, role, browser, API, CLI, platform, integration, or operational coverage. GATE-REL-07 remains BLOCKED.

| Chargeback grouped views | PASS for Project, Environment, Virtual Key, Model on one retained settlement | `phase61-chargeback-*.png`; one exact row in each grouping. |
| Chargeback export JSON | PASS for one retained settlement | `phase62-export-json.json`, downloaded file and success screenshot. |
| Chargeback export CSV via Console | FAIL — DEF-014 | Backend returns valid CSV, UI JSON parser errors and no file download; `phase61-*`. |

| DEF-001 targeted accessibility tests | FAIL, reproduced CP63 | Two original Setup Vitest cases fail axe `label-content-name-mismatch`; CP31 live runtime scan passed. State mismatch remains unresolved; `phase63-def001-targeted-vitest.*`. |

| Export matches selected group dimension | FAIL — DEF-015 | Model table key differs from successful JSON export `group_key`; export request remains project-grouped, `phase64-*`. |

| DEF-009 WebKit target retest (CP65) | UNVERIFIED | WebKit auth did not persist; screenshot shows empty sign-in form, not drawer. `phase65-*`; no layout conclusion. |
