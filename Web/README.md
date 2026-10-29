# WorkLog 前端

前端使用 Vue 3、Vite、Element Plus、Tiptap 和 Monaco，提供工作台、任务列表／看板／甘特图、待办和富文本日志，支持浅色与深色主题。

完整项目安装见[项目 README](../README.md)。本文介绍独立前端运行和数据模式；测试与发布流程统一见[开发文档](../docs/DEVELOPMENT.md)。

## 启动与构建

准备 Node.js 24.15+ 和 npm。以下命令在 `Web/` 目录执行：

```sh
npm ci
npm run dev
```

打开 Vite 输出的地址。默认 `public/config.json` 为 `{}`，使用浏览器本地数据，不需要后端，也没有示例项目、任务或日志。初始分类和每周工作日规则属于默认设置。

构建并预览：

```sh
npm run build
npm run preview
```

`dist/` 是完整静态产物，可交给 Nginx、Caddy 等 HTTP 服务。也可用 Python 本地检查：

```sh
python3 -m http.server 12450 --directory dist
```

打开 `http://localhost:12450`。不要用 `file://` 双击 HTML，模块和 Monaco workers 需要 HTTP(S)。资源采用相对路径，可部署到 `/worklog/` 等子目录。静态部署不是 PWA，不支持断网首次加载。

## 选择数据模式

### 浏览器本地模式

未设置 `apiBaseUrl` 或设为空字符串时，业务数据保存在当前站点的 `localStorage`，不请求业务 API：

| 存储键 | 内容 |
| --- | --- |
| `worklog.v2.dashboard` | 项目、分类、任务、待办、工作台文字及版本 |
| `worklog.v2.entry.YYYY-MM-DD` | 指定日期的日志、更新时间及版本 |
| `worklog-theme` | 手动选择的主题 |

数据仅属于当前浏览器和站点来源（协议、域名、端口），不与其他设备或 MCP 共享。清理站点数据、退出隐私模式或存储故障可能丢失记录。项目 ZIP 导出仅包含项目及关联任务、分类，不包含待办或日志，不能代替完整备份。

存储损坏或读取失败时显示错误，不以空数据覆盖原记录；写入失败恢复界面并保留表单草稿。旧键 `worklog-dashboard`、`worklog.entry.*` 不再读取，也不会自动删除、转换或上传后端。需要旧数据时先单独备份，再按当前结构转换。

### 后端模式

Go 服务提供网页时自动生成公开的 `config.json`，使用同源 `/api` 和服务端业务时区。安装方式见[部署文档](../docs/DEPLOYMENT.md)。Go 不读取磁盘上的 `dist/config.json`。

独立静态部署需要编辑 `dist/config.json`，无需重新构建：

```json
{
  "apiBaseUrl": "/api",
  "businessTimezone": "Asia/Shanghai"
}
```

API 基础 URL 必须包含 `/api` 等前缀，可使用以 `/` 开头的站内路径或完整 HTTP(S) URL；不能是相对路径或 `//host`。推荐同源代理。浏览器使用 `credentials: same-origin`，不提供页面填写 Token 或跨域 Cookie 登录；CORS 本身不提供认证。不要把任何密码、Token 或数据库连接串放进公开配置。

