# CP105 — DEF-032 exact duplicate provider identity and target selection

**Date:** 2026-11-27  
**Disposition:** **PASS (scoped; exact duplicate identity and action targeting)**  
**Source:** `737079ddb847d97dbcf832f610ed0048556cb65b` (unsigned RC7)  
**Runtime:** exact-source Linux binary SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`; isolated SQLite runtime at `18145`, real Console through `https://127.0.0.1:19444`  
**Browser / role:** Chromium 156, real Playwright UI, Org Owner, 1440×1000, QA005 project / Budgetless Dev

## Reproduction

Created disposable, write-only-credential-free connections through the actual Console. The first pair shared name, adapter, endpoint `http://127.0.0.1:19096`, billing mode, region, CIDR and project. The second pair used the same fields and `http://127.0.0.1:19090`, a deterministic local OpenAI-compatible fixture. Both loopback forms explicitly enabled the private/loopback option and allowed only `127.0.0.1/32`. No paid or external provider was contacted.

1. Inspect Providers → Connections: the exact 19096 pair has stable suffixes `(fz2mwcxk)` and `(bwqsn6fv)` despite identical visible configuration.
2. Open Models → Add deployment → Provider connection: both duplicate options carry their distinct stable suffix. Cancel without saving.
3. Open Edit on the `fz2mwcxk` row: the drawer heading identifies that short ID. Cancel without saving.
4. Select Disable from that row's menu. HTTP 200/readback and the real UI show only `fz2mwcxk` Disabled; the identical `bwqsn6fv` sibling remains Unverified.
5. Delete `fz2mwcxk`. The confirmation quotes that exact name and suffix. HTTP 204/readback leaves `bwqsn6fv` present. Delete the remaining `bwqsn6fv` by its row; its confirmation quotes its own suffix; HTTP 204/readback leaves no test pair.
6. On the second exact pair, select Probe from the row carrying suffix `7nyfv9rh`. The deterministic loopback probe returned HTTP 200, `valid:true`, latency 7 ms; only `7nyfv9rh` changed to Active while duplicate sibling `qed4wk3s` remained Unverified. Both disposable records were deleted through the UI and final readback found zero test-pair rows.

Screenshots for list, picker, edit, one-row disable, probe target result and both delete confirmations were visually inspected. Evidence preserves the exact synthetic suffixes, state changes, API statuses and screenshot hashes. No secret values were created, read, or stored.

## Result and scope

The full reported ambiguity was not reproduced after stable IDs were added. A same-project pair with identical provider fields remained distinguishable in the list, deployment picker, edit drawer and delete confirmation, and row-scoped Probe/Disable/Delete affected only the selected record. This closes DEF-032 for this Owner/SQLite/Chromium exact-RC7 workflow. It does not certify other roles, browsers, storage backends, pagination, or long identifiers. The earlier CP104 **PARTIAL** reflects the then-available inventory, which contained no duplicate names; CP105 adds the previously missing duplicate fixture and supersedes that current disposition without removing CP104 history.

Evidence: [sanitized result](../evidence/CP105-def032-exact-duplicate-provider-identity.json), [list](../evidence/screenshots/CP105-def032-list.png), [deployment picker](../evidence/screenshots/CP105-def032-picker.png), [edit](../evidence/screenshots/CP105-def032-edit.png), [disabled selected row](../evidence/screenshots/CP105-def032-selected-disabled.png), [successful Probe](../evidence/screenshots/CP105-def032-duplicate-probe-target.png), and [delete confirmations](../evidence/screenshots/CP105-def032-delete-first.png) / [second](../evidence/screenshots/CP105-def032-delete-second.png).

`GATE-REL-07` remains **BLOCKED**; QA-005 continues.
