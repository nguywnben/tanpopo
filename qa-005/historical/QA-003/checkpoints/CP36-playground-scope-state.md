# CP36 — Playground state after switching workspace scope

**Baseline:** frozen RC5 `3423d4f523ef0ac77469a1201b8b753d1d31c902`  
**Runtime:** isolated SQLite at `127.0.0.1:18116`; real Chromium 156; synthetic Org Owner  
**Disposition:** same-tab scope transition exposed DEF-046 **CONFIRMED, Low**. FT-09, SCR-15 and WF-06 remain **PARTIAL**; scope and send-gating behavior was exercised only for this path. No request was sent while the no-environment scope was selected. `GATE-REL-07` remains **BLOCKED**.

## Real-user journey and evidence

1. In `Default Organization / default / dev`, opened Playground, entered a synthetic route and prompt, then sent. The request produced an expected 503 because no compliant deployment exists; the page showed the error, retained the conversation and minted its short-lived budget-capped session. Visually inspected [source scope screenshot](../evidence/CP36-playground-source-scope.png).
2. Switched the workspace selector to `Default Organization / QA003 Empty Project DEF035 / No env`. The new scope title and prerequisite banner were correct, and Send was disabled. The old model error, route and prompt remained visible. Visually inspected [target scope screenshot](../evidence/CP36-playground-target-scope.png).
3. Returned to `default/dev`. The same previous error and transcript were still visible. Visually inspected [returned scope screenshot](../evidence/CP36-playground-returned-scope.png).
4. Repeated this same-tab flow in a second fresh browser login, with the same stale state observed. The sanitized displayed state is in [CP36 result](../evidence/CP36-playground-scope-result.json).

## Finding and limits

- DEF-046 is a confirmed stale-UI/context-labeling issue. The reproducible steps, expected/actual behavior, impact, hypothesis and acceptance criteria are in the [DEF-046 report](../reports/DEF-046-playground-state-survives-scope-switch.md).
- The ephemeral session was reset when scope changed; old transcript/error state was not. Because the target had no environment, Send was disabled and the old prompt was not submitted under another scope. No cross-scope data-plane disclosure or backend authorization defect was demonstrated.
- No implementation changed. QA-003 remains incomplete and `GATE-REL-07` remains **BLOCKED**.
