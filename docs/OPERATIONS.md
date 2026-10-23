# 备份与恢复

当前没有日志历史、删除回收站或后台备份调度。请定期备份、限制权限并做恢复演练。  
备份包含私人数据，应加密保存到与服务独立的安全位置。

命令使用与服务一致的数据库配置；Go 不自动读取 `.env`，运行前需显式载入。升级前备份，避免不同版本进程并行执行迁移。

## SQLite 在线备份

**禁止只复制运行中的主 `.db` 文件**，已提交数据可能仍在 WAL 中。使用在线一致性 `VACUUM INTO`：

```sh
./bin/worklog backup ./backups/worklog-2026-10-21.db
```

备份可在 HTTP 进程运行时执行，自动检查 `integrity_check` / `foreign_key_check`，目标已存在时拒绝覆盖。  
备份只是时间点快照，不包含之后的提交。

## SQLite 恢复

先停止所有 HTTP / stdio 写入进程，恢复到**新文件**，保留原库回退：

```sh
WORKLOG_DB_DSN=./data/restored.db ./bin/worklog restore ./backups/worklog-2026-10-21.db
WORKLOG_DB_DSN=./data/restored.db ./bin/worklog
```

重新查询 dashboard、任务与日志确认无误后，再切换正式配置。  
restore 校验源和目标数据库，拒绝已有目标 `.db` / `-wal` / `-shm`，写入后 fsync。  
不要把仍在写入、依赖 WAL 的活跃数据库当作独立备份传入 restore。

## 容器

```sh
docker compose exec worklog /app/worklog backup /app/data/backup-2026-10-21.db
docker compose cp worklog:/app/data/backup-2026-10-21.db ./backup-2026-10-21.db
```

将备份另存到安全位置，不要只保留在同一个数据卷。复制到主机后检查文件权限。

恢复时先停服务，用挂载同一卷的临时容器执行 restore 到新的数据库路径：

```sh
docker compose stop worklog
# 将备份放在本机 ./backups 中，临时只读挂载到 /restore。
docker compose run --rm --no-deps \
  -v "$PWD/backups:/restore:ro" \
  -e WORKLOG_DB_DSN=/app/data/restored.db \
  worklog restore /restore/worklog-2026-10-21.db
```

备份目录和文件需允许容器 UID 10001 读取。  
确认恢复结果后，将 Compose 中 `WORKLOG_DB_DSN` 改为 `/app/data/restored.db` 再启动；  
当前默认 Compose 将 DSN 固定为 `/app/data/worklog.db`，仅修改 `.env` 不会改变它。

## PostgreSQL

使用 PostgreSQL 原生 `pg_dump -Fc` / `pg_restore`，而不是 SQLite 的 backup / restore 命令。  
使用 `.pgpass` 或其他安全凭证机制，不把密码写入共享命令或日志。

```sh
pg_dump -h db.example.com -U worklog -d worklog -Fc -f worklog.dump
# 预先创建独立的恢复数据库，停止写入后恢复并验证，再切换 DSN。
pg_restore -h db.example.com -U worklog -d worklog_restored --no-owner worklog.dump
```

远程连接配置验证证书的 TLS；恢复目标用户需有相应权限。迁移不会自动删除数据库，也不会替代备份。
