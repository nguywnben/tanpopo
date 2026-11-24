# DEF-031 — Invalid MCP/A2A CIDR feedback is obscured behind registration drawer on mobile

**Severity:** Low (P3)  
**Status:** Confirmed on frozen QA-002 baseline `40bba5b55c2da6fc00b107b315c9b3167fb20169`.  
**Release gate:** `GATE-REL-07` remains **BLOCKED**.

## Summary

At 375×844 in WebKit, both MCP server and A2A agent registration forms accept an invalid CIDR such as `127.0.0.1/33` and submit it to the API. The API correctly rejects each request with HTTP 400 and creates no record, but the user-facing error is rendered in the page behind the still-open registration drawer. The screenshot exposes only a clipped red error icon; the error text is covered by the opaque drawer. The field itself has no inline invalid state. This is a recoverable UX/validation defect in the unresolved DEF-005 private endpoint workflow. It did not bypass the SSRF/CIDR boundary.

## Reproduction

1. On the isolated RC4 Console, sign in as the synthetic Org Owner and open **MCP Hub → Register server** in WebKit at 375×844.
2. Enable **Allow private or loopback endpoint**. The `Allowed CIDRs` field appears. The real form has no document/body horizontal overflow at this width.
3. Enter unique slug/name values, a local endpoint such as `http://127.0.0.1:19996/mcp`, and invalid CIDR `127.0.0.1/33`. Do not use a reachable fixture.
4. Submit. The browser sends `POST /admin/v1/mcp/servers` with the invalid prefix. API responds HTTP 400 `invalid_request`, message `allowed_cidrs must contain valid non-wildcard CIDR prefixes`.
5. The registration drawer remains open. The error message is outside the dialog in the page layer. In the captured screenshot its text is behind the opaque drawer and only a clipped red icon is visible. DOM hit testing at the alert's center returns the drawer's endpoint label, confirming occlusion.
6. Fetch the server list: the attempted slug is absent. The invalid CIDR is rejected before server registration or endpoint contact.
7. Repeat in **A2A Agents & Active Tasks → Register agent** with a unique slug and the same invalid CIDR. CP67 confirms the A2A POST also returns 400, the agent list has no new row, and the error text remains obscured behind that drawer.

### Independent Chromium mobile retest — CP118

At 375×844, real Chromium submitted A2A endpoint `http://127.0.0.1:19189` with the private-endpoint option enabled and `127.0.0.1/33` in the **Allowed CIDRs** field. The actual POST returned HTTP 400 `invalid_request` with `allowed_cidrs must contain valid non-wildcard CIDR prefixes`; a follow-up list remained at six agents and the unique slug was absent. No endpoint traffic occurred. The drawer retained the invalid input and remained open. Screenshot inspection confirms the red page-level error banner sits behind the open drawer: only its clipped icon is visible, while its text is obscured. The DOM/API contains the error text, but it is not visually readable. This independently reconfirms the A2A case in Chromium mobile in addition to WebKit CP67; it is not a new defect. See [CP118](../checkpoints/CP118-def031-chromium-mobile-a2a.md), `../evidence/CP118-a2a-mobile-invalid-cidr-form.png`, `../evidence/CP118-a2a-mobile-invalid-cidr-result.png`, and sanitized `../evidence/CP118-a2a-mobile-invalid-cidr.json`.

## Expected versus actual

- **Expected:** Validate CIDR syntax in the form and show a clear, visible field-level message before submission; if the API rejects a value, display its error above the drawer or within the drawer, keep the user's entries, and identify the offending field.
- **Actual:** `127.0.0.1/33` passes the form's client-side path and is submitted. The API rejects it safely, but the error toast is rendered behind the open mobile drawer, leaving the user without readable feedback in the screenshot. No row persists.

## Impact

Users configuring private MCP or A2A access can be blocked by an invalid prefix without seeing why. They may retry or change unrelated fields, and the form's apparent failure is difficult to recover from on narrow screens. Backend protection remains effective; no SSRF or authorization bypass was observed. Other CIDR classes and desktop behavior remain unverified.

## Facts and hypothesis

**Facts:** real WebKit mobile MCP and A2A request payloads contained `allowed_cidrs:["127.0.0.1/33"]`; both APIs returned 400 with the stated validation message; follow-up lists had no row for either slug. Both screenshots show the drawer covering the message. DOM evaluation located each alert outside its form; the alert center hit-tested to a drawer label.

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
- `../evidence/CP67-webkit-a2a-invalid-cidr.json` — A2A payload, API rejection, no-persistence check, and error occlusion.
- `../evidence/CP67-webkit-a2a-invalid-cidr-result-375.png` — visually inspected A2A mobile state.
- [CP67](../checkpoints/CP67-webkit-a2a-cidr-error-parity.md) — independent A2A parity retest.
- [CP118](../checkpoints/CP118-def031-chromium-mobile-a2a.md) — independent Chromium mobile A2A retest; confirms the error text remains behind the drawer after a real API rejection.
- Related security/control issue: [DEF-005 handoff](../../../tanpopo/qa/engineering-handoffs/DEF-005-private-endpoint-cidr-input.md).

No product code, server configuration, database, or external endpoint was modified or contacted.
