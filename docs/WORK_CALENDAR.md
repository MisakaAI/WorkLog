# 工作日历自定义

本文用于调整周末、节假日和调休显示。修改前端的 `work-calendar.json` 即可，后端 `config.json` 没有日历配置项。旧的浏览器 `worklog.v2.calendar` 数据不再读取。

## 文件位置与生效方式

- **开发或构建前**：编辑 `Web/public/work-calendar.json`。Vite 开发服务直接提供该文件，`cd Web && npm run build` 会将它复制到 `Web/dist/work-calendar.json`。
- **Linux 或独立静态部署**：替换前端发布目录中的 `work-calendar.json`。Go 默认目录是 `Web/dist`；自定义 `static_dir` 时，替换该目录中的文件。升级可能覆盖此文件，请另存自定义副本。
- **Windows／macOS 桌面版**：资源嵌入程序，不能用磁盘文件覆盖。需在构建前修改 `Web/public/work-calendar.json`，再重新构建桌面版。
- **Docker**：可修改源文件后重建镜像，或在 Compose 对应用服务添加只读挂载：

  ```yaml
  volumes:
    - ./work-calendar.json:/app/web/work-calendar.json:ro
  ```

  保留原有数据卷；宿主机文件必须存在且容器用户可读。若修改了容器的静态目录，挂载目标也要相应调整。

本地模式与后端模式均从前端页面同目录读取 `./work-calendar.json`，支持子目录部署，不请求后端日历 API。磁盘静态部署替换文件后刷新页面即可，无需重启后端；配置不会自动轮询更新。每次加载使用 `no-store`，如使用 CDN 或反向代理缓存，还需确保该文件不被缓存或及时清理缓存。

默认文件为 [Web/public/work-calendar.json](../Web/public/work-calendar.json)，包含 **2026 年七个节日和六个调休上班日**。它不会在线自动更新；新年份需自行维护，未覆盖的日期仅按每周规则判断。

## JSON 结构

```json
{
  "workingWeekdays": [1, 2, 3, 4, 5],
  "holidays": [
    { "name": "元旦", "startDate": "2026-01-01", "endDate": "2026-01-03" }
  ],
  "adjustedWorkdays": [
    { "name": "元旦调休", "date": "2026-01-04" }
  ]
}
```

三个数组均为必填字段，可以使用空数组，但不要删除字段。JSON 不支持注释或尾随逗号。

| 字段 | 含义与约束 |
| --- | --- |
| `workingWeekdays` | 每周正常工作的星期编号：`0` 周日、`1` 周一，依次到 `6` 周六；只能填 0–6 的整数，不能重复 |
| `holidays` | 放假区间；`name` 是非空名称，`startDate`、`endDate` 是合法的 `YYYY-MM-DD` 自然日期，包含首尾两天；开始不能晚于结束，各区间不能重叠 |
| `adjustedWorkdays` | 特别指定的上班日期；`name` 是非空名称，`date` 是合法自然日期，同一日期不能重复 |

日期判定优先级：**调休上班日 > 节假日区间 > 每周工作日规则**。例如周日可以通过 `adjustedWorkdays` 标成上班；即使落在放假区间内，仍以上班为准。额外休息日则加入 `holidays`，一天假期的起止日期填同一天。

这些规则用于日期面板和甘特图中的日期类型、名称与休息日样式，不会自动修改任务日期、按工作日排期或删除日志。

## 示例：改为单休（周日休息）

```json
{
  "workingWeekdays": [1, 2, 3, 4, 5, 6],
  "holidays": [],
  "adjustedWorkdays": []
}
```

若仍要保留法定节假日，可只修改默认文件的 `workingWeekdays`，保留其他两个数组；若企业不按法定调休安排上班，应一并调整 `adjustedWorkdays`。

## 示例：不显示节假日和调休，只按每周规则

清空两个节日相关数组即可，无需删除整个 JSON 文件：

```json
{
  "workingWeekdays": [1, 2, 3, 4, 5],
  "holidays": [],
  "adjustedWorkdays": []
}
```

此时不再出现节日名或调休名，但周末仍作为休息日显示。

## 示例：不区分工作日与休息日，也不显示节假日和调休

把所有星期都设为工作日，并清空节日与调休：

```json
{
  "workingWeekdays": [0, 1, 2, 3, 4, 5, 6],
  "holidays": [],
  "adjustedWorkdays": []
}
```

所有日期会统一按普通工作日显示，不再出现休息日背景、节假日或调休标记。这不是隐藏日期面板或甘特图本身，也不是移除所有位置的普通“工作日”提示。不要把 `workingWeekdays` 设为空来尝试关闭标记：空数组表示每周没有正常工作日，所有未被调休覆盖的日期都会被视为休息日。

## 加载失败与检查

文件不存在、HTTP 错误、JSON 损坏或字段不合法都会触发界面的加载失败提示，可修正后点击“重试”或刷新页面。失败时暂按周一至周五工作、周末休息显示，不影响任务和日志编辑；这不等同于读取成功的自定义配置。

可在浏览器开发者工具的 Network 面板检查 `work-calendar.json` 请求，确认返回的是 JSON 而非代理或 SPA 回退的 HTML，并检查是否发布到了页面对应的目录。源码修改后可运行 `cd Web && npm test` 检查相关回归测试；默认发布日历的 2026 年内容也有测试校验，若有意改变默认安排，应同步调整相应测试预期。
