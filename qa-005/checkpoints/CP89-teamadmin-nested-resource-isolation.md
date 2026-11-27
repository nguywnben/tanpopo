# CP89 — Team Admin sibling-project nested resource isolation

**Source/runtime:** frozen unsigned RC7 source `737079ddb847d97dbcf832f610ed0048556cb65b`; Linux/amd64 binary SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`; isolated exact-source SQLite on `127.0.0.1:18162` through ephemeral loopback HTTPS `127.0.0.1:19443`; Playwright Chromium 156. Authentication used a pure synthetic `TEAM_ADMIN` bound to the QA005 No Budget Project. The Owner-only discovery/control reads confirmed that the sibling Initial Project had one environment, one MCP server, and one A2A agent before Team Admin probes.

## Result

**PASS (scoped, read-only authorization boundary).** Every exercised sibling-project or sibling-environment resource query was denied with HTTP 403 `insufficient_role_permission`; no resource rows were returned. The Team Admin’s unfiltered MCP server, A2A agent and A2A task lists each returned HTTP 200 with zero rows, despite Owner control reads confirming one MCP server and one A2A agent in the sibling project. No product writes or paid provider calls occurred.

## Reproduction

1. As the QA Owner, enumerate synthetic projects and the sibling project’s environment, then read MCP server and A2A agent collection metadata to establish that the sibling project contains one item in each collection.
2. Start a separate authenticated session as the pure Team Admin assigned only to `QA005 No Budget Project`.
3. Attempt read-only access to `GET /admin/v1/projects/{siblingProjectId}/environments`, `GET /admin/v1/models?env_id={siblingEnvId}`, `GET /admin/v1/deployments?project_id={siblingProjectId}&env_id={siblingEnvId}`, `GET /admin/v1/virtual-keys?project_id={siblingProjectId}&env_id={siblingEnvId}`, `GET /admin/v1/routes?env_id={siblingEnvId}`, and `GET /admin/v1/budgets?project_id={siblingProjectId}&include_ancestors=true`. All six returned HTTP 403 `insufficient_role_permission`.
4. Query `/admin/v1/mcp/servers`, `/admin/v1/a2a/agents`, and `/admin/v1/a2a/tasks` as the Team Admin. Each returned 200 with zero rows, while the Owner control reads returned one sibling-project MCP server and one sibling-project A2A agent.

Sanitized, count-only response evidence: [CP89 JSON](../evidence/CP89-teamadmin-nested-resource-isolation.json). This is not a full cross-project/tenant assurance: item-ID endpoints, writes, pagination, additional roles, organization-level sharing, other databases and browser engines remain open. No object content was retained. No product/specification changes. `GATE-REL-07` remains BLOCKED.
