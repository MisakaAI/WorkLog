#!/usr/bin/env bash
set -euo pipefail

# Resolve the latest release tag reachable from HEAD, independent of caller cwd.
ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
if ! command -v git >/dev/null 2>&1; then
    printf 'Git is required to determine the release version.\n' >&2
    exit 1
fi
if ! tag="$(git -C "$ROOT" describe --tags --abbrev=0 --match 'v[0-9]*' HEAD 2>/dev/null)"; then
    printf 'No release tag reachable from HEAD. Fetch tags (git fetch --tags) or create a vX.Y.Z release tag before packaging.\n' >&2
    exit 1
fi
# Keep artifact paths safe; prerelease/build suffixes are preserved verbatim.
if [[ ! "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?(\+[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?$ ]]; then
    printf 'Invalid release tag: %s (expected vX.Y.Z with optional prerelease/build suffix).\n' "$tag" >&2
    exit 1
fi
printf '%s\n' "${tag#v}"
