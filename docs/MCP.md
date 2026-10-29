# MCP 客户端指南

MCP 让 AI 客户端直接查询项目、任务、待办和工作日志，也可按授权创建任务、更新进度或保存日志。例如，可以让 AI 汇总今天的工作、整理未完成事项，再确认需要写入的内容。

MCP、REST 和浏览器工作台共用同一份数据，修改会相互可见，不是独立的 AI 工作区。建议先接入只读令牌，确认查询结果后再按需授予写权限。认证与权限边界见[身份认证与安全](AUTHENTICATION.md)。

## 1. 启动服务并检查健康状态

先准备可用的服务 JSON 配置，按实际位置执行：

```sh
./bin/worklog --conf /absolute/path/config.json
```

另开终端检查（默认本地地址）：

```sh
curl --fail --silent --show-error http://127.0.0.1:12450/healthz
```

预期返回 `{"ok":true}`，表示数据库可用；这不代表 MCP 认证或工具调用已成功。`/healthz` 无需认证，但仍检查 Host。若修改了监听地址、端口或白名单，应相应调整 URL。

服务业务配置只从 JSON 读取，不从环境变量或 `.env` 读取。HTTP MCP 地址为 `/mcp`，即使在本机也必须使用 Bearer，不接受 Basic 或工具参数中的身份。

## 2. 配置 HTTP 客户端

### Codex

先确认服务配置中的凭证来源：显式设置 `write_token` 时使用该值；否则读取 `token_file`。内置默认路径是 `./data/mcp-token`，但仓库的 `config.example.json` 使用 `/var/lib/worklog/mcp-token`；相对路径以服务进程的工作目录为基准。

下面仅适用于使用默认令牌路径的本地服务：

```sh
export WORKLOG_MCP_TOKEN="$(cat ./data/mcp-token)"
```

若服务 JSON 显式设置了 `write_token`，应安全地将该值注入客户端环境，而不是读取可能过期的文件。
只读 AI 使用服务配置的 `read_token`，不要载入自动生成的写令牌。不要在日志、截图或共享终端中输出凭证，也不要提交到仓库。

`~/.codex/config.toml`：

```toml
[mcp_servers.worklog]
url = "http://127.0.0.1:12450/mcp"
bearer_token_env_var = "WORKLOG_MCP_TOKEN"
```

保存配置后，在已设置令牌环境变量的终端执行 `codex`，再按下文验证连接。

远程 URL 改为 `https://worklog.example.com/mcp`，由 Nginx 保留 Authorization、Host、Origin 并代理。  
`bearer_token_env_var` 是 **Codex 第三方客户端配置，不是 WorkLog 服务端配置**；上面的 `export` 仅向 Codex 提供凭证。客户端环境变量名可自定，与该字段一致即可。

### 其他 Streamable HTTP 客户端

```json
{
  "url": "http://127.0.0.1:12450/mcp",
  "headers": { "Authorization": "Bearer <从客户端安全环境注入，不要提交到仓库>" }
}
```

配置外层键及环境注入语法因客户端而异，不要直接把示例占位值当作凭证。

## 3. 连接后验证 tools/list

重启或重新连接客户端，通过客户端的 MCP 调试功能发送 `tools/list`，应能看到下表的 **21 个工具**及其完整 JSON Schema。再调用不写数据的 `get_dashboard_summary`，确认统计与浏览器默认视图一致。默认查询排除归档项目及其任务；`list_projects` 可用 `includeArchived: true` 查看全部项目。

