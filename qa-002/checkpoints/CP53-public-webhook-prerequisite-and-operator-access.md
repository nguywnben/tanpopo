# CP53 — Public webhook prerequisite and Operator access

**Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`  
**Result:** webhook delivery BLOCKED by missing usable Operator credentials; no product defect classified. QA-OBS-003 recurred.  
**Release gate:** `GATE-REL-07 BLOCKED`.

## Findings

The active `qa002-cp39` test login is an Org Owner. Its real Console navigation contains Overview, Traces, FinOps, provider/model/routing pages, Extensions, MCP Hub, Playground, Governance, Access & Keys and Security, but no Operations entry. The UI correctly restricts Operations to the Operator role. The isolated runtime has an Operator-bound principal, but the available credentials did not authenticate; a saved QA owner credential also failed for that principal. I attempted one existing test identity against an alternate local runtime through its HTTPS proxy; that login also failed. No password reset, identity mutation or database role edit was performed.

To remove the separate public-receiver prerequisite, I created a synthetic `webhook.site` receiver token (HTTP 201) with a harmless fixed response, but did not send any event or payload. Since no usable Operator session was available to register a webhook, I deleted the unused receiver (HTTP 204). No Tanpopo webhook subscription or signing secret was created; no external event was sent.

The Org Owner's real Overview showed 4 requests in 24 hours, a 25% error rate, zero active alerts, and “All systems nominal.” This repeats QA-OBS-003. The actual state and visually inspected screen are preserved.

## Evidence

- `../evidence/CP53-org-owner-navigation.png` — visually inspected Org Owner session, navigation without Operations, and Overview showing 25% errors / zero alerts / “All systems nominal.”
- `../evidence/CP53-webhook-prerequisite.json` — sanitized role, prerequisite and cleanup results; no receiver UUID, secret, or raw logs.

## Coverage disposition

- **BLOCKED:** register a public HTTPS webhook, trigger a synthetic event, verify HMAC signature, inspect receiver payload and exercise retry/remote failure recovery. Exact prerequisite is a valid local Operator credential for the isolated 18086 runtime (or an equivalent Operator test account). The Org Owner restriction is expected and not a product defect.
- **PASS:** the disposable public receiver was created and fully deleted without receiving event data; no product/database mutation occurred.
- **Observation recurrence:** QA-OBS-003. No new defect was added.
- **Other QA areas remain executable:** CP54 continues with real route-explain interactions.

QA-002 remains in progress. No implementation was changed.
