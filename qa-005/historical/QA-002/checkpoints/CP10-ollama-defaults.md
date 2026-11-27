# CP10 — Ollama billing/region defaults and local connection probe

**Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`  
**Environment:** Isolated SQLite runtime `127.0.0.1:18084`; authenticated Org Owner/Operator Console; bundled official `@tanpopo/ollama` package; disposable HTTP fixture at loopback port 19106. No Tanpopo source changed.

## DEF-013 reproduction and result

The fresh test tenant initially had no active provider adapters. Through Extensions → Marketplace, staged the official Ollama package, reviewed its permission delta (including loopback hosts, `api_key` slot, provider hooks, and private IP access), explicitly acknowledged the review, and activated it. Installed screen showed active 0.1.0. Evidence: `../evidence/CP10-extension-marketplace.png`, `CP10-ollama-permission-review.png`, `CP10-ollama-review-bottom.png`, `CP10-ollama-activated.png`. Screenshots were visually inspected.

Opened Providers → Adapters → New connection. With Ollama selected and untouched, the real UI showed `Self-hosted infra`, `On-prem`, “Allow private or loopback upstream” enabled, and `127.0.0.1/32`; no credential was bound. This matches the bundled manifest's declared SELF_HOSTED_INFRA default. Screenshot `../evidence/CP10-ollama-defaults.png` was visually inspected.

Created `QA002 Ollama Synthetic` at `http://127.0.0.1:19106` through the Console. The actual connections screen showed ON_PREM · Self-hosted and initially Unverified. Read-only SQLite confirmed `billing_mode=SELF_HOSTED_INFRA`, `data_region=ON_PREM`, `allow_private_ip=1`, `allowed_cidrs_json=["127.0.0.1/32"]`, `auth_mode=NONE`, and active persisted state after probe. The isolated fixture returned synthetic Ollama model metadata. Console “Probe connection” returned success in 9 ms and state changed to Active. Evidence screenshots: `CP10-ollama-connection-persisted.png`, `CP10-ollama-connection-row.png`, `CP10-ollama-probe.png`; sanitized state in `../evidence/CP10-ollama-defaults.json`.

**Result: DEF-013 VERIFIED FIXED (scoped).** Commercial API / US defaults did not reproduce. No real model inference, model/deployment lifecycle, alternate local adapter, secret-backed auth, or role boundary was tested. The synthetic fixture demonstrates probe and state persistence, not production Ollama compatibility.

GATE-REL-07 remains **BLOCKED**; QA-002 continues.