开发模式只有 `/api` 代理到 `127.0.0.1:12450`，`config.json` 从 `public/` 提供。联调需要暂时修改 `public/config.json` 并启动后端，详见[前端热更新](../docs/DEVELOPMENT.md#前端热更新)。preview 不继承开发代理。

后端模式不读写本地业务缓存，不自动迁移或同步两种模式的数据；后端故障也不会切回本地模式。移除 API 配置后才能重新访问原本地数据。

### 配置读取规则

- 页面启动时相对于页面地址读取 `./config.json`，使用同源凭证、禁用缓存，超时为 5 秒。
- 仅允许 `apiBaseUrl` 和 `businessTimezone` 两个可选字段，不读取 `.env.local`、构建时业务变量或可执行 `config.js`。`window.WORKLOG_CONFIG` 仅是内部载体。
- 文件缺失、HTTP 错误、JSON 损坏、未知字段、非法 URL／时区都会阻止启动，不发送业务请求、不写本地数据。只有成功读取合法 `{}` 才表示默认本地模式。
- 修改配置后刷新页面。自定义 CSP 应允许 `connect-src 'self'`；跨域 API 还需允许对应来源。
- 独立前端的 `businessTimezone` 应与后端一致。未配置时日期标签使用浏览器时区，日志首次日期仍以 `/today` 为准，任务表单默认日期使用浏览器本地日期。

## 保存与使用限制

- 项目和分类支持排序。普通管理修改在点击保存后生效，取消丢弃草稿。**删除项目例外**：输入完整名称确认后立即删除项目及全部关联任务，取消管理窗口不会撤销；不会顺带提交其他项目草稿。删除分类将任务移到保存列表的首分类。
- 任务支持组合筛选、四级优先级和五种卡片色。日期可空，截止不能早于开始。甘特图仅显示有日期的任务，按自然日绘制，单日期视为单日；提供周／月／15 天／30 天视图，不自动排期。拖动状态时可能补填日期。
- 顶部工作台文字双击编辑，最长 255 个 UTF-16 码元（部分字符占两个码元），失焦／回车保存，Esc 取消。
- 日志支持二／三级标题、文字格式、列表、引用、代码、分隔线和撤销／重做。HTML 源码经编辑器过滤后转成 Tiptap JSON，不直接存 HTML；不支持的颜色会被移除并提示。切换日期或放弃草稿后重置撤销历史。
- 日志每 10 秒检查自动保存，失焦也保存，只提交有变化的正文。源码未应用时暂停自动保存。保存期间的新输入留待下次保存，切换日期保留内存草稿，但关闭页面后不能保证恢复。
- 任意日志写入失败都会暂停该日期的自动重试。普通错误可手动重试；版本冲突时先复制草稿，再确认放弃并重新加载。工作台冲突时也应先保留草稿，再刷新读取最新数据，不反复覆盖提交。
- 工作台写入期间阻止重复交互。任务／项目／分类失败保留弹窗，待办／拖动失败恢复原状态。请求超时 15 秒，不自动重试写操作；超时可能发生在服务端提交之后。
- 跨午夜刷新今天标签与甘特图，但不会把正在编辑的日志切换到新一天。没有多设备离线同步或自动冲突合并。

后端字段、容量限制和版本规则见 [REST API](../docs/REST_API.md)；前端使用的协议见[工作台快照](docs/API工作台.md)和[工作日志](docs/API工作日志.md)。

### 健康状态与工作日历

品牌 `worklog.` 末尾的点显示后端健康状态：未配置后端或尚未完成检查时为原色，检查成功为墨绿色，连接／认证／数据库或响应异常为深红色。启动时检查，此后每次检查完成 30 秒再检查，离开页面停止轮询。请求为 API 基础地址下的 `GET /healthz`。

绿点不代表某次保存成功，红点也不触发本地降级或写入重试。

两种模式都读取前端的 `work-calendar.json`，不使用日历 API 或旧 `worklog.v2.calendar` 缓存。加载失败显示重试入口，暂按周一至周五工作显示，不阻止任务和日志编辑。默认年份、自定义规则和更新方法见[工作日历自定义](../docs/WORK_CALENDAR.md)。

## 开发者参考

测试命令、一次性后端和 Playwright 使用方式见[开发与测试](../docs/DEVELOPMENT.md#前端与真实后端联调)。集成测试会修改数据，绝不能指向个人或生产实例；不要与 Playwright 共用测试实例或端口。

| 文件 | 职责 |
| --- | --- |
| `src/bootstrap.js`、`src/runtimeConfig.js`、`src/main.js` | 并行加载公开配置和应用资源，全部成功后挂载；失败时停止启动 |
| `startupPreloads.js`、`vite.config.js` | 预加载应用静态 JS／CSS，保留轻量启动入口；升级 Vite 后检查生成 HTML 和请求复用 |
| `src/api/client.js`、`src/api/index.js` | HTTP、错误处理、本地／后端适配及静态日历加载 |
| `src/api/worklog.js`、`src/worklogSaving.js` | 按日期读写、正文与版本绑定、保存去重 |
| `src/dashboard.js`、`src/workCalendar.js` | 数据校验、筛选、日期和关联变更 |
| `src/theme.css`、`src/style.css` | 主题变量、布局和共享样式 |
| `tests/fixtures/` | 测试数据，不进入发布产物 |

启动入口只使用 bootstrap，不要另加静态 main 入口。预加载不包含 Monaco 等动态功能，CSS 使用 `preload as="style"` 而非阻塞加载界面的 stylesheet。应用资源失败与配置失败都应进入启动错误处理。

Monaco 在首次打开源码弹窗时按需加载。`src/monacoFeatures.js` 注册所需功能，`src/monacoWorkers.js` 配合 Vite 插件保留独立 HTML／editor workers，避免隐式全量注册。升级后应检查 worker 协议、HTML 补全／格式化、主题和保存。

应用图标源为 `public/favicon.svg`。在已安装 Pillow、Playwright 和 Google Chrome 的环境中执行 `python3 ico.py`，生成 `public/logo.png` 和多尺寸 `public/favicon.ico`，再重新构建。

业务 ID 优先使用 `crypto.randomUUID`，否则使用 `crypto.getRandomValues`；缺少安全随机源时显示错误，不生成弱随机 ID。视觉检查建议覆盖浅深色、1440／1280／768／390px、拖动、保存失败、日志快速切日和源码编辑；自动测试不代替这些交互检查。
