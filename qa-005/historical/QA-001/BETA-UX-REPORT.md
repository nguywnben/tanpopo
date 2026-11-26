# Beta UX Report

## Checkpoint 2026-11-23

No visual UX findings are claimed: browser automation/desktop access was unavailable and no screenshots were captured. The automated axe checks reported two reproducible accessibility test failures concerning a visible step label not being included in the button's accessible name. See `DEF-001`. Layout, responsive widths, themes, localization, loading, navigation, and role-specific states remain unverified.

## Checkpoint 2 — Chromium rendering

Installed Playwright was used with system Chromium against the actual Vite dev server at `http://127.0.0.1:4173/setup` (frontend only; backend absent). Screenshots are `evidence/setup-desktop-1440.png` and `evidence/setup-mobile-375.png`. I visually inspected the desktop screenshot: the page is otherwise blank and centers an error banner, “Setup status unavailable — Admin API request failed (500).” Chromium observed four 500 resource responses, no page/console JavaScript exceptions. Document width equals viewport width at desktop 1440 and mobile 375. This records the backend-dependent unavailable state, not a confirmed product defect. No other UI state has been visually tested.

## Checkpoint 3 — real app screens

Authenticated Chromium screenshots of all 12 primary navigation pages were captured at 1440px and 375px; contact sheet `evidence/contact-sheet-1440.png` was visually inspected. Representative detailed screens: first-run form, setup review/completion, overview empty state, Marketplace/permission consent/active extension, Providers adapter/secrets/connection/probe, Access members, reviewer access denial, Security approvals. No JS page errors or external runtime requests were recorded in the main-page sweep. The tables and empty states are legible and visually consistent at 1440px; responsive per-page behavior at 375px is recorded for width overflow but still needs detailed visual inspection. Provider probe failure is clearly shown as a red banner and unverified connection rather than false success. `DEF-001` remains the existing automated accessibility issue; no new UX defect is assigned yet.


## Checkpoint 4 — route/theme/viewport audit

- Real Chromium E2E screen audit completed 61 samples over 27 routes: 42 at 1440px, 4 at 768px, 15 at 375px; 46 light and 15 dark. Each sample had one non-empty H1, document width equaled viewport width, and axe recorded zero violations. No external runtime requests, console errors, or uncaught page errors occurred. Structured data: `evidence/phase4-chromium-e2e.json`.
- Interactive journey passed through extension activation, provider probe against the harness fake upstream, model/deployment/route, key issuance, actual chat completion, and trace display. Multi-org switching, command palette keyboard flow, and Vietnamese/Japanese translations were also exercised.
- Previously captured and visually reviewed screenshots still document actual application states (setup, extension consent/activation, secret sealing, provider failure, role denial, and screen contact sheet). The E2E harness itself did not capture new screenshots; it only saved audit metrics. The two prior Vitest axe failures remain DEF-001; the new route sweep did not report blocking axe issues and does not close that finding.
- Firefox and WebKit packages downloaded but cannot launch in this host because required GTK4/Graphene/HarfBuzz ICU/Manette/Hyphen/GLESv2 system libraries are absent. Chromium is the only browser engine visually exercised. Performance observer outputs are invalid due to a harness initialization bug; no performance conclusion is claimed.


## Checkpoint 5 — readiness state contradiction

`DEF-002` is a major operational UX issue: the provider screen continues to say `Created Unverified` after two failed probes, while an owner can activate its deployment and both Models and Routes display `Healthy`. The first request then fails with a generic 503 `no_compliant_deployment_available`. The resolver blocks the request safely, but the success banner and green target state mislead operators about service readiness. Evidence: `evidence/phase5-unverified-connection-health-mismatch.json` and linked screenshots.

- Route Explain visually presented zero eligible and zero rejected candidates for the retained model after the temporary deployment was removed. The result does not explain that no deployment is configured; retain as a usability observation for later comparison, not a confirmed defect. Screenshot: `evidence/routes-explain-no-deployment.png`.


## Checkpoint 6 — Firefox/WebKit visual coverage

