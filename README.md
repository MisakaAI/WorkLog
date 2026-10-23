# Worklog

Worklog 是一个单用户工作台，支持项目与任务管理、待办事项、甘特图和富文本工作日志。  
前端使用 Vue，后端使用 Go，提供 REST API 与 MCP 工具，浏览器和 AI 客户端共享同一份业务数据。

默认使用纯 Go SQLite，无需 PostgreSQL 或 CGO；也支持 PostgreSQL。  
数据保存在服务端，重启后不会丢失。当前不提供多用户注册、OAuth、自动冲突合并或日志历史记录。

## 开发环境

- Go **1.27.3**（以 [go.mod](go.mod) 为准）。
- 推荐 Node.js **24.15+** 和 npm。
- 默认无需 Docker 或独立数据库。

## 部署与启动

在项目根目录构建前端和 Go 服务：

```sh
(cd Web && npm ci && npm run build)
go build -o bin/worklog ./cmd/worklog
./bin/worklog
```

打开 **http://127.0.0.1:8080** 即可使用。Go 服务同时提供前端静态文件、`/api` 和 `/mcp`，无需单独启动前端服务器。

首次启动自动创建 `data/worklog.db`、执行迁移并生成 MCP 令牌文件 `data/mcp-token`，不注入示例业务数据。  
请从项目根目录运行，默认配置使用相对路径。SQLite 数据应放在本机磁盘，不要放在 NFS / SMB 上。

默认仅监听本机地址。同机其他进程也可以访问 REST，不是安全隔离边界；  
共享电脑建议设置 Basic 密码，详见[身份认证与安全](docs/AUTHENTICATION.md)。  
也可不使用环境变量：将 [config.example.json](config.example.json) 复制为 `/etc/worklog/config.json` 并修改其中路径，再执行：

```sh
./bin/worklog --conf=/etc/worklog
```

`--conf` 支持目录或 JSON 文件；指定后只使用配置文件和内置默认值，忽略 `WORKLOG_*` 环境变量。配置文件权限建议设为 `0600`。

自定义配置、Nginx 反向代理及 Docker / PostgreSQL 部署见[部署与配置](docs/DEPLOYMENT.md)。

## 简单使用

- 在工作台创建项目、添加任务，在列表／看板／甘特图中管理进度与排期。
- 使用待办记录临时事项，按日期编辑富文本工作日志；服务端默认业务时区为 `Asia/Shanghai`。
- 多个标签页或 MCP 同时修改时，旧版本保存会返回冲突。先复制未保存内容，再重新加载最新数据，不要反复覆盖提交。
- AI 客户端可通过 `/mcp` 接入，使用独立 Bearer 令牌；配置示例见 [MCP 客户端指南](docs/MCP.md)。浏览器不需要配置该令牌。
- 定期备份数据，SQLite 可执行 `./bin/worklog backup ./backups/worklog.db`；**不要只复制运行中的 `.db` 文件**。  
恢复步骤见[备份与恢复](docs/OPERATIONS.md)。

## 文档

- [部署与配置](docs/DEPLOYMENT.md)：配置文件、环境变量、Nginx 反向代理、Docker 与 PostgreSQL。
- [systemd 持久运行](docs/SYSTEMD.md)：专用用户、配置文件、开机自启、日志与服务维护，附 [worklog.service 示例](docs/worklog.service)。
- [身份认证与安全](docs/AUTHENTICATION.md)：本地／Basic／Bearer／stdio、权限矩阵与安全边界。
- [REST API](docs/REST_API.md)：端点、细粒度资源、版本与幂等约定，以及前端快照协议。
- [MCP 客户端指南](docs/MCP.md)：工具列表、Codex、HTTP 与 stdio 配置。
- [备份与恢复](docs/OPERATIONS.md)：SQLite 和 PostgreSQL 数据维护。
- [开发与测试](docs/DEVELOPMENT.md)：前端热更新、测试命令、代码结构与存储并发约定。
- [工作日历自定义](docs/WORK_CALENDAR.md)：通过 JSON 设置工作日、节假日、调休、单休或取消节日标记。
- [前端说明](Web/README.md)：独立前端运行与配置。
