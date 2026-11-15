# `@tanpopo/anthropic` Official Provider Adapter

Tier-2 sandboxed WebAssembly 1.0 provider adapter for Anthropic (`https://api.anthropic.com`).

## Capabilities

- Bidirectional OpenAI `/v1/chat/completions` ↔ Anthropic `/v1/messages` translation (`system`/`developer` extraction, `tool_use`/`tool_result`, `image` and PDF `document` `blob_ref://` splicing).
- Stateful SSE stream normalization (`message_start`, `content_block_start`, `content_block_delta`, `message_delta`, `message_stop`) with multi-byte UTF-8 boundary preservation and 5-component usage accounting (`input_tokens`, `output_tokens`, `cache_read_tokens`, `cache_write_tokens`).
- `/native/anthropic/v1/messages` passthrough inspection.
- Blind host-injected `x-api-key` credential slot (`secret_slot: api_key`); zero plaintext secrets in Wasm linear memory and zero ambient WASI/FS/socket imports.

## Build Reproducibility

```bash
./extensions/official/build-wasm.sh
# Equivalent direct clang invocation:
clang --target=wasm32 -mcpu=mvp -nostdlib -ffreestanding -O2 -Wall -Wextra -Werror \
  -Wl,--no-entry -Wl,--strip-all -Wl,--initial-memory=262144 \
  -Wl,--max-memory=16777216 -Wl,-z,stack-size=65536 \
  extensions/official/anthropic/src/adapter.c \
  -o extensions/official/anthropic/dist/adapter.wasm
```

## Contract Verification

```bash
TANPOPO_TEST_HERMETIC=1 CGO_ENABLED=0 go run ./cmd/tanpopo plugin test extensions/official/anthropic
```
