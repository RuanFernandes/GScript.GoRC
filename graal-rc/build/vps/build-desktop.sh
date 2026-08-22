#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

VERSION="${VERSION:-}"
APP_NAME="${APP_NAME:-graal-rc}"
OUTPUT_DIR="${OUTPUT_DIR:-$ROOT_DIR/ci-artifacts/vps${VERSION:+/$VERSION}}"
TARGETS="${TARGETS:-windows-amd64,windows-386,linux-amd64,linux-386,darwin-amd64,darwin-arm64}"

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

IFS=',' read -r -a target_list <<< "$TARGETS"
for target in "${target_list[@]}"; do
  case "$target" in
    windows-amd64|windows-386)
      arch="${target#windows-}"
      printf 'Building Windows %s NSIS installer...\n' "$arch"
      CGO_ENABLED=0 task windows:package ARCH="$arch" FORMAT=nsis INSTALL_SCOPE=machine
      windows_suffix="x86"
      [[ "$arch" == "amd64" ]] && windows_suffix="x64"
      cp "bin/${APP_NAME}-${arch}-installer.exe" "$OUTPUT_DIR/nullbornes-rc-windows-${windows_suffix}-installer.exe"
      ;;
    linux-amd64)
      printf 'Building Linux x64 AppImage...\n'
      APPIMAGETOOL="${APPIMAGETOOL:-appimagetool}" CGO_ENABLED=1 task linux:create:appimage ARCH=amd64
      cp "bin/${APP_NAME}-x86_64.AppImage" "$OUTPUT_DIR/nullbornes-rc-linux-x64.AppImage"
      ;;
    linux-386)
      printf 'Building Linux x86 portable tarball...\n'
      CGO_ENABLED=1 task linux:create:tar ARCH=386
      cp "bin/${APP_NAME}-linux-386.tar.gz" "$OUTPUT_DIR/nullbornes-rc-linux-x86.tar.gz"
      ;;
    darwin-amd64|darwin-arm64)
      arch="${target#darwin-}"
      printf 'Building macOS %s application bundle...\n' "$arch"
      task darwin:package ARCH="$arch"
      mac_suffix="arm64"
      [[ "$arch" == "amd64" ]] && mac_suffix="x64"
      tar -C bin -czf "$OUTPUT_DIR/nullbornes-rc-macos-${mac_suffix}.tar.gz" "${APP_NAME}.app"
      ;;
    *)
      printf 'Unknown target %s. Use platform-architecture names from the native matrix.\n' "$target" >&2
      exit 2
      ;;
  esac
done

sha256sum "$OUTPUT_DIR"/nullbornes-rc-* > "$OUTPUT_DIR/SHA256SUMS.txt"
printf 'Desktop artifacts written to %s\n' "$OUTPUT_DIR"
