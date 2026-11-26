# CP115 — MCP Hub inventory under organization and unrelated project scope

**Baseline:** frozen RC4 `40bba5b55c2da6fc00b107b315c9b3167fb20169`  
**Runtime:** isolated Tanpopo `18084`  
**Browser/role:** real Chromium, 1440 × 1000; QA Org Owner + Operator  
**Product implementation modified:** No  
**Release gate:** `GATE-REL-07 BLOCKED`

## Scope and reproduction

Read-only follow-up to CP114 to distinguish project-scoped MCP ownership from the Console's inventory presentation. In the actual workspace chooser, selected organization `QA-002 A2A Org` with **No project / No env**, navigated to MCP Hub, then selected the separate project `qa-002-a2a-project` with **No env** and returned to MCP Hub. Finally selected the rows' owning project `QA002 Audit mwto47wa` and its `stage` environment as a positive control. Visually inspected screenshots after all three scope states. No Sync, register, edit, grant, delete, or other resource mutation was sent.

## Results

The Org Owner view showed two MCP rows in both contexts: `qa002-cp114-dns-rebind` and `qa002-mcp-cidr`. In the separate project context the two rows still displayed **Sync tools** actions. The authenticated API request in each context was `GET /admin/v1/mcp/servers?limit=100` (HTTP 200) with no project filter. Both server records carried `project_id=prj_01m84ymb7we009px5hb5pfwsns`, which the authenticated project listing identifies as `QA002 Audit mwto47wa`. The selected separate project's ID is `prj_01m84xhbtwe00937gta0tjq86f`; neither returned server belongs to that project. Both server records have `environment_id=null`.

The table provides no owning-project column or per-row scope label. Under organization-only scope, the Register server button is disabled; under the separate project scope, it is enabled. No action was invoked on a row belonging to the other project. The account is an Org Owner authorized across both projects; this test found no authorization bypass and does not establish whether Org Owners are intentionally meant to administer an organization-wide MCP registry from every project context.

Screenshots visually inspected: `../evidence/CP115-mcp-org-only.png`, `CP115-mcp-sibling-project-no-env.png`, `CP115-mcp-owning-project-stage.png`, and the earlier repeat capture `CP115-mcp-owning-project-stage-initial.png`. The positive control displayed the same two rows in the project/environment associated with their API `project_id`. Sanitized API/project mapping is `../evidence/CP115-mcp-workspace-scope.json`; it contains request paths/statuses, row IDs/slugs/project IDs, selected project name/ID mapping, UI text, and no credentials or secret values.

## Disposition

**No new defect assigned.** This independently corroborates **QA-OBS-008**, the open MCP inventory-scope/ownership-label contract question already recorded in CP43/CP45. The evidence confirms that the owner UI/API intentionally or otherwise enumerates organization-wide rows without a project filter, while each row is project-associated. Without a normative contract, this cannot safely be classified as a confirmed functional defect. Lack of a scope label creates an operator ambiguity when the active project differs, especially because the row exposes an action; the authorized owner did not execute it. A Team Admin cross-project case remains outside this test.

The earlier CP114 rebinding server was registered into the isolated `QA002 Audit mwto47wa` project and remains in that isolated QA runtime because no supported MCP delete flow was established. No production or external workspace data was touched.

## Consolidated status summary

- **Historical DEF-001–015:** preserve scoped QA-002 retest outcomes only; no QA-001 PASS is inherited. DEF-005 remains PARTIAL: CP114 passed a limited separate-lookup DNS rebinding peer-boundary scenario, but mixed DNS answers, resolution/connection races, other redirect/transport/runtime cases, and A2A rebinding remain open. DEF-007 successful events/chains and populated shred do not cover injected audit/storage failure atomicity or independent backup/physical erasure proof.
- **Existing DEF-016–038:** unchanged and preserved with prior outcomes; confirmed high-impact issues and unresolved reports remain open as recorded. DEF-039 Low remains the latest confirmed product defect.
- **New defects:** none in CP115. QA-OBS-008 is further evidenced, not promoted without a product contract.
- **Coverage completed:** real Chromium Owner view in organization-only and unrelated project-only scopes, MCP list/API comparison, source-project mapping, refresh-free cross-scope navigation, and visual inspection. No mutations or foreign-project actions.
- **Coverage blocked/remaining:** Team Admin cross-project inventory/action authorization, contract for owner-visible organization-wide MCP rows, and actual action authorization when selected project differs remain unverified. Firefox/WebKit current-image execution is blocked by CP111. Broad feature, role, platform, integration, and operational coverage remains incomplete.
- **Recommended handoffs:** clarify whether MCP Hub is an organization-wide registry; if so, display each row's project and communicate scope. Preserve per-project authorization in action handlers. Continue DEF-005 and high-impact DEF-038/007, DEF-023/035/039, and remaining defect retests.

`GATE-REL-07` remains **BLOCKED**. QA-002 remains in progress.
