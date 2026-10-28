#!/usr/bin/env bash
set -euo pipefail

# Linux 卸载脚本，可从源码目录或解压后的发布包中运行，不依赖包内的可执行文件。
# 必须使用与安装时相同的身份和 XDG 环境变量：
#   root（sudo）卸载系统服务；普通用户只卸载自己的 systemd 用户服务。
# 不带参数时默认保留配置和数据；系统级卸载额外询问是否删除账户及配置/数据。
# purge / --purge 不作交互确认，直接删除配置和数据，系统级还删除 worklog 用户/组。
# 注意：安装时可能复用了已有 worklog 账户，彻底清理前务必确认没有其他用途。
die() { printf '错误：%s\n' "$*" >&2; exit 1; }
usage() {
    printf '%s\n' \
        '用法：bash uninstall.sh [purge|--purge]' \
        '      bash uninstall.sh --help' \
        '' \
        'root 执行：卸载系统级 Worklog；普通用户执行：卸载当前用户的 Worklog。' \
        '不带参数：默认保留配置和数据；系统级卸载分别询问是否删除用户/组及配置/数据。' \
        'purge / --purge：无需确认，删除配置和数据；系统级同时删除 worklog 用户/组。' \
        '警告：purge 不可撤销，可能删除安装时复用的 worklog 账户。' \
        '请使用与安装时相同的身份及 XDG 路径。清理前请自行备份。'
}

