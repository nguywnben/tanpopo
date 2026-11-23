# CP19 — DEF-001 targeted Setup tests on current baseline

**Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`  
**Checkpoint parent:** `0b5846c83ee8089a6cca5dd111989ee5aa8117df`  
**Implementation changes:** none.  
**Release gate:** GATE-REL-07 remains **BLOCKED**.

## Test execution

From `web/console`, reran the two exact test cases named in the historical DEF-001 reproduction:

```text
npm test -- src/pages/console.test.tsx -t 'hides unavailable presets|offers only adapters'
```

Vitest reported **2 passed, 11 skipped**. Installed dependencies included `@astryxdesign/core@0.7.16` and axe-core 4.14.1. Sanitized output is `evidence/CP19-def001-targeted-vitest.txt`.

## Reconciliation

QA-001's original failure and CP63 failure output are preserved in the immutable historical workspace and came from the pre-engineering QA-001 baseline. QA-002 tests source commit `40bba5b`, after PR #43. Both targeted assertions now pass. Independently, CP03 exercised the post-engineering Setup wizard in a real Chromium browser and reported all four step names with visible text plus zero axe violations; CP17 visually retested the clean-install flow and inspected its screenshots.

**Result:** DEF-001 is **VERIFIED FIXED (scoped)** on the QA-002 baseline for the original two Setup test states and the executed Chromium wizard states. This does not establish accessibility across all shared stepper uses, routes, screen readers, browsers or locales. QA-001 history remains unchanged and is not converted into a QA-002 pass.

No product source or test files were changed in this checkpoint.
