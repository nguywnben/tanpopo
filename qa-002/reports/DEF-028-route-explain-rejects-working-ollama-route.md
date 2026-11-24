# DEF-028 — Route Explain rejects a working Ollama route as dialect-mismatched

**Severity:** Moderate (P2)  
**Status:** Confirmed on frozen QA-002 baseline `40bba5b55c2da6fc00b107b315c9b3167fb20169`.  
**Release gate:** `GATE-REL-07` remains **BLOCKED**.

## Summary

Routes & Policies → Route Explain reports zero eligible deployments and rejects both healthy targets as `DIALECT_MISMATCH` for the same Ollama priority-failover route that returned a real successful Playground stream. The Console sends `dialect: "openai_compatible"` for every explain request and provides no dialect selector. An authenticated dry-run API control with `dialect: "ollama"` selected both targets. The simulator therefore gives operators a false no-route result for a route that the live normalized chat workflow successfully uses.

## Reproduction

1. Create a healthy self-hosted Ollama connection and deployment in the test environment, then create a route. CP52's route `qa002-cp52-failover-route` has two healthy Ollama deployments.
2. Submit a real Playground streaming request through that route. CP52 recorded HTTP 200 after a controlled first-target 503 and second-target success. CP54 repeated the route in the healthy primary state: HTTP 200 and a persisted trace selected the first deployment. The latest Playground key was revoked after the control.
3. Open Routes & Policies → Route Explain. Enter `qa002-cp52-failover-route`, choose `On-prem`, leave capability requirements empty, and click Explain route.
4. The page reports “Dry-run verified”, zero eligible candidates and two rejected deployments. Detailed output gives `DIALECT_MISMATCH` for both, with detail `Deployment uses native dialect "" instead of "openai_compatible".` Screenshot `../evidence/CP54-route-explain-valid.png` was visually inspected.
5. Authenticated dry-run API controls returned zero candidates for `openai_compatible` and `openai_chat`; `dialect: "ollama"` selected the two distinct deployment IDs with no rejection. All controls were dry-run only.
6. Checking `tools` in the UI did not alter the dialect rejections; the simulator continues to show zero candidates before a user can learn about other capability constraints.

## Expected versus actual

- **Expected:** explain a standard supported chat route consistently with actual gateway behavior, or let the operator select/understand the requested dialect. If a native-only dialect constraint is being simulated, label it and require an explicit dialect rather than silently applying one that contradicts successful normal inference.
- **Actual:** the UI silently submits `openai_compatible`; the simulator rejects every deployment on this Ollama route as dialect-mismatched, despite the route successfully serving `/v1/chat/completions` through Playground. The UI offers no dialect input or explanation of the distinction.

## Impact

Route authors may conclude a healthy route is unusable, change a valid route or deployment unnecessarily, or fail to diagnose the actual policy/capability issue. This undermines dry-run decisions used to validate business routing and deployment changes. No inference or spend is created by Route Explain itself.

## Facts and root-cause hypothesis

**Facts:** the real route returned HTTP 200 and a persisted trace selected its first deployment; Route Explain returned HTTP 200 with `dry_run=true`, zero eligible candidates and two `DIALECT_MISMATCH` rejections; an authenticated dry-run with `dialect: "ollama"` selected both deployment IDs. The standard Playground request and the simulator were performed against the same route on the same source baseline. The route selector/Playground fixtures made no external provider calls.

**Source observation / hypothesis:** Console `RoutingPage.tsx` constructs `dialect: 'openai_compatible'` in `runExplain()` and exposes no dialect control. The backend applies that dialect to deployment matching. This likely causes the mismatch between the normal adapter-backed chat request and the dry-run. This is a root-cause hypothesis; runtime discrepancy is the confirmed finding.

## Acceptance criteria

- Use the same normalized request semantics as the live data-plane path when explaining a standard chat route.
- Where dialect affects eligibility, present an explicit, understandable dialect choice or derive it from the selected route/request type; do not silently use an incompatible default.
- For any candidate rejection, explain whether it applies to native passthrough or standard adapter-backed inference.
- Verify the same healthy Ollama route: Route Explain selects the expected ordered deployments for standard chat; a deliberately requested unsupported native dialect still fails closed with a clear reason; a live request and dry-run remain consistent without upstream egress or budget holds from the dry-run.

## Evidence

- `../evidence/CP54-route-explain-ollama-mismatch.json` — sanitized UI/API responses and live trace identity.
- `../evidence/CP54-route-explain-valid.png` and `CP54-route-explain-tools.png` — visually inspected false no-candidate state.
- `../evidence/CP54-playground-route-success.png` — visually inspected successful inference; persistent trace selects the primary deployment.
- `../evidence/CP52-playground-failover-result.png` — prior same-route streamed failover positive control.
- `../evidence/CP54-key-revoked.png` — temporary test key cleanup.

QA-002 remains in progress. No product implementation was changed and no release approval is implied.
