# DEF-049 — Setup claims an initial route but creates none

- **Severity:** Low — first-install UX/business-contract clarity
- **Baseline:** `3423d4f523ef0ac77469a1201b8b753d1d31c902` (RC5)
- **Status:** Confirmed in one fresh SQLite `--role all` bootstrap path; adapter-enabled flow remains unverified.
- **Affected workflow:** fresh self-hosted install and first route setup.

## Reproduction

1. On a fresh instance with no provider adapters enabled, open Instance setup.
2. Continue through the initial-owner and organization/project flow.
3. The wizard headline reads: “Claim this self-hosted gateway and provision its initial owner and route.”
4. The provider step says no adapter is enabled and instructs the operator to finish setup, activate an adapter later, then add connections. Review states `Initial connection: Skipped`.
5. Complete setup. In the resulting `default/dev` workspace, `GET /admin/v1/routes?limit=100` returns 200 with `data: []`; Routes & Policies displays “No routes in this environment”.
6. Open Create route. No deployment target is available and the form says “Add at least one deployment target before saving”; Create route is disabled.

Screenshots and sanitized readback: [CP53 first-install wizard](../evidence/CP53-fresh-bootstrap-before.png), [provider step](../evidence/CP53-bootstrap-step2.png), [review](../evidence/CP53-bootstrap-review.png), [CP54 empty Routes](../evidence/CP54-fresh-bootstrap-routes.png), [route prerequisites](../evidence/CP54-create-route-prerequisites.png), [API/UI result](../evidence/CP54-fresh-bootstrap-routes.json). Full checkpoint: [CP54](../checkpoints/CP54-fresh-bootstrap-route-state.md).

## Expected and actual

- **Expected:** clearly distinguish tenant bootstrap from route readiness, and state that route creation requires adapter activation, a provider connection, model, and deployment; alternatively create a usable initial route when prerequisites are available.
- **Actual:** setup headline promises route provisioning, but the tested successful path creates no route. The skipped-connection message partly clarifies the prerequisite, but operators can still leave setup without a route while the headline describes one as part of provisioning.
- **Impact:** an operator may assume the gateway can route inference immediately after setup, then discover the missing prerequisites later. The empty state and disabled save prevent an invalid route; no security impact or traffic loss observed.

## Acceptance criteria

1. Wizard headline and Review accurately describe what bootstrap creates and whether inference routing is ready.
2. If no adapter/connection is configured, setup clearly explains the steps required before a route can be created.
3. If a route is an installation requirement, bootstrap must create a valid route or fail with an actionable prerequisite error; it must not report success while implying a missing route was created.
4. Verify both fresh install with no adapter and install with a configured adapter/connection, including persisted route readback.

## Cause and limits

Root cause is unconfirmed. The empty route is consistent with the absence of a provider connection and deployment; this finding concerns the mismatch in user-facing setup contract. No provider request was made. Other adapter-enabled setup paths remain unverified.
