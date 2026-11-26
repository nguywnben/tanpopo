# QA-001 CP65 handoff package

This directory is the portable, QA-only handoff package for independent engineering and follow-up QA. Start with [QA-001 Engineering Handoff CP65](QA-001-ENGINEERING-HANDOFF-CP65.md), then use the linked historical reports, defect register, coverage matrix, and evidence index. All original CP checkpoints and the 15 defect records are retained.

## Package contents

- Consolidated engineering handoff: `QA-001-ENGINEERING-HANDOFF-CP65.md`
- Historical defect register: `DEFECT-REGISTER.md`
- Historical coverage and phase updates: `PRODUCT-COVERAGE-MATRIX.md`
- QA reports: Alpha, Beta/UX, business logic, regression, security/reliability, master test plan, and release readiness.
- Evidence index and phase records: `evidence/` (screenshots and structured sanitized JSON/text results).
- Targeted standalone handoffs: `engineering-handoffs/`.

## Sanitization and scope

The package contains QA records and synthetic test evidence only. It does not include product source changes, secrets or credentials, database files, or sensitive runtime logs. Synthetic identities, resource IDs, and local fixture details in the records are QA evidence. One-time credential values were not retained in evidence. Reviewers should continue to handle the package as internal QA material.

## Status

QA-001 is incomplete. All 15 defects remain open; some historical results conflict or are limited to one path, as documented. Coverage includes PASS, FAIL, BLOCKED, and UNVERIFIED results. No release readiness or stability claim is made. **GATE-REL-07 remains BLOCKED.**

## Integrity

The publication commit contains files under `qa/` only. It does not merge into the product branch and does not publish a release. To verify the evidence package after checkout, run `sha256sum -c HANDOFF-SHA256SUMS` from this directory; the checksum manifest is generated at publication time and excludes itself.
