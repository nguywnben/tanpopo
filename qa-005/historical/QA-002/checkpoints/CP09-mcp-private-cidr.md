# CP09 — MCP private endpoint CIDR registration

**Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`  
**Runtime:** Authenticated Chromium Console on isolated SQLite server `127.0.0.1:18084`; disposable synthetic registration only. No implementation source changed.

## DEF-005 scoped retest

On MCP Hub → MCP servers → Register server, entered a synthetic server at `http://127.0.0.1:19105/mcp` and enabled “Allow private or loopback endpoint.” The real form showed required Allowed CIDRs. Submitting `0.0.0.0/0` produced the visible error “Private upstream access requires explicit non-wildcard CIDRs”; observed MCP server POST count remained zero. With the same form, replacing the value with `127.0.0.1/32` submitted and returned HTTP 201. The success notice appeared and a fresh server list showed `QA002 synthetic MCP CIDR`, slug `qa002-mcp-cidr`, and the local endpoint. Screenshot evidence: `../evidence/CP09-mcp-cidr-wildcard.png`, `CP09-mcp-cidr-valid.png`, `CP09-mcp-cidr-persisted.png`. All three were visually inspected during the run.

Read-only SQLite state corroborated `allow_private_ip=1`, `allowed_cidrs_json=["127.0.0.1/32"]`, `transport_kind=STREAMABLE_HTTP`, active state, project and org scope, version 1. Sanitized snapshot: `../evidence/CP09-mcp-cidr.json`.

**Result: PARTIAL PASS.** The historical omission of CIDR input did not reproduce for either A2A (CP08) or MCP (this checkpoint); both real registration forms rejected wildcard CIDR and persisted the explicit loopback range. Runtime SSRF/endpoint gating, tool sync, schema pinning, grants, and invocation were not tested. The existing fixture is a synthetic A2A endpoint and is not a valid Streamable HTTP MCP service; therefore those checks remain UNVERIFIED, and no inference is made from the registration response about actual endpoint network policy.

GATE-REL-07 remains **BLOCKED**; QA-002 is in progress.
