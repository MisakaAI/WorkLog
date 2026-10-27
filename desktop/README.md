# Windows x86-64 桌面包

这是一层独立打包设施：**不修改现有 Go/Vue 源码、API、数据库逻辑、Web/Docker 构建或默认端口**。打包使用原始 `./cmd/worklog`，不是另写一个后端。

## Linux 构建

在 **Linux x86-64** 上，准备：

- Go **1.27.3**（与根目录 `go.mod` 一致）。脚本优先使用 PATH 中的 Go，也识别当前开发镜像的 `/opt/go/bin/go`；其他位置可设置 `GO_BIN=/绝对路径/go`。
- Node.js **24.22.1**、npm **11.19.1**。桌面脚本严格检查这些版本，不影响项目其他构建命令。
- 可以访问 npm、Go 模块源、Electron GitHub Releases 和 electron-builder 二进制下载源的网络；首次下载需要足够磁盘空间（建议至少 3 GB）。支持这些工具自身的缓存/镜像配置，不跳过下载错误。
- 默认 **ZIP 不需要 Wine、MinGW、CGO 或系统 SQLite**。可选 NSIS 安装包需要可运行的 `wine`（建议 Wine 10+ 及其 32 位支持）；脚本在构建前检查，并明确报错。

从项目根目录执行：

```sh
./desktop/build-win-x64.sh
# 或明确指定 Go：
GO_BIN=/opt/go/bin/go ./desktop/build-win-x64.sh

# 可选：在已安装并初始化 Wine 的主机上生成交互式安装包
./desktop/build-win-x64.sh installer
```

脚本会先用固定的 `@electron/get 3.1.0` 预下载并校验 Electron（最长 30 分钟），避免 electron-builder 自身 10 分钟请求上限导致慢速下载反复丢失。当前环境直连 GitHub 较慢，可使用以下**同一构建脚本**的镜像命令（不关闭校验，镜像地址会记录到构建信息）：

```sh
ELECTRON_MIRROR=https://npmmirror.com/mirrors/electron/ ./desktop/build-win-x64.sh
```

若已有下载好的官方 `electron-v45.0.0-win32-x64.zip`，可运行 `WORKLOG_ELECTRON_ZIP=/绝对路径/electron-v45.0.0-win32-x64.zip ./desktop/build-win-x64.sh`；脚本先验证固定 SHA-256，再填充标准下载缓存，不需要重新下载。此选项只指 Electron 官方运行时 ZIP，不是项目最终发布 ZIP。

`package.json` 还固定了 Windows Electron ZIP 的 SHA-256（来自锁定的 Electron npm 包中的 `checksums.json`），镜像不能通过替换自己的校验文件绕过它；升级 Electron 时需同步更新版本和该摘要。ZIP 打包与校验共用锁文件固定的 `7zip-bin`，无需另行下载 7zip 工具集。可选安装包还需要下载 NSIS 等工具；受限网络可按 electron-builder 的 `ELECTRON_BUILDER_BINARIES_MIRROR` 配置可信镜像。

默认产物：

```text
desktop/dist/Worklog-1.0.0-win-x64.zip             # 可分发，完整解压运行
desktop/dist/win-unpacked/Worklog.exe              # 完整目录也可直接分发
desktop/dist/win-unpacked/resources/backend/
  worklog.exe                                    # 原始 Go 后端，Windows AMD64
  worklog-supervisor.exe                         # Windows 生命周期/端口监管
desktop/dist/win-unpacked/resources/web/           # Vue 构建产物
desktop/dist/win-unpacked/resources/app.asar       # Electron 主进程
desktop/dist/win-unpacked/resources/BUILD-INFO.json
```

安装包为 `desktop/dist/Worklog-1.0.0-win-x64-setup.exe`。NSIS 引导程序自身为 32 位，安装的应用是 x64。安装仅针对当前用户，不需要系统级服务，卸载默认保留用户数据。

