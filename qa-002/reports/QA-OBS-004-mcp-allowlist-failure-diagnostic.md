# QA-OBS-004 — MCP allowlist rejection has a generic sync diagnostic

**Classification:** Unresolved UX/operations observation; not a confirmed defect.  
**Severity:** Not assigned.  
**Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`.  
**Evidence:** CP25 mismatch and allowed-control requests/screenshots; see `checkpoints/CP25-mcp-cidr-negative-control.md`.

CP40 repeated the user-visible generic 502 for `localhost` with a CIDR that did not match the selected IPv6 peer, or allowed only the unselected IPv4 result. The explicit `::1/128` positive control synchronized successfully. The fixture confirms the blocked cases made no request and successful case used peer `::1`; this supports enforcement but does not expose the exact internal rejection reason. See `checkpoints/CP40-operator-and-hostname-cidr-boundary.md` and `evidence/CP40-operator-and-dns-allowlist.json`.

CP41 also returned the same generic 502 for a 307 redirect and left the configured row Active. A direct request to the redirect target succeeded; neither same-CIDR nor cross-CIDR redirect was followed. This narrows the observation to safe but non-actionable errors on refused syncs. Whether redirects are intentionally unsupported and whether “Active” denotes configured state remain unresolved.

## Reproduction and facts

1. Register an isolated MCP server at `http://127.0.0.1:19110/mcp`, check “Allow private or loopback endpoint”, and configure `127.0.0.2/32` (the reachable fixture is `127.0.0.1`). Registration returns 201 and the registry displays the server as `Active` with zero tools.
2. Click `Sync tools`. The management API returns HTTP 502 with `code=mcp_sync_failed`, `type=upstream_error`, `message=upstream MCP tool synchronization failed`, and `retryable=true`. The Console shows “upstream MCP tool synchronization failed”. The instrumented local fixture records no request.
3. Register the same endpoint a second time but allow `127.0.0.1/32`. Sync returns HTTP 200 and the same fixture records `initialize`, `notifications/initialized`, and `tools/list`.

## Expected versus actual

A useful operator diagnostic would identify that the resolved endpoint address is outside the configured allowed CIDRs and direct the operator to review the explicit allowlist, while continuing to reveal no sensitive network or credential detail. Actual Console/API messaging is the same generic upstream synchronization failure, classified as retryable, and does not identify the address-policy mismatch. The registry retains an `Active` state with zero tools after the failed sync.

## Impact and uncertainty

An operator may retry a deterministic configuration error or investigate endpoint availability and credentials instead of the CIDR policy. The allowed-versus-mismatched same-endpoint control supports CIDR enforcement and the fixture proves the blocked request was not delivered. It does not independently expose the exact internal rejection reason; generic error mapping may be an intentional security policy, and `Active` may mean configured/enabled rather than healthy. Therefore this remains an observation pending product error-state semantics and policy review, not a confirmed defect.

## Follow-up evidence needed

Confirm documented MCP server state semantics and the intended safe diagnostic contract for allowlist denials. If administrators are expected to act on policy rejection, add a localized actionable error class and a visible failed-sync/configuration status; otherwise document why generic `upstream_error` and `Active` are intentional.
