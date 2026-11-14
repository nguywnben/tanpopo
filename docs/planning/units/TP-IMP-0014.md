# `TP-IMP-0014` — Extension SDK Scaffolder (`plugin init/validate/pack`) & Offline Cassette Test Runner (`plugin test`)

| Property | Value |
|---|---|
| **Unit ID** | `TP-IMP-0014` |
| **Milestone / Epic** | `M1` (`Foundation & Vertical Slice`) · `EP-02` |
| **Status** | `BLOCKED` *(Waiting on `TP-IMP-0012`, `TP-IMP-0013` — upstream `TP-IMP-0006` = `DONE`)* |
| **Size** | `M` (`cmd/tanpopo/main.go`, `sdk/*`, `internal/cli/plugin/*`, `internal/extension/testharness/*`) |
| **Review Tier** | `STANDARD_REVIEW` *(Contract verification toolchain for extension authors)* |
| **Technical Owner** | `Developer Experience & Extension SDK Lead` |

---

## 1. Intent
Provide a language-agnostic Extension SDK scaffolder and `Offline Cassette Replay Test Harness` wired directly under the `tanpopo plugin` subcommand tree of the single `tanpopo` binary so any community developer can scaffold, schema-validate, pack reproducible **`.tpkg`** archives, and execute the full contract suite (`sync`, `SSE stream`, `tool_calls`, `error normalization`, `no-secret-echo`) in seconds **with zero external Internet access and zero paid API keys**, promoting model capability assurance badges from `DECLARED` to `TESTED` (`SPEC-EXT-02`, `SPEC-QA-01`).

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `go.mod`, `go.sum` (add lightweight `Apache-2.0`-licensed CLI flag parser such as `spf13/cobra`).
  - `cmd/tanpopo/main.go` (mount `tanpopo plugin init|validate|test|pack` immediately in `M1`; remaining remote administration subcommands land in `TP-IMP-0031`).
  - `sdk/plugin-schema/v1/extension-manifest.schema.json` & `sdk/plugin-schema/v1/cassette.schema.json`.
  - `sdk/templates/tier1-declarative/*` & `sdk/templates/tier2-wasm/*` (shipping pre-assembled sample `.wasm` binaries compiled from auditable `.wat` sources inside the template so `tanpopo plugin test` completes in `< 5s` without requiring host `TinyGo`).
  - `internal/extension/testharness/runner.go` (offline cassette replay runner guarded by `testkit/netguard` blocking `100%` of sockets outside `127.0.0.1`, replaying arbitrarily byte-chunked SSE streams and running `CanaryScanner` across all outputs).
  - `internal/cli/plugin/cmd.go` (`tanpopo plugin init --tier 1|2` with `declarative|wasm` aliases, `validate`, `test`, and `pack`).
- **Out-of-Scope:** Online keyless `Fulcio/Rekor` signing and `OCI Registry` push workflows (owned by `TP-IMP-0035`).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0305`, `TP-FR-0903`, `TP-BR-0201`, `TP-BR-0401`, `CT-01`..`CT-18`.
- **Dependencies:** `TP-IMP-0006`, `TP-IMP-0012`, `TP-IMP-0013`.
- **Design Refs:** [`extension-contract.md`](../../specifications/extension-contract.md) (`SPEC-EXT-01`), [`developer-experience.md`](../../operations/developer-experience.md) (`SPEC-OPS-04`), [`contract-test-matrix.md`](../../quality/contract-test-matrix.md) (`SPEC-QA-01`).

## 4. API, Wire & Data Contracts
- **Canonical Cassette Schema (`Cassette YAML/JSON`):** `{ case_id, capability, normalized_input, upstream_recorded_exchange (status, headers, raw_body | sse_frames), expected_normalized_output | expected_canonical_error }`.
- **Exit Code Table Matching `SPEC-OPS-04` (`developer-experience.md`) `100%`:**
  - `0` = All schema validations and contract cassettes passed.
  - `2` = CLI flag syntax error or `manifest.yaml` JSON Schema validation failure.
  - `14` = Capability/security violation or `CanaryScanner` secret-leak detection.
  - `22` = Cassette contract assertion mismatch or `.tpkg` digest mismatch.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1`:** Given a network-isolated environment (`TANPOPO_TEST_HERMETIC=1`), When building `tanpopo` and running `init --tier 1 -> validate -> test -> pack` (and likewise for `--tier 2`), Then every step exits `0`, produces a bit-for-bit reproducible `.tpkg` archive (`SHA-256` invariant via epoch-`0` timestamps and deterministic tar entry ordering), and emits a JSON report upgrading capability badges to `TESTED`.
- **`AC-2`:** Given an SSE cassette split mid-multi-byte UTF-8 rune and mid-`data:` prefix, When running `tanpopo plugin test`, Then the harness accurately detects misassembled chunks or dropped end-of-stream frames.
- **`NEG-1`:** A sample Adapter accidentally echoing an `Authorization` header or secret slot value into `error.message` → immediately caught by `CanaryScanner` inside `plugin test` and exits with code `14`.
- **`NEG-2`:** A manifest omitting `contractVersion` → `tanpopo plugin validate` exits with code `2`; a normalized output mismatching the expected cassette → `tanpopo plugin test` exits with code `22`.

## 6. Test Commands & Required Evidence
- Pure-Go verification (`CGO_ENABLED=0`):
  - POSIX: `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -v ./cmd/tanpopo/... ./internal/extension/testharness/... ./internal/cli/plugin/...`
  - PowerShell: `$env:CGO_ENABLED='0'; $env:TANPOPO_TEST_HERMETIC='1'; go test -v ./cmd/tanpopo/... ./internal/extension/testharness/... ./internal/cli/plugin/...`
- Data-race verification (`-race`): `go test -v -race ./internal/extension/testharness/... ./internal/cli/plugin/...`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** `tanpopo plugin test` unconditionally enables `testkit/netguard` by default — zero packets ever leave loopback even if cassette fixtures reference production URLs such as `https://api.openai.com`.
- **Rollback:** Isolated CLI subcommand tree with zero impact on user persistence state.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0006`, `TP-IMP-0012`, and `TP-IMP-0013` are complete.
- **DoD:** Completes the full `init -> validate -> test -> pack` lifecycle for both `--tier 1` and `--tier 2` templates in `< 5 seconds`, matching `SPEC-OPS-04` exit codes `0 / 2 / 14 / 22`.
