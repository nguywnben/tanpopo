# CP62 — FinOps project-scope switching, saved budget, and refresh behavior

**Baseline:** frozen RC6 source `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`  
**Runtime:** isolated exact-source offline instance on `127.0.0.1:18132`  
**Actor:** synthetic Org Owner  
**Product changes:** none

## Result

- **PASS (scoped):** In real Chromium, the populated CP55 project (`QA004 CP55 Ops Billing`) displayed its persisted monthly project budget. The empty CP57 project (`QA004 CP57 Project Recovery`) showed zero budget ceilings and “No budgets configured.” Switching between the two did not leak the CP55 row into CP57; returning to CP55 restored it.
- **PASS (scoped):** Used the actual row editor to save a 100 micro-USD hard limit. The `PUT /admin/v1/budgets` returned HTTP 200. A subsequent authenticated list read returned the same budget id and project scope with `hard_limit_micros=100`, warning thresholds `[50, 80, 95]`, and version 5. No inference or provider spend occurred.
- **UNVERIFIED:** A hard refresh while CP55 was selected initialized the Console at the account’s observed CP57 default project/environment, whose FinOps view was empty. The CP55 budget remained persisted and reappeared when CP55 was selected again. No frozen-baseline product contract was found requiring the last active scope to survive refresh, so this behavior is recorded as an observation, not a defect.

## Reproduction

1. Sign into the isolated RC6 Console as the synthetic Org Owner and open **FinOps**.
2. Select `QA004 CP55 Ops Billing`; observe its single monthly project budget row.
3. Save the hard limit as `100` micro-USD and confirm the green save message.
4. Select `QA004 CP57 Project Recovery`; observe `Budget ceilings 0` and “No budgets configured.”
5. Return to CP55; observe the budget row again.
6. Hard refresh while CP55 is selected. The shell initializes at CP57, and its empty state is shown.
7. Verify authenticated `GET /admin/v1/budgets?limit=100`: the CP55 record remains with limit 100 and version 5.

## Visual and API evidence

- [CP55 saved budget](../evidence/CP62-finops-cp55-saved.png)
- [CP57 empty state](../evidence/CP62-finops-cp57-empty.png)
- [Post-refresh default scope](../evidence/CP62-finops-after-refresh-default-scope.png)
- [Sanitized interaction and persisted-state evidence](../evidence/CP62-finops-project-scope.json)

The screenshots were opened and visually inspected during this checkpoint. The scope selector overlays part of the page in the first two captures, but the selected project, budget count/empty state, confirmation and limit value remain visible.

## Coverage limits

Only one budget in one project and an empty second project were compared. Organization, environment, and Virtual Key budget tiers; warning behavior; cap enforcement; chargeback and exports; other roles; additional browsers; concurrent scope changes; and whether the product intends to remember the last scope on refresh remain unverified.

No new defect is assigned. QA-004 remains incomplete. `GATE-REL-07` remains **BLOCKED**.
