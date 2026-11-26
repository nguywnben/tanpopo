# CP124 — Normalized OpenAI image request at the restored project budget boundary

**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1` (artifact run 45581179274)
**Result:** scoped **PASS** for pre-egress project-budget rejection of one normalized OpenAI image request. This reinforces that DEF-074 is specific to the tested native Ollama image shape. No defect was added; QA remains open and GATE-REL-07 remains BLOCKED.

## Reproduction

After CP123, the project’s original 100-micro-USD hard limit was restored. Authenticated readback before the request showed 2,036 exact + 14 estimated micro-USD settled (above the limit), zero active holds, and version 148. An Org Owner issued a PROVIDER key through the real Console restricted to `/v1/*` and `qa004-cp121-priced-vision`.

A cookie-free `POST /v1/chat/completions` sent two previously used synthetic PNGs as standard OpenAI `image_url` blocks to the same healthy Ollama-backed model/deployment, priced at 1,000,000 nano-USD/image (2,000 micro-USD total). Exact RC6 returned HTTP 429 `project_budget_exceeded`, `retryable:false`. The dedicated OpenAI-compatible loopback fixture recorded zero requests, establishing rejection before provider egress. No external provider was contacted.

The persisted trace `trc_01m8b74fmke008jsm30cr76zbp` records status 429 and `project_budget_exceeded`; its `EXACT`/zero cost projection is consistent with this rejected, undispatched request. Chargeback has no row for the temporary key. Afterward, project limit, exact/estimated settled values, zero holds, and version 148 were unchanged. The key was gracefully revoked (REVOKED v2); the original QA Ollama fixture was restored and `/api/tags` returned HTTP 200.

## Classification

CP123 showed that the normalized OpenAI image form on this deployment settles the 2,000-micro-USD image charge. CP124 now shows that with this project already over its restored 100-micro limit, the normalized form is denied before egress with no hold or charge. These controls contrast with CP121/122, where equivalent native Ollama `messages[].images[]` requests bypassed image settlement and the nearly exhausted budget boundary. **DEF-074 remains a FAIL scoped to native Ollama image media extraction on the tested path.** Other native adapters, media formats, budget scopes, concurrent holds, exports, and cross-runtime enforcement remain open.

`../evidence/CP124-normalized-budget-boundary-results.json` contains sanitized machine readback. Real screenshots were visually inspected:

- `../evidence/CP124-normalized-budget-key-form.png`
- `../evidence/CP124-normalized-budget-traces.png`
- `../evidence/CP124-normalized-budget-after.png`
- `../evidence/CP124-normalized-budget-key-revoked.png`

QA continues; **GATE-REL-07 remains BLOCKED.**
