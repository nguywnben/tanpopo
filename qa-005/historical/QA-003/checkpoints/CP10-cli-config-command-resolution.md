# CP10 — CLI config command resolution and safe reconcile abort

**Baseline:** frozen RC5 `3423d4f523ef0ac77469a1201b8b753d1d31c902`; exact-source binary `/tmp/tanpopo-qa003`.
**Status:** DEF-004 verified fixed for command resolution, fixture-backed config show, and missing-config fail-closed behavior. Live authenticated CLI access and successful configuration reconcile remain unverified. `GATE-REL-07` remains **BLOCKED**.

## Procedure

1. Ran `tanpopo config --help`; it exited 0 and lists both `show` and `reconcile` under the same command group.
2. Started an isolated HTTP fixture on `127.0.0.1:19201` that serves only a synthetic JSON body at `GET /admin/v1/overview/summary`. Ran `tanpopo --output json config show` against it with a synthetic, fixture-only token. The command exited 0 and emitted the synthetic summary (`total_requests=7`, exact settled 1234 micro-USD, zero estimated/holds/unmetered). No Tanpopo service or real credential was contacted.
3. Ran `config reconcile` with a nonexistent desired YAML file and temp applied/lockfile paths. It exited 14 with the generic configuration-rejected message. Neither output file was created.

Machine-readable command output and file-existence checks are in `../evidence/CP10-cli-config.json`. No source or dependency file was changed.

## DEF-004 disposition

**VERIFIED FIXED for the tested RC5 command-resolution slice.** Both commands resolve correctly, `config show` reaches the expected endpoint and renders its response, and missing desired configuration fails closed without creating artifacts. A real Admin API credential/session, valid reconcile, malformed/permission-invalid files, and applied-state recovery remain unverified.

## Coverage update

- **PARTIAL:** FT-15 Console/CLI/Admin API/Data Plane surface; WF-14 GitOps reconcile and safe abort.
- No new defect confirmed. Other CLI commands and platform-specific shell behavior remain open. `GATE-REL-07` remains **BLOCKED**.
