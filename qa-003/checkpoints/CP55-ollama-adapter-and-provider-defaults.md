# CP55 — Ollama adapter activation and provider defaults

**Baseline:** RC5 source `3423d4f523ef0ac77469a1201b8b753d1d31c902`
**Runtime:** fresh isolated SQLite `--role all`, `127.0.0.1:18119`; Chromium 156/Linux.
**Scope:** DEF-013 retest; Marketplace → adapter approval → connection creation and persistence.
**Status:** one scoped default path passed; QA-003 remains in progress and `GATE-REL-07` BLOCKED.

## Journey and results

1. Bootstrapped a new synthetic owner/organization on a fresh local SQLite database with no enabled provider adapters. Owner credentials, bootstrap token, master key, and database remain in `/tmp` only.
2. Opened Extensions → Marketplace. The catalog contained eight packages and showed the Ollama Local & Self-Hosted Adapter with `Origin=Official`, `Publisher signature=None`, and `Tier=declarative`. The catalog entry describes `SELF_HOSTED_INFRA` billing and `NONE | BEARER` auth.
3. Staged `@tanpopo/ollama` through the real UI. Permission review showed Origin trust `Official` but Publisher signature `Unsigned`; requested private-IP access was `Enabled`, TLS requirement `Disabled`, body inspection `Disabled`, and declarative execution. The synthetic owner explicitly checked consent and selected **Approve & activate**. The Installed tab then showed the adapter Active, Official, Unsigned, declarative.
4. Opened Providers → Connections → New connection. With `@tanpopo/ollama` selected, the form defaulted to **Self-hosted infra**, **On-prem**, **No credential**, and private/loopback access checked with `127.0.0.1/32`; endpoint and name were empty. It did not default to commercial API billing or a US region.
5. Created a synthetic, non-probed loopback connection at `http://127.0.0.1:19272` with no secret. The UI returned HTTP 201 and showed `Unverified`, `ON_PREM · Self-hosted`, and `None bound`. A fresh authenticated GET `/admin/v1/connections?limit=100` returned HTTP 200 with the same connection, `billing_mode=SELF_HOSTED_INFRA`, `data_region=ON_PREM`, `allow_private_ip=true`, and `state=CREATED_UNVERIFIED`. No probe or provider egress was attempted.

## DEF-013 disposition

**VERIFIED FIXED for the reported Ollama billing/region default.** The new-connection form and persisted readback both use self-hosted/on-prem settings, so the historical commercial API/US default was not reproduced. Other adapter defaults, credential handling, full provider recovery and probe behavior remain open; this does not close broader FT-03.

## Additional observations

- The synthetic Official Ollama package is labeled `Unsigned` in Marketplace permission review and Installed UI. This is consistent with the previously recorded DEF-043 provenance-label discrepancy and independently reconfirms those displayed values; no signature bypass or exploit was attempted.
- Private-IP permission is visible in the consent table and the connection form requires an explicit non-wildcard CIDR. This run did not weaken SSRF controls; it used the explicitly allowed loopback `/32` and made no request to it.
- Visually inspected screenshots: [fresh bootstrap](../evidence/CP55-bootstrap-start.png), [empty Installed page](../evidence/CP55-extensions-initial.png), [Marketplace](../evidence/CP55-marketplace-initial.png), [permission review](../evidence/CP55-ollama-stage-review.png), [consented permissions](../evidence/CP55-ollama-consent.png), [activated adapter](../evidence/CP55-ollama-activated.png), [active adapter list](../evidence/CP55-adapters-active.png), [default form](../evidence/CP55-new-connection-defaults.png), [created connection](../evidence/CP55-ollama-connection-created.png), and [persisted after navigation/reload](../evidence/CP55-ollama-persisted-refresh.png). Sanitized state: [result JSON](../evidence/CP55-ollama-flow-result.json).

## Limits

Adapter update, secret rotation, probe failure/recovery, healthy deployment, inference, and non-Ollama billing/region defaults were not exercised in this checkpoint. No Tanpopo implementation was modified and no real provider credentials were used.