# 严格限制参数，避免拼写错误导致用户误以为数据已经被清理。
PURGE=false
case "${1:-}" in
    --help|-h) [[ $# == 1 ]] || die '参数过多，请使用 --help。'; usage; exit 0 ;;
    purge|--purge) [[ $# == 1 ]] || die '参数过多，请使用 --help。'; PURGE=true ;;
    '') [[ $# == 0 ]] || die '不支持空参数，请使用 --help。' ;;
    *) die '未知参数，请使用 --help。' ;;
esac

[[ "$(uname -s)" == Linux ]] || die '此脚本仅支持 Linux。'
for tool in systemctl rm realpath uname; do
    command -v "$tool" >/dev/null 2>&1 || die "缺少必需命令：$tool"
done
[[ -d /run/systemd/system ]] || die '当前主机必须使用 systemd 启动。'
trap 'exit 130' INT
trap 'exit 143' TERM

# 路径与 install-linux.sh 保持一致；不要通过 sudo 推测原登录用户的安装位置。
# root 模式不会卸载任何用户级安装，用户模式也不会操作系统级安装。
if (( EUID == 0 )); then
    APP=/opt/worklog
    CONFIG=/etc/worklog
    DATA=/var/lib/worklog
    UNIT=/etc/systemd/system/worklog.service
    CTL=(systemctl)
else
    [[ -n "${HOME:-}" ]] || die '必须设置 HOME。'
    APP="${XDG_DATA_HOME:-$HOME/.local/share}/worklog/app"
    CONFIG="${XDG_CONFIG_HOME:-$HOME/.config}/worklog"
    DATA="${XDG_DATA_HOME:-$HOME/.local/share}/worklog/data"
    UNIT="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user/worklog.service"
    CTL=(systemctl --user)
fi

# 递归删除前检查路径：拒绝相对路径、控制字符、根目录和相互嵌套的目标。
# realpath -m 允许目录不存在（便于重复卸载），同时解析 .. 和已有符号链接。
# 比较实际路径可防止异常 XDG 设置让程序目录覆盖本应保留的配置或数据。
PATHS=("$APP" "$CONFIG" "$DATA" "$UNIT")
RESOLVED=()
for path in "${PATHS[@]}"; do
    [[ "$path" == /* && ! "$path" =~ [[:cntrl:]] ]] || die '安装路径必须为绝对路径，且不能包含控制字符。'
    resolved="$(realpath -m -- "$path")"
    [[ "$resolved" != / ]] || die "拒绝操作根目录：$path"
    if [[ -n "${HOME:-}" ]]; then
        [[ "$resolved" != "$(realpath -m -- "$HOME")" ]] || die "拒绝操作整个用户主目录：$path"
    fi
    for previous in "${RESOLVED[@]}"; do
        if [[ "$resolved" == "$previous" || "$resolved" == "$previous/"* || "$previous" == "$resolved/"* ]]; then
            die '安装目标路径重叠；为防止误删，请检查 XDG 路径或符号链接后手动处理。'
        fi
    done
    RESOLVED+=("$resolved")
done

# 在任何修改之前验证 systemd 连接。失败时不删除程序，以免服务继续运行却丢失文件。
"${CTL[@]}" show-environment >/dev/null || die '无法连接 systemd；用户安装请正常登录后运行，不要使用 sudo -u。'
printf '程序目录：%s\n服务单元：%s\n配置目录：%s\n数据目录：%s\n' "$APP" "$UNIT" "$CONFIG" "$DATA"

# 将“是否清理数据”和“是否删除账户”分开记录，允许用户独立选择。
# 提示均在修改之前完成；只有单个 Y/y 表示同意，回车、其他输入或 EOF 均表示否。
# 回答否只跳过对应清理，不取消程序和服务的卸载。
confirm() {
    local answer=''
    printf '%s [y/N]：' "$1"
    IFS= read -r answer && [[ "$answer" == [Yy] ]]
}
REMOVE_DATA=false
REMOVE_ACCOUNT=false
if [[ "$PURGE" == true ]]; then
    # purge 是显式的非交互彻底清理选项，不读取标准输入，不再二次确认。
    REMOVE_DATA=true
    if (( EUID == 0 )); then REMOVE_ACCOUNT=true; fi
    printf '\npurge 模式：不再确认，将永久删除配置和数据；系统级同时删除 worklog 用户/组。\n'
elif (( EUID == 0 )); then
    printf '\n警告：worklog 用户/组可能在安装前就已存在；删除前请确认没有其他用途。\n'
    printf '若保留数据却删除账户，文件属主仍为原 UID/GID，重装后可能需要手动修复权限。\n'
    if confirm '是否同时删除 worklog 用户和同名组？'; then REMOVE_ACCOUNT=true; fi
    if confirm '是否同时永久删除上述配置和数据（数据库、令牌等）？'; then REMOVE_DATA=true; fi
fi

# 在修改之前检查账户管理工具和目标身份。只删除明确命名的 worklog 用户/组，
# 绝不追踪删除安装时复用账户的其他主组；即使异常配置也不允许删除 UID/GID 0。
# getent 用来识别已不存在的账户，使彻底卸载可以重复执行。
if [[ "$REMOVE_ACCOUNT" == true ]]; then
    for tool in getent userdel groupdel; do
        command -v "$tool" >/dev/null 2>&1 || die "缺少必需命令：$tool"
    done
    if account_entry="$(getent passwd worklog)"; then
        IFS=: read -r _ _ account_uid _ <<< "$account_entry"
        [[ "$account_uid" != 0 ]] || die '拒绝删除 UID 为 0 的 worklog 用户。'
    fi
    if group_entry="$(getent group worklog)"; then
        IFS=: read -r _ _ group_gid _ <<< "$group_entry"
        [[ "$group_gid" != 0 ]] || die '拒绝删除 GID 为 0 的 worklog 组。'
    fi
fi

# 先停止并禁用服务，再删除程序，避免 Restart=on-failure 导致服务重新拉起。
# 单元文件不存在时服务仍可能运行，必须同时检查运行状态；只有确实不存在且
# 已停止的服务才跳过 stop。缺失单元无需 disable，支持重复执行。
# systemctl 的实际操作一旦失败即退出，不忽略权限或停止失败等重要错误。
load_state="$("${CTL[@]}" show --property=LoadState --value worklog.service)"
active_state="$("${CTL[@]}" show --property=ActiveState --value worklog.service)"
if [[ "$load_state" != not-found || "$active_state" != inactive ]]; then
    "${CTL[@]}" stop worklog.service
fi
if [[ "$load_state" != not-found ]]; then
    "${CTL[@]}" disable worklog.service
fi
rm -f -- "$UNIT"
"${CTL[@]}" daemon-reload
rm -rf -- "$APP"

# 只清理由安装脚本约定的目录，不读取并追踪 config.json 中自定义的外部路径。
# 安装目录外的数据库（如 PostgreSQL）、自定义存储路径及备份不会被删除；
# 配置和数据目录内的所有文件（包括 backups 下的备份）都会随目录删除。
# 不关闭用户 lingering：同一用户的其他后台服务可能仍然需要它。
if [[ "$REMOVE_DATA" == true ]]; then
    rm -rf -- "$CONFIG" "$DATA"
    printf '\n已删除安装目录内的配置和数据。\n'
else
    printf '\n配置保留于 %s，数据保留于 %s。\n' "$CONFIG" "$DATA"
fi

# 在服务停止、文件清理完成后再删除账户。刻意不使用 userdel -r，避免追踪删除
# 复用账户的自定义主目录或邮件目录。只清理上面列出的安装路径。
# 某些系统的 userdel 会自动删除同名主组，因此删除用户后重新检查组是否存在。
# 不强制删除：若账户仍有其他进程，或该组仍是其他用户的主组，则报错退出。
if [[ "$REMOVE_ACCOUNT" == true ]]; then
    if getent passwd worklog >/dev/null; then userdel worklog; fi
    if getent group worklog >/dev/null; then groupdel worklog; fi
    printf 'worklog 用户和同名组已清理。\n'
elif (( EUID == 0 )); then
    printf 'worklog 用户/组已保留。\n'
fi
printf '卸载完成。外部数据库、自定义存储路径及用户 lingering 设置未作更改。\n'
