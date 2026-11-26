# CP122 — Non-zero native image pricing at the project hard-budget boundary

**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1` (artifact run 45581179274)
**Result:** FAIL — additional Major DEF-074 impact. A project budget had only 50 micro-USD of a 100-micro-USD monthly limit available. A native request containing two images priced at 1,000 micro-USD each should fail the pre-egress hard-budget check. Exact RC6 sent the request to the fixture, returned HTTP 200, and persisted a zero-dollar estimated request. No new defect ID was assigned. The temporary key was revoked and the original QA fixture restored.

## Reproduction

The test used the RC6 owner Console in `QA004 Test Root Org / default / dev`, the CP121 vision model, active loopback deployment, and single-target route. The deployment rate card was retained as `image_unit_nano_usd: 1000000`, zero token rates, and `self_hosted_zero_rate:false`. One million nano-USD equals 1,000 micro-USD ($0.001) per image; the two-image request therefore costs 2,000 micro-USD ($0.002) before any token cost.

Before dispatch, the authenticated monthly PROJECT budget for `prj_01m895k8pee008b440xk2ef12p` had a 100-micro-USD hard limit. The Console showed exact settled $0.000036 plus estimated $0.000014 and no active hold: 50 micro-USD had been consumed, leaving only 50 micro-USD. The documented two-phase budget contract requires checking settled spend plus carryover and active pre-holds against the hard ceiling before dispatch; a correctly priced 2,000-micro request exceeds the limit.

An Org Owner issued a temporary PROVIDER-plane VKEY limited to `/native/ollama/*` and `qa004-cp121-priced-vision`. A cookie-free `POST /native/ollama/api/chat` sent two valid distinct synthetic PNGs in the native `messages[0].images` field. The loopback fixture received the request with both image hashes, no Authorization header, and no prompt/image bytes stored. Instead of a pre-egress 429 `project_budget_exceeded`, Tanpopo returned HTTP 200 and the fixture answered. The trace is `trc_01m8b6669re00bqvd07kjbfg7a` (200, no error, Exact/$0).

After the request, authenticated `GET /admin/v1/usage/summary?group_by=key` reported the new VKEY at 0 exact micro-USD, 0 estimated micro-USD, 0 exact / 1 estimated request, 0 unmetered. The project budget remained at 100 micro-USD with exact settled 36, estimated settled 14, and zero reserved hold. Thus the request both crossed the pre-egress boundary and left budget accounting unchanged, while the fixture proves external-to-Tanpopo loopback dispatch occurred. This extends CP121’s missing image-unit settlement into hard-budget enforcement under DEF-074.

The real Console then gracefully revoked the temporary VKEY; authenticated readback showed `REVOKED` version 2. The original QA Ollama fixture was restarted and `/api/tags` returned HTTP 200. No external provider, real credential, or production code was involved.

## Evidence

- `../evidence/CP122-image-budget-boundary-results.json` — sanitized before/after budget, fixture, trace, Chargeback and revocation readbacks.
- `../evidence/CP122-image-budget-key-form.png` — real route/model-restricted key form.
- `../evidence/CP122-image-budget-traces.png` — successful request trace; visually inspected.
- `../evidence/CP122-image-budget-after.png` — real Budgets & Holds UI after dispatch; 100-micro limit, 50-micro settled use, no hold; visually inspected.
- `../evidence/CP122-image-budget-chargeback.png` — VKEY Chargeback UI row with 0 exact / 1 estimated, both $0; visually inspected.
- `../evidence/CP122-image-budget-key-revoked.png` — successful graceful revoke; visually inspected.

## Limits

This test exercises one non-streamed native Ollama chat, a project-level Monthly hard budget, and a loopback self-hosted deployment with a configured non-zero image price. It does not establish the behavior for normalized OpenAI image shapes, other native adapters, image-pixel rates, concurrent budget holds, multi-node Valkey budget enforcement, or nonzero token-plus-image combinations. QA continues; **GATE-REL-07 remains BLOCKED.**
