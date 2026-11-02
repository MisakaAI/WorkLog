#!/usr/bin/env bash
set -euo pipefail

# No sudo, real systemctl, account changes, or host installation paths.
# Python only rewrites a temporary installer copy's context; production scripts
# have no test-only environment overrides. Go/npm/zstd are mocked for packaging.
ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
for tool in python3 bash install tar cmp diff grep stat readlink; do
    command -v "$tool" >/dev/null || { printf 'Required test tool: %s\n' "$tool" >&2; exit 1; }
done
TEST_ROOT="$(mktemp -d)"
trap 'rm -rf -- "$TEST_ROOT"' EXIT
trap 'printf "FAILED at line %s\n" "$LINENO" >&2' ERR
export TEST_ROOT
REAL_INSTALL="$(command -v install)"
export REAL_INSTALL
TOOLS="$TEST_ROOT/tools"
mkdir -p "$TOOLS"
# An allowlisted PATH prevents accidental fallback to host systemctl/useradd.
for tool in bash cp mv mktemp find rm chmod mkdir dirname tar cmp; do
    ln -s "$(command -v "$tool")" "$TOOLS/$tool"
done

cat > "$TOOLS/install" <<'MOCK'
#!/usr/bin/env bash
set -euo pipefail
args=()
while (( $# )); do
    case "$1" in
        -o|-g) shift 2 ;; # Test permissions, but never change ownership.
        *) args+=("$1"); shift ;;
    esac
done
[[ "${args[-1]}" == "$TEST_ROOT/"* ]] || exit 90
exec "$REAL_INSTALL" "${args[@]}"
MOCK
cat > "$TOOLS/id" <<'MOCK'
#!/usr/bin/env bash
set -euo pipefail
case "$*" in
    worklog) [[ -e "$TEST_CONTEXT/account" ]] ;;
    '-u worklog') printf '12345\n' ;;
    '-gn worklog') printf 'test-worklog\n' ;;
    -un) printf 'test-user\n' ;;
    *) exit 91 ;;
esac
MOCK
cat > "$TOOLS/useradd" <<'MOCK'
#!/usr/bin/env bash
set -euo pipefail
[[ "$*" == "--system --user-group --home-dir $TEST_DATA --no-create-home --shell "*' worklog' ]]
printf '%s\n' "$*" >> "$TEST_CONTEXT/useradd.log"
: > "$TEST_CONTEXT/account"
MOCK
cat > "$TOOLS/uname" <<'MOCK'
#!/usr/bin/env bash
case "$*" in -s) echo Linux ;; -m) echo x86_64 ;; *) exit 91 ;; esac
MOCK
cat > "$TOOLS/sleep" <<'MOCK'
#!/usr/bin/env bash
[[ "$*" == 2 ]]
MOCK
cat > "$TOOLS/nologin" <<'MOCK'
#!/usr/bin/env bash
exit 1
MOCK
cat > "$TOOLS/systemctl" <<'MOCK'
#!/usr/bin/env bash
set -euo pipefail
if [[ "$TEST_MODE" == user ]]; then
    [[ "$1" == --user ]]
    shift
else
    [[ "$1" != --user ]]
fi
printf '%s\n' "$*" >> "$TEST_CONTEXT/systemctl.log"
[[ "$1" != "${MOCK_FAIL_COMMAND:-}" ]] || exit 42
case "$*" in
    show-environment) ;;
    'is-active --quiet worklog.service') [[ -f "$TEST_CONTEXT/active" ]] ;;
    'stop worklog.service')
        # The running binary and assets must still be the old release at stop.
        cmp "$TEST_CONTEXT/old-binary" "$TEST_APP/bin/worklog"
        cmp "$TEST_CONTEXT/old-index" "$TEST_APP/Web/dist/index.html"
        rm -f "$TEST_CONTEXT/active"
        ;;
    daemon-reload)
        [[ -e "$TEST_UNIT" || -L "$TEST_UNIT" ]]
        cmp "$TEST_PACKAGE/bin/worklog" "$TEST_APP/bin/worklog"
        cmp "$TEST_PACKAGE/Web/dist/index.html" "$TEST_APP/Web/dist/index.html"
        : > "$TEST_CONTEXT/reloaded"
        ;;
    'enable worklog.service')
        [[ -f "$TEST_CONTEXT/reloaded" ]]
        : > "$TEST_CONTEXT/enabled"
        ;;
    'restart worklog.service')
        [[ -f "$TEST_CONTEXT/reloaded" && -f "$TEST_CONTEXT/enabled" ]]
        : > "$TEST_CONTEXT/active"
        ;;
    'status worklog.service --no-pager') ;;
    *) printf 'Unexpected systemctl call: %s\n' "$*" >&2; exit 91 ;;
