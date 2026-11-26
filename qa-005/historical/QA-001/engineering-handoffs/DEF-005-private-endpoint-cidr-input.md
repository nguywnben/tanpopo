# Engineering handoff — DEF-005: private MCP/A2A registration cannot supply allowed CIDRs

## Impact

The Console exposes a private/loopback endpoint toggle for both MCP server registration and A2A agent registration, but neither form lets an operator specify the CIDR allowlist required by the Admin API. Submitting the private form is rejected, so private integration registration, discovery, and downstream governance journeys cannot be tested or used through the Console.

## Reproduction

1. Sign in as an isolated Org Owner and open MCP Hub → Register server.
2. Enter the required slug, name, and endpoint; enable “Allow private or loopback endpoint.”
3. Submit. The Console request omits `allowed_cidrs`; the API rejects it with HTTP 400 requiring that field. No record is created.
4. Repeat in A2A Agents → Register agent; it has the same missing input/payload behavior.

Original HTTP/UI evidence: `../evidence/phase10-private-integration-endpoint-journeys.json`, `../evidence/phase10-mcp-private-endpoint-validation.png`, `../evidence/phase10-a2a-private-endpoint-rejected.png`. Retest form screenshot: `../evidence/phase27-mcp-form.png`; it shows the private endpoint toggle and no CIDR input.

## Expected behavior

When a private/loopback endpoint is enabled, the form should collect one or more explicit CIDR ranges, validate them, and send them as `allowed_cidrs`. Keep backend SSRF and CIDR enforcement authoritative. Do not infer or silently widen the allowed range. Public endpoint registration should continue to omit private CIDR settings.

## Acceptance checks

- MCP and A2A private endpoint forms expose accessible add/remove CIDR controls only when the private endpoint option is enabled.
- Invalid, empty, host-bit, and overly broad ranges receive clear inline validation; valid narrow ranges reach the backend in `allowed_cidrs`.
- The API still rejects missing/invalid CIDRs and blocks addresses outside the submitted ranges.
- Public HTTPS registration, edit/reopen state, keyboard and mobile behavior, and failed-submit recovery remain functional.
- Tests cover both form payloads and end-to-end registration/discovery against an isolated private mock endpoint.

QA classification: confirmed Console workflow defect; no product implementation changed. GATE-REL-07 remains BLOCKED. This handoff does not claim the broader MCP/A2A journeys are complete.

## Scope comparison (CP43)

The Providers → New Connection form accepted an explicit `127.0.0.1/32` CIDR for a loopback Ollama endpoint, and the isolated probe succeeded. This narrows current evidence to the MCP and A2A registration forms; it does not show a system-wide inability to enter CIDRs. Evidence: `../evidence/phase43-ollama-connection-filled.png`, `phase43-ollama-probe-result.json`.
