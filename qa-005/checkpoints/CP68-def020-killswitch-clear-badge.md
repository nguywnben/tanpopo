# CP68 — DEF-020 active kill-switch clear and shell badges

**Source:** frozen RC7 exact source `737079ddb847d97dbcf832f610ed0048556cb65b`, built binary `/tmp/qa005-bin/tanpopo` (linux/amd64, SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`); isolated SQLite QA runtime `127.0.0.1:18149`, role `all`; Chromium 156.
**Disposition:** **PARTIAL**. The Security → Kill Switches trigger/clear path did not reproduce stale shell badges. The independent historical approval-queue approve/reject resolution path remains UNVERIFIED, so DEF-020 is not closed.

## Reproduction

1. Signed in as the synthetic QA Owner and opened Security → Kill Switches. Initial API/UI state reported zero active switches.
2. Selected the existing synthetic project (`Initial Project`), entered a clearly labeled QA-only reason, confirmed the exact project slug, previewed the blast radius, and used the actual UI review/trigger path. Preview showed one org, one project, three virtual keys and one connection. Trigger returned HTTP 200, config epoch 43, six affected entities and zero streams severed. The list showed one active project freeze; header and Security navigation displayed badge `1`. The active-state screenshot was visually inspected.
3. Used **Clear switch**, entered a QA-only clear reason, and confirmed. The authenticated UI issued DELETE and received HTTP 200. Follow-up API reads returned an empty active-switch list. Without reloading or navigating away, the Security page displayed `Active kill switches 0`, “No active kill switches”, and the success notice; the header Kill Switch button and Security navigation both had no numeric badge. The screenshot was visually inspected.
4. Authenticated audit API readback returned both `kill_switch.triggered` and `kill_switch.cleared`, with the same switch ID, project target, organization partition and synthetic reason metadata.

## Classification and limits

The tested emergency project-freeze path now refreshes its badges and empty state correctly in the same session, in contrast with the preserved QA-002 repeated stale-badge findings. This is a scoped non-reproduction on the exact RC7 Owner path. The separate approval-queue approve/reject count behavior from historical CP28 remains untested in QA-005; other kill-switch levels, roles, browsers, active-stream termination and request denial/recovery while frozen remain UNVERIFIED. Do not infer they pass.

Sanitized API state and evidence are in [CP68 JSON](../evidence/CP68-def020-killswitch-clear.json); the create form, review confirmation, active switch, clear confirmation and same-session cleared state are visually inspected screenshots. No credentials or secrets are present. The synthetic project freeze was cleared and confirmed absent; no provider was called and no paid use occurred. No implementation/specification change. `GATE-REL-07` remains BLOCKED.
