#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
DESKTOP="$ROOT/desktop"
TARGET="${1:-zip}"
case "$TARGET" in
  zip|installer) ;;
  *) echo "用法: $0 [zip|installer]" >&2; exit 2 ;;
esac
fail() { printf '构建前置检查失败：%s\n' "$*" >&2; exit 1; }
[[ "$(uname -s)" == Linux && "$(uname -m)" == x86_64 ]] || fail '需要 Linux x86-64 构建主机。'
command -v node >/dev/null || fail '缺少 Node.js 24.22.1。'
command -v npm >/dev/null || fail '缺少 npm 11.19.1。'
[[ "$(node --version)" == v24.22.1 ]] || fail '此桌面构建固定使用 Node.js 24.22.1。'
[[ "$(npm --version)" == 11.19.1 ]] || fail '此桌面构建固定使用 npm 11.19.1。'
GO_BIN="${GO_BIN:-$(command -v go || true)}"
# This development image installs Go outside PATH. Do not modify global PATH.
if [[ -z "$GO_BIN" && -x /opt/go/bin/go ]]; then GO_BIN=/opt/go/bin/go; fi
[[ -n "$GO_BIN" && -x "$GO_BIN" ]] || fail '缺少 Go 1.27.3；安装后设置 GO_BIN=/绝对路径/go。'
[[ "$("$GO_BIN" version)" == 'go version go1.27.3 linux/amd64' ]] || fail '需要与 go.mod 一致的 Go 1.27.3 linux/amd64。'
if [[ "$TARGET" == installer ]]; then
  command -v wine >/dev/null || fail 'NSIS 安装包需要 Wine（建议 Wine 10+，含 x86/32 位支持）；ZIP 不需要。请安装 Wine 或运行不带参数的构建命令。'
  wine --version || fail 'Wine 不可运行；请完成 Wine 初始化。'
fi
cd "$ROOT"
# Never leave an old release looking like the result of a failed rebuild.
# dist contains generated desktop artifacts only (ZIP, installer, unpacked app).
rm -rf -- "$DESKTOP/dist"
echo '[1/6] 根据现有锁文件安装并构建 Vue（不覆盖 Web/dist）'
npm --prefix Web ci
mkdir -p "$DESKTOP/build/backend"
npm --prefix Web run build -- --outDir "$DESKTOP/build/web" --emptyOutDir

echo '[2/6] 测试监管进程，交叉编译原始 Go 后端及桌面监管进程'
GOTOOLCHAIN=local "$GO_BIN" test ./desktop/supervisor
export CGO_ENABLED=0 GOOS=windows GOARCH=amd64 GOTOOLCHAIN=local
"$GO_BIN" build -mod=readonly -trimpath -buildvcs=false -ldflags='-s -w' -o "$DESKTOP/build/backend/worklog.exe" ./cmd/worklog
"$GO_BIN" build -mod=readonly -trimpath -buildvcs=false -ldflags='-s -w' -o "$DESKTOP/build/backend/worklog-supervisor.exe" ./desktop/supervisor
unset GOOS GOARCH

echo '[3/6] 根据独立桌面锁文件安装 Electron 打包依赖并测试'
# Only the Windows runtime fetched by electron-builder is needed.
ELECTRON_SKIP_BINARY_DOWNLOAD=1 npm --prefix "$DESKTOP" ci
# Reuse the lockfile-pinned 7zip binary for archive tests and packaging.
export ELECTRON_BUILDER_7ZIP_PATH="$(node -e 'process.stdout.write(require(process.argv[1]).path7za)' "$DESKTOP/node_modules/7zip-bin")"
chmod +x "$ELECTRON_BUILDER_7ZIP_PATH"
npm --prefix "$DESKTOP" test

echo '[4/6] 记录工具版本、源码版本及锁文件摘要'
export WORKLOG_DESKTOP_GO_BIN="$GO_BIN"
node "$DESKTOP/scripts/build-info.cjs"

echo '[5/6] 预下载并校验 Electron，然后打包 Windows x86-64 应用（未签名）'
node "$DESKTOP/scripts/prefetch-electron.cjs"
# Prevent a build host's signing environment from changing this unsigned build.
export CSC_IDENTITY_AUTO_DISCOVERY=false
unset CSC_LINK WIN_CSC_LINK CSC_KEY_PASSWORD WIN_CSC_KEY_PASSWORD
if [[ "$TARGET" == installer ]]; then
  npm --prefix "$DESKTOP" run pack:installer
else
  npm --prefix "$DESKTOP" run pack:win
fi

echo '[6/6] 验证 Windows PE 架构、资源及可分发产物'
node "$DESKTOP/scripts/verify.cjs" "$TARGET"
echo "完成：$DESKTOP/dist"
