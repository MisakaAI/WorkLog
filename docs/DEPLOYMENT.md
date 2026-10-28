# 部署与配置

Linux 优先从 [Releases](https://github.com/MisakaAI/WorkLog/releases) 下载 `.tar.zst` 预编译包部署，目标主机无需 Go / Node.js；下载部署与自行编译发布包的独立步骤见 [Linux / systemd 部署](DEPLOYMENT.md#linux-systemd-部署)。源码构建、发布包打包与部署流程见本文后续章节；认证细节见[身份认证与安全](AUTHENTICATION.md)。
远程访问推荐同源部署：Nginx → 本机 Go 服务，由 Go 提供前端、`/api` 和 `/mcp`。

## JSON 配置

编译后的 Go 程序支持通过 `--conf` 指定 JSON 文件或目录；指定目录时读取其中的 `config.json`：

```sh
# 以实际服务运行用户执行，或由管理员创建后授予该用户访问权限。
install -d -m 700 /etc/worklog
install -m 600 config.example.json /etc/worklog/config.json
# 编辑配置，尤其是数据库、令牌和前端产物的绝对路径。
./bin/worklog --conf=/etc/worklog
# 也可直接指定文件：
./bin/worklog --conf=/etc/worklog/config.json
```

完整模板见 [config.example.json](../config.example.json)。例如：

```json
{
  "addr": "127.0.0.1:12450",
  "db_driver": "sqlite",
  "db_dsn": "/var/lib/worklog/worklog.db",
  "static_dir": "/opt/worklog/Web/dist",
  "token_file": "/var/lib/worklog/mcp-token",
  "timezone": "Asia/Shanghai",
  "stdio_read_only": false
}
```

- **业务配置仅使用 JSON 和内置默认值**，不读取 `WORKLOG_*` 环境变量或 `.env`。不指定 `--conf` 时仅使用内置默认值，不自动搜索配置文件；Windows 桌面版有单独的默认配置路径约定。
- 主机／Origin 白名单使用字符串数组，两个开关使用 JSON 布尔值，其余使用字符串。不支持 JSON 注释；未知字段、错误类型、无效配置或不存在的文件会导致启动失败。
- `static_dir` 设为空字符串可关闭静态服务。本地未设置 `write_token` 时仍自动生成／读取 `token_file`。
- **所有相对路径仍以进程工作目录为基准**，不是配置文件目录。独立部署建议像上例一样使用绝对路径，并预先创建可由服务用户写入的数据目录；前端目录需包含构建好的文件。
- 配置可能包含密码和令牌，建议文件权限 `0600`、目录权限 `0700`，仅允许服务用户访问，不要提交到版本库或放入静态目录。修改后需重启。
- `--conf` 必须放在子命令之前，所有命令共用配置。`./bin/worklog --conf=/etc/worklog` 后面的子命令决定程序的运行模式：

| 命令 | 作用与适用场景 |
| --- | --- |
| `serve` | 启动常驻 HTTP 服务，提供网页、REST API 和 MCP HTTP 接口；这是默认命令。会在服务启动前自动执行数据库迁移。部署常规 Web 服务使用它。 |
| `stdio` | 启动 MCP stdio 传输，从标准输入／输出与本机 MCP 客户端通信；不是 HTTP 服务，通常由 Codex 等 MCP 客户端按需启动，协议输出占用 stdout。也会自动迁移数据库。 |
| `migrate` | 只初始化／升级数据库结构，然后退出；适合部署时显式执行迁移，不会启动 HTTP 或 stdio 服务。 |
| `backup PATH` | 创建 SQLite 数据库备份到指定路径，并校验备份；目标文件必须不存在。用于定期备份，不启动服务。PostgreSQL 请使用 `pg_dump`，见[运维说明](OPERATIONS.md)。 |
| `restore PATH` | 将指定的独立 SQLite 备份恢复到配置中的数据库路径，然后退出；目标数据库及其 WAL/SHM 文件必须不存在，不会覆盖已有数据库。恢复前应停止所有写入进程。PostgreSQL 请使用 `pg_restore`。 |

例如：

```sh
./bin/worklog --conf=/etc/worklog serve
./bin/worklog --conf=/etc/worklog stdio
./bin/worklog --conf=/etc/worklog migrate
./bin/worklog --conf=/etc/worklog backup /var/backups/worklog/backup.db
./bin/worklog --conf=/etc/worklog restore /var/backups/worklog/backup.db
./bin/worklog --help
```

不写子命令时（例如 `./bin/worklog --conf=/etc/worklog`）等同于 `serve`，会启动常驻 HTTP 服务而不是仅检查配置后退出；进程会持续运行，直到被停止。`--help` 则只显示用法并退出。

配置目录通常由管理员管理；不要为了读取 `/etc/worklog` 而使用 root 运行服务。反向代理部署时，还需设置 `basic_password`、`write_token`、`allowed_hosts` 和 `allowed_origins`，见下表和 Nginx 示例。

## systemd 持久运行

按 [Linux Systemd 部署说明](DEPLOYMENT.md#linux-systemd-部署)解压发布包并运行 `install.sh`：root 执行时安装系统服务，创建／复用专用 `worklog` 用户，配置位于 `/etc/worklog/config.json`；非 root 执行时默认以当前用户安装 systemd 用户服务，配置位于 `~/.config/worklog/config.json`（支持 XDG 路径）。重复安装保留已有配置和数据。系统服务模板见 [worklog.service](worklog.service)，用户服务由安装脚本单独生成。

## 配置字段

完整模板见 [config.example.json](../config.example.json)。以下为程序内置默认值；模板中的部署路径可能不同。

| JSON 字段 | 默认／用途 |
| --- | --- |
| `addr` | `127.0.0.1:12450` |
| `db_driver` | `sqlite` 或 `postgres`，默认 sqlite |
| `db_dsn` | `./data/worklog.db`；PostgreSQL 使用连接 URL |
| `user_id` | `owner`，仅由可信服务端设置 |
| `timezone` | `Asia/Shanghai`，程序内嵌 tzdata |
| `static_dir` | `Web/dist`；空串关闭静态服务 |
| `token_file` | `./data/mcp-token`，本地未设置写令牌时生成／读取 |
| `write_token` | 至少 32 字节的独立 Bearer 写令牌 |
| `read_token` | 可选、与写令牌不同的至少 32 字节只读令牌 |
| `basic_user` / `basic_password` | `worklog` / 空；非空密码至少 8 字节 |
| `allowed_hosts` | 主机名字符串数组，不含协议和端口；默认 localhost / loopback |
| `allowed_origins` | 精确 Origin 字符串数组，默认本机 Vite 的 localhost / 127.0.0.1:5173 |
| `mcp_allow_destructive` | `false`；设为 `true` 才允许 MCP / stdio 写入方删除／清空，不扩大 REST Bearer 权限 |
| `stdio_read_only` | `false`；设为 `true` 强制 stdio 只读 |

非 loopback 绑定必须显式设置写令牌和 Basic 密码。  
即使绑定 loopback，远程代理部署也应设置强凭证和准确的域名／HTTPS Origin 白名单。

数据目录以 0700、数据库／令牌／备份以 0600 创建。已有目录不会自动改变权限。  
SQLite WAL 必须放在本机磁盘，不能使用 NFS / SMB 等不适合 WAL 的共享文件系统。

## 静态文件与运行时配置

Go 从服务端 JSON 配置派生并动态提供公开的 `/config.json`，固定使用同源 `/api` 并传入业务时区，**不读取磁盘上的 dist/config.json**，也不会将令牌或数据库连接信息注入浏览器配置。
首页与配置不长缓存；hash assets 使用 `private, max-age=31536000, immutable`。

`static_dir` 必须指向专用前端产物目录。启动时会拒绝源码根、文件系统根及包含已配置数据库／令牌和默认备份路径的目录。
使用 `--conf` 时，也会拒绝将配置文件放在静态目录内。静态服务只放行首页、配置、图标和 `assets/` 下的文件，不提供目录列表，拒绝越界符号链接。仍不得把源码、数据库或备份放入静态目录。

独立静态部署及本地浏览器模式见[前端说明](../Web/README.md)。  
独立前端连接后端时，推荐同源代理，并在运行时配置中设置与后端一致的 `businessTimezone`。  
没有配置时，今天标签使用浏览器时区；日志首次日期仍以 `/today` 为准，任务表单默认日期沿用浏览器本地日期。

## Docker Compose

在项目根目录准备配置和数据目录（以下权限命令适用于 Linux）：

```sh
cp config.docker.example.json config.local.json
chmod 600 config.local.json
# 用当前用户编辑 config.local.json：
# 分别执行 openssl rand -hex 32，填入独立的 write_token 和 basic_password；
# 修改 allowed_hosts / allowed_origins 为实际域名和 HTTPS Origin。
install -d -m 700 deploy
sudo install -o 10001 -g 10001 -m 600 config.local.json deploy/config.json
docker compose up --build -d
docker compose logs --tail=100 worklog
```

- `deploy/config.json` 只读挂载到 `/app/config/config.json`；SQLite 数据和令牌保存在挂载到 `/app/data` 的 `worklog-data` 命名卷中。配置不进入 Git 或镜像；升级时不要删除数据卷。
- 容器以 UID:GID `10001:10001` 非 root 运行；配置文件需允许该 UID 读取。修改配置时，编辑当前用户持有的 `config.local.json`，重新执行上面的 `sudo install` 并重建服务容器（`docker compose up -d --force-recreate worklog`），确保单文件挂载读取到新文件。rootless Docker、Docker Desktop 或启用用户映射时，需按实际挂载权限调整。
- 主机只发布 `127.0.0.1:12450`；容器内监听 `0.0.0.0:12450`，因此必须配置凭证。模板的凭证为空，会拒绝启动，不提供共享默认密码。
- 镜像包含前端与纯 Go 可执行文件，不包含 Node / Go 编译器。直接运行镜像也必须只读挂载自己的 JSON 到 `/app/config/config.json`；所有应用配置均来自该文件，不通过环境变量覆盖。
- 日历使用前端 `work-calendar.json`。可在构建前编辑 `Web/public/work-calendar.json`，或通过只读挂载替换镜像中的 `/app/web/work-calendar.json`，详见[工作日历自定义](WORK_CALENDAR.md)。

### Compose 使用 PostgreSQL

首次部署时，以上步骤改用 [config.postgres.example.json](../config.postgres.example.json) 创建 `config.local.json`，不要使用 SQLite 模板。在安装到 `deploy/config.json` 之前，另准备数据库密码文件并补全 DSN：

```sh
# 以宿主机当前用户执行；生成独立于 Worklog 令牌和 Basic 密码的数据库密码。
(umask 077; openssl rand -hex 32 > deploy/postgres-password.txt)
# 用当前用户编辑 config.local.json：将密码文件中的值填入 db_dsn 的密码部分；
# 仍需设置 write_token / basic_password，再安装容器配置。
sudo install -o 10001 -g 10001 -m 600 config.local.json deploy/config.json
docker compose -f compose.yaml -f compose.postgres.yaml up --build -d
```

PostgreSQL 官方镜像通过文件型 secret 读取初始化密码，不依赖宿主机环境变量或 `.env`；Compose 中的 `POSTGRES_USER`、`POSTGRES_DB` 和 `POSTGRES_PASSWORD_FILE` 仅用于第三方镜像的固定初始化参数，没有宿主机环境变量插值。模板使用 `worklog` 数据库和角色。密码文件仅宿主机用户可读，勿将其设为空或提交到版本库。

PostgreSQL 不发布外部端口，使用独立持久化卷。修改密码文件不会修改已初始化数据库的密码，需要同时在数据库中轮换凭证并更新 JSON 的 DSN。配置 PostgreSQL 不会自动搬迁 SQLite 数据。

镜像构建需要访问基础镜像仓库；配置文件存在不代表当前部署环境已验证镜像构建或容器运行。

## PostgreSQL

不使用时无需安装。先创建专用数据库及有迁移权限的用户：

```sql
CREATE USER worklog WITH PASSWORD 'REPLACE_WITH_SECRET';
CREATE DATABASE worklog OWNER worklog;
```

在服务使用的 JSON 文件中设置：

```json
{
  "db_driver": "postgres",
  "db_dsn": "postgres://worklog:URL_ENCODED_SECRET@db.example.com:5432/worklog?sslmode=verify-full"
}
```

以上字段合并到完整配置中，再执行：

```sh
./bin/worklog --conf=/etc/worklog migrate
./bin/worklog --conf=/etc/worklog
```

密码等 URL 特殊字符需编码；远程数据库使用验证证书的 TLS，本机可信测试可用 `sslmode=disable`。
建议在 `db_dsn` 中显式填写完整的主机、用户、数据库和凭证。Worklog 不接受 PostgreSQL 环境配置：存在非空 `PG*` 环境变量时会拒绝启动，请从服务运行环境清除并把连接参数写入 JSON 的 `db_dsn`。DSN 保持 pgx 的标准连接串语义（包括驱动的密码文件、service 和 TLS 文件机制）；需要证书时推荐在 DSN 中显式指定 `sslrootcert`、`sslcert`、`sslkey` 路径。这里取消的是环境变量配置入口，并不禁止数据库驱动读取连接所需文件。原生 `pg_dump` / `pg_restore` 的配置不受此限制。

`serve`（默认命令）和 `stdio` 也会自动迁移。  
配置 PostgreSQL 不会自动搬迁已有 SQLite 数据。

两种数据库有独立的内嵌 goose 迁移。  
PostgreSQL 使用 advisory session lock；SQLite 升级应由一个部署启动者执行，随后可有多个进程访问同一本地文件，不要并行运行不同版本迁移。  
升级前备份，恢复见[运维说明](OPERATIONS.md)。

## Nginx HTTPS 反向代理

### 配置 Go 服务

先按上文准备 `/etc/worklog/config.json`，将以下字段合并到配置中，保留数据库、前端和令牌文件的绝对路径：

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

将域名替换为实际值，分别执行 `openssl rand -hex 32` 生成独立密码和令牌，替换占位值。配置文件仅允许服务用户及管理员读取。

```sh
./bin/worklog --conf=/etc/worklog
```

正式运行推荐使用 [systemd](DEPLOYMENT.md#linux-systemd-部署) 保持服务运行。

### 配置 Nginx

先将域名解析到服务器，并准备有效的 TLS 证书。  
以下配置放入 Nginx 的 `http` 上下文（例如发行版启用的站点配置文件），替换域名和证书路径：

```nginx
server {
    listen 80;
    server_name worklog.example.com;
    return 301 https://worklog.example.com$request_uri;
}

server {
    listen 443 ssl;
    server_name worklog.example.com;

    ssl_certificate     /etc/letsencrypt/live/worklog.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/worklog.example.com/privkey.pem;
    ssl_protocols TLSv1.2 TLSv1.3;

    client_max_body_size 10m;
    gzip on;
    gzip_types text/css application/javascript application/json image/svg+xml;

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

访问 `https://worklog.example.com`，使用配置中的 `basic_user` / `basic_password` 登录浏览器认证提示框。
不要在 Nginx 再叠加一层 `auth_basic`：浏览器 Basic 和 MCP Bearer 均由 Go 验证，共用 `Authorization` 请求头。

**禁止直接向公网开放 Go 的 12450 端口；Basic 密码和令牌必须通过 HTTPS 传输。** HTTPS Origin 必须明确加入白名单，即使代理到本机也不能省略。

### 部署检查

1. 确认 Go 服务启动，`curl http://127.0.0.1:12450/healthz` 返回可用状态。
2. 按上文设置 Nginx 证书、代理和请求体上限，执行 `nginx -t` 后重载。
3. 浏览器通过 HTTPS 域名访问，确认需要 Basic 认证，任务／日志保存后刷新仍存在。
4. 确认 Go 12450 端口没有对公网发布，并使用 MCP 客户端验证 Bearer 通道。
5. 在入口配置适当的请求限速／认证失败防护，不要把应用业务预算当作防暴力破解。

应用最大请求体为工作台 10 MiB、日志 1 MiB、MCP envelope 2 MiB；  
上述 Nginx `client_max_body_size 10m` 避免代理先以默认 1 MiB 拦截工作台，应用仍会检查各端点的更细限制。  
代理也可能生成自己的错误页，不能假定所有错误都符合应用 JSON 格式。

### 子目录部署

支持页面与资源挂载 `/worklog/`，但 API 仍在同源根 `/api/`，MCP 仍为 `/mcp`；不自动支持所有端点重定位到 `/worklog/api/`。

如需该拓扑，将上述 Nginx 配置中的 `location /` 替换为以下位置块。公共代理设置应放在同一个 HTTPS `server` 中，或通过共享 snippet 引入：

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
    # 尾部 / 用于剥离 /worklog/ 前缀。
    proxy_pass http://127.0.0.1:12450/;
}
location /api/ { proxy_pass http://127.0.0.1:12450; }
location = /mcp { proxy_pass http://127.0.0.1:12450; }
location = /healthz { proxy_pass http://127.0.0.1:12450; }
location / { return 404; }
```

静态资源采用相对路径，Go 的动态配置仍指向根 `/api`。正式环境需实际验证 TLS、代理和客户端行为，不能以本地测试代替公网验收。


## Linux systemd 部署


Worklog 在 Linux 下以一个 Go 进程提供前端静态文件、REST API 和 MCP。前后端构建产物分文件安装，不需要运行 Node.js、Vite 或单独的前端服务。

推荐从 Releases 下载预编译包；需要修改源码时再自行构建。两种方式最终使用相同的安装脚本。

## 1. 从 Releases 下载部署（推荐）

在 [Releases](https://github.com/MisakaAI/WorkLog/releases) 下载与主机架构匹配的文件：

| `uname -m` | 发布包 |
| --- | --- |
| `x86_64` | `worklog-v<版本>-linux-x86_64.tar.zst`（Go amd64） |
| `aarch64` / `arm64` | `worklog-v<版本>-linux-arm64.tar.zst` |

目标主机需要 Linux、systemd、Bash、GNU 常用文件工具、支持 zstd 的 tar 和 `zstd`，不需要 Go、Node.js 或独立数据库。Debian / Ubuntu 可通过 `sudo apt install zstd` 安装解压依赖。

例如下载后解压（将版本号替换为实际版本）：

```sh
tar --zstd -xf worklog-v0.1.0-linux-x86_64.tar.zst
cd worklog-v0.1.0-linux-x86_64
```

压缩包包含 `bin/worklog`、`Web/dist/`、`install.sh`、配置示例、systemd 模板、版本与架构信息及运维文档。

### A. root 安装：系统服务

```sh
sudo bash ./install.sh
```

脚本默认创建专用 `worklog` 系统用户（已存在则复用），**服务不以 root 运行**。安装后自动启动并设置开机自启：

| 路径 | 用途与权限 |
| --- | --- |
| `/opt/worklog/bin/worklog` | 后端，root 管理 |
| `/opt/worklog/Web/dist` | 前端产物，服务用户只读 |
| `/etc/worklog/config.json` | root 管理，服务主组只读，`0640` |
| `/var/lib/worklog` | 数据库、WAL 和令牌，worklog 可写，`0700` |
| `/etc/systemd/system/worklog.service` | 系统服务单元 |

root 模式使用 [worklog.service](worklog.service) 模板，默认账户主组为 `worklog`；复用已有账户时配置文件使用该账户实际主组。

### B. 普通用户安装：当前用户服务

**不使用 sudo** 执行：

```sh
bash ./install.sh
```

脚本不会创建用户，而是以当前用户运行 `systemd --user` 服务：

| 路径 | 用途 |
| --- | --- |
| `~/.local/share/worklog/app/bin/worklog` | 后端 |
| `~/.local/share/worklog/app/Web/dist` | 前端 |
| `~/.config/worklog/config.json` | 当前用户配置，`0600` |
| `~/.local/share/worklog/data` | 数据和令牌，`0700` |
| `~/.config/systemd/user/worklog.service` | 用户服务单元 |

支持绝对路径的 `XDG_DATA_HOME` 和 `XDG_CONFIG_HOME` 覆盖上述基础目录。这些是安装路径约定，不是 Worklog 业务配置环境变量。

需要当前用户的 systemd 用户管理器可访问；请通过正常登录会话执行，不要简单使用 `sudo -u`。脚本会在修改文件前检查连接。用户服务使用单独生成的单元，不使用系统服务的 `User=worklog`、`ProtectHome` 或 `StateDirectory`。

默认随用户管理器启动。若需要**开机即运行、退出登录后仍保持运行**，由管理员执行：

```sh
sudo loginctl enable-linger 用户名
```

用户服务与系统服务默认都使用 `127.0.0.1:12450`；不要同时安装运行两份而不修改端口和数据路径。

## 2. 自行编译再部署

只在构建机器上安装 [README](../README.md#开发环境) 要求的 Go、Node.js / npm，以及 tar、zstd。编译不需要 root：

```sh
bash scripts/build-linux.sh
# Go 不在 PATH 时：
# GO=/opt/go/bin/go bash scripts/build-linux.sh
# 覆盖发布版本：
# VERSION=0.2.0 bash scripts/build-linux.sh
```

脚本只构建一次前端，然后以 `CGO_ENABLED=0` 交叉编译 Linux amd64 和 arm64，输出：

```text
bin/worklog-v<版本>-linux-x86_64.tar.zst
bin/worklog-v<版本>-linux-arm64.tar.zst
```

默认版本读取仓库 `VERSION`。不需要 Windows SDK、目标架构编译器或目标机器上的构建工具。将对应压缩包复制到目标主机，按第 1 节解压安装；**不要直接在源码仓库运行 `scripts/install-linux.sh`**，安装脚本依赖发布包布局。

开发环境中从源码构建并手动启动，可在项目根目录执行：

```sh
(cd Web && npm ci && npm run build)
go build -o bin/worklog ./cmd/worklog
./bin/worklog
```

打开 `http://127.0.0.1:12450` 即可使用。首次启动会创建 `data/worklog.db`、执行迁移并生成 MCP 令牌文件。需要打包给 Linux 主机部署时，使用上文的 `scripts/build-linux.sh` 生成 Releases 格式发布包；不要直接在源码仓库运行安装脚本。

## 3. 配置与远程访问

首次安装生成使用绝对路径的最小 SQLite 配置，仅监听 `127.0.0.1:12450`。启动时自动创建数据库、执行迁移，并生成 MCP 读写令牌文件；无需额外执行迁移命令。

打开 **http://127.0.0.1:12450**。系统级服务工作目录为 `/var/lib/worklog`，用户级为对应 `data` 目录。所有业务配置仅来自 JSON；相对路径按服务工作目录解析，建议保留绝对路径。

编辑配置后重启：

```sh
# 系统服务
sudoedit /etc/worklog/config.json
sudo systemctl restart worklog.service

# 用户服务（按自己的 XDG 路径调整）
"${EDITOR:-vi}" ~/.config/worklog/config.json
systemctl --user restart worklog.service
```

完整字段见 [部署与配置](DEPLOYMENT.md)。配置包含秘密，不要放进静态目录或版本库。SQLite 数据必须位于本机磁盘，不要使用 NFS / SMB。

远程部署推荐 Nginx / Caddy / Cloudflare Tunnel → 本机 Worklog，外部入口使用 HTTPS。必须另外设置 Worklog 的独立 Basic 密码、写令牌，以及实际域名的 `allowed_hosts` 和 HTTPS `allowed_origins`；代理应保留实际 Host。Cloudflare Access 不替代 Worklog 自身认证。具体示例见 [Nginx HTTPS 反向代理](DEPLOYMENT.md#nginx-https-反向代理)和[身份认证与安全](AUTHENTICATION.md)。

安装脚本不会修改防火墙、开放公网监听、安装代理或生成 TLS 证书。

## 4. 状态、日志和日常维护

系统服务：

```sh
sudo systemctl status worklog.service --no-pager
sudo journalctl -u worklog.service -n 100 --no-pager
sudo journalctl -u worklog.service -f
sudo systemctl restart worklog.service
sudo systemctl disable --now worklog.service
```

用户服务：

```sh
systemctl --user status worklog.service --no-pager
journalctl --user -u worklog.service -n 100 --no-pager
journalctl --user -u worklog.service -f
systemctl --user restart worklog.service
systemctl --user disable --now worklog.service
```

健康检查：

```sh
curl --fail http://127.0.0.1:12450/healthz
# 若移除了本机 Host 白名单，指定允许的域名：
# curl --fail -H 'Host: worklog.example.com' http://127.0.0.1:12450/healthz
```

修改单元后需执行对应的 `systemctl [--user] daemon-reload` 再重启；仅修改 JSON 不需要 daemon-reload。日志进入 journald，其持久化与轮转由主机配置决定。

启动失败时检查日志中的配置、权限、端口占用和路径问题；触发启动频率限制后，修复问题并执行对应的 `systemctl [--user] reset-failed worklog.service`，再启动。

## 5. 升级与备份

**先备份，再升级。** 解压新版本，用与原安装相同的身份及 XDG 路径重新执行 `install.sh`：

- 保留已有 `config.json`，不自动合并新版本配置。
- 保留数据库、令牌和数据目录；不会从压缩包注入业务数据。
- 准备新文件后停止已有服务，再替换二进制和整套前端，更新服务单元并启动。
- 不在运行中直接覆盖可执行文件，也不混用两个版本的前端资源。
- 安装脚本会覆盖对应服务单元；自行修改的单元应先备份，持久自定义优先使用 systemd drop-in。

脚本会检查服务是否处于运行状态，但不保证后续运行永不出错，也不自动恢复数据库或回滚失败升级。**数据库迁移可能不可逆，回滚程序不等于回滚数据库。**

系统服务的 SQLite 在线备份示例：

```sh
sudo -u worklog install -d -m 700 /var/lib/worklog/backups
sudo -u worklog sh -c 'cd /var/lib/worklog && /opt/worklog/bin/worklog --conf=/etc/worklog backup "/var/lib/worklog/backups/worklog-$(date +%Y%m%d-%H%M%S).db"'
```

用户服务（默认路径）：

```sh
mkdir -p ~/.local/share/worklog/data/backups
~/.local/share/worklog/app/bin/worklog --conf="$HOME/.config/worklog/config.json" \
  backup "$HOME/.local/share/worklog/data/backups/worklog-$(date +%Y%m%d-%H%M%S).db"
```

备份目标必须不存在。不要只复制运行中的 `.db`，应将备份另存到其他存储。PostgreSQL 使用专用备份工具，恢复步骤见[备份与恢复](OPERATIONS.md)。

## 6. 卸载与安全边界

目前不提供自动删除数据的卸载命令。先 `disable --now` 停止对应服务，再删除对应单元并 `daemon-reload`；程序目录可以删除，配置和数据默认应保留，确认备份后再自行清理。不要误删用户安装下与 app 相邻的 data 目录。

系统服务启用只读文件系统、隐藏 home、无 capabilities、禁止提权、私有临时目录，只有状态目录可持久写入。若改变数据或令牌路径，需要同步调整目录权限与服务沙箱。用户服务不提供同等文件系统沙箱，它以当前用户权限访问文件，适合个人安装而非安全隔离。

两种服务均使用 `UMask=0077`、异常退出重启和 SIGTERM 优雅退出。系统模式需支持模板中的 systemd 指令，可用 `sudo systemd-analyze verify /etc/systemd/system/worklog.service` 检查。SELinux / AppArmor、容器或受限主机可能需要额外策略。安装脚本不会自动配置这些策略。
