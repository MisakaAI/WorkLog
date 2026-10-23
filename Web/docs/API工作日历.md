# 工作日历静态 JSON

工作日历已从后端 API 改为前端静态资源，不再提供 `GET /api/work-calendar`。

本地模式和后端模式均读取页面同目录的 `work-calendar.json`，不使用后端 API 基础 URL，也不读取浏览器的 `worklog.v2.calendar` 缓存。源文件为 `Web/public/work-calendar.json`，构建时会复制到 `Web/dist/work-calendar.json`。

默认包含 2026 年节假日和调休。字段、优先级、单休示例、取消节日标记以及部署更新方式见[工作日历自定义](../../docs/WORK_CALENDAR.md)。

日期面板和甘特图共享同一份加载结果；请求、JSON 解析或结构校验失败时显示错误和重试入口，并暂按周一至周五工作、周末休息显示，不阻止任务和日志编辑。没有日历配置 UI 或写 API。
