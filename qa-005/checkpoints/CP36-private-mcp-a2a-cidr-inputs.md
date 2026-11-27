# CP36 — DEF-005 MCP/A2A private-endpoint CIDR forms

**Baseline:** exact source `737079ddb847d97dbcf832f610ed0048556cb65b`; local Linux amd64 build SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`; isolated SQLite Console runtime `127.0.0.1:18145`; real Chromium 1440×1000; QA Owner / Operator. One existing synthetic MCP server and one synthetic A2A agent were visible before testing. No new endpoint was registered and no data-plane call was sent.

## Result: PASS (scoped)

In MCP Hub → Register server, toggled **Allow private or loopback endpoint**. An **Allowed CIDRs · Required** input appeared with an example of a narrow loopback CIDR. Filled otherwise valid synthetic server details targeting `127.0.0.1` while leaving CIDRs blank and submitted. The Console displayed “Private upstream access requires explicit non-wildcard CIDRs.” No POST to the MCP Admin API was issued, and no record was created.

Repeated the same interaction for MCP Hub → A2A Agents & Active Tasks → Register agent. The private toggle revealed the required Allowed CIDRs field. An otherwise valid synthetic loopback registration without CIDRs produced the same clear inline error and no A2A registration POST. No product SSRF or authorization boundary was bypassed.

This resolves the historical missing-input slice in both UI forms on exact RC7 and confirms client-side prevention of omission. It does not test invalid/wildcard CIDR recovery, multiple/IPv6 ranges, valid new MCP/A2A form submission in this run, keyboard/mobile behavior, DNS resolution/rebinding, or real protocol invocation. A2A data-plane capability was not inferred from the Console form test.

Screenshots visually inspected: `../evidence/screenshots/CP36-mcp-register-cidr-required.png` and `CP36-a2a-cidr-required.png`. Sanitized observations are in [`CP36-private-cidr-forms.json`](../evidence/CP36-private-cidr-forms.json); no secrets were recorded.
