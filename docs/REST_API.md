# REST API

Vue 工作台使用完整快照 API；细粒度资源接口供其他客户端集成，与 MCP 和快照共享同一业务 Service 及数据库，不是第二套数据。

## 通用约定

- 基础路径 `/api`，成功均为 **200 + JSON**，没有 `data` 包装。
- 认证、Host / Origin 检查及权限见[身份认证与安全](AUTHENTICATION.md)。本地默认可匿名访问 REST，远程使用 Basic 或 Bearer。
- 个人响应使用 `Cache-Control: private, no-store`。
- 请求体要求 JSON，拒绝未知字段、重复 key、非法类型及必填字段的 null。
- 日期严格为 `YYYY-MM-DD`；revision / version 为非负 JavaScript 安全整数。
- 请求体不能指定 userId 或所有者，路径 ID 应进行 URL 编码。

错误结构：

```json
{
  "error": { "code": "VERSION_CONFLICT", "message": "数据已被其他页面修改，请备份草稿并重新加载" },
  "requestId": "req_..."
}
```

常见状态码：

- `400` 输入非法；
- `401` 未认证；
- `403` 权限或请求可信性检查失败；
- `404` 不存在；
- `409` 版本／幂等冲突；
- `413` 正文过大；
- `429` 调用过频；
- `500` 服务故障；
- `503` 繁忙或超时。

## 端点

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET / PUT | `/api/dashboard` | 完整工作台快照 |
| GET / PUT | `/api/projects` | 项目集合 |
| GET / PUT | `/api/categories` | 分类集合 |
| GET / POST | `/api/tasks` | 查询／创建任务 |
| GET / PATCH / DELETE | `/api/tasks/{id}` | 单任务操作 |
| GET / POST | `/api/todos` | 查询／创建待办 |
| PATCH / DELETE | `/api/todos/{id}` | 更新／删除待办 |
| GET | `/api/today` | 业务时区中的今天日志 |
| GET | `/api/worklog/dates` | 有内容的日志日期 |
| GET / PUT | `/api/worklog/{date}` | 按日期读取／保存日志 |
| GET | `/healthz` | 无认证健康检查，仍校验 Host |

完整字段与校验规则沿用前端协议参考：

- [核心 HTTP API 与错误处理](../Web/docs/API说明.md)
- [工作台快照](../Web/docs/API工作台.md)
- [工作日志与富文本结构](../Web/docs/API工作日志.md)
- [工作日历静态 JSON 与自定义](WORK_CALENDAR.md)

日志和日历不属于 dashboard 快照。日历不再通过 REST API 提供：前端在本地和后端模式下都读取同目录的 `work-calendar.json`，默认包含 2026 年节假日／调休，没有日历写 API。

## 项目集合

`GET /api/projects` 返回：

```json
{"projects":[{"id":"client-uuid","name":"项目"}],"projectRevision":1,"revision":1}
```

`PUT /api/projects` 提交完整集合和当前集合版本：

```json
{"projectRevision":1,"projects":[{"id":"client-uuid","name":"新名称"}]}
```

遗漏的项目被删除，其任务在同一事务内移至 `projectId: ""`，不会删除任务。删除项目受破坏性权限限制。

## 分类集合

`GET /api/categories` 返回 `{ categories: ["开发", ...], categoryRevision, revision }`。

`PUT /api/categories` 使用旧名称映射，允许交换名称：

```json
{
  "categoryRevision": 1,
  "categories": [
    {"originalName":"开发", "name":"产品"},
    {"originalName":"产品", "name":"开发"},
    {"originalName":"", "name":"新分类"}
  ]
}
```

`originalName` 和 `name` 都必填，新建用空 originalName。  
未出现在映射中的分类被删除，其任务移至结果首分类，至少保留一个分类。  
批量分类管理属于受控破坏性能力。

两类集合 PUT 均返回 `{ projects, categories, projectRevision, categoryRevision, revision, updatedTasks }`，  
其中 updatedTasks 为发生变化的完整任务（含 version）。  
集合接口也接受 `expectedRevision` 作为版本输入别名，但不能同时提交两种版本字段。

## 任务

