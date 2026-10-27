#!/usr/bin/env bash
set -euo pipefail

# Build-time prerequisites: Go (see go.mod), Node.js/npm, and network access
# for uncached Go modules and npm packages. No Windows SDK or CGO is needed.
# Override the Go executable with e.g. GO=/opt/go/bin/go, or the release
# version with VERSION=0.2.0 (otherwise read the repository's VERSION file).
ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"
GO="${GO:-go}"
VERSION="${VERSION:-$(<"$ROOT/VERSION")}"
VERSION="${VERSION#v}"
# Keep filenames predictable and prevent path separators or shell metacharacters.
if [[ ! "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?(\+[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?$ ]]; then
    printf 'Invalid VERSION: expected X.Y.Z with optional prerelease/build suffix.\n' >&2
    exit 1
fi
OUTPUT="$ROOT/bin/worklog-v${VERSION}-windows-x86_64.exe"
WINRES_TOOL='github.com/tc-hib/go-winres@v0.3.3'
RESOURCE_PREFIX="$ROOT/cmd/worklog-desktop/rsrc"
RESOURCE="${RESOURCE_PREFIX}_windows_amd64.syso"

if ! command -v "$GO" >/dev/null 2>&1; then
    printf 'Go executable not found: %s (set GO or update PATH)\n' "$GO" >&2
    exit 1
fi
if ! command -v npm >/dev/null 2>&1; then
    printf 'npm is required to build the embedded frontend.\n' >&2
    exit 1
fi
# Do not overwrite or delete a resource owned by another build.
if [[ -e "$RESOURCE" || -L "$RESOURCE" ]]; then
    printf 'Resource already exists; remove it before building: %s\n' "$RESOURCE" >&2
    exit 1
fi
cleanup() {
    rm -f -- "$RESOURCE"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

(
    cd "$ROOT/Web"
    npm ci && npm run build
)

# Run the pinned pure-Go compiler on the host, even if the caller has set
# GOOS/GOARCH for cross-compilation. @version keeps it out of our go.mod.
HOST_OS="$("$GO" env GOHOSTOS)"
HOST_ARCH="$("$GO" env GOHOSTARCH)"
GOOS="$HOST_OS" GOARCH="$HOST_ARCH" CGO_ENABLED=0 \
    "$GO" run "$WINRES_TOOL" make \
    --in "$ROOT/cmd/worklog-desktop/winres.json" \
    --arch amd64 --out "$RESOURCE_PREFIX"

mkdir -p "$ROOT/bin"
# Omit VCS metadata so temporary resources/checkout state do not affect output.
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
    "$GO" build -tags desktop -trimpath -buildvcs=false \
    -ldflags '-H=windowsgui -s -w' \
    -o "$OUTPUT" ./cmd/worklog-desktop

printf 'Built %s\n' "$OUTPUT"
