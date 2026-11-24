# DEF-031 — Invalid MCP CIDR feedback is obscured behind registration drawer on mobile

**Severity:** Low (P3)  
**Status:** Confirmed on frozen QA-002 baseline `40bba5b55c2da6fc00b107b315c9b3167fb20169`.  
**Release gate:** `GATE-REL-07` remains **BLOCKED**.

## Summary

At 375×844 in WebKit, the MCP registration form accepts an invalid CIDR such as `127.0.0.1/33` and submits it to the API. The API correctly rejects the request with HTTP 400 and creates no server, but the user-facing error is rendered in the page behind the still-open registration drawer. The screenshot exposes only a clipped red error icon; the error text is covered by the opaque drawer. The field itself has no inline invalid state. This is a recoverable UX/validation defect in the unresolved DEF-005 private endpoint workflow. It did not bypass the SSRF/CIDR boundary.

## Reproduction

1. On the isolated RC4 Console, sign in as the synthetic Org Owner and open **MCP Hub → Register server** in WebKit at 375×844.
2. Enable **Allow private or loopback endpoint**. The `Allowed CIDRs` field appears. The real form has no document/body horizontal overflow at this width.
3. Enter unique slug/name values, a local endpoint such as `http://127.0.0.1:19996/mcp`, and invalid CIDR `127.0.0.1/33`. Do not use a reachable fixture.
4. Submit. The browser sends `POST /admin/v1/mcp/servers` with the invalid prefix. API responds HTTP 400 `invalid_request`, message `allowed_cidrs must contain valid non-wildcard CIDR prefixes`.
5. The registration drawer remains open. The error message is outside the dialog in the page layer. In the captured screenshot its text is behind the opaque drawer and only a clipped red icon is visible. DOM hit testing at the alert's center returns the drawer's endpoint label, confirming occlusion.
6. Fetch the server list: the attempted slug is absent. The invalid CIDR is rejected before server registration or endpoint contact.

## Expected versus actual

- **Expected:** Validate CIDR syntax in the form and show a clear, visible field-level message before submission; if the API rejects a value, display its error above the drawer or within the drawer, keep the user's entries, and identify the offending field.
- **Actual:** `127.0.0.1/33` passes the form's client-side path and is submitted. The API rejects it safely, but the error toast is rendered behind the open mobile drawer, leaving the user without readable feedback in the screenshot. No row persists.

## Impact

Users configuring private MCP access can be blocked by an invalid prefix without seeing why. They may retry or change unrelated fields, and the form's apparent failure is difficult to recover from on narrow screens. Backend protection remains effective; no SSRF or authorization bypass was observed. The same form's public-endpoint flow and the A2A invalid-CIDR flow were not retested in this checkpoint.

## Facts and hypothesis

**Facts:** real WebKit mobile request payload contained `allowed_cidrs:["127.0.0.1/33"]`; API returned 400 with the stated validation message; follow-up server list had no row for the slug. The screenshot shows the drawer covering the message. DOM evaluation located the alert outside the dialog; its center hit-tested to a drawer label.

**Hypothesis:** form validation delegates CIDR parsing to the backend and the shared page-level error toast uses a lower stacking layer than the mobile drawer. This likely explains both the lack of pre-submit validation and occluded response message; no source-level cause is asserted in this runtime report.

## Acceptance criteria

- Invalid, empty, host-bit, and overly broad CIDRs are rejected before POST with clear inline feedback, consistent with DEF-005's original acceptance checks.
- When a server-side validation error occurs, its complete text is visible and announced while the drawer remains open at mobile and desktop sizes.
- User-entered values remain intact after failure; correcting the CIDR allows a subsequent safe submission.
- API remains authoritative and rejects malformed/wildcard ranges; private access is never widened or inferred.
- Verify MCP and A2A forms independently, including keyboard and WebKit mobile operation.

## Evidence

- `../evidence/CP66-webkit-mcp-form.json` — toggle visibility, submitted malformed payload from the client-intercept preflight, and width measurements.
- `../evidence/CP66-webkit-mcp-form-private-off-375.png` — visually inspected mobile form with private CIDR disabled.
- `../evidence/CP66-webkit-mcp-form-private-on-invalid-375.png` — client-intercept preflight state; POST was aborted before reaching the runtime.
- `../evidence/CP66-webkit-mcp-invalid-cidr-result.json` — actual HTTP 400, exact payload, field/list state, and DOM hit-test occlusion evidence.
- `../evidence/CP66-webkit-mcp-invalid-cidr-result-375.png` — visually inspected mobile result; error copy is obscured behind drawer.
- [CP66](../checkpoints/CP66-webkit-mcp-cidr-form-validation.md) — method and scope limits.
- Related security/control issue: [DEF-005 handoff](../../../tanpopo/qa/engineering-handoffs/DEF-005-private-endpoint-cidr-input.md).

No product code, server configuration, database, or external endpoint was modified or contacted.
