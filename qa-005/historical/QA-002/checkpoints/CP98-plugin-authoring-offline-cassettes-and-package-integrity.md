# CP98 — Plugin authoring, offline cassettes, and package integrity

**Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`  
**CLI:** local QA build with explicit source metadata; Go 1.26.11  
**Test artifacts:** isolated under `/tmp/qa002-cp98-*`; no extension was installed into a Tanpopo runtime

## Work performed

Used `tanpopo plugin init` to create fresh Tier-1 declarative and Tier-2 Wasm extension projects. Ran `validate` and offline `test` on both. Each scaffold passed validation and four cassette cases: sync chat, split-UTF8 stream, 401/no-secret-echo normalization, and tool-call normalization. Both tiers packaged; two successive `.tpkg` outputs per tier were byte-identical.

Additional isolated checks:

- Compatible `contractVersion=0.1` with supported contract `0.1` and target core `0.2.0` returned COMPATIBLE/exit 0.
- The same package against supported contract `0.2` returned INCOMPATIBLE/exit 22 with `CONTRACT_INCOMPATIBLE`.
- Changing the declarative package's allowed egress host to `*` was rejected at validation (exit 14, wildcard policy message); no artifact was installed or network call made.
- Flipping a byte in a copy of the Wasm artifact without updating its pinned digest was rejected at validation (exit 22, sha256 mismatch).

Evidence is [CP98-plugin-authoring-results.json](../evidence/CP98-plugin-authoring-results.json). All test files, package archives, and corrupted copies remain outside the repository under `/tmp`.

## Results and limits

| Area | Result | Limits |
| --- | --- | --- |
| Tier-1/Tier-2 CLI scaffold and schema validation | PASS (scoped) | Starter templates only. |
| Offline cassette harness | PASS for four cases per tier | Does not prove live third-party compatibility or complete sandbox isolation. |
| `.tpkg` reproducibility | PASS for both tiers | Repeated local build only; no cross-host/OS reproducibility test. |
| Wasm artifact digest guard | PASS for one changed-byte negative case | Signature/trust root, malformed Wasm corpus, traps/quarantine and runtime resource limits remain untested. |
| Egress wildcard rejection | PASS for literal `*` negative | DNS rebinding, redirects, private addresses, allowlist bypass, and live host broker remain covered only by prior SSRF records and open gaps. No SSRF boundary was weakened. |
| End-to-end installation/permission consent/runtime isolation | UNVERIFIED | No package was installed into the persistent QA runtime. |

## Consolidated defect/status summary

- **Historical DEF-001–015:** CP98 did not retest them. Preserve limited scoped QA-002 results: DEF-001–004, DEF-006, DEF-008–015 have passing paths; DEF-005 remains partial; DEF-007 successful audit paths do not close failure atomicity/populated erasure. Full acceptance remains incomplete; no QA-001 PASS is inherited.
- **Historical open DEF-016–035:** remain open; no disposition changed.
- **New defects:** none.
- **Coverage completed:** Tier-1 and Tier-2 CLI scaffold/validation/cassettes/package, repeatable archive hashes, compatible/incompatible migration checks, explicit wildcard-egress denial, and corrupted-Wasm digest denial.
- **Coverage blocked:** none specific to this phase. Native ingress and remote CLI remain blocked as documented in CP96/97.
- **Coverage remaining:** real package installation, permission-diff and consent, adapter runtime invocation, blind secret broker, host restriction during execution, Wasm trap/CPU/memory/fuel, quarantine, rollback, external package trust and platform parity; all earlier incomplete product/defect coverage remains open.
- **Recommended handoffs:** no new engineering handoff. Preserve DEF-035/023/024/029/030, DEF-034/033, DEF-005 and DEF-007 priorities.
- **Release gate:** `GATE-REL-07` remains **BLOCKED**.
