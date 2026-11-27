# CP117 — MCP cross-CIDR HTTP 301/308 redirect boundary

**Baseline:** frozen RC4 `40bba5b55c2da6fc00b107b315c9b3167fb20169`  
**Runtime:** isolated Tanpopo `18084`  
**Browser/role:** real Chromium, 1440 × 1000; QA Org Owner + Operator  
**Product implementation modified:** No  
**Release gate:** `GATE-REL-07 BLOCKED`

## Controlled endpoints

Started two temporary local MCP fixtures: an allowed origin on `127.0.0.1:19190` and a live disallowed redirect target on `127.0.0.2:19191`. The origin returned HTTP 301 on its first `POST /mcp` and HTTP 308 on its second, each with `Location: http://127.0.0.2:19191/mcp`. The target would respond with an empty initialize result if contacted. Both fixtures logged only method/path/status and retained no request bodies. Both listeners were stopped after the test.

## Real user workflow and results

1. In the real Console, selected `QA002 Audit mwto47wa / stage`, opened MCP Hub → Register server, and registered `qa002-cp117-redirect-301-308` at the allowed origin with the explicit private endpoint option and exact `127.0.0.1/32` allowlist.
2. Registration returned HTTP 201 and the row persisted in the MCP table.
3. Clicked **Sync tools** twice. The first origin response was 301; the second was 308. Both sync calls returned HTTP 502 and displayed `upstream MCP tool synchronization failed`.
4. The allowed origin received exactly one POST on each attempt. The live disallowed redirect target received zero requests across both attempts. The row remained Active with zero tools. No tool call or grant occurred.

Screenshots were visually inspected: `../evidence/CP117-redirect-form-allowed-origin.png`, `CP117-redirect-registered.png`, `CP117-redirect-301-result.png`, `CP117-redirect-308-result.png`, and `CP117-redirect-final-state.png`. Sanitized fixture and Admin API events are in `../evidence/CP117-redirect-301-308.json`.

## Disposition and limits

**PASS, scoped for cross-CIDR denial:** with only `127.0.0.1/32` authorized, the MCP HTTP client did not follow either 301 or 308 to the live `127.0.0.2` target. This complements CP41's 307 and CP50's 302/303 failures and CP114's exact-peer DNS rebind check. No SSRF bypass was found; no boundary was relaxed.

**DEF-005 remains PARTIAL.** This verifies these two status codes for MCP Streamable HTTP in runtime 18084 only. It does not establish whether redirect following is a supported contract, per-hop revalidation under every status/transport, HTTPS downgrade behavior, redirect loops/relative locations/auth headers, other runtime versions, A2A redirects, or resolver/connection race behavior. QA-OBS-004's generic UI message recurred; it is not a new defect in this test.

The row remains in isolated QA data because a supported delete route has not been established. No external network host or provider credential was used.

## Consolidated status summary

- **Historical DEF-001–015:** limited scoped QA-002 results only; no QA-001 PASS is inherited. DEF-005 stays PARTIAL after CP114/116/117 scoped enforcement cases. DEF-007 audit success paths still do not establish failure atomicity, injected-storage behavior, or independent backup/physical erasure. Other dispositions remain as in the defect register.
- **Existing DEF-016–039:** preserved unchanged. DEF-039 Low remains the latest confirmed defect; CP115 continues QA-OBS-008 without assigning a new defect.
- **New defects:** none in CP117.
- **Coverage completed:** real Chromium authorized-origin registration, 301/308 cross-CIDR redirects, actual sync UI/API results, origin/target request counts, persistence state, and screenshot inspection.
- **Coverage blocked/remaining:** HTTPS and relative redirect cases, redirect semantics across additional transports and A2A, full per-hop/race analysis, other runtimes, roles, browsers and platforms, plus broad full-product gaps. Firefox/WebKit current-image execution remains blocked by CP111.
- **Recommended handoffs:** no new handoff; preserve fail-closed cross-CIDR behavior and clarify redirect contract. Continue open high-impact DEF-038/007, scope defects DEF-023/035/039, DEF-005 residuals, and all remaining defect retests.

`GATE-REL-07` remains **BLOCKED**. QA-002 remains in progress.
