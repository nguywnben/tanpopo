# CP92 — Fresh-install Setup journey and DEF-001 retest

**Source/binary:** frozen RC4 `40bba5b55c2da6fc00b107b315c9b3167fb20169`, existing QA binary  
**Runtime:** new isolated `/tmp` runtime at `127.0.0.1:18087`, separate database and temporary encryption key  
**Browser:** real Chromium at 1440×1000; follow-up login in a separate browser context  
**Gate:** `GATE-REL-07 BLOCKED`

## Environment and journey

All persistent QA runtimes on 18083–18086 reported bootstrap initialized. A separate disposable runtime was started from the frozen binary with a new data directory, one-time bootstrap token, and independently generated key. Before setup, `/readyz` returned Ready and unauthenticated `/admin/v1/bootstrap/status` returned `initialized=false`, eight Marketplace catalog entries, and zero available adapters. The four persistent comparison runtimes on 18083–18086 all reported initialized; the sanitized status matrix is `evidence/CP92-runtime-bootstrap-state-sanitized.json`. The disposable 18087 runtime is stopped and removed. On an already-initialized runtime, the login page’s “Run first-time setup” link returned to Login; this is the bootstrap guard and did not alter state. Its screenshot is retained as an environment check, not a defect finding.

The real browser completed the four-step wizard:

1. Entered a synthetic `.invalid` owner identity, a test-only password, organization/project names, and the one-time bootstrap token.
2. Reviewed the no-adapter state. The UI said no provider adapters were enabled and explained that the eight bundled official adapters must be activated after setup. Skipped the optional first connection.
3. Acknowledged master-key custody.
4. Reviewed the owner, organization/project and skipped-connection state, then completed setup.

Bootstrap status then returned `initialized=true`; the authenticated Admin API showed one `default` project and one owner member. A separate Chromium context signed in using the new account and loaded the Overview, showing the persisted `default / dev` workspace, zero requests, no holds and $0 settled spend. The immediate post-submit navigation first displayed a loading state; the separate login confirmed the settled page. No raw credentials, token, or encryption key were retained in repository evidence. The temporary runtime was stopped and its key, bootstrap token, password file, database, WAL and data directory removed.

## DEF-001 result

**PASS — verified fixed for this fresh-install journey.** Real Chromium exposed Setup stepper accessible labels `Go to step 1: Owner account`, `Go to step 2: First connection`, `Go to step 3: Key custody`, and `Go to step 4: Review`; each accessible name includes the corresponding visible step text. The visible Continue button was discoverable by its exact accessible name and activated the next step. The Complete setup action was accessible and completed bootstrap. This independently retests DEF-001 on a new instance under the frozen RC4 baseline. Other browsers/locales and broader accessibility remain open.

## Evidence

- `evidence/CP92-fresh-setup-step0.png`
- `evidence/CP92-fresh-setup-step0-filled.png`
- `evidence/CP92-fresh-setup-step1.png`
- `evidence/CP92-fresh-setup-step2.png`
- `evidence/CP92-fresh-setup-step3-review.png`
- `evidence/CP92-fresh-setup-complete.png`
- `evidence/CP92-fresh-install-journey.json`
- `evidence/CP92-fresh-install-persistence.json`
- `evidence/CP92-runtime-bootstrap-state-sanitized.json`
- `evidence/CP92-18083-setup-route-state.png` and `evidence/CP92-18083-setup-link-redirect.png` — initialized-runtime comparison, visually inspected.

Screenshots were visually inspected during the journey; the first/final page and intermediate connection, custody and review states were inspected individually. The screenshot masks bootstrap/password values; only the synthetic email and non-secret test labels are visible.

## Consolidated status

- **Historical DEF-001–015:** DEF-001 independently passed the original visible-name/accessibility criterion in a fresh four-step RC4 install. DEF-002–004 and DEF-006–015 retain their prior scoped results but remain incomplete across all roles/platforms; DEF-005 remains partial. No QA-001 PASS is inherited.
- **Historical defects failing / unverified:** DEF-016–034 remain open; no status changed in this phase. Preserve DEF-034 and the DEF-020 recurrence.
- **New findings:** no new defect. Fresh setup completed without an enabled adapter by skipping optional provider setup; this is the observed supported path, with post-bootstrap activation still required for first inference.
- **Coverage completed:** fresh bootstrap, no-adapter state, owner/org/project setup, custody acknowledgement, persistence, new-session login, initial empty Overview, and DEF-001 accessible names.
- **Blocked:** none for CP92. Existing managed Cloud webhook egress, disabled A2A runtime, unavailable RC4 artifact DevBox, and full OTLP collector acceptance remain recorded blockers/unverified items.
- **Coverage remaining:** independent DEF-002–015 acceptance across their residual roles/platforms, DEF-005 SSRF cases, DEF-007 failure atomicity/populated erasure, adapter activation and first inference on this fresh instance, open DEF-016–034 regression/remediation, and broad remaining browser/platform/integration/accessibility/operations cases.
- **Recommended handoffs:** unchanged—DEF-034, DEF-016/023/029/030, DEF-033, DEF-005 without weakening SSRF, and DEF-007.

QA-002 remains in progress; this checkpoint is not a release decision.