Ran authenticated 27-route scans in Firefox and WebKit from the matching official Playwright container over HTTPS. Both recorded zero axe violations, overflow, external requests, console errors, or page errors. Representative desktop and mobile screenshots were captured for Overview, Models, Routes, FinOps Budgets and Security Approvals in both engines. Visual inspection found the 375px navigation collapse and content readable in the inspected states. Axe/layout checks at 375px for these engines remain outstanding; Chromium covers responsive axe/layout. Evidence: `evidence/phase5-cross-browser.json`, `firefox-https-*.png`, and `webkit-*.png`.


## Checkpoint 7 — one-off paint/interaction timing

With corrected observers on Chromium Overview at 1440×900, the single run recorded LCP 512 ms, CLS 0.0019, TTFB 14.9 ms, FCP 232 ms; max Event Timing duration was 56 ms across the command palette interaction. This is a spot observation, not an INP percentile or performance acceptance result. Details: `evidence/phase6-performance-observation.json`.


## Checkpoint 8 — FinOps empty state

The FinOps empty state says “No budgets configured” but offers no Add/Create action. Budget creation is possible via the Admin API; after API seeding, the organization budget was visible under workspace `No project` and the UI successfully updated its ceiling. Negative numeric input was rejected inline. This leaves a moderate workflow gap (DEF-003), not an API creation failure. Visual evidence: `finops-budgets-empty-1440.png`, `finops-budget-org-scope.png`, `finops-budget-edited-ui.png`, `finops-budget-negative-validation.png`.

## Checkpoint 10 — MCP and A2A registration states

- Logged in as the isolated Org Owner in real Chromium and inspected the MCP and A2A empty states. The A2A page explains AgentCard, residency and delegation bounds; both pages expose a registration form. Screenshots: `evidence/phase10-mcp-hub-list.png`, `evidence/phase10-mcp-register-form.png`, `evidence/phase10-a2a-agents-empty.png`.
- Enabling the visible private/loopback endpoint checkbox and submitting either form leaves the drawer open and displays a red error requiring `allowed_cidrs`. Neither form has a CIDR input or sends that field. The backend returns HTTP 400 for each attempt; no integration was created. This is DEF-005, Moderate. Error-state screenshots: `evidence/phase10-mcp-private-endpoint-validation.png` and `evidence/phase10-a2a-private-endpoint-rejected.png`.
- The MCP empty-state and registration drawer are legible at 1440px. Broader tool discovery, schema approval and AgentCard interactions could not be completed through this private endpoint path. Public HTTPS registration and the rest of both integration workflows remain open.

## Checkpoint 11 — project/environment scope provisioning

From the workspace scope switcher, the Org Owner opened the project form, created `QA Isolation 2026-11-23`, then created `QA Sandbox` within it. The selector visibly switched from the default project/no-env scope to the new project/no-env scope and then to the new environment. The forms and final Overview are captured in `evidence/phase11-new-project-form.png`, `phase11-project-created.png`, and `phase11-environment-created.png`; the final screen is readable at 1440px. QA records remain in the isolated database for continued scope testing.

## Checkpoint 12 — organization switching

Operations → Organizations created `QA Boundary Organization`; the green success banner and second tenant row appeared. Switching into it changed the top scope to `QA Boundary Organization / No project / No env`. The open scope menu showed both organization names but no project in the new organization; the QA project remains under Default Organization. Evidence: `evidence/phase12-org-created.png` and `phase12-second-org-scope.png`. The switcher and empty scope are visually clear at 1440px. Cross-org access denial for a non-operator is still not verified.

## Checkpoint 13 — Team Admin role view

Access & Keys displayed the synthetic `qa-boundary-probe@example.invalid` principal with Developer and Team Admin badges after the temporary Owner role was removed. The signed-in Overview footer displayed “Developer, Team Admin”; the application presented the expected sidebar. Screenshots: `evidence/phase13-boundary-probe-team-admin.png` and `phase13-team-admin-session.png`. The organization boundary was also exercised through an authenticated browser API call; see the Alpha report for 200/404 results. This does not establish parity for every role or page.

## Checkpoints 14–15 — Team Admin access controls