中间产物位于 `desktop/build/`，不覆盖 `Web/dist` 或根目录 `bin/`。构建脚本执行两个 `npm ci`，分别使用前端原有锁文件和独立的 `desktop/package-lock.json`；Electron **45.0.0**、electron-builder **26.18.0**、校验用 7zip-bin **5.2.0** 均固定版本。Go 使用现有 `go.mod` / `go.sum` 和 `-mod=readonly`，`CGO_ENABLED=0 GOOS=windows GOARCH=amd64`。SQLite 是已有的 `modernc.org/sqlite v1.60.1`，支持 Windows amd64，无需原生数据库 DLL。

脚本在最后检查两个 Go EXE 和 Electron EXE 的 PE/AMD64 标识、前端 JS、Electron 运行文件及 ZIP 完整性和成员。`BUILD-INFO.json` 记录工具版本、源码提交和锁文件 SHA-256。固定依赖便于重复构建，但不承诺 ZIP/NSIS 时间戳等元数据逐字节一致。生成文件与依赖目录均已忽略，不要提交。可用 `sha256sum desktop/dist/*.zip` 单独记录发布校验值。

## Windows 安装/首次启动

需要受 Electron 支持的 **Windows 10/11 x86-64**；无需安装 Go、Node.js、npm、SQLite 或项目依赖。

- ZIP：将**整个 ZIP 解压**到普通本地目录，双击 `Worklog.exe`；不能只复制一个 EXE 或直接在压缩包中运行。
- 安装包：运行 `*-setup.exe`，按向导安装，从快捷方式启动。
- 首次启动创建数据库、自动迁移并生成持久 MCP 令牌；没有示例业务数据。数据库健康检查通过后才显示窗口。若启动失败/超时、服务退出或持续失去健康状态，会出现中文错误对话框及日志位置。
- 双开会聚焦已有窗口，不会再开一个后端。ZIP 与安装版共享同一个桌面用户数据目录。
- **点击窗口右上角 ×（或按 Alt+F4）只会最小化到任务栏，不退出。**窗口不隐藏，Go 后端继续运行；点击任务栏中的 Worklog 图标即可恢复原窗口和当前编辑内容。
- **真正退出：右键任务栏中的 Worklog 图标 →“退出 Worklog”**（Windows 跳转列表的任务项）。系统自带的“关闭窗口”与 × 一样只会最小化，请选择新增的“退出 Worklog”。这不是右下角通知区域的系统托盘，不额外增加托盘图标。
- 退出命令通过单实例机制通知现有主进程，再执行原有后端清理；不会额外启动 Go。应用已退出时再次点击残留的“退出 Worklog”任务也不会重新启动应用。若系统策略禁止创建任务列表，应用会明确报错退出，而不是留下没有退出入口的驻留窗口。
- 未签名：SmartScreen 可能显示“Windows 已保护你的电脑”，企业策略/杀毒软件也可能阻止运行。只在确认发布来源与校验值后选择“更多信息 → 仍要运行”（若策略允许）；不要关闭系统防护。构建不启用签名、自动更新或托盘。

## 数据与日志

稳定保存于当前 Windows 用户的：

```text
%APPDATA%\WorklogDesktop\
  worklog.db                    # SQLite；还可能有 -wal / -shm
  mcp-token                     # 持久随机令牌，不提供给渲染进程
  backend.json                  # 桌面层生成的完整启动配置，内含令牌
  desktop.log                   # 本次日志，最多 4 MiB
  desktop.log.previous          # 上次日志
  ...                           # Electron 浏览器缓存/偏好
```

不向安装目录或系统临时目录写数据库，解压位置/版本改变不会改变数据位置。使用 Windows 用户配置目录继承的 ACL 保护数据和令牌；不要把该目录共享给其他用户。`backend.json` 每次启动重建，不是用户配置入口；继承的 `WORKLOG_*` 变量不会改变桌面数据库或监听范围。业务时区沿用默认 `Asia/Shanghai`。桌面数据库与原来项目目录的 `data/` 独立，不会自动迁移已有数据。

通过任务栏“退出 Worklog”真正退出后会关闭监管管道，监管进程关闭 Windows Job Object，从内核终止它和 Go 后端；Electron 崩溃使管道断开，监管进程自身被结束也会触发相同清理。监管进程先加入 Job，再启动后端，避免“启动后才关联 Job”的竞态；不支持嵌套 Job 的主机策略会在启动后端之前报错。

