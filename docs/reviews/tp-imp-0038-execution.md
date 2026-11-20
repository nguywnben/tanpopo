# TP-IMP-0038 Execution Checkpoint

**Status:** Implementation is complete locally on `develop`; Unit 38 remains `READY` under `DEC-084` until its PR is merged and post-merge CI passes. No `DONE` claim is made.

## DoR evidence

- `TP-IMP-0020`, `TP-IMP-0029`, `TP-IMP-0030`, and `TP-IMP-0037` are DONE. Unit 37 implementation PR [#20](https://github.com/nguywnben/tanpopo/pull/20) and acceptance governance PR [#21](https://github.com/nguywnben/tanpopo/pull/21) are merged.
- The four required post-merge checks for Unit 37 passed in [CI run 44678817933](https://github.com/nguywnben/tanpopo/actions/runs/44678817933) on `d7999240b00c1370bfd84286b1c9f5350be9c2f0`.
- `GAP-01` and `GAP-07` are CLOSED; `CR-10` is approved. `DEC-084` is on main in [PR #22](https://github.com/nguywnben/tanpopo/pull/22), merged at `bb0fcd295c0e6191578211c15548d0ebd068b71d`. All four required post-merge checks passed in [CI run 44682388225](https://github.com/nguywnben/tanpopo/actions/runs/44682388225).

## Implementation and review

- Base SHA: `bb0fcd295c0e6191578211c15548d0ebd068b71d`. Implementation commit `b383e49217de8a71b0631437d1b77e68af0120f3` is pushed to `origin/develop`.
- Implementer: **GPT-6 Luna**. Independent reviewer: **Noether** (`code-reviewer`), final verdict APPROVE. Acceptance owner: **GPT-6 Luna**, pending required PR and post-merge CI.
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

## GitHub checkpoint and remaining release gate

- No Unit 38 implementation PR exists yet. Public GitHub API inspection found no open PR; the compare page is [main...develop](https://github.com/nguywnben/tanpopo/compare/main...develop?expand=1). Unit 38 is not DONE.
- Fetching public `origin/main` and `origin/develop` succeeded. Main remains `bb0fcd295c0e6191578211c15548d0ebd068b71d`; the tested implementation checkpoint is verified on `origin/develop` at `b383e49217de8a71b0631437d1b77e68af0120f3`.
- Push succeeded, but PR creation is blocked by GitHub CLI authentication: `GH_TOKEN` is rejected as invalid, and `gh pr create` with `GH_TOKEN` unset reports no login. Restore the GitHub CLI credential in the Codex Cloud environment, then open the PR from the compare page or CLI.
- After opening the PR, wait for the four required checks, merge under `DEC-060`, record acceptance, sync `develop`, and verify all four post-merge jobs on the exact main SHA. Do not open Unit 39 or create a tag/release.