The Team Admin did not see Operations or Security in the nav; direct `/ops/health` showed an explicit role-specific denial. The Access & Keys Members page was available and showed Add member/Edit roles actions. Opening Add member displayed only email and roles, with no project scope field. Submitting produced a red 403 permission error while the drawer remained open and the member count stayed at four. Evidence: `evidence/phase14-team-admin-ops-denial.png`, `phase14-team-admin-members.png`, `phase15-team-admin-add-member-form.png`, and `phase15-team-admin-add-member-rejected.png`. The empty project scope mismatch is DEF-006.

## Checkpoint 16 — backup action error state

Operations → Backups rendered a clear empty state and an enabled Create encrypted backup button. After submit, a red banner showed the HTTP 503 prerequisite message; the zero-receipt empty state and enabled retry button remained. Repeating after adding a valid test config file showed the same result. Screenshots: `evidence/phase16-backups-empty.png`, `phase16-backup-console-failure.png`, `phase16-backup-config-file-still-503.png`. The action availability mismatch is DEF-008. CLI archive creation/verification details are in the Alpha and Security reports.

## Checkpoint 17 — Operations and audit states

Captured and visually inspected the real Chromium screens for Operations health, upgrade preflight, webhook list/drawer, and Security audit log. The health page clearly reports Ready, Serving traffic, SQLite WAL, and one OK node. Upgrade preflight for `0.2.0` clearly reports Compatible / READY_TO_UPGRADE. The webhook empty state explains the HMAC credential prerequisite; Add webhook opens a legible drawer, but Register remains disabled without a signing secret. No submission occurred. The audit log verification action displays a green “Hash chain verified” banner for 172 events. Screenshots: `phase17-ops-health.png`, `phase17-ops-upgrades.png`, `phase17-ops-webhooks.png`, `phase17-webhook-register-form.png`, `phase17-upgrade-preflight-result.png`, and `phase17-audit-chain-verify.png`; sanitized responses are in `phase17-operations-interactions.json`.

## Checkpoint 18 — narrow Operations views across browser engines

At 375×844, real Chromium, Firefox, and WebKit sessions loaded Operations health, Webhooks, and Security Audit Log with no API errors and no horizontal document overflow. In all three engines the webhook drawer’s form fields appeared in the accessible text tree and Register was disabled while no HMAC credential was available. Visual inspection showed the drawer within the viewport in Chromium and Firefox. In WebKit, its heading begins at x=392px, outside the 375px viewport; the screenshot remains on the empty state and the form cannot be seen. Added DEF-009, Moderate. Evidence and geometry: `phase18-ops-cross-engine-mobile.json`, `phase18-webkit-drawer-layout.json`, and the three per-engine Webhooks screenshots. This finding is scoped to WebKit at the tested mobile width; broader Safari versions/devices remain untested.

## Checkpoint 22 — Security kill-switch states

The authenticated Chromium Security screen showed the trigger form and zero-impact blast-radius warning, followed by a clear confirmation dialog for the isolated QA provider connection. After trigger, the green status banner reported one entity affected and zero streams severed; after Clear, the green completion message and empty state showed zero active switches. The preview, confirmation, active and cleared screenshots were captured and visually inspected: `phase22-killswitch-preview.png`, `phase22-killswitch-confirm.png`, `phase22-killswitch-active.png`, and `phase22-killswitch-cleared.png`. No new UX defect was assigned. This is one desktop Chromium journey; mobile and other target-level states remain open.

## Checkpoint 23 — audit log confirmation

The Security Audit Log visibly listed the kill-switch trigger and clear actions in sequence. Verify hash chain produced a clear green success state for 189 events. The long event table is readable at 1440px, though this check does not cover mobile audit presentation or detail inspection for each event. Screenshot `evidence/phase23-post-clear-audit-verify.png` was visually inspected; response and text summary: `evidence/phase23-post-clear-audit-verify.json`.

## Checkpoint 24 — kill-switch form on mobile engines

