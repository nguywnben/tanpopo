# CP31 — Project policy write persistence and stale ETag conflict

**Baseline:** frozen RC5 source `3423d4f523ef0ac77469a1201b8b753d1d31c902`  
**Runtime:** isolated SQLite at `127.0.0.1:18116`; real Chromium 156; synthetic Org Owner; `QA003 Empty Project DEF035 / No env`  
**Disposition:** DEF-039's RC4 project-only prerequisite is not reproduced; the Owner edit and concurrency path succeeded. Overall DEF-039 remains **PARTIAL** because Team Admin and other acceptance criteria remain untested. No product implementation was modified. `GATE-REL-07` remains **BLOCKED**.

## Project-only policy edit and persistence

1. Selected the synthetic empty project with **No env** and opened Routes & Policies → Policies. The page showed the effective project policy and Project editor. The original values were EU, US, APAC, and ON_PREM. Visually inspected [before-change screenshot](../evidence/CP31-policy-before-change.png).
2. Unchecked APAC and saved in the Console. The UI showed “Saved Project policy.” The authenticated PUT returned HTTP 200. Effective-policy readback and the page after navigation showed EU, US, ON_PREM. The initial save materialized the new project policy at version/ETag v1; evidence: [write result](../evidence/CP31-def039-policy-write.json), [after-save screenshot](../evidence/CP31-policy-after-save.png), [after-navigation screenshot](../evidence/CP31-policy-after-navigation.png).
3. Opened two independent authenticated Chromium sessions at the same project-only scope. Both read the policy at ETag v2 (the second edit removed US and persisted EU, ON_PREM). Session B removed ON_PREM and saved successfully: request `If-Match: "v2"`, response 200/ETag `"v3"`, persisted regions EU.
4. Session A, still holding v2, changed allowed models to `qa003-stale-draft-model` and saved. Its stale `If-Match: "v2"` returned HTTP 409 with current ETag v3. Final readback remained EU, version 3, and the stale model draft was not persisted. The Console presented the two-version conflict modal, preserved the unsaved model draft, and showed the latest server version. Visually inspected [conflict screenshot](../evidence/CP31-policy-stale-write-result.png); successful v2→v3 intermediate state is shown in the [second-write result](../evidence/CP31-policy-second-write-result.png); sanitized API/session summaries are [concurrency evidence](../evidence/CP31-policy-concurrency.json) and [second-write result](../evidence/CP31-policy-second-write-result.json).

## Reconciliation and observations

This RC5 owner journey disproves the historical UI gate in the tested project/no-environment context and demonstrates persisted Project editing plus optimistic concurrency. It does not close DEF-039: the original acceptance also requires Team Admin writes, scope-specific environment behavior, monotonic parent-relaxation rejection, fresh-session consistency, and complete policy audit attribution. The stale-conflict screen correctly preserves the draft and exposes a server snapshot. A red banner behind the modal says `resource version conflict: current revision is v3`; the modal gives a clearer explanation. This redundant technical banner is recorded as a UX observation, not a separate defect in this checkpoint.

No new defect is filed. The policy became more restrictive only in a synthetic project with no environment or workload. The unsaved stale draft was not adopted. No routes or deployment state were modified.

## Coverage update

- DEF-039 remains **PARTIAL**, with Owner project-only write/persistence and one real two-session `If-Match` conflict path covered.
- SCR-09 remains PARTIAL. Team Admin, org-only controls, environment policy, parent-relaxation rejection, fresh login, and other browser/platform coverage remain open.
- The broader historical disposition count remains 17 VERIFIED FIXED for scoped RC5 scenarios, 8 PARTIAL, and 14 UNVERIFIED.
- QA-003 remains incomplete; `GATE-REL-07` remains **BLOCKED**.
