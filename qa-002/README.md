# QA-002 — RC4 independent Alpha/Beta regression

**Workspace:** branch `qa/qa-002-regression`, based on `40bba5b55c2da6fc00b107b315c9b3167fb20169` (2026-11-24).
**Scope:** independent regression; do not modify Tanpopo implementation.
**Release gate:** `GATE-REL-07` remains **BLOCKED**.

This workspace is separate from QA-001. Historical evidence is reference material only; no QA-001 PASS is inherited. QA-001 remains the immutable record at branch `qa/qa-001-handoff-cp65`, commit `d994f82128f0e78a4e7bb7dd4ecd705cee60d85f` in the sibling checkout `/workspace/tanpopo`.

## Current records

- [Baseline and environment CP00](checkpoints/CP00-baseline.md)
- [Coverage matrix](COVERAGE-MATRIX.md)
- [Historical defect retest register](DEFECT-RETEST-REGISTER.md)
- `evidence/` — sanitized command outcomes, API/state snapshots, and visually reviewed screenshots.
- `reports/` — phase reports and new defect findings (stable IDs begin at DEF-016).

Each checkpoint records the tested revision/runtime, actions, evidence, and PASS / FAIL / BLOCKED / UNVERIFIED result. A PASS is limited to the exact scenario evidenced. No test or artifact may be called PASS based only on source inspection, HTTP success, or an automated test.

## Evidence hygiene

Use disposable local databases and synthetic provider/integration fixtures. The configured provider keys may only be passed to the intended provider request from the runtime environment; do not print, persist, or include them in screenshots or reports. Sanitize response headers and logs. Keep databases, credentials, key material, and raw runtime logs outside version control. Store reproducible summaries and screenshots here.

## Checkpoint status

| Checkpoint | Scope | Result |
| --- | --- | --- |
| CP00 | Exact baseline, artifact inventory, environment/tooling diagnosis | PARTIAL PASS; no published RC4 build/release artifact found; see checkpoint |
| CP01 | Fresh-install Setup and first authenticated session | PASS for the executed setup/login path; broader persistence, roles, and features remain open |
| CP02 | Release linker provenance and full Go race suite | Linker settings verified; Hydrogen artifact UNVERIFIED; race suite PASS (86 packages) |
