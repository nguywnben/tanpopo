# QA-OBS-007 — Explicitly allowed IPv4-mapped IPv6 loopback is rejected

**Classification:** Contract/compatibility observation, not a confirmed defect.  
**Severity:** Not assigned.  
**Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`.

## Reproduction and actual behavior

As an instance-only Operator in the real Console, submitted a Streamable HTTP endpoint `http://[::ffff:127.0.0.1]:19125/mcp`, enabled private/loopback access, and supplied the exact narrow CIDR `::ffff:127.0.0.1/128`. The Admin API returned 403 `egress_private_ip_blocked` with “IPv4-mapped IPv6 address ::ffff:127.0.0.1 is forbidden”. No MCP server row was persisted and the controlled local fixture received no traffic. The response-time UI screenshot was visually inspected. Sanitized evidence: `../evidence/CP42-ipv4-mapped-address.json` and `../evidence/CP42-mapped-register-after-submit.png`.

## Expected behavior and uncertainty

The UI says private/loopback endpoints may be allowed with explicit CIDRs, but it does not enumerate supported IP representations. An IPv4-mapped IPv6 address semantically represents IPv4 loopback; the implementation explicitly refuses it even when the exact mapped address is allowlisted. This is fail-closed and blocks the request. It could be intentional hardening against normalization ambiguity. No documented acceptance criterion establishes that mapped notation must be accepted, so this is not classified as a product defect.

## Follow-up

Product/security owners should state whether endpoint configuration supports mapped IPv6 forms. If supported, normalize equivalent addresses consistently before the explicit-CIDR check; if intentionally forbidden, expose that restriction in endpoint help and API contract. Verify other mapped prefixes only after that contract is defined. Normal IPv4 loopback access and native IPv6 loopback access both succeeded in CP40–41, but do not resolve this representation-specific contract question.
