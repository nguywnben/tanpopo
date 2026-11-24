# DEF-027 — Route target choices hide deployment identity

**Severity:** Moderate (P2)  
**Status:** Confirmed on frozen QA-002 baseline `40bba5b55c2da6fc00b107b315c9b3167fb20169`.  
**Release gate:** `GATE-REL-07` remains **BLOCKED**.

## Summary

In a project/environment containing multiple healthy deployments of the same upstream model in the same region, the Routes & Policies target selector renders each choice with the same label, for example `qa002:latest (ON_PREM · Healthy)`. It omits the provider connection and deployment identity. Route operators therefore cannot tell which provider they are adding or order first when creating a priority-failover route. The route and persisted trace still use distinct deployment IDs, so this is a Console identification/configuration defect, not a backend routing collision.

## Reproduction

1. In an isolated project/environment, use real Console UI to create a second self-hosted Ollama connection with endpoint `http://127.0.0.1:19181`, CIDR `127.0.0.1/32`, and probe it successfully. A separate existing connection points to `http://127.0.0.1:19180`.
2. Register both deployments for the same logical model and upstream model name `qa002:latest`; both show region `ON_PREM` and health `Healthy`. A third matching healthy deployment was present in the environment.
3. Open Routes & Policies → Create route → Add deployment target. The selector presents three choices, each visibly named `qa002:latest (ON_PREM · Healthy)`; none shows provider connection, endpoint, deployment ID, or another distinguishing field. The screenshot `../evidence/CP52-route-deployment-options.png` was visually inspected, and the live DOM exposed three identical accessible option names.
4. Add two targets and create a priority-failover route. The target rows and configured-route fallback chain likewise show only `qa002:latest` and health, not connection identity. API/persistent route data contains different ordered deployment IDs and the associated connection records map them to different provider connections.
5. In a controlled demonstration, the first endpoint returned synthetic HTTP 503 and the second endpoint returned a synthetic streamed response. The configured route tried both in order and completed successfully; trace waterfall confirms the two distinct deployment IDs. This confirms that the duplicate labels conceal materially different targets.

## Expected versus actual

- **Expected:** identify every selectable deployment with sufficient non-secret context, such as connection name, logical/upstream model, region, health, and a stable short deployment ID. Carry the same distinction into ordered targets, fallback-chain summaries, and route inspection.
- **Actual:** multiple providers with the same model/region/health have identical visible and accessible names. Route summaries also omit the connection identity.

## Impact

An operator can put an unintended provider first or include an unintended fallback without recognizing the choice. That can alter cost, data residency/provider trust, availability behavior, and incident routing. The effect is especially acute where failover order is a business or compliance decision.

## Facts and hypothesis

**Facts:** CP52 had three distinct `ACTIVE_HEALTHY` deployment IDs for the same `qa002:latest`/`ON_PREM` context; the route API persisted distinct ordered IDs associated with two different provider connections; all three UI options had exactly the same text and no visible provider/deployment identity. A real controlled request attempted the first endpoint (503), then selected the second deployment and returned 200.

**Hypothesis:** the selector label formatter uses only upstream model name, region, and health, omitting the connection/deployment identity. The independent UI evidence is sufficient to confirm the defect; no root cause is asserted beyond the visible behavior.

## Acceptance criteria

- Disambiguate deployment candidates in visual text and accessible names with provider connection name and stable deployment identity, plus model/region/health as appropriate.
- Show the same provider identity in ordered route targets, fallback summaries, route inspection/explain, and edit forms.
- Keep exact connection secrets and sensitive endpoint credentials hidden.
- Verify with at least two identical upstream model names on different connections: select a known primary and fallback, save/reload, then confirm the persisted order and synthetic failover waterfall match the selected identities.

## Evidence

- `../evidence/CP52-multi-target-failover-and-route-selector.json` — sanitized UI observation, ordered deployment IDs, endpoint mapping, trace waterfall, and cleanup.
- `../evidence/CP52-route-deployment-options.png` — three indistinguishable choices, visually inspected.
- `../evidence/CP52-route-two-targets-selected.png` and `CP52-route-failover-created.png` — target order and configured route, visually inspected.
- `../evidence/CP52-playground-failover-result.png` — synthetic primary 503 followed by backup success, visually inspected; DEF-017 pending trace metadata recurred.

QA-002 remains in progress. No product implementation was changed, and no release approval is implied.