Firefox 153 and WebKit 26.5 both displayed the Security kill-switch drawer at 375×844. The 250px-wide heading was placed from x=73 to x=323, the form fields and footer actions were visible, and the document did not overflow horizontally. “Review & trigger” remained disabled with required fields empty; active switch count stayed at zero. Visual evidence: `phase24-firefox-killswitch-375.png` and `phase24-webkit-killswitch-375.png`. DEF-009 remains specific to the previously observed WebKit Webhooks drawer; this test does not retest that view.

Detailed measurements and sanitized responses are indexed in `evidence/phase24-killswitch-cross-engine-mobile-sanitized.json`.

## Checkpoint 25 — provider probe error states

After network revision 9 was published, the active isolated runtime showed a red `connection validation runtime unavailable` banner after Probe and left the connection `Created Unverified`. In the restored runtime, a direct allowed-origin retry showed `upstream connection could not be reached` and likewise retained the unverified state. Both error states are clearly distinguished visually; the HTTPS proxy attempt was an origin-policy rejection and was not counted as a provider result. Screenshots `phase25-provider-probe-after-republish.png` and `phase25-restored-direct-provider-probe.png` were visually inspected. No successful inference is claimed.

## Checkpoint 27 — WebKit drawer discrepancy and form feedback

At 375×844 in WebKit 26.5, the Add webhook interaction now visibly opens the drawer within the viewport; its heading remains at x=73 from 0 through 1000ms and the document width stays 375px. The CP18 screenshot and measured offscreen layout remain preserved; DEF-009 stays open until the earlier result is reconciled against the same browser/build and interaction timing. The private A2A form visibly offers the private-endpoint checkbox but no CIDR control; submission leaves the form open with a red API error banner, contributing to DEF-005. Visual evidence was inspected directly, not inferred from DOM assertions. Browser/feature UX coverage remains partial; GATE-REL-07 remains BLOCKED.

## Checkpoint 28 — A2A registration state transition

The real registration drawer showed the private endpoint option but no CIDR field. Its unmodified request therefore continues to fail (DEF-005). During a separate QA-only backend smoke, the browser harness injected `127.0.0.1/32`; after HTTP 201 the page changed to a green success banner, count 1, and an Active row. The before/after screenshots were visually reviewed. The mock was not called; this validates the displayed success transition only when the backend accepts the explicit CIDR. No product code changed and A2A task UX remains untested.

## Checkpoint 31 — Setup step naming retest

The fresh-instance Setup screenshot shows “Owner account” with accessible name “Go to step 1: Owner account”; the visible phrase is present in the accessible name. The axe scan found zero WCAG violations on this view. DEF-001 therefore did not reproduce in the live browser state; its two prior Vitest failures remain unresolved and the finding stays open. Evidence: `phase31-fresh-setup-step1.png`, `phase31-def001-fresh-setup-labeling.json`, and `phase31-fresh-setup-axe.json`. The wizard was not submitted.

## Checkpoints 32–34 — key issue and A2A mismatch

The Issue Virtual Key drawer visually offers only model and MCP tool access. The A2A agents tab is visible elsewhere in the Console, but a standard key cannot use the enabled data plane: the cookie-free `GET /a2a/agents` call returned 403 because the issued key had `PROVIDER` plane only. This mismatch is DEF-010. The UI key was revoked after the probe. Evidence includes the reviewed Issue drawer and A2A agent-list screenshots. GATE-REL-07 remains BLOCKED.

## Checkpoint 35 — Access & Keys loading state

The immediate post-navigation capture showed a loading placeholder; by 300ms the list had rendered the revoked QA keys and stayed stable through 2500ms. This was capture timing, not a persistent stale screen in the sampled run. The loaded screenshot was visually reviewed. Delayed-network and concurrent-change behavior remain untested.

## Checkpoints 36–37 — key lifecycle feedback

The rotation confirmation clearly stated the 24-hour old/new key grace and one-time replacement display. Following a graceful UI revoke, the Console showed a green success banner with “0 active streams severed” and updated the row to Revoked without manual reload; the revoked token then failed with 401. Screenshots of the confirmation, modal and refreshed row were visually inspected. Emergency abort UX and active-stream behavior remain open.
