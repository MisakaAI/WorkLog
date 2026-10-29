# REST API

本文供接入其他程序的开发者查阅。Vue 工作台的常规修改整体读取和保存项目、任务、待办等数据，分类管理使用分类集合接口；其他客户端也可只修改单个任务或待办。这些接口与 MCP 使用同一份服务端数据。

先确认服务已启动，再按文末的[调用示例](#调用示例)读取数据。浏览器安装使用见[项目 README](../README.md)。

## 通用约定

- 基础路径 `/api`。业务成功响应为 **200**，除项目导出返回 ZIP 外均为 JSON，没有 `data` 包装。
- 认证、Host / Origin 检查及权限见[身份认证与安全](AUTHENTICATION.md)。本地默认可匿名访问 REST，远程使用 Basic 或 Bearer。
- 个人响应使用 `Cache-Control: private, no-store`。
- 除项目导入使用 ZIP 外，请求体要求 `application/json`；拒绝未知字段、重复键、非法类型及必填字段的 `null`。写接口不接受查询参数。
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
| GET | `/api/projects/export` | 导出项目 ZIP |
| POST | `/api/projects/import` | 导入项目 ZIP |
| GET / PUT | `/api/categories` | 分类集合 |
| GET / POST | `/api/tasks` | 查询／创建任务 |
| GET / PATCH / DELETE | `/api/tasks/{id}` | 单任务操作 |
| GET / POST | `/api/todos` | 查询／创建待办 |
| PATCH / DELETE | `/api/todos/{id}` | 更新／删除待办 |
| GET | `/api/today` | 业务时区中的今天日志 |
| GET | `/api/worklog/dates` | 有内容的日志日期 |
| GET / PUT | `/api/worklog/{date}` | 按日期读取／保存日志 |
| GET | `/healthz` | 运维健康检查，无需认证，仍校验 Host / Origin |
| GET | `/api/healthz` | 前端健康检查，使用与业务 API 相同的认证及 Host / Origin 检查 |

两个健康检查入口共用数据库 Ping、2 秒探测超时与最多 4 个并发探测的限制，不消耗业务 API / MCP 调用额度。
健康时返回 `200 {"ok":true}`；数据库不可用、探测超时或检查繁忙时返回 `503`。
`/api/healthz` 还可能返回认证或可信性检查错误；前端通过已配置的 API 基础 URL 追加 `/healthz` 访问，无需推断站点根路径。
健康检查不读写业务记录，成功不代表某次业务保存成功，也不会改变前端的本地／后端数据源选择。

工作台和日志的完整字段、富文本结构见：

- [工作台快照](../Web/docs/API工作台.md)
- [工作日志与富文本结构](../Web/docs/API工作日志.md)
- [工作日历静态 JSON 与自定义](WORK_CALENDAR.md)

日志不属于 dashboard 快照。工作日历是前端静态 JSON，没有读写日历的 REST API，见[工作日历自定义](WORK_CALENDAR.md)。

## 项目归档与快照范围

- 项目新增布尔字段 `archived`，历史项目默认 `false`。归档不删除项目或关联任务。
- `GET /api/dashboard` 默认只返回未归档项目及其任务；`GET /api/dashboard?includeArchived=true` 返回全部数据，并携带 `includeArchived: true`。
- `PUT /api/dashboard` 普通快照保存会保留未下发的归档项目和任务。管理归档数据时应先读取完整快照，再携带 `includeArchived: true` 和当前 `revision` 保存；设置项目 `archived: true/false` 即归档／取消归档。完整快照中删除项目时，客户端需同时移除其所有关联任务。
- `GET /api/projects` 默认隐藏归档项目，使用 `?includeArchived=true` 可读取全部；默认任务列表及摘要也排除归档项目的任务。
- `PUT /api/projects` 默认保留省略的归档项目；若确需删除归档项目，提交完整集合并附带 `includeArchived: true`。删除仍受破坏性权限限制。
- 输入项目名称确认删除只是前端交互，不是 API 授权参数。浏览器的保存与删除行为见[前端使用限制](../Web/README.md#保存与使用限制)。

## 项目 JSON ZIP 导入导出

这是项目数据交换功能，**不是** `worklog backup`：不包含数据库文件、用户凭证、版本历史、幂等记录、工作日志或待办，也不包含未分配到项目的任务。

- `GET /api/projects/export`：导出当前用户全部项目（包括已归档项目）及关联任务和分类。
- `GET /api/projects/export?projectId=ID`：只导出指定项目及其任务；归档项目同样支持，不存在返回 404。
- 成功响应为 `application/zip`，附件名为 `worklog-projects.zip`；压缩包只有 `projects.json`，包含 `format: "worklog-projects"`、`version: 1`、`projects`、`categories`、`tasks`。项目保留 `archived` 状态，任务字段与 dashboard 一致。
- `POST /api/projects/import`：以 `Content-Type: application/zip` 直接上传 ZIP 正文，不使用 multipart。请求上限 10 MiB，解压 JSON 上限 8 MiB；只接受上述单文件版本化格式，校验引用、分类、日期等字段，最终状态仍受工作台 8 MiB 容量预算约束。
- 导入需要写权限，不需要删除权限。导入在当前用户的单个事务内追加项目和任务，生成新的 ID 并重映射任务归属；同名项目自动添加 ` (导入 N)` 后缀并按 40 UTF-16 码元限制截断原名。分类按忽略大小写的名称合并，不覆盖已有项目、任务、待办或工作台文字。失败时整体回滚。
- 导入成功返回包含归档数据的 dashboard（`includeArchived: true`）和新版本；重复导入会追加新副本，不是幂等重放。超时后应重新读取确认结果，不要直接重试。
- 页面支持按项目或全部导出，只导出已保存的数据；本地模式生成同格式 ZIP，可日后上传后端导入。本地模式不提供导入按钮。
- 导入前会确认，并提示当前项目管理中未保存的修改将被丢弃。导入成功后立即刷新项目草稿和工作台，不需要再次点击保存。

## 项目集合

`GET /api/projects` 返回：

```json
{"projects":[{"id":"client-uuid","name":"项目","archived":false}],"projectRevision":1,"revision":1}
```

`PUT /api/projects` 提交完整集合和当前集合版本：

```json
{"projectRevision":1,"projects":[{"id":"client-uuid","name":"新名称","archived":false}]}
```

遗漏的未归档项目及其关联任务在同一事务内删除，其他项目和未分配任务不受影响。归档项目默认保留，只有提交 `includeArchived: true` 时才将遗漏的归档项目视为删除。**整个项目集合 PUT 都需要写入和破坏性权限**，即使本次仅新增、改名或归档，也不能使用 REST Bearer 写令牌。

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

两类集合 PUT 均返回 `{ projects, categories, projectRevision, categoryRevision, revision, updatedTasks }`。响应的项目和任务始终为默认可见范围：`projects` 不含归档项目，`updatedTasks` 仅包含未归档项目及未分配范围内发生变化的完整任务（含 `version`），即使请求带了 `includeArchived: true` 也如此。需要同步完整归档状态时，应再读取 `GET /api/dashboard?includeArchived=true`。

集合接口也接受 `expectedRevision` 作为版本输入别名，但不能同时提交两种版本字段。

## 任务

- `GET /api/tasks` 返回 `{ tasks: [完整任务 + version], revision }`。
- 可按 `projectId`（空表示未分配）、`status`、`tag`、`priority`、`search`、`startDate`、`endDate` 筛选。
- search 在 title / description 中大小写不敏感匹配。日期筛选为闭区间排期重叠，单日期视为单日，启用范围后排除无排期任务。
- 保留数组顺序，不隐式分页或排序。
- `GET /api/tasks/{id}` 返回完整任务及 `version`，兼容数字及旧字符串 ID。归档项目的任务返回 404，且不接受 `includeArchived` 参数；读取归档任务请使用完整 dashboard。
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
- 同键同输入重放首次成功响应，不重复创建或提升 revision；同键不同输入返回 `409 IDEMPOTENCY_CONFLICT`。比较使用适配后 JSON 对象的规范编码，不会先统一业务空白或默认值；省略字段和显式填写默认值、标题首尾空白不同，都可能产生冲突。重放可能是旧响应，后续修改前重新读取。
- 除明确的幂等 POST 重放外，资源仍存在时旧版本写入返回 409；成功删除后重发可能返回 404。超时可能发生在提交之后，先重新读取确认，不盲目重发快照或日志。
- Vue 的日志草稿与失败状态按日期保留在内存；写失败暂停该日期自动提交，409 后先复制草稿，再明确确认放弃并重新加载。不要把内存草稿当作持久备份。

## 调用示例

以下适用于本地默认模式，远程需使用 Basic 或安全注入的 Bearer 头。不要在共享命令或日志中直接写密码。创建后先检查返回的 JSON 和版本，再使用真实 ID 执行更新；幂等键应为每个新的创建请求生成唯一值：

```sh
curl -s http://127.0.0.1:12450/api/dashboard
curl -s http://127.0.0.1:12450/api/today
curl -s -X POST http://127.0.0.1:12450/api/tasks \
  -H 'Content-Type: application/json' -H 'Idempotency-Key: unique-request-001' \
  -d '{"title":"实现后端","status":"progress"}'
# 将 REAL_ID 和 version 替换为实际返回的值：
curl -s -X PATCH http://127.0.0.1:12450/api/tasks/REAL_ID \
  -H 'Content-Type: application/json' -d '{"version":1,"status":"done"}'
# 先 GET 指定日期，以下仅适用于该日期尚未保存、version 为 0 的情况：
curl -s -X PUT http://127.0.0.1:12450/api/worklog/2026-10-21 \
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
