# CP25 — Playground transcript across workspace scope change

**Historical finding:** DEF-046, Low. **QA-004 outcome:** PASS for one project-scoped Developer changing from its only assigned project to **No project** in Chromium. Switching to a different populated project, other roles, in-flight request races, browser engines, and persisted error/recovery states remain UNVERIFIED. GATE-REL-07 remains BLOCKED.

## Reproduction

The synthetic Developer had only the `default` project and its `dev` environment; permitted project/model/deployment/route APIs showed one project, one model, two deployments, and the synthetic `qa004-loopback` route. In Playground, the Developer submitted a real prompt with a 1,000-micro-USD cap. The local mock returned `Hello from synthetic Ollama.` and an execution trace showed HTTP 200, 0 micro-USD, and 70 ms.

The Developer then selected **No project** in the real scope picker. The selected workspace changed to `No project / No env`, the prior prompt and response disappeared, the old trace panel disappeared, the model/route selection reset, and the page displayed “Select an active environment in the top bar to start a Playground session.” Hard refresh returned the user's sole assigned project (`default / dev`) but did not restore the old transcript or trace. The exact state snapshots and assertions are in `evidence/CP25-playground-scope-switch.json`; screenshots `CP25-playground-before-scope-switch.png`, `CP25-playground-after-no-project.png`, and `CP25-playground-after-scope-refresh.png` were visually inspected.

## Disposition

DEF-046 did not reproduce in this scoped flow. Workspace change removed the previous context's transcript and trace, and refresh did not resurrect them. This does not test switching between two populated authorized projects because this identity had only one assigned project. No product implementation was changed; the request used a local synthetic endpoint and displayed zero charge.
