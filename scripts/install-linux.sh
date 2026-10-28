#!/usr/bin/env bash
set -euo pipefail

# Run from an extracted release, not from a source checkout.
PACKAGE="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
die() { printf 'Error: %s\n' "$*" >&2; exit 1; }
if [[ "${1:-}" == --help ]]; then
    printf 'Usage: bash install.sh\nRoot: system service as worklog. Otherwise: current-user systemd service.\nExisting configuration and data are preserved. Back up before upgrading.\n'
    exit 0
fi
[[ $# == 0 ]] || die 'Unknown arguments; use --help.'
[[ "$(uname -s)" == Linux ]] || die 'Linux is required.'
for tool in systemctl install cp mv mktemp id find rm chmod sleep uname dirname; do
    command -v "$tool" >/dev/null 2>&1 || die "Required command not found: $tool"
done
[[ -d /run/systemd/system ]] || die 'This host must be booted with systemd.'
[[ -x "$PACKAGE/bin/worklog" && -f "$PACKAGE/Web/dist/index.html" && -f "$PACKAGE/ARCH" ]] || die 'Run install.sh from an extracted Linux release package.'
case "$(uname -m)" in
    x86_64) arch=amd64 ;;
    aarch64|arm64) arch=arm64 ;;
    *) die 'Unsupported CPU architecture.' ;;
esac
[[ "$(<"$PACKAGE/ARCH")" == "$arch" ]] || die 'Release architecture does not match this host.'

if (( EUID == 0 )); then
    APP=/opt/worklog
    CONFIG=/etc/worklog
    DATA=/var/lib/worklog
    UNIT=/etc/systemd/system/worklog.service
    CTL=(systemctl)
    # Validate access before modifying files or creating users.
    "${CTL[@]}" show-environment >/dev/null || die 'Cannot connect to systemd.'
    if ! id worklog >/dev/null 2>&1; then
        command -v useradd >/dev/null 2>&1 || die 'useradd is required.'
        nologin="$(command -v nologin || true)"
        [[ -n "$nologin" ]] || die 'nologin is required.'
        useradd --system --user-group --home-dir "$DATA" --no-create-home --shell "$nologin" worklog
    fi
    [[ "$(id -u worklog)" != 0 ]] || die 'worklog must not be a root account.'
    GROUP="$(id -gn worklog)"
else
    [[ -n "${HOME:-}" ]] || die 'HOME must be set.'
    APP="${XDG_DATA_HOME:-$HOME/.local/share}/worklog/app"
    CONFIG="${XDG_CONFIG_HOME:-$HOME/.config}/worklog"
    DATA="${XDG_DATA_HOME:-$HOME/.local/share}/worklog/data"
    UNIT="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user/worklog.service"
    CTL=(systemctl --user)
    "${CTL[@]}" show-environment >/dev/null || die 'Cannot connect to the current user systemd manager. Log in normally (do not use sudo -u); see docs/DEPLOYMENT.md.'
