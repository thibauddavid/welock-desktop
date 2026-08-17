#!/usr/bin/env bash
#
# Rebuild the committed, opaque engine helper binaries that welock-desktop embeds
# (internal/app/sidecar/bin). This is the ONE step that needs the engine source — run
# it on an engine bump, then commit the refreshed binaries. It mirrors a web client
# re-fetching a prebuilt wasm module when it bumps the engine pin.
#
# The helper has NO cgo (the BLE radio lives in the app, not the helper), so it
# cross-compiles from any host: a universal (arm64+amd64) macOS binary via lipo, and a
# Windows amd64 binary — both built here on macOS.
#
# Usage:  tools/build-sidecar.sh <path-to-engine-src>   (or set WELOCK_ENGINE_SRC)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(cd "$SCRIPT_DIR/.." && pwd)"
ENGINE="${1:-${WELOCK_ENGINE_SRC:-}}"
BIN="$REPO/internal/app/sidecar/bin"

if [ -z "$ENGINE" ]; then
  echo "usage: tools/build-sidecar.sh <path-to-engine-src>  (or set WELOCK_ENGINE_SRC)" >&2
  exit 1
fi

if [ ! -d "$ENGINE/cmd/sidecar" ]; then
  echo "engine source not found at: $ENGINE (pass its path as arg 1)" >&2
  exit 1
fi

mkdir -p "$BIN"
VER="$(git -C "$ENGINE" describe --tags --always 2>/dev/null || echo dev)"
LD="-s -w -X main.coreVersion=$VER"
echo "building engine helper $VER from $ENGINE"

( cd "$ENGINE"
  echo "  darwin/arm64 + darwin/amd64 -> universal"
  GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags "$LD" -o /tmp/welock-sidecar_arm64 ./cmd/sidecar
  GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags "$LD" -o /tmp/welock-sidecar_amd64 ./cmd/sidecar
  lipo -create -output "$BIN/welock-sidecar_darwin" /tmp/welock-sidecar_arm64 /tmp/welock-sidecar_amd64
  rm -f /tmp/welock-sidecar_arm64 /tmp/welock-sidecar_amd64

  echo "  windows/amd64"
  GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "$LD" -o "$BIN/welock-sidecar_windows.exe" ./cmd/sidecar
)

echo "done — review + commit:"
ls -la "$BIN"
