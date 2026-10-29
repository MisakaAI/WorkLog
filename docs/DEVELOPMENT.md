# 开发与测试

本文面向修改源码、运行测试和制作发布包的开发者。安装现成程序见[部署与配置](DEPLOYMENT.md)。

## 环境与本地运行

准备 Git、Go **1.27.3**（见 [go.mod](../go.mod)）、Node.js **24.15+** 和 npm，以及下载依赖的网络。SQLite 使用纯 Go 驱动，普通 CLI 构建不需要 CGO 或独立数据库；macOS 原生桌面版另需 Apple SDK，见 [macOS 构建](MACOS_DESKTOP.md#在-mac-上构建-app-和-dmg)。

在仓库根目录执行：

```sh
(cd Web && npm ci && npm run build)
go build -o bin/worklog ./cmd/worklog
./bin/worklog
```

访问 `http://127.0.0.1:12450`。首次启动自动创建数据库并迁移；默认数据位于仓库下的 `data/`。验证服务可用：

```sh
curl --fail http://127.0.0.1:12450/healthz
```

预期返回 `{"ok":true}`。此步骤不需要 Git tag。不要使用个人数据库运行下文的集成测试。

## 前端热更新

只开发前端时，在 `Web/` 中运行 `npm run dev` 即可，默认数据保存在浏览器本地，不连接 Go。

需要前后端联调时，先运行上面的 Go 服务，再将 `Web/public/config.json` 暂时改为：

```json
{"apiBaseUrl":"/api","businessTimezone":"Asia/Shanghai"}
```

另开终端：

```sh
cd Web
npm run dev
```

打开 Vite 输出的地址（通常为 `http://localhost:5173`）。只有 `/api` 被代理到 `127.0.0.1:12450`；`config.json` 仍由 Vite 从 `public/` 提供。业务时区应与 Go 配置一致。使用默认开发端口；若改用其他端口，需要同步调整后端 `allowed_origins`。

后端不可用时显示错误，不自动切换本地模式。完成联调后将 `public/config.json` 恢复为 `{}`，避免把个人联调配置发布出去。`npm run preview` 不使用开发代理。不要将开发服务器公开到互联网。

## 代码结构

```text
cmd/worklog/                 HTTP / stdio、迁移与 SQLite 备份恢复命令
cmd/worklog-desktop/         Windows 托盘 / macOS 菜单栏入口（desktop tag）
internal/app/                共用 HTTP 监听、重启与优雅关闭
internal/desktop/            平台原生菜单、浏览器、单实例与用户目录保护
internal/config/             JSON 配置、内置默认值和私有令牌文件
internal/domain/             模型、身份、公开错误和输入／富文本校验
internal/service/            共用业务、事务、版本／幂等
internal/repository/         Repository 和事务接口
internal/storage/            SQLite / PostgreSQL 与各自内嵌迁移
internal/transport/rest/     HTTP、静态文件、CORS 和错误响应
internal/transport/mcp/      MCP SDK、工具和审计
internal/transport/security/ 身份解析、Host / Origin 和调用限制
internal/integration/        实际数据库、HTTP / MCP / stdio 集成测试
Web/src/                    Vue 前端
Web/tests/                  单元、组件、真实后端适配层和浏览器测试
```

REST 和 MCP 使用同一 Service，不应另写一套工具业务规则。手写参数化 SQL，无 ORM；  
方言差异限制在 storage，SQLite 和 PostgreSQL 使用独立迁移。

## 存储与并发约定

- 表包括 workspaces、projects、categories、tasks、todos、worklogs、idempotency、resource_versions 和 goose 版本表。资源复合主键／外键包含 user_id，数组用显式 position 保序。
- SQLite 使用本地 WAL，每个连接启用 foreign_keys、5000ms busy_timeout、synchronous NORMAL，连接池最多 8 个。读事务提供一致性快照，写事务使用 `BEGIN IMMEDIATE`，不用进程 mutex 代替数据库锁；锁竞争返回 `503 DATABASE_BUSY`。
- PostgreSQL 使用 pgx stdlib、BOOLEAN / JSON、BIGINT revision。写事务 READ COMMITTED READ WRITE 并锁拥有者行，读事务 REPEATABLE READ READ ONLY。
- 权限、版本和最终引用状态在业务层校验，写入原子提交；出错、取消或 panic 回滚，COMMIT 成功后不再 ROLLBACK。
- 工作台写入增加根 revision，集合及逐资源版本协调更新。resource_versions 保留删除墓碑，防止复用客户端 ID 后旧版本覆盖。
- 删除项目会同时删除其关联任务；分类交换／删除在同一事务中调整任务分类，失败不留下部分状态。
- 幂等键、规范化请求指纹和首次成功响应同事务保存，重放不增加版本。详见 [REST API](REST_API.md)。
- 业务长度按 UTF-16 单位校验，幂等键按 UTF-8 字节校验，不能以 MCP Schema 的字符长度限制替代 Service 校验。
- SQL 占位符转换只在 storage。固定 SQL 不得包含字面量 `?` 或 PostgreSQL `?` 操作符；当前转换器不是 SQL 词法解析器。
- 名称键采用 Go 的 Unicode 简单小写；前端按码点转换并处理 İ，不使用整串 `toLowerCase()` 的上下文规则。颜色和名称的两端契约样本在 `internal/domain/testdata/`。
- 日志读取适配器无版本缓存；`worklogSaving.js` 在采纳响应时绑定正文和版本，保存期间的新编辑只由成功写入推进基准。
- 所有工作台修改在 finalize 中检查 8 MiB 快照预算；旧超限状态只允许严格缩减，详见 REST API 的恢复说明。
- 完整快照适合小规模个人数据，不是高吞吐团队协作架构。没有自动旧数据导入、自动冲突合并或账号系统。

## 后端测试

```sh
go test ./...
go test -race ./...
go vet ./...
```

默认执行 SQLite、HTTP / MCP / stdio 和备份恢复等测试。  
未配置 PostgreSQL 时相关用例明确 SKIP，不能算作 PostgreSQL 已通过。

使用一次性 PostgreSQL 数据库，测试用户需有 CREATE SCHEMA 权限，每个测试使用独立 schema：

```sh
export WORKLOG_TEST_POSTGRES_DSN='postgres://testuser:password@127.0.0.1:5432/testdb?sslmode=disable'
go test ./... -count=1
go test -race ./... -count=1
```

可选的本机容器准备方式：

```sh
docker run --rm -d --name worklog-test-pg -p 127.0.0.1:55432:5432 \
  -e POSTGRES_USER=worklog_test -e POSTGRES_PASSWORD=test_only_password \
  -e POSTGRES_DB=worklog_test postgres:18-alpine
# 等待 pg_isready 后，以 55432 端口配置 WORKLOG_TEST_POSTGRES_DSN。
# 用完后：docker stop worklog-test-pg
```

## 前端与真实后端联调

```sh
cd Web
npm ci
npm test
npm run build
```

`npm test` 包含 Node 单元和 Vue / jsdom 组件测试。组件测试也可单独执行 `npm run test:components`；专用入口在加载测试工具前固定使用测试模式，避免继承 `NODE_ENV=production` 导致 Vue Test Utils 无法记录组件事件。不要用生产模式直接启动 Vitest 替代该入口。

真实后端适配层测试必须使用一次性数据库，不能使用个人数据：

```sh
# 终端 1，从仓库根目录进入 Web；自动构建并使用临时配置和数据库。
cd Web
node tests/start-backend.js
# 终端 2，同样从仓库根目录进入 Web：
cd Web
WORKLOG_TEST_API_URL=http://127.0.0.1:22450/api npm run test:backend
```

测试启动器使用独立 JSON，不继承生产业务配置；结束后通过 Ctrl+C 停止后端并清理临时目录。测试工具的 `WORKLOG_TEST_*`、浏览器选择与 CI 变量只用于测试编排，不是应用配置入口。

## 浏览器 smoke 与 CI

```sh
cd Web
npm run build
npx playwright install chromium webkit
WORKLOG_TEST_WEBKIT=1 npm run test:e2e
# 使用本机 Chrome 时：
# WORKLOG_CHROMIUM_EXECUTABLE=/usr/bin/google-chrome npm run test:e2e
```

Playwright 自动构建并启动一次性 SQLite 后端，不访问个人数据库。  
覆盖日志与待办持久化、409 后停止自动重放以及显式重载，不等价于完整视觉、移动端拖动或真实 macOS / iOS Safari 验收。

[CI 配置](../.github/workflows/test.yml) 包括 PostgreSQL、Go vet / race、前端单元／组件、构建、Chromium / WebKit smoke 和真实后端适配联调。  
是否实际通过应查看本次运行结果。Docker 镜像构建、实际 HTTPS 代理、完整视觉／移动端交互需要在对应环境另外验证。

## 构建发布包

发行打包需要完整 Git 仓库和有效标签；从源码 ZIP 可以构建普通 CLI，但不能直接运行发行打包脚本。

| 目标 | 命令 | 产物（位于 `bin/`） |
| --- | --- | --- |
| Linux amd64 / arm64 | `bash scripts/build-linux.sh` | `worklog-v<版本>-linux-x86_64.tar.zst`、`worklog-v<版本>-linux-arm64.tar.zst` |
| Windows x64 | `bash scripts/build-windows.sh` | `worklog-v<版本>-windows-x86_64.exe` |
| macOS 当前架构 | 在 Mac 上执行 `bash scripts/build-macos.sh` | `worklog.app` 及 `worklog-v<版本>-macos-arm64.dmg` 或 `macos-x86_64.dmg` |

脚本均执行 `npm ci` 和前端生产构建。Go 不在 PATH 时可用 `GO=/path/to/go bash scripts/build-linux.sh`，其他构建脚本也支持 `GO`。

- Linux 额外需要 tar、zstd。脚本使用 `CGO_ENABLED=0` 交叉构建两种架构，目标主机不需要编译工具。安装脚本依赖发布包布局，**不要直接在源码目录运行 `scripts/install-linux.sh`**。
- Windows 可在 Linux 交叉构建，不需要 Windows SDK、MinGW 或 Wine。资源编译器由脚本下载固定版本，细节见 [Windows 构建](WINDOWS_DESKTOP.md#构建)。
- macOS 需要 CGO、Xcode Command Line Tools 和 Apple SDK，不能以普通 Linux 交叉编译代替。另一架构、DMG 检查及签名限制见 [macOS 构建](MACOS_DESKTOP.md#在-mac-上构建-app-和-dmg)。

### 版本与标签

三个脚本共用 `scripts/release-version.sh`，通过以下命令取当前 HEAD 可达的最近标签，并验证格式：

```sh
git describe --tags --abbrev=0 --match 'v[0-9]*' HEAD
```

标签必须为 `vX.Y.Z`，可带预发行／构建后缀，例如 `v0.2.0-rc.1`。这不是按标签创建时间或版本大小取最新值。标签后的开发提交沿用该标签版本，正式发布应在对应标签提交上构建。

不读取根目录 `VERSION`，也不接受 `VERSION` 环境变量覆盖。标签缺失或格式非法会报错。`Web/package.json` 的版本仅为内部 npm 元数据；Linux 包及安装目录中的 `VERSION` 是脚本生成的安装记录。

已有完整克隆使用 `git fetch --tags`；浅克隆使用 `git fetch --unshallow --tags`。CI checkout 应设置 `fetch-depth: 0`。

### 发布步骤

先运行相关测试，再提交代码、脚本和文档。以下版本号只是示例，请替换：

```sh
git tag -a v0.2.0-rc.1 -m "Release v0.2.0-rc.1"
bash scripts/test-release-version.sh
bash scripts/build-linux.sh
bash scripts/build-windows.sh
# 在 Mac 上：bash scripts/build-macos.sh
git push origin HEAD
git push origin v0.2.0-rc.1
```

检查包内文件、版本和架构后，在 GitHub 创建 Release 并上传对应产物。构建脚本不负责创建 GitHub Release，也不提供自动更新。桌面版发布前应按平台文档进行实机检查；交叉编译成功不代表目标系统已运行通过。
