# 工作日志 API

日志以日期为单位独立读取和保存，不包含在工作台快照中。正文使用编辑器的 Tiptap／ProseMirror JSON 结构，而不是 HTML 字符串。

## GET `/today`

返回服务端业务时区中的今天日志。业务时区默认为 `Asia/Shanghai`，可由后端配置。响应示例：

```json
{
  "date": "2026-10-19",
  "content": { "type": "doc", "content": [{ "type": "paragraph" }] },
  "updatedAt": null,
  "version": 0
}
```

未保存的日期返回上述空文档、`updatedAt: null` 和 `version: 0`，读取本身不会创建记录。已保存但正文为空的日志会返回实际更新时间和版本。

## GET `/worklog/{date}`

读取指定日期，可查询过去或未来日期。日期必须是严格合法的 `YYYY-MM-DD`。合法但尚未保存的日期返回空文档，不返回 `404`。

`updatedAt` 为 UTC ISO 8601 时间戳，固定三位毫秒，例如 `2026-10-20T16:30:00.123Z`。未保存时为 `null`。

## PUT `/worklog/{date}`

以读取到的 `version` 整体替换该日期日志。首次保存的版本应为 `0`；成功后返回完整日志资源，版本增加 1，更新时间由服务端生成。

```json
{
  "content": { "type": "doc", "content": [{ "type": "paragraph" }] },
  "version": 0
}
```

版本不符时返回 `409 VERSION_CONFLICT`，正文保持不变。空文档保存会保留日期版本记录，但从有内容日期列表中移除；保存空日志需要后端授予删除／清空权限。普通写权限仍可改写非空正文，不保证旧内容可恢复，详见[权限说明](../../docs/AUTHENTICATION.md#2-认证渠道与权限)。不要通过重读最新版本后自动提交旧草稿。

工作台前端的手动保存、定时检查自动保存、失焦保存和源码应用共用此接口。保存期间继续输入的内容作为新草稿保留；草稿和请求按日期隔离。写入失败后，该日期不会自动重放；冲突时先备份草稿，再明确确认放弃并重新读取。

日志请求最大 1 MiB，超限返回 `413`。

## GET `/worklog/dates`

返回有内容的日志日期，按升序去重：

```json
{ "dates": ["2026-10-19", "2026-10-21"] }
```

可选地提供 `startDate` 和／或 `endDate` 查询参数进行闭区间过滤；不带参数时返回全部日期。Vue 前端当前请求全量日期，不分页。空白文本或空段落不算有内容；非空文本及 `horizontalRule` 会被计入，只有分隔线也会显示日期标记。保存为空文档后该日期会从结果中移除。

## 正文结构与校验

后端会检查节点类型、父子关系、属性和文字格式标记（marks），不能只提交 `type: "doc"`。支持的节点：

- `doc`、`paragraph`、`text`、`heading`（level 2/3）
- `bulletList`、`orderedList`、`listItem`、`blockquote`
- `codeBlock`、`horizontalRule`、`hardBreak`

支持的 marks：`bold`、`italic`、`underline`、`strike`、`code`、`textStyle`（字体颜色）。合法的列表起始编号、列表类型、代码块语言及编辑器生成的合法属性会保留，以避免 JSON 往返丢失。

字体颜色仅接受可解析的 CSS 颜色值，包括合法的命名颜色、hex、RGB(A) 和 HSL(A) 格式；拒绝任意 CSS 表达式和资源 URL。脚本、任意 HTML、图片、嵌入、链接、未知节点和危险属性均不属于支持格式。服务端限制正文 1 MiB、节点深度 64 层及最多 50,000 个节点，并执行完整结构校验。
