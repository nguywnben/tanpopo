# CP11 — SSRF boundaries and MCP/A2A registration

## Result

CP11 records a scoped pass for loopback SSRF controls and registration-form validation. It does not close DEF-005 or establish full MCP/A2A coverage. `GATE-REL-07` remains **BLOCKED** and QA-003 remains in progress.

## Provider connection behavior

On the frozen RC5 runtime, the New provider connection drawer began with private/loopback access unchecked. Submitting `http://127.0.0.1:19189` without enabling it returned HTTP 400 `ssrf_blocked_private_ip`; no new row was created (three rows before and after). The red inline error was visually inspected in [the denied screenshot](../evidence/CP11-ssrf-private-denied.png), and the unchecked form state is shown in [the form screenshot](../evidence/CP11-ssrf-private-disabled.png).

With the explicit private endpoint option enabled and a specific loopback `/32` allowed, the synthetic local redirect fixture received `GET /v1/models` and returned a redirect to `127.0.0.2`. The second loopback trap received no request. Probe returned HTTP 200 with `valid=false`, `upstream_unreachable`; the connection stayed `CREATED_UNVERIFIED`. This is evidence that this tested redirect path did not reach the second address, not proof of complete redirect/DNS/race safety. See [machine evidence](../evidence/CP11-ssrf-loopback.json) and the [visually inspected probe result](../evidence/CP11-ssrf-redirect-probe.png).

## MCP and A2A registration

Using real Chromium form interactions, the MCP and A2A private endpoint forms each rejected `0.0.0.0/0` with visible “explicit non-wildcard CIDRs” feedback, and did not submit that invalid form. Replacing it with `127.0.0.1/32` allowed registration: MCP `POST /admin/v1/mcp/servers` and A2A `POST /admin/v1/a2a/agents` returned HTTP 201, and both rows appeared in the UI as Active. Registration success screenshots and invalid-CIDR screenshots were visually inspected: [MCP rejected wildcard](../evidence/CP11-mcp-invalid-cidr.png), [MCP registered](../evidence/CP11-mcp-private-registered.png), [A2A rejected wildcard](../evidence/CP11-a2a-invalid-cidr.png), and [A2A registered](../evidence/CP11-a2a-private-registered.png). See [request/readback evidence](../evidence/CP11-mcp-a2a-cidr.json).

The create responses and list readbacks do not include the CIDR values. Therefore the evidence establishes client validation plus successful creation after entering an exact CIDR, but does not independently prove stored CIDR persistence or enforcement on later MCP/A2A network requests. Those checks remain open. No wildcards were registered.

## DEF-005 disposition

**PARTIAL, positive scoped result.** Its original omission is not reproduced in these desktop Chromium forms. Residual acceptance: stored allowlist persistence and enforcement, mobile error visibility, mixed DNS answers, DNS rebinding, connection-time address pinning/races, redirects across public/private address transitions, and MCP/A2A transport parity. The synthetic DNS/rebinding scenario has not been executed and is not marked passed. Existing private-address authorization controls were not weakened.

## Coverage changes

- FT-07, FT-08, SCR-10: **PARTIAL** for registration forms and create/list appearance only. MCP discovery, schema pinning, grants and approvals are untested; A2A discovery interaction and task authorization/lifecycles are untested.
- FT-11: **PARTIAL** for denied private provider creation, explicit loopback allow, and the tested redirect behavior only.
- Browser used: Chromium desktop 1440×1000. Mobile/browser parity is not established by this checkpoint.

## Evidence hygiene

Evidence contains only synthetic local endpoints and test tenant state. No credentials or production data are included. The database remains local and is not part of the handoff.
