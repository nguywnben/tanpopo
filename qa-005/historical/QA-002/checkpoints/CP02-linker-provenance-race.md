# CP02 — Release linker provenance and full race suite

**Date:** 2026-11-24 (Asia/Saigon).
**Source baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`.
**Result:** Linker configuration and injection behavior **PASS**; RC4 DevBox artifact **UNVERIFIED**; Go race suite **PASS**.

## Version/build provenance

- `cmd/tanpopo/main.go` documents `version`, `commit`, and `date` as linker-populated build metadata. Its source defaults are `0.1.0-dev`, `none`, and `unknown`.
- `.goreleaser.yaml` uses `-X main.version={{ .Version }} -X main.commit={{ .Commit }} -X main.date={{ .Date }}`. This verifies how official release builds are intended to report version/commit/date.
- The local source-baseline build used the exact commit and date and reported JSON version `0.1.0-dev`, the exact commit SHA, and date `2026-11-24`. The source default is expected in a non-release source build and is not filed as a defect.
- A separate probe build with version label `qa-provenance-probe` reported the injected label, exact commit, and build date via `--version --output json`. This verifies the flags are effective; it does **not** establish that this is the RC4 release binary.
- `.goreleaser.yaml` defines five supported archive targets and SHA-256 checksums. The repository has no git tags and the public Releases API returned zero releases. The user located RC4 in a separate Hydrogen DevBox; no DevBox integration is exposed in this task, so exact RC4 artifact bytes, metadata, checksum/signature, and source provenance remain **UNVERIFIED**. Testing proceeds against the requested source baseline only.

## Interrupted race suite — actual rerun

Executed the complete repository command with Go 1.26.11, CGO enabled, and the worktree/permission settings below:

```sh
umask 0022
PATH=/tmp/tanpopo-qa002-go/bin:$PATH
GOPATH=/tmp/tanpopo-qa002-gopath
GOCACHE=/tmp/qa002-gocache
GOTOOLCHAIN=local
CGO_ENABLED=1
GOFLAGS=-buildvcs=false
go test -race ./...
```

Result: exit `0`; **86 packages passed**, 0 failures. The first invocation from the earlier interrupted turn continued in the background after its tool session was canceled; when found, it was stopped to avoid a duplicate competing run. The current attributable rerun completed successfully and its final summary is retained in `evidence/CP02-race-suite-summary.json`. No product files were changed.

## Environment flags and interpretation

- `GOFLAGS=-buildvcs=false` avoids a Go VCS status query error from this linked worktree. The build’s commit value was explicitly set to the verified baseline SHA.
- `umask 0022` allows the vault negative test to create an actual 0644 fixture; without it the ambient restrictive umask turned the fixture into 0600 and that one test could not exercise its intended condition.
- The prior uncorrected full suite result is preserved in the CP00 tool summary as an environment/setup failure, not counted as a product failure. The corrected full suite and race suite both pass.
