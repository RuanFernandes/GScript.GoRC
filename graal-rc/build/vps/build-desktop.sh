#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

ARCH="${ARCH:-amd64}"
VERSION="${VERSION:-}"
APP_NAME="${APP_NAME:-graal-rc}"
OUTPUT_DIR="${OUTPUT_DIR:-$ROOT_DIR/ci-artifacts/vps${VERSION:+/$VERSION}}"

if [[ "$ARCH" != "amd64" ]]; then
  printf 'Only amd64 is supported until matching native libraries are available.\n' >&2
  exit 2
fi

for tool in docker node npm task wails3 pwsh makensis tar sha256sum; do
  command -v "$tool" >/dev/null 2>&1 || {
    printf 'Required build tool is missing: %s\n' "$tool" >&2
    exit 1
  }
done

mkdir -p "$OUTPUT_DIR"

if [[ -n "$VERSION" ]]; then
  node build/ci/set-version.mjs "$VERSION"
fi

# Build the frontend and bindings once. The platform tasks also depend on this
# task, but Task's run: once scope does not cross separate task invocations.
task common:build:frontend BUILD_FLAGS='-tags production'

printf 'Building Windows x64 NSIS installer...\n'
CGO_ENABLED=1 task windows:package ARCH="$ARCH" FORMAT=nsis INSTALL_SCOPE=machine
cp "bin/${APP_NAME}-${ARCH}-installer.exe" "$OUTPUT_DIR/nullbornes-rc-windows-x64-installer.exe"

printf 'Building Linux x64 AppImage...\n'
APPIMAGETOOL="${APPIMAGETOOL:-appimagetool}" CGO_ENABLED=1 task linux:create:appimage ARCH="$ARCH"
cp "bin/${APP_NAME}-x86_64.AppImage" "$OUTPUT_DIR/nullbornes-rc-linux-x64.AppImage"

printf 'Building macOS x64 application bundle...\n'
task darwin:package ARCH="$ARCH"
tar -C bin -czf "$OUTPUT_DIR/nullbornes-rc-macos-x64.tar.gz" "${APP_NAME}.app"

sha256sum "$OUTPUT_DIR"/nullbornes-rc-* > "$OUTPUT_DIR/SHA256SUMS.txt"
printf 'Desktop installers written to %s\n' "$OUTPUT_DIR"
