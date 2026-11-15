# `@tanpopo/gemini` Official Provider Adapter

Tier-2 sandboxed WebAssembly 1.0 provider adapter for Google Gemini (`https://generativelanguage.googleapis.com`).

## Capabilities & Security Invariants

- Bidirectional OpenAI `/v1/chat/completions` ↔ Gemini `:generateContent` / `:streamGenerateContent?alt=sse` translation (`contents`, `systemInstruction`, `functionDeclarations`/`functionCall`/`functionResponse`, inline image/PDF `inlineData`, `blob_ref://` `fileData` splicing).
- **Zero Query-String Secrets (`NEG-2`):** Credentials are strictly blind-injected via HTTP header `x-goog-api-key`; outbound URLs never contain `?key=`.
- 5-component usage accounting (`promptTokenCount`, `candidatesTokenCount`, `cachedContentTokenCount`, `thoughtsTokenCount`).
- `/native/gemini/v1beta/models/*` passthrough inspection.

## Build Reproducibility

```bash
./extensions/official/build-wasm.sh
# Equivalent direct clang invocation:
clang --target=wasm32 -mcpu=mvp -nostdlib -ffreestanding -O2 -Wall -Wextra -Werror \
  -Wl,--no-entry -Wl,--strip-all -Wl,--initial-memory=262144 \
  -Wl,--max-memory=16777216 -Wl,-z,stack-size=65536 \
  extensions/official/gemini/src/adapter.c \
  -o extensions/official/gemini/dist/adapter.wasm
```

## Contract Verification

```bash
TANPOPO_TEST_HERMETIC=1 CGO_ENABLED=0 go run ./cmd/tanpopo plugin test extensions/official/gemini
```
