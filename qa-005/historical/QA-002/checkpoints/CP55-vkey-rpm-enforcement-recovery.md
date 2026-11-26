# CP55 — Virtual Key RPM enforcement and recovery

**Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`  
**Result:** PASS for one isolated environment-scoped provider key; no new defect classified.  
**Release gate:** `GATE-REL-07 BLOCKED`.

## Test and outcome

The Access & Keys issue form was visually inspected in Chromium. It exposes key name, credential plane, route patterns, model access, and MCP tool access; no RPM/TPM/concurrency controls were present. The documented screen contract does not specify these key-level controls, so this is retained as a provisioning-contract coverage gap rather than a defect. To exercise the Admin API's supported per-key RPM field, a temporary PROVIDER key was created for `qa002-cp43-env2` with `rpm_limit=1`, then used from a clean browser context carrying only its bearer credential against the isolated synthetic provider route.

Two immediate real Data Plane calls produced HTTP 200 followed by HTTP 429 with `Retry-After: 60` and `rate_limit_exceeded`. Persistent trace metadata for the rejected request had no selected deployment or connection, and the fixture call count stayed unchanged at 3, demonstrating that the rejected request did not reach the provider. After more than the advertised 60-second interval from key creation, a third request returned HTTP 200 and fixture count advanced from 3 to 4. The temporary key was revoked through the Console and the UI screenshot showed it in `Revoked` state; persistent state also reports REVOKED.

An initial harness attempt mixed the authenticated Admin session cookie into a Data Plane call and received the expected 401 `invalid_credential_plane`; that attempt was excluded from RPM results and no provider dispatch occurred. The corrected test used an isolated clean browser context with only the Virtual Key bearer credential.

## Evidence

- [`../evidence/CP55-rpm-enforcement-recovery.json`](../evidence/CP55-rpm-enforcement-recovery.json) — request outcomes, request trace IDs, dispatch counts, and final key state.
- [`../evidence/CP55-key-issue-form.png`](../evidence/CP55-key-issue-form.png) — visually inspected issue form and exposed key-grant controls.
- [`../evidence/CP55-rpm-key-created.png`](../evidence/CP55-rpm-key-created.png) — key appeared in environment key list before request exercise.
- [`../evidence/CP55-rpm-recovery-key-revoked.png`](../evidence/CP55-rpm-recovery-key-revoked.png) — visually inspected final key revocation and historical temporary-key state.

## Status and remaining coverage

**PASS:** one environment-key RPM=1 positive call, immediate over-limit fail-fast 429 with `Retry-After`, no egress for the rejected request, recovery after the window, and key revocation.  
**UNVERIFIED:** project/organization policy limits, TPM, concurrency, simultaneous parent/key limit precedence, fixed-window boundary and distributed-node behavior, other roles, retry-after timing precision, provider-side 429 distinction, and UI provisioning of cap values. No product defect was added.

No Tanpopo implementation was changed. QA-002 remains in progress.
