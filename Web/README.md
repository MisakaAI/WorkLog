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

`npm run build` 生成完整的 `dist/`，部署时只需这个目录，无需 Node.js、源码、数据库或后端服务。  
正式部署可使用 Nginx、Caddy 或任何静态 HTTP 服务；本地也可使用：

```sh
python3 -m http.server 8080 --directory dist
```

浏览器打开 `http://localhost:8080`。  
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
| `worklog.v2.calendar` | 可选的工作日历配置；未设置则周一至周五工作，无节假日和调休预置 |
| `worklog-theme` | 手动选择的主题 |

数据只属于该浏览器、该站点来源（协议／域名／端口），不提供跨设备同步。清理站点数据、隐私模式退出或浏览器存储故障可能导致数据不可用；  
重要记录请自行备份。存储读取失败或数据损坏会报错，不会用空数据覆盖原记录；写入失败会回滚界面，保留表单草稿，不显示保存成功。

本次使用新的 `worklog.v2.*` 命名空间，**不再读取旧版 `worklog-dashboard` 和 `worklog.entry.*`**，避免旧示例缓存再次出现。  
旧键不会被自动删除（其中可能包含用户记录），也不自动导入新存储或上传后端。如需保留旧数据，先手动备份并按新结构校验转换。

工作日历沿用 `src/workCalendar.js` 的结构，可通过浏览器控制台写入 `worklog.v2.calendar` 后刷新。  
没有页面配置入口，后端配置格式见[工作日历 API 说明](docs/API工作日历.md)。

### 2. 后端模式

仓库根目录已提供可运行的 Go / SQLite / PostgreSQL 后端和 MCP Server，配置、认证、备份及部署见 [后端 README](../README.md)。  
构建本前端后在根目录启动 Go 程序，即可通过 `http://127.0.0.1:8080` 使用同源 `/api`；Go 提供的 `config.js` 会自动选择真实后端。  
开发时运行后端，再使用 `VITE_API_BASE_URL=/api npm run dev`，Vite 会将 `/api` 代理到本机 8080 端口。

将 **API 基础 URL（包含 `/api` 等前缀）** 配置为以下任一种方式：

