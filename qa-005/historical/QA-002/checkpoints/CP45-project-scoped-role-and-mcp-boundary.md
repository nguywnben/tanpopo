# CP45 — Project-scoped Team Admin and MCP collection boundary

- **Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169` (RC4)
- **Result:** PASS for the tested project-scoped MCP and API read boundaries; QA-OBS-008 remains an organization-owner UI-label uncertainty. No new product defect was established in this phase.
- **Environment:** isolated SQLite runtime at `127.0.0.1:18086`; real Chromium over local HTTPS proxy at `127.0.0.1:18445`; synthetic owner and Team Admin principals.
- **Product source changes:** none. No test suite was run.

## Actions and evidence

1. As ORG_OWNER, selected the secondary CP43 project and registered an MCP server row pointing at reserved example endpoint `https://example.com/mcp`. The row was not synced; no request was sent to the endpoint. Screenshot `../evidence/CP45-owner-project2-mcp-created.png` was visually inspected.
2. As ORG_OWNER, opened Access & Keys and created a project-scoped TEAM_ADMIN assigned only to the first project through the Console form. Screenshots `../evidence/CP45-team-admin-add-member-form.png`, `../evidence/CP45-team-admin-assignment-form.png`, and `../evidence/CP45-team-admin-member-created.png` were visually inspected. As elsewhere in the isolated RC4 environment, normal local enrollment is not available (DEF-021); a temporary password hash was provisioned out-of-band for this synthetic authorization-only session, then cleared and the account suspended at the end of CP45.
3. Signed in as the synthetic Team Admin. Its project chooser showed only the first project and “No project”; it did not list the second project. Screenshot `../evidence/CP45-team-admin-project-options.png` was visually inspected.
4. Team Admin opened MCP Hub; the Console and same-session `GET /admin/v1/mcp/servers` returned the seven first-project servers, excluding the secondary-project row. Screenshot `../evidence/CP45-team-admin-mcp-list.png` was visually inspected.
5. Same-session direct `GET /admin/v1/budgets?project_id=<secondary-project>` returned `403 insufficient_role_permission`; the selected-project FinOps view returned the two first-project budget rows. Screenshot `../evidence/CP45-team-admin-finops.png` was visually inspected. No budget write was attempted.
6. Owner-only MCP view returned eight rows, including the new secondary-project row, while the organization-wide table did not identify each row's project. This confirms authorized org-wide enumeration and retains QA-OBS-008 as a UI/contract question. Team Admin did not access the second project's row; do not weaken existing authorization.

## Consolidated defect status

- **Historical DEF-001–015:** DEF-001–004 and DEF-006–015 remain verified fixed only for the documented QA-002 post-baseline scenarios; DEF-005 remains partial with explicit CIDR controls passed and redirect, DNS rebinding, mapped-address support, alternate runtimes still open.
- **Confirmed open defects:** DEF-016–025, including CP43 DEF-023/024 and CP44 DEF-025. The project-scoped authorization checks in this phase passed and do not change any finding's status.
- **Observations:** QA-OBS-001, QA-OBS-003–008 remain open; CP45 supports the role-filtering/authorization portion of QA-OBS-008 but does not establish whether owner-visible rows should be project-labelled.
- **Recommended handoff:** keep DEF-016–025 in engineering intake per [status summary](../STATUS-SUMMARY.md); retain QA-OBS-008 for product contract clarification. DEF-005 remains fail-closed security validation.

## Coverage

- **PASS:** real UI member provisioning for one Team Admin, role-specific project chooser, first-project MCP list, response row project IDs, and explicit denial for a different project's budget query.
- **BLOCKED:** none in this phase.
- **Remaining:** other project roles/actions, Team Admin create/update attempts against project2 across all APIs, full org/project/environment role matrix, member list refresh under DEF-016, first-login enrollment without out-of-band password provisioning, and other browser/platform modes. Prior evidence and gaps remain in [coverage matrix](../COVERAGE-MATRIX.md).

`GATE-REL-07` remains **BLOCKED**. QA-002 continues.
