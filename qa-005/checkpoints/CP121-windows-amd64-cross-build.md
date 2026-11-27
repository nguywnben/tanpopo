# CP121 — Windows amd64 cross-build

**Source:** frozen RC7 `737079ddb847d97dbcf832f610ed0048556cb65b`.
**Status:** Windows compile PASS (scoped); Windows runtime UNVERIFIED/BLOCKED because this QA host is Linux/amd64 and no Windows execution host is attached.

Used the cached Go 1.26.11 toolchain at `/tmp/qa005-go-root/go`, locked with `GOTOOLCHAIN=local`, cached modules at `/tmp/qa-gopath/pkg/mod`, and an ephemeral RAM Go build cache/temp directory. Ran:

`GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -o /dev/shm/tanpopo-rc7-windows-amd64.exe ./cmd/tanpopo`

The cross-build completed successfully. `file` identified a PE32+ x86-64 Windows console executable. Artifact SHA-256: `47a4ff222a878c2cc3858c6339e54936af86665612b06bfb4798e3ab7efe96db`.

This detects compile-time target incompatibilities only. No Windows process, filesystem, service shutdown, installer, browser, database driver, networking, or recovery behavior was executed. The Windows binary was held only in ephemeral `/dev/shm`, not committed. The overlay remained full, so `TMPDIR` and `GOCACHE` were redirected to RAM; no dependency or evidence cleanup was performed.
