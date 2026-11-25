# CP81 — DEF-039 project policy without an environment

**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`  
**Role/browser:** Org Owner, real Chromium, 1440×1000  
**Scope:** `QA004 Test Root Org / QA-004 Isolated Canary / No env`

## Result

**DEF-039 PASS (scoped).** The historical symptom was that project policy management required an environment. At project scope with `No env`, the Policies tab loaded a Project editor, allowed an actual policy change, persisted it, and allowed it to be restored. The effective policy endpoint returned the project policy at both steps. The screenshots were visually inspected. This does not cover other roles, organization policy, environment policy, policy inheritance conflicts, guardrail lifecycle, or other browsers.

## Reproduction and evidence

1. Sign in as the isolated synthetic Org Owner on the exact RC6 runtime.
2. Select the synthetic org and `QA-004 Isolated Canary`, then choose `No env`.
3. Open Routes & Policies → Policies. The effective policy table and editable Project scope form are both present without an environment selection.
4. Change Allowed model slugs from `*` to `qa004-cp81-temporary`, save, and read the project effective-policy API. The PUT returned HTTP 200, ETag `"v5"`, and version 5; the subsequent GET returned the synthetic slug at version 5.
5. Restore Allowed model slugs to `*`, save, and read the API again. The PUT and GET returned HTTP 200, ETag `"v6"`, version 6, and `allowed_models: ["*"]`.

The clean, paired HTTP/UI state evidence is [CP81-def039-policy-clean-roundtrip.json](../evidence/CP81-def039-policy-clean-roundtrip.json). The initial and edited/restored screenshots are [project/no-environment](../evidence/CP81-policy-page-project-no-env.png), [edited state](../evidence/CP81-policy-noenv-edited.png), and [restored state](../evidence/CP81-policy-noenv-restored.png). Both edited and restored screenshots were visually inspected. The initial screenshot shows the Project editor and no active-environment prerequisite.

An earlier exploratory write produced an immediate effective read with the changed `allowed_models` but an unchanged version/ETag. The script did not restore because it incorrectly conditioned cleanup on a version increment. This was corrected through the UI before further testing; API readback confirmed `*`. A subsequent clean round trip advanced v4→v5→v6 and remained consistent in response and readback. The initial mismatch was not reproduced and remains an unresolved timing/evidence anomaly, not a confirmed defect. Its raw contemporaneous output is retained in [CP81-def039-policy-edit-lifecycle.json](../evidence/CP81-def039-policy-edit-lifecycle.json) and [CP81-def039-roundtrip.json](../evidence/CP81-def039-roundtrip.json); the latter's response listener captured the final restore response, as noted here, and should not be interpreted as the edit response.

## Expected and actual

- **Expected:** an Org Owner can inspect and edit the project-level policy at project scope, without choosing an environment; a saved policy persists and subsequent writes use the current version.
- **Actual:** this path worked. A harmless synthetic allowlist was saved and then restored. Effective GET and PUT body/ETag remained aligned for the clean retest.

## Coverage gaps

Other roles and policy scopes; inherited restrictions and attempts to relax them; multiple policy types and guardrail attach/detach; conflict/retry behavior; refresh and scope-switch races; keyboard/screen-reader paths; Firefox/WebKit; and PostgreSQL-backed runtime policy editing remain unverified. No product defect is added from the one unreproduced readback anomaly.

**Campaign status:** 23 historical findings have scoped PASS, 6 are PARTIAL, DEF-025 remains FAIL, and 20 are UNVERIFIED. DEF-051–068 remain 18 confirmed new failures. QA continues; **GATE-REL-07 remains BLOCKED.**
