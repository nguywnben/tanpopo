# CP33 — Policy values across project scope switches

**Baseline:** frozen RC5 source `3423d4f523ef0ac77469a1201b8b753d1d31c902`  
**Runtime:** isolated SQLite at `127.0.0.1:18116`; Chromium; synthetic Org Owner  
**Disposition:** scope-switch scenario **PASS**; full Policies coverage and DEF-039 remain **PARTIAL**. No new defect confirmed. `GATE-REL-07` remains **BLOCKED**.

## Reproduction and evidence

1. Logged in as the synthetic organization owner and opened Routes & Policies → Policies in `QA003 Empty Project DEF035 / No env`. The effective allowed regions were `EU`, with provenance “Project floor”; the project edit form had EU selected and the other region checkboxes clear. Visually inspected [synthetic project screen](../evidence/CP33-policy-empty-project.png).
2. Switched the workspace selector to `Default Organization / default / dev`. The effective regions were `EU, US, APAC, ON_PREM`, and the editor showed all four selected. Provenance showed the organization floor and environment policy for the other displayed constraints. Visually inspected [default/dev screen](../evidence/CP33-policy-default-project.png).
3. Returned to `QA003 Empty Project DEF035 / No env`. The effective regions and project form returned to the EU-only values. Visually inspected [returned project screen](../evidence/CP33-policy-returned-project.png).
4. At each step, an authenticated no-store `GET /admin/v1/policies/effective` for the selected project returned HTTP 200. The synthetic project read retained ETag `"v3"`, `version: 3`, and `regions: ["EU"]`; `default` retained ETag `"v1"`, `version: 1`, and all four regions. Returning to the synthetic project produced the same v3/EU result. Sanitized response and rendered text are in [CP33 result](../evidence/CP33-policy-isolation-result.json).

## Result and limits

- No stale policy value or editor checkbox crossed between these two project contexts during sequential navigation, including returning to the first context. No writes were made in this phase.
- This is scoped positive evidence only. It does not test delayed or out-of-order policy responses, organization-only selector behavior, another role, environment policy editing, a fresh browser session, or persistence after reload. DEF-039 remains **PARTIAL**; SCR-09 remains **PARTIAL**.
- FT-10, SCR-08, and all other coverage labels are unchanged. QA-003 remains in progress and `GATE-REL-07` remains **BLOCKED**.
