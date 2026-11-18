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

for tool in cosign cyclonedx-gomod cyclonedx-npm docker go govulncheck goreleaser npm openssl python3 trivy curl; do
  require "$tool"
done

if ! docker info >/dev/null 2>&1; then
  echo "Docker daemon is unavailable; artifact verification requires a local OCI runtime" >&2
  exit 2
fi

DIST_DIR="$ROOT_DIR/dist"
mkdir -p "$DIST_DIR"
TMP_DIR="$(mktemp -d)"
VERIFY_STARTED_ON="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
export VERIFY_STARTED_ON
IMAGE="tanpopo:artifact-verification"
CONTAINER=""
COMPOSE_PROJECT=""
cleanup() {
  if [[ -n "$CONTAINER" ]]; then docker rm -f "$CONTAINER" >/dev/null 2>&1 || true; fi
  if [[ -n "$COMPOSE_PROJECT" ]]; then
    TANPOPO_MASTER_KEY_FILE="$TMP_DIR/master.key" docker compose \
      --project-name "$COMPOSE_PROJECT" \
      --file deploy/compose/docker-compose.standalone.yml \
      --file "$TMP_DIR/compose.override.yml" down --volumes --remove-orphans >/dev/null 2>&1 || true
  fi
  if [[ -d "$TMP_DIR/webui-dist" ]]; then
    rm -rf "$ROOT_DIR/internal/webui/dist"
    mv "$TMP_DIR/webui-dist" "$ROOT_DIR/internal/webui/dist"
  fi
  rm -rf "$TMP_DIR"
}
trap cleanup EXIT

echo "Building the Web Console from its locked npm dependency graph"
npm --prefix web/console run build
cp -a "$ROOT_DIR/internal/webui/dist" "$TMP_DIR/webui-dist"
rm -rf "$ROOT_DIR/internal/webui/dist"
mkdir -p "$ROOT_DIR/internal/webui/dist"
cp -a "$ROOT_DIR/web/console/dist/." "$ROOT_DIR/internal/webui/dist/"

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
cyclonedx-npm --output-format JSON --output-file "$DIST_DIR/sbom-web.cdx.json" web/console/package.json
test -s "$DIST_DIR/sbom-go.cdx.json"
test -s "$DIST_DIR/sbom-web.cdx.json"
node -e 'const b=JSON.parse(require("fs").readFileSync(process.argv[1],"utf8"));if(!Array.isArray(b.components)||b.components.length===0)process.exit(1)' "$DIST_DIR/sbom-web.cdx.json"

echo "Checking reachable Go vulnerabilities"
govulncheck ./...

echo "Running the live EXT-ACCEPT-01 production-binary suite"
TANPOPO_TEST_HERMETIC=1 go test -count=1 ./tests/integration -run '^TestEXTAccept01_'

echo "Building hardened rootless OCI image"
docker build --pull --file deploy/docker/Dockerfile --tag "$IMAGE" .
docker image inspect "$IMAGE" --format '{{.Config.User}}' | grep -Fx '65532:65532'

echo "Scanning OCI image for Critical and High vulnerabilities"
trivy image --severity CRITICAL,HIGH --exit-code 1 "$IMAGE"

echo "Checking standalone Compose configuration"
openssl rand -hex 32 > "$TMP_DIR/master.key"
chmod 0400 "$TMP_DIR/master.key"
if [[ "$(stat -c '%u' "$TMP_DIR/master.key")" != 65532 ]]; then
  if [[ "$(id -u)" == 0 ]]; then
    chown 65532:65532 "$TMP_DIR/master.key"
  elif command -v sudo >/dev/null 2>&1; then
    sudo chown 65532:65532 "$TMP_DIR/master.key"
  else
    echo "Compose secret verification needs sudo to map the key owner to UID 65532" >&2
    exit 2
  fi
fi
TANPOPO_MASTER_KEY_FILE="$TMP_DIR/master.key" docker compose \
  --file deploy/compose/docker-compose.standalone.yml config --quiet
cat > "$TMP_DIR/compose.override.yml" <<EOF
services:
  tanpopo:
    image: $IMAGE
    ports:
      - !override "127.0.0.1::8080"
EOF
echo "Starting the hardened standalone Compose service and checking secret access"
COMPOSE_PROJECT="tanpopo-artifact-verification-$$"
STARTED_MS="$(python3 -c 'import time; print(int(time.monotonic() * 1000))')"
TANPOPO_MASTER_KEY_FILE="$TMP_DIR/master.key" docker compose \
  --project-name "$COMPOSE_PROJECT" \
  --file deploy/compose/docker-compose.standalone.yml \
  --file "$TMP_DIR/compose.override.yml" up --detach
