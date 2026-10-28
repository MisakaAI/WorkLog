#!/usr/bin/env bash
set -euo pipefail

# Build on any Go-supported host; targets need neither Go nor Node.js.
ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"
GO="${GO:-go}"
VERSION="${VERSION:-$(<"$ROOT/VERSION")}"
VERSION="${VERSION#v}"
if [[ ! "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?(\+[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?$ ]]; then
    printf 'Invalid VERSION: expected X.Y.Z with optional prerelease/build suffix.\n' >&2
    exit 1
fi
for tool in "$GO" npm tar zstd; do
    command -v "$tool" >/dev/null 2>&1 || { printf 'Required command not found: %s\n' "$tool" >&2; exit 1; }
done
STAGE="$(mktemp -d)"
trap 'rm -rf -- "$STAGE"' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

(cd "$ROOT/Web" && npm ci && npm run build)
mkdir -p "$ROOT/bin"
for arch in amd64 arm64; do
    # Match the human-readable Windows release architecture naming.
    label="$arch"
    [[ "$arch" != amd64 ]] || label=x86_64
    name="worklog-v${VERSION}-linux-${label}"
    package="$STAGE/$name"
    mkdir -p "$package/bin" "$package/Web" "$package/docs"
    GOOS=linux GOARCH="$arch" CGO_ENABLED=0 \
        "$GO" build -trimpath -buildvcs=false -ldflags '-s -w' \
        -o "$package/bin/worklog" ./cmd/worklog
    cp -R "$ROOT/Web/dist" "$package/Web/dist"
    cp "$ROOT/config.example.json" "$ROOT/config.docker.example.json" \
        "$ROOT/config.postgres.example.json" "$ROOT/README.md" "$package/"
    cp "$ROOT/Web/README.md" "$package/Web/README.md"
    printf '%s\n' "$VERSION" > "$package/VERSION"
    printf '%s\n' "$arch" > "$package/ARCH"
    cp "$ROOT"/docs/*.md "$ROOT/docs/worklog.service" "$package/docs/"
    cp "$ROOT/scripts/install-linux.sh" "$package/install.sh"
    chmod 755 "$package/install.sh" "$package/bin/worklog"
    # Compress to a temporary file first so failed builds do not publish partial archives.
    tar -C "$STAGE" -cf - "$name" | zstd -T0 -10 -o "$STAGE/$name.tar.zst"
    mv -f -- "$STAGE/$name.tar.zst" "$ROOT/bin/$name.tar.zst"
    printf 'Built %s\n' "$ROOT/bin/$name.tar.zst"
done