fi
# Keep generated JSON/systemd paths unambiguous; ordinary spaces are supported.
for path in "$APP" "$CONFIG" "$DATA" "$UNIT"; do
    [[ "$path" == /* && ! "$path" =~ [[:cntrl:]] ]] || die 'Installation paths must be absolute and contain no control characters.'
done
json_escape() { local s="$1"; s="${s//\\/\\\\}"; s="${s//\"/\\\"}"; printf '%s' "$s"; }
unit_escape() { local s; s="$(json_escape "$1")"; printf '%s' "${s//%/%%}"; }
# ExecStart also expands dollar signs, unlike WorkingDirectory.
exec_escape() { local s; s="$(unit_escape "$1")"; printf '%s' "${s//\$/\$\$}"; }

printf 'Installing into %s\nConfiguration: %s\nData: %s\n' "$APP" "$CONFIG" "$DATA"
printf 'Existing configuration and data will be preserved. Back up before upgrading.\n'
# Prepare all new application files before stopping the old service.
install -d -m 755 "$APP" "$(dirname -- "$UNIT")"
STAGE="$(mktemp -d "$APP/.install.XXXXXX")"
trap 'rm -rf -- "$STAGE"' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
mkdir -p "$STAGE/bin"
install -m 755 "$PACKAGE/bin/worklog" "$STAGE/bin/worklog"
cp -R "$PACKAGE/Web/dist" "$STAGE/dist"
find "$STAGE/dist" -type d -exec chmod 755 {} +
find "$STAGE/dist" -type f -exec chmod 644 {} +

if (( EUID == 0 )); then
    install -d -o root -g "$GROUP" -m 750 "$CONFIG"
    install -d -o worklog -g "$GROUP" -m 700 "$DATA"
else
    install -d -m 700 "$CONFIG" "$DATA"
fi
if [[ ! -e "$CONFIG/config.json" && ! -L "$CONFIG/config.json" ]]; then
    # No shared credentials, no public listener; let Worklog create its local MCP token.
    printf '{\n  "addr": "127.0.0.1:12450",\n  "db_driver": "sqlite",\n  "db_dsn": "%s/worklog.db",\n  "static_dir": "%s/Web/dist",\n  "token_file": "%s/mcp-token",\n  "timezone": "Asia/Shanghai",\n  "allowed_hosts": ["localhost", "127.0.0.1", "::1"],\n  "allowed_origins": []\n}\n' \
        "$(json_escape "$DATA")" "$(json_escape "$APP")" "$(json_escape "$DATA")" > "$STAGE/config.json"
    if (( EUID == 0 )); then
        install -o root -g "$GROUP" -m 640 "$STAGE/config.json" "$CONFIG/config.json"
    else
        install -m 600 "$STAGE/config.json" "$CONFIG/config.json"
    fi
fi
if (( EUID == 0 )); then
    cp "$PACKAGE/docs/worklog.service" "$STAGE/worklog.service"
else
    # User services cannot rely on privileged mount namespaces or StateDirectory.
    # In particular, ProtectHome would hide this installation's own files.
    printf '[Unit]\nDescription=Worklog web and MCP server\nStartLimitIntervalSec=60\nStartLimitBurst=5\n\n[Service]\nWorkingDirectory="%s"\nExecStart="%s/bin/worklog" "--conf=%s/config.json" serve\nRestart=on-failure\nRestartSec=5s\nTimeoutStopSec=30s\nUMask=0077\nNoNewPrivileges=true\n\n[Install]\nWantedBy=default.target\n' \
        "$(unit_escape "$DATA")" "$(exec_escape "$APP")" "$(exec_escape "$CONFIG")" > "$STAGE/worklog.service"
fi
# Do not overwrite a live executable or mix old/new frontend assets.
if "${CTL[@]}" is-active --quiet worklog.service; then
    "${CTL[@]}" stop worklog.service
fi
install -d -m 755 "$APP/bin" "$APP/Web"
mv -f -- "$STAGE/bin/worklog" "$APP/bin/worklog"
# This is the dedicated application asset directory, never the data directory.
rm -rf -- "$APP/Web/dist"
mv -- "$STAGE/dist" "$APP/Web/dist"
install -m 644 "$PACKAGE/VERSION" "$APP/VERSION"
install -m 644 "$STAGE/worklog.service" "$UNIT"
"${CTL[@]}" daemon-reload
"${CTL[@]}" enable --now worklog.service
sleep 2
if ! "${CTL[@]}" is-active --quiet worklog.service; then
    "${CTL[@]}" status worklog.service --no-pager || true
    die 'Service failed to start; inspect journalctl (add --user for a user installation).'
fi
"${CTL[@]}" status worklog.service --no-pager
printf '\nInstalled. Default URL: http://127.0.0.1:12450 (unless changed in existing config).\n'
if (( EUID != 0 )); then
    printf 'User service runs as %s. For boot startup and operation after logout, an administrator must enable lingering:\n' "$(id -un)"
    printf '  sudo loginctl enable-linger %q\n' "$(id -un)"
fi
