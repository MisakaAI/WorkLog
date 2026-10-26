# MCP 客户端指南

REST、Vue 与 MCP 共享同一业务数据。使用官方 `github.com/modelcontextprotocol/go-sdk v1.8.0`，支持该 SDK 的稳定协议 `2026-07-28` 及兼容协商的旧协议；HTTP 采用 sessionless Streamable HTTP + JSON 响应，路径 `/mcp`。

认证与破坏性授权详见[身份认证与安全](AUTHENTICATION.md)。  
MCP HTTP 即使在本机也必须使用 Bearer，不接受 Basic 或工具参数中的身份。不要把令牌提交到仓库。

## Codex：HTTP

默认本地模式可从自动生成的令牌文件载入凭证：

```sh
export WORKLOG_MCP_TOKEN="$(cat ./data/mcp-token)"
codex
```

若服务显式设置了 `WORKLOG_WRITE_TOKEN`，应安全地将该值注入客户端环境，而不是读取可能过期的文件。  
只读 AI 使用服务配置的 `WORKLOG_READ_TOKEN`。

`~/.codex/config.toml`：

```toml
[mcp_servers.worklog]
url = "http://127.0.0.1:12450/mcp"
bearer_token_env_var = "WORKLOG_MCP_TOKEN"
```

远程 URL 改为 `https://worklog.example.com/mcp`，由 Nginx 保留 Authorization、Host、Origin 并代理。  
客户端环境变量名可自定，与 `bearer_token_env_var` 一致即可。

## 其他 Streamable HTTP 客户端

```json
{
  "url": "http://127.0.0.1:12450/mcp",
  "headers": { "Authorization": "Bearer <从客户端安全环境注入，不要提交到仓库>" }
}
```

配置外层键及环境注入语法因客户端而异。  
支持官方 SDK 的客户端可使用 `StreamableClientTransport`；  
实际协议调用示例见 `internal/integration/system_test.go`。

## 本地 stdio

Claude Desktop 等兼容客户端可配置：

```json
{
  "mcpServers": {
    "worklog": {
      "command": "/absolute/path/WorkLog/bin/worklog",
      "args": ["stdio"],
      "env": {
        "WORKLOG_DB_DRIVER": "sqlite",
        "WORKLOG_DB_DSN": "/absolute/path/WorkLog/data/worklog.db",
        "WORKLOG_TOKEN_FILE": "/absolute/path/WorkLog/data/mcp-token",
        "WORKLOG_STATIC_DIR": "",
        "WORKLOG_MCP_ALLOW_DESTRUCTIVE": "false",
        "WORKLOG_STDIO_READ_ONLY": "false"
      }
    }
  }
}
```

替换所有绝对路径。stdio 身份由可信 OS 用户与启动环境决定，stdout 仅传协议、日志写 stderr。  
可与 HTTP 进程共享同一本地 SQLite 文件，但不能并行运行不同版本的升级迁移。只读客户端设置 `WORKLOG_STDIO_READ_ONLY=true`。

## 工具列表

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

- 写入使用 `expectedVersion` / `expectedRevision`，先读取当前版本再修改。
- 创建使用 `idempotencyKey`；同键重试和冲突语义见 [REST API](REST_API.md#版本幂等与失败恢复)。
- `move_task` 原子修改项目和状态；`set_todo_done` 接受 boolean，不是 toggle。
- 工具业务失败返回 `isError`，正文含与 REST 一致的公开 error code/message；不要把错误当作成功响应继续修改。
- MCP 修改会提升工作台根 revision，旧浏览器标签页保存时可能收到 409。
- 默认禁止删除、批量分类管理和保存空日志。模型确认参数不能授权；是否开启 `WORKLOG_MCP_ALLOW_DESTRUCTIVE` 由运维决定。
- 工具审计记录可信身份、工具、写入类型、结果和耗时，不记录私人参数。
