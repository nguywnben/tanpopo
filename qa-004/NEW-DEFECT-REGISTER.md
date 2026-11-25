# QA-004 new defect register

Investigation remains ongoing; this register does not imply full acceptance. See the standalone reports for full reproduction, evidence, impact, hypotheses, and acceptance criteria.

| ID | Severity | Status | Summary | Reproduction / evidence | Expected vs actual | Affected workflow / business impact |
|---|---|---|---|---|---|---|
| DEF-051 | Major | FAIL | Ollama native usage counters are ignored and successful usage is estimated | [Report](reports/DEF-051-ollama-native-counters-estimated.md); `evidence/CP07-native-ollama-first.json`; `evidence/CP07-native-ollama-settlements.json` | Expected exact 6/3 counters; actual settlement estimates 45/45 tokens from response bytes | Ollama native inference, billing, budgets, chargeback |
| DEF-052 | Major | FAIL | Successful native trace accuracy/cost diverge from ledger and FinOps | [Report](reports/DEF-052-native-traces-disagree-with-finops.md); `evidence/CP07-native-ollama-stream-state.json`; `evidence/CP07-native-ollama-settlements.json`; inspected Traces/FinOps screenshots | Expected per-trace accuracy/cost to reconcile; both traces show exact/0 while ledger says estimated/1 and exact/1 micro-USD | Operations, support, audit, billing reconciliation |
| DEF-053 | High | FAIL | Token price API/UI/docs say per token, while settlement uses per-million calculation | [Report](reports/DEF-053-token-price-unit-contract-mismatch.md); `evidence/CP06-deployment-form.png`; `evidence/CP06-deployment-state.json`; `evidence/CP07-native-ollama-settlements.json` | Exact 7/4 usage at configured 1000/2000 nano-USD per token documents to 15 micro-USD; actual cost is 1 micro-USD; contract conflict needs owner resolution | Deployment pricing, settlement, budgets, invoices, chargeback |

Next available ID: `DEF-054`.
