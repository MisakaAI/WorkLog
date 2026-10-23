# 使用 systemd 持久运行

适用于使用 systemd 的 Linux 主机：以专用非 root 用户运行编译后的 Worklog，支持退出 SSH 后继续运行、异常退出重启和开机自启。
本说明使用 JSON 配置文件，不依赖环境变量。服务模板见 [worklog.service](worklog.service)。

## 目录约定

| 路径 | 用途 |
| --- | --- |
| `/opt/worklog/bin/worklog` | 编译后的 Go 程序，root 管理 |
| `/opt/worklog/Web/dist` | 前端构建产物，root 管理，服务用户只读 |
| `/etc/worklog/config.json` | 配置文件，root 管理，服务用户只读 |
| `/var/lib/worklog` | 本机磁盘上的数据库、WAL、令牌及备份，服务用户可写 |
| `/etc/systemd/system/worklog.service` | systemd 服务单元 |

服务工作目录为 `/var/lib/worklog`。配置中的相对路径以该目录为基准，建议统一使用绝对路径。
SQLite 数据不要放在 NFS / SMB 上；不要把数据库、配置、令牌或备份放入前端静态目录。

## 1. 构建和安装

先在项目根目录按 [README](../README.md#部署与启动) 构建，构建过程不需要 root：

```sh
(cd Web && npm ci && npm run build)
go build -o bin/worklog ./cmd/worklog
```

创建专用系统账户（已存在则跳过），然后安装文件。以下命令仍在项目根目录执行：

```sh
sudo useradd --system --user-group --home-dir /var/lib/worklog --no-create-home --shell /usr/sbin/nologin worklog

sudo install -d -o root -g root -m 755 /opt/worklog/bin /opt/worklog/Web/dist
sudo install -o root -g root -m 755 bin/worklog /opt/worklog/bin/worklog
sudo cp -R Web/dist/. /opt/worklog/Web/dist/
sudo chown -R root:root /opt/worklog
sudo find /opt/worklog/Web/dist -type d -exec chmod 755 {} +
sudo find /opt/worklog/Web/dist -type f -exec chmod 644 {} +

sudo install -d -o worklog -g worklog -m 700 /var/lib/worklog
sudo install -d -o root -g worklog -m 750 /etc/worklog
sudo install -o root -g worklog -m 640 config.example.json /etc/worklog/config.json
```

不同发行版的账户管理命令或 `nologin` 路径可能不同，请按实际系统调整。
这里配置文件使用 `root:worklog`、`0640`，目录使用 `0750`，让管理员可修改、专用服务账户只读；与文件归服务用户所有时使用 `0600` 的方案不同，不要直接改成 `root:root 0600`，否则服务无法读取。

## 2. 编辑配置

```sh
sudoedit /etc/worklog/config.json
```

复制的模板已经使用上述部署路径。仅本机直接访问的最小配置可以是：

```json
{
  "addr": "127.0.0.1:8080",
  "db_driver": "sqlite",
  "db_dsn": "/var/lib/worklog/worklog.db",
  "static_dir": "/opt/worklog/Web/dist",
  "token_file": "/var/lib/worklog/mcp-token",
  "timezone": "Asia/Shanghai",
  "allowed_hosts": ["localhost", "127.0.0.1", "::1"],
  "allowed_origins": []
}
```

`write_token` 留空时，本机监听会自动生成／读取 `token_file`，它保存的是**读写令牌**；独立只读令牌通过 `read_token` 配置。
`serve` 启动会自动迁移数据库，无需另加 `ExecStartPre`。

### Nginx / Cloudflare Access 代理

远程入口应使用 HTTPS，并配置 Worklog 自身的凭证。Cloudflare Access 不替代 Worklog 的 Basic / Bearer 认证。
在 JSON 中设置以下字段（替换示例值，不要原样使用占位符）：

```json
{
  "basic_user": "worklog",
  "basic_password": "REPLACE_WITH_INDEPENDENT_RANDOM_PASSWORD",
  "write_token": "REPLACE_WITH_ANOTHER_INDEPENDENT_RANDOM_TOKEN",
  "allowed_hosts": ["worklog.example.com", "localhost", "127.0.0.1", "::1"],
  "allowed_origins": ["https://worklog.example.com"]
}
```

以上是应合并到完整配置中的字段，不是替换全部配置。可分别执行 `openssl rand -hex 32` 生成独立密码和令牌。Basic 密码至少 8 字节，令牌至少 32 字节。

代理应保留网站实际 `Host`。`allowed_hosts` 不带协议和端口；`allowed_origins` 使用浏览器访问的完整 Origin，不带路径或结尾 `/`，无需填写 Cloudflare Access 的登录域名。
TLS 在代理终止、上游使用 HTTP 时，也要显式加入网站的 HTTPS Origin；不要只依赖 `X-Forwarded-Proto`。

Go 默认仅监听本机地址，不要直接向公网开放 8080。Nginx 配置见[部署与配置](DEPLOYMENT.md#nginx-https-反向代理)。
Cloudflare Tunnel 若不在同一主机网络空间，需按实际网络调整监听地址；非回环监听必须显式配置写令牌和 Basic 密码。

## 3. 安装并启动服务

在项目根目录执行：

```sh
sudo install -o root -g root -m 644 docs/worklog.service /etc/systemd/system/worklog.service
sudo systemctl daemon-reload
sudo systemctl enable --now worklog.service
sudo systemctl status worklog.service --no-pager
```

`enable` 设置开机启动，`--now` 同时立即启动。首次启动时程序会创建数据库并执行迁移。

检查健康状态和日志：

```sh
curl --fail http://127.0.0.1:8080/healthz
sudo journalctl -u worklog.service -n 100 --no-pager
sudo journalctl -u worklog.service -f
```

如果删除了 `allowed_hosts` 中的本机地址，健康检查也要使用允许的 Host，例如：

```sh
curl --fail -H 'Host: worklog.example.com' http://127.0.0.1:8080/healthz
```

服务日志写入 journald，不在数据目录生成日志文件。日志是否跨主机重启保留，取决于主机 journald 的持久化与轮转配置。

## 4. 常用管理与升级

```sh
sudo systemctl restart worklog.service   # 修改 JSON 后重启
sudo systemctl stop worklog.service
sudo systemctl start worklog.service
sudo systemctl disable --now worklog.service  # 停止并取消开机启动
```

修改 `.service` 文件后，必须先执行 `sudo systemctl daemon-reload`，再重启服务；只修改 JSON 时不需要 daemon-reload。

升级前先备份。构建好新版本后，停止服务，再替换二进制及前端产物；不要覆盖运行中的可执行文件，也不要覆盖 `/etc/worklog` 或 `/var/lib/worklog`：

```sh
sudo systemctl stop worklog.service
sudo install -o root -g root -m 755 bin/worklog /opt/worklog/bin/worklog
# 清理旧的专用前端产物目录，再复制新产物；不要对数据目录执行此操作。
sudo rm -rf /opt/worklog/Web/dist
sudo install -d -o root -g root -m 755 /opt/worklog/Web/dist
sudo cp -R Web/dist/. /opt/worklog/Web/dist/
sudo chown -R root:root /opt/worklog/Web/dist
sudo find /opt/worklog/Web/dist -type d -exec chmod 755 {} +
sudo find /opt/worklog/Web/dist -type f -exec chmod 644 {} +
sudo systemctl start worklog.service
sudo systemctl status worklog.service --no-pager
```

数据库迁移可能改变结构，回滚程序不等于回滚数据库。恢复流程见[备份与恢复](OPERATIONS.md)。

## 5. 备份与服务沙箱

SQLite 可在服务运行时使用内置在线备份命令。以下命令以服务用户运行，并明确使用同一配置和工作目录：

```sh
sudo -u worklog install -d -m 700 /var/lib/worklog/backups
sudo -u worklog sh -c 'cd /var/lib/worklog && /opt/worklog/bin/worklog --conf=/etc/worklog backup "/var/lib/worklog/backups/worklog-$(date +%Y%m%d-%H%M%S).db"'
```

备份目标必须是不存在的新文件。不要仅复制运行中的 `.db`；应另行把备份安全复制到其他存储，备份与数据库位于同一磁盘不能防范磁盘损坏。PostgreSQL 使用其专用备份工具，详见[备份与恢复](OPERATIONS.md)。

示例服务启用了以下限制：

- 使用 `worklog` 用户，无额外 Linux capabilities，禁止提权。
- `ProtectSystem=strict` 使文件系统默认只读，仅 `/var/lib/worklog` 可持久写入。
- `ProtectHome=true` 隐藏用户 home；不要引用 `/home/...` 下的程序、前端或配置。
- `PrivateTmp=true` 使用服务私有临时目录，`UMask=0077` 限制新文件权限。
- 异常退出每 5 秒重启；60 秒内启动失败达到 5 次后暂停重试。正常停止不会触发重启。
- SIGTERM 触发程序优雅退出，systemd 最多等待 30 秒。

如果更改数据库／令牌位置，需同步调整目录所有权和服务的 `ReadWritePaths`，并确保 SQLite 所在目录可写，以便创建 WAL / SHM 文件。
命令行手工执行的备份不受服务单元沙箱约束，仍受执行用户的文件权限约束。

启动失败时先查看 `journalctl`，重点检查配置是否合法、服务用户能否读取配置和前端、数据目录是否可写，以及端口是否被占用。
修复后如果触发了启动频率限制：

```sh
sudo systemctl reset-failed worklog.service
sudo systemctl start worklog.service
```

本示例需要支持上述指令的 systemd；可在目标主机运行 `sudo systemd-analyze verify /etc/systemd/system/worklog.service` 检查。容器或启用 SELinux / AppArmor 的系统还可能需要额外的沙箱支持或安全策略。
