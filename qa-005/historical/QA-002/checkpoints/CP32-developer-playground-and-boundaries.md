# CP32 — Developer read boundaries and synthetic Playground inference

**Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`
**Runtime:** isolated SQLite Tanpopo at `127.0.0.1:18086`, Chromium, CP26 synthetic Ollama-compatible fixture
**Release gate:** `GATE-REL-07` remains **BLOCKED**.

## Results

- **Developer session — PASS for this synthetic role.** A separate browser authenticated as the CP30 synthetic HUMAN member with role exactly `DEVELOPER`. To test role behavior, a temporary password hash was provisioned out of band in the disposable DB; no plaintext was retained in evidence. This does not alter DEF-021's local enrollment finding.
- **Restricted member and Security views — PASS.** The Access & Keys → Members & Roles view showed the explicit message that this role can inspect keys but cannot list organization members. The authenticated `GET /admin/v1/members` returned 403. Security was absent from the sidebar; direct `/security/approvals` navigation showed “Access restricted” and the assigned-role explanation. Screenshots were visually inspected.
- **Allowed Playground journey — PASS for synthetic upstream response.** The Developer opened Playground, used the persisted route `qa002-cp22-route-repro`, sent one synthetic prompt, received HTTP 200 and “Synthetic inference completed.” The local fixture count increased from 3 to 4 requests; it received one streamed completion. The ephemeral session cap displayed `$0.500000` and the session remained on the page. The prompt is synthetic.
- **Cross-feature trace defect — DEF-017 independently recurred.** Playground rendered `req_01m85d6pnse00a3hbf35x18wtj`, then polled `/admin/v1/traces/{req_id}` five times and received 404. A persisted trace list row exists with ID `trc_01m85d6pnse00a3jb10ca2padf`, HTTP status 200, EXACT usage, and zero total cost at the configured synthetic pricing. FinOps summary reported six exact requests and 6 micro-USD total. The success card remained “Trace metadata pending…” in the visually inspected screenshot. The zero charge on this individual row is consistent with this fixture model's configured price; the summary aggregates prior rows as well.
- No new defect confirmed beyond DEF-017. Other Developer write boundaries, key/tool behavior, different models, cancellation, and full export reconciliation remain UNVERIFIED.

## Evidence

- `../evidence/CP32-developer-members-denied.png`
- `../evidence/CP32-developer-playground-before.png`
- `../evidence/CP32-developer-playground-success.png`
- `../evidence/CP32-developer-workflow.json`
- `../evidence/CP32-developer-persisted-trace.json`

## Limitations

No configured provider secret was used; all inference stayed on the deterministic loopback fixture. Credential setup was isolated to disposable test data. Source code remained unchanged.
