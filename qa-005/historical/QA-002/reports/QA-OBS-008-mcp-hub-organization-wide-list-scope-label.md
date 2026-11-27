# QA-OBS-008 — MCP Hub shows organization-wide servers without project labels

- **Classification:** Unresolved UI/business-contract observation; not a confirmed defect or authorization finding
- **First evidence:** CP43

## Observation

A real ORG_OWNER session selected the secondary CP43 project/environment, but MCP Hub still displayed all seven MCP server rows registered under the first project. The organization-only view displayed the same seven rows. The visible table has no project column. A2A was empty in both tested contexts. Screenshots: `../evidence/CP43-org-only-mcp.png`, `../evidence/CP43-project2-mcp.png`, and `../evidence/CP43-org-only-a2a.png`.

The authenticated `GET /admin/v1/mcp/servers` returned the seven first-project server resources for the ORG_OWNER in both scopes. The endpoint is organization-wide for an actor with an organization binding. Server registration requires `project_id`; its current UI action targets the selected project. Thus, CP43 did not observe unauthorized access or a cross-tenant leak, and this may be an intentional organization registry.

## Uncertainty / follow-up

No normative UI contract located in this pass says whether MCP Hub is a global registry or a current-project view. The API response includes `project_id`, but the table omits it; the workspace selector remains visible and changes the target for new registration. This can make existing rows appear associated with the selected project and can conceal which project a Sync action affects.

Before classifying, confirm intended registry scope with product/API contracts, then test with a non-org project-scoped Team Admin and a second project containing its own server. If global, display the owning project and make action scope clear. If contextual, filter rows by selected project and ensure collection authorization remains fail-closed. Do not narrow valid ORG_OWNER authorization based on this observation alone.

## CP45 role-scoped comparison

CP45 added one MCP server record to the isolated secondary project through the real Console form using the reserved public example endpoint `https://example.com/mcp`; **Sync tools was never selected and the endpoint was not contacted**. As ORG_OWNER, the table contained eight rows, including the secondary-project row and seven first-project rows. The table still had no project column. A synthetic project-scoped TEAM_ADMIN assigned only to the first project then signed in: its scope menu listed only that project and No project, MCP Hub/API returned only the seven first-project records, and a direct budget collection request scoped to the secondary project returned 403 `insufficient_role_permission`. This is evidence that project-level authorization correctly filters the collection; no cross-project access by the Team Admin was observed. It also confirms the owner list is intentionally org-wide at the API authorization layer. The question whether the organization-wide table should label each row's project remains a presentation/contract uncertainty, not a security defect. See CP45 evidence `../evidence/CP45-owner-project2-mcp-created.png`, `../evidence/CP45-team-admin-project-options.png`, and `../evidence/CP45-team-admin-mcp-list.png`.
