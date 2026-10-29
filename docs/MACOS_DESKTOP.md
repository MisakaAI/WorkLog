# macOS 菜单栏桌面端

WorkLog 以单进程运行原生 AppKit 菜单栏、本地 HTTP 和数据库服务，在默认浏览器显示界面；不使用 Electron 或 WebView，没有 Dock 图标，关闭浏览器不会退出服务。

## 下载安装与首次使用

1. 从 [GitHub Releases](https://github.com/MisakaAI/WorkLog/releases) 下载 `worklog-v<版本>-macos-arm64.dmg`（Apple Silicon）或 `worklog-v<版本>-macos-x86_64.dmg`（Intel）。项目目标系统为 macOS 13+；Apple Silicon 运行 x86_64 版需要 Rosetta。
2. 将镜像中的 `worklog.app` 拖入 `/Applications`（或 `~/Applications`），推出镜像后启动，无需 Go 或 Node.js。
3. 首次初始化数据库及迁移后自动打开浏览器；关闭页面后用菜单“打开 Worklog”进入。异常查看 `worklog.log`。

发行流程不提供开发者身份签名或公证。下载后可能需在“隐私与安全性”中允许打开，请先核对来源；Apple Silicon 链接器的临时签名不等于身份签名或公证。

默认 `127.0.0.1:0` 由系统分配空闲端口，浏览器使用实际 URL。完整退出后端口可能改变；MCP 客户端需更新 URL，或配置固定回环端口。

## 菜单与日常操作

| 实际菜单文字 | 行为 |
| --- | --- |
| 打开 Worklog | 打开现有服务页面，不另建服务。 |
| ● 服务运行中 | 不可点击；还会显示启动、重启、备份、异常或停止状态。 |
| 导入备份… / 导出备份… | 选择文件／目录，执行下述 SQLite 恢复／备份。 |
| 登录后启动 | 默认关闭；勾选后在当前用户图形登录时静默启动，不自动打开浏览器。 |
| 重启服务 | 排空 HTTP 后重启监听，复用数据库、业务、认证及端口；不打开浏览器，重复点击合并。 |
| 退出 Worklog | 停止 HTTP 并等待请求清理，再释放数据库、菜单栏与锁。 |

**“重启服务”不重载配置。** HTTP 服务对象已创建后，即使首次绑定失败，修改 JSON 也必须完整退出再启动；仅对象尚未创建（配置／数据库初始化失败）时重试才重新加载。异常时菜单栏保留，可排查后重试；不会打开未就绪或被其他进程占用的端口。

Finder 重复打开或直接重复运行可执行文件，均通知已有实例打开页面；后者使用文件锁和私有通知标记，不另开 IPC 网络端口。单实例按用户数据目录区分，与应用名、版本、`--conf` 无关；新参数不切换已有配置。退出中重开可能需稍后重试；进程死亡后内核释放锁，**不要删除锁文件**。

重启／退出前等待页面保存；程序不会关闭标签页或保存未提交编辑。HTTP 优雅关闭 15 秒超时后断开连接，仍等待请求清理，总耗时可能更长；浏览器操作不阻塞退出。支持 SIGINT／SIGTERM，注销／关机请求清理但系统可强制终止。

## 数据、配置与安全

工作目录固定在当前用户的 Application Support，与 Finder 启动位置无关：

```text
~/Library/Application Support/WorklogDesktop/
  data/worklog.db       # SQLite 数据库（运行时还可能有 WAL 等文件）
  data/mcp-token        # MCP Bearer 令牌
  worklog.log           # JSON 日志
  worklog.log.1         # 启动时发现日志达到 5 MiB 后轮转的旧日志
  config.json           # 可选；也可能由导入备份创建
  .instance.lock        # 持久锁文件，请勿删除
  .open-request         # 重复启动的临时通知标记
```

日志仅启动时轮转，运行中持续增长。配置优先级：`--conf` 文件或目录 → 上述 `config.json` → 桌面默认值；不读取业务环境变量或 `.env`。先退出已有实例，再指定配置启动：

```sh
open "/Applications/worklog.app" --args --conf "/absolute/path/config.json"
```

`--conf` 相对路径在切换目录前解析；JSON 内相对数据／令牌路径以桌面数据目录为准。沿用 CLI 的 JSON、身份、时区、认证及 SQLite／PostgreSQL 格式；复用已有数据库请用绝对路径，先停其他写入实例并备份。启动执行必要迁移，不主动导入或清空其他数据库。见 [CLI 与维护流程](OPERATIONS.md)。

### 备份与恢复

- **导出**：在线一致性备份生成 `worklog-backup-时间-UUID.db`，包含已提交 WAL 数据并检查完整性与外键，不覆盖文件、不停服务。备份含私人数据，需权限受控并另存独立介质。
- **导入前**：保存编辑，停止同库其他 HTTP／stdio 写入进程，只选独立备份而非使用中的数据库。外部修改过配置时先完整重启，以免覆盖未生效设置。
- **导入**：排空本进程 HTTP、关库，在当前数据库目录恢复为新的 `worklog-restored-时间-UUID.db`。迁移及完整性验证后原子更新 JSON 的 `db_dsn`，再启动服务；无配置时在数据目录创建 `config.json`。保留原库和其他设置，端口可能改变，需重新打开页面核对任务与日志。
- **回退**：未切换配置时失败会尝试恢复原服务；切换后启动失败可重试。手动回退需退出并把 `db_dsn` 改回原库。菜单仅支持 SQLite；PostgreSQL 用 `pg_dump`／`pg_restore`，见[备份与恢复](OPERATIONS.md)。

### 权限与本地安全边界

- 新目录 `0700`，数据库、令牌、日志及锁文件 `0600`。数据树需当前用户持有且无组／其他用户权限；符号链接、特殊文件、扩展 ACL 均拒绝，不自动修复权限。
- 使用本机文件系统，默认令牌原子发布需要硬链接。数据目录外的数据库、令牌、配置及其目录权限／ACL 由用户负责。
- `addr` 仅允许 `127.0.0.1`；保留 Basic／Bearer、Host／Origin／Fetch Metadata、静态白名单和请求限制，不向浏览器注入令牌。仅可信回环且无 Basic 密码时免密；**本机 HTTP 不隔离其他本地程序**，共享电脑建议设至少 8 字节 `basic_password`。
- 默认不信任 Vite Origin，不扩展显式可信名单；自定义 `allowed_hosts` 保留 `127.0.0.1`。动态 `/config.json` 指向 `/api`，不用浏览器本地存储模式。
- 忽略 `static_dir`，始终嵌入前端；更新前端或 `Web/public/work-calendar.json` 必须重建。见[身份认证与安全](AUTHENTICATION.md)。

## 升级、登录后启动与卸载

升级前导出备份、保存 JSON，退出后替换 `.app`，启动并核对数据。不提供自动更新；旧应用不保证兼容已迁移数据，回退需备份恢复。

将应用安装到 `/Applications` 或 `~/Applications` 后再勾选“登录后启动”。它为当前用户创建 LaunchAgent，无需管理员权限：

```text
~/Library/LaunchAgents/io.github.misakaai.worklog.desktop.plist
```

注册保存可执行文件／配置绝对路径，以 `--autostart` 静默启动；`KeepAlive=false`，退出不自动拉起。登录重复进程静默退出，手动重复启动则打开页面。菜单勾选反映路径是否匹配；系统“登录项与扩展”可独立阻止启动。

只读 DMG／App Translocation 中不能启用。移动、重命名或更换配置路径后需重新勾选；取消勾选只删除注册文件，不停当前服务，系统记录可能留到注销，但下次登录不再通过该文件启动。

**删除 `.app` 不会删除登录启动配置或用户数据。** 卸载前先取消“登录后启动”，退出后再删除应用。若应用已删除，可手动删除注册文件：

```sh
rm -f "$HOME/Library/LaunchAgents/io.github.misakaai.worklog.desktop.plist"
```

路径失效不会不断重试。确认备份后才单独清理数据目录。

## 在 Mac 上构建 .app 和 DMG

准备 macOS 13+、Xcode Command Line Tools（`xcode-select --install`）、`go.mod` 指定的 Go、Node.js 24.15+／npm、Git 仓库与标签及联网环境。**AppKit 需要 CGO 和 Apple SDK，不能在普通 Linux 交叉构建。** 版本见 [DEVELOPMENT：版本与标签](DEVELOPMENT.md#版本与标签)。

```sh
bash scripts/build-macos.sh
# 默认当前 Mac 架构；也可使用 Apple SDK 构建另一架构：
ARCH=arm64 bash scripts/build-macos.sh
ARCH=amd64 bash scripts/build-macos.sh
# Go 不在 PATH 时：GO=/path/to/go bash scripts/build-macos.sh
```

产物为 `bin/worklog.app` 及 `bin/worklog-v<版本>-macos-arm64.dmg` 或 `macos-x86_64.dmg`，不生成 universal app。**同名 `.app`／DMG 已存在会报错，换架构前先移走产物，不能直接连续覆盖构建。**

脚本执行 `npm ci`、前端构建及 `CGO_ENABLED=1 go build -tags desktop`，嵌入前端、Monaco workers、时区和迁移。macOS 13 下限由 `MACOSX_DEPLOYMENT_TARGET=13.0`／`LSMinimumSystemVersion=13.0` 声明，仍需实机验证；`WorklogReleaseVersion` 记录完整版本，标准 Bundle 字段用数字版本。

`hdiutil` 创建含应用与 `/Applications` 链接的压缩只读 UDZO 镜像，验证后才发布临时目录中的产物。仅打包已有应用（替换版本／架构）：

```sh
bash scripts/create-macos-dmg.sh bin/worklog.app bin/worklog-v0.2.0-rc.1-macos-arm64.dmg
```

该命令不修改应用或核对版本／架构，调用者负责文件名一致。

## 验证清单

以下待执行，不代表已运行。用独立测试账号、临时配置和数据库，避免个人数据及登录项。在 Mac 仓库根目录运行：

```sh
go test -race ./...
(cd Web && npm ci && npm run build)
CGO_ENABLED=1 go test -race -tags desktop ./...
CGO_ENABLED=1 go vet -tags desktop ./...
# 构建后，只读挂载镜像，核对版本、架构、图标和 Applications 链接：
bash scripts/test-macos-dmg.sh bin/worklog-v0.2.0-rc.1-macos-arm64.dmg
```

替换实际镜像名；检查脚本不安装或启动应用。PostgreSQL 跳过不算通过；自动测试／交叉架构构建不能替代实机验收：

- [ ] Apple Silicon、Intel、最低系统：Finder 安装／首次启动、中文菜单、深浅色、多屏、浏览器及 Basic 提示。
- [ ] 重复运行／Finder reopen 仅保留一个服务；重启保留数据／认证／端口，JSON 完整重启才生效。
- [ ] 端口冲突／异常重试、退出、SIGTERM、注销／关机；未就绪时不打开错误页面。
- [ ] 临时 SQLite：原生选择／确认、WAL 备份恢复、原库保留、失败回退、外部配置修改拒绝；私有权限／符号链接／扩展 ACL 拒绝。
- [ ] 登录注册与 plist、实际重登录静默启动、系统限制、路径改变后重新注册、删除应用后的启动项清理。
