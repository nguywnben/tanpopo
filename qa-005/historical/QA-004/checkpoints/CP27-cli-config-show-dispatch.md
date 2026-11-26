# CP27 — CLI `config show` dispatch

**Historical finding:** DEF-004, Moderate. **QA-004 outcome:** PASS for command recognition/dispatch only; authenticated instance-overview output remains UNVERIFIED. GATE-REL-07 remains BLOCKED.

## Reproduction

Using the exact Linux amd64 executable from verified RC6 workflow artifact 45581179274 (binary SHA-256 `9a62f8844a85aa1fd3fd9be22365b04e9ce655a470fbe9de670e19678644d34b`, embedded commit `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`), run:

```text
tanpopo config show --help
tanpopo config show --output json
```

`--help` reached the `Show the instance overview` command and listed the `--output` format flag. The second command without an Admin API token exited 14 with `TANPOPO_ADMIN_TOKEN is required for Admin API access. Export a valid Admin token and retry.` This is the expected authentication prerequisite from the executed command handler, not an `unknown command`/duplicate-command failure. `tanpopo config --help` also listed `show` as a child command.

Sanitized executable provenance and exact stdout/stderr/exit status are in `evidence/CP27-cli-config-show.json`. No Tanpopo source code or configuration was modified.

## Disposition and limits

The historical command-shadowing failure did not reproduce: RC6 dispatches `config show` correctly. This is a scoped PASS for command registration and help/dispatch. A valid Admin API token was not available to this CLI invocation, so the returned instance overview, JSON/YAML output correctness, error/recovery behavior against live control-plane endpoints, and other CLI commands remain UNVERIFIED.
