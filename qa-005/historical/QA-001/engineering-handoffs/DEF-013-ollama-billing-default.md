# Engineering handoff — DEF-013: Ollama connection defaults to commercial billing

**Severity:** Moderate (cost attribution metadata can be wrong)  
**Status:** Confirmed default mismatch on isolated fresh-install runtime 8083; no product code changed.

## Reproduction

1. Activate the bundled `@tanpopo/ollama` adapter in a fresh test tenant.
2. Open Providers → New Connection and leave all values at their defaults.
3. Observe that Ollama is selected while Billing mode is `Commercial API` and region is `US`.
4. Compare the bundled manifest description, which says Ollama defaults to `SELF_HOSTED_INFRA`; then select `SELF_HOSTED_INFRA` and `ON_PREM` to create the isolated test connection.

## Evidence

- `../evidence/phase43-new-connection-form.png`: selected provider and default form values.
- `../evidence/phase43-ollama-connection-filled.png` / `phase43-ollama-probe-result.json`: manually corrected self-hosted/on-prem values and successful local probe.
- `extensions/official/ollama/manifest.yaml` states `defaulting to SELF_HOSTED_INFRA billing mode`.

## Expected vs actual

- **Expected:** selecting Ollama preselects self-hosted infrastructure billing and an appropriate local region, or clearly requires an explicit billing choice before creation.
- **Actual:** the form starts at `COMMERCIAL_API` / `US`. A user accepting defaults can persist commercial billing metadata for a loopback self-hosted provider.

## Affected workflows and impact

First-time local Ollama/vLLM setup, provider connection creation, chargeback classification, and any downstream reporting or policy keyed by billing mode. The CP43 test connection was corrected before submission, so this finding does not claim that a misclassified record was actually created or billed.

## Suggested investigation

Initialize or derive the form defaults from the selected adapter's declared billing defaults and data-region semantics. Validate the saved connection metadata against adapter/provider type. Add UI coverage for selecting each local adapter and accepting defaults.

GATE-REL-07 remains BLOCKED.
