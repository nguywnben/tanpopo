# TP-IMP-0037 Execution Checkpoint

**Status:** Implementation complete; the final Windows cleanup-test correction passed independent review and local verification. CI on the final pushed SHA remains pending. Unit 37 remains `READY_FOR_REVIEW` until merge and post-merge CI on `origin/main` pass.

## Authority and ownership

- Product Owner delegation: `DEC-072`.
- Implementer and delegated acceptor: **GPT-6 Luna**.
- Initial review corrections: **Galileo** (`code-reviewer`) and **Harvey** (`security-auditor`).
- Independent review corrections: **James** (`code-reviewer`) and **Laplace** (`security-auditor`).
- Final review on the hardened diff: **Faraday** (`code-reviewer`) APPROVE and **Plato** (`security-auditor`) PASS.
- Windows CI correction reviews: **Archimedes**, **Anscombe**, **Erdos**, **Newton**, and **Boyle** (`code-reviewer`); **Lagrange**, **Avicenna**, **McClintock**, **Volta**, and **Fermat** (`security-auditor`). Final delta review: **Boyle** APPROVE and **Fermat** PASS. Their findings led to an explicitly test-only, Tier-2 WASM timeout override that is disclosed in the report and never promotes `TESTED` badges.
- Final Windows cleanup-harness review: **Herschel** (`code-reviewer`) APPROVE; **Descartes** (`security-auditor`) PASS after the child environment and fixture-path isolation correction.

## Baseline and branch

- Working branch: `develop`.
- `origin/main` baseline: `b17a2fc65f3ce139507897244d2897a73e187b34`.
- Working branch is `develop`; the last pushed PR head is `b56dfd262741fb0029fff716a48a9c2abe67808b`. A focused Windows test correction is being prepared on the same branch.
- PR [#18](https://github.com/nguywnben/tanpopo/pull/18) is open from `develop` to `main`. No Unit 38 work has started.

## Implemented checkpoint

- Added the MCP side-effect firewall and synchronous/asynchronous approval workflow with exact argument canonicalization, key-recipient-aware Four-Eyes checks, reviewer-safe argument previews, single-use replay protection, and invalidation when server/tool/schema context changes.
- Added Admin approval endpoints and OpenAPI contracts, including required decision reasons and transactional audit records. Pending approval webhook events are recorded with approval creation in one transaction; admission is capped at 1,000 per organization and 100 per virtual key, with 30-day expiry cleanup.
- Added sealed webhook receiver URLs, audience checks, redirect blocking, HMAC signing, stable event fan-out, bounded retries, tenant queue limits, and retention measured from terminal transition.
- Made budget, kill-switch, extension-quarantine, and pending-approval events transactional with their state changes. Removed shared-tenant stream counts from notification payloads.
- Forced tenant RLS on PostgreSQL webhook queue data. PostgreSQL dispatcher startup explicitly assumes and verifies `tanpopo_webhook_worker`, discarding the connection if role verification fails. The runbook grants only required credential metadata columns. The current `serve` runtime remains SQLite and is documented as not yet PostgreSQL/RLS validated.
- Added per-organization transaction locks for approval and webhook admission bounds, current active/previous key-recipient self-approval checks, migrations `0019`/`0020`, and aligned SQLite/PostgreSQL schema snapshots. Dispatcher-owned SQLite enqueue retries are bounded and clean up failed commit transactions on the same connection. Governance records include DEC-075 through DEC-080.
- Windows Stage 1 initially failed only in `TestBedrockOfflineCassetteContractHarness`: stream chunk 4 hit the 50 ms watchdog at 83 ms hosted-runner time. DEC-081 keeps production quotas unchanged and adds a capped Tier-2-WASM test-only override; the report discloses its exact duration, no capability receives `TESTED`, and Tier 1/supplied-runtime uses are rejected.

## Verification completed

- Focused regression tests passed for rotation self-approval, public synchronous Admin API approval while MCP waits, SDK asynchronous approval/Admin API/exact replay, concurrent approval admission, concurrent webhook capacity admission, PostgreSQL role fail-closed behavior, busy commit cleanup, and webhook retention. Queue-capacity/role tests passed 10 repetitions; webhook race tests passed.
- `npm ci --ignore-scripts` and `npm audit --audit-level=high` passed; audit reported zero vulnerabilities.
- Full `go test ./...` passed on the current implementation with `CGO_ENABLED=0`, `TANPOPO_TEST_HERMETIC=1`, and `umask 0022`.
- Race tests passed for `internal/admin/http`, MCP approval/hub, `internal/iam/vkey`, and `internal/webhook`. An earlier webhook race attempt exposed SQLite busy contention in the concurrency fixture; bounded retry and the commit cleanup regression were added, then the webhook package race test passed.
- `go run ./scripts/ci/check.go --all` and `git diff --check` passed on the current implementation.
- Local full Go tests and the targeted Bedrock/harness tests pass with the reviewed Windows correction. Its final code and security reviews are **Boyle** APPROVE and **Fermat** PASS.
- First PR CI run [`44548907596`](https://github.com/nguywnben/tanpopo/actions/runs/44548907596): Stage 0, Ubuntu Stage 1, macOS Stage 1, and the release artifact dry run passed; Windows Stage 1 failed at the Bedrock timeout described above. The test-only correction is included in the current PR head and verified by the subsequent run.
- On PR head `54c7ed0e8fc6f63298971ad798a5940fb26ac4b2`, a Windows rerun passed after one of five iterations had timed out while removing a SQLite file. On the next head `b56dfd262741fb0029fff716a48a9c2abe67808b`, CI run [`44556604510`](https://github.com/nguywnben/tanpopo/actions/runs/44556604510) reproduced the timeout. That evidence did not distinguish a runtime handle leak from a test cleanup issue. The revised fixture runs route and shutdown assertions in a child process, drops runtime references, and keeps that process alive while the parent verifies database deletion; only then does it release the child. The subprocess receives only required platform/test variables and explicitly rooted data paths. **Herschel** (code-reviewer) APPROVE and **Descartes** (security-auditor) PASS on the final diff. Focused tests passed five repetitions and under `-race` locally. Awaiting CI on the pushed correction. The release artifact dry run [`44556604509`](https://github.com/nguywnben/tanpopo/actions/runs/44556604509) passed.
- CI run [`44579268784`](https://github.com/nguywnben/tanpopo/actions/runs/44579268784) on `49cf5ff` failed Stage 0 because the standalone child test used an unticketed `t.Skip`, which the repository floor guard prohibits. The child path is now folded into the existing test with a fixed selector and validated random handshake. The local `--floor-guard` gate, focused five-run test, race test, code review, and security review pass on the current unpushed correction; CI must verify the same on the next pushed SHA.
- The official fixture is pinned to `@modelcontextprotocol/sdk` `1.32.2`. Scripted fixtures do not establish support for untested Claude Code, Cursor, or Codex releases.
- No live PostgreSQL server was available, so PostgreSQL runtime behavior has static review and fail-closed unit coverage, not a live integration run.

## Next steps

1. Complete code and security review of the Windows test correction; push it to `develop` and resolve any new CI findings.
2. Mark PR #18 ready and merge under `DEC-072` after required checks pass.
3. Verify the merge SHA, Unit 37 acceptance record, and green post-merge CI on `origin/main`; synchronize `develop` before checking Unit 38 DoR.
