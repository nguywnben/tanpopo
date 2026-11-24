# CP118 — DEF-031 A2A invalid CIDR feedback in Chromium mobile viewport

**Baseline:** frozen RC4 `40bba5b55c2da6fc00b107b315c9b3167fb20169`  
**Runtime:** isolated Tanpopo `18084`  
**Browser/role:** real Chromium mobile emulation, 375 × 844; QA Org Owner + Operator  
**Product implementation modified:** No  
**Release gate:** `GATE-REL-07 BLOCKED`

## Reproduction and result

1. At 375 × 844, opened the actual navigation menu, went to MCP Hub → A2A Agents & Active Tasks → Register agent.
2. Entered unique synthetic values and endpoint `http://127.0.0.1:19189`, enabled **Allow private or loopback endpoint**, and entered invalid `127.0.0.1/33` in the actual **Allowed CIDRs** field.
3. Submitted once. The API returned HTTP 400 `invalid_request`, `allowed_cidrs must contain valid non-wildcard CIDR prefixes`.
4. Read the A2A collection; it remained at six agents and the attempted slug was absent. No endpoint sync/discovery, task, key grant, or network call occurred.

The screenshot was visually inspected. The drawer remains open and preserves entered values, but the red page-level error banner is rendered behind the opaque drawer: at this narrow viewport only its clipped error icon is visible at the drawer's left edge; the validation text cannot be read on screen. The text exists in the page body/API evidence, but DOM presence does not make it visually available to the user. There is no inline error beside the CIDR field.

Evidence: `../evidence/CP118-a2a-mobile-invalid-cidr-form.png`, `CP118-a2a-mobile-invalid-cidr-result.png`, and sanitized `CP118-a2a-mobile-invalid-cidr.json`. The JSON attempt confirms HTTP status/message, unchanged list count, no matching row, and no endpoint call. The initial automation attempt used a broad locator that filled the endpoint URL instead of the CIDR field; the browser rejected that malformed URL locally and emitted no POST. It is excluded from product results and preserved as `CP118-excluded-locator-misfill-*` evidence.

## DEF-031 disposition

**FAIL — DEF-031 Low independently reconfirmed in Chromium mobile A2A.** This broadens the earlier WebKit mobile evidence (CP66/67) to a second browser engine and directly confirms error occlusion after a real 400 response. Server-side CIDR validation remains fail-closed and no row persists. No SSRF or authorization boundary was weakened.

This does not test MCP in Chromium mobile, correction-and-resubmit, keyboard/screen-reader announcement, desktop visibility, other invalid CIDR types, or Firefox/WebKit on the current managed image. Existing DEF-031 acceptance criteria remain in [the report](../reports/DEF-031-mcp-cidr-validation-error-hidden-by-mobile-drawer.md).

## Consolidated status summary

- **Historical DEF-001–015:** retain scoped QA-002 outcomes only; no QA-001 PASS is inherited. DEF-005 remains PARTIAL after CP114/116/117 security boundary cases. DEF-007 successful event/chain and crypto-shred cases do not prove failure atomicity, injected-storage behavior, or independent backup/physical erasure.
- **Existing DEF-016–039:** preserved with prior dispositions. CP118 independently reconfirms DEF-031 Low; no new defect ID is added.
- **Coverage completed:** actual A2A invalid-CIDR submission in a 375 × 844 Chromium viewport, HTTP 400 and no-persistence check, no endpoint traffic, and visual inspection of the drawer/error layering.
- **Coverage blocked/remaining:** correction/recovery and accessible announcement; MCP/other invalid-value cases; current Firefox/WebKit runs remain blocked by CP111. Broader role/browser/platform/full-product gaps remain.
- **Recommended handoffs:** add CP118 evidence to the DEF-031 engineering handoff; show CIDR validation beside the field or in a layer above the drawer and announce it accessibly. Preserve the server-side rejection. Continue all open defect retests and DEF-005 residuals.

`GATE-REL-07` remains **BLOCKED**. QA-002 remains in progress.
