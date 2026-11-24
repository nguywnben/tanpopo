# QA-OBS-009: Workspace selection returns to runtime default after refresh

**Classification:** Unresolved product preference/UX contract; not currently a confirmed defect.

## Observation

In a real Chromium owner session, a fresh login selected `QA002 CP43 Secondary Project / qa002-cp43-env2`. The owner then selected `QA002 CP39 Tenant Project / qa002-cp39-env` using the workspace chooser. FinOps displayed the two persisted rows for that project's environment and project. A full page refresh returned to the secondary project/environment, matching the fresh-login/runtime default, and showed its `No budgets configured` state with zero project-local rows.

The selected workspace and view changed together. The project1 rows were not deleted; they were visible before refresh. No budget mutation occurred.

## Uncertainty

The available product contract does not specify whether the last manually selected workspace should persist across refresh, new sessions, or devices, nor whether an explicit “default workspace” preference exists. This is therefore not assigned a defect ID. If the intended contract is to persist the user's most recent selection, the behavior is a user-facing state-retention defect; if runtime default is intentional, the product should make that reset clear where it affects the displayed data.

The selected project2 view also does not include the persisted organization budget ceiling; that is separately confirmed as DEF-024 and is not reclassified here.

## Evidence and reproducibility

1. Sign in as the synthetic owner.
2. Select `QA002 CP39 Tenant Project / qa002-cp39-env` in the top workspace chooser.
3. Open FinOps → Budgets & Holds and observe two rows.
4. Reload the page and observe the runtime-selected secondary project/environment and zero local rows.

Evidence: `../evidence/CP49-finops-before-refresh.png`, `../evidence/CP49-finops-after-refresh.png`, and `../evidence/CP49-workspace-refresh-state.json`; checkpoint: `../checkpoints/CP49-workspace-default-after-refresh.md`.