- `GET /api/tasks` 返回 `{ tasks: [完整任务 + version], revision }`。
- 可按 `projectId`（空表示未分配）、`status`、`tag`、`priority`、`search`、`startDate`、`endDate` 筛选。
- search 在 title / description 中大小写不敏感匹配。日期筛选为闭区间排期重叠，单日期视为单日，启用范围后排除无排期任务。
- 保留数组顺序，不隐式分页或排序。
- `GET /api/tasks/{id}` 返回完整任务及 version，兼容数字及旧字符串 ID。
- `POST /api/tasks` 必填 title；其他字段见快照协议。默认 description / projectId / 日期为空，status 为 planned，分类为“开发”或首分类，priority 为“中”，color 为 white。后端不自动补开始／截止日期。
- `PATCH /api/tasks/{id}` 必填 `version`，也接受 `expectedVersion` 别名，只更新显式给出的业务字段，不能改变 id / 所有者。
- `DELETE /api/tasks/{id}` 提交 JSON `{ "version": 1 }`，需要破坏性权限。

## 待办

- `GET /api/todos` 返回 `{ todos: [完整待办 + version], revision }`，支持 `done=true/false` 和 `search` 筛选，完成不重排。
- `POST /api/todos` 必填 title，done 默认为 false。
- `PATCH /api/todos/{id}` 必填 version，可修改 title、done。
- `DELETE /api/todos/{id}` 提交同样的版本 JSON，需要破坏性权限。

## 版本、幂等与失败恢复

- dashboard 使用根 `revision`；项目／分类集合使用各自 revision；任务／待办和每日日志使用各自 version。
- 每次成功工作台写入提升根 revision，MCP／细粒度接口修改也会让旧页面快照失效。日志单独计版。
- 旧版本写入返回 `409 VERSION_CONFLICT`，不自动覆盖；显式 PATCH 即使字段值相同也消耗 version。
- 创建任务／待办的 POST 需要 `Idempotency-Key` 头或 body `idempotencyKey`，同时提供时必须一致。ID 由服务端生成 UUID。
- 幂等键为 1–200 UTF-8 字节，拒绝控制字符；记录按可信身份隔离且不自动过期。
- 同键同规范化输入重放首次成功响应，不重复创建或提升 revision；同键不同输入返回 `409 IDEMPOTENCY_CONFLICT`。重放可能是旧响应，后续修改前重新读取。
- 除明确的幂等 POST 重放外，旧版本重复写返回 409。超时可能发生在提交之后，先重新读取确认，不盲目重发快照或日志。
- Vue 的日志草稿与失败状态按日期保留在内存；写失败暂停该日期自动提交，409 后先复制草稿，再明确确认放弃并重新加载。不要把内存草稿当作持久备份。

## 调用示例

以下适用于本地默认模式，远程需额外使用 `-u worklog:...` 或安全注入的 Bearer 头：

```sh
curl -s http://127.0.0.1:8080/api/dashboard
curl -s http://127.0.0.1:8080/api/today
curl -s -X POST http://127.0.0.1:8080/api/tasks \
  -H 'Content-Type: application/json' -H 'Idempotency-Key: unique-request-001' \
  -d '{"title":"实现后端","status":"progress"}'
# 使用实际返回的 ID 和当前 version：
curl -s -X PATCH http://127.0.0.1:8080/api/tasks/REAL_ID \
  -H 'Content-Type: application/json' -d '{"version":1,"status":"done"}'
curl -s -X PUT http://127.0.0.1:8080/api/worklog/2026-10-21 \
  -H 'Content-Type: application/json' \
  -d '{"version":0,"content":{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"完成后端联调"}]}]}}'
```

富文本完整保存为 Tiptap JSON，不转 HTML 或纯文本，不静默移除未知节点。  
上限 1 MiB、64 层节点、50000 节点，JSON 本身另限 128 层。颜色／语言属性和节点父子结构均经服务端校验。

## 工作台容量与旧数据恢复

所有工作台写入（完整快照、细粒度 REST 和 MCP）共用 **8 MiB** 容量预算，
按规范化、推进版本后的完整 dashboard JSON 编码计量，包含 UTF-8 和 JSON 转义字节。
这与 HTTP 请求体的 10 MiB 限制不同，避免多次小请求累积成浏览器无法提交的快照。
超限返回 `413 DASHBOARD_CAPACITY_EXCEEDED`；业务数据、版本与幂等记录均不提交。

升级不会自动修改或截断既有超限数据。此类工作台仍可读取，且允许总编码字节数严格减少的写入，
即使一次缩减后仍大于 8 MiB。先读取任务及其 version，通过 `PATCH /api/tasks/{id}`
分批缩短 description，或用具备删除权限的身份删除不需要的任务；每次操作使用最新版本。
单次 PATCH 仍需小于 HTTP 请求体上限。恢复到预算内后即可正常使用浏览器快照保存。
日志独立存储，不计入工作台容量。
