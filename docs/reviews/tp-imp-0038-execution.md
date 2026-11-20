# TP-IMP-0038 Execution Checkpoint

**Status:** READY under `DEC-084`; implementation has not started.

## DoR evidence

- `TP-IMP-0020`, `TP-IMP-0029`, `TP-IMP-0030`, and `TP-IMP-0037` are DONE on `origin/main` at `d7999240b00c1370bfd84286b1c9f5350be9c2f0`.
- Unit 37 implementation PR [#20](https://github.com/nguywnben/tanpopo/pull/20) and acceptance governance PR [#21](https://github.com/nguywnben/tanpopo/pull/21) are merged. Four required post-merge checks passed in [CI run 44678817933](https://github.com/nguywnben/tanpopo/actions/runs/44678817933).
- `GAP-01` and `GAP-07` are CLOSED; `CR-10` is approved.
- `DEC-084` opens this unit as READY. Implementation starts after the opening decision is on main with green required CI.

## Scope and gates

Implement the spec for zero-side-effect Route Explain and CLI, cost-capped ephemeral Playground sessions, MCP Hub and Approval Queue UI, and the required `GATE-M3` journeys. Preserve the current Astryx version/theme and design system; run typecheck, tests, build, Astryx Doctor on `dist`, axe-core accessibility, and the specified Go route/session security tests.

Review tier: `STANDARD_REVIEW`; obtain an independent `code-reviewer` review of the final diff and rerun review if it changes. **GPT-6 Luna** is the implementer and delegated acceptor. Record the exact name of each review agent that participates after review.
