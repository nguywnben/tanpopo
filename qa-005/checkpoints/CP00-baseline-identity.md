# CP00 — QA-005 frozen source and compiled binary identity

**Mission:** TEEP-00 baseline stabilization; not release certification.  
**Source:** `737079ddb847d97dbcf832f610ed0048556cb65b`.  
**QA branch:** `qa/qa-005-baseline-stabilization`.

## Results

- **PASS — source identity:** Fresh clone of `nguywnben/tanpopo` was made into `/workspace/tanpopo-qa-005`; the QA branch was created at the exact requested RC7 commit. The working tree was clean. Read-only remote refs showed `main` and `develop` at the same SHA. Existing QA refs remained at the recorded hashes; no historical branch was changed.
- **PASS — toolchain provenance:** `/usr/bin/go` is not a Go language toolchain; it returned `Go: Unknown option: --version`. Downloaded Go 1.26.11 from the official `go.dev` archive and verified SHA-256 `9849f4e04ee35303714e34fbc3ac8ab44ea3e448a95aa182f91e3b9b79c710ec`. Module resolution succeeded without changing `go.mod` or `go.sum`.
- **PASS — source build provenance:** Built the actual RC7 source using `go build -trimpath -o /tmp/qa005-bin/tanpopo ./cmd/tanpopo`. `go version -m` reports `vcs.revision=737079ddb847d97dbcf832f610ed0048556cb65b`, `vcs.modified=false`, Linux/amd64 and Go 1.26.11. Binary SHA-256: `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`.
- **BLOCKED — production signature integration:** Frozen source has an empty `extensions/official/release-signatures.json` signature array. The owner manifest ref exists at the specified SHA. Per instruction, the production signing integration was not attempted; no signing branch checkout, signature change, key access or re-signing request was made.
- **UNVERIFIED — official RC7 CI artifact:** CP00 establishes only a local, exact-source test build. A published CI artifact has not yet been independently fetched and matched.
- **BLOCKED — paid provider interoperability:** Environment status reports provider secret bindings as unknown and no secret bindings configured; paid calls are not authorized. QA-005 will use deterministic local fixtures.

## Limits

The CLI build default reports `0.1.0-dev`, commit `none`, build date `unknown`, because this QA binary was built without release linker flags. The Go binary metadata independently records the exact VCS revision/time and clean state. Do not represent this binary as an official release artifact, signed artifact, or production build.

Sanitized build metadata is [CP00 JSON evidence](../evidence/CP00-build-provenance.json). Historical QA-001–004 records and files were copied byte-for-byte into `qa-005/historical/`; QA-005 dispositions for all 79 defects remain UNVERIFIED at campaign intake. No product source, specification, signing branch, tag or release was changed. `GATE-REL-07` remains BLOCKED.
