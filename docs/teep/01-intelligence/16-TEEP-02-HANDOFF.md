# 16 — Structured Handoff to `TEEP-02A` (Master Feature Catalog)

- **Document ID:** `TEEP-01-DOC-16`
- **From:** `TEEP-01` (Product & Technology Intelligence)
- **To:** `TEEP-02A` (Master Feature Catalog & Release Wave Scoping)
- **Handoff Date:** `2026-11-27`
- **Branch:** `planning/teep-01-intelligence` (anchored at frozen RC9 `ac9ae87a68f095203de82968ac5d08729e33a531`)
- **Gate Verdict:** **`READY_FOR_TEEP_01_REVIEW_GATE`** (`GATE-REL-07` remains `BLOCKED`).

---

## 1. What `TEEP-01` Delivers to `TEEP-02A`

`TEEP-01` provides a **100% source-verified, primary-evidence-backed intelligence baseline** so `TEEP-02A` never has to rely on outdated discovery notes, README assumptions, or synthetic competitor marketing claims:

1. **Complete RC9 Ground-Truth Audit ([`04-CURRENT-TANPOPO-PRODUCT-AUDIT.md`](./04-CURRENT-TANPOPO-PRODUCT-AUDIT.md), [`data/rc9-subsystem-and-api-audit.json`](./data/rc9-subsystem-and-api-audit.json)):**
   - Identifies the `10` crown-jewel RC9 capabilities to **`KEEP`**, the `9` unwired/unreachable runtime paths (`rollup`, `vault/kms`, `ttl.Cleaner`, `CreateDebugCaptureSession`, `TokenVerifier`, `OIDC`, `WorkloadIdentityExchanger`, `GuardrailHooks`/non-provider extensions, and `request_timeout_ms`), the `4` `404` OpenAPI Admin endpoints, the `17` unexposed Console UI endpoints (`+ 2` dead client functions), and the exact `403 org_trust_policy_denied` Day-0 onboarding blocker.
