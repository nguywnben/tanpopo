# CP47 — MCP private endpoint sync, initial schema review, and project boundary

**Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`
**Result:** FAIL DEF-026; PASS for the scoped private-CIDR positive control and Team Admin cross-project denial.
**Product source changes:** none.

## Work performed

Using real Chromium UI interactions, the organization owner registered an isolated MCP fixture in project2 with `Allow private IP` enabled and `127.0.0.1/32`. Sync reached the fixture, performed MCP initialize/initialized/tools-list, and discovered one read-only synthetic tool. The real Console displayed the success notice “0 schema changes require approval” while the same server/tool were explicitly marked locked and offered an **Approve schema** action. After the owner reviewed and pinned the schema at `read_only`, the UI showed the tool pinned and server active. No `tools/call` or Virtual Key grant occurred.

A real Team Admin session scoped to project1 attempted the same sync against the project2 server. The API returned 403 `insufficient_role_permission`, and the loopback fixture had zero requests before and after the attempt. This supports a fail-closed project authorization boundary for this action.

Actual screenshots were inspected during the flow and after each state transition. The first-review dialog visibly identified the schema hash and default risk tier; the success state showed the pinned schema. See the CP47 screenshot files and sanitized JSON evidence linked below.

## Finding

- **DEF-026 — Low (P3), confirmed:** MCP sync success copy reports zero schema changes requiring approval even though a newly discovered schema is locked pending its initial review. This is a UI/business-state mismatch; the lock worked and no tool execution occurred.
- Standalone report: `../reports/DEF-026-mcp-initial-schema-approval-count-message.md`.
- Evidence: `../evidence/CP47-mcp-private-and-scope-boundary.json` and `../evidence/CP47-*.png`.

## Coverage disposition

- **PASS:** owner registration and sync to a specific loopback `/32`; synthetic discovery; first schema review/pin; Team Admin cross-project sync denied before endpoint contact.
- **FAIL:** sync banner does not distinguish first-discovery locks from schema drift approvals.
- **UNVERIFIED:** unchanged resync and actual drift after pin; grant/call/revoke for this endpoint; role variants; other CIDRs/runtimes; DNS rebinding; broader redirect behavior; IPv4-mapped notation; other browsers/platforms. Existing DEF-005 partial status remains partial and its SSRF/authorization controls must remain fail-closed.

## Evidence index

- `../evidence/CP47-owner-private-mcp-form.png`
- `../evidence/CP47-owner-private-mcp-registered.png`
- `../evidence/CP47-owner-private-mcp-synced.png`
- `../evidence/CP47-team-admin-scoped-mcp.png`
- `../evidence/CP47-owner-first-schema-approval-form.png`
- `../evidence/CP47-owner-first-schema-approved.png`
- `../evidence/CP47-mcp-private-and-scope-boundary.json`

The endpoint was synthetic and bound to loopback. Temporary Team Admin authentication was removed after the authorization attempt; the QA principal remains suspended. No secrets, cookies, database, or raw service logs are included.
