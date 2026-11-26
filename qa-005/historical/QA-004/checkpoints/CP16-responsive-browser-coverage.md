# CP16 — Responsive Chromium interactions and browser availability

**Outcome:** PARTIAL. At 320×844, 390×844, and 768×844, the real Chromium UI and selected mobile workflows remained within the viewport width and were usable. The alternate browser engines could not be run in this Cloud image. No new product defect was confirmed in this phase. GATE-REL-07 remains BLOCKED.

## Mobile and tablet user journeys

Using the project-scoped Developer account, signed in through the actual Console at each responsive viewport. At 390×844, opened Developer Playground, submitted `Reply exactly: mobile screenshot final` to the isolated `qa004-loopback` synthetic Ollama endpoint, and visually verified the rendered assistant response. The completed trace remained available in the Playground. The final viewport screenshot is `evidence/CP16-mobile-playground-result-viewport.png`; the initial empty state is `evidence/CP16-mobile-playground-before.png`. The response was from the local synthetic provider; no live provider credential or paid endpoint was used.

At 390×844 and 320×844, opened FinOps → Chargeback, changed the accessible custom `Group by` combobox from Project to Model, and verified the `qa004-mock` aggregate displayed. The four options (Project, Environment, Virtual Key, Model) opened in the real touch viewport. FinOps rendered the summary cards and grouping without horizontal overflow. At 768×844, the same Chargeback view rendered as a readable tabular layout. Screenshots `CP16-mobile-chargeback.png`, `CP16-320-finops-chargeback.png`, `CP16-tablet-finops-chargeback.png`, `CP16-mobile-chargeback-filter-open.png`, and `CP16-mobile-chargeback-model-filter.png` were opened and visually inspected.

Downloaded the real Chargeback CSV and JSON using the mobile-visible export buttons. Both files parsed. The CSV header was `group_key,exact_micro_usd,estimated_micro_usd,exact_request_count,estimated_request_count,unmetered_request_count`; its one row reported 8 exact requests, 1 estimated request, and zero unmetered requests at export time. The JSON carried the same values. Additional synthetic Playground calls made after export increased the live exact totals, so the export snapshot is intentionally not presented as a current ledger total. See `evidence/CP16-mobile-chargeback-export.csv` and `.json`.

Measured `document.documentElement.scrollWidth` at 320, 390, and 768 CSS pixels; it equaled the viewport at each width. This rules out document-level horizontal overflow for the screens exercised, not every component or width. The controls and page at these sizes are not evidence of accessibility conformance, native mobile support, or full responsive coverage.

## Alternate browser runtime blockers

- Firefox 153.8 was downloaded to `/tmp/qa004-browsers`. Headless launch did not complete within a 10-second bounded attempt. Output included `Sandbox: writing /proc/self/uid_map: EROFS` and `RenderCompositorSWGL failed mapping default framebuffer`; a writable `/tmp` Fontconfig cache did not resolve the timeout. This is an execution-environment blocker, not a Tanpopo result.
- WebKit 2336 was downloaded to `/tmp/qa004-browsers`, but Playwright could not launch it. `ldd` reports absent `libgtk-4.so.1`, `libwebkitgtk-6.0.so.4`, `libjavascriptcoregtk-6.0.so.1`, `libWPEBackend-fdo-1.0.so.1`, `libWPEWebKit-2.0.so.1`, `libbacktrace.so.0`, `libgraphene-1.0.so.0`, `libharfbuzz-icu.so.0`, `libmanette-0.2.so.0`, and `libwpe-1.0.so.1`.
- The exact captured browser diagnostics are sanitized in `evidence/CP16-browser-platform-blockers.json`.

No product defect is inferred from the missing browser runtimes. Firefox, WebKit, Safari, real mobile devices, keyboard/screen-reader conformance, and cross-engine differences remain UNVERIFIED. The full QA campaign is incomplete; continue with other executable coverage rather than treating this checkpoint as a stopping point.
