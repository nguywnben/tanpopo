# QA-OBS-015 — Managed Cloud direct-egress limitation blocks live webhook delivery

- **Status:** BLOCKED environmental integration coverage; no Tanpopo defect assigned for delivery failure
- **Observed:** CP86, isolated application runtime `127.0.0.1:18084`

## Evidence

The real Console created a valid subscription for a synthetic public HTTPS receiver and queued a real `kill_switch.triggered` event. The receiver observed zero requests. In the isolated Tanpopo SQLite queue, that delivery reached `DEAD_LETTER` after eight attempts, with no HTTP response status and the safe error `transport failure`.

At the Cloud environment boundary, `environment_status` reported current observations, `network_policy.mode=unrestricted`, `state=enforced`, and no network diagnostics. A no-proxy HTTPS request from the environment failed during DNS resolution (HTTP status 000); the same public host through the managed shell proxy returned HTTP 200. Tanpopo's guarded HTTP client explicitly avoids `http.ProxyFromEnvironment`, preventing an ambient proxy from bypassing SSRF resolution and destination checks.

## Classification

This establishes that the managed shell's proxy path works while direct DNS/outbound networking used by the product process is unavailable in this Cloud environment. The configured egress policy is unrestricted, but the application's SSRF-guarded path cannot use the shell's proxy. The result is **not** evidence that Tanpopo's normal-network webhook delivery or HMAC implementation is defective. Signature verification, successful delivery, remote 4xx/5xx handling and live retry recovery remain **UNVERIFIED** pending an environment that supports the product's direct guarded egress path.

The webhook was configured only for one QA-generated event with synthetic project metadata. No payload or secret reached the external receiver. Its temporary token and the test signing credential were deleted; the isolated subscription and its single dead-letter row were cleaned after the missing product removal operation was confirmed. Sanitized details are in [CP86 evidence](../evidence/CP86-webhook-delivery-and-cleanup-sanitized.json).
