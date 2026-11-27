# CP17 — Fresh setup and Google provider boundary

**Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`  
**Runtime:** separate disposable SQLite runtime on port 18086; screenshots were captured in Chromium at 1440×1000.  
**Implementation changes:** none (`git diff -- web` empty).  
**Release gate:** GATE-REL-07 remains **BLOCKED**.

## Fresh-install journey — PASS for the executed path

A clean runtime first displayed the bootstrap token, owner, organization and project form. The next wizard step explicitly said no provider adapters were enabled and instructed the operator to finish setup, then activate a bundled adapter in Extensions → Marketplace. We completed setup with no initial connection, acknowledged master-key custody, reviewed the organization/project and skipped initial connection, then completed bootstrap. A second, clean browser session logged in successfully; the QA organization and project were visible in the workspace selector. Screenshots for owner, provider-empty, custody, review, overview and second-session persistence were visually inspected: `evidence/CP17-fresh-setup-owner-step.png`, `CP17-fresh-setup-no-adapters.png`, `CP17-fresh-setup-custody.png`, `CP17-fresh-setup-review.png`, `CP17-fresh-setup-overview.png`, and `CP17-fresh-login-persistence.png`.

This verifies the standard clean-install path only. Localization, alternate browsers, bootstrap retry/failure, key-custody recovery, and adapter-staged pre-bootstrap installs remain UNVERIFIED. `evidence/CP17-fresh-setup-first-connection.png` also shows the same no-adapter message; despite its initial working filename, it is not evidence that Gemini was selectable.

## Empty Playground and failure feedback

After login with no provider/model, Playground opened with a blank “Model or route slug” field and a disabled Send button. Entering a synthetic nonexistent slug and prompt minted an ephemeral session, preserved the prompt as a conversation turn, and displayed “Playground inference failed (503).” The screenshots `evidence/CP17-empty-playground.png` and `evidence/CP17-empty-playground-error.png` were visually inspected. No false success was shown. The UI message exposes only the HTTP status; actionable provider/model detail is not shown.

We then opened Traces, FinOps → Chargeback, and Overview in the authenticated UI. The trace persisted as HTTP 503 `no_compliant_deployment_available`; Chargeback showed zero exact/estimated settlement and zero active holds. Overview showed 1 request, 100% errors, zero spend/holds, zero alerts, and “All systems nominal.” All three screenshots were visually inspected: `evidence/CP17-empty-traces-after-503.png`, `CP17-empty-chargeback-after-503.png`, and `CP17-overview-after-failed-playground.png`. This cross-screen evidence is recorded as unresolved observation `reports/QA-OBS-003-overview-nominal-with-total-failure.md`, not a defect pending alert-contract comparison. No successful inference occurred.

## Google provider separation

On the 18084 owner runtime, the official Gemini extension was installed and activated through the Marketplace consent flow; a write-only Google credential was sealed, and a Google connection was created. The actual Console probe returned HTTP 200 with `valid=false`, `failure_code=upstream_unreachable`; the connection remained Unverified. `probe_runtime_unavailable` was not reproduced on this runtime. The saved Marketplace, permission, consent, activation, secret, connection, and probe screenshots remain in `evidence/CP17-*` and were visually inspected during the interaction.

Separately, direct HTTPS calls using the configured QA credential through the Cloud HTTPS proxy returned provider 404 `NOT_FOUND` for `gemini-2.0-flash` and `gemini-2.5-flash`; Google said both models were unavailable for the relevant account and recommended `gemini-3.8-flash`. A small `gemini-3.5-flash-lite` prompt returned `OK` (200). Sanitized exact outcomes are in `evidence/CP17-google-direct-api.json`; the credential is neither printed nor persisted. The app's Gemini validation hook requests `/v1beta/models`; model-generation availability is a separate check.

The Cloud environment is proxy-only for external HTTPS: proxy-routed provider requests succeed, while direct socket egress is unavailable (`Network is unreachable`, prior CP17 probe). This supports an infrastructure boundary for the app's `upstream_unreachable`, not a product defect attribution. Since the standard fresh wizard had no enabled adapters, the SetupPage `gemini-2.0-flash` preset was not selectable in that clean journey. Treat stale model availability as an unresolved compatibility observation for adapter-staged/bootstrap scenarios, not a confirmed defect. The exact app Google inference remains BLOCKED by egress/runtime reachability.

## Status and next work

- Fresh bootstrap + second-session persistence: **PASS**, scoped to Chromium/1440 and no initial provider.
- Empty Playground failure feedback: **PASS** for truthful 503 indication; detailed recovery and persistent accounting: **UNVERIFIED**.
- Google credential direct provider access through configured proxy: **PASS** for the tested model calls; this is not Tanpopo inference.
- Tanpopo Google connection probe: **BLOCKED** by Cloud/runtime upstream reachability; state remained Unverified.
- RC4 DevBox artifact: remains **UNVERIFIED** per CP00/CP02; no new artifact access.
- Race suite: CP02 records the rerun result as PASS for 86 packages; this checkpoint did not rerun it.
- All other coverage and historical defects remain as recorded in the matrix/register; QA-002 is not complete.

Next: independently exercise Marketplace post-bootstrap activation from this fresh runtime; compare Overview alert behavior for an invalid model, provider outage, and healthy request; and continue the historical defect retests and untested screens/browsers. No findings were reclassified from QA-001 evidence.
