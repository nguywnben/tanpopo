# CP80 — DEF-001 fresh setup progress accessible names

**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`  
**Runtime:** fresh isolated exact-RC6 instance at `127.0.0.1:18140`, separate temporary data directory  
**Browser:** real Chromium at 1440×1000  
**Product source modified:** no

## Result

**PASS (scoped) — DEF-001 did not reproduce in the current exact-RC6 fresh setup state.** On the first setup step, Chromium's accessibility tree reported each progress button's accessible name with its corresponding visible title:

- `Owner account` → `Go to step 1: Owner account`
- `2 First connection` → `Go to step 2: First connection`
- `3 Key custody` → `Go to step 3: Key custody`
- `4 Review` → `Go to step 4: Review`

Steps 2–4 carried `aria-disabled="true"` while setup prerequisites remained incomplete. Clicking was not attempted for those disabled steps. No bootstrap or owner data was submitted. The real setup screenshot was visually inspected.

## Reproduction and evidence

1. Start the exact RC6 binary in a new, separate temporary working/data directory with an isolated test Master KEK file and loopback listener.
2. Open `/setup` in Chromium; wait for the bootstrap-token field to load.
3. Read the browser accessibility tree and compare each progress button name with its visible step title and `aria-disabled` state.

Sanitized names and limits are in [the accessibility readback](../evidence/CP80-def001-accessible-names.json); the inspected first-step screen is [here](../evidence/CP80-fresh-setup-step1.png).

## Expected versus actual

- **Expected:** progress controls expose accessible names that include their visible step labels and do not allow users to skip incomplete setup prerequisites.
- **Actual:** the AX names contain all four visible titles; later steps are marked unavailable until preceding required values are supplied.
- **Limits:** one fresh state in Chromium only. The prior `Vitest` `label-content-name-mismatch` failures remain unreconciled; axe-core was not installed in the isolated runner. This is a scoped retest, not complete accessibility acceptance.

QA continues; **GATE-REL-07 remains BLOCKED**.
