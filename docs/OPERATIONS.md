# 备份与恢复

以下以 **Linux 自动安装的 systemd 系统服务、SQLite 数据库**为主例。日常只需：停服 → 备份 → 启动；CLI 恢复时停服，确认后将旧库留存为带日期的 `.bak`，在原路径恢复，成功后再启动。

- 程序：`/opt/worklog/bin/worklog`；配置：`/etc/worklog/config.json`；运行用户：`worklog`。
- 默认数据库：`/var/lib/worklog/worklog.db`。本文约定备份目录为 **`/var/lib/worklog/backups`**（脚本创建，并非程序自动调度）。
- 备份命名：`backup_$(date +%Y%m%d).db`，例如 **`backup_20261029.db`**。同日重跑拒绝覆盖；确需再备份时，手动换一个未使用的文件名。

先确认现用配置的 `db_driver`、`db_dsn`、`token_file`；修改过安装路径或服务用户时需相应调整。业务配置只从 JSON 读取，不使用环境变量或 `.env`；**相对路径以进程工作目录为准，不是配置文件所在目录**。不要把浏览器访问的 `/config.json` 当成服务配置。

当前没有日志历史、删除回收站或后台备份调度。请定期备份并演练恢复；升级前先备份，不要让不同版本进程并行迁移。备份含私人数据，应加密另存到独立安全位置，不能只留在数据目录（卸载清理可能连同备份一起删除）。[配置与凭证也要另行备份](#配置与凭证另行备份)。

## 1. 停服备份（推荐）

先停止其他访问同一数据库的 HTTP／stdio 写入进程，不要只关浏览器。备份脚本见 [`scripts/worklog-backup.sh`](../scripts/worklog-backup.sh)：只处理原本正在运行的系统服务，先检查路径和权限，再停服；备份失败或中断也会尝试重新启动服务。

在仓库根目录执行（**会短暂停服**）：

```bash
sudo /bin/bash scripts/worklog-backup.sh
```

CLI 使用 `VACUUM INTO` 创建一致性快照，并检查 `integrity_check`／`foreign_key_check`；备份文件以 0600 创建，已有目标不会被覆盖。**禁止只复制运行中的主 `.db` 文件**，否则可能遗漏 WAL 中的已提交数据。命令失败后留下的文件不能当作有效备份，不要直接拿去恢复。

脚本不在恢复流程中使用，也不会启动原本已停止的服务。自动重新启动仅是尽力而为（断电或强制终止无法保证）；执行后检查 `sudo systemctl status worklog.service --no-pager`，启动失败时查看 `sudo journalctl -u worklog.service -n 100 --no-pager`。备份只是时间点快照，不包含后续提交。

### 可选：cron 自动备份

复用同一份[停服备份脚本](../scripts/worklog-backup.sh)，不另写一套逻辑：使用 `sudoedit` 将脚本完整内容保存到 `/usr/local/sbin/worklog-backup.sh`（包含首行 `#!/bin/bash`）；若文件已存在，先检查内容再合并，不要清空或直接覆盖已有脚本：

```bash
sudoedit /usr/local/sbin/worklog-backup.sh
sudo chown root:root /usr/local/sbin/worklog-backup.sh
sudo chmod 0700 /usr/local/sbin/worklog-backup.sh
```

脚本默认 `compress_zstd=1`，须提前安装 `zstd`，或在脚本中改为 `0`。cron 以 root 停启系统服务，但脚本中的备份命令仍保留 `sudo -u worklog`。先检查语法，再手动执行验证（**会短暂停服**）；手动执行也使用同一把锁：

```bash
sudo /bin/bash -n /usr/local/sbin/worklog-backup.sh
# 仅在语法检查通过后执行：
sudo env PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin /usr/bin/flock -n /run/lock/worklog-backup.lock /bin/bash /usr/local/sbin/worklog-backup.sh
```

确认备份结果和服务状态正常后再启用定时任务。同日目标 `.db` 或 `.db.zst` 已存在会拒绝执行，**不要删除已有备份来绕过检查**；原服务已停止时脚本也会退出，不会将其启动。

先预建 root 私有日志（已有日志只调整权限，不清空），再用 `sudo crontab -e` 编辑 **root 的 crontab**：

```bash
sudo sh -c 'umask 077; touch /var/log/worklog-backup.log'
sudo chown root:root /var/log/worklog-backup.log
sudo chmod 0600 /var/log/worklog-backup.log
sudo crontab -e
```

加入以下内容，示例为**每天机器本地时区 03:00**。确认本机 `flock` 位于 `/usr/bin/flock`，否则替换为实际绝对路径：

```cron
PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
0 3 * * * /usr/bin/flock -n /run/lock/worklog-backup.lock /bin/bash /usr/local/sbin/worklog-backup.sh >> /var/log/worklog-backup.log 2>&1
```

`flock -n` 持锁至脚本结束；已有任务持锁时直接跳过，避免 cron 重叠。日期计算留在脚本内，**不要把含 `%` 的日期命令直接放进 crontab**（cron 会特殊处理 `%`）。定期检查日志和服务状态；日志持续追加，按需配置轮转并保持 root 所有、0600 权限。

系统未安装或未启用 cron／crond 时，需按发行版安装并启用相应服务（包名、服务名不同）。cron 不补跑关机期间错过的任务。脚本不自动清理历史备份，需自行规划留存、空间和加密异地保存。**恢复前先暂停该 cron 条目并确认正在执行的备份已结束，避免与恢复冲突；恢复及验证完成后再按需启用。**

## 2. 恢复现用数据库

**`restore` 的参数是独立 SQLite 备份文件，目标由现用配置的 `db_dsn` 决定，无需修改配置。** 目标已存在时，CLI 校验备份后显示 `[y/N]` 确认提示；只有输入完整一行 `Y` 或 `y` 并回车才继续。直接回车默认取消；`yes`、带空格的输入、其他输入或 EOF 也取消，原库不变，命令返回非零。

确认后，旧库留存为 `target.YYYYMMDD.bak`（日期取执行命令时的本地日期），再在原目标路径恢复。例如 `/var/lib/worklog/worklog.db` 留存为 `/var/lib/worklog/worklog.db.20261029.bak`。同日 `.bak` 或其 `-wal`／`-shm` 已存在时拒绝覆盖。目标不存在时不询问，直接创建新库；源与目标不能是同一文件，已有目标必须是普通文件，不能是符号链接。

**① 选择备份。** 以下示例使用 `backup_20261029.db`；若是 `.db.zst`，必须先按下一节手动解压为 `.db`。备份源必须是普通文件，不能是文件符号链接（请使用真实文件路径）；不能使用仍在写入、依赖活跃 WAL 的数据库文件充当独立备份。

```bash
backup_file=/var/lib/worklog/backups/backup_20261029.db
```

**② 停服 → 以服务用户恢复（按提示确认）→ 成功后启动。** 先停止其他访问同一数据库的 HTTP／stdio 进程，不要只关浏览器。沿用上面终端中的 `backup_file`：

```bash
sudo systemctl stop worklog.service &&
    sudo -u worklog /opt/worklog/bin/worklog --conf /etc/worklog/config.json restore "$backup_file" &&
    sudo systemctl start worklog.service &&
    sudo systemctl status worklog.service --no-pager
```

取消或恢复失败时，`&&` 不会启动服务，应保持停服并检查错误。若已完成旧库留存，后续恢复失败也会保留 `.bak`，不会自动回滚；确认前取消或校验失败则原库不变。不要删除 `.bak` 来绕过同日冲突，也不要不检查错误就重复恢复或启动服务。恢复会校验源和目标，写入后 fsync；启动后检查 dashboard、任务与日志，确认无误后再按留存策略管理 `.bak`。升级后的迁移可能不可逆，切回旧程序不等于恢复旧数据库。

**目标 `-wal`／`-shm` 存在时，即使为空也拒绝恢复。** 应先停止所有访问该库的进程，让 SQLite 正常完成 checkpoint 和关闭；仍有冲突时先排查，**不能手动删除 WAL，也不要删除或移走 WAL／SHM 来绕过检查**。

**若此前误用 root 恢复导致数据库 owner 错误**，先停服并确认其他同库进程已停止，再执行以下命令修正主库所有权；后续恢复始终使用上面的 `sudo -u worklog` 命令：

```bash
sudo chown worklog:worklog /var/lib/worklog/worklog.db
```

## 可选：zstd 压缩与手动解压

主备份脚本顶部的 `compress_zstd=1` 默认开启压缩，设为 `0` 可关闭。开启时在停服前检查 `zstd`；备份成功后先启动服务、确认运行并解除 trap，再压缩同一个 `backup_file`，不延长停服时间。保留原 `.db`，生成同名 `.db.zst`（例如 `backup_20261029.db.zst`），不强制覆盖。压缩失败会返回非零且不输出成功提示；服务已恢复，原 `.db` 仍保留。

**WorkLog 不会自动解压 `.zst`。restore 前必须手动解压为 `.db`。** 因原 `.db` 可能仍存在，以下另取未使用的输出名，不覆盖已有文件：

```bash
sudo -u worklog bash <<'BASH'
set -euo pipefail
umask 077
command -v zstd >/dev/null || { echo '请先安装 zstd' >&2; exit 1; }
backup_file=/var/lib/worklog/backups/backup_20261029.unpacked.db
zstd -d -q -k -o "$backup_file" -- /var/lib/worklog/backups/backup_20261029.db.zst
chmod 600 "$backup_file"
BASH
```

解压成功后，在恢复步骤①中设置 `backup_file=/var/lib/worklog/backups/backup_20261029.unpacked.db`。解压失败时不要使用输出文件，也不要强制覆盖重试，应检查错误并另取新输出名。

## 补充：在线备份与其他部署方式

### SQLite 在线备份

允许 HTTP 服务运行时执行同一 `backup` 命令；先按主流程准备私有备份目录，选未使用的文件名。使用与正在运行的服务**相同版本**的程序和配置，因为 CLI 打开数据库也会执行迁移：

```bash
backup_file="/var/lib/worklog/backups/backup_$(date +%Y%m%d).db"
sudo -u worklog sh -c 'cd /var/lib/worklog && /opt/worklog/bin/worklog --conf /etc/worklog/config.json backup "$1"' sh "$backup_file"
```

### 路径与命令替换

CLI 恢复原则不变：停止所有访问同一数据库的进程，以实际运行用户和现用配置执行 `restore`；已有目标需输入完整一行 `Y`／`y` 并回车确认，旧库留存为 `target.YYYYMMDD.bak`，回车默认取消。无需切换 `db_dsn`；仅恢复成功后启动并验证。桌面导入及下文 Compose 的新目标恢复流程保持不变。

| 运行方式 | 调整要点 |
| --- | --- |
| 当前用户 systemd 服务 | 使用 `systemctl --user stop worklog.service`／`systemctl --user start worklog.service`，不加 sudo；程序为 `~/.local/share/worklog/app/bin/worklog`，配置为 `~/.config/worklog/config.json`，数据在 `~/.local/share/worklog/data`。自定义 XDG 路径见[安装说明](DEPLOYMENT.md#当前用户服务) |
| 手动 CLI | 使用实际程序及 `--conf /绝对路径/config.json`。内置默认数据库／令牌为 `./data/worklog.db`、`./data/mcp-token`，仓库模板则使用 `/var/lib/worklog/`，不要混用 |
| Windows 桌面版 | 默认工作目录为 `%APPDATA%\WorklogDesktop`，数据库为其下 `data\worklog.db`，配置存在时读取该目录的 `config.json` |
| macOS 桌面版 | 默认工作目录为 `~/Library/Application Support/WorklogDesktop`，数据库为其下 `data/worklog.db`，配置存在时读取该目录的 `config.json` |
| Compose／PostgreSQL | 见下文，SQLite 命令不适用于 PostgreSQL |

桌面版显式传入 `--conf`、修改 `db_dsn` 或导入备份后，以实际路径为准；恢复前退出桌面应用及其他写入进程。

## 容器

<details>
<summary>Compose 部署的备份与恢复（systemd 用户可跳过）</summary>

项目的 Compose 将本地 `deploy/config.json` 只读挂载为容器内 `/app/config/config.json`，并将 `worklog-data` 命名卷挂载为 `/app/data`。配置文件按[部署说明](DEPLOYMENT.md#docker-compose)授予 UID 10001 读取权限。PostgreSQL Compose 部署执行下列命令时，应同部署时一样添加 `-f compose.yaml -f compose.postgres.yaml`；SQLite 的备份／恢复命令不适用于 PostgreSQL。

以下在 Compose 项目目录执行。容器中由 UID 10001 创建唯一备份目录，再复制到主机的独立私有目录：

```sh
umask 077
mkdir -p ./backups
backup_dir=$(mktemp -d "$PWD/backups/container-XXXXXXXX")
container_backup_dir=$(docker compose exec -T worklog mktemp -d /app/data/backup-XXXXXXXX)
backup_name="worklog-$(basename "$container_backup_dir").db"
docker compose exec -T worklog /app/worklog --conf /app/config/config.json backup "$container_backup_dir/$backup_name"
docker compose cp "worklog:$container_backup_dir/$backup_name" "$backup_dir/$backup_name"
chmod 600 "$backup_dir/$backup_name"
backup_file="$backup_dir/$backup_name"
```

将备份加密另存到安全位置，不要只保留在同一个数据卷。主机副本核验并异地保存后，可按留存策略清理卷内备份；不要把数据库备份当作配置或凭证备份。

恢复前先按下节[单独备份配置与凭证](#配置与凭证另行备份)，再停止所有访问该数据库的 HTTP／stdio 进程。复制配置并只修改副本的 `db_dsn` 为 `/app/data/restored.db`（必须尚不存在）：

```sh
docker compose stop worklog
sudo install -o "$(id -u)" -g "$(id -g)" -m 600 ./deploy/config.json ./restore.json
# 用当前用户编辑 restore.json：将 db_dsn 改为 /app/data/restored.db。
sudo install -o 10001 -g 10001 -m 600 ./restore.json ./deploy/restore.json
# backup_file 为选定备份的绝对路径；仅将副本交给容器 UID 10001。
restore_dir=$(mktemp -d /tmp/worklog-restore-XXXXXXXX)
sudo install -o 10001 -g 10001 -m 600 "$backup_file" "$restore_dir/worklog.db"
sudo chown 10001:10001 "$restore_dir"
sudo chmod 700 "$restore_dir"
docker compose run --rm --no-deps \
  -v "$restore_dir:/restore:ro" \
  -v "$PWD/deploy/restore.json:/app/config/restore.json:ro" \
  worklog --conf /app/config/restore.json restore /restore/worklog.db
```

临时容器沿用 Compose 的数据卷和 `/app/worklog` 入口，并通过 `--conf` 读取**单独挂载的恢复配置**。上述命令保持配置／备份为 0600、暂存目录为 0700，并将所有权交给 UID:GID 10001:10001；原备份的权限和所有权不变。容器只需遍历挂载后的目录，不必放开主机整个备份目录。不要使用 `chmod 644`、`chmod -R 777` 或全员可读权限来绕过错误。

这些命令适用于常规 Linux Docker。若启用了 rootless、user namespace 映射或 SELinux，需按主机策略核实映射后的 UID 和挂载标签；不要直接放宽权限。

保留原库，使用恢复配置启动隔离的验证实例（不要同时启动正式服务）：

```sh
docker compose run --rm --no-deps --service-ports \
  -v "$PWD/deploy/restore.json:/app/config/restore.json:ro" \
  worklog --conf /app/config/restore.json
```

确认 dashboard、任务与日志无误并停止验证实例后，将已验证的恢复配置安装为正式配置，再重建服务容器。原配置应已按下节单独备份；镜像默认启动命令已包含 `--conf /app/config/config.json`：

```sh
sudo install -o 10001 -g 10001 -m 600 ./deploy/restore.json ./deploy/config.json
# 同步日后部署使用的配置副本，避免下次升级切回原数据库：
sudo install -o "$(id -u)" -g "$(id -g)" -m 600 ./deploy/restore.json ./config.local.json
docker compose up -d --force-recreate worklog
```

重建容器不会删除数据卷；不要使用 `down -v`。单文件挂载在配置被替换后应重建容器，不能仅依赖重启旧容器。

恢复命令结束、不再使用暂存挂载后，仅清理此次暂存副本：

```sh
sudo rm -- "$restore_dir/worklog.db"
sudo rmdir -- "$restore_dir"
```

不修改环境变量，也不将恢复结果覆盖回原数据库路径。

</details>

## 配置与凭证另行备份

数据库快照不包含服务 JSON、Basic 密码、Bearer 令牌文件、代理 TLS 密钥或部署配置。单独加密保存现用配置与凭证，限制访问范围，并记录对应版本、数据库备份路径和备份时间；不要把凭证写进公开备份清单。

本机示例（容器的现用配置改为 `./deploy/config.json`；若当前用户无权读取，使用 `sudo install -o "$(id -u)" -g "$(id -g)" -m 600 ...` 复制，不要更改原文件权限）：

```sh
umask 077
mkdir -p ./backups
secrets_dir=$(mktemp -d "$PWD/backups/secrets-XXXXXXXX")
install -m 600 ./config.json "$secrets_dir/config.json"
# 仅在使用自动令牌文件时执行，并替换成配置中的实际 token_file：
install -m 600 /actual/path/mcp-token "$secrets_dir/mcp-token"
```

显式设置 `write_token` 时，有效写令牌在 JSON 中，不应把旧令牌文件当成现用凭证。恢复到隔离实例时不要意外对外暴露原凭证；若怀疑泄露，按[凭证轮换说明](AUTHENTICATION.md#5-凭证保管与轮换)处理。

## PostgreSQL

使用 PostgreSQL 原生 `pg_dump -Fc` / `pg_restore`，而不是 SQLite 的 backup / restore 命令。原生工具使用自身的连接参数，不接受 WorkLog 的 `--conf`。
使用 `.pgpass` 或其他安全凭证机制，不把密码写入共享命令或日志。原生工具不会读取 WorkLog JSON 的 TLS 设置；下面显式启用证书验证，请替换主机、用户和 CA 证书的绝对路径，并先准备好证书。

```sh
umask 077
mkdir -p ./backups
backup_dir=$(mktemp -d "$PWD/backups/postgres-XXXXXXXX")
backup_file="$backup_dir/worklog-$(basename "$backup_dir").dump"
pg_dump --dbname='host=db.example.com user=worklog dbname=worklog sslmode=verify-full sslrootcert=/absolute/path/ca.crt' \
  -Fc -f "$backup_file"
# 请数据库管理员预先创建空的 worklog_restored，并授予 worklog 恢复权限。
# 停止写入后恢复并验证，绝不覆盖原库。
pg_restore --dbname='host=db.example.com user=worklog dbname=worklog_restored sslmode=verify-full sslrootcert=/absolute/path/ca.crt' \
  --no-owner "$backup_file"
```

复制现用 JSON 配置，只修改副本的 `db_dsn` 指向新库，验证后再切换正式配置。远程连接配置验证证书的 TLS；恢复目标用户需有相应权限。迁移不会自动删除数据库，也不会替代备份。
