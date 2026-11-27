# CP60 — DEF-056 Team Admin model and deployment provisioning

**Source:** frozen unsigned RC7 commit `737079ddb847d97dbcf832f610ed0048556cb65b`.
**Artifact:** exact-source Linux/amd64 binary SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`; `vcs.modified=false`.
**Runtime:** isolated SQLite Control/Data Plane at `127.0.0.1:18145`; real Chromium; synthetic `TEAM_ADMIN`; `QA005 Org / QA005 No Budget Project / QA005 Budgetless Dev`.
**Disposition:** **PASS (scoped)** for historical DEF-056 on current RC7. The prior QA-004 HTTP 403 remains historical evidence. No source/spec change, inference request, external provider, or paid usage.

## Reproduction

1. As the synthetic Org Owner, used Access & Keys → Members & Roles to create a new account with **Team Admin only** and project scope `QA005 No Budget Project`. Activated it through the actual one-time invite enrollment screen. The synthetic username and one-time credential are not secret evidence; the invitation token and password are excluded from the repository and retained only in mode-0600 `/tmp` files.
2. Signed in as the newly activated Team Admin. The real account navigation identified the role as Team Admin and active project/environment scope as QA005.
3. Opened the visible **Register model** action and submitted `qa005-teamadmin-def056-probe` (`QA005 Team Admin DEF056 Probe`, context 8192, max output 2048). The API returned HTTP 201 with assurance `DECLARED`; the Console showed its success toast.
4. Navigated to Logical Models, hard-reloaded, and confirmed the same row remained. Authenticated `GET /admin/v1/models?limit=100` returned the same model ID, slug, display name, dimensions and declared capabilities.
5. Still as this Team Admin, used **Add deployment** with the existing project-scoped loopback Ollama connection and `qa005-native:latest`, explicitly selecting the self-hosted zero-rate declaration. No provider inference was sent. The API returned HTTP 201, connection `ACTIVE`, and deployment `ACTIVE_HEALTHY` with all rates zero.
6. Hard-reloaded again. The deployment stayed Healthy in the Console; authenticated `GET /admin/v1/deployments?limit=100` returned the same ID, model/connection association, `ACTIVE_HEALTHY` state and zero-rate card.

## Disposition and limitations

The historical QA-004 report that a Team Admin model creation was denied with HTTP 403 does not reproduce on the exact QA-005 RC7 source in this scoped SQLite runtime. The actual role-only principal created both the logical model and healthy deployment; state persisted across hard reload and API readback. DEF-056 is therefore **PASS (scoped)** for current RC7. The conflicting prior QA-004 denial remains preserved; this result does not assert when or why the behavior changed.

One pure Team Admin, one project scope, one SQLite runtime, Chromium, a single declared model and one zero-rate deployment are covered. Cross-project and organization scope, other roles and role combinations, edit/delete, deployment error paths, nonzero pricing, other capabilities/backends/browsers and inference remain UNVERIFIED. The Console’s Logical Model registry displayed no model retirement action; this is already captured separately as PRODUCT_GAP PG-004. The unbound model/deployment remain isolated test data; no cleanup endpoint was exercised.

## Evidence

Sanitized request/state readback and limitations: [CP60 evidence](../evidence/CP60-def056-teamadmin-provisioning.json). Visually inspected screenshots: `CP60-teamadmin-only-invite-form.png`, `CP60-teamadmin-enrollment-screen.png`, `CP60-teamadmin-activated.png`, `CP60-teamadmin-model-screen.png`, `CP60-teamadmin-model-form.png`, `CP60-teamadmin-model-result.png`, `CP60-teamadmin-model-persisted-after-reload.png`, `CP60-teamadmin-deployment-form.png`, `CP60-teamadmin-deployment-result.png`, and `CP60-teamadmin-deployment-persisted.png`. The screenshot of the one-time invite disclosure was deleted before evidence staging; no invitation token or password is committed.

Campaign continues and `GATE-REL-07` remains BLOCKED.
