# CP41 — MCP HTTP redirect SSRF boundary

**Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`  
**Result:** PASS for the scoped fail-closed redirect behavior; redirect support and broader SSRF cases remain UNVERIFIED.  
**Environment:** isolated SQLite runtime `127.0.0.1:18086`, Chromium through the local HTTPS proxy, synthetic loopback fixtures. No product source changes.

## Procedure

1. In the real Console as an instance-only Operator, registered a Streamable HTTP endpoint at `http://127.0.0.1:19119/mcp`, with private access explicitly enabled and only `127.0.0.1/32` allowed. Its fixture returned HTTP 307 to another same-CIDR endpoint at `127.0.0.1:19120/mcp`.
2. Clicked Sync tools for that exact server row. The Console/API returned HTTP 502 `mcp_sync_failed`; the origin logged one request, and the same-CIDR redirect target logged zero requests.
3. Registered the redirect target directly at `http://127.0.0.1:19120/mcp` with the same CIDR. Direct sync returned 200, discovered one tool, and the fixture recorded `initialize`, `notifications/initialized`, and `tools/list`. This verifies that the target fixture was healthy and reachable.
4. Registered a second origin at `http://127.0.0.1:19116/mcp` with only `127.0.0.1/32` allowed. Its 307 pointed to IPv6 loopback `[::1]:19118`, outside the configured CIDR. Sync again returned HTTP 502; the origin logged one request and the IPv6 target logged zero.
5. Repeated the cross-CIDR request and captured the response-time screenshot. The Console visibly reported “upstream MCP tool synchronization failed”; its registry row stayed Active with zero tools.

## Evidence and interpretation

- `../evidence/CP41-redirect-allow-filled.png`, `CP41-redirect-allow-sync.png` — same-CIDR redirect form and generic failed-sync state.
- `../evidence/CP41-direct-target-filled.png`, `CP41-direct-target-sync.png` — direct target positive control; screenshot visually inspected and showed one discovered tool plus the expected first-sync schema review lock.
- `../evidence/CP41-redirect-deny-filled.png`, `CP41-redirect-deny-sync-response.png` — cross-CIDR redirect and response-time screenshot, visually inspected.
- `../evidence/CP41-redirect-boundary.json` — sanitized HTTP outcomes, fixture request counts and persisted rows.

Both redirect targets received no requests. Since even a same-CIDR redirect was not followed, the evidence demonstrates that this tested client path fails closed on redirects. It does not demonstrate target-by-target revalidation after a redirect; redirects may simply be unsupported. No claim is made about other transports, redirect status codes, HTTPS redirects, redirect chains, DNS rebinding, public/private mixed DNS, or alternate runtimes.

## Classification

- **DEF-005:** scoped SSRF/redirect coverage PASS; overall finding remains PARTIAL PASS until remaining address-resolution and runtime permutations are tested.
- **Product defect:** none confirmed. An actionable redirect-specific error could improve diagnosis but is not raised here as a defect without a documented redirect-support contract. Generic 502/state behavior is already captured in QA-OBS-004.
- Directly configured private endpoint access succeeded; redirect-to-private target did not reach its fixture. This distinguishes app fail-closed behavior from a network/fixture failure.

The temporary Operator credential was cleared/suspended after testing, and redirect fixture processes/logs were removed. Synthetic MCP registry records remain only in the isolated QA database. GATE-REL-07 remains **BLOCKED**.
