# DEF-053 — Token rate contract says nano-USD per token while settlement applies configured values per million

**Severity:** High  
**Status:** FAIL — contract and implementation behavior conflict; reproduced cost impact with synthetic exact usage.  
**Affected workflow:** Deployment creation/edit, pricing, provider settlement, budgets, chargeback and FinOps.

## Reproduction

1. On the isolated RC6 runtime, create a deployment through the real Models Console with input rate `1000` and output rate `2000` using the visible fields “Input token (nano-USD)” and “Output token (nano-USD).” The API readback preserves `input_token_nano_usd:1000` and `output_token_nano_usd:2000`.
2. Send the synthetic stream request that returns exact usage of 7 input and 4 output tokens.
3. Read the durable settlement row and compare it with the documented unit contract.

## Expected / actual

The RC6 `docs/architecture/data-model.md` says token rates are nano-USD (`10^-9 USD / token`). `docs/specifications/usage-billing-observability.md` states nano-USD per token and gives `$0.015 / 1M tokens = 15 nano-USD / token`. The Console labels the fields per input/output token in nano-USD, and the API fields are named `input_token_nano_usd` / `output_token_nano_usd`.

Under that stated contract, exact 7/4 token usage at 1000/2000 nano-USD per token is 15,000 nano-USD, or 15 micro-USD after ceiling conversion. Actual settlement is 1 micro-USD. Source inspection shows the pricing calculator fields are `InputPer1MNanoUSD` / `OutputPer1MNanoUSD`, divides the token product by 1,000,000, and the admin mapping passes the per-token-named DTO values directly into those per-million fields. This is a 15x difference in this low-volume sample (minimum one-micro ceiling applies); at scale the denominator creates a 1,000,000x unit mismatch before rounding.

## Evidence

- `../evidence/CP06-deployment-form.png` — real Console form, visually inspected; labels omit the per-million denominator.
- `../evidence/CP06-deployment-state.json` — deployment API readback with configured rates.
- `../evidence/CP07-native-ollama-stream.json` — exact 7/4 upstream counters.
- `../evidence/CP07-native-ollama-settlements.json` — actual persisted cost of 1 micro-USD.
- Source contract references: RC6 `docs/architecture/data-model.md` line 19; `docs/specifications/usage-billing-observability.md` line 26; Console English locale keys `models.rate.input_token_nano_usd` and `models.rate.output_token_nano_usd`; calculator `internal/finops/pricing/calculator.go`; direct mapping in `internal/admin/http/models.go`.

### Independent provider-plane retest (CP30)

The recovered synthetic Ollama route returned exact usage of 7 input / 4 output tokens with the same displayed 1,000 / 2,000 nano-USD fields and `self_hosted_zero_rate=false`. Under the Console's per-token labels, this is 15,000 nano-USD, or 15 micro-USD. FinOps again recorded 1 micro-USD for the exact request. This independently reproduces the documented unit conflict in a provider-plane route; it does not resolve which contract Product intends. See `evidence/CP30-provider-recovery-results.json`.

## Business impact

Pricing can be under-collected by orders of magnitude, while organization owners have no UI cue that the configured values are interpreted per million. Budgets, chargeback and customer invoices can be materially wrong. A unit change can also make existing saved price cards unsafe to reinterpret without migration.

## Root-cause hypothesis (not confirmed)

Pricing code and snapshot schema may have evolved to per-million rates while public DTO names, Console labels, architecture docs and billing specification retained per-token semantics. Alternatively, those contracts may be stale. Product ownership must select one canonical unit and reconcile every boundary. This QA finding records the observed contract conflict, not an assumption about intended commercial pricing.

## Acceptance criteria

- A single canonical rate unit is selected and stated consistently in API/OpenAPI schemas, persistence/snapshots, calculator, Console labels/help, examples and billing docs.
- Round-trip create/read preserves meaning and values; no silent unit conversion or ambiguous legacy field names.
- Existing deployments retain their historical economic meaning through an explicit migration/versioning strategy.
- Deterministic tests verify one token, one million tokens, mixed input/output, ceiling boundaries, zero/self-hosted rates, and budget enforcement against the displayed prices.
- The UI communicates example cost or denominator sufficiently to prevent configuration mistakes.