**退出边界：**原后端没有可在无控制台 Windows 子进程上调用的优雅关闭协议；为保持原代码不变，这里使用 Job 的强制终止。SQLite 已有 WAL/事务逻辑负责下次打开恢复，不保证尚未提交的请求或前端未保存编辑能保留。请等待界面显示保存完成再退出。备份/恢复按原项目[运维文档](../docs/OPERATIONS.md)操作，备份前停止桌面应用，使用包内 `resources\backend\worklog.exe --conf "%APPDATA%\WorklogDesktop\backend.json" backup <新备份路径>`；不要单独拷贝运行中或尚有 WAL 的 `.db`。

## 端口、界面与安全边界

1. Electron 在用户目录准备配置，以 `--conf` 启动**原始后端**，设置 `addr=127.0.0.1:0`、SQLite 路径、包内静态目录及稳定令牌。显式 `write_token` 确保桌面层与后端使用同一个持久凭证。后端读取令牌文件时始终要求普通文件；Unix 另检查私有权限，Windows 使用文件系统 ACL，不把 Go 返回的模拟权限位当作 Unix `0600`。
2. 后端自己调用原有 `ListenAndServe`，由操作系统实际绑定可用动态端口。监管进程通过 `GetExtendedTcpTable` 获取**该后端 PID 的 IPv4 回环监听端口**，仅向 Electron 主进程报告。没有预占/释放空闲端口，也不解析含糊的启动日志，不绑定公网地址。
3. Electron 检查 `/healthz` 的 `200 {"ok":true}`，再加载同源 `http://127.0.0.1:<实际端口>/`。Go 原有 `/config.js` 动态提供 `/api`，不需要改 Vue API 地址或建立反向代理。
4. 关闭 Node 集成、启用 context isolation / renderer sandbox，不使用 preload、关闭开发者工具；禁止弹窗、webview、权限申请、外部导航及外部网络资源，允许同源 API/资源、同源 blob worker/导出和 data 图片/字体。CSP 的 inline script/style 仅用于兼容现有页面和编辑器，不启用 eval。

桌面 REST 沿用原应用的本机免密模式；**其他同机进程仍可访问本机端口，不构成同机安全隔离**。动态端口每次可能变化，不能把它当作固定 Web/MCP 部署地址。浏览器主题等 origin-scoped 偏好可能随端口变化而重置；业务数据始终在稳定 SQLite 文件中。

## 启动时报 SQLite 错误或 `chmod 600`

旧版存在两个独立的 Windows 兼容性问题：

- 日志为 `initialize_sqlite_database` → `ping_database`、`sqlite_code: 1`：Windows 绝对路径未经转换就构造 `file:` URI，盘符被当成 URI authority，SQLite 拒绝连接。后端现在先转换反斜杠并为盘符添加前导 `/`，生成 `file:///C:/...`；打开数据库和备份校验共用此转换，空格、中文、`#`、`%` 也会正确转义。
- 单独运行后端提示 `token file must be regular and private (chmod 600)`：旧版错误地在 Windows 上检查 Unix 权限位，已有令牌在再次读取时会被拒绝。Windows 不使用 `chmod 600`，应通过用户目录的 ACL 保护令牌。此错误与桌面端的 SQLite 连接失败不是同一个故障；桌面端显式提供 `write_token`，不会触发此文件权限检查。

重新构建并完整解压新版桌面包后启动，**无需删除数据库、令牌或 `%APPDATA%\\WorklogDesktop`**。直接运行 `resources\\backend\\worklog.exe` 不会自动使用桌面配置；若要操作桌面数据库，请使用 `--conf "%APPDATA%\\WorklogDesktop\\backend.json"`，否则它会按当前工作目录和 `WORKLOG_*` 环境变量加载独立配置。

## 启动时报 `Cannot read properties of undefined (reading 'appId')`

旧版主进程读取 `package.json.build.appId`，但 electron-builder 会从包内元数据中移除 `build`。现已改为独立的运行时 ID，并与构建配置校验一致。

