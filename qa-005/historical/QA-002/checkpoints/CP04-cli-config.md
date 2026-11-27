# CP04 — Built CLI config command regression

**Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`  
**Status:** PASS for CLI command routing, JSON formatting, and fail-closed missing-file validation against a local synthetic endpoint; real Admin API authorization and valid reconciliation remain UNVERIFIED.

Ran the exact baseline binary (no source changes). A disposable local fixture returned a sanitized summary from `/admin/v1/overview/summary`; `tanpopo --output json config show` exited 0 and rendered the summary. `tanpopo config --help` exited 0 and listed both `show` and `reconcile`. Invoking reconcile with an absent desired file exited nonzero (14), stated that configuration was rejected, and created neither applied config nor lockfile.

DEF-004's historical duplicate-command/shadowing symptom did not reproduce in these command-resolution checks. Because the fixture accepts a synthetic token and is not Tanpopo's Admin API, this does not verify server auth, tenant scoping, or integration against persisted service state. Evidence: `evidence/CP04-cli-config.json`. The local fixture source and command output are kept outside the repository; no tokens or service logs are recorded.
