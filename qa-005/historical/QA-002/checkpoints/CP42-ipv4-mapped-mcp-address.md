# CP42 — IPv4-mapped IPv6 MCP endpoint

**Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`  
**Result:** mapped-address endpoint rejected fail-closed; support contract remains UNVERIFIED. No defect classified.  
**Runtime:** isolated SQLite `127.0.0.1:18086`, real Chromium Console, synthetic dual-stack loopback fixture. No product source changes.

## Reproduction and result

1. Authenticated in Chromium as an instance-only `OPERATOR` and opened MCP Hub → Register MCP server.
2. Entered endpoint `http://[::ffff:127.0.0.1]:19125/mcp`, checked “Allow private or loopback endpoint”, and explicitly entered `::ffff:127.0.0.1/128`.
3. Submitted through the real UI. Registration returned HTTP 403 `egress_private_ip_blocked` with message `IPv4-mapped IPv6 address ::ffff:127.0.0.1 is forbidden`. The response-time screenshot `../evidence/CP42-mapped-register-after-submit.png` was visually inspected; the form remained open and displayed the error.
4. SQLite contained no CP42 server row and the controlled dual-stack fixture logged zero requests. The same normal IPv4 loopback fixture path is reachable through the app as shown by CP41's direct target control.

## Classification and limits

This is fail-closed protection for the tested mapped address. The product explicitly rejects the address even when a narrow matching mapped CIDR is supplied. Whether mapped IPv6 notation is supported by the documented endpoint contract is not established; no defect is claimed. Other mapped prefixes, CIDR normalization, data-plane proxy behavior, and non-loopback forms remain UNVERIFIED.

Sanitized result: `../evidence/CP42-ipv4-mapped-address.json`. The synthetic Operator was suspended and its temporary password/hash cleared; the fixture and temporary scripts were removed. GATE-REL-07 remains **BLOCKED**.
