# TP-IMP-0038 Execution Checkpoint

**Status:** **`DONE`** under `DEC-085`. PR #23 merged into `main` at `7c5538ff9e9c6530e505c07f77382d9647b039b1`; all four required post-merge CI jobs passed on that SHA.

## DoR evidence

- `TP-IMP-0020`, `TP-IMP-0029`, `TP-IMP-0030`, and `TP-IMP-0037` are DONE. Unit 37 implementation PR [#20](https://github.com/nguywnben/tanpopo/pull/20) and acceptance governance PR [#21](https://github.com/nguywnben/tanpopo/pull/21) are merged.
- The four required post-merge checks for Unit 37 passed in [CI run 44678817933](https://github.com/nguywnben/tanpopo/actions/runs/44678817933) on `d7999240b00c1370bfd84286b1c9f5350be9c2f0`.
- `GAP-01` and `GAP-07` are CLOSED; `CR-10` is approved. `DEC-084` is on main in [PR #22](https://github.com/nguywnben/tanpopo/pull/22), merged at `bb0fcd295c0e6191578211c15548d0ebd068b71d`. All four required post-merge checks passed in [CI run 44682388225](https://github.com/nguywnben/tanpopo/actions/runs/44682388225).

## Implementation and review

- Base SHA: `bb0fcd295c0e6191578211c15548d0ebd068b71d`. Implementation commit `b383e49217de8a71b0631437d1b77e68af0120f3` and CI correction `60120d01ecbd5496f2ff542465d4378c5eaa6991` were merged via PR #23.
- Implementer and delegated acceptance owner: **GPT-6 Luna**. Independent final reviewer: **Noether** (`code-reviewer`), APPROVE on the final diff, including the early-cancellation CI correction.
- `POST /admin/v1/playground/sessions` now creates an owner-bound ephemeral key, applies TTL and budget limits, enforces a `TOTAL_LIFETIME` virtual-key ledger through the exact key expiry (including month rollover), and revokes the key if ledger setup fails.
- Route Explain requires a data-residency selection, constrains it to inherited policy when present, and includes it in the dry-run request.
- Playground streams with an ephemeral key and `credentials: omit`; prompts stay in browser memory. It supports comparison, captures both `X-Tanpopo-Request-Id` values, and shows matching Admin API trace summaries.
- MCP Hub lists/registers servers, syncs tool schemas, surfaces drift, and grants tools to selected environment keys. The approval queue renders canonical argument previews as text, blocks schema-drifted approvals in both UI and Admin API, and reports successful decisions after refreshing the queue.
- Astryx remains `0.7.16`, neutral theme, StyleX, and local Figtree assets. No dependency or lockfile changed.

## Verification

- Full Go suite: `umask 0022; CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test ./...` — passed with local socket access. The first unrestricted run exposed a stale route-count assertion (`89`); it now expects `90`, and the full rerun passed.
- Full console suite: `npx vitest run --no-file-parallelism --maxWorkers=1` — 65 tests passed across 9 files.
- `npm run typecheck` — passed.
- `npm run build` — passed; initial JavaScript gzip is 123.90 KB, under the 250 KB limit.
- `npm run doctor -- --built dist` — 0 failures (20 pass, 2 warnings). The theme-font warning is Doctor's inability to prove the local Figtree font mapping from neutral theme metadata; the build contains local Figtree `.woff2` assets and the air-gap gate passed.
- Selected repository gates `--floor-guard --ui-layout --canary --dag --license --lockfiles` — passed.
- Unit 38 Vitest accessibility checks use axe-core and passed with no critical/serious violations.
- Temporary Chromium Playwright journeys passed: OAuth PKCE initiation/redirect; MCP register/sync/grant; approval schema-drift blocking, escaped preview and approval; Playground comparison with both traces and no cookies. A paced 100-chunk stream rendered all chunks at 115 tokens/s from first to last token (107 assistant DOM updates); a 2,000-token stream canceled at 19 rendered characters.
- `git diff --check` — clean.

## Acceptance and GitHub evidence

- Implementation PR [#23](https://github.com/nguywnben/tanpopo/pull/23) merged at `7c5538ff9e9c6530e505c07f77382d9647b039b1`. **GPT-6 Luna** implemented and accepted Unit 38 under `DEC-072`/`DEC-085`; **Noether** (`code-reviewer`) approved the final diff.
- PR CI [run 44693349787](https://github.com/nguywnben/tanpopo/actions/runs/44693349787) passed Stage 0 and Stage 1 on Ubuntu, macOS, and Windows. Windows passed the repeated runtime database cleanup regression. Release Artifact Dry Run [run 44693349866](https://github.com/nguywnben/tanpopo/actions/runs/44693349866) completed successfully without publishing.
- Post-merge CI [run 44695353169](https://github.com/nguywnben/tanpopo/actions/runs/44695353169) passed all four required jobs on the actual `origin/main` SHA `7c5538ff9e9c6530e505c07f77382d9647b039b1`.
- `develop` was fast-forwarded to the verified main SHA before this governance acceptance update. `DEC-085`, the implementation index, dependency graph, milestone map, readiness summary, and this execution record capture Unit 38 as DONE. Units 39–47 remain BLOCKED. No tag or release was created under `GATE-REL-07`.
