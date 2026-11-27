# CP99 — DEF-031 mobile invalid-CIDR feedback

**Source/runtime:** frozen unsigned RC7 source `737079ddb847d97dbcf832f610ed0048556cb65b`; runtime binary SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`; exact-source SQLite at `127.0.0.1:18162`; Chromium 156 at 375×844; `ORG_OWNER` (`qa005-owner@example.invalid`).

## Result

**PASS (scoped mobile validation/feedback retest).** In both the MCP and A2A registration drawers, an invalid private endpoint allowlist was visibly rejected inside the open drawer. The MCP form contained `127.0.0.1/33`; the A2A form contained wildcard `0.0.0.0/0`. Each showed the inline message “Private upstream access requires explicit non-wildcard CIDRs.” No registration POST was emitted, readback returned HTTP 200 with zero matching records, and neither synthetic endpoint had a listener. The form did not submit invalid configuration or contact an upstream. Both screenshots were visually inspected.

## Reproduction

1. Sign in to the frozen exact-source Console as Owner and set Chromium viewport to 375×844.
2. Open MCP Hub → Register server. Enable private/loopback access, enter otherwise complete synthetic loopback fields and `127.0.0.1/33`, then submit.
3. Observe the error inside the still-open registration drawer. Confirm zero registration POSTs and query the server list for the synthetic slug.
4. Repeat in A2A Agents → Register agent with otherwise complete synthetic fields and `0.0.0.0/0`.
5. Confirm the same in-drawer error, zero POSTs, and zero matching agents in API readback.

Screenshots: [MCP invalid CIDR](../evidence/screenshots/CP99-def031-mcp-invalid-cidr-mobile.png) and [A2A wildcard CIDR](../evidence/screenshots/CP99-def031-a2a-invalid-cidr-mobile.png). Sanitized record: [CP99 JSON](../evidence/CP99-def031-mobile-invalid-cidr-feedback.json).

## Limits and reconciliation

This is a narrow, real Chromium mobile retest of visible client-side rejection and non-persistence; it does not show that the server independently rejects these exact malformed values. Host-bit, empty/missing, overlapping or overly broad non-wildcard ranges, IPv6, comma-separated entries, corrected-valid retry, keyboard-only interaction, desktop, WebKit/Firefox, other roles/backends, DNS rebinding, redirect and data-plane SSRF behavior remain UNVERIFIED. The earlier QA-001 observation that the form omitted a CIDR field and left server errors in the drawer is preserved as historical evidence. The current RC7 forms expose the control and visibly explain this rejection in the tested slice. `GATE-REL-07` remains BLOCKED.
