# CP133 — Local login brute-force rate limit

**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`  
**Status:** PASS (scoped runtime rate limit and system audit API)  
**Role / environment:** public local-login endpoint on isolated runtime 18128; system-audit readback through the saved Org Owner + Instance Operator Console session.

## Procedure and result

1. Submitted ten sequential `POST /admin/v1/auth/login` requests with a synthetic nonexistent email and a deliberately incorrect test-only password. Each returned HTTP 401 `invalid_credentials`.
2. Attempt 11 returned HTTP 429 `auth_rate_limited` and `Retry-After: 300`; attempt 12 remained HTTP 429 with the same `Retry-After`.
3. Queried the Operator-authorized `SYSTEM` audit partition through `GET /admin/v1/audit-events?chain_partition=SYSTEM&action=auth.login_rate_limited`. Three matching events targeted only the synthetic identity: one `max_failures_exceeded` and two `brute_force_throttled` events (sequences 16–18).
4. Verified the SYSTEM hash chain: HTTP 200, `intact: true`, 18 events checked.
5. Opened the real Security → Audit Log UI in Chromium and visually inspected its actual filter and event table. The page rendered the currently selected organization partition and no System partition selector was visible. This screenshot is not evidence that the UI displays SYSTEM events; API verification is recorded separately.

The synthetic login identity did not exist. No production identity or password was used. A 300-second IP-level cooldown was observed for the loopback test client; no subsequent login attempts were made. The existing authenticated QA session remained usable.

## Evidence

- [sanitized attempt and audit results](../evidence/CP133-login-rate-limit-results.json)
- [real Security Audit Log UI screenshot](../evidence/CP133-security-audit-ui-organization-partition.png)

The screenshot was visually inspected. No new product defect was assigned. The rate-limit threshold is stricter than the contract matrix’s example of 30 attempts (429 began on attempt 11); this satisfies the stated protection and is not treated as a defect. The visible System-audit UI workflow remains UNVERIFIED because this slice found no partition selector; no inference is made about other Operator Console paths.

## Remaining coverage

OIDC/PKCE authentication, expired/forged JWT validation, login behavior across multiple processes/IPs, cooldown reset/expiry, and discoverability of SYSTEM events in the Console remain UNVERIFIED. This checkpoint does not inherit prior tests or claim complete AUTH-02 coverage.

No Tanpopo source was modified. **GATE-REL-07 remains BLOCKED.**
