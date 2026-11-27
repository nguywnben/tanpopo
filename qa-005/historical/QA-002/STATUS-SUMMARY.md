# QA-002 consolidated status summary

**Frozen baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`  
**Latest checkpoint:** CP119
**Release gate:** `GATE-REL-07 BLOCKED`  
**QA state:** In progress; coverage remains incomplete. QA-001 results are not inherited.


## Latest phase: CP119 — A2A CIDR correction and recovery

- **Historical defects:** DEF-001–015 retain scoped QA-002 results only; no QA-001 PASS is inherited. DEF-005 remains PARTIAL; DEF-007 fault atomicity/storage faults/independent erasure proof remain open. DEF-016–030 and DEF-032–039 remain preserved; DEF-031 Low remains confirmed despite this recovery pass.
- **Existing finding:** DEF-031 initially fails because its mobile error copy is hidden. CP119 PASS is limited to correction and recovery: valid `/32` after `/33` produced 201 and persisted. No new defect ID.
- **Coverage completed:** same A2A form recovered from `/33` to exact `/32`; actual UI success state and seven-agent list corroborated by authenticated API and a separate mobile browser context. No discovery or task request.
- **Coverage blocked/remaining:** error visibility/accessibility remains open; MCP form recovery, Firefox/WebKit, keyboard and screen reader behavior remain unverified. Broad full-product coverage remains incomplete.
- **Recommended handoffs:** add the recovery pass to DEF-031; keep the requirement for visible field-level feedback on failure. Continue DEF-005 residuals and all open defects.
- **Checkpoint:** [CP119](checkpoints/CP119-def031-cidr-correction-recovery.md). `GATE-REL-07` remains **BLOCKED**.

## Latest phase: CP118 — DEF-031 invalid CIDR error in Chromium mobile

- **Historical defects:** DEF-001–015 retain scoped QA-002 outcomes only; no QA-001 PASS is inherited. DEF-005 remains PARTIAL after CP114/116/117 security checks. DEF-007 audit success paths do not close failure atomicity, storage faults, or independent erasure proof. DEF-016–038 remain preserved/open; DEF-039 Low remains confirmed.
- **Existing finding:** DEF-031 Low independently reconfirmed in Chromium mobile A2A, extending prior WebKit CP66/67 evidence. No new ID.
- **Coverage completed:** at 375×844, actual A2A submission of loopback endpoint plus malformed `/33` returned 400 `invalid_request`; collection remained at six agents with no attempted row. Screenshot inspection shows the red validation message behind the drawer, with only a clipped icon visible. No endpoint call.
- **Coverage blocked/remaining:** MCP mobile parity, correction/retry, keyboard/screen-reader announcement and desktop behavior remain open. Firefox/WebKit current-image runs remain blocked by CP111. Broad full-product and historical retests remain incomplete.
- **Recommended handoffs:** update DEF-031 handoff with CP118; display validation beside the CIDR field or above the drawer and announce it accessibly, preserving server-side rejection. Continue DEF-005 residuals, DEF-038/007 and all outstanding defects.
- **Checkpoint:** [CP118](checkpoints/CP118-def031-chromium-mobile-a2a.md). `GATE-REL-07` remains **BLOCKED**.

## Latest phase: CP117 — MCP cross-CIDR redirects (301/308)

- **Historical defects:** DEF-001–015 retain limited scoped QA-002 outcomes only; no QA-001 PASS is inherited. DEF-005 remains PARTIAL despite scoped CP114/116/117 security checks. DEF-007 successful audit paths still do not cover failure atomicity, storage faults, or independent erasure proof. DEF-016–038 remain preserved/open; DEF-039 Low remains confirmed.
- **New defects:** none. Two live redirect attempts to a disallowed peer failed closed before target contact.
- **Coverage completed:** real Chromium owner registered an MCP origin at exact allowed `/32`, then actual Sync calls encountered 301 and 308 responses to a live disallowed loopback target. Both returned 502; origin logged one request per attempt; target logged zero. Persisted state and visually inspected screenshots corroborate the result.
- **Coverage blocked/remaining:** redirect support/per-hop validation contract, HTTPS downgrade and relative/loop redirects, mixed DNS answers, resolver/connection races, A2A redirect cases, other runtimes/transports and full product/role/platform coverage remain open. Firefox/WebKit current-image runs remain blocked (CP111).
- **Recommended handoffs:** none new. Preserve cross-CIDR fail-closed enforcement and clarify redirect semantics. Continue DEF-005 residuals, DEF-038/007, DEF-023/035/039, and all remaining defect retests.
- **Checkpoint:** [CP117](checkpoints/CP117-mcp-redirect-301-308-boundary.md). `GATE-REL-07` remains **BLOCKED**.

## Latest phase: CP116 — A2A private endpoint without explicit authorization

- **Historical defects:** DEF-001–015 retain scoped QA-002 outcomes only; no QA-001 PASS is inherited. DEF-005 remains PARTIAL; CP114 scoped peer enforcement across rebinding and CP116 A2A default-deny improve evidence but do not close mixed-answer, race, redirect, transport, or runtime gaps. DEF-007 fault atomicity, injected storage failures, and independent erasure proof remain incomplete. DEF-016–038 remain preserved/open; DEF-039 Low remains confirmed.
- **New defects:** none. A loopback endpoint submitted with the private exception unchecked was rejected before persistence or endpoint contact.
- **Coverage completed:** real Chromium A2A registration UI with explicit exception unchecked; HTTP 400 `invalid_endpoint_url`; post-submit list confirmed no row; screenshot inspection confirmed UI feedback. CP08/68 remain separate scoped positive controls for explicit allowlisting.
- **Coverage blocked/remaining:** full DEF-005 remains partial; mixed DNS, resolver/connection races, redirect modes, other transports/runtimes, and broad A2A roles/lifecycles are open. Firefox/WebKit current-image runs remain blocked by CP111; broad product coverage remains incomplete.
- **Recommended handoffs:** no new handoff. Preserve explicit private-endpoint authorization. Continue DEF-038/007, workspace defects DEF-023/035/039, and remaining historical/new defect retests.
- **Checkpoint:** [CP116](checkpoints/CP116-a2a-private-endpoint-default-deny.md). `GATE-REL-07` remains **BLOCKED**.

## Latest phase: CP115 — MCP inventory visibility across project scopes

- **Historical defects:** DEF-001–015 retain only scoped QA-002 outcomes; no QA-001 PASS is inherited. DEF-005 remains PARTIAL after CP114's scoped exact-peer DNS rebind test. DEF-007 successful event/chain and populated crypto-shred paths do not close failure atomicity, storage-fault, or independent backup-erasure gaps. DEF-016–038 remain preserved/open with prior dispositions. DEF-039 Low remains confirmed.
- **New defects:** none. CP115 independently reconfirms QA-OBS-008: an Org Owner's organization-wide MCP list includes rows owned by a different project, while the UI omits each row's project. Owner has cross-project authority, so no authorization defect is inferred. Team Admin behavior remains unverified.
- **Coverage completed:** real Chromium owner navigation between organization-only and a separate project with no environment; authenticated MCP list responses and project IDs mapped to names; visual inspection of both rows and available actions. No sync/register/edit/delete action or data mutation.
- **Coverage blocked/remaining:** the organization-wide versus selected-project MCP inventory contract is unresolved. Team Admin cross-project list/action authorization remains unverified. Existing Firefox/WebKit current-image execution blockers and broad product/role/platform gaps remain.
- **Recommended handoffs:** clarify and document MCP registry scope. If management is organization-wide, expose each server's project in the list and preserve project-aware action authorization. Continue DEF-005, high-impact DEF-038/007, scope defects DEF-023/035/039, and outstanding retests.
- **Checkpoint:** [CP115](checkpoints/CP115-mcp-workspace-scope-owner.md). `GATE-REL-07` remains **BLOCKED**.

## Latest phase: CP114 — DNS rebinding against explicit exact CIDR

- **Historical defects:** DEF-001–015 retain scoped QA-002 results only; no QA-001 PASS is inherited. DEF-005 remains PARTIAL after this scoped DNS test. DEF-007 successful event/chain and populated shred paths still do not close failure atomicity, injected storage faults, or independent backup-erasure proof. DEF-016–038 remain preserved/open with prior dispositions; DEF-039 Low remains confirmed from CP113.
- **New defects:** none. CP114 did not relax SSRF or authorization behavior and found no bypass.
- **Coverage completed:** real Chromium/Owner registration and eight UI discovery attempts for a hostname rotating between explicitly allowed `127.0.0.1` and disallowed `127.0.0.2`; one sync returned 200 and seven returned 502. The allowed fixture logged nine protocol requests; the disallowed fixture logged zero. Registration persistence and success/error UI states were visually inspected and corroborated by sanitized DNS/API/fixture records.
- **Coverage blocked/remaining:** single mixed-answer DNS responses, DNS resolution-to-connect races, redirect status/HTTPS variants, other transports, A2A rebinding, additional runtimes, roles, and broad feature/platform coverage remain unverified. Firefox/WebKit current-image runs remain blocked (CP111).
- **Recommended handoffs:** none new from CP114. Keep DEF-005 partial and preserve exact peer authorization. Continue DEF-038/007, scope defects DEF-023/035/039, and the rest of the confirmed register.
- **Checkpoint:** [CP114](checkpoints/CP114-dns-rebinding-exact-cidr.md). `GATE-REL-07` remains **BLOCKED**.

## Latest phase: CP113 — Project policy access across workspace scopes

- **Historical defects:** DEF-016–038 remain preserved/open; CP113 changes none of their dispositions. DEF-001–015 retain scoped QA-002 results only; DEF-005 remains PARTIAL and DEF-007 fault atomicity/erasure proof remain incomplete. No QA-001 PASS is inherited.
- **New defect:** DEF-039 Low confirmed: project-only/no-environment Policies UI is blocked by an environment prerequisite although the project effective-policy API returns 200 and Project is a supported edit tier. No write, authorization bypass, or policy relaxation occurred.
- **Coverage completed:** real Chromium owner journey through organization-only, project-only, and project/environment policy scopes; effective API/UI comparison; Project editor selection; navigation; hard refresh; visually inspected screenshots. Hard refresh returned to runtime-default project/no-env, recurring QA-OBS-009.
- **Coverage blocked/remaining:** Team Admin project-policy authorization is UNVERIFIED because no usable Team Admin session is available. Firefox/WebKit current-image execution remains blocked (CP111). Full independent DEF-001–015 acceptance remains incomplete; DEF-005 DNS rebinding/mixed answers and other SSRF residuals remain open; DEF-007 failure atomicity and erasure proof remain open; DEF-016–039 retesting and broad feature/platform coverage remain incomplete.
- **Recommended handoffs:** add DEF-039 to the workspace-scope engineering handoff with DEF-023/024/029/030/035. Prioritize DEF-038/007 for backup integrity, audit atomicity, and verifiable erasure. Preserve SSRF, RBAC, and monotonic policy boundaries.
- **Checkpoint:** [CP113](checkpoints/CP113-project-policy-scope-and-console-state.md); [DEF-039](reports/DEF-039-project-policy-ui-requires-an-environment.md). `GATE-REL-07` remains **BLOCKED**.

## Latest phase: CP112 — Google provider probe and Cloud egress-path retest

- **Historical defects:** DEF-016–038 remain preserved/open; no disposition changed. DEF-001–015 retain scoped QA-002 outcomes only; DEF-005 partial, DEF-007 fault atomicity/erasure gaps remain. No QA-001 PASS is inherited.
- **New defects:** none. `upstream_unreachable` remains QA-OBS-017/environmental; historical `probe_runtime_unavailable` remains UNVERIFIED on RC4.
- **Coverage completed:** real Chromium probe on the first of two duplicate Google rows returned HTTP 200/`valid=false`/`upstream_unreachable`, 2068 ms and left it Unverified. Forced direct no-proxy request timed out before TCP/TLS; the configured proxy reached the same Google model-list endpoint with an environment-bound QA key (HTTP 200, one model). The frozen Gemini probe source uses that endpoint and host-blind header injection. Screenshot visually inspected. Stored connection key equality to the environment key was not established.
- **Coverage blocked/remaining:** guarded app-to-Google direct egress remains blocked in this managed Cloud path; no inference sent. Need a runtime with supported direct egress for app probe/inference and a matching-config three-runtime comparison; probe_runtime_unavailable remains untested on RC4. Do not bypass SSRF checks or ambient-proxy restrictions.
- **Recommended handoffs:** no new product handoff; retain QA-OBS-017 as infrastructure. Continue DEF-038/007 and existing product defects, including DEF-032 duplicate identity.
- **Checkpoint:** [CP112](checkpoints/CP112-google-provider-egress-path-retest.md). `GATE-REL-07` remains **BLOCKED**.

## Latest phase: CP111 — Cross-browser execution environment diagnostics

- **Historical defects:** no product finding disposition changed; DEF-016–038 remain preserved/open. CP110 remains the latest product defect retest. No QA-001 PASS is inherited.
- **New defects:** none. Firefox/WebKit launch failures are managed-image blockers, not Tanpopo defects.
- **Coverage completed:** Chromium remains usable. Firefox 153 was downloaded and its Playwright launch retried; `uid_map: EROFS` and software framebuffer mapping errors persist. WebKit downloaded but lacks GTK4/graphics/input libraries; the approved dependency installer could not switch to root (`su: Authentication failure`). Product `/healthz` on 18084 stayed 200.
- **Coverage blocked/remaining:** no Firefox or current-image WebKit product flow could run in CP111. Existing scoped WebKit evidence from earlier checkpoints is preserved. Need browser support provisioning; meanwhile continue independent Chromium, API and CLI QA. Cross-browser/platform coverage remains incomplete.
- **Recommended engineering handoffs:** unchanged; continue DEF-038/007 and open product-defect priorities. Environment owner needs Firefox user-namespace/compositor support and the listed WebKit libraries before these browser phases can resume.
- **Checkpoint:** [CP111](checkpoints/CP111-browser-runtime-availability.md). `GATE-REL-07` remains **BLOCKED**.

## Latest phase: CP110 — Local HUMAN member enrollment on a second runtime

- **Historical DEF-001–015:** preserve scoped QA-002 outcomes only; DEF-005 remains PARTIAL; DEF-007 gained a CP110 successful member.created event/548-event chain check; this does not close injected-fault atomicity or independent erasure-proof gaps. No QA-001 PASS is inherited.
- **Existing findings:** DEF-016–038 remain preserved/open. CP110 independently reconfirmed DEF-021 Moderate on 18084, and DEF-023 Moderate through same-tenant UI/API scope comparison. No finding was removed, downgraded, or newly added.
- **Coverage completed:** real owner created a synthetic HUMAN Developer; create returned 201 and “Member added.”, explicit-organization API read showed the persisted record, clean Chromium login returned 401 with no enrollment/reset action, and changing to project+environment showed five rows. The new `member.created` event appeared at seq 548 and the UI verified all 548 chain events intact. Screenshots visually inspected.
- **Coverage blocked/remaining:** the supported local-human identity enrollment contract remains unresolved; without an enrollment route, successful new-member login and post-enrollment RBAC cannot be tested. Synthetic account retained because no UI or API member-delete route is documented. All other incomplete product/defect/browser/platform work continues.
- **Recommended handoffs:** add CP110 evidence to DEF-021; pair its scope comparison with DEF-023. Prioritize DEF-038 alongside DEF-007 and preserve other existing handoffs; do not weaken SSRF or authorization boundaries.
- **Checkpoint:** [CP110](checkpoints/CP110-local-member-enrollment-independent-runtime.md); [DEF-021](reports/DEF-021-member-added-without-supported-local-enrollment.md). `GATE-REL-07` remains **BLOCKED**.

## Latest phase: CP109 — Extension permission review and staged-candidate cleanup

- **Historical defects verified fixed / scoped:** DEF-001–004, DEF-006, DEF-008–015 retain only their scoped QA-002 outcomes; DEF-005 remains PARTIAL; DEF-007 has successful populated shred paths but fault atomicity and physical/backup erasure proof remain UNVERIFIED. No QA-001 PASS is inherited.
- **Historical defects failing/unverified:** DEF-016–037 remain preserved/open; DEF-023 was reconfirmed in CP107. DEF-038 High remains confirmed from CP108. No prior status changed.
- **New defects:** none.
- **Coverage completed:** real Chromium staged an uninstalled provider adapter, inspected the persisted Pending Permission Approval state and permission delta, confirmed unchecked consent disables activation, and uninstalled the candidate with HTTP 204 and post-refresh count returning to two. Screenshots visually inspected. No credentials, activation or provider call.
- **Coverage blocked/remaining:** role-separated approval, signing, runtime installation/isolation, secret broker, private egress behavior, upgrade/rollback/quarantine and lockfile/restart behavior remain UNVERIFIED. No private-host permission was approved in this test.
- **Recommended engineering handoffs:** unchanged; prioritize DEF-038 with DEF-007 and continue DEF-023/024/029/030/035, DEF-034/033/005 and DEF-037.
- **Checkpoint:** [CP109](checkpoints/CP109-extension-permission-review-and-candidate-cleanup.md). `GATE-REL-07` remains **BLOCKED**.

## Latest phase: CP108 — Backup verification after organization crypto-shred

- **Historical defects verified fixed / still scoped:** DEF-001–004, DEF-006 and DEF-008–015 retain only their scoped QA-002 retests; DEF-005 remains PARTIAL. DEF-007 has a populated successful crypto-shred path, while injected-fault atomicity and physical/backup erasure proof remain UNVERIFIED. No QA-001 PASS is inherited.
- **Historical defects failing/unverified:** DEF-016–037 remain preserved/open; DEF-023 was reconfirmed in CP107. CP108 adds DEF-038 High. No earlier finding was removed or downgraded.
- **New defect:** DEF-038 High confirmed. Backup creation succeeds on the actual 18084 data, but verify and restore both reject an intact retained audit partition for a successfully crypto-shredded organization. A separate clean isolated database verified as a scoped positive control. No archive, database, key or token is retained.
- **Coverage completed:** correct 18084 runtime/data/KEK matching; encrypted backup creation; same-archive verify and restore failure; absent destination after restore failure; CP85 retained-chain reconciliation; source validation-path inspection; separate clean database positive control.
- **Coverage blocked/remaining:** the defect blocks post-shred backup verification and restore. Corrupt/wrong-key archives, later restore atomicity, pre-shred archive semantics, cluster/PostgreSQL/Valkey, all remaining defect acceptance and broad product/browser/platform coverage remain incomplete. Earlier incorrect DB/key attempts were setup errors and excluded.
- **Recommended engineering handoffs:** prioritize DEF-038 jointly with DEF-007, preserving authenticated deleted-tenant history and tamper detection. Retain handoffs for DEF-023/024/029/030/035, DEF-034/033/005 and DEF-037.
- **Checkpoint:** [CP108](checkpoints/CP108-backup-after-organization-crypto-shred.md); [DEF-038](reports/DEF-038-backup-restore-rejects-deleted-organization-audit-partition.md). `GATE-REL-07` remains **BLOCKED**.

## Latest phase: CP107 — Populated organization crypto-shred and DEF-023 retest

- **Historical defects verified fixed:** DEF-001–004, DEF-006 and DEF-008–015 retain only their scoped QA-002 outcomes; DEF-005 remains PARTIAL. No QA-001 PASS is inherited.
- **Historical defects failing/unverified:** DEF-007 gained a populated successful crypto-shred PASS, but failure atomicity, injected storage/audit faults and independent ciphertext/backup erasure proof remain UNVERIFIED. DEF-016–036 remain open; DEF-023 reconfirmed. DEF-037 Low remains confirmed. No other disposition changed.
- **New defects:** none. The 400 audit response was an invalid initial test query and passed after explicitly setting the tenant/partition.
- **Coverage completed:** new organization → owner member → project/environment → active scoped key → guarded crypto-shred; one active key revoked, resource reads empty after deletion, six lifecycle events and intact chain. DEF-023 false-empty reproduced and contrasted with environment-selected populated state; screenshots visually inspected.
- **Coverage blocked / remaining:** failure injection, ciphertext/backup erasure proof, active-session cancellation timing, secret/webhook resources, four-eyes policy, other roles/platforms and broader product surface remain open.
- **Recommended engineering handoffs:** DEF-023 workspace-scope fix; DEF-007 audit atomicity and verifiable populated erasure; DEF-037 event vocabulary. Preserve SSRF/authorization boundaries.
- **Checkpoint:** [CP107](checkpoints/CP107-populated-org-crypto-shred-and-scope-consistency.md). `GATE-REL-07` remains **BLOCKED**.

## Latest phase: CP106 — Audit Log pagination and event detail

- **Historical defects verified fixed:** DEF-001–004, DEF-006 and DEF-008–015 retain only their scoped QA-002 outcomes; DEF-005 remains PARTIAL. No QA-001 PASS is inherited.
- **Historical defects failing/unverified:** DEF-007 remains partial; successful event presence/chain integrity does not cover failure atomicity, populated erasure, storage faults or cross-tenant isolation. DEF-016–036 remain open. DEF-037 Low remains confirmed from CP105. No defect disposition changed in CP106.
- **New defects:** none.
- **Coverage completed:** real Chromium Audit Log pagination loaded 100 older events (continuous 512→313 sequence), and event detail showed actor/target/hash/metadata without the one-time key secret. Screenshots visually inspected.
- **Coverage blocked / remaining:** combined action-filter/cursor behavior, concurrent inserts during pagination, deep history exhaustion, other roles/tenants/browsers, audit fault injection and populated erasure remain open.
- **Recommended engineering handoffs:** resolve DEF-037 action vocabulary; continue DEF-007 atomicity/erasure investigation.
- **Checkpoint:** [CP106](checkpoints/CP106-audit-log-pagination-and-detail.md). `GATE-REL-07` remains **BLOCKED**.

## Latest phase: CP105 — Virtual Key audit event-name contract

- **Historical defects verified fixed:** DEF-001–004, DEF-006, and DEF-008–015 retain their limited scoped QA-002 results; CP104 expanded DEF-011. DEF-005 remains PARTIAL. No QA-001 PASS is inherited.
- **Historical defects failing/unverified:** DEF-007 remains partial: CP105 confirms issue/revoke success events and an intact chain, but event append failure atomicity, populated-organization erasure and storage-fault behavior remain UNVERIFIED. DEF-016–036 remain open; CP105 changes no disposition for them. DEF-023 was independently reproduced in CP101.
- **New defects:** DEF-037 Low confirmed: published `virtual_key.issued` action mismatches emitted/filterable `virtual_key.created`; exact documented filter returns zero although the event exists under the actual name. No missing record, secret exposure or chain failure was observed.
- **Coverage completed:** actual Console key issue/revoke events; UI/API exact-action filters; issue metadata and 511-event chain verification; screenshots visually inspected.
- **Coverage blocked / remaining:** audit failure injection, atomicity, populated erasure, cross-tenant isolation and downstream action-name consumers remain untested. Full role/browser/platform and product coverage remains incomplete.
- **Recommended engineering handoffs:** resolve DEF-037 canonical event vocabulary; retain DEF-007 append-failure/erasure handoff; continue DEF-005 without weakening SSRF or authorization boundaries.
- **Checkpoint:** [CP105](checkpoints/CP105-audit-contract-virtual-key-event-name.md); [DEF-037](reports/DEF-037-virtual-key-audit-event-name-contract.md). `GATE-REL-07` remains **BLOCKED**.

## Latest phase: CP102 — Google runtime inventory comparison

- **Historical defects verified fixed:** DEF-001–004, DEF-006, and DEF-008–015 retain only their scoped QA-002 outcomes; DEF-005 remains partial and DEF-007 has unverified failure atomicity/populated erasure. No QA-001 PASS is inherited.
- **Historical defects failing/unverified:** DEF-016–036 remain open; CP101 most recently reconfirmed DEF-023. CP102 changed no defect disposition.
- **New defects:** none.
- **Coverage completed:** authenticated real-Chromium read-only provider inventory on 18083/18084/18086; actual 18085 retained-credential failure captured; screenshots visually inspected. 18084 alone has Google configured (two duplicate rows); 18083 has no provider setup; 18086 has only synthetic Ollama.
- **Coverage blocked:** matching Google probe comparison is unavailable because configurations differ and 18085 login is blocked. `upstream_unreachable` remains supported as Cloud direct-egress incompatibility; `probe_runtime_unavailable` remains UNVERIFIED on RC4. Google app inference remains unverified.
- **Coverage remaining:** restore authorized 18085 access or use a suitable isolated runtime, configure matching Google adapter/connection in the test instances, and exercise an identical probe only where supported egress permits. Continue all other QA in parallel.
- **Recommended engineering handoffs:** unchanged. Continue DEF-005 without weakening SSRF, and prioritize DEF-023/029/030/035 workspace consistency, DEF-034 revocation, DEF-033 budget semantics, and DEF-007 audit atomicity.
- **Checkpoint:** [CP102](checkpoints/CP102-google-provider-runtime-inventory.md); evidence links are listed there. `GATE-REL-07` remains **BLOCKED**.


## Latest phase: CP103 — alternate-runtime private CIDR controls

- **Historical defects verified fixed:** DEF-001–004, DEF-006 and DEF-008–015 remain scoped QA-002 results only; DEF-005 remains PARTIAL, DEF-007 failure atomicity/populated erasure remain UNVERIFIED. No QA-001 PASS is inherited.
- **Historical defects failing/unverified:** DEF-016–036 remain open; DEF-023 was reconfirmed in CP101. No historical defect status otherwise changed.
- **New defects:** none.
- **Coverage completed:** on isolated runtime 18086, actual Chromium registration and synchronization of the same loopback fixture with an out-of-range `/32` and a matching `/32`; the first remained at zero fixture contacts/zero tools, the matching control contacted the fixture and synced one tool. Both records persisted after refresh. Screenshots visually inspected.
- **Coverage blocked / remaining:** DNS rebinding, mixed DNS answers, redirect chains/other statuses, resolver races, additional transports/hosts, A2A alternate-runtime CIDR pair, roles, browsers and platforms remain open. `probe_runtime_unavailable` remains UNVERIFIED on RC4; QA-OBS-017 retains Cloud egress classification.
- **Recommended engineering handoffs:** preserve explicit CIDR enforcement. QA-OBS-004’s generic failed-sync message may be reviewed for actionable diagnosis without exposing unsafe details. Continue current high-impact defect priorities in the register.
- **Checkpoint:** [CP103](checkpoints/CP103-alternate-runtime-private-cidr-positive-and-negative-control.md). `GATE-REL-07` remains **BLOCKED**.


## Latest phase: CP104 — A2A terminal task transition guards

- **Historical defects verified fixed:** DEF-001–004, DEF-006 and DEF-008–015 retain scoped QA-002 results; CP104 expands DEF-011. DEF-005 remains PARTIAL; DEF-007 failure atomicity and populated erasure remain UNVERIFIED. No QA-001 PASS is inherited.
- **Historical defects failing/unverified:** DEF-016–036 remain open; CP101 most recently reconfirmed DEF-023. No status change to open findings.
- **New defects:** none. QA-OBS-009 recurred after a full-page route navigation returned to the runtime-default No env project; key issuance was correctly disabled until the authorized project/environment was reselected.
- **Coverage completed:** fresh A2A-only issuance; input-required task creation and Console resume; terminal duplicate resume/cancel returned 409 with unchanged completed/version/cost state; no extra endpoint side effect; key revoke and replay denial; new owner session confirmed persistence and no terminal controls. Real screenshots were inspected.
- **Coverage blocked / remaining:** concurrent/in-flight task cancellation and revocation, timeout, endpoint/runtime-failure recovery, multihop, cross-region, non-owner roles, other browsers/platforms, and disabled-runtime UX remain open.
- **Recommended engineering handoffs:** no new DEF-011 handoff from this phase. Keep terminal guards and A2A-only authorization boundaries intact.
- **Checkpoint:** [CP104](checkpoints/CP104-a2a-terminal-task-transition-guards.md). `GATE-REL-07` remains **BLOCKED**.

## Historical defects DEF-001–015

| Status | IDs | Notes |
| --- | --- | --- |
| Verified fixed for scoped post-baseline scenarios | DEF-001–004, DEF-006, DEF-008–015 | Each passes only the specific QA-002 retest scenarios recorded in [DEFECT-RETEST-REGISTER.md](DEFECT-RETEST-REGISTER.md). This is not a full role/platform regression. |
| Partial pass; residual security/support coverage open | DEF-005 | Explicit private-IP/CIDR controls passed for executed cases; CP68 completed one enabled-runtime private A2A register/discover/task journey. CP66/67 confirm mobile invalid-CIDR errors hidden behind both drawers while APIs still reject safely. Redirects were not followed; DNS rebinding, mixed answers, alternate runtimes, and IPv4-mapped endpoint support remain unresolved. See QA-OBS-004/007 and CP25/40–42. |
| Successful paths include populated organization crypto-shred; fault atomicity and proof residuals open | DEF-007 | CP06/16/18/46/79/85/105/107 verified successful audit/resource/create/shred paths and intact chains. CP107 deleted a populated synthetic organization containing a member, project, environment and active key; one active key was revoked and six events verified intact. Injected failures, audit/storage atomicity and ciphertext/backup erasure proof remain UNVERIFIED; do not treat historical missing-event evidence as erased. |

## Confirmed open defects

| ID | Severity | Current summary |
| --- | --- | --- |
| DEF-016 | Moderate | Team Admin member creation succeeds, but unscoped member-list refresh is forbidden and UI reports failure/empty list. |
| DEF-017 | Moderate | Playground polls a request ID where trace lookup expects a trace ID; success card remains pending. |
| DEF-018 | Moderate | FinOps/API report Virtual Key lifetime budgets as monthly. |
| DEF-019 | Moderate | Playground hides canonical budget rejection reason; CP51 extends the generic-429 reproduction to organization enforcement on a child project/environment request. |
| DEF-020 | Moderate | Security shell badges remain stale after clearing switches or deciding approvals until reload. |
| DEF-021 | Moderate | Local HUMAN member creation has no supported enrollment path for first login. |
| DEF-022 | Moderate | Session UI retains revoked role/navigation/actions until reload after server rejects the session. |
| DEF-023 | Moderate | Organization member list is falsely empty without both project and environment selected; CP71/80/100/101 and CP107 independently reconfirmed UI 0 versus persisted/API member state, including after a successful new-org Add member. |
| DEF-024 | Moderate | Project FinOps view hides an applicable persisted organization ceiling and displays a false empty state. |
| DEF-025 | Low | Models enables Add deployment without environment scope; its form cannot select an environment and reports a page-level validation after submission. |
| DEF-026 | Low | MCP sync banner reports zero schema changes requiring approval while a first-discovered schema is visibly locked pending review. |
| DEF-027 | Moderate | Routes & Policies cannot distinguish same-model deployments with identical region/health labels; provider and deployment identity are hidden from target choices and route summaries. |
| DEF-028 | Moderate | Route Explain hardcodes `openai_compatible` and rejects healthy Ollama deployments as dialect-mismatched even when the same route completes a real Playground request. |
| DEF-029 | Low | Access & Keys can repopulate prior environment-scoped Virtual Key metadata after a delayed response arrives under No project / No env scope. |
| DEF-030 | Moderate | Routes & Policies can repopulate prior environment routes after a delayed response under No project / No env or a different selected project/environment; stale Edit/Delete remain available. |
| DEF-031 | Low | Invalid MCP/A2A CIDR reaches the API, and the 400 validation error is obscured behind the open drawer on WebKit mobile. APIs reject before persistence; no security bypass. |
| DEF-032 | Low | Distinct same-project provider records can be indistinguishable in Providers table, actions and edit forms when display fields duplicate. CP76 did not probe or mutate either record. |
| DEF-033 | Moderate | FinOps Create budget can update an existing same-scope ceiling once while reporting Created, then submit fixed revision v1 and fail 409 at v2. CP78 restored the tested cap. |
| DEF-034 | Moderate | Webhook subscription cannot be disabled or deleted through the Console or Admin API; active external destinations cannot be revoked through a supported product workflow. |
| DEF-035 | Moderate | Models shows sibling-project environment deployments with Disable/Delete actions while organization-only or an empty project is selected, without identifying deployment ownership. No unauthorized access or mutation was observed. |
| DEF-036 | Low | Extensions Marketplace offers Stage for review for an already active Google Gemini package; the 409 duplicate rejection is rendered as raw storage/unique-constraint text with no direct upgrade recovery action. No persistent change. |
| DEF-037 | Low | Virtual Key issue audit action is documented as `virtual_key.issued`, but actual UI/API events use `virtual_key.created`; exact documented filter returns zero while the event exists under the emitted name. Issue/revoke records and 511-event chain verified. |
| DEF-038 | High | Backup verify and restore reject valid retained audit history for an organization after successful crypto-shred; see [standalone report](reports/DEF-038-backup-restore-rejects-deleted-organization-audit-partition.md). |
| DEF-039 | Low | Project-only/no-environment Policies UI blocks project policy review/edit despite a successful project effective-policy API response; see [standalone report](reports/DEF-039-project-policy-ui-requires-an-environment.md). |

All details, reproduction steps, expected behavior, evidence and acceptance criteria are in the linked [defect reports](reports/). DEF-029 is a distinct delayed-response issue from DEF-023’s false empty organization member list. DEF-030 confirms the same general scope-race class in Routes & Policies, where stale routes expose Edit/Delete and enabled Save. DEF-031 concerns invalid-CIDR form feedback in both MCP and A2A and does not weaken or bypass API enforcement. CP47 adds a scoped loopback positive control and confirms Team Admin cross-project sync denial before fixture contact; DEF-026 captures the inconsistent first-review notice.

## Historical phase: CP86

- **Historical defects verified fixed:** DEF-001–004, DEF-006, DEF-008–015 retain only their scoped QA-002 results; CP82 independently revalidated DEF-014/015 over four groupings. DEF-005 remains partial. No QA-001 PASS is inherited.
- **Historical defects failing / unverified:** DEF-016–034 remain confirmed open. DEF-023 was reconfirmed in CP80. DEF-007 has a fresh successful empty-organization crypto-shred/audit-chain path, while its failure atomicity and populated-resource acceptance remain unverified. DEF-034 is newly confirmed; actual delivery remains environment-blocked rather than a product defect.
- **New findings:** DEF-034 Moderate confirmed: webhook subscriptions have no UI revoke action and authenticated DELETE returns 404. QA-OBS-015 records the direct-egress limitation. DEF-020 also recurred after clear; no new ID for that recurrence. Existing QA-OBS-001–014 remain preserved.
- **Coverage completed:** CP79 project provisioning audit and 315-event chain; CP80 DEF-023 comparison and keyboard tabs; CP81 Overview windows; CP82 CSV/JSON group exports; CP83/84 compatible and blocked-incompatible upgrade preflights; CP85 disposable empty-organization create/shred and intact deleted-org chain; CP86 webhook registration, one synthetic kill-switch event/outbox, switch clear, secret cleanup, and authenticated subscription-removal failure. Browser screenshots were visually inspected.
- **Coverage blocked:** CP86 direct webhook delivery/signature verification is BLOCKED by managed Cloud direct DNS/egress: direct no-proxy HTTPS failed during DNS, while the managed shell proxy returned HTTP 200; Tanpopo’s guarded transport intentionally bypasses ambient proxy. Runtime 18445 A2A remains disabled by configuration; RC4 artifact verification remains UNVERIFIED because the artifact is in a separate unavailable DevBox.
- **Coverage remaining:** independent DEF-001–015 acceptance retests remain incomplete overall; DEF-005 DNS rebinding/mixed answers/redirect-chain/alternate runtimes; DEF-007 failure atomicity, populated organization and storage-fault paths; DEF-034 revoke/disable behavior after engineering support; actual upgrade/ABORTED_SAFE and cross-role denial; webhook delivery/HMAC in direct-egress-capable infrastructure; provider outage/recovery for QA-OBS-003; FinOps filters/pagination/roles/locales; and remaining product, browser, platform, integration, accessibility, and operations coverage. PASS/FAIL/BLOCKED/UNVERIFIED remain distinct in the matrix.
- **Recommended handoffs:** prioritize DEF-034 subscription revocation; keep DEF-016/023/029/030 state behavior coupled with authorization acceptance; DEF-033 budget create/update semantics; DEF-005 SSRF acceptance without weakening controls; DEF-007 audit atomicity/populated erasure; and actual upgrade safe-abort behavior.
- **Checkpoint:** [CP86](checkpoints/CP86-webhook-delivery-and-subscription-lifecycle.md), [CP85](checkpoints/CP85-operations-org-crypto-shred.md), [CP84](checkpoints/CP84-operations-incompatible-preflight.md).

## Historical phase: CP84

- **Historical defects verified fixed:** DEF-001–004, DEF-006–015 retain only their individually scoped QA-002 results. CP82 independently retested DEF-014/015 across four supported grouping dimensions. DEF-005 remains PARTIAL. No QA-001 PASS is inherited.
- **Historical defects failing / unverified:** DEF-016–033 remain confirmed open as listed above; broad acceptance remains incomplete. DEF-023 was independently reconfirmed in CP80. DEF-007 failure injection and organization mutation/delete/storage-fault/cross-tenant paths remain UNVERIFIED.
- **New findings:** no new defect ID in CP79–84. Confirmed new defects remain DEF-016–033 with severity in their register/reports. QA-OBS-001–014 remain preserved, including unresolved QA-OBS-003.
- **Coverage completed:** CP79 provisioning audit success path/315-event chain; CP80 DEF-023 false empty/API comparison and keyboard tabs; CP81 Overview windows and alert contract; CP82 four FinOps export groupings in CSV/JSON; CP83 compatible upgrade preflight; CP84 incompatible upgrade blocking response with detailed extension/range/reason and SYSTEM audit attribution. Screenshots were visually inspected.
- **Coverage blocked:** runtime 18445 A2A remains configuration-disabled. Webhook delivery requires usable Operator access plus controlled receiver/signing setup. RC4 artifact verification remains UNVERIFIED because the separate DevBox artifact is unavailable in this workspace.
- **Coverage remaining:** independent DEF-001–015 acceptance retests remain incomplete overall; DEF-005 DNS rebinding/mixed answers/redirect-chain/alternate runtimes; DEF-007 failure/mutation paths; actual upgrade/ABORTED_SAFE execution and non-Operator denial; controlled provider outage/recovery for QA-OBS-003; FinOps date filters/pagination/roles/locales; other Console workflows, integrations, browsers, viewports, accessibility, and operations/platform scenarios. The matrix preserves PASS, FAIL, BLOCKED and UNVERIFIED separately.
- **Recommended handoffs:** keep workspace state defects (DEF-016/023/029/030) coupled to explicit authorization acceptance; prioritize DEF-033 FinOps create/update semantics, DEF-005 SSRF contract without weakening enforcement, DEF-007 audit atomicity, and a real upgrade safe-abort lifecycle.
- **Checkpoint:** [CP84](checkpoints/CP84-operations-incompatible-preflight.md), [CP83](checkpoints/CP83-operations-upgrade-preflight.md), [CP82](checkpoints/CP82-finops-export-grouping-regression.md).

## Open observations

- QA-OBS-001: A2A graceful key revocation during a dispatched task; contract unresolved.
- QA-OBS-003: overview presents “All systems nominal” despite substantial request failures; CP53 recurred at 4 requests / 25% error / zero alerts.
- QA-OBS-004: MCP CIDR sync failure uses generic retryable diagnostics.
- QA-OBS-005: budget activation/backfill semantics.
- QA-OBS-006: trace and settlement identifiers differ; correlation contract unresolved.
- QA-OBS-007: IPv4-mapped private endpoint notation rejected; supported endpoint contract unresolved.
- QA-OBS-008: MCP Hub org-wide list shown without project labels; CP115 confirmed the same project-owned rows and Sync actions remain visible under an unrelated selected project. Global vs selected-project presentation and Team Admin action contract remain unresolved; no Owner bypass inferred.
- QA-OBS-009: manual project/environment selection returns to the runtime-selected default after refresh; workspace persistence contract unresolved.
- QA-OBS-010: A2A agent registration and A2A-only key provisioning remain available in a runtime whose A2A data-plane handlers return the configured disabled response; control-plane/data-plane availability and UX contract unresolved (CP59).
- QA-OBS-011: legacy numeric host `127.1` resolves to loopback through system NSS but MCP registration rejects it as a DNS resolution failure; support is undocumented, and the endpoint is not contacted (CP60).
- QA-OBS-012: one project-switch run displayed `env_id does not belong to supplied project_id` while switching workspace; a request-logged follow-up returned 200 for both old and new scopes and settled with no error. Keep unclassified pending a repeatable capture (CP70).
- QA-OBS-013: Provider Connections lists organization-wide rows in selected-project context without owner-project labels; the Org Owner API behavior is intentional by authorization design, but presentation contract remains open (CP77).
- QA-OBS-014: repeated A2A-project FinOps captures differed on whether a project budget row existed before the CP78 write; the pre-action API response was not retained, so the cause is unresolved and no defect is assigned.
- QA-OBS-015: managed Cloud shell proxy can reach public HTTPS, but direct no-proxy DNS/egress used by Tanpopo's SSRF-guarded webhook client failed; public delivery and HMAC verification are blocked pending a direct-egress-capable environment. This is not classified as a Tanpopo defect (CP86).

## Coverage

- **Completed in CP43–58:** real Chromium scope switching across organization-only, project/no-environment, project/environment and a second project; navigation/refresh; visually inspected Access, FinOps, Routes, Traces, Playground, MCP, A2A, Overview, Providers, Models, Extensions and Security states; API/persisted-state comparison for members and budgets; Models no-environment create attempt generated validation but no mutation; real Team Admin sessions confirmed project chooser/MCP filtering and 403 on different-project reads/sync. CP47 owner sync contacted only the allowlisted synthetic fixture, visually exposed the first-schema lock/status mismatch, and owner pin resolved it. CP48 delayed a project1 FinOps budget response during a real workspace switch; skeletons replaced old rows and the selected project rows appeared after settlement. CP49 confirmed refresh returns a manually selected project to the fresh-login/runtime default; affected rows changed with scope and remained persisted, leaving preference behavior as QA-OBS-009. CP50 exercised HTTP 302/303 MCP redirects; both failed closed before target contact with no tool persistence. CP51 exercised one synthetic child project/environment route under an organization budget: positive control settled 2 micro-USD; reduced org limit caused a generic Playground 429 with canonical trace reason and no upstream dispatch; limit restored and temporary Playground keys revoked. CP52 exercised a two-target priority-failover stream: first synthetic provider returned 503, backup returned 200 and the trace waterfall recorded both ordered deployment IDs; a temporary key was revoked. The route UI rendered three distinct deployments with identical labels (DEF-027). CP54 used Route Explain on the same route: UI returned zero eligible deployments and two dialect rejections, while an API dry-run with dialect `ollama` selected both; the same route then returned a real 200 Playground stream selecting the primary. CP55 exercised a real bearer-only RPM-limited Data Plane key: 200 then fail-fast 429/Retry-After without egress, then 200 after the interval, followed by Console revocation. CP56 used the real policy UI to set EU-only organization residency, verified it resolved at Organization/Project/Environment API scopes, visually confirmed child editors exposed only EU, and restored all four regions. CP57 used the same healthy ON_PREM route under EU-only policy, observed 503 before provider dispatch, restored the organization policy, and verified 200 recovery; the key was revoked. CP58 verified hostname-resolved loopback CIDR enforcement: mismatched CIDR caused zero fixture calls; exact loopback `/32` reached the synthetic MCP fixture. DEF-026 and QA-OBS-004 recurred. CP59 compared an A2A-disabled runtime against the enabled 18084 runtime: the former returned the source-configured 404 gate while allowing control-plane provisioning, and the latter completed a fresh input-required task after navigation and hard refresh on the same task ID. The A2A-only key was revoked; no new defect assigned. DEF-028 remains confirmed; DEF-017 and QA-OBS-006 last recurred in prior phases.
- **Blocked:** CP53 originally lacked a usable Operator credential, but CP86 used the working Operator + Org Owner session to register a webhook and queue one synthetic event. Live delivery/signature remained BLOCKED by direct Cloud egress/DNS (QA-OBS-015); no request reached the receiver. CP43 found Routes & Policies guarded in organization-only workspace, while CP56 confirmed an owner can choose Organization edit scope from an active project/environment workspace; the direct org-only policy workflow expectation remains unresolved. Google adapter/runtime failures and RC4 artifact verification remain as recorded in prior checkpoints.
- **Remaining:** broader independent retest of DEF-001–030 on additional roles/platforms; full non-owner cross-project authorization; workspace transitions under latency and failure; organization-only policy workflow and other policy enforcement on live inference (CP57 covered one ON_PREM/EU path); multi-project MCP labeling/actions and first-discovery/drift counts; HTTP redirects beyond CP41/50, HTTPS redirects, redirect chains, DNS rebinding/mixed DNS and alternate runtime behavior (CP58 covers a single wildcard hostname resolution only); scope races outside FinOps and delayed/error refresh recovery; workspace persistence preference contract; effective budget display plus isolated spend/enforcement regression (CP51 verifies one organization ceiling enforcement path, while DEF-024 display remains open); TPM/concurrency/parent-policy limit enforcement and simultaneous ancestor/key cap precedence; model/plugin policy dimensions, guardrail confirmation and optimistic-lock recovery; remaining screens, business workflows, browser/platform modes, integrations and operational failures. See [coverage matrix](COVERAGE-MATRIX.md) for detailed PASS/FAIL/BLOCKED/UNVERIFIED entries.

## CP55 consolidated defect and coverage update

- **Historical DEF-001–015:** no CP55 retest; retain existing scoped statuses and open DEF-005 residuals.
- **New findings:** none. DEF-016–028 remain open. No new defect ID assigned.
- **Coverage completed:** one environment-scoped RPM=1 Virtual Key accepted one call, rejected the next with canonical 429/Retry-After before provider dispatch, recovered after the interval, and was revoked. The issue form did not expose rate-cap fields; the UI provisioning contract remains unverified.
- **Blocked:** no CP55 blocker. CP53 webhooks still need usable Operator credentials.
- **Remaining:** TPM, concurrency, parent policy and distributed rate limits; UI configuration contract; continue policy and workspace-scope business-state exploration.

## CP58 consolidated defect and coverage update

- **Historical DEF-001–015:** no CP58 full retest. DEF-005 remains partial; the single hostname-resolved CIDR case failed closed, while DNS rebinding/mixed answers, redirects, alternate runtimes, and mapped-address support remain open.
- **New findings:** none. DEF-016–028 remain open. DEF-026 recurred; QA-OBS-004 recurred.
- **Coverage completed:** `127.0.0.1.nip.io` resolution to loopback, mismatched-CIDR zero-egress denial, exact-CIDR positive control, persisted MCP/tool state, and visual review.
- **Blocked:** none in CP58. CP53 webhooks remain blocked on Operator access.
- **Remaining:** DNS rebinding/mixed answers, HTTPS/chained redirects, alternate runtimes, and other coverage gaps in the matrix.
## CP57 consolidated defect and coverage update

- **Historical DEF-001–015:** no CP57 retest; current scoped dispositions are unchanged. DEF-005 remains partial with security-contract gaps open.
- **New findings:** none. DEF-016–028 remain open. QA-OBS-006 recurred because Data Plane request IDs and trace-list IDs differ.
- **Coverage completed:** real synthetic route positive control; EU-only org policy propagated through environment scope; 503 fail-closed rejection before provider dispatch with persisted error trace; policy restore and 200 recovery; temporary key revoked and state verified.
- **Blocked:** no CP57 blocker. CP53 webhook delivery still lacks a usable Operator credential.
- **Remaining:** other policy regions and dimensions, alternate targets/roles, concurrency, broader route failures, and all incomplete areas in the coverage matrix.
## CP56 consolidated defect and coverage update

- **Historical DEF-001–015:** no CP56 retest; current scoped statuses remain unchanged, with DEF-005 partial and its security-contract gaps open.
- **New findings:** none. DEF-016–028 remain open; no new defect assigned.
- **Coverage completed:** organization residency changed through the real UI, resolved effective policy narrowed at Organization/Project/Environment scopes, child editors exposed only the inherited region, and the original organization policy was restored and verified. No inference or provider call was sent.
- **Blocked:** no CP56 blocker. CP53 webhook delivery remains blocked on Operator access.
- **Remaining:** policy route enforcement, other policy dimensions, guardrail workflows, optimistic-lock handling, alternate roles, org-only policy workflow, and broader campaign as listed above.

## CP54 consolidated defect update

- **Historical DEF-001–015:** no CP54 retest; scoped outcomes and residual limits are unchanged. DEF-005 remains partial.
- **Open findings:** DEF-016–028 remain open. DEF-028 confirms the Route Explain/live route inconsistency; DEF-017 recurred. DEF-019 remains expanded by CP51.
- **Coverage completed:** Route Explain on the same healthy multi-target Ollama route, API dry-run dialect comparison, real Playground positive control, and temporary key revocation.
- **Coverage blocked:** CP53 webhook registration/delivery still needs usable Operator credentials.
- **Remaining:** other adapters/dialects and policy/capability explain cases remain unverified; broader regression continues.

## CP53 consolidated coverage update

- **Historical defects:** no defect retested in CP53; existing scoped statuses are preserved.
- **Findings:** no new defect. QA-OBS-003 recurred.
- **Coverage completed:** exact Org Owner Operations navigation boundary; attempted alternate Operator credentials; disposable public receiver provisioned and deleted without any event payload.
- **Coverage blocked:** webhook registration, delivery, signature validation and retry until an Operator test credential is available.
- **Coverage remaining:** all unaffected regression areas continue; webhook blocker does not suspend other QA.

## CP52 consolidated defect update

- **Historical DEF-001–015:** no CP52 retest; preserve scoped statuses and limits. DEF-005 remains partial.
- **Open findings:** DEF-016–028 remain open. DEF-017 recurred after successful failover; DEF-027 and DEF-028 are confirmed. DEF-019 retains the CP51 organization-budget expansion; DEF-024 remains a visibility failure.
- **Coverage completed:** real provider connection/probe, same-model second deployment, priority-failover route, synthetic 503→200 streaming response, persistent ordered trace and key revocation.
- **Coverage blocked:** none in CP52.
- **Coverage remaining:** multi-target all-fail/timeout/cancel/weights, route edit/reorder and provider-specific failures; see matrix.

## CP51 consolidated defect update

- **Historical DEF-001–015:** CP51 changed no historical retest disposition; preserve their scoped QA-002 results and residual limits as recorded above/register. DEF-005 remains partial.
- **Confirmed findings:** DEF-016–026 remain open. CP51 expands DEF-019 to organization-scope budget rejection and observes DEF-017 pending trace metadata again. DEF-024 remains a UI visibility failure despite the scoped backend enforcement pass. No new defect ID was added.
- **Coverage completed:** one full synthetic provider→model→healthy deployment→route→streaming Playground journey; organization cap positive/rejection/recovery; UI and DB verification; test-key revocation.
- **Coverage blocked:** none in CP51. Google credentials/provider adapter and RC4 artifact limitations remain as historical environment statuses.
- **Remaining:** simultaneous budget scope precedence, additional roles/browsers, provider-side 429 differentiation, and cross-screen recovery remain unverified; see matrix.

## Recommended engineering handoffs

1. DEF-017, DEF-018, DEF-019, DEF-020, DEF-023 and DEF-024: prepare cross-screen state and business visibility fixes with end-to-end persistence regressions.
2. DEF-027: disambiguate route target selection and fallback summaries by provider connection and deployment identity.
3. DEF-016 and DEF-021: agree the member enrollment/list contract and align UI success/error states with supported onboarding.
4. DEF-005: keep security boundaries fail-closed; engineering should define redirects and supported mapped-address notation before changing behavior. Do not weaken SSRF or authorization.
5. DEF-022: ensure live session role changes reconcile client state without exposing actions after server invalidation.
6. DEF-026: align sync result counts/copy with initial schema lock state while retaining the pin-before-use control.
7. DEF-028: make Route Explain use or expose the live route's actual request dialect semantics; preserve dry-run/no-egress and reject truly incompatible native requests clearly.

`GATE-REL-07` remains **BLOCKED**. No product implementation was changed, no release was approved, and QA-002 is not complete.

## CP59 consolidated defect and coverage update

- **Historical DEF-001–015:** not retested in CP59; retain the existing scoped states. DEF-005 remains partial.
- **Open findings:** DEF-016–028 remain open; no new defect. QA-OBS-010 records the A2A control-plane/data-plane configuration contract question.
- **Coverage completed:** actual A2A discovery, A2A-only key scope, input-required lifecycle, persisted task identity across navigation/refresh, real UI resume to completion, fixture isolation metadata, and key revocation.
- **Blocked:** A2A use on the 18445 runtime because its A2A plane is disabled by configuration; config provenance not readable. CP53 webhook delivery still needs Operator access.
- **Remaining:** full DEF-023 all-screen scope sweep; unresolved DEF-005 DNS-rebinding/transport and alternate-runtime cases; independent retests and all remaining feature/role/platform areas. See CP59 and the matrix.

## CP60 consolidated defect and coverage update

- **Historical DEF-001–015:** no new retests; preserve scoped dispositions. DEF-005 remains PARTIAL.
- **New findings:** none confirmed. DEF-016–028 remain open. QA-OBS-004 recurred; QA-OBS-011 records unsupported/noncanonical host behavior without security bypass evidence.
- **Coverage completed:** confirmed DEF-023 source cause and cross-scope API/UI mismatch; renewed visual review of existing CP43 contrasts; explicit-CIDR fail-closed checks for `127.1` and `localhost.` against a metadata-only fixture.
- **Blocked:** fresh Team Admin access because CP45 intentionally suspended its credential; CP53 webhooks remain blocked on Operator credentials.
- **Remaining:** full scope-switch matrix on remaining screens/roles; DEF-005 rebinding, multiple answers, HTTPS chains and alternate runtimes; independent historical retests; other platform/integration coverage.

## CP61 consolidated defect and coverage update

- **Historical DEF-001–015:** not retested; all prior scoped statuses remain. DEF-005 stays PARTIAL.
- **Open findings:** DEF-016–028 remain open; DEF-026 reproduced again on direct IPv6 loopback. No new defect ID. QA-OBS-004 recurred.
- **Coverage completed:** direct IPv6 literal with mismatching and exact CIDR, fixture zero-contact/positive controls, repeat sync, persistent schema lock, and screenshot inspection.
- **Blocked:** none specific to CP61; CP53 Operator credential gap remains.
- **Remaining:** DNS rebinding/mixed-answer races, redirect chains/HTTPS, alternate runtime, direct IPv6 grants/calls, workspace scope coverage, and all other incomplete feature/role/platform cases.

## CP62 consolidated defect and coverage update

- **Historical DEF-001–015:** not retested in CP62; preserve scoped dispositions. DEF-005 remains partial.
- **New finding:** DEF-029 Low confirmed. Ten old environment keys appeared under No project / No env after a held HTTP 200 response; navigation cleared them. No secret or authorization bypass was observed.
- **Other open defects:** DEF-016–028 remain open; DEF-023 and DEF-029 are related to AccessPage scope but distinct. DEF-026 and QA-OBS-004 recurred in CP61.
- **Coverage completed:** forced response-order test for Access & Keys, exact project/env IDs in returned rows, organization-only before/after screenshots, and navigation recovery.
- **Blocked:** no CP62 blocker. Fresh Team Admin test remains blocked because prior synthetic credential was intentionally suspended; CP53 webhook integration remains blocked on Operator access.
- **Recommended handoff:** include DEF-029’s stale response guard with DEF-023’s scope-dependent member loading, but retain separate acceptance tests.
- **Remaining:** scope races on other pages and between environments/projects, limited-role exposure matrix, additional DEF-005 cases, independent historical retests, and all remaining feature/browser/platform coverage.

## CP63 consolidated defect and coverage update

- **Historical DEF-001–015:** not retested; retain their prior scoped dispositions. DEF-005 remains partial.
- **New finding:** DEF-030 Moderate confirmed. Three environment routes reappeared under No project / No env after a delayed HTTP 200; Edit/Delete remained visible and Save route was enabled. No mutation or auth bypass occurred.
- **Other new findings:** DEF-029 Low remains confirmed for stale Virtual Key metadata; DEF-023 remains a separate organization-members false-empty issue.
- **Coverage completed:** real-browser race on route listing, scope switch, old environment response, route edit/delete menu, enabled edit drawer and screenshot review.
- **Blocked:** none in CP63. Prior Team Admin credential and webhook Operator limitations remain.
- **Recommended handoff:** fix scope-bound async state across Access & Keys and Routes & Policies, preserving separate acceptance tests for membership visibility and route mutation controls.
- **Remaining:** cross-environment route races, page-by-page race sweep, role-limited actions, full historical retests, integrations and platform/browser coverage.


## CP87 consolidated status update

- **Historical defects verified fixed:** DEF-001–004, DEF-006, DEF-008–015 retain only their prior scoped QA-002 results. CP87 did not retest them; independent acceptance coverage remains incomplete overall. DEF-005 remains partial. No QA-001 PASS is inherited.
- **Historical defects failing / unverified:** DEF-016–034 remain confirmed open, including CP86 DEF-034 and the DEF-020 recurrence. No defect status changed in CP87. All prior defect evidence remains preserved.
- **New findings:** No new defect. QA-OBS-016 records unclear organization-wide Chargeback scope labeling when the workspace selector shows a project. QA-OBS-015 Cloud direct-egress blocker remains.
- **Coverage completed:** CP87 real Chromium owner Chargeback view; visual comparison of selected project and organization-wide project grouping; API/UI amount and row agreement; valid recent, malformed and reversed API window handling; selected-project query comparison. Screenshots were visually inspected; no mutation.
- **Coverage blocked:** direct webhook delivery/HMAC remains blocked by managed Cloud direct DNS/egress; runtime 18445 A2A remains disabled by configuration; RC4 artifact verification remains UNVERIFIED because the artifact is in a separate unavailable DevBox.
- **Coverage remaining:** independent DEF-001–015 acceptance coverage remains incomplete; DEF-005 DNS rebinding/mixed answers/redirect chains/alternate runtimes; DEF-007 failure atomicity and populated-org erasure; Chargeback roles/pagination/locales/date-filter UI/project-scope contract; all open DEF-016–034 remediation and regression; actual upgrade safe-abort; provider outage/recovery; remaining product, browser, platform, accessibility, integration and operations coverage.
- **Recommended handoffs:** prioritize DEF-034 subscription revocation; DEF-016/023/029/030 scope/state consistency; DEF-033 budget create/update semantics; DEF-005 SSRF acceptance without weakening controls; DEF-007 audit atomicity/populated erasure. Confirm the Chargeback scope-label contract (QA-OBS-016).
- **Checkpoint:** [CP87](checkpoints/CP87-finops-chargeback-scope-and-date-windows.md); prior [CP86](checkpoints/CP86-webhook-delivery-and-subscription-lifecycle.md).


## CP88 consolidated status update

- **Historical DEF-001–015:** no new retests in CP88. Prior QA-002 scoped statuses remain, DEF-005 remains partial, and full independent acceptance coverage remains incomplete. No QA-001 PASS is inherited.
- **Historical defects failing / unverified:** DEF-016–034 remain confirmed open; no status changed. Preserve CP86 DEF-034 and DEF-020 recurrence.
- **New findings:** no new defect. Team Admin's Security Audit Log request returned 403 and displayed an explicit role-denial message; source RBAC omits audit-read permission for Team Admin while allowing scoped kill-switch/approval actions. Treated as intentional authorization, not a bypass or product defect.
- **Coverage completed:** CP88 real Chromium Team Admin session, Security route, audit endpoint denial, visible UI state, and no returned protected rows. No mutation.
- **Coverage blocked:** direct webhook delivery/HMAC by managed Cloud egress; A2A on runtime 18445 by its disabled configuration; RC4 artifact verification UNVERIFIED in the unavailable separate DevBox.
- **Coverage remaining:** independent DEF-001–015 acceptance coverage remains incomplete; DEF-005 SSRF cases; DEF-007 failure atomicity/populated erasure; open DEF-016–034 remediation and regression; remaining Console roles/scopes, business workflows, cross-feature state, browsers/platforms, integrations, accessibility and operations.
- **Recommended handoffs:** DEF-034, DEF-016/023/029/030, DEF-033, DEF-005 without weakening SSRF controls, and DEF-007. No handoff added from CP88.
- **Checkpoint:** [CP88](checkpoints/CP88-team-admin-security-audit-boundary.md); prior [CP87](checkpoints/CP87-finops-chargeback-scope-and-date-windows.md).


## CP89 consolidated status update

- **Historical DEF-001–015:** no new retests in CP89; scoped prior QA-002 dispositions remain, DEF-005 remains partial, independent acceptance coverage remains incomplete. No QA-001 PASS is inherited.
- **Historical defects failing / unverified:** DEF-016–034 remain open; no status changed. Preserve DEF-034 and the DEF-020 recurrence.
- **New findings:** no new defect. Existing trace API/UI inspection showed metadata only for two rows, both with `has_debug_payload=false`; full privacy/export acceptance remains unverified.
- **Coverage completed:** real Chromium authenticated trace list/detail, API comparison and visual inspection; read-only YAML/listener check. No mutation.
- **Coverage blocked:** no configured OTLP collector endpoint/access was established and no local 4317/4318 listener or YAML setting was found; process environment read was denied, so collector outage/drop counters were not tested. Direct webhook delivery/HMAC remains managed-egress blocked; A2A on 18445 is config-disabled; RC4 artifact remains unverified in the separate DevBox.
- **Coverage remaining:** independent DEF-001–015 acceptance coverage; full TP-AC-F0804 marker/credential search and exporter outage/recovery; trace roles/search and other waterfall cases; DEF-005 SSRF cases; DEF-007 failure atomicity/populated erasure; all open DEF-016–034 remediation/regression; remaining product/browser/platform/integration/accessibility/operations coverage.
- **Recommended handoffs:** unchanged—DEF-034, DEF-016/023/029/030, DEF-033, DEF-005 without weakening SSRF controls, DEF-007. No new defect handoff.
- **Checkpoint:** [CP89](checkpoints/CP89-trace-explorer-metadata-privacy.md); prior [CP88](checkpoints/CP88-team-admin-security-audit-boundary.md).


## CP90 consolidated status update

- **Historical DEF-001–015:** CP90 did not retest them. Preserve prior scoped QA-002 results; DEF-005 remains partial and independent acceptance remains incomplete. No QA-001 PASS is inherited.
- **Historical defects failing / unverified:** DEF-016–034 remain open; no status changed. Preserve CP86 DEF-034 and DEF-020 recurrence.
- **New findings:** no new defect. A narrow real owner Vault list/API read exposed only metadata for one existing QA credential; no plaintext observed.
- **Coverage completed:** real Chromium Providers → Vault Secrets and authenticated API field comparison. Existing QA credential left untouched; no mutation.
- **Coverage blocked:** full secret-privacy scan requires credential-bearing inference plus persistent log/API search; OTel fail-open lacks confirmed collector endpoint/access; direct webhook delivery/HMAC is managed-egress blocked; runtime 18445 A2A is disabled by configuration; RC4 artifact verification remains unverified in separate DevBox.
- **Coverage remaining:** independent DEF-001–015 acceptance; full DEF-005 SSRF coverage; DEF-007 populated erasure/failure atomicity; secret binding/rotation/revocation/OAuth and role coverage; all open DEF-016–034 regression/remediation; all remaining features, browsers/platforms/integrations/accessibility/operations.
- **Recommended handoffs:** unchanged—DEF-034, DEF-016/023/029/030, DEF-033, DEF-005 without weakening SSRF, and DEF-007.
- **Checkpoint:** [CP90](checkpoints/CP90-vault-secret-metadata.md); prior [CP89](checkpoints/CP89-trace-explorer-metadata-privacy.md).


## CP91 consolidated status update

- **Historical DEF-001–015:** no new retests in CP91. Preserve scoped prior QA-002 results; DEF-005 remains partial; independent acceptance is incomplete; no QA-001 PASS is inherited.
- **Historical defects failing / unverified:** DEF-016–034 remain confirmed open; no status changed. Preserve DEF-034 and DEF-020 recurrence.
- **New findings:** no new defect. QA-OBS-016 expanded: owner org-wide chargeback, Overview and traces display organization-level rows while a project remains selected, with no explicit report-scope label. Values agree and this is consistent with Owner organization authorization.
- **Coverage completed:** mapped two trace rows to a sibling project environment and reconciled them to the 24-hour Overview total under a selected empty project; real UI screenshots inspected.
- **Coverage blocked:** direct webhook delivery/HMAC by managed Cloud egress; runtime 18445 A2A by configuration; RC4 artifact unverified in separate DevBox; OTel collector outage acceptance awaits known endpoint/access.
- **Coverage remaining:** independent DEF-001–015 acceptance; DEF-005 SSRF cases; DEF-007 failure atomicity/populated erasure; trace/overview scope for non-owner roles and scope contract; open DEF-016–034 regression/remediation; remaining features, browsers/platforms, integrations, accessibility and operations.
- **Recommended handoffs:** unchanged—DEF-034, DEF-016/023/029/030, DEF-033, DEF-005 without weakening SSRF, and DEF-007. Clarify global-report scope labeling.
- **Checkpoint:** [CP91](checkpoints/CP91-trace-and-overview-scope-under-project-selection.md); prior [CP90](checkpoints/CP90-vault-secret-metadata.md).


## CP92 consolidated status update

- **Historical defects verified fixed:** DEF-001 remains VERIFIED FIXED for scoped scenarios; CP92 independently completed a fresh four-step setup on RC4 and verified accessible names include each visible step label. DEF-002–004 and DEF-006–015 retain their previous scoped passes only.
- **Historical defects failing / unverified:** DEF-005 remains partial; DEF-016–034 remain open. DEF-002–015 broader roles/platforms/edge cases are not complete; no QA-001 PASS is inherited. Preserve DEF-034 and DEF-020 recurrence.
- **New findings:** no new defect. A no-adapter fresh install completed when optional connection setup was skipped.
- **Coverage completed:** separate runtime began `initialized=false`; real Setup UI completed owner/org/default-project bootstrap and custody acknowledgement; new session login and persisted one-owner/one-project evidence verified. Screenshots inspected; temporary key/token/password/database removed.
- **Coverage blocked:** CP92 itself had no blocker. Direct webhook/HMAC still managed-egress blocked; runtime 18445 A2A disabled by configuration; RC4 artifact verification unverified in separate DevBox; full OTLP collector acceptance awaits endpoint/access.
- **Coverage remaining:** independent DEF-002–015 residual coverage, DEF-005 SSRF cases, DEF-007 failure atomicity/populated erasure, fresh-instance adapter activation/first inference, open DEF-016–034 regression/remediation, remaining browsers/platforms/roles/integrations/accessibility/operations.
- **Recommended handoffs:** unchanged—DEF-034, DEF-016/023/029/030, DEF-033, DEF-005 without weakening SSRF, and DEF-007.
- **Checkpoint:** [CP92](checkpoints/CP92-fresh-install-and-DEF-001-retest.md); prior [CP91](checkpoints/CP91-trace-and-overview-scope-under-project-selection.md).


## CP93 consolidated status update

- **Historical DEF-001–015:** no defect retest in CP93; CP92 retested DEF-001. Other scoped results remain; DEF-005 is partial and independent acceptance remains incomplete. No QA-001 PASS is inherited.
- **Historical defects failing / unverified:** DEF-016–034 remain open; no status change. Preserve DEF-034 and the DEF-020 recurrence.
- **New findings:** no product defect. QA-OBS-017 records the distinction between current RC4 `upstream_unreachable` (Cloud direct-egress mismatch supported) and historical listener-specific `probe_runtime_unavailable` (exact cause unresolved). DEF-032 duplicate Google rows remain open.
- **Coverage completed:** real RC4 Google connection probe on 18084; connection stayed Unverified; 18086 inventory showed no Google connection; historical 3-listener evidence reconciled without inheriting PASS.
- **Coverage blocked:** a current same-config three-runtime Google comparison lacks configured credential/connection on 18086 and was not established on 18083/18085; actual app-mediated Google inference still awaits guarded direct egress. Direct webhook/HMAC, 18445 A2A configuration, RC4 artifact DevBox and OTLP collector remain blocked/unverified.
- **Coverage remaining:** independent DEF-002–015 residual cases; same-config Google runtime comparison; DEF-005 SSRF cases; DEF-007 failure atomicity/populated erasure; all open DEF-016–034 regressions/remediation; other products, roles, browsers/platforms, integrations, accessibility and operations.
- **Recommended handoffs:** unchanged—DEF-034, DEF-016/023/029/030, DEF-033, DEF-005 without weakening SSRF, DEF-007. No engineering handoff for environment-classified Google transport failures.
- **Checkpoint:** [CP93](checkpoints/CP93-google-provider-probe-runtime-comparison.md); prior [CP92](checkpoints/CP92-fresh-install-and-DEF-001-retest.md).


## CP94 consolidated status update

- **Historical defects verified fixed:** no historical defect retested in CP94; preserve the last independently observed scoped outcomes. DEF-001 remains scoped PASS from CP92 only. No QA-001 PASS is inherited.
- **Historical defects failing / unverified:** DEF-016–034 remain open, including DEF-023 and visually apparent duplicate provider rows DEF-032; DEF-005 remains partial. DEF-002–015 independent acceptance coverage remains incomplete.
- **New findings:** no new defect. CP94 saw the existing duplicate Google connection cards but did not perform destructive actions or independently verify backend identity ambiguity.
- **Coverage completed:** real Chromium at 375 × 844; Vietnamese and dark-theme selection; Overview, Providers and FinOps interaction; persistence after reload; width checks; direct visual inspection of current screenshots. Scoped PASS only.
- **Coverage blocked:** none specific to CP94. Previously recorded egress, A2A runtime, OTel collector and RC4 artifact limitations remain unchanged.
- **Coverage remaining:** other locales/themes/viewports/browsers and accessible mobile interaction; independent DEF-001–015 acceptance; DEF-005 SSRF acceptance; DEF-007 populated erasure/failure atomicity; all open DEF-016–034 regression; remaining Console, API, CLI, integration, security and operational scenarios.
- **Recommended handoffs:** continue existing priorities DEF-034, DEF-016/023/029/030, DEF-033, DEF-005 without weakening SSRF, and DEF-007. No new handoff from CP94.
- **Checkpoint:** [CP94](checkpoints/CP94-vietnamese-dark-mobile-console-regression.md); prior [CP93](checkpoints/CP93-google-provider-probe-runtime-comparison.md).


## CP95 consolidated status update

- **Historical DEF-001–015:** CP95 did not retest these. Preserve prior QA-002 scoped outcomes only: DEF-001–004, DEF-006, and DEF-008–015 have limited passing paths; DEF-005 remains partial; DEF-007’s successful-path audit evidence does not cover populated erasure/failure atomicity. Full independent acceptance remains incomplete. No QA-001 PASS is inherited.
- **Historical defects verified fixed / failing / unverified:** no prior defect disposition changed. DEF-016–034 remain open, including DEF-023/024/029/030/034. DEF-035 is newly confirmed. All historic evidence and checkpoint uncertainty remain intact.
- **New finding:** DEF-035 Moderate confirmed. Org-only and empty-project Models views show three deployments owned by another project’s `stage` environment, without scope identity, while exposing Disable/Delete. No mutation or authorization bypass was demonstrated.
- **Coverage completed:** real Chromium scope selection and SPA navigation across org-only, project-only, and environment contexts; Extensions installed/catalog/lockfile, Models, and Routes & Policies; deployment owner/project mapping via authenticated API; screenshots visually inspected.
- **Coverage blocked:** no CP95-specific blocker. Cloud direct-egress/webhook delivery, A2A runtime 18445 configuration, OTel collector access, and separate DevBox RC4 artifact verification limitations persist.
- **Coverage remaining:** all incomplete feature, role, platform and integration work; complete independent DEF-001–015 retests; DEF-005 SSRF acceptance without weakening boundaries; DEF-007 fault atomicity/populated erasure; DEF-016–035 remediation regression; page-by-page scope and permission checks; Models action lifecycle and recovery; other browsers/platforms.
- **Recommended handoffs:** include DEF-035 with scope/state findings DEF-023/024/029/030. Keep DEF-034, DEF-033, DEF-005, and DEF-007 priorities.
- **Checkpoint:** [CP95](checkpoints/CP95-console-workspace-scope-and-model-deployment-identity.md); prior [CP94](checkpoints/CP94-vietnamese-dark-mobile-console-regression.md). `GATE-REL-07` remains **BLOCKED**.


## CP96 consolidated status update

- **Historical DEF-001–015:** CP96 did not retest them. Preserve limited scoped QA-002 outcomes: DEF-001–004, DEF-006, DEF-008–015 have passing paths; DEF-005 remains partial; DEF-007 successful audit paths do not close fault atomicity/populated erasure. No QA-001 PASS is inherited; full independent acceptance remains incomplete.
- **Historical open DEF-016–035:** remain open; CP96 did not change any defect disposition. DEF-035 is still the newest confirmed defect.
- **New defects:** none. The native attempt was environment-blocked/inconclusive, not assigned to product.
- **Coverage completed:** real UI issue and revoke of two PROVIDER keys restricted to `/native/*`; authenticated API confirmed both REVOKED; exposed endpoint attempt returned Console method fallback 405 and did not establish dispatch. Secrets were removed and excluded from evidence.
- **Coverage blocked:** FT-02 native passthrough positive exchange/fidelity needs the correct enabled QA Data Plane native-ingress address/configuration. No product defect is inferred from a Console fallback.
- **Coverage remaining:** complete independent DEF-001–015 acceptance; DEF-005 residual SSRF checks; DEF-007 fault/erasure cases; open DEF-016–035 regressions; all other feature, browser/platform, role, integration and operational coverage.
- **Recommended handoffs:** no new engineering handoff; continue DEF-035 with DEF-023/024/029/030 and existing DEF-034/033/005/007 priorities.
- **Checkpoint:** [CP96](checkpoints/CP96-native-passthrough-ingress-discovery.md); prior [CP95](checkpoints/CP95-console-workspace-scope-and-model-deployment-identity.md). `GATE-REL-07` remains **BLOCKED**.


## CP97 consolidated status update

- **Historical DEF-001–015:** CP97 did not retest them. Preserve prior scoped QA-002 results only: DEF-001–004, DEF-006, DEF-008–015 have limited passing paths; DEF-005 remains partial; DEF-007 successful audit paths do not close fault atomicity/populated erasure. No QA-001 PASS is inherited; full acceptance remains incomplete.
- **Historical open DEF-016–035:** remain open; no defect disposition changed.
- **New defects:** none. The standalone `doctor` failure rows were caused/inconclusive in the local CLI context and were not classified as product defects; the serving runtime stayed healthy/ready.
- **Coverage completed:** installed/checksummed Go 1.26.11 to `/tmp`; restored dependencies; built the exact source baseline with explicit release metadata; verified CLI version output; exercised safe GitOps success and malformed-input non-overwrite; exercised localized missing-admin-token behavior; compared doctor status to live `/healthz` and `/readyz`.
- **Coverage blocked:** remote CLI calls require `TANPOPO_ADMIN_TOKEN`, absent from this Cloud runtime; native passthrough requires an enabled Data Plane address/config; separate RC4 DevBox artifact remains unavailable/unverified.
- **Coverage remaining:** complete independent DEF-001–015 acceptance; DEF-005 residual SSRF acceptance; DEF-007 populated erasure/failure atomicity; DEF-016–035 regression; remote CLI with a dedicated QA token; non-empty GitOps and interrupted apply recovery; all other feature, role, browser/platform, integration and operational coverage.
- **Recommended handoffs:** unchanged—DEF-035 with DEF-023/024/029/030, DEF-034, DEF-033, DEF-005 without weakening SSRF, and DEF-007.
- **Checkpoint:** [CP97](checkpoints/CP97-cli-provenance-gitops-and-doctor.md); prior [CP96](checkpoints/CP96-native-passthrough-ingress-discovery.md). `GATE-REL-07` remains **BLOCKED**.


## CP98 consolidated status update

- **Historical DEF-001–015:** CP98 did not retest them. Preserve scoped QA-002 outcomes: DEF-001–004, DEF-006, DEF-008–015 have limited passing paths; DEF-005 remains partial; DEF-007 successful audit paths do not close fault atomicity/populated erasure. Full acceptance remains incomplete; no QA-001 PASS is inherited.
- **Historical open DEF-016–035:** remain open; no defect disposition changed.
- **New defects:** none.
- **Coverage completed:** Tier-1 and Tier-2 scaffolds, validation and four offline cassette cases per tier; repeated byte-identical packages; supported/unsupported contract cases; explicit wildcard egress denial; tampered Wasm digest denial. All generated package fixtures remained in `/tmp`.
- **Coverage blocked:** no CP98-specific blocker. Runtime extension installation/isolation is not represented by the offline harness. Remote CLI and native ingress blockers from CP96/97 persist.
- **Coverage remaining:** package permission consent and install, lockfile-to-runtime persistence, actual adapter invocation, blind credential broker, live host restrictions, Wasm traps/quotas/quarantine, rollback and platform parity; all earlier unfinished feature/defect coverage remains.
- **Recommended handoffs:** no new handoff; preserve DEF-035 with DEF-023/024/029/030 and existing DEF-034/033/005/007 priorities.
- **Checkpoint:** [CP98](checkpoints/CP98-plugin-authoring-offline-cassettes-and-package-integrity.md); prior [CP97](checkpoints/CP97-cli-provenance-gitops-and-doctor.md). `GATE-REL-07` remains **BLOCKED**.

## CP99 consolidated status update

- **Historical defects verified fixed:** DEF-001–004, DEF-006, and DEF-008–015 retain only the scoped QA-002 results recorded in the retest register; no QA-001 PASS is inherited. DEF-005 remains PARTIAL. DEF-007 successful audit paths do not close failure atomicity or populated erasure.
- **Historical defects failing / unverified:** DEF-016–035 remain open; CP99 did not retest them. DEF-023 remains confirmed from earlier scope/API comparisons. No prior disposition was removed or downgraded.
- **New defects:** DEF-036 Low confirmed: duplicate Marketplace staging for already-active `@tanpopo/gemini` returns 409 with raw storage error; installation list unchanged. No other new defect.
- **Coverage completed:** real Chromium Installed and Marketplace navigation, catalog visual inspection, duplicate package action, HTTP 409 and before/after persistent installation comparison. Screenshots were visually inspected.
- **Coverage blocked:** none specific to CP99. Extension permission-consent activation, package runtime isolation, remote CLI token, native ingress, direct webhook delivery and separate RC4 artifact blockers remain documented.
- **Coverage remaining:** independent DEF-001–015 acceptance remains incomplete overall; DEF-005 residual SSRF scenarios; DEF-007 fault/populated erasure; DEF-016–036 regression; normal install/permission review/independent approval/activation/upgrade/uninstall; runtime plugin isolation/credential broker; remaining roles, product surfaces, browsers, platforms, integrations, and operations.
- **Recommended engineering handoffs:** add DEF-036 to extension lifecycle handling; preserve DEF-035 with DEF-023/024/029/030, and existing DEF-034, DEF-033, DEF-005 without weakening SSRF, and DEF-007 priorities.
- **Checkpoint:** [CP99](checkpoints/CP99-marketplace-duplicate-install-review.md); previous [CP98](checkpoints/CP98-plugin-authoring-offline-cassettes-and-package-integrity.md). `GATE-REL-07` remains **BLOCKED**.

## CP100 consolidated status update

- **Historical defects verified fixed:** DEF-001–004, DEF-006, and DEF-008–015 retain only their scoped QA-002 results; no QA-001 PASS is inherited. DEF-005 remains PARTIAL. DEF-007 successful audit paths do not close failure atomicity or populated erasure.
- **Historical defects failing / unverified:** DEF-016–036 remain open; CP100 freshly reconfirmed DEF-023 but changed no other disposition. Prior intentional Team Admin/Security 403 boundaries remain separate from the Org Owner false-empty Members defect.
- **New defects:** none. DEF-032 duplicate Google display rows were visually seen again on Providers but identity was not independently mapped or mutated. QA-OBS-009 recurred: hard refresh reset the selected environment to runtime-default no-environment scope.
- **Coverage completed:** real Chromium scope switching across org-only, project-only and project/environment on Providers, MCP Hub, Security, Operations, Access Members and Playground; direct API member counts; hard refresh, explicit scope reselection and navigation/re-entry. WebKit at 375 × 844 independently reproduced the same 0/0/4 member UI/API discrepancy across all three scopes with no horizontal overflow; screenshots visually inspected.
- **Coverage blocked:** fresh Team Admin login was rejected by the live login page, so CP100's broad sweep used only Org Owner + Operator. CP71/CP88 remain the earlier scoped permission evidence; non-owner cross-project coverage is incomplete.
- **Coverage remaining:** full Console scope sweep, remaining roles and workspace permutations, async/error races, DEF-001–015 acceptance residuals, DEF-005 SSRF cases, DEF-007 fault/populated erase, DEF-016–036 regression, and remaining product, platform, integration, browser, accessibility and operational coverage.
- **Recommended engineering handoffs:** CP100 evidence strengthens DEF-023’s explicit organization-scope acceptance. Keep its fix coupled to but distinct from DEF-024/029/030/035. Continue DEF-036/034/033, DEF-005 without weakening SSRF, and DEF-007 priorities.
- **Checkpoint:** [CP100](checkpoints/CP100-console-workspace-scope-cross-surface-regression.md); previous [CP99](checkpoints/CP99-marketplace-duplicate-install-review.md). `GATE-REL-07` remains **BLOCKED**.

## CP101 consolidated status update

- **Historical defects verified fixed:** DEF-001–004, DEF-006, and DEF-008–015 retain only scoped QA-002 results; no QA-001 PASS is inherited. DEF-005 remains PARTIAL. DEF-007 failure atomicity and populated erasure remain UNVERIFIED.
- **Historical defects failing / unverified:** DEF-016–036 remain open; CP101 independently reconfirmed DEF-023 in a second authorized organization. No other disposition changed.
- **New defects:** none. No unauthorized access or cross-tenant bypass was demonstrated.
- **Coverage completed:** real workspace organization switch and organization-only Members & Roles comparison in a second tenant; API returned four members/three projects while UI falsely showed zero; screenshot visually inspected.
- **Coverage blocked:** none specific to this read-only Org Owner journey. The fresh Team Admin retest remains blocked by rejected retained credentials as recorded in CP100.
- **Coverage remaining:** second-org project/environment member scope and other roles; full DEF-001–015 acceptance, DEF-005 residual SSRF, DEF-007 failure/populated erasure, DEF-016–036 retesting, and all other untested product, platform, browser, integration, security, accessibility and operations work.
- **Recommended engineering handoffs:** add the second-organization evidence to DEF-023 acceptance; preserve explicit org-member visibility and Team Admin/tenant authorization boundaries. Continue DEF-036, DEF-035 with DEF-024/029/030, DEF-034, DEF-033, DEF-005 without weakening SSRF, and DEF-007 priorities.
- **Checkpoint:** [CP101](checkpoints/CP101-organization-switch-membership-scope.md); previous [CP100](checkpoints/CP100-console-workspace-scope-cross-surface-regression.md). `GATE-REL-07` remains **BLOCKED**.
