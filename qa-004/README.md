# QA-004 — Comprehensive Release Acceptance

Frozen RC6 baseline: `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1` (PR #46). QA-only branch: `qa/qa-004-regression`.

QA-004 is an independent campaign. Historical QA-001/002/003 results are preserved as references; every one of DEF-001 through DEF-050 starts `UNVERIFIED`. New confirmed defects begin at DEF-051. Status values are `PASS`, `FAIL`, `PARTIAL`, `BLOCKED`, and `UNVERIFIED`. Evidence is sanitized and the execution data directory and test credentials are excluded.

## Current checkpoint

- [CP01 artifact, clean install, and provenance](checkpoints/CP01-artifact-install-provenance.md)
- [CP02 marketplace and authenticated native ingress](checkpoints/CP02-marketplace-native-ingress.md)
- [Historical defect register](HISTORICAL-DEFECT-REGISTER.md)
- [Coverage matrix](COVERAGE-MATRIX.md)
- [New defects](NEW-DEFECT-REGISTER.md)
- [Release signing readiness](RELEASE-SIGNING-READINESS.md)

QA is ongoing. No full product acceptance or release approval is claimed. **GATE-REL-07 remains BLOCKED.**
