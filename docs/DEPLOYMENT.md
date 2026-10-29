# 部署与配置

本文介绍 Linux 服务安装、Docker、数据库配置和 HTTPS 访问。桌面版见 [Windows](WINDOWS_DESKTOP.md)／[macOS](MACOS_DESKTOP.md)，源码运行和发布打包见[开发文档](DEVELOPMENT.md)。

安装完成后，先在本机创建一条任务并刷新，确认数据已保存，再配置远程访问。WorkLog 是单用户系统；不要把一个实例当作多账号服务。

## Linux systemd 部署

### 1. 下载并解压

目标主机需要 Linux、systemd、Bash、GNU 常用文件工具、支持 zstd 的 tar 和 `zstd`。不需要 Go、Node.js 或独立数据库。Debian／Ubuntu 可运行 `sudo apt install zstd` 安装解压依赖。

从 [Releases](https://github.com/MisakaAI/WorkLog/releases) 下载对应文件：

| `uname -m` | 发布包 |
| --- | --- |
| `x86_64` | `worklog-v<版本>-linux-x86_64.tar.zst`（amd64） |
| `aarch64`／`arm64` | `worklog-v<版本>-linux-arm64.tar.zst` |

以下以 x86_64 为例，将 `<版本>` 替换为实际版本：

```sh
tar --zstd -xf worklog-v<版本>-linux-x86_64.tar.zst
cd worklog-v<版本>-linux-x86_64
```

包内包含后端 `bin/worklog`、前端 `Web/dist/`、安装／卸载脚本、配置示例、systemd 模板和文档。安装脚本依赖这个布局，**不要直接在源码仓库运行 `scripts/install-linux.sh`**。需要自行打包时见[构建发布包](DEVELOPMENT.md#构建发布包)。

### 2. 选择一种安装方式

#### 系统服务

```sh
sudo bash ./install.sh
```

脚本创建或复用 `worklog` 用户，服务不以 root 运行。安装完成后自动启动并设置开机自启。

| 路径 | 用途 |
| --- | --- |
| `/opt/worklog/bin/worklog` | 后端程序，root 管理 |
| `/opt/worklog/Web/dist` | 前端文件，服务用户只读 |
| `/etc/worklog/config.json` | 配置，root 管理、服务主组只读（0640） |
| `/var/lib/worklog` | 数据库、WAL 和令牌，服务用户可写（0700） |
| `/etc/systemd/system/worklog.service` | 服务单元 |

模板见 [worklog.service](worklog.service)。复用已有用户时，配置文件使用该用户实际主组。

#### 当前用户服务

不使用 sudo：

```sh
bash ./install.sh
```

脚本不创建用户，以当前用户运行 `systemd --user` 服务。

| 路径 | 用途 |
| --- | --- |
| `~/.local/share/worklog/app/bin/worklog` | 后端程序 |
| `~/.local/share/worklog/app/Web/dist` | 前端文件 |
| `~/.config/worklog/config.json` | 配置（0600） |
| `~/.local/share/worklog/data` | 数据和令牌（0700） |
| `~/.config/systemd/user/worklog.service` | 用户服务单元 |

支持用绝对路径的 `XDG_DATA_HOME` 和 `XDG_CONFIG_HOME` 调整基础目录。这些变量仅影响安装路径，不是应用业务配置。

请在正常登录会话中执行，不要简单使用 `sudo -u`。脚本会先检查 systemd 用户管理器是否可访问。需要开机运行、退出登录后仍保持运行时，由管理员执行：

```sh
sudo loginctl enable-linger 用户名
```

系统服务和用户服务默认都使用 `127.0.0.1:12450`。不要同时运行两份而不调整端口和数据路径。

### 3. 确认启动成功

首次安装自动生成使用绝对路径的 SQLite 配置，创建数据库、执行迁移并生成 MCP 写令牌，无需单独初始化。

```sh
curl --fail http://127.0.0.1:12450/healthz
```

预期返回 `{"ok":true}`。打开 **http://127.0.0.1:12450**，创建任务或日志，保存后刷新确认仍在。

系统服务检查：

```sh
sudo systemctl status worklog.service --no-pager
sudo journalctl -u worklog.service -n 100 --no-pager
sudo systemctl restart worklog.service
```

用户服务检查：

```sh
systemctl --user status worklog.service --no-pager
journalctl --user -u worklog.service -n 100 --no-pager
systemctl --user restart worklog.service
```

启动失败时先看日志中的配置、权限、端口占用和路径错误。触发启动频率限制后，修复原因，再执行对应的 `systemctl [--user] reset-failed worklog.service` 并启动；`[--user]` 表示用户服务才添加该选项。日志保留与轮转由主机 journald 配置决定。

### 4. 修改配置与升级

系统配置为 `/etc/worklog/config.json`，用户配置默认为 `~/.config/worklog/config.json`。修改 JSON 后重启服务，不需要 `daemon-reload`；修改服务单元后才需要对应的 `systemctl [--user] daemon-reload`。

**先[备份](OPERATIONS.md)，再升级。** 解压新版，用与原安装相同的身份及 XDG 路径重新运行 `install.sh`：

- 保留已有配置和数据，不自动合并新配置字段。
- 准备好新文件后停止服务，再替换程序、整套前端和服务单元，最后启动。
- 自定义服务单元会被覆盖，请先备份；持久调整优先使用 systemd drop-in。
- 不保证失败后自动回滚。数据库迁移可能不可逆，换回旧程序不等于恢复旧数据库。

系统服务只允许状态目录持久写入，并启用只读文件系统、隐藏 home、禁止提权等限制。若改用自定义数据或令牌路径，需同步调整权限和沙箱。用户服务以当前用户权限运行，不提供同等文件系统隔离。两者均使用 `UMask=0077`、异常退出重启和 SIGTERM 优雅退出。

安装脚本不配置防火墙、代理或 TLS，也不自动处理 SELinux／AppArmor 策略。可用 `sudo systemd-analyze verify /etc/systemd/system/worklog.service` 检查系统服务指令兼容性。

### 5. 卸载

先将备份复制到安装数据目录之外。使用与安装相同的身份和 XDG 环境：

```sh
sudo bash ./uninstall.sh  # 系统服务
bash ./uninstall.sh       # 用户服务，不使用 sudo
```

两种方式都会停止并禁用服务，删除程序和服务单元：

- 系统级默认分别询问是否删除 `worklog` 用户／同名组、是否删除配置／数据。仅 `Y`／`y` 同意，回车、其他输入和 EOF 均保留；保留数据不影响程序卸载。
- 用户级默认不询问，保留配置和数据，不操作系统用户或组。

`purge`（也支持 `--purge`）**不询问、不二次确认，直接删除，无法撤销**：

```sh
sudo bash ./uninstall.sh purge  # 删除程序、服务、配置、数据和 worklog 用户／同名组
bash ./uninstall.sh purge       # 删除当前用户安装的程序、服务、配置和数据
```

清理只覆盖上述约定目录，不追踪配置中的外部路径，也不删除 PostgreSQL 数据库。数据目录内的所有文件（包括其中的备份）都会被删除。

系统级删除账户时，即使账户在安装前已存在，也会尝试删除。请确认未被其他服务使用。不会删除其他名称的主组，不使用 `userdel -r` 删除自定义主目录；账户仍被进程使用或组被其他用户作为主组时可能失败，已完成的卸载不回滚。只删账户而保留数据后，重装可能需要修复 UID／GID 权限。

卸载不关闭 lingering，以免影响其他用户服务。卸载脚本也可通过源码中的 `scripts/uninstall-linux.sh` 运行，不依赖发布包布局。

## JSON 配置

业务配置仅来自 JSON 和内置默认值，不读取 `WORKLOG_*` 环境变量或 `.env`。普通 CLI 不自动搜索配置文件；桌面版默认配置路径见平台文档。

`--conf` 可指定文件或目录（目录中读取 `config.json`），必须放在子命令之前：

```sh
./bin/worklog --conf=/absolute/path/config.json
./bin/worklog --conf=/absolute/path/config.json --help
```

模板见 [config.example.json](../config.example.json)。安装脚本已创建配置时，直接修改现用文件，不要覆盖成模板。手动部署应先创建服务用户可写的数据目录，使用绝对路径，例如：

```json
{
  "addr": "127.0.0.1:12450",
  "db_driver": "sqlite",
  "db_dsn": "/var/lib/worklog/worklog.db",
  "static_dir": "/opt/worklog/Web/dist",
  "token_file": "/var/lib/worklog/mcp-token",
  "timezone": "Asia/Shanghai"
}
```

**配置内部的相对路径以进程工作目录为准，不是配置文件目录。** systemd 系统服务工作目录为 `/var/lib/worklog`，用户服务为对应的 `data` 目录。

配置文件含密码或令牌时应限制读取权限。手动部署建议文件 0600、目录 0700；系统安装脚本使用 root 与服务主组可读的 0640。不要把配置放入静态目录或版本库，不要为了读取配置而以 root 运行服务。

不支持 JSON 注释。未知字段、错误类型、无效值或指定文件不存在都会导致启动失败；修改后需重启。

### 字段参考

下表为普通 CLI 的内置默认值，模板和桌面默认值可能不同：

| 字段 | 默认值与说明 |
| --- | --- |
| `addr` | `127.0.0.1:12450`，监听地址 |
| `db_driver` | `sqlite`，也可为 `postgres` |
| `db_dsn` | `./data/worklog.db`；PostgreSQL 使用连接串 |
| `user_id` | `owner`，仅由可信服务端设置，不是登录切换功能 |
| `timezone` | `Asia/Shanghai`，程序内嵌时区数据 |
| `static_dir` | `Web/dist`；空字符串关闭磁盘静态服务 |
| `token_file` | `./data/mcp-token`；未设置写令牌时读取或生成 |
| `write_token` | 默认未设置；显式值至少 32 字节 |
| `read_token` | 默认未设置；可选只读令牌，至少 32 字节且不同于写令牌 |
| `basic_user`／`basic_password` | `worklog`／空；非空密码至少 8 字节 |
| `allowed_hosts` | `["localhost","127.0.0.1","::1"]`；主机名，不含协议和端口 |
| `allowed_origins` | `["http://localhost:5173","http://127.0.0.1:5173"]`；精确 Origin，含协议和端口，不含路径 |
| `mcp_allow_destructive` | `false`；允许有写权限的 MCP／stdio 客户端删除、清空等，不扩大 REST Bearer 权限 |
| `stdio_read_only` | `false`；设为 `true` 强制 stdio 只读 |

白名单是字符串数组，两个开关是布尔值，其余字段为字符串。非 loopback 监听必须显式设置写令牌和 Basic 密码；即使监听本机，远程代理部署仍需配置这些凭证和域名白名单。完整权限边界见[身份认证与安全](AUTHENTICATION.md)。

数据目录以 0700、数据库／令牌／备份以 0600 创建，已有目录不会自动修正权限。SQLite WAL 数据必须位于本机磁盘，不要使用 NFS／SMB 共享文件系统。

### CLI 命令

| 命令 | 作用 |
| --- | --- |
| `serve`（默认） | 启动网页、REST API 和 MCP HTTP，启动前自动迁移数据库 |
| `stdio` | 启动本机 MCP 标准输入／输出通信，也会自动迁移；不是 HTTP 服务 |
| `migrate` | 仅初始化／升级数据库，然后退出 |
| `backup PATH` | 创建并校验 SQLite 备份，目标必须不存在 |
| `restore PATH` | 恢复独立 SQLite 备份；目标数据库及 WAL／SHM 必须不存在 |

不写子命令等同于 `serve`，会持续运行，不是仅检查配置。所有命令共用配置，例如 `./bin/worklog --conf=/etc/worklog backup /安全路径/backup.db`。备份与恢复的完整操作见[运维文档](OPERATIONS.md)。

### 前端文件

`static_dir` 应只包含前端构建产物。Go 动态提供公开的 `/config.json`，固定指向同源 `/api` 并传入业务时区，不读取磁盘上的 `dist/config.json`，也不向浏览器输出密码、令牌或数据库连接信息。

首页和配置不长缓存；带 hash 的资源使用 `private, max-age=31536000, immutable`。静态服务只提供首页、公开配置、图标、工作日历和 `assets/` 文件，拒绝目录列表和越界符号链接。启动时拒绝源码根、文件系统根以及含已配置数据库／令牌、默认备份路径的静态目录；使用 `--conf` 时也拒绝配置位于静态目录内。仍不要把任何私有文件放入该目录。

独立静态部署和浏览器本地模式见[前端说明](../Web/README.md)。自定义工作日历见[工作日历文档](WORK_CALENDAR.md)。

## Docker Compose

需要 Git、Docker、Compose 插件和下载基础镜像／依赖的网络。仓库提供本地构建配置，不依赖预先发布的容器镜像。先获取源码；已有克隆可直接进入仓库：

```sh
git clone https://github.com/MisakaAI/WorkLog.git
cd WorkLog
```

以下在仓库根目录操作，权限命令适用于 Linux：

```sh
cp config.docker.example.json config.local.json
chmod 600 config.local.json
install -d -m 700 deploy
openssl rand -hex 32
openssl rand -hex 32
```

用两个不同随机值分别填写 `config.local.json` 的 `write_token` 和 `basic_password`，不要使用共享默认密码。将 `allowed_hosts`／`allowed_origins` 改为实际访问的主机名和 Origin；本机访问时保留模板的 loopback 项。然后：

```sh
sudo install -o 10001 -g 10001 -m 600 config.local.json deploy/config.json
docker compose up --build -d
docker compose logs --tail=100 worklog
curl --fail http://127.0.0.1:12450/healthz
```

打开 `http://127.0.0.1:12450`，在浏览器认证提示中输入 `basic_user`（默认 `worklog`）和刚设置的密码。

- 宿主机仅发布 `127.0.0.1:12450`。容器内部监听 `0.0.0.0:12450`，所以模板凭证留空时会拒绝启动。
- 容器以 UID:GID `10001:10001` 运行。配置只读挂载到 `/app/config/config.json`，数据使用挂载到 `/app/data` 的命名卷；升级时不要删除卷，不要运行 `docker compose down -v`。
- 修改配置时先编辑当前用户持有的 `config.local.json`，重新执行 `sudo install`，再运行 `docker compose up -d --force-recreate worklog`。替换单文件挂载后不要仅重启旧容器。
- rootless Docker、Docker Desktop 或用户映射环境的挂载权限可能不同，应按实际 UID 调整，不要把凭证改为全员可读。
- 镜像含前端、纯 Go 程序和无秘密的配置模板，不含 Node／Go 编译器；用户私有配置不打入镜像。直接运行镜像时也需挂载自己的 JSON 配置，不使用环境变量覆盖。

升级时先[备份](OPERATIONS.md#容器)，更新源码后执行 `docker compose up --build -d`，再检查日志、健康状态和业务保存。

### Compose 使用 PostgreSQL

首次部署时，将上面的 SQLite 模板换成 [config.postgres.example.json](../config.postgres.example.json)。先创建 `deploy/`，再准备独立数据库密码：

```sh
cp config.postgres.example.json config.local.json
chmod 600 config.local.json
install -d -m 700 deploy
(umask 077; openssl rand -hex 32 > deploy/postgres-password.txt)
```

将密码文件中的值填入 `config.local.json` 的 `db_dsn` 密码部分，同时设置独立的 `write_token`、`basic_password` 和访问白名单，然后：

```sh
sudo install -o 10001 -g 10001 -m 600 config.local.json deploy/config.json
docker compose -f compose.yaml -f compose.postgres.yaml up --build -d
```

后续对此部署执行 Compose 检查、升级或备份命令时，也使用同一组 `-f` 文件。

PostgreSQL 使用独立持久化卷，不发布宿主机端口。官方镜像通过文件型 secret 读取初始化密码，模板数据库和角色名均为 `worklog`。Compose 的 `POSTGRES_*` 仅用于数据库镜像初始化，不是 WorkLog 配置入口。

修改密码文件不会轮换已初始化数据库的密码；需要在数据库中改密并同步更新 DSN。切换 PostgreSQL 不会自动搬迁 SQLite 数据。

## PostgreSQL 配置

不使用 PostgreSQL 时无需安装。使用已有数据库服务时，先创建专用用户和数据库，用户需有迁移权限：

```sql
CREATE USER worklog WITH PASSWORD 'REPLACE_WITH_SECRET';
CREATE DATABASE worklog OWNER worklog;
```

将以下字段合并到现用 JSON，替换主机和密码；密码中的 URL 特殊字符需要编码：

```json
{
  "db_driver": "postgres",
  "db_dsn": "postgres://worklog:URL_ENCODED_SECRET@db.example.com:5432/worklog?sslmode=verify-full"
}
```

远程连接使用验证证书的 TLS；仅本机可信测试使用 `sslmode=disable`。建议在 DSN 中写全主机、用户、数据库和凭证。存在非空 `PG*` 环境变量时 WorkLog 拒绝启动，应从服务环境清除。连接串仍遵循 pgx 语义，可读取密码文件、service 和 TLS 文件；证书路径可显式写入 `sslrootcert`、`sslcert`、`sslkey`。原生 `pg_dump`／`pg_restore` 不受应用的环境限制。

`serve` 和 `stdio` 自动迁移，也可显式执行 `./bin/worklog --conf=/etc/worklog migrate`。两种数据库有独立迁移；PostgreSQL 使用 advisory session lock，SQLite 应由一个部署启动者升级。不要并行运行不同版本迁移。升级前备份，迁移不是数据搬迁或备份工具。

## Nginx HTTPS 反向代理

推荐 Nginx → 本机 Go，由 Go 同时提供网页、`/api` 和 `/mcp`。先配置域名解析和有效 TLS 证书。

### 1. 配置 WorkLog

将以下字段合并到现用 JSON，保留数据库、前端和令牌路径。替换域名，分别用 `openssl rand -hex 32` 生成两个独立随机值：

```json
{
  "addr": "127.0.0.1:12450",
  "basic_user": "worklog",
  "basic_password": "REPLACE_WITH_INDEPENDENT_RANDOM_PASSWORD",
  "write_token": "REPLACE_WITH_ANOTHER_INDEPENDENT_RANDOM_TOKEN",
  "allowed_hosts": ["worklog.example.com", "localhost", "127.0.0.1"],
  "allowed_origins": ["https://worklog.example.com"]
}
```

重启服务。HTTPS Origin 必须明确加入白名单，即使代理到本机也不能省略。

### 2. 配置 Nginx

将配置放入 Nginx 的 `http` 上下文（例如已启用的站点文件），替换域名和证书路径：

```nginx
server {
    listen 80;
    server_name worklog.example.com;
    return 301 https://worklog.example.com$request_uri;
}
server {
    listen 443 ssl;
    server_name worklog.example.com;
    ssl_certificate /etc/letsencrypt/live/worklog.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/worklog.example.com/privkey.pem;
    ssl_protocols TLSv1.2 TLSv1.3;
    client_max_body_size 10m;

    location / {
        proxy_pass http://127.0.0.1:12450;
        proxy_http_version 1.1;
        proxy_set_header Host $http_host;
        proxy_set_header Authorization $http_authorization;
        proxy_set_header Origin $http_origin;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Forwarded-For $remote_addr;
        proxy_set_header Connection "";
        proxy_buffering off;
        proxy_read_timeout 30s;
    }
}
```

```sh
sudo nginx -t
sudo systemctl reload nginx
```

### 3. 验证访问

1. 本机 `/healthz` 返回 `{"ok":true}`。若移除了本机 Host 白名单，检查时添加 `-H 'Host: worklog.example.com'`。
2. 访问 `https://worklog.example.com`，确认出现 Basic 认证提示，任务／日志保存后刷新仍存在。
3. 确认 12450 端口没有对公网发布，再按 [MCP 指南](MCP.md)验证 Bearer 连接。
4. 在 Nginx 或入口网关配置限速和认证失败防护。应用业务预算不能代替防暴力破解。

**不要直接向公网开放 Go 的 12450 端口。远程 Basic 密码和令牌必须通过 HTTPS 传输。** 不要再为 `/mcp` 叠加 Nginx `auth_basic`，它与 Bearer 共用 `Authorization` 头。

代理的 10 MiB 请求体上限用于工作台；应用仍限制日志为 1 MiB、MCP 请求为 2 MiB。代理可能返回自己的错误页，不保证都是应用 JSON。保存返回 403 时，先检查 Host／Origin 和代理头，见[认证排查](AUTHENTICATION.md#7-常见问题)。

### 子目录部署

页面和资源可挂在 `/worklog/`，但 API 仍在根 `/api/`，MCP 仍在 `/mcp`，不支持直接把所有端点改为 `/worklog/api/`。

保留上例 HTTPS `server` 的证书和请求体设置。将公共代理设置放到该 `server` 内，再用下列位置块替换原 `location /`：

```nginx
proxy_http_version 1.1;
proxy_set_header Host $http_host;
proxy_set_header Authorization $http_authorization;
proxy_set_header Origin $http_origin;
proxy_set_header X-Forwarded-Proto $scheme;
proxy_set_header X-Forwarded-For $remote_addr;
proxy_set_header Connection "";
proxy_buffering off;
proxy_read_timeout 30s;

location = /worklog { return 301 /worklog/; }
location /worklog/ {
    # 尾部 / 剥离页面路径前缀。
    proxy_pass http://127.0.0.1:12450/;
}
location /api/ { proxy_pass http://127.0.0.1:12450; }
location = /mcp { proxy_pass http://127.0.0.1:12450; }
location = /healthz { proxy_pass http://127.0.0.1:12450; }
location / { return 404; }
```

前端使用相对资源路径，Go 的动态配置仍指向 `/api`。部署后实际检查页面、保存和 MCP；本地测试不能代替 HTTPS 代理验收。
