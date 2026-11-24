# CP50 — MCP HTTP 302/303 redirect boundary

**Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`
**Result:** PASS for scoped fail-closed behavior; no new defect identified.
**Product source changes:** none.

## Work performed

As the synthetic organization owner in real Chromium, registered two Streamable HTTP MCP origins using the Console with private access enabled and only `127.0.0.1/32` allowed. The loopback origin returned HTTP 302 or HTTP 303 to a separately instrumented loopback target. Sync was invoked through each server row's **Sync tools** action.

Both sync requests returned HTTP 502 and the Console showed “upstream MCP tool synchronization failed.” Each origin recorded one `initialize` request; the instrumented redirect target recorded zero requests. No tools were persisted and neither response was followed. The origin and target were isolated synthetic fixtures. Screenshots were visually inspected during form entry and after each failed sync.

CP41 had already established that the same target can be contacted directly when registered as the endpoint, so the current result is consistent with a redirect being refused rather than a target fixture or egress failure. It does not establish per-hop target validation because the client did not follow the redirects.

## Evidence

- `../evidence/CP50-mcp-redirect-status-cases.json`
- `../evidence/CP50-redirect-302-form.png`
- `../evidence/CP50-redirect-302-sync.png`
- `../evidence/CP50-redirect-303-form.png`
- `../evidence/CP50-redirect-303-sync.png`
- CP41 direct-target positive control: `CP41-mcp-redirect-ssrf-boundary.md` and `../evidence/CP41-redirect-boundary.json`.

## Coverage disposition

- **PASS:** both tested HTTP redirect statuses failed closed before target contact; no tool or mutation resulted.
- **UNVERIFIED:** redirect support contract, HTTPS redirects, chains, 301/307/308 fresh retests, DNS rebinding, mixed DNS results and alternate runtime behavior. DEF-005 remains PARTIAL PASS; retain existing SSRF and authorization boundaries.
- **Product defect:** none confirmed. Generic error diagnostics remain tracked as QA-OBS-004.

`GATE-REL-07` remains **BLOCKED**. QA-002 continues.
