# CP44 — Full frozen-baseline Go race suite

**Status:** PASS for the complete Go package suite with race instrumentation and required isolated PostgreSQL/Valkey integrations enabled. This is scoped executable regression evidence, not full Alpha/Beta acceptance.
**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`.
**Product source:** unchanged and mounted read-only.

## Environment and command

Used Go `1.26.11` in the cached `golang:1.26.11` container at the original repository path, with read-only Git metadata, the host CA bundle mounted read-only, and the existing Node 24.22.3 runtime / MCP SDK modules mounted read-only. The build and module caches remained in `/tmp`. PostgreSQL 16 and three-node Valkey services were isolated and loopback-only as documented in [CP43](CP43-postgres-valkey-distributed-storage.md).

Executed:

```text
go test -race -count=1 -p 1 ./...
```

**Result: PASS.** Every package completed without a failing test; the command exited successfully. This run includes source-build/VCS stamping, the official MCP SDK approval/replay integration, the CI workspace check, docs/release scripts, chaos tests, Postgres/Valkey integrations, and all other Go packages in the module. The full test command and environment details are recorded in `evidence/CP44-full-go-race-suite.json`.

## Setup correction and transparency

The initial full-suite attempt was not a product result: the repository was mounted at `/src` and run as root, leaving Go's VCS stamping unable to resolve the linked worktree metadata. The Go-only image also lacked `node`, so CT36 stopped before an upstream call. With the source mounted read-only at its original path, Git metadata mounted read-only, safe-directory configuration, and the host Node runtime and SDK modules available, the previously affected packages (`cmd/tanpopo`, `internal/admin/http`, `scripts/ci`, `scripts/docs`, `scripts/release`, and `tests/integration`) all passed in corrected race-enabled runs; the subsequent complete run passed as a whole.

## Limits

This does not validate every manual business workflow, browser state, role, supported browser, platform, external provider, production signing readiness, operational fault domain, performance target, or acceptance criterion. The broader real-user QA campaign remains PARTIAL, nine confirmed defects remain open (DEF-051–059), historical retests and release-signing readiness retain their recorded statuses, and **GATE-REL-07 remains BLOCKED**.
