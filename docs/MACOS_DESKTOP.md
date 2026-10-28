# macOS 菜单栏桌面端

Worklog 使用原生 AppKit `NSStatusItem` 菜单栏和 Go 本地服务，在系统默认浏览器打开嵌入的 Vue 界面。菜单栏、HTTP、数据库连接均在一个进程内，不使用 Electron、WebView 或独立业务后端进程。关闭浏览器不会退出服务。

## 在 Mac 上构建 .app 和 DMG

需要 macOS 13+（与当前 Go 工具链的最低版本一致）、Xcode Command Line Tools（`xcode-select --install`）、`go.mod` 指定的 Go、Node.js 24.15+、npm、Git 仓库及发行标签，以及下载依赖的网络。AppKit 桥接需要 CGO 和 Apple SDK，不能用普通 Linux 交叉编译代替。

```sh
bash scripts/build-macos.sh
# 默认构建当前 Mac 架构；也可使用 Apple SDK 构建另一架构：
ARCH=arm64 bash scripts/build-macos.sh
ARCH=amd64 bash scripts/build-macos.sh
# Go 不在 PATH 时：GO=/path/to/go bash scripts/build-macos.sh
```

应用目录固定为 `bin/worklog.app`，安装镜像为 `bin/worklog-v<版本>-macos-arm64.dmg` 或 `bin/worklog-v<版本>-macos-x86_64.dmg`。例如当前发行标签为 `v0.2.0-rc.1` 时，arm64 镜像名为 `worklog-v0.2.0-rc.1-macos-arm64.dmg`，镜像中的应用仍名为 `worklog.app`。应用为本次选定的单一架构；构建另一架构前需先移走 `bin/worklog.app`。

