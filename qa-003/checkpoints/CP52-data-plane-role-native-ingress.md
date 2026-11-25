# CP52 — Native ingress in the separated Data Plane role

**Baseline:** frozen RC5 source/binary commit `3423d4f523ef0ac77469a1201b8b753d1d31c902`
**Runtime:** fresh, isolated SQLite service `18117`; `--role data-plane`; generated test-only master key; no tenant/provider credentials
**Disposition:** confirms DEF-048 also affects the separated Data Plane role. `/v1/chat/completions` is mounted and returns a structured 401 without a key; documented `/native/*` paths return bare 404. `FT-02` remains **FAIL**. `GATE-REL-07` remains **BLOCKED**.

## Isolated role setup and controls

A fresh runtime was started from the exact RC5 binary using a new `/tmp` SQLite data directory and one randomly generated, local-only master key. The key was not printed or copied into the QA workspace. `/healthz` returned 200 and `/readyz` returned 200, confirming that the Data Plane role had started and was healthy.

The unauthenticated control `POST /v1/chat/completions` returned Tanpopo's structured JSON 401 `invalid_virtual_key` with an `X-Tanpopo-Request-Id`. This proves the normalized Data Plane route was mounted and handled by the gateway.

In the same ready service, `POST /native/ollama/api/chat` and `POST /native/anthropic/v1/messages` returned raw `404 page not found`, with no Tanpopo request ID. The sanitized [result matrix](../evidence/CP52-data-plane-role-native-results.json) records only status/headers/error metadata and confirms the test master key and credential material were not persisted to evidence. The isolated service was stopped after the read-only route checks; its local test directory remains under `/tmp`.

## Disposition and residual scope

This extends CP51's credentialed role-all 405 result: the documented native route is unreachable in both the combined role-all runtime and the separately started Data Plane role, though the failure responses differ by outer router. It is therefore not only a Console fallback issue. The role-all valid-key request still establishes the end-to-end user impact; the role-specific unauthenticated requests corroborate the separate role's mount behavior without claiming an auth test for native ingress.

The native request still has not reached policy, Virtual Key authorization, routing, SSRF protection or the mock provider. Exact native response/body fidelity, usage and trace behavior remain unverified. No product code was modified; no external network/provider traffic or spend occurred.
