# 备份与恢复

当前没有日志历史、删除回收站或后台备份调度。请定期备份、限制权限并做恢复演练。  
备份包含私人数据，应加密保存到与服务独立的安全位置。

WorkLog 业务配置仅从 JSON 读取，不使用环境变量或 `.env`。下文所有 WorkLog 命令都显式指定 `--conf`，本机示例以 `./config.json` 为现用配置，请替换成实际路径。升级前先备份，不要让不同版本进程并行执行迁移。

## 先找到数据与配置

以正在运行的实例为准，先确认启动参数中的 `--conf`，再查看配置里的 `db_driver`、`db_dsn` 和 `token_file`。**相对路径以进程工作目录为基准，不是配置文件所在目录。**

| 运行方式 | 数据与配置位置 |
| --- | --- |
| 命令行服务 | 内置默认数据库为 `./data/worklog.db`、令牌为 `./data/mcp-token`；仓库的 `config.example.json` 则使用 `/var/lib/worklog/`，不能混用两者 |
| Linux systemd | 系统服务数据为 `/var/lib/worklog`，配置为 `/etc/worklog/config.json`；用户服务路径见[安装说明](DEPLOYMENT.md#当前用户服务) |
| Compose | 主机 `deploy/config.json` 挂载为 `/app/config/config.json`；命名卷 `worklog-data` 挂载为 `/app/data`，示例数据库为 `/app/data/worklog.db`。卷的实际名称可能带 Compose 项目前缀，可用 `docker compose config --volumes` 和 `docker volume inspect <实际卷名>` 定位 |
| Windows 桌面版 | 默认工作目录为 `%APPDATA%\WorklogDesktop`，数据库为其下 `data\worklog.db`；配置存在时默认读取该目录的 `config.json` |
| macOS 桌面版 | 默认工作目录为 `~/Library/Application Support/WorklogDesktop`，数据库为其下 `data/worklog.db`；配置存在时默认读取该目录的 `config.json` |
| PostgreSQL | 数据在 `db_dsn` 指定的 PostgreSQL 实例中，不在本地 SQLite 文件或容器卷中 |

桌面版显式传入 `--conf` 或修改过 `db_dsn` 时，以实际配置为准；导入备份后数据库路径也可能变化。不要把浏览器可访问的 `/config.json` 当成服务配置，它只含公开前端设置。

## SQLite 在线备份

**禁止只复制运行中的主 `.db` 文件**，已提交数据可能仍在 WAL 中。使用在线一致性 `VACUUM INTO`：

每次创建独立目录，并用目录的随机后缀命名备份，避免同一天重试时撞名。以下为 POSIX shell 示例，后续恢复沿用本次的 `backup_file`：

```sh
umask 077
mkdir -p ./backups
backup_dir=$(mktemp -d "$PWD/backups/sqlite-XXXXXXXX")
backup_file="$backup_dir/worklog-$(basename "$backup_dir").db"
./bin/worklog --conf ./config.json backup "$backup_file"
```

备份可在 HTTP 进程运行时执行，自动检查 `integrity_check` / `foreign_key_check`，目标已存在时拒绝覆盖。  
备份只是时间点快照，不包含之后的提交。

### Linux systemd 安装的备份命令

系统服务使用安装的程序和配置，以服务用户执行：

```sh
sudo -u worklog sh -c 'cd /var/lib/worklog && dir=$(mktemp -d /var/lib/worklog/backup-XXXXXXXX) && /opt/worklog/bin/worklog --conf=/etc/worklog backup "$dir/worklog.db"'
```

当前用户服务使用默认路径时：

```sh
backup_dir=$(mktemp -d "$HOME/.local/share/worklog/data/backup-XXXXXXXX")
"$HOME/.local/share/worklog/app/bin/worklog" --conf="$HOME/.config/worklog/config.json" backup "$backup_dir/worklog.db"
```

自定义过 XDG 路径时相应替换。上面备份位于服务数据目录，卸载清理也可能删除它，完成后应复制到目录之外的安全存储，并核对副本权限。

## SQLite 恢复

先停止所有 HTTP／stdio 写入进程。systemd 系统服务用 `sudo systemctl stop worklog.service`，用户服务用 `systemctl --user stop worklog.service`；不要只关浏览器。以服务运行用户执行恢复，使用对应的程序、配置和路径。

以下示例用于手动运行的 CLI。复制现用配置为独立恢复配置，只修改 `db_dsn` 为全新、尚不存在的文件路径，保留原库回退，绝不覆盖已有数据库：

```sh
install -m 600 ./config.json ./restore.json
```

编辑 `./restore.json` 的 `db_dsn`，例如 `"db_dsn": "/absolute/path/WorkLog/data/restored.db"`，其他业务配置保持一致。然后执行：

```sh
# 新终端中先将 backup_file 设为选定备份的绝对路径。
./bin/worklog --conf ./restore.json restore "$backup_file"
./bin/worklog --conf ./restore.json
```

重新查询 dashboard、任务与日志确认无误后，再将正式 JSON 配置的 `db_dsn` 切换为新库路径，并以正式配置启动：

```sh
# 先停止上面的验证进程，避免重复启动。
./bin/worklog --conf ./config.json
```

systemd 部署应将新库保存在服务可写的数据目录，系统服务默认沙箱不允许写到任意外部路径。验证并切换正式配置后，使用对应的 `systemctl start worklog.service`（用户服务加 `--user`）启动，不再保留手动验证进程。

restore 校验源和目标数据库，拒绝已有目标 `.db` / `-wal` / `-shm`，写入后 fsync。若目标已存在，选另一个新路径，**不要删除原库以绕过检查**。
不要把仍在写入、依赖 WAL 的活跃数据库当作独立备份传入 restore。

## 容器

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
