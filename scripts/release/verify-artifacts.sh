#!/usr/bin/env bash
# Copyright 2026 The Tanpopo Authors.
# Licensed under the Apache License, Version 2.0.

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

require() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "required release verification tool is missing: $1" >&2
    exit 2
  fi
}

for tool in cosign cyclonedx-gomod cyclonedx-npm docker go govulncheck goreleaser openssl python3 trivy curl; do
  require "$tool"
done

if ! docker info >/dev/null 2>&1; then
  echo "Docker daemon is unavailable; artifact verification requires a local OCI runtime" >&2
  exit 2
fi

DIST_DIR="$ROOT_DIR/dist"
mkdir -p "$DIST_DIR"
TMP_DIR="$(mktemp -d)"
IMAGE="tanpopo:artifact-verification"
VOLUME="tanpopo-artifact-verification-$$"
CONTAINER=""
cleanup() {
  if [[ -n "$CONTAINER" ]]; then docker rm -f "$CONTAINER" >/dev/null 2>&1 || true; fi
  docker volume rm "$VOLUME" >/dev/null 2>&1 || true
  rm -rf "$TMP_DIR"
}
trap cleanup EXIT

echo "Building all supported release targets without publishing artifacts"
goreleaser build --snapshot --clean
goreleaser release --snapshot --clean --skip=publish
ARCHIVE_COUNT="$(find "$DIST_DIR" -maxdepth 1 -type f \( -name '*.tar.gz' -o -name '*.zip' \) | wc -l | tr -d ' ')"
if [[ "$ARCHIVE_COUNT" != 5 ]]; then
  echo "expected 5 cross-platform archives, found $ARCHIVE_COUNT" >&2
  find "$DIST_DIR" -maxdepth 1 -type f -print >&2
  exit 1
fi

echo "Generating CycloneDX SBOMs for Go and npm dependencies"
cyclonedx-gomod app -json -output "$DIST_DIR/sbom-go.cdx.json" -main cmd/tanpopo .
cyclonedx-npm --output-format JSON --output-file "$DIST_DIR/sbom-web.cdx.json" --ignore-npm-errors
test -s "$DIST_DIR/sbom-go.cdx.json"
test -s "$DIST_DIR/sbom-web.cdx.json"

echo "Checking reachable Go vulnerabilities"
govulncheck ./...

echo "Building hardened rootless OCI image"
docker build --pull --file deploy/docker/Dockerfile --tag "$IMAGE" .
docker image inspect "$IMAGE" --format '{{.Config.User}}' | grep -Fx '65532:65532'

echo "Scanning OCI image for Critical and High vulnerabilities"
trivy image --severity CRITICAL,HIGH --exit-code 1 "$IMAGE"

echo "Checking standalone Compose configuration"
install -m 0600 /dev/null "$TMP_DIR/master.key"
TANPOPO_MASTER_KEY_FILE="$TMP_DIR/master.key" docker compose \
  --file deploy/compose/docker-compose.standalone.yml config --quiet
echo "Starting rootless read-only container and checking readiness"
docker volume create "$VOLUME" >/dev/null
SMOKE_KEK="$(openssl rand -hex 32)"
STARTED_MS="$(python3 -c 'import time; print(int(time.monotonic() * 1000))')"
CONTAINER="$(docker run --detach --read-only --cap-drop=ALL \
  --security-opt=no-new-privileges:true \
  --tmpfs /tmp:rw,noexec,nosuid,size=16m,uid=65532,gid=65532 \
  --volume "$VOLUME:/var/lib/tanpopo" \
  --publish 127.0.0.1::8080 \
  --env TANPOPO_DATA_DIR=/var/lib/tanpopo \
  --env TANPOPO_MASTER_KEY="$SMOKE_KEK" \
  "$IMAGE")"
unset SMOKE_KEK
HOST_PORT="$(docker port "$CONTAINER" 8080/tcp | sed -E 's/.*:([0-9]+)$/\1/' | head -n1)"
if [[ ! "$HOST_PORT" =~ ^[0-9]+$ ]]; then
  echo "Docker did not publish a loopback smoke-test port" >&2
  exit 1
fi

ready=false
for _ in $(seq 1 60); do
  if curl --fail --silent "http://127.0.0.1:${HOST_PORT}/readyz" >/dev/null; then
    ready=true
    break
  fi
  sleep 1
done
if [[ "$ready" != true ]]; then
  docker logs "$CONTAINER" >&2
  echo "rootless container did not become ready within 60 seconds" >&2
  exit 1
fi
READY_MS="$(python3 -c 'import time; print(int(time.monotonic() * 1000))')"
STARTUP_MS="$((READY_MS - STARTED_MS))"
echo "Container cold start to /readyz: ${STARTUP_MS} ms (target < 3000 ms)"
if (( STARTUP_MS >= 3000 )); then
  echo "container cold-start target exceeded" >&2
  exit 1
fi
curl --fail --silent "http://127.0.0.1:${HOST_PORT}/healthz" >/dev/null

RSS_BYTES="$(docker stats --no-stream --format '{{.MemUsage}}' "$CONTAINER" | python3 -c '
import re, sys
value = sys.stdin.read().split(" / ", 1)[0].strip()
match = re.fullmatch(r"([0-9.]+)([KMG]?i?B)", value)
if not match:
    raise SystemExit(f"cannot parse Docker memory usage: {value!r}")
factor = {"B": 1, "KB": 1000, "MB": 1000**2, "GB": 1000**3, "KiB": 1024, "MiB": 1024**2, "GiB": 1024**3}[match.group(2)]
print(int(float(match.group(1)) * factor))
')"
echo "Container idle RSS: ${RSS_BYTES} bytes (target < 134217728 bytes)"
if (( RSS_BYTES >= 134217728 )); then
  echo "container idle RSS target exceeded" >&2
  exit 1
fi

if docker run --rm --entrypoint /bin/sh "$IMAGE" -c true >/dev/null 2>&1; then
  echo "production image unexpectedly contains a shell" >&2
  exit 1
fi

echo "Signing SBOMs with an ephemeral verification-only Cosign key"
export COSIGN_PASSWORD=""
cosign generate-key-pair --output-key-prefix "$TMP_DIR/cosign"
for sbom in "$DIST_DIR/sbom-go.cdx.json" "$DIST_DIR/sbom-web.cdx.json"; do
  signature="$TMP_DIR/$(basename "$sbom").sig"
  cosign sign-blob --yes --key "$TMP_DIR/cosign.key" \
    --output-signature "$signature" "$sbom"
  cosign verify-blob --key "$TMP_DIR/cosign.pub" \
    --signature "$signature" "$sbom"
done
test -s "$DIST_DIR/SHA256SUMS"
cosign sign-blob --yes --key "$TMP_DIR/cosign.key" \
  --output-signature "$TMP_DIR/SHA256SUMS.sig" "$DIST_DIR/SHA256SUMS"
cosign verify-blob --key "$TMP_DIR/cosign.pub" \
  --signature "$TMP_DIR/SHA256SUMS.sig" "$DIST_DIR/SHA256SUMS"

echo "Artifact verification completed. No artifacts were published or tagged."
