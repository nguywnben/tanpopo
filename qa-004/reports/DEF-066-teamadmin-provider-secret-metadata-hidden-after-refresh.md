# DEF-066 — Team Admin cannot see project-scoped secret metadata after refresh

**Severity:** Moderate  
**Status:** FAIL (confirmed current RC6)  
**Area:** Providers, write-only secret metadata, project-scoped Team Admin Console

## Summary

A project-scoped Team Admin can seal a project-owned credential and see its metadata immediately. After hard refresh, the Providers screen displays a global `insufficient role permission` banner and reports zero vault credentials. In the same authenticated session, the project-filtered secret metadata API returns the persisted row (HTTP 200), while the unfiltered organization-wide collection is correctly denied (HTTP 403). The Console omits the authorized project filter and treats that expected denial as failure for the entire Providers view.

No real credential or upstream provider was used. The canary secret value was synthetic, stored only in the isolated test database, and excluded from all QA artifacts.

## Reproduction

1. Start from the fresh isolated RC6 runtime at `127.0.0.1:18132`; create an Org Owner, a project, and a principal assigned only `TEAM_ADMIN` on that project. Activate the invitation through the real **Activate invite** form. The principal’s authenticated session reports exactly `TEAM_ADMIN`.
2. In Chromium, select the assigned project/environment and open **Providers → Seal secret**. Enter a synthetic canary name and a fake non-provider value; submit. `POST /admin/v1/secrets` returns HTTP 201 with project ownership and safe metadata. The Console shows the success message and one metadata row; the screenshot contains only name, type, fingerprint, and credential ID.
3. Hard refresh. Reopen **Providers → Vault Secrets**. The screen shows `insufficient role permission`, a count of zero, and `No vault credentials sealed`.
4. In the same Team Admin session, request `GET /admin/v1/secrets?project_id=<assigned-project>&limit=100`. It returns HTTP 200 and the persisted canary metadata. No `secret_value` field is returned.
5. Request `GET /admin/v1/secrets?limit=100` without the project filter. It returns HTTP 403 `insufficient_role_permission`. This is the intended boundary; the product defect is the Console’s missing authorized filter and the resulting stale/empty screen.

The immediate post-create UI state, refreshed failure state, and authorized API readback were captured in real Chromium and visually inspected. See [CP52 evidence](../checkpoints/CP52-teamadmin-provider-metadata-and-cross-screen-sweep.md) and the [sanitized result JSON](../evidence/CP52-teamadmin-screen-results.json).

## Facts and root-cause evidence

- The Team Admin role has `ActionSecretReadMeta` and `ActionSecretWrite` in the project scope. The runtime confirms project-filtered `GET /admin/v1/secrets` is allowed while an unfiltered collection request is denied.
- Exact RC6 `ProvidersPage.reload` runs connections, secret metadata, and adapter reads together in one `Promise.all`; it updates state only after all three succeed. The unfiltered secret request’s 403 therefore causes the page-level error path and prevents successful response state from being applied.
- Exact RC6 `listSecretMetadata()` requests `/admin/v1/secrets?limit=100` without `project_id`, unlike other project-scoped Console lists.
- These source facts match the independently observed HTTP results and UI state. No authorization bypass or credential disclosure occurred.

## Expected behavior

- A project-scoped Team Admin can view metadata for secrets owned by projects assigned to that principal, including after navigation and hard refresh.
- The Console includes the active project scope in the collection request (or otherwise loads only authorized metadata), and does not show a false empty state when an organization-wide collection is forbidden.
- Organization-wide secret enumeration remains denied to project-only principals; plaintext secret material remains write-only and is never returned or rendered again.
- A failure in one metadata request must not erase independently successful authorized connection/adapter data.

## Actual behavior

The secret row appears immediately from the create response, then disappears from Providers after refresh. The UI shows zero rows and a permission banner even though the same Team Admin can retrieve exactly one safe metadata record by using the documented `project_id` query. The server’s 403 boundary works as intended.

## Business impact

Project administrators cannot reliably reconcile credentials they created or discover a persisted secret after a reload. That blocks safe deletion/rebinding workflows and can lead to duplicate credentials or unnecessary Owner escalation. The false zero count also undermines operational confidence in the credential inventory.

## Acceptance criteria

1. A Team Admin with access to project A can create a project-owned secret and see the same name, type, fingerprint, and credential ID after hard refresh and a fresh login.
2. Project A’s list includes only secrets owned by A; an unfiltered organization-wide list remains denied, and no secret plaintext is returned.
3. Providers Connections, Vault Secrets, and Adapters render their own authorized data even if a separate request is forbidden; errors are scoped to the affected view.
4. Verify Owner, Team Admin, Security Reviewer, Developer, and FinOps Viewer boundaries; two-project and cross-organization isolation; empty, populated, error, refresh, delete, and connection-binding states.

## Evidence

- [Seal secret form](../evidence/CP52-teamadmin-seal-secret-form.png)
- [Secret metadata immediately after creation](../evidence/CP52-teamadmin-secret-immediate.png)
- [Secret metadata after refresh](../evidence/CP52-teamadmin-secret-after-refresh.png)
- [Provider connections page](../evidence/CP52-teamadmin-providers-connections.png)
- [Models](../evidence/CP52-teamadmin-models.png), [FinOps](../evidence/CP52-teamadmin-finops.png), and [Traces](../evidence/CP52-teamadmin-traces.png) same-role controls
- [Sanitized API results](../evidence/CP52-teamadmin-screen-results.json)
