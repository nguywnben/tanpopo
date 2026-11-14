# `TP-IMP-0006` — Hermetic Test Harness (`internal/testkit`): `netguard` Socket Guard, `FakeClock`, `CanaryScanner`, `wasmfixtures` & `FakeLLMServer`

| Property | Value |
|---|---|
| **Unit ID** | `TP-IMP-0006` |
| **Milestone / Epic** | `M1` (`Foundation & Vertical Slice`) · `EP-01` |
| **Status** | **`READY_FOR_REVIEW`** *(Implemented & locally verified under `CGO_ENABLED=0` & `TANPOPO_TEST_HERMETIC=1` (`94.1%` total statement coverage across `./internal/testkit/...` via `go test -count=3 -coverprofile=...`, `0` `time.Sleep`, enhanced redirect/proxy/IPv4-mapped/DNS bypass tests); **blocked from `DONE` pending Product Owner decision on [`CR-11`](../../reviews/unresolved-gaps.md#part-c--implementation-phase-change-requests-pending-product-owner-decision)** regarding `http.DefaultTransport`/`http.Get` Floor Guard alignment, OS `unshare -n` isolation, and Stage 1 CI `-race -count=3` execution)* |
| **Size** | `M` (`5` internal packages under `internal/testkit/*`) |
| **Review Tier** | `STANDARD_REVIEW` |
| **Technical Owner** | `QA Architect + Test Systems Engineer` |

---

## 1. Intent
Build the shared hermetic test harness (`internal/testkit/*`) per [`test-strategy.md`](../../quality/test-strategy.md) Sections 3 & 4 before writing any network client code, guaranteeing **mechanically that no test can ever accidentally resolve external DNS or open a socket to paid Internet AI APIs**, all time-dependent assertions execute deterministically without `time.Sleep()`, pre-compiled adversarial `.wasm` fixtures built from auditable `.wat` sources are ready out-of-the-box, and any secret leak is caught automatically.

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `internal/testkit/netguard/` (when `TANPOPO_TEST_HERMETIC=1`: overrides `http.DefaultTransport` and `net.DefaultResolver = &net.Resolver{PreferGo: true, Dial: loopbackOnlyDial}` to block external DNS queries such as `8.8.8.8:53`, and exports `netguard.CheckLoopbackAddr(addr)` wired directly into `egress.NewGuardedHTTPClient` in `TP-IMP-0010` so even custom `http.Transport` instances are hard-locked to `127.0.0.0/8` and `::1` under test).
  - `internal/testkit/clock/` (`Clock` interface + `RealClock` + concurrency-safe `FakeClock` with `Advance(d)`).
  - `internal/testkit/canary/` (`CanaryScanner` scanning log buffers, temp files, and JSON payloads for `sk-tp-canary-DO-NOT-LEAK-9f8e7d6c` in raw, `base64`, and URL-encoded forms).
  - `internal/testkit/wasmfixtures/` (auditable `.wat` text sources paired with pre-assembled `.wasm` byte slices for 5 adversarial guest fixtures: `echo_transform.wasm`, `infinite_loop.wasm`, `oom_64mib.wasm`, `unreachable_trap.wasm`, `unapproved_wasi_import.wasm` — enabling Wasm tests on Windows/macOS/Linux without requiring host `TinyGo`).
  - `internal/testkit/fakellm/` (`FakeLLMServer` bound to `127.0.0.1:0` supporting OpenAI/Anthropic/Gemini dialects + deterministic fault injection via `X-Tanpopo-Fake-Fault`: `delay_first_byte_ms`, `tcp_rst_after_chunks`, `http_status=429;retry_after=12`, `omit_stream_usage`, `echo_auth_header_in_error`).
- **Out-of-Scope:** `FakeOAuthIdP` and `FakeMCPServer` (added in `TP-IMP-0034` and `TP-IMP-0036` respectively when opening the `v0.2` plane).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0305`, `TP-FR-0806`, `TP-NFR-0101`, `TP-NFR-0102`, `OPS-AC-10`.
- **Dependencies:** `TP-IMP-0005`.
- **Design Refs:** [`test-strategy.md`](../../quality/test-strategy.md) Sections 3 & 4, [`contract-test-matrix.md`](../../quality/contract-test-matrix.md) Section 1.

## 4. API, Wire & Data Contracts
- `netguard.InstallHermeticGuard(t testing.TB)` and `netguard.MustRunHermeticMain(m *testing.M)`: intercept outbound TCP, UDP, and DNS (`net.DefaultResolver`) dials targeting non-loopback addresses and fail with a full call stack trace.
- `fakellm.NewServer(t testing.TB) *FakeLLMServer`: registers automatic teardown via `t.Cleanup` and records received requests (including inbound auth headers and stream-disconnect timestamps on client cancel) for exact assertions in `CT-06`..`CT-16`.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1`:** Given `TANPOPO_TEST_HERMETIC=1`, When calling `FakeLLMServer` at `http://127.0.0.1:<port>/v1/chat/completions` (both synchronous JSON and 50-chunk SSE with `: ping` comments), Then responses arrive in `< 5 ms` with `0` external network dials.
- **`AC-2`:** Given header `X-Tanpopo-Fake-Fault: tcp_rst_after_chunks=5`, When a client reads the SSE stream from `FakeLLMServer`, Then it receives exactly 5 valid chunks followed by a deterministic TCP reset error.
- **`AC-3`:** Given `FakeClock`, When 10 goroutines block on timers and the main goroutine calls `FakeClock.Advance(61 * time.Second)`, Then all waiters unblock deterministically in `< 1 ms` wall-clock time.
- **`NEG-1`:** Attempting `http.Get("https://api.openai.com/v1/models")`, `net.LookupHost("api.openai.com")`, or dialing UDP `8.8.8.8:53` with `netguard` active → intercepted before sending any packet and fails the test immediately.
- **`NEG-2`:** Emitting `sk-tp-canary-DO-NOT-LEAK-9f8e7d6c` (including `base64`-encoded variants) into a test log buffer → `CanaryScanner.AssertClean(t)` detects and fails immediately.
- **`NEG-3`:** Zero fixed `time.Sleep()` calls anywhere inside `testkit` tests.

## 6. Test Commands & Required Evidence
- Pure-Go verification (`CGO_ENABLED=0`):
  - POSIX: `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -v -count=3 ./internal/testkit/...`
  - PowerShell: `$env:CGO_ENABLED='0'; $env:TANPOPO_TEST_HERMETIC='1'; go test -v -count=3 ./internal/testkit/...`
- Data-race verification (`-race`, `CGO_ENABLED=1`): `go test -v -race -count=3 ./internal/testkit/...`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Dual-lock defense-in-depth alongside `OPENAI_API_KEY=CANARY_FORBIDDEN_CI_KEY` configured in `.github/workflows/ci.yml` in `TP-IMP-0005`.
- **Rollback:** Standalone internal test harness package; revertible with zero impact on production binaries.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0005` is complete.
- **DoD:** Achieves `>= 90%` line coverage across `internal/testkit/...` and passes both `CGO_ENABLED=0` and `-race -count=3` with zero flake.
