# DEF-046 — Playground retains another workspace's error and transcript after scope change

**Severity:** Low  
**Status:** Confirmed on frozen RC5, reproduced twice in same-tab Chromium sessions  
**Affected:** Playground workspace switching between environment scope and a project with no environment  
**Baseline:** `3423d4f523ef0ac77469a1201b8b753d1d31c902`  
**Release gate:** `GATE-REL-07` remains **BLOCKED**.

## Reproduction

1. Sign in as the synthetic Org Owner and select `Default Organization / default / dev`.
2. In Playground, send a synthetic prompt with a nonexistent route slug. The data plane returns 503 `no_compliant_deployment_available`; Playground retains the user turn, assistant error, route slug, and an error banner.
3. Without navigating away, switch the workspace selector to `Default Organization / QA003 Empty Project DEF035 / No env`.
4. Observe that Playground displays the new scope and the correct “Select an active environment…” prerequisite, and disables Send. It nevertheless keeps the previous scope's 503 banner, route slug and prompt/assistant transcript. Returning to `default/dev` leaves that stale error/transcript visible as well.

The same-tab flow was repeated twice with synthetic prompt text and failed routes. Visually inspected [source scope](../evidence/CP36-playground-source-scope.png), [no-environment target](../evidence/CP36-playground-target-scope.png), and [returned source scope](../evidence/CP36-playground-returned-scope.png). Sanitized scope titles, visible text and disabled state are in [CP36 result](../evidence/CP36-playground-scope-result.json).

## Expected versus actual

- **Expected:** after the active workspace scope changes and its ephemeral Playground session is reset, the interface should clear old request errors and either clear the transcript or explicitly identify its originating project/environment and ask before reusing it.
- **Actual:** the target's no-environment prerequisite appears alongside an error from the previous `default/dev` scope. The transcript is not marked with its originating scope. Send is disabled in the no-environment context, so this test did not submit the old transcript there.

## Impact

This presents stale request status as if it belonged to the currently selected workspace and leaves prior conversation content visible under another scope. It can confuse diagnosis and may expose a prompt to a user sharing the same browser session/context. The test used synthetic text and did not establish a cross-user or server-side data exposure, nor did it send a follow-up request in the new scope.

## Facts and root-cause hypothesis

**Observed facts:** workspace selector title changed to the empty project/no-environment scope; its prerequisite banner and disabled Send action were correct; the previous 503 banner, route and prompt transcript remained; switching back did not clear them.

**Hypothesis (not proven as root cause):** the Playground reacts to `projectId` / `environmentId` changes by clearing the ephemeral session and trace IDs but leaves `messages` and `error` state intact. Frozen UI source inspection shows the partial reset; it is consistent with the behavior observed.

## Acceptance criteria

1. Changing organization, project, or environment must not display an old error as the current scope's result.
2. On scope change, clear prior transcript content or show an unmistakable origin label and require explicit confirmation before carrying it into a new scoped session.
3. Verify environment A→B, environment→no environment, project→project, organization→organization, rapid repeated changes, refresh, and logout/login.
4. Verify any subsequent request uses only the conversation history intentionally retained for its selected scope; do not silently forward prior-scope content.

## QA scope limits

This finding covers visible same-tab stale state only. It does not establish server-side cross-scope access, prompt persistence, another role's behavior, or a cross-scope follow-up request. Those scenarios remain open in QA-003.
