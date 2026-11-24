# CP67 — WebKit mobile A2A invalid-CIDR error parity

**Result:** FAIL — DEF-031 independently reproduced in the A2A registration form.  
**Baseline:** frozen RC4 source `40bba5b55c2da6fc00b107b315c9b3167fb20169`.  
**Release gate:** `GATE-REL-07` remains **BLOCKED**.

## Method and result

In WebKit at 375×844, I opened **MCP Hub → A2A Agents & Active Tasks → Register agent** as the synthetic Org Owner. I entered a unique QA slug/name, an isolated loopback URL, enabled private/loopback access, entered `127.0.0.1/33`, and submitted. The browser sent the invalid CIDR in `POST /admin/v1/a2a/agents`; the API returned HTTP 400 `invalid_request` with `allowed_cidrs must contain valid non-wildcard CIDR prefixes`. A follow-up list query confirmed no agent with the submitted slug was persisted.

I visually inspected the screenshot: the drawer remains open and the red error glyph is visible in the dimmed page behind it, but the message text is obscured by the drawer. DOM evaluation found the error outside the form; hit-testing its center returned the form's “Max hops” label. No agent endpoint was contacted, and no SSRF bypass occurred. This shows the same error placement/validation behavior affects both private MCP and A2A forms; retain one shared DEF-031 rather than duplicating it.

## Evidence

- `../evidence/CP67-webkit-a2a-invalid-cidr.json` — exact POST payload, 400 response, no-persistence check, and DOM hit-test result.
- `../evidence/CP67-webkit-a2a-invalid-cidr-result-375.png` — visually inspected A2A drawer and obscured error state.
- [DEF-031](../reports/DEF-031-mcp-cidr-validation-error-hidden-by-mobile-drawer.md) — consolidated report, expected/actual, acceptance criteria, and MCP evidence.
- Related unresolved security work: [DEF-005 handoff](../../../tanpopo/qa/engineering-handoffs/DEF-005-private-endpoint-cidr-input.md).

## Coverage update

DEF-031 remains Low (P3), expanded from MCP-only to both MCP and A2A private registration forms at this mobile viewport. DEF-005 remains PARTIAL: backend CIDR checks remain authoritative and fail-closed in this case. Other malformed CIDR classes, desktop error layering, keyboard/screen-reader behavior, DNS rebinding and mixed answers remain UNVERIFIED.

No product implementation, database, or runtime configuration was changed; no integration endpoint was contacted.
