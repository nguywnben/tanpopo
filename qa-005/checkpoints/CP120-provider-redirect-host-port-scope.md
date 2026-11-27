# CP120 — Provider probe redirect to another loopback port

**Source:** frozen RC7 `737079ddb847d97dbcf832f610ed0048556cb65b`; runtime binary SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`.
**Environment:** isolated exact-source SQLite Owner Console at `127.0.0.1:18145`; Chromium 156 / Playwright; local-only deterministic listeners.
**Status:** PASS (scoped) for rejecting a redirect from the configured host:port to a different loopback port; metadata-address rejection remains PARTIAL per CP119.

## Reproduction

1. Started an HTTP redirector on `127.0.0.1:19100`. Its only GET response was HTTP 302 to `http://127.0.0.1:19101/v1/models`.
2. Started a harmless sentinel HTTP service on `127.0.0.1:19101`, recording every request and returning an empty model list if reached.
3. Through the real Providers Console, an Owner created `QA005 Redirect Guard Followup` for `http://127.0.0.1:19100`, enabling private/loopback access with `127.0.0.1/32`. API create returned HTTP 201. The actual form and persisted row were visually inspected; screenshots: [form](../evidence/screenshots/CP120-redirect-private-port-form.png), [result](../evidence/screenshots/CP120-redirect-private-port-result.png).
4. The real “Probe connection” action returned HTTP 200 with `valid:false`, `failure_code=upstream_unreachable`, sanitized message `upstream connection could not be reached`. The redirector observed one GET `/v1/models`; the separate 19101 sentinel observed zero requests. The UI remained Unverified and showed no failure detail, consistent with PG-013.
5. Deleted the temporary connection through the Console; HTTP 204 and the test row was absent. Closed both local listeners; both ports are closed.

## Result and limitations

The exact frozen runtime did not follow this same-IP, different-port redirect to the unauthorized receiver, despite the connection’s explicit private CIDR opt-in. This is a scoped PASS for the configured host:port boundary using a real connection test, not merely an SSRF unit test. No secret, paid provider, external network, or metadata service was involved.

This does not directly prove rejection of the `169.254.169.254` redirect in CP119; there was no metadata-side or socket-level telemetry there, so that distinct result remains PARTIAL. DNS rebinding, alternate schemes, IPv6, other roles/backends/browsers and audit-chain verification remain open. See [sanitized evidence](../evidence/CP120-provider-redirect-host-port.json).
