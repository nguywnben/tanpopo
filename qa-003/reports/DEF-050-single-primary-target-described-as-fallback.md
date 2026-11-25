# DEF-050 — Single primary route target is described as a fallback

**Severity:** Low
**Status:** Confirmed UI/business-language defect on RC5; no routing or authorization bypass observed.
**Baseline:** `3423d4f523ef0ac77469a1201b8b753d1d31c902`
**Evidence:** [CP56-route-target-selected.png](../evidence/CP56-route-target-selected.png), [sanitized route result](../evidence/CP56-route-result.json), [CP56 checkpoint](../checkpoints/CP56-loopback-provider-recovery-to-inference.md).

## Reproduction

1. Activate the built-in Ollama adapter and create an explicitly authorized synthetic loopback connection.
2. Probe successfully, register a model with `streaming` capability only, and create a healthy deployment.
3. Open Routes & Policies → Create route, choose Priority failover, and add that deployment as the **only** route target. Do not add a second target.
4. Observe the inline warning: “At least one fallback target does not declare tool_calling capability.”
5. Save the route and send an authorized tool-call request through it. The gateway correctly returns `400 capability_not_supported` before dispatch; the loopback mock request count does not increase.

## Expected and actual behavior

- **Expected:** The editor should distinguish a primary target from a fallback. With one target, it should either identify the selected target's missing capability as a route-wide limitation, or suppress fallback-specific language. If a warning is intended for a potential future fallback configuration, the UI should say so conditionally.
- **Actual:** A route with one primary target displays a warning that a fallback target lacks the capability, although the route has no fallback target. The actual data-plane guard remains effective and blocks the unsupported tool request.

## Impact and scope

The warning can lead an operator to search for a missing fallback or misunderstand which candidate causes the limitation. It weakens confidence in route validation and obscures the true contract: the only target cannot satisfy tool calls. No incorrect target selection, provider dispatch, security bypass, or charge occurred in this test.

## Acceptance criteria

1. With exactly one target lacking a requested capability, route validation names the target or says the route has no eligible target; it does not call that target a fallback.
2. With two or more targets, fallback-specific warnings identify the affected fallback target(s), and primary/fallback semantics match the selected strategy and ordering.
3. The Playground and API continue to reject unsupported capability requests before any upstream call, with a stable actionable error.
4. UI and API tests cover single-target and multi-target cases for each capability warning. This handoff requests no weakening of capability authorization.
