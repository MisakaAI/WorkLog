#!/usr/bin/env bash
set -euo pipefail

# Build AppKit locally with Apple's SDK; no cross-platform GUI runtime.
ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"
GO="${GO:-go}"
if [[ "$(uname -s)" != Darwin ]]; then
    printf 'Build on macOS with Xcode Command Line Tools installed.\n' >&2
    exit 1
fi
for tool in "$GO" npm xcrun sips iconutil plutil ditto hdiutil; do
    command -v "$tool" >/dev/null 2>&1 || { printf 'Required command not found: %s\n' "$tool" >&2; exit 1; }
done
VERSION="$(bash "$ROOT/scripts/release-version.sh")"
ARCH="${ARCH:-$("$GO" env GOHOSTARCH)}"
case "$ARCH" in
    arm64) LABEL=arm64 ;;
    amd64) LABEL=x86_64 ;;
    *) printf 'Unsupported ARCH: %s (use arm64 or amd64)\n' "$ARCH" >&2; exit 1 ;;
esac
SDK="$(xcrun --sdk macosx --show-sdk-path)"
CLANG="$(xcrun --sdk macosx --find clang)"
mkdir -p "$ROOT/bin"
OUTPUT="$ROOT/bin/worklog.app"
DMG_OUTPUT="$ROOT/bin/worklog-v${VERSION}-macos-${LABEL}.dmg"
# Never merge stale bundle contents or overwrite a running app.
for artifact in "$OUTPUT" "$DMG_OUTPUT"; do
    if [[ -e "$artifact" || -L "$artifact" ]]; then
        printf 'Output already exists; move it away before rebuilding: %s\n' "$artifact" >&2
        exit 1
    fi
done
STAGE="$(mktemp -d "$ROOT/bin/.macos-build.XXXXXX")"
trap 'rm -rf -- "$STAGE"' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
BUNDLE="$STAGE/worklog.app"
mkdir -p "$BUNDLE/Contents/MacOS" "$BUNDLE/Contents/Resources"
bash "$ROOT/scripts/build-macos-icon.sh" "$BUNDLE/Contents/Resources/Worklog.icns"
(cd "$ROOT/Web" && npm ci && npm run build)
GOOS=darwin GOARCH="$ARCH" CGO_ENABLED=1 CC="$CLANG" \
    SDKROOT="$SDK" MACOSX_DEPLOYMENT_TARGET=13.0 \
    "$GO" build -tags desktop -trimpath -buildvcs=false -ldflags '-s -w' \
    -o "$BUNDLE/Contents/MacOS/Worklog" ./cmd/worklog-desktop
# CFBundleVersion must be numeric; the full Git version remains visible in About metadata.
NUMERIC_VERSION="${VERSION%%[-+]*}"
cat > "$BUNDLE/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
    <key>CFBundleIdentifier</key><string>io.github.misakaai.worklog.desktop</string>
    <key>CFBundleName</key><string>Worklog</string>
    <key>CFBundleDisplayName</key><string>Worklog</string>
    <key>CFBundleExecutable</key><string>Worklog</string>
    <key>CFBundleIconFile</key><string>Worklog.icns</string>
    <key>CFBundlePackageType</key><string>APPL</string>
    <key>CFBundleShortVersionString</key><string>${NUMERIC_VERSION}</string>
    <key>CFBundleVersion</key><string>${NUMERIC_VERSION}</string>
    <key>WorklogReleaseVersion</key><string>${VERSION}</string>
    <key>LSMinimumSystemVersion</key><string>13.0</string>
    <key>LSUIElement</key><true/>
    <key>NSHighResolutionCapable</key><true/>
    <key>NSHumanReadableCopyright</key><string>Worklog</string>
</dict></plist>
PLIST
printf 'APPL????' > "$BUNDLE/Contents/PkgInfo"
cp "$ROOT/docs/MACOS_DESKTOP.md" "$BUNDLE/Contents/Resources/README.md"
plutil -lint "$BUNDLE/Contents/Info.plist"
# Publish only after the complete compressed image has been created and verified.
bash "$ROOT/scripts/create-macos-dmg.sh" "$BUNDLE" "$STAGE/release.dmg"
mv -- "$BUNDLE" "$OUTPUT"
mv -- "$STAGE/release.dmg" "$DMG_OUTPUT"
printf 'Built %s\n' "$OUTPUT"
printf 'Built %s\n' "$DMG_OUTPUT"
