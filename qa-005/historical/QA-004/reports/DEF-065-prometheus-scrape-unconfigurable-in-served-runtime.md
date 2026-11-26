# DEF-065 — Prometheus scrape cannot be configured in the served runtime

**Severity:** Moderate
**Status:** FAIL
**First found:** QA-004 CP48
**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`
**Affected workflows:** Prometheus onboarding, monitoring alerts, capacity planning, telemetry access control.

## Summary

The Prometheus metrics registry supports a bearer scrape token and a high-tenant label toggle, and product/operations documentation describes scraping `/metrics` on an internal `:9090` listener. The executable `serve` path does not provide the registry a scrape token or label setting and only binds the configured `--listen` address. In a fresh RC6 runtime, `/metrics` remained unauthorized even when a synthetic candidate token was supplied both to the process environment and as the request bearer; port 9090 refused the connection.

The fail-closed response is the expected security posture. The defect is that normal operators have no supported runtime configuration to authorize a scrape, and the documented listener is not created by `serve`.

## Reproduction

1. Start frozen RC6 on a fresh isolated directory with `--listen 127.0.0.1:18132 --offline`.
2. Set synthetic candidate environment values `TANPOPO_METRICS_SCRAPE_TOKEN` and `TANPOPO_METRICS_INCLUDE_TENANT_LABELS=false` before startup.
3. Confirm `/readyz` returns 200.
4. In Chromium, request `/metrics` without a bearer. It returns 401 `unauthorized`.
5. Request `/metrics` again with a bearer matching the candidate scrape token. It still returns 401 with the same message.
6. Request `http://127.0.0.1:9090/metrics`. The connection is refused; `ss` shows only the configured 18132 listener for this process.
7. Inspect `serve.go`: `NewPipeline` receives only service name/version, DB, and Vault. The only listener is created from the `listen` argument. The tested metrics environment variables are not read by the runtime.

Screenshots were captured from and visually inspected in real Chromium: [without bearer](../evidence/CP48-metrics-no-auth.png), [with the candidate bearer](../evidence/CP48-metrics-with-candidate-token.png). Sanitized statuses and listener observations: [CP48 results](../evidence/CP48-prometheus-scrape-results.json).

## Expected and actual

- **Expected:** the runtime provides a documented, secure way to configure the scrape bearer and tenant-label policy, and serves metrics on the documented internal listener or updates deployment contracts to the actual binding.
- **Actual:** no supported runtime configuration populates `MetricsScrapeToken` or the label toggle. Both missing and candidate credentials return 401. `tanpopo serve` does not bind `:9090`; it binds only its configured `--listen` address.

## Business impact

Operators cannot complete documented Prometheus setup for the normal executable, so alert expressions, HPA inputs, cardinality monitoring, and operational dashboards cannot scrape Tanpopo's metrics. The metrics registry tests do not establish that product deployments can configure it.

## Facts and root-cause hypothesis

**Verified facts:** `PipelineConfig` has `MetricsScrapeToken`, but the actual `serve` call leaves it unset. Source search found no runtime read for the candidate environment variables. The process opened only `127.0.0.1:18132`; loopback port 9090 refused the request. The registry correctly returned 401 for both requests.

**Hypothesis:** scrape-token and tenant-label settings were implemented at the metrics-library boundary and referenced in docs/Helm monitoring resources but not connected to the executable configuration or listener setup.

## Acceptance criteria

1. Provide documented, validated runtime configuration for a scrape bearer token and the tenant-label toggle; secret values must not appear in logs or support bundles.
2. An authenticated scrape with the configured token returns Prometheus exposition; missing and wrong tokens continue to receive 401.
3. Bind the scrape endpoint on the documented internal port/network boundary, or revise Helm/OpenAPI/operations contracts and deployment resources to match the actual listener.
4. Verify high-tenant mode omits org/project labels while retaining bounded labels and verify the default label policy explicitly.
5. Exercise the actual binary and Helm deployment path end to end; library unit tests alone are insufficient.

## Scope boundary

The test proved fail-closed behavior and an inaccessible deployment scrape path. It did not test successful authorized exposition or cardinality policy because no runtime setting exists to configure them.
