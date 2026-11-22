# `TP-IMP-0043` Execution & Verification Report — `k6` Load Suite, Empirical `SLO-01..08` Verification & Timed `FMEA-01..08` / `DR-01..04` Drills

> **Unit:** `TP-IMP-0043` (`M5` / `EP-09` · `STANDARD_REVIEW`)
> **Branch / Workflow:** `develop` -> PR [#35](https://github.com/nguywnben/tanpopo/pull/35) -> `main` (`44689bbc4320398a09f60e03dd28132a9f1e9190`, accepted `2026-11-22` under [`DEC-092`](../governance/decisions.md))
> **Post-Merge CI:** [`44991547245`](https://github.com/nguywnben/tanpopo/actions/runs/44991547245) (`Stage 0-1` `SUCCESS`) · [`44991547244`](https://github.com/nguywnben/tanpopo/actions/runs/44991547244) (`Release Artifact Dry Run` `SUCCESS`)
> **Author:** **`Gemini 4 Hydrogen`**
> **Independent Reviewer (`STANDARD_REVIEW`):**
> - `code-reviewer` (`bf60bead45843d13feed71429f820fe4`): **`APPROVE`** (Critical: 0, High: 0, Medium: 0, Low: 0 — all initial findings verified resolved)

---

## 1. Delivered Scope

1. **Protective Overload Shedding (`NEG-1`, [`nonfunctional-slo.md`](../quality/nonfunctional-slo.md) §2.1):**
   - [`internal/ingress/http/{server.go,chat_completions.go,responses.go}`](../../internal/ingress/http/server.go), [`cmd/tanpopo/serve.go`](../../cmd/tanpopo/serve.go): Added `MaxNodeConcurrentStreams` (`TANPOPO_MAX_CONCURRENT_STREAMS` with fail-closed negative/invalid parsing), atomic `activeStreams` tracking across `/v1/chat/completions` and `/v1/responses`, throttled `200 ms` `runtime/metrics` heap monitoring against `debug.SetMemoryLimit(-1)` (`85%` cap), and fast `503 gateway_overloaded` (`Retry-After: 2`) shedding inside `withIngressEnvelope` while exempting `/healthz`, `/readyz`, and `/metrics`.
   - [`internal/ingress/http/overload_shedding_test.go`](../../internal/ingress/http/overload_shedding_test.go): Verified `< 1 ms` `503 gateway_overloaded` shedding on both `85%` heap ratio and `MaxNodeConcurrentStreams` saturation across both `/v1/chat/completions` (`[DONE]`) and `/v1/responses` (`response.completed`), plus instantaneous recovery once load subsides.
2. **Hermetic `k6` Suite & Benchmark Harness (`SLO-01..08`, `NEG-1`, `OPS-AC-06`):**
   - [`internal/testkit/fakellm/fakellm.go`](../../internal/testkit/fakellm/fakellm.go): Extended cancellable `chunk_delay_ms` fault injection across OpenAI Chat, OpenAI Responses, Anthropic, and Gemini SSE handlers (`internal/testkit/fakellm` `-race` verified).
   - [`tests/perf/harness/main.go`](../../tests/perf/harness/main.go) & [`tests/perf/k6/{sse_1000_streams.js,declar_vs_wasm_overhead.js,overload_shedding_503.js,rolling_restart_zero_drop.js}`](../../tests/perf/k6/sse_1000_streams.js): Implemented self-contained `seed` / `fakellm` / `lb` benchmark harness complying strictly with `CONSTRAINTS.md` (`0` `internal/testkit` imports in harness, `net.Dialer.Control` loopback guard) and executed containerized `grafana/k6` workloads capped cleanly at `--cpus=2 --memory=2g` (`GOMAXPROCS=2`, `GOMEMLIMIT=1750MiB`).
3. **Automated Chaos (`FMEA-01..08`) & Timed Disaster Recovery (`DR-01..04`, `OPS-AC-06`):**
   - [`tests/chaos/fmea_suite_test.go`](../../tests/chaos/fmea_suite_test.go): Automated verification covering `FMEA-01` through `FMEA-08`.
   - [`tests/chaos/dr_drills_test.go`](../../tests/chaos/dr_drills_test.go): Timed recovery drills for `DR-01` (`36.29 ms` CGO0 / `141.4 ms` `-race`), `DR-02` (`30.18 ms` CGO0 / `235.6 ms` `-race` + live PG16/3-shard Valkey Cluster `tanpopo admin verify-state` `278 ms`), `DR-03` (`43.66 ms` CGO0 / `66.4 ms` `-race`), `DR-04` (`4.50 ms` CGO0 / `113.9 ms` `-race` `< 60s`), and `OPS-AC-06` (`100 / 100` active SSE streams across 3-node rolling restart with `0` drops).
4. **Honest Empirical Benchmark Report (`COND-PO-01`):**
   - [`docs/quality/benchmark-evidence.md`](../quality/benchmark-evidence.md): Documented hardware floor, container configs, isolated `Benchmark*` ns/op figures (`256.2 ns/op` policy eval, `475.7 ns/op` snapshot lookup, `14.57 µs` Wasm transform, `18.77 µs` declarative transform), and candid verdicts incl. standalone `2 vCPU` SQLite E2E saturation `FAIL` (`103.69 req/s` due to `DisableKeepAlives: true` + `SQLite MaxOpenConns(1)`), `sse_1000` `PARTIAL` (`1,715 / 1,715` SSE `[DONE]` at `221.4 MiB` RSS vs `+500 RPS` short-request timeouts), `NEG-1` `PASS` (Go `< 1 ms`) / `PARTIAL` (Docker `p95=27.86 ms`), `OPS-AC-06` `PASS` (Go drain) / `PARTIAL` (Docker LB `4/1410` without K8s `preStop`), and `UNVERIFIED` multi-host physical HA / `>= 4h` soak.

---

## 2. Independent Verification & Test Evidence

- **Static Quality Gates (`CGO_ENABLED=0 go run ./scripts/ci/check.go --all`):** **`PASS`** (`0` errors).
- **Go Vet (`go vet ./...`):** **`PASS`** (`0` issues).
- **Full Hermetic Short Suite (`CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -short ./...`):** **`PASS`** across all Go packages.
- **Pure-Go Chaos & DR Suite (`CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -v -count=1 -timeout=10m ./tests/chaos/...`):** **`PASS`** (`13 / 13` tests in `2.673s`).
- **Race Detector Suites (`-race -count=1`):**
  - `internal/testkit/fakellm`: **`PASS`** (`1.076s`).
  - `internal/ingress/http` (`TestProtectiveOverloadSheddingHeapAndStreamsNEG1`): **`PASS`** (`8.020s` — heap `85%`, `/v1/chat/completions`, and `/v1/responses` subtests).
  - `tests/chaos/...`: **`PASS`** (`25.309s` — all `FMEA-01..08`, `DR-01..04`, and `OPS-AC-06` `100 / 100` streams).
  - Live `PostgreSQL 16` (`FORCE RLS`) + `3-shard Valkey Cluster`: `TestTPIMP0040*` (`13.621s`) + `tanpopo admin verify-state` (`278 ms`) **`PASS`**.
- **Isolated Benchmarks (`task-5946`, `GOMAXPROCS=2`):** All 8 production `Benchmark*` functions executed cleanly (`0 B/op` on policy eval & snapshot lookup).