CONTAINER="$(docker compose --project-name "$COMPOSE_PROJECT" \
  --file deploy/compose/docker-compose.standalone.yml \
  --file "$TMP_DIR/compose.override.yml" ps --quiet tanpopo)"
HOST_PORT="$(docker compose --project-name "$COMPOSE_PROJECT" \
  --file deploy/compose/docker-compose.standalone.yml \
  --file "$TMP_DIR/compose.override.yml" port tanpopo 8080 | sed -E 's/.*:([0-9]+)$/\1/' | head -n1)"
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
CONSOLE_HTML="$(curl --fail --silent "http://127.0.0.1:${HOST_PORT}/")"
if [[ "$CONSOLE_HTML" != *"/assets/"* ]]; then
  echo "rootless image did not serve the built Web Console bundle" >&2
  exit 1
fi

CONTAINER_PID="$(docker inspect --format '{{.State.Pid}}' "$CONTAINER")"
RSS_BYTES="$(awk '/^VmRSS:/ {print $2 * 1024}' "/proc/${CONTAINER_PID}/status")"
echo "Tanpopo process idle RSS: ${RSS_BYTES} bytes (target < 134217728 bytes)"
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
    --tlog-upload=false \
    --output-signature "$signature" "$sbom"
  cosign verify-blob --private-infrastructure --key "$TMP_DIR/cosign.pub" \
    --signature "$signature" "$sbom"
done
test -s "$DIST_DIR/SHA256SUMS"
cosign sign-blob --yes --key "$TMP_DIR/cosign.key" \
  --tlog-upload=false \
  --output-signature "$TMP_DIR/SHA256SUMS.sig" "$DIST_DIR/SHA256SUMS"
cosign verify-blob --private-infrastructure --key "$TMP_DIR/cosign.pub" \
  --signature "$TMP_DIR/SHA256SUMS.sig" "$DIST_DIR/SHA256SUMS"

echo "Generating and locally verifying SLSA provenance for SHA256SUMS"
python3 - "$TMP_DIR/slsa-provenance.json" <<'PY'
import datetime, json, os, subprocess, sys
sha = os.environ.get("GITHUB_SHA") or subprocess.check_output(["git", "rev-parse", "HEAD"], text=True).strip()
repo = os.environ.get("GITHUB_REPOSITORY", "unknown/unknown")
workflow = os.environ.get("GITHUB_WORKFLOW", "local-release-artifact-dry-run")
ref = os.environ.get("GITHUB_REF", "local")
run_id = os.environ.get("GITHUB_RUN_ID", "local")
subject = subprocess.check_output(["sha256sum", "dist/SHA256SUMS"], text=True).split()[0]
predicate = {
    "builder": {"id": "https://github.com/" + repo + "/actions/workflows/release-dry-run.yml"},
    "buildType": "https://github.com/" + repo + "/actions/workflows/release-dry-run.yml@v1",
    "invocation": {
        "configSource": {
            "uri": "git+https://github.com/" + repo,
            "digest": {"gitCommit": sha},
            "entryPoint": workflow,
        },
        "parameters": {"repository": repo, "ref": ref, "commit": sha, "runId": run_id},
        "environment": {"runner": "github-hosted-ubuntu-24.04", "workflow": workflow},
    },
    "metadata": {
        "buildInvocationID": run_id,
        "buildStartedOn": os.environ["VERIFY_STARTED_ON"],
        "buildFinishedOn": datetime.datetime.now(datetime.timezone.utc).isoformat(),
        "completeness": {"parameters": True, "environment": True, "materials": True},
        "reproducible": False,
    },
    "materials": [{"uri": "git+https://github.com/" + repo, "digest": {"gitCommit": sha}}],
}
statement = {"_type": "https://in-toto.io/Statement/v0.1", "subject": [{"name": "dist/SHA256SUMS", "digest": {"sha256": subject}}], "predicateType": "https://slsa.dev/provenance/v0.2", "predicate": predicate}
with open(sys.argv[1], "w", encoding="utf-8") as output:
    json.dump(statement, output, sort_keys=True)
PY
cosign attest-blob --yes --tlog-upload=false --type slsaprovenance \
  --predicate "$TMP_DIR/slsa-provenance.json" \
  --key "$TMP_DIR/cosign.key" \
  --output-signature "$TMP_DIR/SHA256SUMS.attestation.sig" \
  --output-attestation "$TMP_DIR/SHA256SUMS.attestation.json" \
  "$DIST_DIR/SHA256SUMS"
cosign verify-blob-attestation --private-infrastructure --key "$TMP_DIR/cosign.pub" \
  --signature "$TMP_DIR/SHA256SUMS.attestation.sig" \
  --type slsaprovenance "$DIST_DIR/SHA256SUMS"

echo "Artifact verification completed. No artifacts were published or tagged."
