# CP48 — Prometheus scrape configuration and listener

**Status:** FAIL for normal-runtime scrape configuration and documented `:9090` listener (DEF-065). Scoped unauthorized-access control PASS. Successful authorized scrape and high-tenant label policy UNVERIFIED. **GATE-REL-07:** BLOCKED.
**Baseline:** frozen RC6 source `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`; artifact run `45581179274`.
**Product source:** unchanged.

## Isolated runtime

- Started a fresh exact RC6 Linux amd64 artifact on `127.0.0.1:18132` with its own SQLite directory and offline mode. `/readyz` returned 200.
- Set synthetic candidate environment values for `TANPOPO_METRICS_SCRAPE_TOKEN` and `TANPOPO_METRICS_INCLUDE_TENANT_LABELS=false`. These were probes, not values documented as supported.
- Actual Chromium requests to `/metrics`, first without a token and then with a bearer matching the candidate token, both returned HTTP 401 `unauthorized`, “Missing or invalid internal metrics scrape token.” Both screenshots were opened and visually inspected.
- A request to the documented internal `127.0.0.1:9090/metrics` listener failed with connection refused. The listener snapshot showed only `127.0.0.1:18132` for this process.

## Source and contract comparison

- The `MetricsConfig` and `PipelineConfig` types allow a scrape token and `IncludeTenantLabels` setting, but the only non-test `NewPipeline` call in `cmd/tanpopo/serve.go` passes service name/version, DB, and Vault.
- The tested candidate environment names are not read in the executable runtime/config path.
- `serveWithConfig` creates a single HTTP listener from its `listen` argument; in `--role all` it mounts the metrics handler on that listener. No separate `:9090` listener is created.
- `monitoring-incidents.md`, OpenAPI, and operational deployment documentation describe `/metrics` at internal `:9090` and refer to the configurable tenant-label policy.
- The implementation's 401 is fail-closed and is not itself a security defect. The defect is lack of supported runtime configuration/listener parity.

## Evidence and defect

- [DEF-065 report](../reports/DEF-065-prometheus-scrape-unconfigurable-in-served-runtime.md)
- [Sanitized result](../evidence/CP48-prometheus-scrape-results.json)
- [Unauthenticated response screenshot](../evidence/CP48-metrics-no-auth.png)
- [Candidate-token response screenshot](../evidence/CP48-metrics-with-candidate-token.png)

## Remaining coverage

Authorized scrape exposition, authenticated scrape integration from the supplied Helm ServiceMonitor, actual supported configuration syntax, tenant-label exclusion, series cardinality under load, and auth rotation remain UNVERIFIED. This checkpoint does not complete OTel, Prometheus, operational, or QA-004 acceptance.
