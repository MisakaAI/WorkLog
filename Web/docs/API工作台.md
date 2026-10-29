# 工作台快照 API

工作台的项目、分类、任务、待办及顶部个人文字作为一个完整快照读取和保存。前端的搜索、筛选、计数、看板、列表和甘特图由此快照在浏览器中计算，不会额外请求查询接口。

## GET `/dashboard`

默认返回当前身份未归档的项目及其任务，以及分类、待办和工作台文字。使用 `GET /dashboard?includeArchived=true` 可读取包含归档项目的完整数据，并返回 `includeArchived: true`。首次读取不创建示例业务数据；默认分类和空集合如下：

```json
{
  "revision": 0,
  "personalLabel": "个人工作台",
  "projects": [],
  "categories": ["开发", "临时", "个人", "产品", "设计", "调研"],
  "tasks": [],
  "todos": []
}
```

返回范围内的集合保留数组顺序，不分页。`personalLabel` 是根字段。快照的业务字段如下：

```json
{
  "revision": 0,
  "personalLabel": "个人工作台",
  "projects": [{ "id": "project-uuid", "name": "项目名称", "archived": false }],
  "categories": ["开发"],
  "tasks": [{
    "id": "task-uuid",
    "title": "任务名称",
    "description": "",
    "projectId": "project-uuid",
    "status": "planned",
    "tag": "开发",
    "priority": "中",
    "startDate": "2026-10-20",
    "date": "2026-10-21",
    "color": "white"
  }],
  "todos": [{ "id": "todo-uuid", "title": "待办", "done": false }]
}
```

## PUT `/dashboard`

提交基于最近读取结果的快照，包含原 `revision` 和本次所有业务数据变更。成功时返回实际保存的快照，`revision` 加一；失败时整次修改不保存。日志不受影响。

普通快照会保留未下发的归档项目及任务，其他省略资源视为删除。管理归档数据时，先读取 `?includeArchived=true`，再携带 `includeArchived: true` 提交完整快照。删除项目必须同时从快照移除它的关联任务；服务端校验最终引用，不替客户端隐式修改。

```json
{
  "revision": 0,
  "personalLabel": "我的工作台",
  "projects": [],
  "categories": ["开发"],
  "tasks": [],
  "todos": []
}
```

服务端以认证身份确定数据所有者，不接受客户端选择 `userId`。写入前会校验整个快照；版本冲突或任一字段校验失败时不会部分保存。重发已成功请求所使用的旧 `revision` 会得到 `409`，不会重复创建数据。

分类改名、交换或删除时，客户端必须同步任务的 `tag`，再提交最终快照。浏览器删除分类会把任务移到保存列表的首分类。项目删除的立即生效和确认行为见[前端使用限制](../README.md#保存与使用限制)。

## 字段规则

### Project

- `id` 是非空且唯一的客户端 ID，不能为 `__all__`；新建 ID 由前端生成。
- `name` 去除首尾空白后非空，最多 40 个 UTF-16 码元；同一工作台内不区分大小写唯一。
- `archived` 为布尔值，省略时为 `false`；归档保留项目和关联任务，但默认视图不展示。
- 数组顺序决定项目显示及选择顺序。“未分配”是前端选项，不是项目记录。

### Category

- `categories` 必须为非空数组；每个名称去除首尾空白后非空，最多 40 个 UTF-16 码元且不重复（按 Unicode 逐码点简单小写判重，与 Go `strings.ToLower` 一致；不做语言相关转换或 Unicode 规范化）。
- 数组顺序保留。分类可自定义；默认新任务使用“开发”，该分类不存在时使用第一个分类。

### Task

- `id` 非空且在任务集合中唯一。快照兼容安全整数 ID；字符串 ID 与其相同文本形式按重复处理。新建任务由前端生成字符串 ID。
- `title` 去除首尾空白后非空，最多 100 个 UTF-16 码元；`description` 为可空纯文本字符串。
- `projectId` 必须是现有项目 ID 或空字符串；`__all__` 是筛选标记，不是合法归属。
- `status` 为 `planned`、`progress`、`done` 之一。
- `tag` 必须属于当前 `categories`。
- `priority` 为 `低`、`中`、`高`、`极高` 之一；`color` 为 `white`、`red`、`orange`、`blue`、`green` 之一。
- `startDate` 和 `date` 各为合法日期或空字符串；两者均填写时，截止日期不得早于开始日期。甘特图按自然日显示，含区间首尾；单日期按单日处理。
- 后端不自动补充任务日期。新建默认值及拖动状态时补日期由前端负责。

### Todo

- `id` 非空且在待办集合中唯一，同样兼容安全整数 ID。
- `title` 去除首尾空白后非空，最多 100 个 UTF-16 码元；`done` 必须为布尔值。
- 待办数组顺序按提交结果保留，完成状态变化不会重新排序。

### personalLabel

纯文本字段；输入最多 255 个 UTF-16 码元，保存时去除首尾空白。可为空字符串；服务端不将其解析为 HTML。

## 校验与扩展接口

JSON 中必需的快照字段不能省略或设为 `null`；未知字段、非法枚举、日期、ID、重复名称及无效资源引用会被拒绝。工作台请求体最大 10 MiB；所有写入口另统一限制保存后的完整快照为 8 MiB（按服务端 JSON 编码字节数计算），预留传输余量。容量超限返回 `413 DASHBOARD_CAPACITY_EXCEEDED`，整次修改回滚，不会静默截断描述。旧版本已有超限数据仅允许继续缩减，恢复方法见 [REST API](../../docs/REST_API.md#工作台容量与旧数据恢复)。

后端还提供项目／分类集合、单任务和单待办 REST 接口，维护对应资源版本及集合版本，与快照共享同一份数据。Vue 的常规工作台修改使用快照；分类管理例外，先读取分类集合版本，再调用 `PUT /categories`，成功后重读 dashboard。集合接口的请求、权限和响应范围见[REST API 说明](../../docs/REST_API.md#分类集合)。
