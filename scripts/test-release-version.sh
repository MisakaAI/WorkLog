#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
STAGE="$(mktemp -d)"
trap 'rm -rf -- "$STAGE"' EXIT
mkdir -p "$STAGE/repo/scripts"
cp "$ROOT/scripts/release-version.sh" "$STAGE/repo/scripts/"
cd "$STAGE/repo"
git init -q
git config user.name 'Release test'
git config user.email 'release-test@example.invalid'
git commit -q --allow-empty -m initial
HELPER="$STAGE/repo/scripts/release-version.sh"

expect_version() {
    local actual
    actual="$(bash "$HELPER")"
    [[ "$actual" == "$1" ]] || { printf 'Expected %s, got %s\n' "$1" "$actual" >&2; exit 1; }
}
expect_failure() {
    if bash "$HELPER" > "$STAGE/stdout" 2> "$STAGE/stderr"; then
        printf 'Expected version resolution to fail\n' >&2
        exit 1
    fi
    [[ ! -s "$STAGE/stdout" && -s "$STAGE/stderr" ]]
}

expect_failure
git tag unrelated
expect_failure
git tag -a v0.2.0-rc.1 -m 'Release candidate'
expect_version 0.2.0-rc.1
VERSION=9.9.9 expect_version 0.2.0-rc.1
(cd "$STAGE" && expect_version 0.2.0-rc.1)
git commit -q --allow-empty -m development
expect_version 0.2.0-rc.1
git tag v0.2.0
expect_version 0.2.0

# Tags on unrelated branch tips must not affect this checkout.
head="$(git rev-parse HEAD)"
git checkout -q -b other
git commit -q --allow-empty -m other
git tag v99.0.0
git checkout -q --detach "$head"
expect_version 0.2.0

git commit -q --allow-empty -m build-metadata
git tag 'v0.3.0-rc.2+build.7'
expect_version '0.3.0-rc.2+build.7'
git commit -q --allow-empty -m invalid
git tag v1.2.3-bad/path
expect_failure
printf 'All release-version tests passed.\n'
