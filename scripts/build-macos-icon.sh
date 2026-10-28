#!/usr/bin/env bash
set -euo pipefail

# Reuse the existing application artwork, including its transparent padding.
# Apple's tools generate all standard and Retina representations in the ICNS.
ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
if [[ $# -ne 1 ]]; then
    printf 'Usage: bash scripts/build-macos-icon.sh /path/to/Worklog.icns\n' >&2
    exit 1
fi
for tool in sips iconutil; do
    command -v "$tool" >/dev/null 2>&1 || { printf 'Required command not found: %s\n' "$tool" >&2; exit 1; }
done
STAGE="$(mktemp -d)"
trap 'rm -rf -- "$STAGE"' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
ICONSET="$STAGE/Worklog.iconset"
mkdir "$ICONSET"
for size in 16 32 128 256 512; do
    sips -z "$size" "$size" "$ROOT/Web/public/logo.png" \
        --out "$ICONSET/icon_${size}x${size}.png" >/dev/null
    retina=$((size * 2))
    sips -z "$retina" "$retina" "$ROOT/Web/public/logo.png" \
        --out "$ICONSET/icon_${size}x${size}@2x.png" >/dev/null
done
iconutil --convert icns --output "$1" "$ICONSET"
