# CP45 — Extension upgrade and rollback discoverability

**Status:** FAIL for the documented empty-rollback-state UI; upgrade/re-consent coverage remains UNVERIFIED. Overall extension lifecycle coverage remains PARTIAL.
**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`.
**Product source:** unchanged.

## Scope and results

Used a real Chromium session as the synthetic `OPERATOR` / `ORG_OWNER` in the separate healthy test-root runtime at `127.0.0.1:18128`. The installed test-root Ollama package was `ENABLED_ACTIVE`, version `0.1.0`, row version 2, with no candidate or previous rollback pin. Its test-root signature is not authentic production signing evidence.

1. **Installed inventory and action menu — FAIL (DEF-060).** The real Console showed the active installation. The action menu contained Review permissions, Upgrade package and Quarantine package; it omitted Rollback. Authenticated API readback confirmed `previous_rollback_version:null`. The specified empty state in `docs/ux/user-flows.md` requires a disabled Rollback action with a tooltip explaining there is no previous pin and identifying Disable/Quarantine as emergency choices.
2. **Upgrade form — PARTIAL.** The real action opened a side panel stating “Any permission escalation triggers explicit re-consent” and requiring a source URI and expected SHA-256. The offline marketplace had only the currently installed Ollama version `0.1.0`; no higher version or eligible test upgrade candidate was available. No candidate was submitted and no installation state was changed. Upgrade, permission-diff, re-consent, activation, rollback, and recovery remain UNVERIFIED.
3. **Marketplace provenance — scoped read.** The real offline Marketplace displayed the cached package versions and separate trust/signature columns. Ollama's verified badge reflects the isolated test root only; the other seven bundled packages remain unsigned/unverified as recorded in CP41. This is not production signing evidence.

## Evidence

- `evidence/CP45-extension-installed-18128.png` — actual Installed screen; visually inspected.
- `evidence/CP45-extension-actions-18128.png` — actual row menu showing that Rollback is absent; visually inspected.
- `evidence/CP45-extension-upgrade-form-18128.png` — real side-panel upgrade form and explicit re-consent notice; visually inspected. No submit action was taken.
- `evidence/CP45-marketplace-18128.png` — actual offline catalog, visually inspected.
- `evidence/CP45-extension-lifecycle-preconditions.json` — sanitized install state, no previous pin, catalog version, and action list.
- Contract: `docs/ux/user-flows.md`, SM-01 Extension Upgrade, Empty state.

## Limits and next coverage

No upgrade, permission escalation, candidate signature, active stream, rollback, or state mutation was exercised because the available offline catalog has no newer Ollama version and no eligible test candidate was staged. Do not interpret this as a passing lifecycle. Next coverage needs an isolated test-only signed candidate (never an authentic release signature) or an equivalent authorized synthetic test artifact; production trust policy remains enforced. The broader campaign is ongoing and **GATE-REL-07 remains BLOCKED**.