只读连接也能列出全部工具；**看见工具不等于获准执行**。不要用删除或清空日志来测试连接。若失败，401 先查令牌来源，403 先查 Host / Origin 与操作权限，详见[认证排查表](AUTHENTICATION.md#7-常见问题)。

## 4. 工具与写入约束

共 21 个工具：

| 分类 | 工具 |
| --- | --- |
| 项目 | `list_projects`, `create_project`, `rename_project`, `delete_project`, `reorder_projects` |
| 任务 | `list_tasks`, `get_task`, `create_task`, `update_task`, `move_task`, `delete_task` |
| 待办 | `list_todos`, `create_todo`, `set_todo_done`, `delete_todo` |
| 分类 | `list_categories`, `manage_categories` |
| 日志 | `get_worklog`, `get_worklog_dates`, `save_worklog` |
| 统计 | `get_dashboard_summary` |

通过客户端的 `tools/list` 查看每个工具的完整 JSON Schema 和描述。额外字段被拒绝，Service 还会独立校验。

- 更新使用 `expectedVersion` / `expectedRevision`；创建项目也需要当前 `expectedRevision`。先读取再修改，版本冲突后重新读取并确认如何合并，不要盲目覆盖。
- 创建使用 `idempotencyKey`，仅对相同请求复用同一个键；同键重试和冲突语义见 [REST API](REST_API.md#版本幂等与失败恢复)。
- `move_task` 原子修改项目和状态；`set_todo_done` 接受 boolean，不是 toggle。
- 工具业务失败返回 `isError`，正文含与 REST 一致的公开 error code/message；不要把错误当作成功响应继续修改。
- MCP 修改项目、任务、待办或分类会提升工作台根 `revision`，旧浏览器标签页保存时可能收到 409。日志按日期独立计版，不提升工作台版本。
- 只读令牌不能写入，即使开启破坏性操作也不例外。普通写权限仍能覆盖已有正文，不能替代备份。
- 默认禁止删除项目／任务／待办、批量分类管理和保存空日志。模型确认参数不能授权；是否在服务 JSON 中开启 `mcp_allow_destructive` 由运维决定。开关是长期授权，会影响所有 MCP 写客户端，不是逐次审批，也不扩大 REST Bearer 权限。
- 工具审计记录可信身份、工具、写入类型、结果和耗时，不记录私人参数。

## 5. 本地 stdio（替代 HTTP）

stdio 不需要先启动 HTTP 服务。Claude Desktop 等兼容客户端可直接启动本地进程：

```json
{
  "mcpServers": {
    "worklog": {
      "command": "/absolute/path/WorkLog/bin/worklog",
      "args": ["--conf", "/absolute/path/stdio.json", "stdio"]
    }
  }
}
```

独立的 `/absolute/path/stdio.json` 示例（这是服务业务配置，不是客户端配置）：

```json
{
  "db_driver": "sqlite",
  "db_dsn": "/absolute/path/WorkLog/data/worklog.db",
  "token_file": "/absolute/path/WorkLog/data/mcp-token",
  "static_dir": "",
  "mcp_allow_destructive": false,
  "stdio_read_only": true
}
```

替换所有绝对路径，并确保 `user_id` 与 HTTP 配置一致（默认 `owner`）。示例强制只读；确需写入时再将 `stdio_read_only` 改为 `false`。连接后同样通过 `tools/list` 和只读调用验证。

stdio 不要求 HTTP Bearer，身份来自可信 OS 用户和 JSON 配置；启动进程本身就是授予数据库访问能力。配置、数据库及其目录只应向可信用户开放。stdout 仅传协议，日志写 stderr。

可与 HTTP 进程共享同一本地 SQLite 文件，但不能并行运行不同版本的升级迁移。

## 6. SDK 与协议细节

服务使用官方 `github.com/modelcontextprotocol/go-sdk v1.8.0`，支持该 SDK 的稳定协议 `2026-07-28` 及兼容协商的旧协议。HTTP 采用无会话的 Streamable HTTP 和 JSON 响应，路径为 `/mcp`。

使用官方 Go SDK 接入时可选 `StreamableClientTransport`；实际连接、`ListTools` 和工具调用示例见 `internal/integration/system_test.go` 的 `ActualMCPAndRESTConsistency`。工具声明及 Schema 见 `internal/transport/mcp/server.go`。
