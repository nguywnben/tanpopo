# CP41 — Forwarded-header spoofing and foreign-origin boundary

**Baseline:** frozen RC5 `3423d4f523ef0ac77469a1201b8b753d1d31c902`<br>
**Runtime:** isolated SQLite `18116`; no cookies or credentials
**Disposition:** sampled spoofed forwarding headers did not bypass Admin authentication; foreign-origin CORS was not granted. QA-003 remains incomplete; `GATE-REL-07` remains **BLOCKED**.

## Reproduction and results

Sent unauthenticated `GET /admin/v1/organizations` with (1) `X-Forwarded-For: 127.0.0.1`, `X-Real-IP: 127.0.0.1`, and a forged `Forwarded` loopback/HTTPS/localhost value; (2) multiple forged forwarding headers; and (3) a foreign `Origin`. All three returned HTTP 401 `unauthenticated`, with no CORS allow-origin or allow-credentials headers. A foreign-origin preflight requesting GET and `x-tanpopo-org-id` returned 405 with no CORS permission headers.

Sanitized status/header evidence: [CP41 result](../evidence/CP41-proxy-header-and-cors-result.json).

## Coverage and limits

- **PASS, scoped:** this unauthenticated Admin read resisted the tested forwarded-header spoofing; the foreign origin did not receive CORS permission for the sampled request.
- **UNVERIFIED:** valid session/key behavior through a configured trusted reverse proxy, write requests/CSRF, other origins, browser-enforced credentialed CORS, and the complete proxy trust contract.
- No new defect was confirmed. These requests do not establish correctness for every proxy deployment.
