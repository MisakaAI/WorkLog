# Worklog

基于 Vue 3、Vite、Element Plus、Tiptap 和 Monaco 的个人工作台，  
包含工作日志、任务看板／列表／甘特图、项目与分类管理、个人待办和浅深色主题。

**无需后端即可使用。** 首次打开没有项目、任务、待办或日志，不再加载示例 JSON。  
六个初始分类和周一至周五工作规则属于默认设置，不是示例业务记录。

## 开发、测试与构建

推荐 Node.js 22.22.2+ 或 24.15+（包含组件测试工具的依赖要求）。

```sh
npm ci
npm run dev
npm test                 # Node 单元 + Vue/jsdom 组件测试
npm run build
npm run preview
```

组件测试使用 `npm run test:components`（可追加 `-- tests/components/文件名.spec.js`）。入口在加载 Vitest / Vue 前固定使用测试模式，不受父进程 `NODE_ENV=production` 影响，保证 `wrapper.emitted()` 事件断言正常；不改变应用构建配置。

`npm run build` 生成完整的 `dist/`，部署时只需这个目录，无需 Node.js、源码、数据库或后端服务。  
正式部署可使用 Nginx、Caddy 或任何静态 HTTP 服务；本地也可使用：

```sh
python3 -m http.server 12450 --directory dist
```

浏览器打开 `http://localhost:12450`。  
创建持久业务 ID 优先使用 `crypto.randomUUID`，LAN HTTP 下回退到 `crypto.getRandomValues`；  
若浏览器连安全随机源也不支持，会显示错误而不会生成弱随机业务 ID。  
构建使用相对资源路径，可部署在站点根目录或 `/worklog/` 等子目录。  
**不要直接以 `file://` 双击 HTML**：ES modules 和 Monaco workers 需要 HTTP(S) 环境。  
静态部署不等于 PWA，暂不支持断网首次加载。

## 应用图标

图标源文件为 `public/favicon.svg`，使用灰白新拟态圆角底板、微拟物手账与紫色燕尾书签。  
页面优先使用 SVG，`public/favicon.ico` 为兼容格式，两者均随构建复制到 `dist/`。

修改 SVG 后，在已安装 Pillow、Playwright 和 Google Chrome 的环境中运行：

```sh
python3 ico.py
```

脚本通过 Chrome 渲染阴影，输出 `public/logo.png`（256px 预览）与包含 16、32、48、64、128、256px 六种尺寸的 `public/favicon.ico`。  
重新构建即可发布更新。

## 两种数据模式

### 1. 本地模式（默认）

不配置后端时，所有业务读写使用当前浏览器同源的 `localStorage`，不会发起业务 API 请求：

| 存储键 | 内容 |
| --- | --- |
| `worklog.v2.dashboard` | 项目及顺序、分类及顺序、任务、待办、顶部工作台文字、工作台版本 |
| `worklog.v2.entry.YYYY-MM-DD` | 指定日期的 Tiptap JSON 日志、更新时间及版本 |
| `worklog-theme` | 手动选择的主题 |

数据只属于该浏览器、该站点来源（协议／域名／端口），不提供跨设备同步。清理站点数据、隐私模式退出或浏览器存储故障可能导致数据不可用；  
重要记录请自行备份。存储读取失败或数据损坏会报错，不会用空数据覆盖原记录；写入失败会回滚界面，保留表单草稿，不显示保存成功。

本次使用新的 `worklog.v2.*` 命名空间，**不再读取旧版 `worklog-dashboard` 和 `worklog.entry.*`**，避免旧示例缓存再次出现。  
旧键不会被自动删除（其中可能包含用户记录），也不自动导入新存储或上传后端。如需保留旧数据，先手动备份并按新结构校验转换。

工作日历在两种模式下都读取前端 `public/work-calendar.json`（构建后为 `dist/work-calendar.json`），默认包含 2026 年节假日和调休，不再读取 `worklog.v2.calendar`。
没有页面配置入口；自定义单双休、节假日或取消节日标记见[工作日历自定义](../docs/WORK_CALENDAR.md)。

