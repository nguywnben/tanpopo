# CP68 — Repeated crypto-shred finalization and receipt semantics

**Baseline:** exact frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`  
**Runtime:** isolated SQLite runtime `127.0.0.1:18128`  
**Browser:** authenticated Operator Console in Chromium; actual audit page screenshot visually inspected  
**Product source modified:** no

## Result

- **PASS (scoped):** After CP67 had successfully shredded its synthetic canary organization, two sequential same-origin `POST /admin/v1/organizations/{orgID}/finalize-erasure` retry calls each returned HTTP 200 with the same target and the same receipt fields.
- **PASS (scoped):** A fresh filtered audit read after both retries still contained exactly one `organization.crypto_shredded` event for the target (sequence 5). The real Security → Audit Log page showed “Hash chain verified” and RFC 8785 integrity across 293 events; the screenshot was visually inspected.
- **UNVERIFIED / contract unclear:** The original CP67 deletion response returned the concrete destroyed DEK ID ending in `:v1:g1`. Re-finalization returns the stable sentinel ending in `:shredded`. Both are non-empty `destroyed_dek_id` values and the API schema does not specify replay equality. Reviewed prose promises idempotent resumability, not exact receipt replay. No defect is assigned from this difference alone.
- **UNVERIFIED:** Whether retry after an initial deletion that revoked one or more active Virtual Keys reproduces the original revoked-key count. This canary had zero VKEYs to revoke. Interrupted Phase 2/3 recovery and concurrent retries are also outside this browser/API slice.

## Reproduction

1. Use the isolated exact-RC6 runtime and the synthetic organization `org_01m8a833jxe00ahft1pharg45y`, which CP67 had already deleted through the Operations Console.
2. Authenticate as the test Operator through the actual Chromium Console. Open Security → Audit Log and select **Verify hash chain**.
3. In that authenticated browser context, send the same-origin CSRF-protected `POST /admin/v1/organizations/org_01m8a833jxe00ahft1pharg45y/finalize-erasure` twice.
4. Compare both status/receipt responses, then read the target's `organization.crypto_shredded` audit events.
5. Confirm each retry returns 200, response fields match between retries, exactly one target shred event remains, and the Audit Log reports an intact chain.

Sanitized outcomes are in [CP68 evidence](../evidence/CP68-idempotent-finalization.json). The screenshot [shows the real Console verification state](../evidence/CP68-audit-chain-verified.png). CP67 contains the original deletion result and populated canary evidence.

## Contract and source review

The accepted security specification says organization erasure remains idempotently resumable if finalization is interrupted while the tenant is `DELETING` ([`secrets-and-tenant-isolation.md`](../../docs/security/secrets-and-tenant-isolation.md#L119)). The endpoint source also describes retry finalization as idempotent. RC6 returns a synthesized `orgID:shredded` identifier on the already-completed path. The current contract does not state that a retry must replay the original generation-specific DEK identifier or original revoked-key count. The evidence therefore supports idempotent status/no duplicate audit, while exact receipt semantics remain an open contract question.

This does not test the destructive delete again; the target was already erased at CP67. It does not establish the `<60s` SLA, crash-window recovery, or multi-node behavior. QA remains ongoing and `GATE-REL-07` remains **BLOCKED**.
