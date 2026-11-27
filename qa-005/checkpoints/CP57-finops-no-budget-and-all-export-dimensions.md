# CP57 — Budgetless exact settlement and FinOps exports across all dimensions

**Source:** frozen unsigned RC7 commit `737079ddb847d97dbcf832f610ed0048556cb65b`.  
**Artifact:** exact-source Linux/amd64 binary SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`; `vcs.modified=false`.  
**Primary runtime:** isolated SQLite Control/Data Plane at `127.0.0.1:18145`; local deterministic Ollama-compatible fixture at `127.0.0.1:19096`; real Chromium on Linux.  
**Identity/scope:** synthetic `ORG_OWNER`; `QA005 Org / QA005 No Budget Project / QA005 Budgetless Dev`. All test data is isolated. No external or paid provider was used.  
**Disposition:** scoped PASS for historical DEF-012, DEF-013, DEF-014, and DEF-015. No new defect or product gap identified. Campaign continues; `GATE-REL-07` remains BLOCKED.

## Budgetless settlement journey (DEF-012)

1. Created the synthetic Project and Environment through the actual workspace picker. The project/environment create requests returned HTTP 201. The environment's inherited project/org budget query returned HTTP 200 with `data: []`.
2. Through the real Providers form, created `QA005 budgetless Ollama` scoped to the new project. The `@tanpopo/ollama` form defaulted to `SELF_HOSTED_INFRA` / `ON_PREM`; the allowed loopback CIDR was explicitly set to `127.0.0.1/32`. The fixture probe returned HTTP 200, `valid: true`, and detected `qa005-native:latest` in 7 ms. The connection became `ACTIVE`.
3. Through Models & Deployments, bound the existing synthetic logical model to this project-scoped connection, using 100,000,000 / 200,000,000 nano-USD input/output rates. The new Environment deployment was `ACTIVE_HEALTHY`.
4. Through Routes & Policies, created `qa005-budgetless-route` targeting that deployment. Through Access & Keys, issued a `PROVIDER`-only key restricted to `/native/ollama/*`. The plaintext was stored only in `/tmp` with mode `0600`; it is excluded from repository evidence. No A2A or MCP plane was granted.
5. Sent one native chat request using the logical model slug and one using the configured custom route slug. Both were loopback-only HTTP 200 responses, returned `X-Tanpopo-Usage-Accuracy: EXACT`, and reported provider counters 7 input / 4 output. At the configured rates, each settled to `7×100,000,000 + 4×200,000,000 = 1,500,000,000` nano-USD = 1,500,000 micro-USD.
6. Independent trace API readback found both exact 1,500,000-micro-USD settlements, correct Environment and Virtual Key IDs, HTTP 200 status, and no debug payload. `/admin/v1/budgets?project_id=<new-project>&include_ancestors=true` remained HTTP 200 with zero rows. `/admin/v1/usage/summary?group_by=project` returned the new project row at 3,000,000 exact micro-USD and two exact requests.
7. After the first request, hard refresh retained the project budget empty state and its `$1.500000` project chargeback row. The second request then raised the project row to `$3.000000` in the real FinOps Chargeback page; the Budgets & Holds page simultaneously displayed zero ceilings and “No budgets configured.” Independent budget API readback after the second request remained empty. The Owner FinOps headline was `$14.500016` because the accepted Owner view is the full organization ledger; the project-group row isolated this project's `$3.000000` and reconciled to the two traces. The global headline was not misinterpreted as project-only.
8. Revoked the synthetic key through the real UI. Readback confirmed `REVOKED`, version 2; the UI reported zero active streams severed. The loopback connection, deployment, and route remain only in the isolated QA database to preserve a reproducible fixture; the only issued test key is revoked.

**DEF-012 result:** the historical behavior did not reproduce. Exact usage settled and appeared in the FinOps headline and project chargeback even when the tested project had no configured budget rows. This is a scoped pass for the exact-source SQLite + Ollama-compatible path, not all billing modes, role scopes, or backends.

## DEF-014/015 export regression

On the populated synthetic Initial Project view, used the real Console to select each supported grouping dimension and trigger browser downloads for both formats. The downloaded content was parsed and compared field-for-field against the matching HTTP 200 usage-summary response; the visible table rows were also captured and visually inspected.

| Grouping | API / CSV / JSON rows | CSV and JSON match API | Download names |
|---|---:|---|---|
| Project | 1 / 1 / 1 | PASS | `tanpopo-chargeback-project.csv`, `.json` |
| Environment | 1 / 1 / 1 | PASS | `tanpopo-chargeback-env.csv`, `.json` |
| Virtual Key | 3 / 3 / 3 | PASS | `tanpopo-chargeback-key.csv`, `.json` |
| Model | 3 / 3 / 3 | PASS | `tanpopo-chargeback-model.csv`, `.json` |

Every exported row preserved the selected `group_key`, IDs, exact/estimated integer micro-USD values, request counts, and (for Virtual Keys) masked key prefixes. Multi-row Virtual Key and Model exports were included. No export silently reused another dimension, parsed CSV as JSON, or disagreed with the table/API in this scope.

## Reproducibility and evidence

- [Sanitized full provider/trace/budget/usage/key readback](../evidence/CP57-def012-budgetless-settlement.json)
- [API-to-CSV/JSON comparison for all four dimensions](../evidence/CP57-finops-export-verification.json)
- Visually inspected screenshots: `CP57-new-project-connection-form.png`, `CP57-new-project-connection-created.png`, `CP57-connection-probed.png`, `CP57-deployment-form.png`, `CP57-deployment-created.png`, `CP57-route-form.png`, `CP57-route-created.png`, `CP57-vkey-form.png`, `CP57-budgetless-trace.png`, `CP57-budgetless-chargeback-reconciled.png`, `CP57-budgetless-zero-budget-reconciled.png`, `CP57-budgetless-after-refresh.png`, and `CP57-test-key-revoked.png`.
- DEF-014/015 screenshots `CP57-chargeback-project.png`, `CP57-chargeback-environment.png`, `CP57-chargeback-virtual-key.png`, and `CP57-chargeback-model.png`; all were opened and visually inspected. Browser-downloaded files and key material remain outside the repository.

## Limits

One SQLite runtime, one Owner, Chromium on Linux, one Ollama-compatible deterministic fixture, two short nonstreaming requests, and the current 24-hour default view were exercised. No estimated or unmetered events, active holds, billing migration, concurrent export, pagination, larger dataset, non-default date range, non-Owner export role, PostgreSQL/Valkey ledger, other adapter, browser engine, or real provider was tested here. A separate fresh PostgreSQL tenant inspection showed no active provider adapter; its offline cached marketplace listed unsigned/unverified candidates. No adapter was installed there. Screenshots `CP57-providers-empty.png`, `CP57-extensions.png`, and `CP57-marketplace.png` preserve that configuration boundary. This did not block the regression because the already-installed adapter and loopback fixture on the isolated SQLite runtime were available. All `CP57-*.png` artifacts are retained; the budget, provider, extension, scope, deployment, route, key, trace, and FinOps states were visually inspected.

No production implementation or specification was modified. No credentials, provider secrets, or downloadable report files were committed.
