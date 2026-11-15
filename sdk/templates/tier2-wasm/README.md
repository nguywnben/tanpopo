# Starter Wasm Provider Adapter (Tier-2 Sandboxed Wasm)

This starter template compiles to a pure WebAssembly 1.0 binary (`dist/adapter.wasm`) executing under Tanpopo's Zero-Ambient-Authority `wazero` sandbox (`<= 16 MiB` linear memory, `0` WASI/socket/filesystem imports).

## Files

- `manifest.yaml`: Extension manifest declaring `runtime.tier: sandboxed_wasm`, `runtime.entrypoint: dist/adapter.wasm`, its verified `sha256` digest, strict `networkEgress.allowedHosts`, and secret slots (`host_blind_inject` only).
- `src/adapter.wat`: Auditable WebAssembly 1.0 source contract using the POC-01 / `canonical_envelope` Linear-Memory Slice ABI `(ptr: i32, len: i32) -> i64`.
- `dist/adapter.wasm`: Pre-assembled WebAssembly 1.0 binary so `tanpopo plugin validate`, `tanpopo plugin test`, and `tanpopo plugin pack` execute offline in `< 1s` without requiring external compilers.
- `cassettes/*.yaml`: Offline deterministic contract cassettes (`chat_sync`, `chat_stream`, `tool_calls`, `error_401`).

## Quick Workflow

```bash
tanpopo plugin validate
tanpopo plugin test
tanpopo plugin pack
```
