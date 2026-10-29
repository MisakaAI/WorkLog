# Windows 托盘桌面端

WorkLog 以单进程运行原生 Win32 托盘、本地 HTTP 和数据库服务，在默认浏览器显示界面；不使用 Electron 或 WebView，关闭浏览器不会退出服务。

## 下载安装与首次使用

1. 从 [GitHub Releases](https://github.com/MisakaAI/WorkLog/releases) 下载 `worklog-v<版本>-windows-x86_64.exe`，放到长期保留的本机目录。项目 manifest 声明 Windows 10+，兼容性仍需实机验证；无需 Go、Node.js 或额外 GUI 运行时。
2. 双击 exe，首次初始化数据库及迁移后自动打开浏览器。未签名 exe 可能触发 SmartScreen／杀毒软件提示，请先核对来源。
3. 页面关闭后用托盘“打开 Worklog”进入；异常查看 `worklog.log`，预托盘错误以消息框提示。

默认 `127.0.0.1:0` 由系统分配空闲端口，浏览器使用实际 URL。完整退出后端口可能改变；MCP 客户端需更新 URL，或配置固定回环端口。

## 托盘菜单与日常操作

| 实际菜单文字 | 行为 |
| --- | --- |
| 打开 Worklog | 打开现有服务页面，不另建服务；浏览器启动失败可重试。 |
| ● 服务运行中 | 不可点击；运行绿、启动／重启／备份橙、异常红、停止灰。 |
| 导入备份… / 导出备份… | 选择文件／目录，执行下述 SQLite 恢复／备份。 |
| 开机启动 | 默认关闭；勾选后在当前用户登录时静默常驻托盘，不自动打开浏览器。 |
| 重启服务 | 优雅停止 HTTP 并排空请求，再启动监听；复用数据库、业务服务和已绑定端口，不自动打开浏览器。重复点击会合并。 |
| 退出 Worklog | 停止 HTTP、等待请求清理，再释放数据库、托盘和单实例句柄。 |

**“重启服务”不重载配置。** HTTP 服务对象已创建后，即使首次绑定失败，修改 JSON 也必须完整退出再启动；仅对象尚未创建（配置／数据库初始化失败）时重试才重新加载。异常时托盘保留，可排查后重试；不会打开未就绪或被其他程序占用的端口。

重复运行通过命名事件通知已有实例打开页面，不另开 IPC 网络端口或切换配置。单实例按用户会话生效；同一用户跨会话仍共用数据目录，**不要同时恢复或维护同一数据库**。

重启／退出前等待页面保存；程序不会关闭标签页或保存未提交编辑。HTTP 优雅关闭 15 秒超时后断开连接，仍等待请求清理，总耗时可能更长；注销／关机最多等 4 秒清理通知，系统可强制终止。

## 数据、配置与安全

工作目录固定为 `%APPDATA%\WorklogDesktop`，不随 exe 位置改变：

```text
%APPDATA%\WorklogDesktop\
  data\worklog.db       # SQLite 数据库（运行时还可能有 WAL 等文件）
  data\mcp-token        # MCP Bearer 令牌
  worklog.log           # JSON 日志
  worklog.log.1         # 启动时发现日志达到 5 MiB 后轮转的旧日志
  config.json           # 可选；也可能由导入备份创建
```

日志仅在启动时轮转，长时间运行仍会增长。配置优先级：显式 `--conf` 文件或目录 → 上述 `config.json` → 桌面内置默认值。不读取 `WORKLOG_*` 业务环境变量或 `.env`。例如在 PowerShell 中：

```powershell
& 'C:\Apps\worklog-v<版本>-windows-x86_64.exe' --conf 'C:\path\config.json'
```

替换实际版本号。`--conf` 相对路径在切换目录前解析；JSON 内相对数据／令牌路径以桌面数据目录为准。沿用 CLI 的 JSON、身份、时区、认证及 SQLite／PostgreSQL 格式；复用已有数据库请用绝对路径，先停其他写入实例并备份。启动执行必要迁移，不主动导入或清空其他数据库；CLI 默认路径不变。

### 备份与恢复

- **导出**：在线一致性备份生成 `worklog-backup-时间-UUID.db`，包含已提交 WAL 数据并检查完整性与外键，不覆盖文件、不停服务。备份含私人数据，需权限受控并另存独立介质。
- **导入前**：保存编辑，停止同库其他 HTTP／stdio 写入进程，只选独立备份而非使用中的数据库。外部修改过配置时先完整重启，以免覆盖未生效设置。
- **导入**：排空本进程 HTTP、关库，在当前数据库目录恢复为新的 `worklog-restored-时间-UUID.db`。迁移及完整性验证后原子更新 JSON 的 `db_dsn`，再启动服务；无配置时在数据目录创建 `config.json`。保留原库和其他设置，端口可能改变，需重新打开页面核对任务与日志。
- **回退**：未切换配置时失败会尝试恢复原服务；切换后启动失败可重试。手动回退需退出并把 `db_dsn` 改回原库。菜单仅支持 SQLite；PostgreSQL 用 `pg_dump`／`pg_restore`，见[备份与恢复](OPERATIONS.md)。

### 权限与本地安全边界

- 启动会主动重设根目录 DACL，仅授予当前用户与 SYSTEM 可继承访问；子项检查 owner／DACL，允许当前用户、SYSTEM、Administrators，拒绝 reparse point、其他主体授权或不支持的 ACL。不会主动修复子项 ACL，但根权限继承仍适用。令牌 ACL 同样允许上述三类主体，不依赖 Unix `chmod`。
- SQLite 使用本机磁盘；默认令牌原子发布需要硬链接，推荐 NTFS。数据目录外的数据库、令牌、配置及其目录权限／ACL 由用户负责。
- `addr` 仅允许 `127.0.0.1`；保留 Basic／Bearer、Host／Origin／Fetch Metadata、静态白名单和请求限制，不向浏览器注入令牌。仅可信回环且无 Basic 密码时免密；**本机 HTTP 不隔离其他本地程序**，共享电脑建议设至少 8 字节 `basic_password`。
- 默认不信任 Vite Origin，不扩展显式可信名单；自定义 `allowed_hosts` 保留 `127.0.0.1`。动态 `/config.json` 指向 `/api`，不用浏览器本地存储模式。
- 忽略 `static_dir`，始终嵌入前端；更新前端或 `Web/public/work-calendar.json` 必须重建。见[身份认证与安全](AUTHENTICATION.md)。

## 升级、开机启动与卸载

升级前导出备份、保存 JSON 并退出旧程序，再启动新 exe。版本文件可并存但共用数据；旧 exe 不保证兼容已迁移数据，回退需备份恢复。不提供自动更新、安装器或数字签名。

“开机启动”无需管理员权限，只管理 `HKCU\Software\Microsoft\Windows\CurrentVersion\Run` 的 `WorklogDesktop` 值，保存 exe／配置绝对路径，以 `--autostart` 登录启动。菜单勾选反映路径匹配状态，失败会提示并记日志；系统设置／策略可独立禁用。

**移动、更换或删除 exe 不会清除启动项。** 路径／配置改变后需重新勾选。卸载前取消“开机启动”，退出并删 exe；已删除程序则手动删除上述 `WorklogDesktop` 值，勿删整个 Run 键。用户数据保留，确认备份后才单独清理。

## 构建

准备 Git 仓库及发行标签、`go.mod` 指定的 Go、Node.js 24.15+、npm 和下载依赖的网络；版本规则统一见 [DEVELOPMENT：版本与标签](DEVELOPMENT.md#版本与标签)。可在 Linux 交叉构建，无需 Windows SDK、MinGW、CGO 或 Wine：

```sh
bash scripts/build-windows.sh
# Go 不在 PATH 时：GO=/path/to/go bash scripts/build-windows.sh
```

产物为 `bin/worklog-v<版本>-windows-x86_64.exe`。脚本执行 `npm ci`、前端构建、固定版本纯 Go 资源编译器及 `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -tags desktop`；`-H=windowsgui` 隐藏控制台。图标、manifest、前端、Monaco workers、时区和迁移均嵌入 exe；临时 `.syso` 自动清理。不生成 `Worklog.exe` 别名或删除其他版本。推荐始终用脚本。

## 验证清单

以下为待执行检查，不代表已运行。请使用独立测试账号、临时配置和数据库，不接触个人数据；交叉编译不能代替 Windows 实机验收。

- [ ] 在构建机运行 `go test -race ./...`、`go vet ./...` 和构建脚本；在 Windows 运行生成 `Web/dist` 后的 `go test -tags desktop ./cmd/worklog-desktop ./internal/desktop`。
- [ ] 检查首次启动、中文菜单、DPI、Explorer 重启后图标恢复、默认浏览器与 Basic 提示；重复启动仅打开已有页面，并检查会话隔离。
- [ ] 保存数据后重启，确认数据／认证／端口保留；修改 JSON 后确认完整重启才生效。检查端口冲突、异常后重试、退出与注销／关机。
- [ ] 检查中文／空格路径和 ACL 拒绝行为；在临时 SQLite 上导出／导入，核对 WAL 数据、原库保留、失败回退和外部配置修改拒绝。
- [ ] 检查启动项勾选、注册表路径、登录后静默启动、移动 exe 后重新登记及卸载清理；检查动态端口下 MCP 客户端连接。