若修复后仍看到 `main.cjs:9` 的同一错误，请检查是否使用了旧产物：仅编译 Go 或 Vue 不会更新 `resources/app.asar`，完整桌面构建中途失败也不代表 ZIP 已更新。构建脚本现在在前置检查通过后清理旧 `desktop/dist`，并在最终校验中检查 ASAR 源码一致性、包内 Windows 启动入口及 ZIP 中的 ASAR。

1. 执行 `./desktop/build-win-x64.sh`，确认成功到达 `[6/6]` 并输出“完成”。
2. 使用 `sha256sum desktop/dist/Worklog-1.0.0-win-x64.zip` 获取新包摘要；在 Windows 用 `Get-FileHash .\Worklog-1.0.0-win-x64.zip -Algorithm SHA256` 核对传输后的文件。
3. 完整解压到新目录，直接启动该目录中的 `Worklog.exe`，不要使用旧快捷方式或仅替换 EXE。应用代码在 `resources/app.asar` 中。

无需删除 `%APPDATA%\WorklogDesktop` 的用户数据；该问题发生在数据库初始化之前。

## 检查与 Windows 验收

Linux 上：

```sh
ELECTRON_SKIP_BINARY_DOWNLOAD=1 npm --prefix desktop ci
npm --prefix desktop test
/opt/go/bin/go test ./desktop/supervisor  # 或 PATH 中的 go
bash -n desktop/build-win-x64.sh
./desktop/build-win-x64.sh               # 包含上述测试、编译、打包及资源校验
```

Windows PowerShell 5.1+ 可运行包内后端/监管进程冒烟测试（从源码目录调用；也可单独复制此脚本）：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\desktop\tests\windows-smoke.ps1 -AppDirectory 'C:\解压后的 Worklog 目录'
```

测试在独立的 `%LOCALAPPDATA%\WorklogDesktop-smoke-<随机值>` 目录中创建再清理测试数据，不碰真实桌面数据；检查端口归属与仅回环监听、健康检查、Vue 入口和同源配置、创建/读取待办、重启持久化、管道关闭/监管进程强杀后的后端清理。

CI 另在原生 `windows-latest` 上运行配置/令牌重载、SQLite 特殊字符路径、备份校验/恢复、SQLite 集成及监管进程单元测试。这些测试不能代替桌面 GUI 和 Job Object 生命周期的手动验收。

还必须在 Windows 实体机或虚拟机手动检查：

- 双击首次启动，完整 Vue 主要界面与编辑器/worker 正常加载（不只是 HTML 入口）。
- 创建项目/待办/工作日志，等待保存，从任务栏右键“退出 Worklog”后重开并确认读回；验证文件导出。
- 安装到只读于普通用户的目录或包含空格/中文的目录后启动，确认写入用户目录。
- 双开只聚焦已有窗口；点击 ×、Alt+F4 或任务栏系统“关闭窗口”后仅最小化，任务栏图标仍在，Go/监管进程及端口保持不变；点击任务栏图标恢复同一窗口和当前编辑内容。
- 右键任务栏图标能看到“退出 Worklog”；点击后 Electron、Go 和监管进程全部退出；再次点击残留的退出任务不启动新服务。强制结束 Electron 主进程后也无 Go 残留。
- 占用 12450 时仍可启动；模拟后台退出、数据目录不可写时出现明确错误。
- 若分发安装包，检查安装/卸载与用户数据保留、SmartScreen 表现。

**当前实现的验证状态：**已在本 Linux 环境成功构建 ZIP 并检查 PE 架构、资源和压缩包完整性；桌面层测试、原项目 Go 测试与前端测试通过。另在 Linux Chromium 中验证了原后端 `:0` 动态端口、打包 Vue、桌面 CSP 下的 Monaco 加载和同源数据保存/刷新读取。本环境没有 Windows 或 Wine，尚未执行上述 PowerShell 冒烟、Windows GUI（包括关闭最小化、任务栏恢复/右键退出）/SQLite/Job Object 生命周期及安装/卸载验收，也未生成可选 NSIS 安装包。

**Linux 的交叉编译和包结构检查不能代替上述 Windows 运行验收**；发布前请在 Windows 10/11 x64 实体机或虚拟机完成清单，不能把 Linux 检查视为 Windows 端到端测试。
