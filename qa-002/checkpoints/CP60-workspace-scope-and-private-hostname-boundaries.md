# CP60 — Workspace-scope consistency and noncanonical private hostname checks

**Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`  
**Result:** FAIL DEF-023 remains supported by exact source control flow and prior independent UI/API evidence. DEF-005 stays PARTIAL: new private-host aliases failed closed with no fixture contact; no SSRF bypass or new defect found.  
**Release gate:** `GATE-REL-07 BLOCKED`.

## DEF-023: broader workspace-scope and business-state consistency

Independent source inspection in [`AccessPage.tsx`](../../web/console/src/pages/access/AccessPage.tsx) verifies the defect mechanism: `refresh()` returns early whenever either `projectId` or `environmentId` is empty; before returning, it clears both `keys` and `members`. The organization-wide `listMembers()` request is only made after this guard. This means a valid organization membership view is coupled to unrelated environment scope, and the page converts a valid organization member collection into a false empty state.

CP43 remains the current real-browser reproduction and was visually re-inspected for this phase: org-only and project-without-environment UI show “Organization members 0 / No members found,” while the authenticated owner API returns the persisted owner; selecting an environment renders the same owner. This applies across two project contexts. The loaded row is explicitly labeled “Organization members,” reinforcing the business scope independent of environment. The screenshots, response comparison, and exact test setup remain linked in [CP43](CP43-console-workspace-scope-and-finops.md); CP60 did not repeat its already completed broad screen sweep.

The behavior is consistent with the independent CP43 finding and source path, not merely a transient stale render. It is a data-visibility/business-state defect; no authorization bypass was observed. We did not create, edit, or remove membership in this phase. Role-specific current retest was BLOCKED because the disposable Team Admin credential from CP45 had been intentionally suspended/cleared; CP45’s prior project-boundary PASS remains scoped and is not inherited as this phase’s PASS.

The wider Console scope baseline from CP43 remains differentiated by intent: organization-level membership and budgets have organization data; Routes & Policies and Playground explain that an environment is required; Traces and A2A show empty states when no data exists; MCP Hub displays owner-authorized organization-wide rows regardless of selected project but omits their project labels (QA-OBS-008); a child project hides an applicable persisted organization budget (DEF-024). These are distinct behaviors/findings, not automatically one defect. Further cross-screen switching under permission changes and backend latency remains open.

## DEF-005: new private-hostname checks

Used the actual MCP Hub registration and Sync flow against the CP58 metadata-only synthetic Streamable HTTP fixture. `getent ahostsv4 127.1` resolved the alias to `127.0.0.1`. Registering `http://127.1:19183/mcp` with private-endpoint consent and an explicit nonmatching `127.0.0.2/32` returned 403 `ssrf_blocked_private_ip` (“DNS resolution failed for endpoint host 127.1”); no MCP server row persisted and the fixture received zero requests. This is fail-closed. System NSS resolution does not prove that the Go runtime resolver contract supports legacy numeric shorthand; there is no evidence this failure weakens the SSRF boundary, and the shorthand support requirement is undocumented.

A second real UI case registered `http://localhost.:19183/mcp` with `127.0.0.2/32`. Local `getent ahosts localhost.` returned `::1` and `127.0.0.1`; registration returned 201, but Sync returned 502 `mcp_sync_failed`, the UI showed “upstream MCP tool synchronization failed,” the persisted row stayed ACTIVE with zero tools, and the synthetic fixture call count remained exactly 3 before/after. The screenshot was visually inspected. The exact mismatch reason is not exposed in the UI/response; this repeats QA-OBS-004. No secrets or endpoint credentials were used.

These results confirm fail-closed behavior only for the tested aliases. They do not constitute DNS rebinding, multiple DNS answer race, redirect-chain, alternate runtime, or cross-platform proof. No CIDR or authorization boundary was changed.

## Evidence

- [`../evidence/CP60-private-hostname-cidr.json`](../evidence/CP60-private-hostname-cidr.json) — sanitized API/UI/fixture outcome for `localhost.`.
- [`../evidence/CP60-localhost-dot-mismatch-form.png`](../evidence/CP60-localhost-dot-mismatch-form.png) and [`../evidence/CP60-localhost-dot-mismatch-result.png`](../evidence/CP60-localhost-dot-mismatch-result.png) — real MCP form and generic failed-sync state, visually inspected.
- [`../evidence/CP60-qa002-cp60-short-host-mismatch-form.png`](../evidence/CP60-qa002-cp60-short-host-mismatch-form.png) — explicit private consent and mismatch form for `127.1`.
- [`../evidence/CP60-noncanonical-hostname-boundary.json`](../evidence/CP60-noncanonical-hostname-boundary.json) — system resolver observations, denied registration, persisted state and zero-contact details.
- [`../evidence/CP43-org-only-access-members.png`](../evidence/CP43-org-only-access-members.png), [`../evidence/CP43-project-no-env-members-reload.png`](../evidence/CP43-project-no-env-members-reload.png), and [`../evidence/CP43-project2-members.png`](../evidence/CP43-project2-members.png) — original visually inspected scope contrast.
- Source path: `web/console/src/pages/access/AccessPage.tsx`, `refresh()` scope guard and organization-wide member list.

## Consolidated QA status

- **Historical DEF-001–015:** not retested in CP60. Preserve the QA-002 scoped record: DEF-001–004 and DEF-006–015 are verified fixed only for their documented acceptance cases; DEF-005 remains partial. QA-001 PASS is not inherited.
- **Confirmed open defects:** DEF-016–028 remain open. DEF-023’s implementation path is confirmed by source and CP43’s API-vs-UI evidence; DEF-024 is a separate ancestor-budget visibility failure. CP60 found no new confirmed defect.
- **Open observations:** QA-OBS-001 and QA-OBS-003–010 remain. QA-OBS-004 recurred on trailing-dot localhost with generic 502 feedback. QA-OBS-011 is added for undocumented legacy numeric host support (OS `getent` resolves `127.1`, runtime reports DNS resolution failure), with no security bypass evidence.
- **Coverage completed:** source-to-observation reconciliation for DEF-023; renewed visual review of the org/no-environment/environment member states; loopback numeric shorthand registration denial; trailing-dot localhost mismatching-CIDR sync; persisted server state and fixture no-contact checks.
- **Blocked:** fresh Team Admin retest because the prior disposable principal is suspended and its password cleared; restore/re-provisioning is required. CP53 webhook delivery still awaits an Operator credential. RC4 artifact remains UNVERIFIED.
- **Remaining:** broaden workspace context switching across all remaining screens and active permission changes; revisit DEF-005 rebinding/mixed-answer, HTTPS/chained redirects and alternate runtimes; independent DEF-001–015 retests where not fresh; remaining features, roles, browsers, integrations and recovery states.

Continue QA-002. `GATE-REL-07` remains **BLOCKED**.
