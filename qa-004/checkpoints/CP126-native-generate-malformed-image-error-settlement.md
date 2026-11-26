# CP126 — Native Ollama Generate malformed image error and hold release

**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1` (artifact run 45581179274)
**Result:** scoped **PASS** for one upstream-rejected malformed image on native `/api/generate`: Tanpopo returned non-retryable 400, no Chargeback row was created, and no reservation remained. No defect was added; QA remains open and GATE-REL-07 remains BLOCKED.

## Reproduction

The isolated project originally had a 100-micro-USD limit and was over budget after prior CP123 work. Through the real FinOps UI, the limit was temporarily raised to 5,000; authenticated readback showed settled exact 2,036 plus estimated 14 micro-USD and no hold. The original 100 limit was restored after this error-path request.

An Org Owner used the real Console to issue a temporary PROVIDER key restricted to `/native/ollama/*` and `qa004-cp121-priced-vision`. A cookie-free `POST /native/ollama/api/generate` sent one deliberately invalid base64 marker in top-level `images`. The dedicated synthetic upstream received one image field with zero valid image hashes, no Authorization header, and returned HTTP 400. Tanpopo surfaced non-retryable HTTP 400 `upstream_bad_request`.

Persistent trace `trc_01m8b7jrvse008cgjf6f3625zn` records status 400 and the mapped error. The Virtual Key Chargeback query returns no row; post-request budget readback, after restoring the original 100 limit, shows exact/estimated settlement unchanged at 2,036/14 micro-USD and zero active hold. The synthetic request content was not written to persistent evidence, the fixture stored only shape/count metadata, and no external provider was contacted.

The temporary key was gracefully revoked (REVOKED v2), original QA fixture restored, and `/api/tags` returned HTTP 200. Real Traces, Chargeback, revoked key, and restored budget screenshots were visually inspected.

## Classification and limitations

This is a scoped negative-input/error settlement PASS for one malformed image on native Generate. It shows no charge/hold after the synthetic upstream rejected the request. It does not establish malformed image handling for native chat, pre-egress validation, malformed media dimensions/size, timeout/connection errors, streaming failures, or production provider billing. It does not resolve DEF-074: valid images on native `/api/generate` remain uncharged at the configured non-zero image price (CP125). The trace says Exact/$0 for this rejected request; this zero-charge case does not repair DEF-052’s successful nonzero settlement mismatches.

See [`sanitized results`](../evidence/CP126-malformed-generate-image-results.json) and the visually inspected screenshots:

- `../evidence/CP126-malformed-generate-budget-increased.png`
- `../evidence/CP126-malformed-generate-key-form.png`
- `../evidence/CP126-malformed-generate-traces.png`
- `../evidence/CP126-malformed-generate-chargeback.png`
- `../evidence/CP126-malformed-generate-key-revoked.png`
- `../evidence/CP126-malformed-generate-budget-restored.png`

QA continues; **GATE-REL-07 remains BLOCKED.**
