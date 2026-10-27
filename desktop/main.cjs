'use strict'

const { app, BrowserWindow, dialog, session } = require('electron')
const fs = require('node:fs')
const path = require('node:path')
const { Backend, prepareConfig, isAppPage, isAppRequest } = require('./runtime.cjs')
const { launchAction, registerTaskbarExit, minimizeOnClose, handleSecondInstance } = require('./window-behavior.cjs')

// electron-builder strips package.json's build metadata from app.asar.
// Keep this runtime ID aligned with build.appId in the source package.json.
if (process.platform === 'win32') app.setAppUserModelId('org.worklog.desktop')
const action = launchAction(process.argv)

// Same path for ZIP and installer, independent of installation path/version.
app.setPath('userData', path.join(app.getPath('appData'), 'WorklogDesktop'))
let backend
let window
let quitting = false
let reporting = false
let log = () => {}

async function fatal(error) {
  if (quitting || reporting) return
  reporting = true
  log(`${error.stack || error}\n`)
  try {
    await dialog.showMessageBox({
      type: 'error', title: 'Worklog 无法继续运行',
      message: error.message || String(error),
      detail: `请退出后重试；如仍失败，请检查磁盘空间、用户目录权限及安全软件拦截。\n日志：${path.join(app.getPath('userData'), 'desktop.log')}`,
    })
  } finally { app.quit() }
}

app.on('before-quit', (event) => {
  if (quitting) return
  event.preventDefault()
  quitting = true
  // Do not let renderer beforeunload dialogs obstruct service cleanup.
  if (window && !window.isDestroyed()) window.destroy()
  void (async () => {
    try { await backend?.stop() }
    catch (error) { log(`cleanup: ${error.message}\n`) }
    app.quit()
  })()
})
app.on('window-all-closed', () => {
  // window.destroy() during a real quit must not bypass the pending Go cleanup.
  if (!quitting) app.quit()
})

if (!app.requestSingleInstanceLock({ action })) {
  app.exit(0)
} else if (action === 'quit') {
  // A stale pinned taskbar task must not start a new UI/backend just to exit.
  app.exit(0)
} else {
  app.on('second-instance', (_event, _argv, _workingDirectory, additionalData) => {
    handleSecondInstance(app, window, additionalData?.action, () => quitting)
  })
  app.whenReady().then(async () => {
    if (process.platform !== 'win32' || process.arch !== 'x64' || !app.isPackaged) {
      throw new Error('此入口只支持已打包的 Windows x86-64 应用，请使用 Linux 构建脚本生成 ZIP 后在 Windows 运行。')
    }
    const dataDir = app.getPath('userData')
    fs.mkdirSync(dataDir, { recursive: true })
    const logFile = path.join(dataDir, 'desktop.log')
    const previousLog = `${logFile}.previous`
    if (fs.existsSync(logFile)) fs.renameSync(logFile, previousLog)
    let written = 0
    log = (text) => {
      // Bound each run's log; never log configuration contents or tokens.
      if (written >= 4 * 1024 * 1024) return
      const entry = Buffer.from(text).subarray(0, 4 * 1024 * 1024 - written)
      written += entry.length
      try { fs.appendFileSync(logFile, entry, { mode: 0o600 }) } catch { /* Disk errors must not prevent cleanup. */ }
    }
    // Install the real exit command before starting Go. Fail visibly rather
    // than leave a minimize-on-close app without its promised exit route.
    registerTaskbarExit(app, process.execPath)
    const configFile = prepareConfig(dataDir, path.join(process.resourcesPath, 'web'))
    backend = new Backend({
      supervisor: path.join(process.resourcesPath, 'backend', 'worklog-supervisor.exe'),
      executable: path.join(process.resourcesPath, 'backend', 'worklog.exe'),
      configFile, dataDir, log,
    })
    backend.on('failure', (error) => void fatal(error))
    const origin = await backend.start()
    if (quitting) return
    const ses = session.defaultSession
    ses.setPermissionRequestHandler((_contents, _permission, callback) => callback(false))
    ses.setPermissionCheckHandler(() => false)
    ses.on('will-download', (_event, item) => {
      // Keep normal exports/downloads, but never execute downloaded files.
      log(`download: ${item.getFilename()}\n`)
    })
    ses.webRequest.onBeforeRequest((details, callback) => {
      callback({ cancel: !isAppRequest(details.url, origin, details.resourceType) })
    })
    ses.webRequest.onHeadersReceived((details, callback) => {
      callback({ responseHeaders: {
        ...details.responseHeaders,
        'Content-Security-Policy': ["default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; font-src 'self' data:; worker-src 'self' blob:; connect-src 'self'; object-src 'none'; base-uri 'self'; frame-src 'none'; form-action 'self'"],
      } })
    })
    window = new BrowserWindow({
      width: 1280, height: 860, minWidth: 900, minHeight: 640,
      show: false, autoHideMenuBar: true, title: 'Worklog',
      webPreferences: {
        nodeIntegration: false, contextIsolation: true, sandbox: true,
        webSecurity: true, allowRunningInsecureContent: false,
        webviewTag: false, devTools: false,
      },
    })
    minimizeOnClose(window, () => quitting)
    window.removeMenu()
    const contents = window.webContents
    contents.setWindowOpenHandler(() => ({ action: 'deny' }))
    contents.on('will-attach-webview', (event) => event.preventDefault())
    for (const eventName of ['will-navigate', 'will-redirect']) {
      contents.on(eventName, (event, url) => {
        if (!isAppPage(url, origin)) event.preventDefault()
      })
    }
    contents.on('will-frame-navigate', (event) => {
      if (!event.isMainFrame || !isAppPage(event.url, origin)) event.preventDefault()
    })
    contents.on('render-process-gone', (_event, details) => {
      void fatal(new Error(`界面进程已停止（${details.reason}）。请重新启动 Worklog。`))
    })
    contents.on('did-fail-load', (_event, code, description, _url, mainFrame) => {
      if (mainFrame && code !== -3) void fatal(new Error(`界面加载失败：${description}（${code}）。`))
    })
    window.once('ready-to-show', () => { if (!quitting) window.show() })
    await window.loadURL(`${origin}/`)
  }).catch((error) => void fatal(error))
}
