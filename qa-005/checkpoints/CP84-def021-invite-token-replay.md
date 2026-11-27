# CP84 — DEF-021 one-time invite replay retest

**Disposition:** PASS (scoped one-time replay rejection).
**Source:** exact unsigned RC7 `737079ddb847d97dbcf832f610ed0048556cb65b`; binary SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`.
**Environment:** isolated SQLite clone `127.0.0.1:18162`; Chromium 156; synthetic Owner and Developer.

## Reproduction

1. In the real Owner Console, open Access & Keys → Members & Roles and choose **Issue invite token** for the pending synthetic Developer. The token stayed in process memory. The action returned HTTP 201.
2. In a separate browser context, open the real invite activation form and submit a new password. The activation request returned HTTP 200 and navigated to Overview.
3. In a fresh context, submit the same consumed token through the same activation UI with a different random password. The request returned HTTP 401 `invalid_enrollment_token` and did not establish a session. The Console displayed “The enrollment token is invalid, expired, or already used.”
4. Fresh Owner API readback returned HTTP 200 and found the same HUMAN/DEVELOPER with `enrollment_pending=false` and `has_password=true`.

The replay response and persistence evidence are in [sanitized JSON](../evidence/CP84-def021-enrollment-token-replay.json). The replay error state was visually inspected in [the screenshot](../evidence/screenshots/CP84-def021-activation-replay.png); the invite field was cleared before capture. The persistent Members & Roles row was visually inspected in [the Owner screenshot](../evidence/screenshots/CP84-def021-member-persistence.png). No token/password/hash was saved.

## Limits

This passes one-time replay rejection for this synthetic SQLite/Chromium path. Token expiry timing, cross-organization rejection, enrollment audit visibility, recovery, other roles/backends/browsers, and permission-negative tests remain UNVERIFIED. An initial scripted extraction attempt did not render a valid activation form and was discarded; the successful run issued the token through the actual Owner row action. This was a harness setup issue, not a product result. DEF-021 remains PASS (scoped) with the wider criteria open. `GATE-REL-07` remains BLOCKED.
