# CP13 — SSRF defaults and controlled MCP DNS rebinding

## Scope and result

QA-003 continues on frozen RC5 `3423d4f523ef0ac77469a1201b8b753d1d31c902`. This checkpoint uses an isolated clone runtime and synthetic network fixtures; Tanpopo implementation was not changed. `GATE-REL-07` remains **BLOCKED**. DEF-005 remains **PARTIAL**.

## Correction to CP11

CP11's prose says the fresh provider drawer began with private/loopback access unchecked. That is a QA-record error. The CP11 form JSON already recorded `defaultPrivate: true`, and the retained CP11 negative test explicitly unchecked the control before submitting. CP13 opened a fresh blank form and visually confirmed that the option is checked and `127.0.0.1/32` is prefilled. CP13 then left both untouched, entered a new connection name and `http://127.0.0.1:19200`, and submitted it. The UI returned HTTP 201; persistent readback showed `allow_private_ip: true`, endpoint `127.0.0.1:19200`, and state `CREATED_UNVERIFIED`. The connection row appeared in the Console. This confirms the actual default behavior; it does not by itself establish a product defect because no acceptance contract requiring a disabled default has been established. The earlier explicit-uncheck denial remains valid.

Evidence: [fresh form image](../evidence/CP13-ssrf-fresh-provider-form.png), [visible defaults](../evidence/CP13-ssrf-form-default.json), [ready-to-submit image](../evidence/CP13-ssrf-default-loopback-ready.png), [created connection and persistent readback](../evidence/CP13-ssrf-default-loopback-result.json), and [created row image](../evidence/CP13-ssrf-default-loopback-created.png). Screenshots were visually inspected.

Two exploratory attempts are retained as environment/configuration evidence, not product failures: an initial attempt through a plain-HTTP browser origin was rejected by Secure-cookie/CSRF validation; using the corrected HTTPS proxy fixed the request context. The Ollama adapter then rejected `rebind.qa003.test` with `disallowed_host` because that adapter's configured `allowed_hosts` excludes custom DNS names. Therefore provider-adapter DNS rebinding was not exercised through Ollama. The successful controlled rebinding test below uses MCP's supported endpoint configuration instead. See [CSRF attempt record](../evidence/CP13-ssrf-domain-create.json), [adapter rejection record](../evidence/CP13-ssrf-default-private-created.json), and visually inspected [CSRF response](../evidence/CP13-ssrf-domain-default-denied.png).

## Controlled DNS rebinding via MCP sync

An isolated custom DNS fixture returned `rebind.qa003.test → 172.29.0.5`; a synthetic Streamable HTTP fixture at that address recorded requests. A synthetic MCP server was registered in the clone with `allow_private_ip=true` and exactly `172.29.0.5/32` allowed. On sync, the fixture received three MCP POST requests and the API returned HTTP 200 with zero discovered tools. This establishes that the initial allowed address was reached, while the fixture's deliberately empty tool behavior means it does not establish successful tool discovery.

The DNS fixture then changed the same host to `127.0.0.1`, which was outside the configured allowlist. A second sync returned HTTP 502 `mcp_sync_failed`; the synthetic upstream's request count stayed at six (zero additional requests). The UI displayed a generic red “upstream MCP tool synchronization failed” notice; the row remained Active with zero tools. This is scoped evidence that this tested re-resolution/rebinding path did not send a request to the changed address. It does not prove safety against concurrent DNS races, mixed answers, IPv6, other transports, or other product paths. The generic UI error does not reveal whether the failure was an allowlist rejection or an outage; no defect is filed without an explicit diagnostic contract.

Evidence: [initial sync screenshot](../evidence/CP13-ssrf-rebind-allowed-sync.png), [failed second sync screenshot](../evidence/CP13-ssrf-rebind-denied-sync.png), and [request counts, DNS state, and HTTP responses](../evidence/CP13-ssrf-rebinding-result.json). Both screenshots were visually inspected. All addresses, tenant records, and fixtures are synthetic and isolated.

## DEF-005 and coverage disposition

The original form omission is not reproduced in the tested Chromium desktop MCP/A2A forms: exact CIDRs were accepted, wildcard CIDRs rejected, and CP12 confirmed `/32` persistence. CP13 adds a scoped provider-default observation and MCP DNS-rebinding test. Provider exact-literal allow/deny and redirect behavior remain as previously scoped. DEF-005 remains **PARTIAL**, not closed: mixed DNS answers, connection races/address pinning, broader public/private redirect transitions, A2A and provider DNS-rebinding parity, alternative transports, and mobile feedback remain unverified.

FT-11, FT-07, FT-08, SCR-10 and the corresponding security workflow remain **PARTIAL**. Chromium desktop 1440×1000 was used. No external provider spend occurred.

