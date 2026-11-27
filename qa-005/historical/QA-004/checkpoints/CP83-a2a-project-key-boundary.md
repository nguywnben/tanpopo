# CP83 — A2A project-scoped key boundary

**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`  
**Runtime:** A2A-enabled isolated RC6 clone at `127.0.0.1:18128`  
**Role:** synthetic Org Owner for setup/readback; A2A-only project Virtual Key for the data-plane requests  
**Result:** PASS (scoped); no new defect

## Reproduction

1. In the Owner Console, select `QA004 Test Root Org / default / dev`.
2. Read the synthetic key metadata. Key `vky_01m898dwx3e00byxdkfywbmvbv` is `CREATED_ACTIVE`, belongs to project `prj_01m895k8pee008b440xk2ef12p`, has only the A2A plane, and allows `/a2a/*`.
3. Use the key with cookies omitted to call `GET /a2a/agents`. It returns HTTP 200 and only the agent `qa004-a2a-loopback` in that project.
4. Using the same key, submit a zero-hold synthetic task to `qa004-cp65-cancel-agent`, whose registered project is `prj_01m89cf7vce008saenf6dyw8gk` (the sibling canary project). The API returns HTTP 404 `resource_not_found`, “Target A2A agent or parent task not found.”
5. Read the task inventory and monitor the loopback endpoint: no task is created and the sibling fixture receives zero calls.

The sanitized results are [CP83-a2a-project-boundary-results.json](../evidence/CP83-a2a-project-boundary-results.json). The actual Console was visually inspected after the request; [the screenshot](../evidence/CP83-a2a-default-scope-after-cross-project-probe.png) shows the default/dev context, both registered agents, and no task created by the cross-project attempt.

## Expected versus actual

- **Expected:** an A2A Virtual Key scoped to a project can discover and dispatch only agents within its authorized project. A cross-project identifier should not disclose or dispatch a sibling agent.
- **Actual:** discovery contains only the key's own project agent; the sibling task attempt returns a uniform not-found response, creates no task, and causes no downstream call.

## Scope and gaps

This verifies one Org Owner-created key and one same-organization sibling-project denial on SQLite/Chromium. Foreign-organization targets, other key scopes/roles, task read/resume/cancel object-ownership matrix, enumeration/pagination, PostgreSQL/Valkey multi-node behavior, and SSRF/DNS rebinding remain unverified. The local sibling fixture used for this probe was stopped afterward; the original CP10 synthetic A2A fixture remains restored on port 18127.

**Campaign status:** 23 historical findings have scoped PASS, 6 are PARTIAL, DEF-025 remains FAIL, and 20 are UNVERIFIED. DEF-051–069 remain 19 confirmed new failures. QA continues; **GATE-REL-07 remains BLOCKED.**
