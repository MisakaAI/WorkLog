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

### Linux

从 [Releases](https://github.com/MisakaAI/WorkLog/releases) 下载与主机架构匹配的 `.tar.zst` 发布包（`linux-x86_64` 为 amd64，`linux-arm64` 为 arm64），解压后运行安装脚本：

```sh
tar --zstd -xf worklog-v<版本>-linux-x86_64.tar.zst
cd worklog-v<版本>-linux-x86_64
sudo bash ./install.sh  # 安装系统服务
# 或 bash ./install.sh，以当前用户安装 systemd 用户服务
```

默认访问 **http://127.0.0.1:12450**。用户服务如需开机运行且退出登录后保持运行，需管理员执行 `sudo loginctl enable-linger 用户名`。
详细安装、配置、升级与维护见[部署文档](docs/DEPLOYMENT.md)。

### Windows

从 [Releases](https://github.com/MisakaAI/WorkLog/releases) 下载并运行 `worklog-v<版本>-windows-x86_64.exe`。这是原生系统托盘桌面版，使用默认浏览器打开界面，数据默认保存在 `%APPDATA%\WorklogDesktop`。详细说明见 [Windows 桌面版](docs/WINDOWS_DESKTOP.md)。

## 发布版本

发行版本以 Git tag 为唯一来源，不再维护仓库根目录的 `VERSION` 文件，也不支持通过 `VERSION` 环境变量覆盖。Linux / Windows 打包脚本共用 `scripts/release-version.sh`，取当前 `HEAD` 可达的最近 `v[0-9]*` 标签（`git describe --tags --abbrev=0`），并验证为 `vX.Y.Z`（可带预发行／构建后缀）。这不是跨分支按创建时间或版本大小取标签；标签后的开发提交沿用该标签版本，正式发布应在对应标签提交上构建。

```sh
# 先提交代码、脚本与文档，再创建发行标签：
git tag -a v0.2.0-rc.1 -m "Release v0.2.0-rc.1"
bash scripts/build-linux.sh
bash scripts/build-windows.sh
# 需要发布到远端时，推送提交及标签：
git push origin HEAD
git push origin v0.2.0-rc.1
```

打包需要 Git 仓库及标签；没有标签或标签格式非法会直接失败，不回退到硬编码版本。已有完整克隆可执行 `git fetch --tags`；浅克隆先执行 `git fetch --unshallow --tags`，CI checkout 设置 `fetch-depth: 0`。源码 ZIP 不包含 Git 元数据，不能直接运行发行打包脚本。

版本解析回归测试：`bash scripts/test-release-version.sh`。前端 `Web/package.json` 的版本是内部 npm 包元数据，不作为整体发行版本；发布包内的 `VERSION` 只是由标签生成的安装记录。

## 简单使用

- 在工作台创建项目、添加任务，在列表／看板／甘特图中管理进度与排期。
- 使用待办记录临时事项，按日期编辑富文本工作日志；服务端默认业务时区为 `Asia/Shanghai`。
- 多个标签页或 MCP 同时修改时，旧版本保存会返回冲突。先复制未保存内容，再重新加载最新数据，不要反复覆盖提交。
- AI 客户端可通过 `/mcp` 接入，使用独立 Bearer 令牌；配置示例见 [MCP 客户端指南](docs/MCP.md)。浏览器不需要配置该令牌。
- 定期备份数据，SQLite 可执行 `./bin/worklog --conf=/etc/worklog backup ./backups/worklog.db`（使用与服务相同的配置；仅默认配置启动时可省略 `--conf`）；**不要只复制运行中的 `.db` 文件**。
恢复步骤见[备份与恢复](docs/OPERATIONS.md)。

## 文档

- [部署、打包与源码构建](docs/DEPLOYMENT.md)：Releases 安装、Linux systemd 部署、打包构建、JSON 配置、Nginx、Docker 与 PostgreSQL。
- [Windows 桌面版](docs/WINDOWS_DESKTOP.md)：Windows 托盘版行为与构建说明。
- [身份认证与安全](docs/AUTHENTICATION.md)：本地／Basic／Bearer／stdio、权限矩阵与安全边界。
- [REST API](docs/REST_API.md)：端点、细粒度资源、版本与幂等约定，以及前端快照协议。
- [MCP 客户端指南](docs/MCP.md)：工具列表、Codex、HTTP 与 stdio 配置。
- [备份与恢复](docs/OPERATIONS.md)：SQLite 和 PostgreSQL 数据维护。
- [开发与测试](docs/DEVELOPMENT.md)：前端热更新、测试命令、代码结构与存储并发约定。
- [工作日历自定义](docs/WORK_CALENDAR.md)：通过 JSON 设置工作日、节假日、调休、单休或取消节日标记。
- [前端说明](Web/README.md)：独立前端运行与配置。