esac
MOCK
chmod +x "$TOOLS/"{install,id,useradd,uname,sleep,nologin,systemctl}

# Build in a disposable checkout with decoy documentation and examples, but no
# docs/worklog.service: neither packaging nor installation may depend on it.
REPO="$TEST_ROOT/repo"
mkdir -p "$REPO/scripts" "$REPO/Web/public" "$REPO/docs"
cp "$ROOT/scripts/"{build-linux,install-linux,uninstall-linux}.sh "$REPO/scripts/"
cp "$ROOT/Web/public/"{config,work-calendar}.json "$REPO/Web/public/"
printf 'not a release payload\n' > "$REPO/README.md"
cp "$REPO/README.md" "$REPO/Web/README.md"
cp "$REPO/README.md" "$REPO/docs/DEPLOYMENT.md"
for name in config.example.json config.docker.example.json config.postgres.example.json; do
    cp "$REPO/README.md" "$REPO/$name"
done
printf '#!/usr/bin/env bash\nprintf "1.2.3\\n"\n' > "$REPO/scripts/release-version.sh"
cat > "$TOOLS/npm" <<'MOCK'
#!/usr/bin/env bash
set -euo pipefail
case "$*" in
    ci) ;;
    'run build')
        mkdir -p dist/assets
        cp public/*.json dist/
        printf 'new index\n' > dist/index.html
        printf 'new asset\n' > dist/assets/new.js
        ;;
    *) exit 91 ;;
esac
MOCK
cat > "$TOOLS/go" <<'MOCK'
#!/usr/bin/env bash
set -euo pipefail
[[ "$GOOS" == linux && "$CGO_ENABLED" == 0 ]]
[[ "$GOARCH" == amd64 || "$GOARCH" == arm64 ]]
while (( $# )); do
    if [[ "$1" == -o ]]; then
        printf '#!/usr/bin/env bash\n# %s fixture\nexit 0\n' "$GOARCH" > "$2"
        exit 0
    fi
    shift
done
exit 91
MOCK
# Intentionally uncompressed tar: test contents, not the zstd codec. Python
# passes through binary data without requiring real compression tools.
printf '#!%s\nimport sys\nfrom pathlib import Path\nassert sys.argv[1:4] == ["-T0", "-10", "-o"]\nPath(sys.argv[4]).write_bytes(sys.stdin.buffer.read())\n' "$(command -v python3)" > "$TOOLS/zstd"
chmod +x "$TOOLS/npm" "$TOOLS/go" "$TOOLS/zstd"
env -i PATH="$TOOLS" HOME="$TEST_ROOT" TMPDIR="$TEST_ROOT" GO=go \
    bash "$REPO/scripts/build-linux.sh" > "$TEST_ROOT/build.log"
for label in x86_64 arm64; do
    name="worklog-v1.2.3-linux-$label"
    archive="$REPO/bin/$name.tar.zst"
    tar -tf "$archive" | LC_ALL=C sort > "$TEST_ROOT/contents"
    printf '%s\n' "$name/" "$name/ARCH" "$name/VERSION" "$name/Web/" \
        "$name/Web/dist/" "$name/Web/dist/assets/" "$name/Web/dist/assets/new.js" \
        "$name/Web/dist/config.json" "$name/Web/dist/index.html" "$name/Web/dist/work-calendar.json" \
        "$name/bin/" "$name/bin/worklog" "$name/install.sh" "$name/uninstall.sh" \
        | LC_ALL=C sort > "$TEST_ROOT/expected-contents"
    diff -u "$TEST_ROOT/expected-contents" "$TEST_ROOT/contents"
    tar -xf "$archive" -C "$TEST_ROOT"
    package="$TEST_ROOT/$name"
    arch="$label"; [[ "$label" != x86_64 ]] || arch=amd64
    [[ "$(<"$package/ARCH")" == "$arch" && "$(<"$package/VERSION")" == 1.2.3 ]]
    [[ -x "$package/bin/worklog" && -x "$package/install.sh" && -x "$package/uninstall.sh" ]]
    cmp "$ROOT/scripts/install-linux.sh" "$package/install.sh"
    cmp "$ROOT/scripts/uninstall-linux.sh" "$package/uninstall.sh"
done
printf 'PASS: both Linux package manifests (mock builds/compression)\n'

run_installer() {
    rm -f "$TEST_CONTEXT/reloaded" "$TEST_CONTEXT/enabled"
    : > "$TEST_CONTEXT/systemctl.log"
    env -i PATH="$TOOLS" HOME="$TEST_CONTEXT/home" \
        XDG_DATA_HOME="$TEST_CONTEXT/user data" XDG_CONFIG_HOME="$TEST_CONTEXT/user config" \
        TEST_ROOT="$TEST_ROOT" REAL_INSTALL="$REAL_INSTALL" TEST_CONTEXT="$TEST_CONTEXT" \
        TEST_APP="$TEST_APP" TEST_DATA="$TEST_DATA" TEST_UNIT="$TEST_UNIT" \
        TEST_MODE="$TEST_MODE" TEST_PACKAGE="$TEST_PACKAGE" MOCK_FAIL_COMMAND="${MOCK_FAIL_COMMAND:-}" \
        bash "$TEST_PACKAGE/install.sh" > "$TEST_CONTEXT/install.log" 2>&1
}
expect_calls() {
    local stopped="$1"
    {
        printf 'show-environment\nis-active --quiet worklog.service\n'
        if [[ "$stopped" == yes ]]; then printf 'stop worklog.service\n'; fi
        printf 'daemon-reload\nenable worklog.service\nrestart worklog.service\nis-active --quiet worklog.service\nstatus worklog.service --no-pager\n'
    } > "$TEST_CONTEXT/expected-calls"
    diff -u "$TEST_CONTEXT/expected-calls" "$TEST_CONTEXT/systemctl.log"
}
snapshot_assets() {
    cp "$TEST_APP/bin/worklog" "$TEST_CONTEXT/old-binary"
    cp "$TEST_APP/Web/dist/index.html" "$TEST_CONTEXT/old-index"
}
check_configs() {
    cmp "$TEST_CONTEXT/backend.saved" "$CONFIG/config.json"
    cmp "$TEST_CONTEXT/frontend.saved" "$TEST_APP/Web/dist/config.json"
    cmp "$TEST_CONTEXT/calendar.saved" "$TEST_APP/Web/dist/work-calendar.json"
    [[ "$(stat -c %a "$CONFIG/config.json")" == 600 ]]
    [[ "$(stat -c %a "$TEST_APP/Web/dist/config.json")" == 640 ]]
}

for TEST_MODE in root user; do
    TEST_CONTEXT="$TEST_ROOT/$TEST_MODE context"
    TEST_PACKAGE="$TEST_CONTEXT/package"
    mkdir -p "$TEST_CONTEXT/home" "$TEST_CONTEXT/systemd"
    cp -R "$TEST_ROOT/worklog-v1.2.3-linux-x86_64" "$TEST_PACKAGE"
    if [[ "$TEST_MODE" == root ]]; then
        TEST_APP="$TEST_CONTEXT/opt/worklog"
        CONFIG="$TEST_CONTEXT/etc/worklog"
        TEST_DATA="$TEST_CONTEXT/var/lib/worklog"
        TEST_UNIT="$TEST_CONTEXT/etc/systemd/system/worklog.service"
    else
        TEST_APP="$TEST_CONTEXT/user data/worklog/app"
        CONFIG="$TEST_CONTEXT/user config/worklog"
        TEST_DATA="$TEST_CONTEXT/user data/worklog/data"
        TEST_UNIT="$TEST_CONTEXT/user config/systemd/user/worklog.service"
    fi
    # Assert every substitution, and only rewrite path assignments, not the
    # embedded root unit. No real privileged branch or /run probe is executed.
    python3 - "$TEST_PACKAGE/install.sh" "$TEST_MODE" "$TEST_CONTEXT" <<'PY'
import pathlib, shlex, sys
path, mode, context = sys.argv[1:]
p = pathlib.Path(path)
s = p.read_text()
for old, new in {
    'APP=/opt/worklog': 'APP=' + shlex.quote(context + '/opt/worklog'),
    'CONFIG=/etc/worklog': 'CONFIG=' + shlex.quote(context + '/etc/worklog'),
    'DATA=/var/lib/worklog': 'DATA=' + shlex.quote(context + '/var/lib/worklog'),
    'UNIT=/etc/systemd/system/worklog.service': 'UNIT=' + shlex.quote(context + '/etc/systemd/system/worklog.service'),
    '[[ -d /run/systemd/system ]]': '[[ -d ' + shlex.quote(context + '/systemd') + ' ]]',
}.items():
    assert s.count(old) == 1, old
    s = s.replace(old, new)
assert s.count('EUID') == 5
s = s.replace('EUID', '0' if mode == 'root' else '1000')
p.write_text(s)
PY
    run_installer
    expect_calls no
    [[ -f "$CONFIG/config.json" && -f "$TEST_UNIT" && -d "$TEST_DATA" ]]
    cmp "$TEST_PACKAGE/Web/dist/config.json" "$TEST_APP/Web/dist/config.json"
    cmp "$TEST_PACKAGE/Web/dist/work-calendar.json" "$TEST_APP/Web/dist/work-calendar.json"
    cp "$TEST_UNIT" "$TEST_CONTEXT/generated-unit"
    if [[ "$TEST_MODE" == root ]]; then
        # Compare EVERY directive, including empty CapabilityBoundingSet, to
        # the reference template. Comments/blank lines are not directives.
        grep -Ev '^[[:space:]]*(#|$)' "$ROOT/docs/worklog.service" > "$TEST_CONTEXT/reference-unit"
        grep -Ev '^[[:space:]]*(#|$)' "$TEST_UNIT" > "$TEST_CONTEXT/actual-unit"
        diff -u "$TEST_CONTEXT/reference-unit" "$TEST_CONTEXT/actual-unit"
        [[ "$(stat -c %a "$CONFIG/config.json")" == 640 ]]
        [[ -s "$TEST_CONTEXT/useradd.log" ]]
        cp "$TEST_CONTEXT/useradd.log" "$TEST_CONTEXT/useradd.saved"
    else
        grep -Fq 'NoNewPrivileges=true' "$TEST_UNIT"
        grep -Fq "WorkingDirectory=\"$TEST_DATA\"" "$TEST_UNIT"
        grep -Fq "ExecStart=\"$TEST_APP/bin/worklog\" \"--conf=$CONFIG/config.json\" serve" "$TEST_UNIT"
        [[ "$(stat -c %a "$CONFIG/config.json")" == 600 ]]
        [[ ! -e "$TEST_CONTEXT/useradd.log" ]]
    fi

    printf 'custom backend\n' > "$CONFIG/config.json"
    printf 'additional backend config\n' > "$CONFIG/extra.json"
    printf 'custom frontend\n' > "$TEST_APP/Web/dist/config.json"
    printf 'custom calendar\n' > "$TEST_APP/Web/dist/work-calendar.json"
    printf 'custom unit\n' > "$TEST_UNIT"
    printf 'keep data\n' > "$TEST_DATA/worklog.db"
    chmod 600 "$CONFIG/config.json" "$TEST_UNIT"
    chmod 640 "$TEST_APP/Web/dist/config.json"
    cp "$CONFIG/config.json" "$TEST_CONTEXT/backend.saved"
    cp "$TEST_APP/Web/dist/config.json" "$TEST_CONTEXT/frontend.saved"
    cp "$TEST_APP/Web/dist/work-calendar.json" "$TEST_CONTEXT/calendar.saved"
    cp "$TEST_UNIT" "$TEST_CONTEXT/unit.saved"
    printf 'obsolete asset\n' > "$TEST_APP/Web/dist/stale.js"
    snapshot_assets
    printf '#!/usr/bin/env bash\n# upgraded\nexit 0\n' > "$TEST_PACKAGE/bin/worklog"
    printf 'upgraded index\n' > "$TEST_PACKAGE/Web/dist/index.html"
    run_installer
    expect_calls yes
    check_configs
    cmp "$TEST_CONTEXT/unit.saved" "$TEST_UNIT"
    [[ "$(stat -c %a "$TEST_UNIT")" == 600 ]]
    [[ ! -e "$TEST_APP/Web/dist/stale.js" ]]
    [[ "$(<"$CONFIG/extra.json")" == 'additional backend config' ]]
    [[ "$(<"$TEST_DATA/worklog.db")" == 'keep data' ]]

    # A missing unit is recreated on upgrade without resetting configuration.
    rm "$TEST_UNIT"
    snapshot_assets
    run_installer
    expect_calls yes
    check_configs
    cmp "$TEST_CONTEXT/generated-unit" "$TEST_UNIT"

    # Materialize live runtime links before deleting their targets in old dist.
    # Cover relative and absolute targets without retaining other old assets.
    mkdir -p "$TEST_APP/Web/dist/local-settings"
    cp "$TEST_CONTEXT/frontend.saved" "$TEST_APP/Web/dist/local-settings/frontend.json"
    cp "$TEST_CONTEXT/calendar.saved" "$TEST_APP/Web/dist/local-settings/calendar.json"
    chmod 640 "$TEST_APP/Web/dist/local-settings/frontend.json"
    printf 'obsolete asset\n' > "$TEST_APP/Web/dist/local-settings/stale.js"
    rm "$TEST_APP/Web/dist/"{config,work-calendar}.json
    ln -s local-settings/frontend.json "$TEST_APP/Web/dist/config.json"
    ln -s "$TEST_APP/Web/dist/local-settings/calendar.json" "$TEST_APP/Web/dist/work-calendar.json"
    snapshot_assets
    run_installer
    expect_calls yes
    check_configs
    for config in config.json work-calendar.json; do
        [[ -f "$TEST_APP/Web/dist/$config" && ! -L "$TEST_APP/Web/dist/$config" ]]
    done
    [[ ! -e "$TEST_APP/Web/dist/local-settings" ]]

    # Live runtime links become regular files; dangling links stay links.
    # Backend config/unit links stay links, and external targets are not changed.
    for kind in live dangling; do
        for file in "$CONFIG/config.json" "$TEST_APP/Web/dist/config.json" \
            "$TEST_APP/Web/dist/work-calendar.json" "$TEST_UNIT"; do
            rm "$file"
            target="../$(basename "$file").$kind"
            # Exercise external targets separately from the in-tree regression.
            if [[ "$kind" == live ]]; then printf 'external settings\n' > "$(dirname "$file")/$target"; fi
            ln -s "$target" "$file"
        done
        snapshot_assets
        run_installer
        expect_calls yes
        for file in "$CONFIG/config.json" "$TEST_APP/Web/dist/config.json" \
            "$TEST_APP/Web/dist/work-calendar.json" "$TEST_UNIT"; do
            if [[ "$kind" == live && "$file" == "$TEST_APP/Web/dist/"* ]]; then
                [[ -f "$file" && ! -L "$file" ]]
            else
                [[ -L "$file" && "$(readlink "$file")" == "../$(basename "$file").$kind" ]]
            fi
            if [[ "$kind" == live ]]; then
                [[ "$(<"$file")" == 'external settings' ]]
                target="$(dirname "$file")/../$(basename "$file").$kind"
                [[ "$(<"$target")" == 'external settings' ]]
            else
                [[ ! -e "$file" ]]
            fi
        done
    done

    # Only missing config files get defaults; the other runtime setting stays.
    rm "$CONFIG/config.json" "$TEST_APP/Web/dist/config.json" "$TEST_UNIT"
    snapshot_assets
    run_installer
    expect_calls yes
    [[ -f "$CONFIG/config.json" && ! -L "$CONFIG/config.json" ]]
    cmp "$TEST_PACKAGE/Web/dist/config.json" "$TEST_APP/Web/dist/config.json"
    [[ -L "$TEST_APP/Web/dist/work-calendar.json" && ! -e "$TEST_APP/Web/dist/work-calendar.json" ]]

    # Failure must not bypass stop/reload/enable or fall back to enable --now.
    for MOCK_FAIL_COMMAND in stop daemon-reload enable restart; do
        snapshot_assets
        : > "$TEST_CONTEXT/active"
        printf 'next release: %s\n' "$MOCK_FAIL_COMMAND" > "$TEST_PACKAGE/bin/worklog"
        if run_installer; then
            printf 'Expected failure at %s\n' "$MOCK_FAIL_COMMAND" >&2
            exit 1
        fi
        [[ "$(tail -n 1 "$TEST_CONTEXT/systemctl.log")" == "$MOCK_FAIL_COMMAND"* ]]
        if [[ "$MOCK_FAIL_COMMAND" == stop ]]; then
            cmp "$TEST_CONTEXT/old-binary" "$TEST_APP/bin/worklog"
            cmp "$TEST_CONTEXT/old-index" "$TEST_APP/Web/dist/index.html"
        fi
    done
    unset MOCK_FAIL_COMMAND
    if [[ "$TEST_MODE" == root ]]; then
        cmp "$TEST_CONTEXT/useradd.saved" "$TEST_CONTEXT/useradd.log"
    fi
    printf 'PASS: %s install, upgrades, missing files, symlinks, ordering and failures\n' "$TEST_MODE"
done
printf 'All isolated Linux release tests passed.\n'
