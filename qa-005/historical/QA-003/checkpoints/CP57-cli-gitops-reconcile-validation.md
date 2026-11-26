# CP57 — CLI GitOps reconcile and safe failure behavior

**Baseline:** RC5 source `3423d4f523ef0ac77469a1201b8b753d1d31c902`; exact-source CLI `/tmp/tanpopo-qa003`
**Scope:** `tanpopo config reconcile` with fully synthetic config, lockfile and artifact under `/tmp`; no Tanpopo service or external package source contacted
**Status:** scoped CLI acceptance passed; FT-15 and WF-14 remain PARTIAL; QA-003 continues and `GATE-REL-07` remains BLOCKED.

## Procedure and results

1. Prepared a synthetic declarative file containing a loopback endpoint and a complete `${ENV:QA003_SYNTHETIC_PROVIDER_KEY}` reference, a synthetic empty applied config, and a lockfile pinning a tiny non-executable `.tpkg` fixture under `@qa003/synthetic`.
2. Ran the exact RC5 CLI with `--output json`, explicit `--desired`, `--applied`, `--lockfile`, and `--artifact` paths. Reconcile exited 0 and reported APPLIED. The applied file exactly matched desired bytes; the lockfile SHA-256 was unchanged.
3. Modified the local synthetic artifact without changing the pinned digest. Reconcile exited 14 (CLI security rejection); the previously applied config and lockfile remained byte-for-byte unchanged.
4. Replaced the desired file in turn with an inline secret sentinel, a partial `${ENV:...}` reference, a malformed environment-variable name, duplicate YAML keys, and a symlink to the desired file. Each invocation exited 14; the applied config and lockfile remained unchanged. The synthetic sentinel was not present in captured CLI output. Raw CLI diagnostics were not stored.
5. The first draft fixture used uppercase `OFFICIAL` for `trustTier` and failed lockfile validation. The canonical value is lowercase `official`; after correcting this harness fixture, the valid-path invocation passed. This was test-fixture setup, not product behavior.

Sanitized results: [CP57-gitops-reconcile-result.json](../evidence/CP57-gitops-reconcile-result.json). The local synthetic files remain under `/tmp/qa003-cp57-gitops` and are not part of the handoff branch.

## Coverage and limits

- The valid local CLI reconciliation path, lockfile immutability, package-digest enforcement, rejected secret/reference/YAML inputs, and symlink rejection are now directly exercised on the exact source baseline.
- This does not establish reconciliation against a live authenticated instance, provider package semantics, lockfile concurrency/drift during a long reconcile, process interruption, restart consumption of applied config, environment-secret resolution, or platform-specific filesystem behavior. No Tanpopo implementation changed and no new product defect was confirmed.
