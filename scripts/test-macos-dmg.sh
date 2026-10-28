#!/usr/bin/env bash
set -euo pipefail

# Inspect the final distribution image without launching or installing its app.
if [[ $# -ne 1 || ! -f "$1" ]]; then
    printf 'Usage: bash scripts/test-macos-dmg.sh /path/to/worklog-vVERSION-macos-ARCH.dmg\n' >&2
    exit 1
fi
NAME="$(basename -- "$1")"
if [[ ! "$NAME" =~ ^worklog-v(.+)-macos-(arm64|x86_64)\.dmg$ ]]; then
    printf 'Unexpected DMG filename: %s\n' "$NAME" >&2
    exit 1
fi
VERSION="${BASH_REMATCH[1]}"
ARCH="${BASH_REMATCH[2]}"
STAGE="$(mktemp -d)"
MOUNT="$STAGE/mount"
MOUNTED=0
cleanup() {
    if [[ "$MOUNTED" == 1 ]]; then
        hdiutil detach "$MOUNT" >/dev/null || return 1
    fi
    rmdir "$MOUNT" "$STAGE"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
mkdir "$MOUNT"
hdiutil attach -readonly -nobrowse -mountpoint "$MOUNT" "$1" >/dev/null
MOUNTED=1
APP="$MOUNT/worklog.app"
PLIST="$APP/Contents/Info.plist"
# Check the actual directory spelling, even on case-insensitive HFS+ volumes.
APPS=("$MOUNT"/*.app)
[[ ${#APPS[@]} -eq 1 && "${APPS[0]}" == "$APP" ]]
[[ -d "$APP" && ! -L "$APP" ]]
[[ -L "$MOUNT/Applications" && "$(readlink "$MOUNT/Applications")" == /Applications ]]
plutil -lint "$PLIST"
[[ "$(plutil -extract WorklogReleaseVersion raw "$PLIST")" == "$VERSION" ]]
[[ "$(plutil -extract CFBundleIdentifier raw "$PLIST")" == io.github.misakaai.worklog.desktop ]]
[[ "$(plutil -extract LSUIElement raw "$PLIST")" == true ]]
EXECUTABLE="$(plutil -extract CFBundleExecutable raw "$PLIST")"
[[ -x "$APP/Contents/MacOS/$EXECUTABLE" ]]
[[ "$(lipo -archs "$APP/Contents/MacOS/$EXECUTABLE")" == "$ARCH" ]]
ICON="$(plutil -extract CFBundleIconFile raw "$PLIST")"
[[ -s "$APP/Contents/Resources/$ICON" ]]
printf 'Passed: %s contains worklog.app (%s, %s), app icon and Applications link.\n' "$NAME" "$VERSION" "$ARCH"