2. **Complete QA-005 & Architectural Debt Preservation ([`12-ARCHITECTURAL-DEBT-AND-RESIDUAL-RISKS.md`](./12-ARCHITECTURAL-DEBT-AND-RESIDUAL-RISKS.md), [`data/qa-debt-and-risk-ledger.json`](./data/qa-debt-and-risk-ledger.json)):**
   - Preserves `DEF-001`..`DEF-086` (`15` open `PARTIAL`s including deep root-cause dissections of `DEF-041`, `DEF-072`'s newly uncovered PostgreSQL `FORCE RLS` `0`-count bypass in `handleRetireMasterKEK`, and `DEF-078`), `PG-001`..`PG-014`, `RR-001`..`RR-029` (including all 4 `.tpkg` digest divergence vectors), `COND-PO-01`..`05`, and all `18` `O(N_orgs)` RLS loops + `7` missing PostgreSQL indexes under `ARCH-OBS-01`.
3. **2026 Market, Protocol & Model Intelligence ([`02-MARKET-LANDSCAPE.md`](./02-MARKET-LANDSCAPE.md), [`03-COMPETITIVE-CAPABILITY-MATRIX.md`](./03-COMPETITIVE-CAPABILITY-MATRIX.md), [`05-PROTOCOL-AND-TECHNOLOGY-INTELLIGENCE.md`](./05-PROTOCOL-AND-TECHNOLOGY-INTELLIGENCE.md)):**
   - Resolves all M0 `[UNVERIFIED]` items (`Portkey → Palo Alto Prisma AIRS`, `TensorZero` archived, `protectai/llm-guard` archived, `Smithery` shut down, `OpenRouter` Aug 2026 BYOK tier), maps `MCP 2026-07-28` Stateless HTTP and `LF A2A v1.0.1` wire contracts, and proves empirically across `7,193` LiteLLM models (`1,664` fractional sub-nano rates) why `RateCard` v3 must standardize on `nano_usd_per_1m_tokens` ($10^{-15}$ USD/tok).
4. **25-Domain Preliminary Capability Inventory ([`13-PRELIMINARY-CAPABILITY-INVENTORY.md`](./13-PRELIMINARY-CAPABILITY-INVENTORY.md), [`data/preliminary-capability-inventory.json`](./data/preliminary-capability-inventory.json)):**
   - `161` substantive candidate capabilities (`CAP-001`..`CAP-161`: `10 KEEP`, `73 IMPROVE`, `5 REPLACE`, `1 REMOVE`, `72 ADD`) + `12` validated original product innovations (`INN-01`..`INN-12`) + `10` falsifiable `HYPOTHESIS / UNVERIFIED` enterprise quality attribute scenarios (`QAS-ENT-01`..`10`).

---

## 2. Recommended `TEEP-02A` Intake Sequence & Guardrails

1. **Step 1 — Resolve Founder Decision Points (`OQ-01` .. `OQ-16`):**
   - Review [`14-OPEN-QUESTIONS-AND-DECISION-POINTS.md`](./14-OPEN-QUESTIONS-AND-DECISION-POINTS.md) with the Founder/Coordinator first (especially `OQ-01` `Apache-2.0` boundary, `OQ-02` `v1.0.0` vs multi-wave horizon slicing, `OQ-04` Merkle `.tpkg` signing, `OQ-05` clean A2A `v1.0.1` replacement, `OQ-09` `cedar-go` ABAC, `OQ-12` `SYSTEM` operator separation, and `OQ-14` `--admin-listen` port separation).
2. **Step 2 — Promote / Prune / Wave-Assign `CAP-001` .. `CAP-161` into the Master Feature Catalog:**
   - Ingest [`data/preliminary-capability-inventory.json`](./data/preliminary-capability-inventory.json) directly.
   - Ensure **100% of `P0-Foundational` defect/debt/wiring closures** (`RR-001`, `ARCH-OBS-01`, `DEF-004`/`041`/`048`/`072`/`078`, `PG-001`..`PG-014`, Day-0 `< 90s` setup, `109 RPS` → pooled keep-alive + budget lease unlock) are scheduled ahead of or alongside net-new surface expansion so new capabilities are never built atop `O(N_orgs)` RLS loops or broken CLI/Console wiring.
3. **Step 3 — Non-Negotiable Invariants `TEEP-02A` Must Carry Forward:**
   - **Zero Consumer-Subscription ToS Arbitrage (`DEC-004`):** Reject any feature request to pool personal ChatGPT Plus, Claude Pro, or GitHub Copilot consumer sessions (`CAP-081`, `CAP-121`).
   - **Strict 4-Plane Separation (`DEC-003`):** Unify cross-plane causal lineage (`X-Tanpopo-Trace-Root`, `CAP-096`) without merging Provider, Plugin, MCP, and A2A wire schemas.
   - **`CGO_ENABLED=0` & Permissive Dependency Gate (`ADR-0001`, `CONSTRAINTS.md`):** Keep `wazero`, `cedar-go`, `cel-go`, `crewjam/saml`, `elimity-com/scim`, `go-spiffe/v2`, `go-tuf/v2`, and `oras-go/v2` `100%` pure Go and `Apache-2.0`/`MIT`/`BSD` clean (`0` `GPL-3.0` / `AGPL` / `ELv2`).
   - **Astryx Neutral, Air-Gap & 15-Locale Accessibility (`ADR-0007`):** Preserve `0` external runtime requests, Light/Dark parity, `WCAG 2.2 AA`, `<= 250 KB` gzip initial JS budget, and 15-language parity.
   - **Epistemic & Release Governance (`COND-PO-01`, `GATE-REL-07`):** Keep all unmeasured targets labeled `HYPOTHESIS / UNVERIFIED` until proven by benchmark harnesses, preserve every historical `DEF-*` / `PG-*` / `RR-*` item until verified in QA, and never tag or publish a release without explicit human Product Owner authorization.
