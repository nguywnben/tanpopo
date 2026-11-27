# CP63 — DEF-065 Prometheus scrape on the served runtime

**Source:** frozen unsigned RC7 commit `737079ddb847d97dbcf832f610ed0048556cb65b`.
**Artifact:** exact-source local Linux/amd64 binary, SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`, `vcs.modified=false`.
**Disposition:** historical DEF-065 **PASS (scoped QA-005 retest)**. The prior QA-004 FAIL remains preserved; this does not certify remote Prometheus deployment.

## Reproduction

1. Created an isolated SQLite online-backup clone and confirmed `PRAGMA integrity_check=ok`.
2. Started exact RC7 with `--metrics-listen 127.0.0.1:19157`, the main application at `127.0.0.1:18157`, offline mode, and a synthetic QA-only `TANPOPO_METRICS_SCRAPE_TOKEN` value. No production credential or provider was used.
3. Both `/healthz` and `/readyz` returned HTTP 200. `ss` showed the metrics and app listeners bound only to loopback.
4. On the metrics listener, GET `/metrics` without a token returned 401 `unauthorized`; a wrong synthetic bearer token also returned 401. The configured token returned HTTP 200 `text/plain; version=0.0.4` with Prometheus HELP, TYPE and metric sample lines, including `tanpopo_config_snapshot_age_seconds{sync_channel="db_poll"}`.
5. The main application listener's `/metrics` also returned 401 without auth and 200 with the configured token in this loopback-only deployment. No conclusion is drawn about intended remote exposure when the main listener uses a non-loopback bind.
6. Used real Chromium 156 via Playwright with the synthetic token in an HTTP Authorization header. The browser rendered the authorized text exposition; the screenshot was visually inspected and does not display the token.

## Classification and limits

**DEF-065 — PASS (scoped):** the current exact RC7 served runtime exposes a configurable metrics listener and serves Prometheus data when correctly authorized. The 401 responses without and with a wrong token are expected fail-closed authorization, not the historical configuration defect.

The token environment variable is not surfaced in `serve --help` and was not found in the searched operator docs, despite being required for a successful authorized scrape. This is recorded as **PRODUCT_GAP PG-009** (configuration discoverability); the authentication guard must remain. The auth token was synthetic and excluded. Prometheus server ingestion, alerts, remote bind/network behavior, TLS, token rotation, other backends, platforms and cluster roles remain UNVERIFIED. Exact evidence is [CP63 evidence](../evidence/CP63-def065-prometheus-scrape.json) and the [visually inspected screenshot](../evidence/screenshots/CP63-authorized-prometheus-scrape.png). No product code/spec changed; campaign continues and `GATE-REL-07` remains BLOCKED.
