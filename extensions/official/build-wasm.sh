#!/usr/bin/env bash
# Copyright 2026 The Tanpopo Authors.
# Licensed under the Apache License, Version 2.0.

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
CLANG="${CLANG:-clang}"

build_adapter() {
  local name="$1"
  local src="${ROOT_DIR}/extensions/official/${name}/src/adapter.c"
  local out="${ROOT_DIR}/extensions/official/${name}/dist/adapter.wasm"
  mkdir -p "$(dirname "${out}")"
  "${CLANG}" --target=wasm32 -mcpu=mvp -nostdlib -ffreestanding -O2 -Wall -Wextra -Werror \
    -Wl,--no-entry -Wl,--strip-all -Wl,--initial-memory=262144 \
    -Wl,--max-memory=16777216 -Wl,-z,stack-size=65536 \
    "${src}" -o "${out}"
  sha256sum "${out}"
}

build_adapter "anthropic"
build_adapter "gemini"
