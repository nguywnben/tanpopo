# CP126 — RC9 Final Foundation Verification

**Baseline:** `ac9ae87a68f095203de82968ac5d08729e33a531`
**Parent evidence:** `qa/qa-005-closeout-rc8` at `7073e2fecfdabf45f5a8d445d8d4b3b4c1f22889`, CP125
**Mission scope:** bounded Engineering Round 6 verification; no comprehensive campaign restart
**DEF-081:** PASS (scoped)
**DEF-082:** PASS (scoped)
**New defects:** none confirmed; no DEF-087 assigned
**Verdict:** **BASELINE_STABLE** for entry to TEEP-01 research only
**GATE-REL-07:** **BLOCKED**

## Checkpoint summary

- Downloaded and identity-checked artifact 15204068659 from run 46048372483 before expiry; exact commit and Linux/amd64 binary identity match RC9. All five archive manifest entries matched before temporary cleanup.
- Used the actual RC9 binary with PostgreSQL 16 FORCE RLS, restricted NOBYPASSRLS application role and a healthy three-primary Valkey Cluster.
- Actual Console approval and provider setup succeeded after an explicit organization trust policy. Corrected one local fixture endpoint configuration error.
- Exercised actual OpenAI-compatible SSE and native Ollama NDJSON streams. Quarantine terminated in-flight streams; both protocol requests were rejected with 503 `extension_quarantined` and generated zero additional outbound fixture POSTs.
- Real Console switch inventory and security badge cleared immediately without a reload; explicit unquarantine and permission reapproval restored `ENABLED_ACTIVE`.
- Restart retained both enabled adapters. Actual post-restart inference succeeded through both protocols.
- All eight targeted PostgreSQL forced-RLS tests passed. Four-eyes independent approval, containment webhook queue/atomic rollback and bounded SQLite harness tests passed with their narrower scopes stated in the report.
- Audit chain verified intact. No active switches remained; both adapters were active after recovery.

## Preserved limitations

CP125's 15 historical PARTIAL findings and all DEF-001–086 records remain. Explicit revoked-principal testing, PostgreSQL independent approval success, actual webhook delivery, live SQLite runtime, broader platform/browser coverage, production signing and local cryptographic artifact attestation remain unverified or blocked. The full residual-risk register and historical ledger overlay are in the final-rc9 package.

This checkpoint does not declare QA-005 complete or product release-ready. `BASELINE_STABLE` means sufficient for TEEP-01 research; production certification is separate. No source code, specification, protected branch, owner signing manifest, tag or release was changed.
