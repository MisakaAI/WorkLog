# 备份与恢复

当前没有日志历史、删除回收站或后台备份调度。请定期备份、限制权限并做恢复演练。  
备份包含私人数据，应加密保存到与服务独立的安全位置。

Worklog 业务配置仅从 JSON 读取，所有 Worklog 命令都显式指定 `--conf`，不使用环境变量或 `.env`。以下本机示例以 `./config.json` 为现用配置；请替换成实际路径。相对数据库路径以进程工作目录为基准，不是配置文件所在目录。升级前备份，避免不同版本进程并行执行迁移。

## SQLite 在线备份

**禁止只复制运行中的主 `.db` 文件**，已提交数据可能仍在 WAL 中。使用在线一致性 `VACUUM INTO`：

```sh
./bin/worklog --conf ./config.json backup ./backups/worklog-2026-10-21.db
```

备份可在 HTTP 进程运行时执行，自动检查 `integrity_check` / `foreign_key_check`，目标已存在时拒绝覆盖。  
备份只是时间点快照，不包含之后的提交。

## SQLite 恢复

先停止所有 HTTP / stdio 写入进程。复制现用配置为独立恢复配置，只修改 `db_dsn` 为**全新、尚不存在的文件路径**，保留原库回退，绝不覆盖已有数据库：

```sh
cp ./config.json ./restore.json
chmod 600 ./restore.json
```

编辑 `./restore.json` 的 `db_dsn`，例如 `"db_dsn": "/absolute/path/WorkLog/data/restored.db"`，其他业务配置保持一致。然后执行：

```sh
./bin/worklog --conf ./restore.json restore ./backups/worklog-2026-10-21.db
./bin/worklog --conf ./restore.json
```

重新查询 dashboard、任务与日志确认无误后，再将正式 JSON 配置的 `db_dsn` 切换为新库路径，并以正式配置启动：

```sh
# 先停止上面的验证进程，避免重复启动。
./bin/worklog --conf ./config.json
```

restore 校验源和目标数据库，拒绝已有目标 `.db` / `-wal` / `-shm`，写入后 fsync。若目标已存在，选另一个新路径，**不要删除原库以绕过检查**。
不要把仍在写入、依赖 WAL 的活跃数据库当作独立备份传入 restore。

## 容器

项目的 Compose 将本地 `deploy/config.json` 只读挂载为容器内 `/app/config/config.json`，并将 `worklog-data` 命名卷挂载为 `/app/data`。配置文件按[部署说明](DEPLOYMENT.md#docker-compose)授予 UID 10001 读取权限。

```sh
docker compose exec worklog /app/worklog --conf /app/config/config.json backup /app/data/backup-2026-10-21.db
docker compose cp worklog:/app/data/backup-2026-10-21.db ./backup-2026-10-21.db
```

将备份另存到安全位置，不要只保留在同一个数据卷。复制到主机后检查文件权限。

恢复时先停止所有访问该数据库的 HTTP / stdio 进程，复制配置并只修改副本的 `db_dsn` 为 `/app/data/restored.db`（必须尚不存在）：

```sh
docker compose stop worklog
sudo install -o "$(id -u)" -g "$(id -g)" -m 600 ./deploy/config.json ./restore.json
# 用当前用户编辑 restore.json：将 db_dsn 改为 /app/data/restored.db。
sudo install -o 10001 -g 10001 -m 600 ./restore.json ./deploy/restore.json
# 将备份放在本机 ./backups 中，临时只读挂载到 /restore。
docker compose run --rm --no-deps \
  -v "$PWD/backups:/restore:ro" \
  -v "$PWD/deploy/restore.json:/app/config/restore.json:ro" \
  worklog --conf /app/config/restore.json restore /restore/worklog-2026-10-21.db
```

临时容器沿用 Compose 的数据卷和 `/app/worklog` 入口，并通过 `--conf` 读取**单独挂载的恢复配置**。备份、配置文件及其父目录需允许容器 UID 10001 读取／遍历；不要为方便而将凭证设为全员可读。

保留原库，使用恢复配置启动隔离的验证实例（不要同时启动正式服务）：

```sh
docker compose run --rm --no-deps --service-ports \
  -v "$PWD/deploy/restore.json:/app/config/restore.json:ro" \
  worklog --conf /app/config/restore.json
```

确认 dashboard、任务与日志无误并停止验证实例后，将正式配置副本 `config.local.json` 的 `db_dsn` 改为 `/app/data/restored.db`，重新安装并重建服务容器。镜像默认启动命令已包含 `--conf /app/config/config.json`：

```sh
sudo install -o 10001 -g 10001 -m 600 ./config.local.json ./deploy/config.json
docker compose up -d --force-recreate worklog
```

重建容器不会删除数据卷；不要使用 `down -v`。单文件挂载在配置被替换后应重建容器，不能仅依赖重启旧容器。

不修改环境变量，也不将恢复结果覆盖回原数据库路径。

## PostgreSQL

使用 PostgreSQL 原生 `pg_dump -Fc` / `pg_restore`，而不是 SQLite 的 backup / restore 命令。原生工具使用自身的连接参数，不接受 Worklog 的 `--conf`。
使用 `.pgpass` 或其他安全凭证机制，不把密码写入共享命令或日志。

```sh
pg_dump -h db.example.com -U worklog -d worklog -Fc -f worklog.dump
# 预先创建全新的独立恢复数据库，停止写入后恢复并验证；绝不覆盖原库。
pg_restore -h db.example.com -U worklog -d worklog_restored --no-owner worklog.dump
```

复制现用 JSON 配置，只修改副本的 `db_dsn` 指向新库，验证后再切换正式配置。远程连接配置验证证书的 TLS；恢复目标用户需有相应权限。迁移不会自动删除数据库，也不会替代备份。
