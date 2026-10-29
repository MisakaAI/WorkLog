#!/bin/bash
set -euo pipefail
compress_zstd=1  # 设为 0 关闭压缩
umask 077
if (( compress_zstd )); then
    sudo -u worklog sh -c 'command -v zstd >/dev/null' || { echo '请先安装 zstd，或设 compress_zstd=0' >&2; exit 1; }
fi
backup_file="/var/lib/worklog/backups/backup_$(date +%Y%m%d).db"
install -d -o worklog -g "$(id -gn worklog)" -m 700 /var/lib/worklog/backups
cd /var/lib/worklog
sudo -u worklog test -x /opt/worklog/bin/worklog
sudo -u worklog test -r /etc/worklog/config.json
sudo -u worklog test -w /var/lib/worklog/backups
for file in "$backup_file" "$backup_file.zst"; do
    if [[ -e "$file" || -L "$file" ]]; then
        echo "拒绝覆盖：$file" >&2
        exit 1
    fi
done
systemctl is-active --quiet worklog.service
restart_service() {
    rc=$?
    if ! systemctl start worklog.service; then
        echo '启动失败，请检查 journalctl -u worklog.service；备份是否成功以命令结果为准。' >&2
        rc=1
    fi
    exit "$rc"
}
trap restart_service EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
systemctl stop worklog.service
sudo -u worklog /opt/worklog/bin/worklog --conf /etc/worklog/config.json backup "$backup_file"
systemctl start worklog.service
systemctl is-active --quiet worklog.service
trap - EXIT INT TERM
if (( compress_zstd )); then
    sudo -u worklog zstd -q -k -- "$backup_file"
    sudo -u worklog chmod 600 "$backup_file.zst"
    echo "压缩成功：$backup_file.zst（原 .db 保留）"
fi
echo "备份成功：$backup_file"
