# QA-003 — Complete Alpha/Beta Regression

**Status:** In progress; not complete. **GATE-REL-07: BLOCKED.**

## Frozen baseline and immutable history

- RC5 source: `3423d4f523ef0ac77469a1201b8b753d1d31c902`
- QA-001 branch / commit: `qa/qa-001-handoff-cp65` / `d994f82128f0e78a4e7bb7dd4ecd705cee60d85f`
- QA-002 branch / commit: `qa/qa-002-regression` / `1052ecb25a51e7ea94045946ede865ee062d4419`
- Product source and tests are not changed by this branch. Only `qa-003/` documentation and sanitized QA evidence belong here.

## Current checkpoint

See [CP00 — baseline and environment inventory](checkpoints/CP00-baseline-environment.md). QA-003 independently retests all 39 historical defects; every row currently starts UNVERIFIED. Historical findings and acceptance criteria are referenced without importing prior PASS results.

## Records

- [Historical defect retest register](DEFECT-RETEST-REGISTER.md)
- [Full product coverage matrix](COVERAGE-MATRIX.md)
- Checkpoints and evidence are appended under `checkpoints/` and `evidence/`.
- New findings begin at DEF-040 in `reports/`.

## Operating constraints

Use isolated synthetic tenants/endpoints and keep cumulative real provider spend at or below USD 3. Credentials must not appear in docs, screenshots, logs, or source control. No implementation fixes, releases, tags, or human release approval are part of QA-003. Blocked and untested work remains explicitly labeled. QA continues past routine checkpoints.
