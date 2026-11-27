# CP23 — Interrupted Go race suite rerun

**Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`  
**Source changes:** none; `git diff 40bba5b -- . ':!qa-002'` was empty.  
**Result:** `go test -race ./...` **PASS**, exit code 0.

## Toolchain recovery

The current `/usr/bin/go` is not a Go command; `go version` and `go test` return `Go: Unknown option: ...`. A Go 1.26.11 Linux/amd64 archive already existed in `/tmp`. Its SHA-256, `9849f4e04ee35303714e34fbc3ac8ab44ea3e448a95aa182f91e3b9b79c710ec`, matches the official Go download index. I invoked the extracted toolchain by absolute path.

The first direct run stopped before testing because Go's default build cache under `/home/agent/.cache/go-build` is read-only. The completed run used writable temporary `GOCACHE`, `GOMODCACHE`, and `GOPATH` under `/tmp`, with `GOFLAGS=-buildvcs=false` and `umask 0022`. Dependencies resolved from the configured network.

## Actual result

- 86 packages passed under the race detector.
- 22 packages had no test files.
- 0 failures or race reports.
- The sanitized summary and full-output log hash are in `evidence/CP23-race-suite.json`; the raw runner log remains outside version control at `/tmp/qa002-cp23-race.log`.

This is an automated suite result only. It does not establish product stability or close Alpha/Beta coverage. QA-002 continues, the RC4 DevBox artifact remains UNVERIFIED, and GATE-REL-07 remains **BLOCKED**.
