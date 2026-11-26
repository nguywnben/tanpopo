# CP25 — MCP Streamable HTTP sync, schema pin, grant and invocation

**Frozen source:** `737079ddb847d97dbcf832f610ed0048556cb65b`  
**Runtime:** local exact-source Linux binary, SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`; isolated SQLite at `127.0.0.1:18145`.  
**Browser and endpoint:** real Chromium/Playwright Console flows; deterministic loopback MCP Streamable HTTP mock `127.0.0.1:19093/mcp`; explicit private consent and `127.0.0.1/32`. No external provider or paid call.

## Result: PASS (scoped MCP read-only tool lifecycle)

1. Registered `qa005-mcp-fixture` through MCP Hub. The Console showed an active Streamable HTTP server with zero tools.
2. Ran Sync tools. The actual client completed `initialize`, `notifications/initialized`, and `tools/list`. One valid tool, `qa_status`, was stored with JCS hash `efc8ee5a…a9a28`. First discovery set the tool **Drift locked** pending schema approval, as expected for a newly observed tool.
3. As synthetic project Org Owner, reviewed the mock’s read-only schema, pinned the observed hash with `read_only` risk classification, and granted the namespaced tool to an MCP-only key whose allowed tool list contained only `qa005-mcp-fixture__qa_status`.
4. A cookie-free MCP Streamable HTTP client initialized protocol `2025-03-26` (HTTP 200), sent `notifications/initialized` (202), listed exactly the granted namespaced tool (200), and invoked it (200). The broker contacted only the local endpoint and returned the fixture’s structured `status=ready` result.
5. The MCP-only key’s attempt to call the PROVIDER plane returned HTTP 403 `scope_or_plane_denied`. Revoked the temporary key using Access & Keys; follow-up MCP initialization returned HTTP 401 `invalid_virtual_key`. The screenshot was visually inspected and shows the masked key as Revoked.
6. Exact-source `tanpopo admin verify-state` verified the SQLite audit chain as valid over 204 events with zero orphaned holds.

This is a scoped pass for one read-only MCP tool and a deterministic mock. Mutating-tool HITL/approval, malformed catalog pages, cursor pagination, denied-tool invocation, server credential injection, concurrent sessions, SSE responses/cancellation, key revocation during active calls, and external MCP interoperability remain unverified. The test-only server registration remains in the isolated QA database because the Console provides synchronization but no server-delete action. The loopback mock has been stopped; its endpoint record remains test-only and is not externally reachable.

Evidence: `evidence/CP25-mcp-dataplane.json`; screenshots `CP25-mcp-server-registered.png`, `CP25-mcp-tools-synced.png`, `CP25-mcp-schema-pinned.png`, `CP25-mcp-tool-grant.png`, and `CP25-mcp-key-revoked.png` (all visually inspected where rendered). Test key plaintext is excluded and revoked; its local mode-0600 file is removed after the next phase.
