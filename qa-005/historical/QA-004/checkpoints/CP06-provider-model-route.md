# CP06 — Synthetic provider, logical model, deployment, and route setup

**Outcome:** PARTIAL. The exact RC6 artifact accepted and persisted a valid loopback-only synthetic Ollama-compatible provider configuration through the real Console.

## Journey and verification

1. Inspected Providers inventory and opened create form in Chromium.
2. Created “QA004 synthetic Ollama” at `http://127.0.0.1:18125`, with `SELF_HOSTED_INFRA`, `ON_PREM`, explicit `127.0.0.1/32` private-IP allowance, no credential, and no external egress.
3. Probed it from the UI. Actual synthetic `/v1/models` and `/api/tags` routes returned the expected `qa004-mock`; the probe reported valid/HTTP 200 and state advanced to `ACTIVE`.
4. Registered `qa004-mock`, then created a healthy on-prem deployment with input/output form values 1000/2000 and a route `qa004-loopback` using `PRIORITY_FAILOVER` with one target.
5. Read back API state: provider, probe, logical model, deployment `ACTIVE_HEALTHY`, and route/target IDs all persisted.

## Visual evidence

Visually inspected `evidence/CP05-provider-inventory.png`, `evidence/CP05-provider-new-connection.png`, `evidence/CP06-provider-created.png`, `evidence/CP06-provider-after-probe.png`, `evidence/CP06-models-inventory.png`, `evidence/CP06-model-register-form.png`, `evidence/CP06-model-registered.png`, `evidence/CP06-deployment-form.png`, `evidence/CP06-deployment-created.png`, `evidence/CP06-routes-inventory.png`, `evidence/CP06-route-create-form.png`, `evidence/CP06-route-target-selected.png`, and `evidence/CP06-route-created.png`. JSON readbacks: `CP06-provider-probe.json`, `CP06-deployment-state.json`, `CP06-route-created.json`.

## Observations and limits

- The deployment form labels per-token nano-USD values without a displayed million-token denominator; exact settlement later confirmed a rate-unit contract mismatch, DEF-053.
- The route form warned its selected deployment did not advertise `tool_calling`; the form still allowed a single target to be saved under a failover strategy. Capability-specific routing and real failover remain UNVERIFIED.
- This is a local deterministic fixture, not a live external provider interoperability result. Authenticated provider secrets, multi-provider failover, timeout/retry, recovery and multimodal/tool workflows are open.
