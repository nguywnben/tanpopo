# CP10 — MCP/A2A controls, Virtual Key plane boundaries, and input-required lifecycle

**Outcome:** PASS for the scoped historical findings DEF-005, DEF-007, DEF-010, and DEF-011. MCP execution and the wider RBAC, SSRF, multi-role, and A2A lifecycle matrices remain incomplete.

## Runtime separation and A2A enablement

All requests used the exact RC6 artifact recorded in CP01 on isolated local runtimes. Runtime `18124` retained the default A2A-disabled configuration. Runtime `18128` was a clone with A2A explicitly enabled for this test. Using the A2A-only synthetic Virtual Key against both:

- Default runtime: HTTP 404 `resource_not_found`, “A2A plane is disabled.”
- Explicitly enabled clone: HTTP 200 with the registered-agent listing.

The comparison is captured in `evidence/CP10-a2a-default-off-vs-enabled.json`. This is configuration behavior, not a product defect: it matches RC6's documented opt-in deployment contract. The synthetic A2A service was limited to `127.0.0.1:18127`; no public service or customer data was contacted.

## Private endpoint configuration (DEF-005)

In Chromium, opened the actual MCP and A2A registration forms and enabled private/loopback access. Both forms displayed a required Allowed CIDRs field. The A2A form rejected submission when private access had no explicit CIDR; the registry remained unchanged. With `127.0.0.1/32`, it accepted the synthetic loopback agent and persisted it Active. The MCP and A2A form screenshots and A2A readback are included in `evidence/CP10-*`.

This verifies the previously missing configuration-control slice. It does not establish resolver/redirect/DNS-rebinding resistance, IPv6 behavior, CIDR boundary handling, or full MCP discovery and invocation security.

## Provisioning and authorization (DEF-010)

Created an A2A-only Virtual Key from Access & Keys and retained its one-time secret outside the repository in a mode-0600 `/tmp` file. Using the key, A2A discovery returned HTTP 200. A separate PROVIDER-only key received HTTP 403 `scope_or_plane_denied` (“Virtual Key is not permitted on A2A plane”). Evidence contains outcomes only; key material is excluded. Authorization remains plane-specific.

## Actual input-required lifecycle (DEF-011)

The synthetic agent returned `input-required` to a real task create request. The task remained visible in the real A2A Console task table. Opened Resume, inspected the required follow-up-input form, supplied `Use period 2026-Q4.`, and submitted. The persisted task advanced to `completed` with a synthetic result artifact, and the refreshed Console showed Completed with no remaining task action. API lifecycle evidence is in `CP10-a2a-task-lifecycle.json` and `CP10-a2a-input-required-api.json`; the actual UI states are in the input-required, resume-form, and completed screenshots.

## Audit retest (DEF-007)

CP01's clean-install readback contains `organization.created`, `project.created`, and `environment.created`; the audit-chain verifier returned `intact: true`. This is a current-baseline scoped pass for the three successful bootstrap creations. Other create paths, actor attribution across roles, and atomicity/fault behavior remain unverified.

## Visual evidence review

The real Chromium screenshots were opened and visually reviewed during this phase. The private MCP/A2A CIDR controls are visible, the Resume form requires input, and the final task table visibly reports both tasks Completed. See `CP10-mcp-private-toggle.png`, `CP10-a2a-private-toggle.png`, `CP10-a2a-input-required-ui.png`, `CP10-a2a-resume-form.png`, and `CP10-a2a-resumed-completed-ui.png`.

## Remaining coverage

- MCP server registration, schema pinning, tool grants, tool calls, credential handling, revocation, and failure recovery.
- A2A cancellation, timeout/error, retries, asynchronous task/event behavior, owner/member role boundaries, cross-project scope, sub-budget holds, and regional mismatch.
- SSRF regression matrix for DNS resolution, redirects, rebinding, IPv6, CIDR overlaps, and endpoint mutation.
- Organization Owner, Team Admin, Security Reviewer, FinOps Viewer, Developer, and Operator access matrix.
- Broader provisioning audit mutations and injected-failure behavior.

These gaps remain `UNVERIFIED`; this checkpoint does not complete QA-004 or change GATE-REL-07.
