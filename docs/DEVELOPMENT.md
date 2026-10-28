# 开发与测试

构建与本地启动见 [README](../README.md#部署与启动)。  
发行版本以 Git tag 为准，打包与打标签流程见 [发布版本](../README.md#发布版本)；工具链和依赖版本以 `go.mod`、`Web/package-lock.json` 和 CI 配置为准，不在本文保存某次机器上的通过数量或阶段性验收结果。

## 前端热更新

保持 Go 服务运行，另开终端：

```sh
cd Web
npm run dev
```

Vite 将 `/config.json` 和 `/api` 代理到 `127.0.0.1:12450`，直接复用 Go 服务 JSON 配置派生的 API 地址和业务时区。无需环境变量；后端不可用时显示启动或连接错误，不会自动切换本地模式。不要将开发服务器公开到互联网。

## 代码结构

```text
cmd/worklog/                 HTTP / stdio、迁移与 SQLite 备份恢复命令
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
- 项目删除迁移任务，分类交换／删除在同一事务中调整，失败不留下部分状态。
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
# 在 Web 目录，自动构建并使用临时 JSON 配置和一次性 SQLite 数据库。
cd Web
node tests/start-backend.js
# 另一终端：
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
