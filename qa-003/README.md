# QA-003 — Complete Alpha/Beta Regression

**Status:** In progress; not complete. **GATE-REL-07: BLOCKED.**

## Frozen baseline and immutable history

- RC5 source: `3423d4f523ef0ac77469a1201b8b753d1d31c902`
- QA-001 branch / commit: `qa/qa-001-handoff-cp65` / `d994f82128f0e78a4e7bb7dd4ecd705cee60d85f`
- QA-002 branch / commit: `qa/qa-002-regression` / `1052ecb25a51e7ea94045946ede865ee062d4419`
- Product source and tests are not changed by this branch. Only `qa-003/` documentation and sanitized QA evidence belong here.

## Current checkpoint

See [CP00 — baseline and environment inventory](checkpoints/CP00-baseline-environment.md), [CP01 — exact-source build and fresh-install journey](checkpoints/CP01-fresh-install-and-provenance.md), [CP02 — crypto-shred and restore](checkpoints/CP02-crypto-shred-and-restore.md), [CP03 — membership scope and enrollment](checkpoints/CP03-membership-scope-and-enrollment.md), and [CP04 — audit failure atomicity](checkpoints/CP04-audit-failure-atomicity.md). QA-003 independently retests all 39 historical defects; prior campaign results are reference material only.

CP05 adds the [webhook lifecycle retest](checkpoints/CP05-webhook-lifecycle.md). CP06 records [WebKit mobile coverage and browser runtime limits](checkpoints/CP06-webkit-mobile-and-browser-runtime.md).

## Latest consolidated status (CP06)

- Historical defects: **5 VERIFIED FIXED** for the stated RC5 scopes (DEF-009, DEF-021, DEF-023, DEF-034, DEF-038); **1 PARTIAL** (DEF-007); **33 UNVERIFIED**. No historical defect has a fresh RC5 FAIL or BLOCKED disposition yet.
- New defects: none confirmed to date. No issue is inferred from test harness mistakes or known security boundaries.
- Coverage: partial on FT-04, FT-09, FT-12, FT-15, FT-16; SCR-01, SCR-02, SCR-03, SCR-13, SCR-14, SCR-16; WF-01 and WF-13. WebKit mobile is partial; Firefox is blocked by container sandbox/filesystem restrictions; other platforms are unverified. Other coverage remains UNVERIFIED. RC5 release artifact verification remains UNVERIFIED because the RC4/RC5 artifacts were stored in a separate DevBox not available here.
- Active environment: fresh and restored local SQLite runtimes, real Chromium interactions and visually inspected screenshots. Go build/dependency resolution succeeded in the container; the interrupted race suite package was rerun after installing its test runtime dependency and passed (CP01). External provider spend is USD 0.
- Remaining: independently retest the other 33 defects, continue all feature/screen/workflow and platform coverage, and test other persistence backends, webhook delivery, and remaining DEF-007 paths. QA-003 is not complete; `GATE-REL-07` remains **BLOCKED**.

## Records

- [Historical defect retest register](DEFECT-RETEST-REGISTER.md)
- [Full product coverage matrix](COVERAGE-MATRIX.md)
- Checkpoints and evidence are appended under `checkpoints/` and `evidence/`.
- New findings begin at DEF-040 in `reports/`.

## Operating constraints

Use isolated synthetic tenants/endpoints and keep cumulative real provider spend at or below USD 3. Credentials must not appear in docs, screenshots, logs, or source control. No implementation fixes, releases, tags, or human release approval are part of QA-003. Blocked and untested work remains explicitly labeled. QA continues past routine checkpoints.
