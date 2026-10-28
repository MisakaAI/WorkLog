#!/usr/bin/env bash
set -euo pipefail

# Package an already built app with macOS's built-in disk image tools.
if [[ $# -ne 2 ]]; then
    printf 'Usage: bash scripts/create-macos-dmg.sh /path/to/worklog.app /path/to/output.dmg\n' >&2
    exit 1
fi
if [[ "$(uname -s)" != Darwin ]]; then
    printf 'DMG packaging requires macOS.\n' >&2
    exit 1
fi
for tool in ditto hdiutil plutil; do
    command -v "$tool" >/dev/null 2>&1 || { printf 'Required command not found: %s\n' "$tool" >&2; exit 1; }
done
APP="$1"
OUTPUT="$2"
if [[ ! -d "$APP" || ! -f "$APP/Contents/Info.plist" ]]; then
    printf 'App bundle not found: %s\n' "$APP" >&2
    exit 1
fi
plutil -lint "$APP/Contents/Info.plist"
if [[ "$OUTPUT" != *.dmg ]]; then
    printf 'Output must have a .dmg extension: %s\n' "$OUTPUT" >&2
    exit 1
fi
if [[ -e "$OUTPUT" || -L "$OUTPUT" ]]; then
    printf 'Output already exists; move it away before packaging: %s\n' "$OUTPUT" >&2
    exit 1
fi
mkdir -p "$(dirname -- "$OUTPUT")"
STAGE="$(mktemp -d "$(dirname -- "$OUTPUT")/.dmg-build.XXXXXX")"
trap 'rm -rf -- "$STAGE"' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
mkdir "$STAGE/image"
ditto "$APP" "$STAGE/image/worklog.app"
ln -s /Applications "$STAGE/image/Applications"
hdiutil create -volname worklog -srcfolder "$STAGE/image" -fs HFS+ \
    -format UDZO -imagekey zlib-level=9 "$STAGE/worklog.dmg"
hdiutil verify "$STAGE/worklog.dmg"
mv -- "$STAGE/worklog.dmg" "$OUTPUT"
printf 'Packaged %s\n' "$OUTPUT"
