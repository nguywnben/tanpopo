# CP143 — direct-listener forwarded-header boundary

**Date:** 2026-11-26  
**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`  
**Runtime:** isolated SQLite `127.0.0.1:18128`  
**Outcome:** PASS (scoped) — forwarded identity/client headers alone do not authenticate either control-plane session reads or Provider-plane model discovery. No defect assigned.

## Reproduction

Sent same-origin, cookie-free requests from a real Chromium page to `GET /admin/v1/auth/session` and `GET /v1/models`. A no-header control returned HTTP 401 on both. Added only forwarded identity headers (`X-Forwarded-User`, `X-Forwarded-Email`, `X-Auth-Request-Email`, `X-Auth-Request-User`, `X-Auth-Request-Roles`); both endpoints still returned 401. Added only `X-Forwarded-For`, `X-Forwarded-Host`, `X-Forwarded-Proto`, and `X-Real-IP`; both remained 401. No cookie, bearer key, or authorization credential was attached, and no data was changed.

This is a narrow fail-closed check on direct access to the isolated untrusted listener. No trusted reverse proxy or proxy-auth/OIDC configuration was present, so it does not verify positive trust behavior, XFF chain resolution, proxy TLS/OIDC redirects, or allowlist configuration. Those remain UNVERIFIED; no claim is made about a configured production proxy.

## Evidence

- [Sanitized request matrix](../evidence/CP143-untrusted-proxy-header-boundary.json)

This security API check produced no screenshot; previous CP141/142 UI screenshots were visually inspected. All defect counts remain unchanged. QA-004 is ongoing. **GATE-REL-07 remains BLOCKED.**
