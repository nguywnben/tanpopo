# CP104 — A2A terminal task transition guards

**Baseline:** frozen RC4 `40bba5b55c2da6fc00b107b315c9b3167fb20169`<br>
**Runtime:** A2A-enabled isolated QA server `18084`<br>
**Role/browser:** authorized Org Owner + Operator, real Chromium at 1440 × 1000<br>
**Fixture:** existing synthetic loopback agent at `127.0.0.1:19186`<br>
**Product implementation:** unchanged

## Reproduction and result

1. A first harness run used a full-page navigation after selecting the project and landed back on the runtime-default `QA002 CP78 Fresh Budget / No env` context; Access & Keys correctly disabled environment-bound key issuance and showed its scope prompt. This repeats QA-OBS-009 and was not treated as a product defect. I then selected the agent project through the Console workspace picker and used in-app navigation to preserve `stage`. Issued a fresh Virtual Key through Access & Keys. After explicitly deselecting the default PROVIDER plane and selecting A2A, the API recorded only `allowed_planes:["A2A"]` and `allowed_routes:["/a2a/*"]`. The key secret remained in memory only and is not in evidence.
2. Submitted a task to the local fixture through the A2A data plane with a 5,000-micro hold. The API returned HTTP 201 in `input-required`; the actual A2A task screen showed Resume and Cancel for that state.
3. Used the real Console Resume action with the synthetic approval input. The same task ID completed with HTTP 200 and 3,000 micros settled. The endpoint recorded exactly the initial task call and one follow-up call; neither carried the Virtual Key Authorization header.
4. After completion, the task table showed an em dash in Actions. Tried a same-origin authenticated Admin API resume again with the correct session CSRF token and same valid payload. The API returned HTTP 409 `task_not_resumable`, stating completed tasks require `input-required`. Tried cancel on the same completed task; API returned HTTP 409 `invalid_state_transition` (`completed -> canceled`).
5. Read task state before and after the terminal attempts. It remained `completed`, version 5, settled cost 3,000 micros. The fixture had exactly two calls for this task and received no extra side effect from either rejected terminal transition. The A2A-only key was revoked through the Console; a subsequent A2A discovery request returned HTTP 401 `invalid_virtual_key`.

Two preliminary duplicate-resume harness requests did not reach state-transition validation: one had a duplicate Content-Type and returned HTTP 415; another omitted the session CSRF token and returned HTTP 403 `csrf_validation_failed`. Both are harness/auth-control outcomes, not product failures. The corrected retry above supplied one JSON Content-Type and the authenticated session's CSRF token; no token or credential value was saved.

The visible no-action state and backend 409 responses align for this completed-task case. This adds a scoped **PASS** for terminal resume/cancel rejection to the prior DEF-011 input-required resume/cancel evidence; it does not close concurrency, timeout, provider/runtime-failure, multihop, cross-region, or other-role/platform cases.

## Evidence

- `../evidence/CP104-a2a-terminal-task-inventory.json` and `.png` — read-only inventory of six registered agents and ten persisted completed/canceled/failed tasks before this test; screenshot visually inspected.
- `../evidence/CP104-workspace-picker.png` and `CP104-a2a-project-env-picker.png` — organization/project/environment scope selection, visually inspected.
- `../evidence/CP104-a2a-terminal-key-plane-options.png` and `CP104-a2a-terminal-key-a2a-only-selected.png` — plane selector default and corrected A2A-only state, visually inspected.
- `../evidence/CP104-a2a-terminal-key-issue-disabled.png` — default No env state after full-page navigation; visually inspected, QA-OBS-009 recurrence.
- `../evidence/CP104-a2a-terminal-key-form.png`, `CP104-a2a-terminal-key-plane-options.png`, `CP104-a2a-terminal-key-a2a-only-selected.png`, `CP104-a2a-terminal-key-created.png`, and `CP104-a2a-terminal-key-revoked.png` — issuance, explicit least-privilege plane selection, inventory, revocation and replay denial; visually inspected. The one-time secret was never captured in the post-create screenshot.
- `../evidence/CP104-a2a-input-required-before-terminal.png` — same-ID task in `input-required` with permitted Resume/Cancel actions, visually inspected.
- `../evidence/CP104-a2a-completed.png` — Console-resumed task and completed state, visually inspected.
- `../evidence/CP104-a2a-terminal-results.json` — sanitized task creation/resume, terminal cancel rejection, persistence, fixture-call delta and key revocation/replay denial. It also retains the initial 415 request-harness result.
- `../evidence/CP104-a2a-terminal-resume-rejected.json` — corrected CSRF-authenticated 409 resume result and before/after task state.
- `../evidence/CP104-a2a-terminal-resume-rejected.png` and `CP104-a2a-terminal-guard-after-attempts.png` — real task screen after attempts; screenshots visually inspected. The prior successful-resume toast remained visible in-session and was not counted as the duplicate-attempt result.
- `../evidence/CP104-a2a-terminal-state-new-session.json` and `.png` — separate authenticated owner session confirms completed state/version 5 and no terminal controls; screenshot visually inspected.

## Consolidated status summary

- **Historical defects verified fixed:** DEF-001–004, DEF-006, DEF-008–015 remain scoped QA-002 results only; CP104 independently expands DEF-011. DEF-005 remains PARTIAL and DEF-007 failure atomicity/populated erasure remain UNVERIFIED. QA-001 PASS results are not inherited.
- **Historical defects failing or unverified:** DEF-016–036 remain open; DEF-023 was most recently reconfirmed in CP101. No defect disposition changed in CP104.
- **New defects:** none. Terminal A2A transitions were denied with state-specific conflicts; preliminary request-format/CSRF harness failures were correctly distinguished from product behavior. QA-OBS-009 recurred when a full-page navigation reset the selected environment.
- **Coverage completed:** A2A-only provisioning; fresh input-required task; real UI resume and settlement; terminal duplicate resume/cancel API rejection; UI action availability; state/version/cost persistence; no repeated fixture side effect; key revocation and replay denial; visual screenshots.
- **Coverage blocked / remaining:** in-flight cancellation/revocation races, concurrent resume submissions, task timeout, endpoint/runtime failure recovery, multi-hop/delegation, cross-region, other A2A roles, browsers/platforms, and the configuration-disabled runtime UX remain unverified or blocked.
- **Recommended engineering handoffs:** no new DEF-011 handoff from this phase. Preserve terminal-state guards and keep the one-time key/CSRF boundaries intact.
- **Release gate:** `GATE-REL-07` remains **BLOCKED**.