### 2. 后端模式

仓库根目录已提供可运行的 Go / SQLite / PostgreSQL 后端和 MCP Server，配置、认证、备份及部署见 [后端 README](../README.md)。  
构建本前端后在根目录启动 Go 程序，即可通过 `http://127.0.0.1:12450` 使用同源 `/api`；Go 提供的 `config.json` 会自动选择真实后端。
在 `Web/` 中执行 `npm run dev` 即可无后端运行：Vite 从 `public/config.json` 提供 `{}` 配置，数据保存在浏览器中。
开发服务器仅保留 `/api` 代理供后端联调使用；如需后端模式，应将 `public/config.json` 的 `apiBaseUrl` 配置为 `/api` 并启动 Go 后端。
`npm run build` 与 `npm run preview` 同样使用发布的 `config.json`，默认本地模式。
需要验证纯本地模式时使用构建后的 preview。

**独立静态部署配置后端：** 编辑 `dist/config.json` 中的 **API 基础 URL（包含 `/api` 等前缀）**，无需重新构建。
注意 Go 内置静态服务会动态生成 `/config.json`，只输出 `apiBaseUrl` 和 `businessTimezone`，不会读取这个文件；
Go 部署固定同源 `/api` 并自动传入业务时区，子目录代理约定见 [部署与配置](../docs/DEPLOYMENT.md#子目录部署)。

```json
{
  "apiBaseUrl": "/api",
  "businessTimezone": "Asia/Shanghai"
}
```

跨域 URL 仅在部署层已解决认证时可用；CORS 本身不提供登录能力。

用户配置只使用公开的 `config.json`，不读取构建时业务环境变量，也不使用 `.env.local` 或可执行 `config.js` 配置 API。
`window.WORKLOG_CONFIG` 仅是应用内部载体，不再是部署人员编辑的文件。
发布的 `config.json` 默认为 `{}`；未设置 `apiBaseUrl` 或显式设置为 `""` 时使用本地模式。

启动入口相对于页面 URL 请求 `./config.json`（支持 `/worklog/` 子目录），
使用同源凭证、禁用缓存，并设置 5 秒超时；同时并行加载应用 JS/CSS，减少串行等待。
应用模块只导出挂载函数，只有配置读取、校验及资源加载都完成后才挂载，避免首批业务请求使用错误地址。
配置返回前显示“正在加载配置…”，返回后若资源仍未就绪则显示“正在准备应用…”。
`config.json` 是必需发布文件，不存在读取失败回退：

- 文件缺失（404/410）、网络失败、超时、其他 HTTP 错误（包括 401/403/5xx）、JSON 损坏、非对象、未知字段、非法 URL 或时区：显示启动错误，不挂载应用，不发送业务 API 请求，不写本地业务数据。
- 只有成功读取合法配置后才启动；`{}`、省略 API 地址或显式空 API 地址表示本地模式。

JSON 仅允许 `apiBaseUrl` 和 `businessTimezone` 两个可选字段；不允许注释、尾逗号或密钥。
修改配置后刷新页面；URL 不能是相对路径、`//host` 或非 HTTP(S) 地址。
站点自定义 CSP 时需允许 `connect-src 'self'` 读取 JSON 和同源 API；配置跨域 API 时还需允许该 API 来源。
配置不再需要额外的可执行配置脚本或相应 `script-src` 放行规则（应用模块和首屏主题脚本仍遵循原 CSP 要求）。

配置后所有业务数据由 API 提供，**不读取本地业务缓存，不自动迁移或同步两种模式的数据，也不会在后端故障时偷偷切回本地模式**。  
移除配置后可重新使用原有本地数据。

品牌 `worklog.` 末尾的圆点仅展示后端健康状态：未配置后端或首次检查尚未完成时保留原色；
配置后通过 API 基础 URL 下的 `GET /healthz` 检查，成功为墨绿色，连接、认证、数据库或响应格式异常为深红色。
页面加载时立即检查，每次检查完成后 30 秒再检查；恢复后自动变绿，离开页面停止轮询。
健康检查与业务请求使用相同的地址配置、同源凭证和 15 秒超时，不读取或写入本地业务缓存。
**绿点不代表某次业务保存成功，红点也不会触发本地降级或写操作重试。**

当前已接入的接口（基础 URL 后追加）：

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| GET | `/healthz` | 检查后端认证与数据库可用性，成功返回 `{ ok: true }` |
| GET / PUT | `/dashboard` | 读取／原子保存带 `revision` 的完整工作台 |
| GET | `/today` | 读取今天日志 |
| GET / PUT | `/worklog/:date` | 读取／按版本保存指定日期日志 |
| GET | `/worklog/dates` | 返回全部有内容日志的日期 `{ dates: [] }` |

请求直接收发 JSON，不使用 `{ data: ... }` 包装。日志保存使用标准 `PUT`，不再使用 `PUSH`。  
后端工作台完整快照容量预算为 8 MiB（按服务端 JSON 编码计），所有写入口统一检查；超限会返回错误并保留原数据。

接口字段和冲突处理见 [HTTP API 说明](docs/API说明.md)及其[工作台](docs/API工作台.md)、[工作日志](docs/API工作日志.md)文档；后端额外提供共享同一数据的细粒度 REST / MCP 接口，但 Vue 当前仍使用完整快照保存。

客户端请求超时为 15 秒，不自动重试写操作。  
使用同源凭证；跨域后端需要正确配置 CORS，当前不支持跨域 Cookie 登录或页面填写 Token，因此浏览器工作台正式推荐同源代理及部署层认证；  
配置跨域 URL 并不自动建立认证渠道。独立静态部署可配置 `businessTimezone` 与后端保持一致，  
未配置时日期标签使用浏览器时区，日志初次日期仍采用 `/today`。后端故障时主工作台显示错误与重新加载入口；  
日历统一加载并向日期面板和甘特图共享降级、错误与重试状态，日志日期标记故障不阻止日志编辑。

## 主要行为

- 项目／分类管理支持拖动排序，普通修改在弹窗点击保存后生效，取消会丢弃这些草稿修改。删除项目前需输入完整项目名称进行二次确认（前端校验），确认后立即删除项目及其所有任务，无需点击保存，取消项目管理也不会撤销删除；其他未保存的项目修改仍保留为草稿，不随删除提交。删除分类则将任务移至保存列表第一个分类，不删除任务。
- 分类名称 trim 后非空、最长 40、不区分大小写唯一，至少保留一个分类。默认开发／临时／个人／产品／设计／调研，新任务默认开发（不存在时用首分类）。
- 任务支持项目、搜索、分类、高优先级组合筛选、四级优先级和五种卡片色。拖动修改项目和状态，必要时补填开始／截止日期。
- 起止日期可空，均填时截止不得早于开始。甘特图只显示有日期的任务，单日期视为单日，区间含首尾，按自然日绘制，不自动排期。提供周／月／15 天／30 天视图和前后翻页。
- 顶部文字双击编辑，最长 255 码元，失焦／回车保存、Esc 取消。
- 日志支持二／三级标题、粗斜体、下划线、删除线、颜色、列表、引用、代码、分隔线和撤销／重做。源码编辑通过 Tiptap 过滤 HTML 并转换为 JSON，不直接保存 HTML。源码导入与粘贴会移除不受支持的颜色并提示，保留正文和其他格式。加载日期或确认放弃草稿时重置撤销历史，不能撤销到其他日期。
- 日志每 **10 秒**检查自动保存，失焦也保存，只提交有改动的文档。任何写入失败都会暂停该日期的自动重试；普通错误可手动重试，版本冲突必须先复制草稿，再确认放弃草稿并重新加载最新版本。保存中继续输入的新内容保留到下次保存；切换日期保留内存草稿，错误不串到其他日期，离开有未保存内容的页面会提示。源码未应用前暂停自动保存。
- 工作台写入期间阻止重复交互并显示保存状态；任务、项目和分类保存失败时弹窗保持打开，待办／拖动失败恢复原数据。
- 后端并发更新使用工作台 `revision` 和日志 `version` 检查。冲突不会自动覆盖服务端；请先复制未保存草稿，日志可使用明确确认的“重新加载”入口，工作台可刷新页面读取最新数据，必要时重新应用修改。多设备协作和离线同步不在当前范围内。页面跨午夜会刷新今天标签和甘特图，并提示仍在编辑的日期，不会自动把正在编辑的日志切到新一天。

### 后端集成与浏览器测试

以下命令均在 `Web/` 下运行，需要 Go。`npm test` 不含真实后端与浏览器测试。

浏览器 smoke 使用真实 Go 服务和一次性 SQLite 数据库：

```sh
npm run build
npx playwright install chromium
npm run test:e2e
# 可选：npx playwright install webkit
# WORKLOG_TEST_WEBKIT=1 npm run test:e2e
```

Playwright 自动调用 `tests/start-backend.js`：在系统临时目录创建数据库、权限为 `0600` 的 JSON 配置和 Go 可执行文件，
使用 `--conf` 启动，退出时删除临时目录。不设置或读取 WORKLOG 业务环境变量；父进程即使带有业务环境变量，也由 `--conf` 忽略。
测试工具变量可保留：`WORKLOG_E2E_PORT` 修改默认端口 `22450`，`WORKLOG_CHROMIUM_EXECUTABLE` 指定已安装的 Chrome，
`WORKLOG_TEST_WEBKIT=1` 增加 WebKit 项目，`CI` 控制失败重试。

适配器集成测试需要单独的全新数据库，**不要指向个人或生产后端**。在两个终端运行（端口须一致）：

```sh
# 终端 1：会等待运行，结束后按 Ctrl+C 清理临时资源
node tests/start-backend.js
# 终端 2：仅测试工具读取此 URL，不是业务配置环境变量
WORKLOG_TEST_API_URL=http://127.0.0.1:22450/api npm run test:backend
```

集成测试会修改数据；再次运行前应重启临时后端。不要与 Playwright 共用同一实例或端口。

## 代码导航与 review

- `src/bootstrap.js`、`src/runtimeConfig.js`、`src/main.js`：并行读取公开 JSON 与加载应用资源；配置校验及资源加载完成后才显式挂载，任一路失败均停止启动。
- `src/api/client.js`：运行时地址校验、统一 HTTP、错误及超时处理。
- `src/api/index.js`：工作台的本地／后端适配及静态日历 JSON 加载。
- `src/api/worklog.js`：日志读写、显式版本参数及日期标记，不依赖静态文件。
- `src/dashboard.js`、`src/workCalendar.js`：结构校验、筛选、日期及关联变更规则。
- `src/worklogSaving.js`：日期正文与基准版本绑定、保存去重及保存期间继续编辑。
- `src/theme.css`：浅深色语义变量；`src/style.css`：布局和共享控件。
- `tests/fixtures/`：仅用于回归测试的固定数据，不属于发布资源，不会进入 `dist/`。

### Monaco 按需注册

HTML 源码弹窗首次打开时才加载 Monaco。`src/monacoFeatures.js` 注册最小功能；  
`vite.config.js` 仅替换 Monaco HTML worker 管理器的内部入口，使用 `src/monacoWorkers.js` 避免隐式全量注册，保留独立 HTML／editor workers。  
升级 Monaco 时必须运行完整测试，检查 worker 协议、源码弹窗打开、HTML 补全／格式化、主题切换和保存。

视觉与浏览器回归建议覆盖浅深色 1440、1280、768、390px，任务拖动、表单保存失败、日志快速切日及源码编辑。  
自动测试不替代这些真实浏览器交互检查。