脚本复用 `scripts/release-version.sh`，版本取 HEAD 可达的最近发行标签，不接受 `VERSION` 覆盖；规则见 [发布版本](../README.md#发布版本)。完整 Git 版本记在 `Info.plist` 的 `WorklogReleaseVersion`，标准 Bundle 版本字段使用对应的数字版本。

脚本执行 `npm ci`、Vue 生产构建和 `CGO_ENABLED=1 go build -tags desktop`，将前端、Monaco workers、时区和迁移嵌入可执行文件，再生成 `Info.plist`（`LSUIElement=true`，无 Dock 图标）。应用图标复用 `Web/public/logo.png`，由 macOS 自带的 `sips`／`iconutil` 生成包含标准及 Retina 尺寸的 `Worklog.icns`，通过 `CFBundleIconFile` 声明，供 Finder 等系统界面显示。菜单栏使用系统模板图标，适应深浅色。目标电脑无需 Go 或 Node.js。两种架构分别打包，不生成 universal app；Apple Silicon 运行 x86_64 版本需要 Rosetta。

DMG 使用 macOS 自带的 `hdiutil` 创建为压缩、只读的 UDZO 镜像，无需 Python 或第三方打包依赖。镜像包含 `worklog.app` 和指向 `/Applications` 的快捷链接；打开镜像，将应用拖入 Applications，推出镜像后从 Applications 启动。也可将 `.app` 直接复制到 `~/Applications`。

构建先写入临时目录，镜像生成并通过 `hdiutil verify` 后才发布 `.app` 与 `.dmg`；如果同名产物已存在会报错，请先移走旧产物。需要仅打包已有应用时可执行：

```sh
bash scripts/create-macos-dmg.sh bin/worklog.app bin/worklog-v0.2.0-rc.1-macos-arm64.dmg
```

单独打包命令不修改应用，也不判断版本和架构，调用者应确保文件名与实际应用一致。此阶段不提供签名或公证流程；通过网络分发后 macOS 可能要求在“隐私与安全性”中允许打开。Apple Silicon 链接器可能生成运行所需的临时签名，它不代表开发者身份签名或公证。

## 菜单与生命周期

```text
打开 Worklog
────────────────
● 服务运行中          （不可点击）
────────────────
导入备份…
导出备份…
────────────────
✓ 登录后启动
────────────────
重启服务
退出 Worklog
```

- 首次启动完成数据库初始化和 HTTP 监听后，自动打开默认浏览器；URL 使用实际绑定地址。默认仅监听 `127.0.0.1:0`，由系统分配空闲端口。
- “打开 Worklog”只打开现有服务页面。重复双击 `.app` 时，Finder 的 reopen 事件会打开页面；直接重复运行可执行文件时，通过文件锁识别已有实例，并写入私有目录中的空通知标记，已有实例约 200 毫秒内处理。启动期间的通知保留、合并；不会另开服务或 IPC 网络端口。
- 单实例按用户数据目录区分，与 app 名称、版本或 `--conf` 无关。已有实例运行时，新启动的配置参数不会切换它的配置。锁文件保留在磁盘，进程死亡后内核自动释放锁；无需手工删除锁文件。正在退出时再次启动可能需要等退出完成后重试。
- 状态显示启动中、运行中、重启中、异常或已停止。服务初始化或端口绑定失败时保留菜单栏，可修复问题后重启；不打开未就绪或由其他进程占用的端口。
- “重启服务”先优雅关闭 HTTP、排空请求，再启动监听；复用数据库、业务服务、认证及已绑定端口，不自动打开浏览器。多次点击合并。服务已成功初始化后，修改配置需要完整退出再启动；初始化失败时重启会重试加载配置。
- “退出 Worklog”先取消启动／运行，停止 HTTP（15 秒优雅超时后关闭连接，继续等待请求清理），再释放数据库和菜单栏、单实例锁。浏览器打开操作在独立队列执行，不阻塞服务重启和退出。终端直接运行时也处理 SIGINT／SIGTERM。注销／关机由 AppKit 请求异步清理，但系统仍可强制终止进程。

退出或重启前请等待页面保存完成。退出不会强制关闭浏览器标签页，也不能保存尚未提交到服务的编辑。

## 登录后启动与删除应用

“登录后启动”默认关闭。先将 `worklog.app` 安装到 `/Applications` 或 `~/Applications`，再在菜单中勾选。应用会为当前用户创建以下 LaunchAgent，下一次图形登录时启动菜单栏和本地服务，不自动打开浏览器：

```text
~/Library/LaunchAgents/io.github.misakaai.worklog.desktop.plist
```

采用用户 LaunchAgent，以兼容当前未提供身份签名的发行方式；不需要管理员权限。注册保存应用可执行文件的绝对路径和显式配置参数，通过 `--autostart` 静默启动；`KeepAlive=false`，退出应用后不会自动拉起。登录触发的重复进程静默退出，手动重复启动仍会打开已有实例页面。菜单勾选反映当前应用／配置对应的注册文件；系统“登录项与扩展”中的限制可能独立阻止启动。修改系统设置后，应在系统设置中允许运行。

只读 DMG 和 App Translocation 中的应用不能启用此选项。注册后移动或重命名应用会使原路径失效，应在新位置重新勾选；更改显式配置路径后也需重新注册。取消勾选会删除此应用的注册文件，不影响当前运行的服务；下一次登录不再启动。

**删除 `.app` 不会自动删除登录启动配置，也不会删除用户数据。** 建议先取消“登录后启动”，再退出并删除应用。如果已删除应用，可手动删除注册文件：

```sh
rm -f "$HOME/Library/LaunchAgents/io.github.misakaai.worklog.desktop.plist"
```

删除注册文件后，当前登录会话的系统记录可能保留到注销；下一次登录不会再通过该文件启动。应用路径不存在时启动会失败，因为未启用 KeepAlive，不会不断重启。数据仍保留在下述 Application Support 目录，可按需单独备份、清理。

## 菜单备份与恢复

- “导出备份…”选择目录后，通过现有 SQLite 在线备份逻辑生成 `worklog-backup-时间-UUID.db` 并验证完整性；服务继续运行，不直接复制使用中的数据库，也不覆盖同名文件。备份含私人数据，请妥善保管。
- “导入备份…”选择独立的 `.db` 备份并确认后，先排空 HTTP 请求、关闭数据库，再恢复到新的 `worklog-restored-时间-UUID.db`，执行迁移和完整性验证，最后原子更新配置并启动服务。原数据库保留，其他配置字段和认证设置保留。导入后重新打开 Worklog 页面，检查任务与日志。
- 验证或配置切换失败时不切换原数据库，并尝试恢复原服务；服务启动失败可从菜单重试。配置文件已在外部修改时，请完整退出再启动后导入，避免覆盖尚未生效的设置。
- 导入前先保存页面编辑，并停止使用相同数据库的其他 HTTP／stdio 写入程序；不要把仍在使用中的数据库当作独立备份导入。此流程沿用 Windows 桌面版的备份／恢复实现。
- 菜单备份仅支持 SQLite。PostgreSQL 请使用 `pg_dump`／`pg_restore`，详见 [备份与恢复](OPERATIONS.md)。

## 数据、配置与安全

数据默认位于当前用户的 Application Support，工作目录固定于此，与 Finder 启动位置无关：

```text
~/Library/Application Support/WorklogDesktop/
  data/worklog.db       # SQLite、WAL、迁移
  data/mcp-token        # 原有 Bearer 令牌
  worklog.log           # JSON 日志
  worklog.log.1         # 下次启动达到 5 MiB 时轮转
  config.json          # 可选，用户自行创建
  .instance.lock       # 持久锁文件，请勿删除
  .open-request        # 重复启动时的临时通知标记
```

新目录使用 `0700`，数据库、令牌、日志及锁文件使用 `0600`。启动前检查现有数据树：必须由当前用户持有、没有组／其他用户权限，且不含符号链接、特殊文件或扩展 ACL；发现不安全权限会报错，不擅自修改已有数据。请使用本机文件系统；令牌的原子发布沿用硬链接机制。日志仅在启动时轮转，长时间运行会继续增长。

默认读取上述目录的 `config.json`，不存在时使用现有 `config.LoadDesktop` 默认值，不读取业务环境变量或 `.env`。也可显式指定 JSON 文件或目录：

```sh
"/Applications/worklog.app/Contents/MacOS/Worklog" --conf "/absolute/path/config.json"
# 或通过 LaunchServices 首次启动并传参：
open "/Applications/worklog.app" --args --conf "/absolute/path/config.json"
```

显式 `--conf` 的相对路径在切换工作目录前解析；配置内部相对数据库／令牌路径以 Application Support 中的 Worklog 目录为准。显式配置在该目录之外的数据、令牌和配置文件时，其目录权限与 ACL 由用户负责。沿用原有 SQLite／PostgreSQL 配置和数据格式，不会自动迁移、覆盖或清空其他版本数据；复用已有数据库前先关闭其他写入实例并备份。SQLite 可使用下述菜单导入／导出；其他维护方式见 [CLI 与维护流程](OPERATIONS.md)。

安全行为与 Windows 桌面版相同：仅允许 `127.0.0.1` 监听；忽略磁盘 `static_dir`，始终提供嵌入资源；保留 Basic／Bearer、Host／Origin／Fetch Metadata、安全路径白名单和请求限制；不向浏览器注入 MCP 令牌，不新增 Vite 开发 Origin 信任。本机免密条件不变，本机 HTTP 不隔离同一电脑的其他程序，共享电脑建议配置 `basic_password`（至少 8 字节）。动态端口在完整退出后可能改变，MCP 客户端需更新 URL，或显式配置固定回环端口。

## 测试与待验收

```sh
go test -race ./...
# 先生成 Web/dist，再测试实际桌面入口；不会打开菜单或浏览器：
cd Web && npm ci && npm run build
cd ..
CGO_ENABLED=1 go test -race -tags desktop ./cmd/worklog-desktop ./internal/desktop ./internal/app ./internal/config ./internal/transport/...
go vet -tags desktop ./cmd/worklog-desktop ./internal/desktop
# 只读挂载安装镜像，核对应用名、版本、架构、图标和 Applications 链接：
bash scripts/test-macos-dmg.sh bin/worklog-v0.2.0-rc.1-macos-arm64.dmg
```

macOS 专属自动测试覆盖并发单实例选举、跨进程通知、启动前通知保留、关闭及异常退出后重新获得锁、危险锁文件、私有目录／符号链接／ACL、URL 拒绝、服务失败重试、取消启动，以及重启后数据／令牌／认证／端口保留。新增测试覆盖登录注册的幂等开关、plist 参数转义及系统解析、私有权限／危险路径拒绝、删除应用后注册仍保留、静默登录与重复启动，以及 SQLite 导出／导入往返、原库保留、认证／配置保留、失败回退和外部配置修改拒绝。登录与备份测试仅使用临时 HOME／数据库，不注册真实用户登录项。现有 HTTP 与安全回归测试继续适用。

DMG 打包已在 macOS arm64 实际执行：arm64 与 x86_64 镜像均通过 `hdiutil verify` 和只读挂载检查，确认内含 `worklog.app`、对应版本和架构的可执行文件、应用图标及指向 `/Applications` 的链接。检查脚本也已加入 macOS CI；未完成 Finder 拖拽安装的图形交互验收。

本次实现已在 macOS 27 arm64、Go 1.27.3 上通过 `go test -race ./...`、`go test -race -tags desktop ./...`、`go vet -tags desktop ./...`，完成 arm64／x86_64 `.app` 构建、plist 校验，以及 Windows 桌面入口和 Linux CLI 交叉构建。使用一次性临时 HOME 启动实际 arm64 应用，确认 AppKit 启动、默认浏览器请求本地页面、重复运行通知原实例（仍只有一个 HTTP 服务），以及 SIGTERM 后 HTTP／进程清理。测试未访问个人 Worklog 数据。

新增功能已在同一 macOS arm64 环境完成自动测试；实际 arm64 应用以临时 HOME 和 `--autostart` 启动的冒烟检查验证静默启动、手动重复启动打开已有服务、登录触发的重复进程静默退出和 SIGTERM 清理。

图形自动化连接不可用，因此没有完成菜单点击、原生文件选择／确认弹窗、Finder reopen 或浏览器画面验收；实际注销再登录、系统设置对 LaunchAgent 的处理也未验证。Intel 实机、macOS 13 实机、注销／关机与 PostgreSQL 未验证。新增 macOS CI 配置尚未在远端运行；本地未配置 PostgreSQL 的用例按现有规则跳过。

自动测试不等于图形交互验收。发布前仍需在目标 Mac 上检查 Finder 首次／重复启动、中文菜单、深浅色／多显示器菜单栏、默认浏览器及 Basic 提示、重启与退出、注销／关机。Intel 与最低系统版本也需分别验收；构建另一架构不代表已在对应硬件运行。此阶段不提供自动更新、身份签名或公证。
