# CP98 — DEF-050 route tool-call capability warning

**Source/runtime:** frozen unsigned RC7 source `737079ddb847d97dbcf832f610ed0048556cb65b`; runtime binary SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`; exact-source SQLite `127.0.0.1:18162`; Chromium 156; Owner `qa005-owner@example.invalid`; QA005 No Budget Project / QA005 Budgetless Dev.

## Result

**PARTIAL (fresh DEF-050 UI retest).** The real Create route drawer was opened without submitting. With one target (`CP58 OpenAI SSE loopback fixture / qa005-model · fsf2nr`) it showed “At least one target deployment does not declare tool_calling capability.” It no longer incorrectly calls the sole target a “fallback.” With a second priority-failover target added (`QA005 budgetless Ollama / qa005-playground-low:latest · 9trtct`), the same generic message remained and did not identify which target or fallback lacks the capability. Thus the original contradiction is corrected in the tested slice, but the current message falls short of the accepted target-specific/actionable wording.

## Reproduction

1. Sign in to the exact-source Owner Console and open Routes & Policies → Create route.
2. Enter a synthetic slug, leave Priority failover selected, choose only the CP58 OpenAI loopback deployment, and click Add target.
3. Inspect the warning and the single primary target. Do not click Create route.
4. Add the QA005 budgetless Ollama deployment as the second ordered target and inspect the warning again. Do not save.
5. Verify no route create request occurred and close the drawer without changes.

Visually inspected screenshots: [empty route form](../evidence/screenshots/CP98-def050-route-create-form.png), [single primary warning](../evidence/screenshots/CP98-def050-single-primary-warning.png), and [two-target warning](../evidence/screenshots/CP98-def050-two-target-warning.png). Sanitized evidence: [CP98 JSON](../evidence/CP98-def050-route-capability-warning.json).

## Limits and reconciliation

No route was created or changed, and no provider call occurred. This tests copy/state only. It does not independently inspect each model's capability record, send an unsupported tool-call Playground request, prove rejection before egress, or test other strategies/capabilities, roles, backends or browser engines. Historical QA-003 evidence that server-side capability enforcement rejected before dispatch remains preserved but is not counted as QA-005 proof. Keep DEF-050 PARTIAL pending actionable target identification and fresh no-egress enforcement coverage. `GATE-REL-07` remains BLOCKED.