**独立静态部署时配置，推荐：** 编辑 `dist/config.js`，无需重新构建。  
注意 Go 内置静态服务会动态生成 `/config.js`，不会读取这个文件；  
Go 部署固定同源 `/api` 并自动传入业务时区，子目录代理约定见 [部署与配置](../docs/DEPLOYMENT.md#子目录部署)。

```js
window.WORKLOG_CONFIG = { apiBaseUrl: '/api', businessTimezone: 'Asia/Shanghai' }
// 跨域 URL 仅在部署层已解决认证时可用；CORS 本身不提供登录能力。
```

**构建时配置：** 在项目根目录 `.env.local` 中设置后重新构建：

```dotenv
VITE_API_BASE_URL=https://backend.example.com/api
```

运行时 `apiBaseUrl` 优先于环境变量；发布的 `config.js` 默认为 `{}`，因此不覆盖构建配置。  
显式设置 `apiBaseUrl: ''` 强制本地模式。修改配置后刷新页面；配置值不能是相对路径、`//host` 或非 HTTP(S) URL。不要在前端配置里放密钥。

配置后所有业务数据由 API 提供，**不读取本地业务缓存，不自动迁移或同步两种模式的数据，也不会在后端故障时偷偷切回本地模式**。  
移除配置后可重新使用原有本地数据。

当前已接入的接口（基础 URL 后追加）：

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| GET / PUT | `/dashboard` | 读取／原子保存带 `revision` 的完整工作台 |
| GET | `/today` | 读取今天日志 |
| GET / PUT | `/worklog/:date` | 读取／按版本保存指定日期日志 |
| GET | `/worklog/dates` | 返回全部有内容日志的日期 `{ dates: [] }` |
| GET | `/work-calendar` | 读取完整工作日历配置 |

请求直接收发 JSON，不使用 `{ data: ... }` 包装。日志保存使用标准 `PUT`，不再使用 `PUSH`。  
后端工作台完整快照容量预算为 8 MiB（按服务端 JSON 编码计），所有写入口统一检查；超限会返回错误并保留原数据。

接口字段和冲突处理见 [HTTP API 说明](docs/API说明.md)及其[工作台](docs/API工作台.md)、[工作日志](docs/API工作日志.md)、[工作日历](docs/API工作日历.md)文档；后端额外提供共享同一数据的细粒度 REST / MCP 接口，但 Vue 当前仍使用完整快照保存。

客户端请求超时为 15 秒，不自动重试写操作。  
使用同源凭证；跨域后端需要正确配置 CORS，当前不支持跨域 Cookie 登录或页面填写 Token，因此浏览器工作台正式推荐同源代理及部署层认证；  
配置跨域 URL 并不自动建立认证渠道。独立静态部署可配置 `businessTimezone` 与后端保持一致，  
未配置时日期标签使用浏览器时区，日志初次日期仍采用 `/today`。后端故障时主工作台显示错误与重新加载入口；  
日历统一加载并向日期面板和甘特图共享降级、错误与重试状态，日志日期标记故障不阻止日志编辑。

## 主要行为

- 项目／分类管理在弹窗点击保存后生效；取消不产生写入，支持拖动排序。删除项目将任务移至“未分配”；删除分类将任务移至保存列表第一个分类，均不删除任务。
- 分类名称 trim 后非空、最长 40、不区分大小写唯一，至少保留一个分类。默认开发／临时／个人／产品／设计／调研，新任务默认开发（不存在时用首分类）。
- 任务支持项目、搜索、分类、高优先级组合筛选、四级优先级和五种卡片色。拖动修改项目和状态，必要时补填开始／截止日期。
- 起止日期可空，均填时截止不得早于开始。甘特图只显示有日期的任务，单日期视为单日，区间含首尾，按自然日绘制，不自动排期。提供周／月／15 天／30 天视图和前后翻页。
- 顶部文字双击编辑，最长 255 码元，失焦／回车保存、Esc 取消。
- 日志支持二／三级标题、粗斜体、下划线、删除线、颜色、列表、引用、代码、分隔线和撤销／重做。源码编辑通过 Tiptap 过滤 HTML 并转换为 JSON，不直接保存 HTML。源码导入与粘贴会移除不受支持的颜色并提示，保留正文和其他格式。加载日期或确认放弃草稿时重置撤销历史，不能撤销到其他日期。
- 日志每 **10 秒**检查自动保存，失焦也保存，只提交有改动的文档。任何写入失败都会暂停该日期的自动重试；普通错误可手动重试，版本冲突必须先复制草稿，再确认放弃草稿并重新加载最新版本。保存中继续输入的新内容保留到下次保存；切换日期保留内存草稿，错误不串到其他日期，离开有未保存内容的页面会提示。源码未应用前暂停自动保存。
- 工作台写入期间阻止重复交互并显示保存状态；任务、项目和分类保存失败时弹窗保持打开，待办／拖动失败恢复原数据。
- 后端并发更新使用工作台 `revision` 和日志 `version` 检查。冲突不会自动覆盖服务端；请先复制未保存草稿，日志可使用明确确认的“重新加载”入口，工作台可刷新页面读取最新数据，必要时重新应用修改。多设备协作和离线同步不在当前范围内。页面跨午夜会刷新今天标签和甘特图，并提示仍在编辑的日期，不会自动把正在编辑的日志切到新一天。

浏览器 smoke 使用真实 Go 服务和一次性数据库：先 `npm run build`，再 `npx playwright install chromium webkit`，执行 `WORKLOG_TEST_WEBKIT=1 npm run test:e2e`。该命令需要 Go；`WORKLOG_CHROMIUM_EXECUTABLE` 可指定已安装的 Chrome。

## 代码导航与 review

- `src/api/client.js`：配置选择、统一 HTTP、错误及超时处理。
- `src/api/index.js`：工作台与日历的本地／后端适配。
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
