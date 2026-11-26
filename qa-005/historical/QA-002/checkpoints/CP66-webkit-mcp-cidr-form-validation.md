# CP66 — Mobile WebKit MCP CIDR form validation

**Result:** FAIL — DEF-031 Low confirmed; DEF-005 remains open with secure backend rejection.  
**Baseline:** frozen RC4 source `40bba5b55c2da6fc00b107b315c9b3167fb20169`.  
**Release gate:** `GATE-REL-07` remains **BLOCKED**.

## Method and observations

In WebKit at 375×844, I opened the real **MCP Hub → Register server** drawer as the synthetic Org Owner. With private access disabled, no CIDR field appeared. Enabling **Allow private or loopback endpoint** exposed `Allowed CIDRs`. Viewport/document/body widths were each 375px; no horizontal overflow was measured. I visually inspected the off and invalid-submit screenshots.

I submitted an isolated local endpoint URL and invalid `127.0.0.1/33`. The browser request carried the invalid CIDR and the API returned HTTP 400 `invalid_request` with `allowed_cidrs must contain valid non-wildcard CIDR prefixes`. The attempted server slug did not appear in the server list. No fixture was contacted. The drawer remained open, but the page-level error was underneath the opaque drawer. DOM hit testing at the alert center returned the drawer's endpoint label, corroborating the screenshot. The user could see a clipped red icon but not the full error copy.

This confirms DEF-031 Low: mobile validation/error feedback is hidden, while backend security validation is functioning. It does not represent an SSRF bypass. The A2A form, other malformed prefix classes, and desktop-specific alert placement remain unverified.

## Evidence

- `../evidence/CP66-webkit-mcp-form.json` — live toggle behavior, invalid request payload intercepted during preflight, and mobile width measurement.
- `../evidence/CP66-webkit-mcp-form-private-off-375.png` — private-endpoint option off, CIDR field absent; visually inspected.
- `../evidence/CP66-webkit-mcp-form-private-on-invalid-375.png` — client-intercept preflight: form filled with `/33`; the harness aborted the POST before it reached the runtime to preserve a nonmutating validation check. The following actual request/400 is separately captured below.
- `../evidence/CP66-webkit-mcp-invalid-cidr-result.json` — actual API response, no-persistence check, and DOM hit-test proving the error is behind the drawer.
- `../evidence/CP66-webkit-mcp-invalid-cidr-result-375.png` — actual response state visually inspected.
- [DEF-031](../reports/DEF-031-mcp-cidr-validation-error-hidden-by-mobile-drawer.md) — reproduction, impact, hypothesis, and acceptance criteria.
- Related remaining security coverage: [DEF-005](../../../tanpopo/qa/engineering-handoffs/DEF-005-private-endpoint-cidr-input.md).

## Coverage update

DEF-005 remains PARTIAL: prior narrow allowlist controls passed and tested redirects/private CIDR mismatches failed closed; DNS rebinding, mixed answers, alternate runtimes, and remaining transport/host contracts remain unverified. CP66 adds a validated UI error-path failure without weakening the boundary. Continue with A2A form parity and DNS behavior when the environment permits.

No implementation or runtime configuration was changed; no registered integration endpoint was contacted.
