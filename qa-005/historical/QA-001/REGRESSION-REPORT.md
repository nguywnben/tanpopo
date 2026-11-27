# Regression Report

## Checkpoint 2026-11-23

Baseline automated UI result: typecheck passes; Vitest has 38 passing and 2 failing tests due to DEF-001. No Go regression suite could run because `/usr/bin/go` rejects standard Go CLI arguments. No product changes were made, so no before/after regression comparison exists.

Chromium checkpoint: Vite Setup fallback rendered at 1440px and 375px without horizontal overflow. Admin API returned HTTP 500 because no backend process is running; this is an environment limitation, not a regression conclusion.

## Checkpoint 3

Go `go test ./...` passed after setting an explicit test-appropriate umask (`0022`); the prior sole failure under `0077` is explained by actual file permissions. Frontend test baseline still has 38/40 tests passing with `DEF-001`. Real app browser flow validated first-run and login; Gemini upstream probe remains unsuccessful in this proxy-only cloud egress environment. No product files changed.


## Checkpoint 4 — browser regression evidence

- Existing end-to-end Chromium harness passed a disposable full path: extension activation, connection health probe against a deterministic fake server, model/deployment/route setup, virtual key issuance, chat completion 200, and trace persistence/visibility. It also passed multi-org switch, keyboard command palette, and VI/JA string checks.
- 61 accessibility/layout audits across 27 routes, light/dark, desktop/tablet/mobile recorded zero axe violations and zero overflows; no external requests, console errors, or page exceptions. Detailed result: `evidence/phase4-chromium-e2e.json`.
- Existing Go suite result from checkpoint 3 remains: `go test ./...` passed with `umask 0022`. No product code changed. DEF-001 remains; its two Vitest failures are not cleared by the route sweep.
- Harness caveat: LCP/CLS/INP were all reported as zero because its performance observer init callback is not invoked. Those readings are invalid; no regression pass for performance is claimed. Firefox/WebKit engine checks could not launch because host libraries are missing.


## Checkpoint 5 — DEF-002 discovered

A focused Chromium journey on the retained isolated DB reproduced `DEF-002`: failed upstream probe → deployment creation as PROVISIONED → owner activation accepted as ACTIVE_HEALTHY → route target shown Healthy → data-plane request returns 503 `no_compliant_deployment_available`. Key revoked, route/deployment removed, no product code changed. This is an initial reproduction, not a retest. Evidence is indexed at `evidence/phase5-unverified-connection-health-mismatch.json`.


## Checkpoint 6 — cross-browser regression

Firefox and WebKit completed 27 authenticated route axe/layout checks each via the official Playwright 1.62.0 container over HTTPS. Both returned zero axe violations, overflow, missing H1, external requests, console errors, and uncaught page errors. See `evidence/phase5-cross-browser.json`; this is rendering coverage, not exhaustive workflow parity in these engines.


## Checkpoint 7 — observer instrumentation cross-check

A corrected Chromium observer init on Overview recorded one-run TTFB 14.9 ms, FCP 232 ms, LCP 512 ms, CLS 0.0019; the command palette interaction's max Event Timing duration in the sample was 56 ms. This is not a percentile or release performance pass. See `evidence/phase6-performance-observation.json`.


## Checkpoint 8 — budget workflow

API budget creation (PUT 200), organization scope list, UI limit update (PUT 200), and negative-value validation were exercised. The empty Console state has no create action; see DEF-003. The synthetic $120 monthly record remains only in isolated QA data. Budget reservation/enforcement is unverified.
