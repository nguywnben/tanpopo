# CP114 — DNS rebinding with exact private CIDR authorization

**Baseline:** frozen RC4 `40bba5b55c2da6fc00b107b315c9b3167fb20169`  
**Runtime:** isolated Tanpopo `18084`  
**Browser/role:** real Chromium, 1440 × 1000; QA Org Owner + Operator  
**Product implementation modified:** No  
**Release gate:** `GATE-REL-07 BLOCKED`

## Objective and controlled setup

Extend DEF-005's SSRF/private-endpoint investigation with a DNS rebinding case, while preserving the explicit allowlist and without weakening authorization. In the actual MCP Hub form, registered a synthetic Streamable HTTP endpoint using `http://7f000001.7f000002.rbndr.us:19189/mcp`, enabled the explicit private/loopback exception, and set the sole allowed CIDR to `127.0.0.1/32`. This test hostname rotated DNS answers between `127.0.0.1` (the exact allowed fixture) and `127.0.0.2` (a separately bound, live but disallowed fixture). It returned one address per sampled DNS lookup; this did not test a single mixed-address DNS response.

Both fixtures were ephemeral local HTTP servers on port 19189. They accepted MCP initialization and `tools/list`, returned zero tools, and logged only method, path, and Host header. Request bodies/payloads were not retained. The only endpoint action was read-only discovery. No tool was granted or called, and no provider credentials or external services were used.

## Reproduction and actual results

1. In the selected project/environment, open **MCP Hub → Register server**.
2. Enter the hostname and `/mcp` path above, enable **Allow private or loopback endpoint**, enter exactly `127.0.0.1/32`, and register.
3. Confirm the HTTP 201 registration and persisted row.
4. Use the actual **Sync tools** button eight times while DNS rotates between the two loopback addresses. Capture each app API response, before/after DNS samples and fixture request counts.
5. Compare the two fixture logs and inspect the actual screenshots.

Registration returned HTTP 201 and persisted the MCP row. Of eight discovery attempts, one returned HTTP 200 and seven HTTP 502. DNS samples before/after the attempts showed both answers over the test interval. The allowed `127.0.0.1` fixture received nine MCP protocol requests in total; the disallowed `127.0.0.2` fixture received zero. The successful sync discovered zero tools, as configured. The record remained Active with zero tools after refresh. The seven 502 outcomes are expected on attempts where the resolver supplied or shifted to the disallowed address during discovery; their generic UI message is not evidence of a bypass or a new defect.

Real screenshots were visually inspected: `../evidence/CP114-dns-rebind-registration-form.png`, `CP114-dns-rebind-registered.png`, `CP114-dns-rebind-sync-1.png` (visible failure), `CP114-dns-rebind-sync-7.png` (visible successful zero-tool result), and `CP114-dns-rebind-final-state.png` (visible final failure and persisted row). The sanitized per-attempt record is `../evidence/CP114-dns-rebinding.json`.

## Disposition and limits

**Scoped PASS:** the actual integration reached only the explicitly allowed loopback peer; the separately live, disallowed loopback peer received no requests while DNS changed between lookups. The fail-closed 502 states and positive control were both observed in the real UI and corroborated by local fixture logs. No SSRF bypass was found; no authorization boundary was relaxed.

**DEF-005 remains PARTIAL.** This evidence does not cover a single DNS response containing both allowed and disallowed addresses, resolver/cache behavior outside this rbndr.us rotation pattern, HTTP redirect chains (including other status codes and HTTPS), IPv4-mapped support, alternate transports, A2A on this rebinding scenario, alternate runtime deployments, or concurrency between resolution and connection. Rebinding during multiple protocol requests may yield a 502 even after earlier requests reached the permitted fixture. Determine whether this is a supported/expected endpoint availability behavior separately from the security boundary.

No new defect is assigned. The registered synthetic row remains in isolated QA data because no supported delete flow was established; the ephemeral listeners were stopped after the run and no listener remains on port 19189. Existing SSRF, CIDR, and authorization checks remain intact.

## Status summary

- Historical defects DEF-001–015: limited scoped QA-002 outcomes remain as recorded; no QA-001 PASS is inherited. DEF-005 remains partial; DEF-007 success-path presence/chain results do not cover audit failure atomicity, storage faults, or independently verifiable erasure.
- Existing defects DEF-016–038: preserved with prior dispositions; CP114 changes none. DEF-039 Low remains the latest confirmed Console scope issue from CP113.
- New defects: none in CP114.
- Coverage completed: real Chromium registration and repeated discovery through a rebinding hostname; exact `/32` positive peer versus a live disallowed peer; actual UI response states, persistence, fixture request logs, DNS samples, and visual screenshot inspection.
- Coverage blocked or remaining: mixed-answer DNS, resolver/connection race, redirect/status variants, other transports and runtimes, A2A rebinding, broader role/browser/platform/security scenarios. Firefox/WebKit current-image execution remains blocked by CP111; many feature and integration journeys remain untested.
- Recommended handoffs: no new handoff from CP114. Keep DEF-005 in the security review as partial coverage and preserve strict peer authorization. Continue high-priority DEF-038/007, DEF-023/035/039 scope, and other confirmed defects.

`GATE-REL-07` remains **BLOCKED**. QA-002 remains in progress.
