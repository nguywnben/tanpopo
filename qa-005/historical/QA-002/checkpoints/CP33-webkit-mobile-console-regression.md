# CP33 — WebKit mobile Console regression at 375×844

**Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`  
**Runtime:** disposable SQLite Tanpopo on `127.0.0.1:18086`, locally HTTPS proxied to WebKit 26.5, synthetic data only  
**Release gate:** `GATE-REL-07` remains **BLOCKED**.

## Actions and results

- Signed in as the isolated Org Owner in WebKit at 375×844, opened the responsive navigation, and visited Overview, Access & Keys, Members & Roles, FinOps, and Security.
- The responsive navigation opens as a white left drawer over a dimmed page, displays grouped destinations, workspace/member identity, locale and theme controls, and closes on navigation. The initial screenshot was taken during its transition and showed only the dimmed page; this was a capture-timing artifact. A second screenshot after 1.3 seconds visually confirmed the rendered drawer. No navigation defect is reported.
- Each visited route loaded persisted content. Browser-observed admin API reads returned 200. No horizontal document overflow was measured: `innerWidth`, document width and body width were all 375 for the route captures.
- Access & Keys showed 19 environment Virtual Keys, including active and revoked cards; Members & Roles showed three persisted principals and roles; FinOps showed exact/estimated/hold/unmetered summaries and budget rows; Security showed an empty pending queue and kill-switch action. This is accumulated QA runtime state, not a fresh-install state.
- Overview displayed 13 requests and a 53.85% internal/upstream error rate alongside zero alerts and “All systems nominal.” This independently reproduces the presentation tension documented in QA-OBS-003. The workload includes accumulated intentional negative tests; alert semantics remain unresolved, so this remains an observation rather than a defect.
- Visually inspected the overview, settled navigation drawer, Access & Keys, Members & Roles, FinOps, and Security screenshots after interaction. Screen content was readable at this width. Some in-page tab bars extend beyond the viewport and expose later tabs only by horizontal scrolling; the document itself does not overflow. Usability with touch scrolling and at 320px remains UNVERIFIED.

## Evidence

- `../evidence/CP33-webkit-overview-375.png`
- `../evidence/CP33-webkit-navigation-settled-375.png`
- `../evidence/CP33-webkit-navigation-open-375.png` (transition frame; superseded by settled capture)
- `../evidence/CP33-webkit-access-keys-375.png`
- `../evidence/CP33-webkit-access-members-375.png`
- `../evidence/CP33-webkit-finops-375.png`
- `../evidence/CP33-webkit-security-375.png`
- `../evidence/CP33-webkit-mobile.json` (sanitized viewport, rendered text and API status summary)

## Scope limits

This is a responsive smoke pass for these five screens and one role. Other routes, menu keyboard/focus behavior, touch scrolling of secondary tabs, error/loading states, 320px and tablet/desktop widths, locales, themes, and other browser engines remain UNVERIFIED. No implementation file was changed.
