# Journey 2: Extension Authoring SDK

Tanpopo is **extension-first**: new AI providers, custom routing predicates, PII redaction pipelines, and specialized adapters compile cleanly outside Tanpopo Core using the `tanpopo plugin` CLI toolchain and reproducible `.tpkg` archives.

---

## 1. Execution Tiers

| Tier | Runtime | Use Case | Isolation & Policy |
| --- | --- | --- | --- |
| **Tier 1 (`declarative`)** | Host JSON/YAML pipeline | OpenAI-compatible HTTP dialects, custom auth headers, model rewrites | Zero arbitrary code execution; strict schema & allowed host egress list |
| **Tier 2 (`wasm`)** | Pure-Go `wazero` Wasm sandbox | Custom wire codecs, streaming frame translators, bespoke signing | Linear-memory slice ABI, strict memory/CPU caps (`<= 16 MiB`), `0` FS/env/socket authority, explicit HTTP egress allowlists, auto-quarantine after `> 20` traps in `60s` (`INC-03`) |

> **Contract Compliance (`DEC-004`):** Tanpopo ships adapters using documented HTTP/SSE/WebSocket/gRPC APIs only — zero undocumented private-session or residential-proxy scraping extensions.

---

## 2. Scaffold, Validate & Contract-Test Offline (`100%` Loopback)

```bash
# 1. Scaffold a Tier-1 declarative or Tier-2 Wasm extension
tanpopo plugin init my-provider --namespace acme --tier 1 --dir ./my-provider

# 2. Validate manifest.yaml schema, capability namespaces, egress bounds & artifacts
tanpopo plugin validate ./my-provider

# 3. Run offline cassette contract tests (loopback isolation, zero outbound Internet)
tanpopo plugin test ./my-provider

# Optional: execute one specific cassette
tanpopo plugin test ./my-provider --cassette ./my-provider/cassettes/chat-stream.json
```

### Reproducible `.tpkg` Packaging & Lockfile Pinning

Every `.tpkg` package builds bit-for-bit reproducibly so SHA-256 digests remain verifiable across CI and air-gapped sites:

```bash
tanpopo plugin pack ./my-provider -o ./dist/acme-my-provider.tpkg
```

Pinned installations are recorded in `tanpopo.lock` and validated declaratively with:

```bash
tanpopo config reconcile
```

---

## 3. N / N-1 ABI Compatibility & Core Upgrades

Before rolling out a newer Tanpopo Core binary or updating an extension manifest, verify ABI contract compatibility across your installed lockfile:

```bash
# Check extension manifest contractVersion against Core N / N-1 support window
tanpopo plugin migrate-check ./my-provider \
  --supported-contracts 1,2 \
  --target-core 1.0.0

# Preflight all extensions installed in an environment before upgrading Core
tanpopo upgrade preflight --target-version 1.0.0
```

See [docs/operations/developer-experience.md](../operations/developer-experience.md) and [proto/tanpopo/extension/v1/runtime.proto](../../proto/tanpopo/extension/v1/runtime.proto) for full protobuf wire contracts.
