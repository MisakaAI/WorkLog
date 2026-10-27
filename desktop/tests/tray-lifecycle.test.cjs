'use strict'

const { test } = require('node:test')
const assert = require('node:assert/strict')
const { EventEmitter } = require('node:events')
const fs = require('node:fs')
const path = require('node:path')
const vm = require('node:vm')

const mainSource = fs.readFileSync(path.join(__dirname, '../main.cjs'), 'utf8')
const behaviorSource = fs.readFileSync(path.join(__dirname, '../window-behavior.cjs'), 'utf8')
const origin = 'http://127.0.0.1:51432'

function deferred() {
  let resolve
  const promise = new Promise((done) => { resolve = done })
  return { promise, resolve }
}

// Drain promise continuations (ready, backend startup, fatal, and shutdown)
// without timers, real Electron, child processes, or filesystem writes.
const settle = () => new Promise((done) => setImmediate(done))

function createDesktop({ userTasks = 'success', missingIcon = false } = {}) {
  const events = []
  const files = new Map()
  const windows = []
  const trays = []
  const backends = []
  const dialogs = []
  const quitEvents = []
  const ready = deferred()
  const stopped = deferred()
  const paths = { appData: '/memory/app-data' }
  let finalQuits = 0

  function cancelableEvent() {
    return { defaultPrevented: false, preventDefault() { this.defaultPrevented = true } }
  }

  const app = new EventEmitter()
  Object.assign(app, {
    isPackaged: true,
    setAppUserModelId() {},
    getPath: (name) => paths[name],
    setPath: (name, value) => { paths[name] = value },
    requestSingleInstanceLock: () => true,
    whenReady: () => ready.promise,
    setUserTasks(tasks) {
      assert.deepEqual(Array.from(tasks), [])
      events.push('app.setUserTasks')
      if (userTasks === 'throw') throw new Error('policy denied')
      return userTasks !== 'failure'
    },
    quit() {
      events.push('app.quit')
      const event = cancelableEvent()
      quitEvents.push(event)
      app.emit('before-quit', event)
      if (!event.defaultPrevented) {
        finalQuits++
        events.push('app.quit.final')
      }
    },
    exit() { assert.fail('normal startup must not call app.exit') },
  })

  class BrowserWindow extends EventEmitter {
    constructor(options) {
      super()
      this.options = options
      this.visible = false
      this.minimized = false
      this.destroyed = false
      this.webContents = new EventEmitter()
      this.webContents.setWindowOpenHandler = (handler) => { this.openHandler = handler }
      windows.push(this)
      events.push('window.created')
    }
    removeMenu() {}
    isDestroyed() { return this.destroyed }
    isMinimized() { return this.minimized }
    show() { this.visible = true; events.push('window.show') }
    hide() { this.visible = false; events.push('window.hide') }
    focus() { events.push('window.focus') }
    restore() { this.minimized = false; events.push('window.restore') }
    minimize() { this.minimized = true; this.emit('minimize') }
    close() {
      const event = cancelableEvent()
      this.emit('close', event)
      if (!event.defaultPrevented) this.destroy()
      return event
    }
    destroy() {
      this.destroyed = true
      events.push('window.destroy')
      this.emit('closed')
      // Electron emits this during destroy; it must not skip pending cleanup.
      app.emit('window-all-closed')
    }
    async loadURL(url) {
      this.url = url
      this.emit('ready-to-show')
    }
  }

  class Tray extends EventEmitter {
    constructor(icon) {
      super()
      this.icon = icon
      this.destroyed = false
      trays.push(this)
      events.push('tray.created')
    }
    setToolTip(text) { this.tooltip = text }
    setContextMenu(menu) { this.menu = menu; events.push('tray.menu') }
    destroy() { this.destroyed = true; events.push('tray.destroy') }
  }

  class Backend extends EventEmitter {
    constructor(options) {
      super()
      this.options = options
      backends.push(this)
      events.push('backend.created')
    }
    async start() { events.push('backend.start'); return origin }
    stop() { events.push('backend.stop'); return stopped.promise }
  }

  const defaultSession = new EventEmitter()
  defaultSession.setPermissionRequestHandler = (handler) => { defaultSession.permissionRequest = handler }
  defaultSession.setPermissionCheckHandler = (handler) => { defaultSession.permissionCheck = handler }
  defaultSession.webRequest = {
    onBeforeRequest(handler) { this.beforeRequest = handler },
    onHeadersReceived(handler) { this.headersReceived = handler },
  }

  const electron = {
    app, BrowserWindow, Tray,
    session: { defaultSession },
    Menu: { buildFromTemplate: (template) => template },
    nativeImage: {
      createFromPath(iconPath) {
        assert.equal(iconPath, '/memory/resources/tray.ico')
        return { isEmpty: () => missingIcon }
      },
    },
    dialog: {
      async showMessageBox(options) { dialogs.push(options); events.push('dialog.fatal') },
    },
  }
  const memoryFs = {
    mkdirSync(dir) { files.set(dir, '') },
    existsSync: (file) => files.has(file),
    renameSync(from, to) { files.set(to, files.get(from)); files.delete(from) },
    appendFileSync(file, text) { files.set(file, (files.get(file) || '') + text.toString()) },
  }
  const runtime = {
    Backend,
    prepareConfig(dataDir) {
      events.push('config.prepared')
      const file = path.join(dataDir, 'backend.json')
      files.set(file, '{}')
      return file
    },
    isAppPage: (url, expectedOrigin) => url === `${expectedOrigin}/`,
    isAppRequest: (url, expectedOrigin) => url.startsWith(`${expectedOrigin}/`),
  }
  const behaviorModule = { exports: {} }
  vm.runInNewContext(behaviorSource, { module: behaviorModule }, { filename: 'window-behavior.cjs' })
  const dependencies = {
    electron,
    'node:fs': memoryFs,
    'node:path': path,
    './runtime.cjs': runtime,
    './window-behavior.cjs': behaviorModule.exports,
  }
  vm.runInNewContext(mainSource, {
    require(name) {
      assert.ok(Object.hasOwn(dependencies, name), `unexpected dependency: ${name}`)
      return dependencies[name]
    },
    process: {
      platform: 'win32', arch: 'x64', argv: ['worklog.exe'], resourcesPath: '/memory/resources',
    },
    Buffer,
  }, { filename: 'main.cjs' })

  return {
    app, events, files, windows, trays, backends, dialogs, quitEvents, stopped,
    get finalQuits() { return finalQuits },
    async initialize() { ready.resolve(); await settle() },
  }
}

test('ready, close-to-tray, native minimize, restore routes, and deferred Exit cleanup', async () => {
  const desktop = createDesktop()
  const { app, events } = desktop
  assert.equal(desktop.trays.length, 0, 'startup waits for app readiness')
  await desktop.initialize()

  assert.equal(desktop.trays.length, 1)
  assert.equal(desktop.backends.length, 1)
  assert.equal(desktop.windows.length, 1)
  assert.deepEqual(desktop.dialogs, [])
  assert.ok(events.indexOf('tray.created') < events.indexOf('backend.start'))
  assert.ok(events.indexOf('tray.menu') < events.indexOf('backend.start'), 'exit route exists before Go starts')
  const [window] = desktop.windows
  const [tray] = desktop.trays
  const showMenu = tray.menu.find((item) => item.label === '显示主界面')
  const exitMenu = tray.menu.find((item) => item.label === '退出 Worklog')
  assert.ok(showMenu && exitMenu)
  assert.equal(window.url, `${origin}/`)
  assert.equal(window.visible, true)

  assert.equal(window.close().defaultPrevented, true)
  assert.equal(window.visible, false)
  assert.equal(window.destroyed, false)
  assert.equal(tray.destroyed, false)
  assert.equal(desktop.quitEvents.length, 0)
  assert.equal(events.includes('backend.stop'), false)

  for (const [route, show] of [
    ['tray click', () => tray.emit('click')],
    ['tray menu', () => showMenu.click()],
    ['second instance', () => app.emit('second-instance', {}, [], '/memory', { action: 'show' })],
  ]) {
    window.minimize()
    const before = events.length
    show()
    assert.equal(window.minimized, false, `${route} restores a minimized window`)
    assert.equal(window.visible, true, `${route} shows the hidden window`)
    assert.deepEqual(events.slice(before), ['window.restore', 'window.show', 'window.focus'])
    window.close()
  }

  showMenu.click()
  const beforeMinimize = events.length
  window.minimize()
  assert.equal(window.listenerCount('minimize'), 0, 'native minimize is not intercepted')
  assert.equal(window.minimized, true)
  assert.equal(window.visible, true, 'native minimize does not hide the taskbar window')
  assert.equal(window.destroyed, false)
  assert.deepEqual(events.slice(beforeMinimize), [])
  assert.equal(desktop.quitEvents.length, 0)
  assert.equal(events.includes('backend.stop'), false)

  exitMenu.click()
  await settle()
  assert.equal(desktop.quitEvents.length, 1, 'Exit enters app.quit / before-quit')
  assert.equal(desktop.quitEvents[0].defaultPrevented, true)
  assert.equal(tray.destroyed, true)
  assert.equal(window.destroyed, true)
  assert.deepEqual(events.slice(-4), ['app.quit', 'tray.destroy', 'window.destroy', 'backend.stop'])
  assert.equal(desktop.finalQuits, 0, 'window-all-closed must not bypass deferred backend.stop')

  const duringQuit = events.length
  tray.emit('click')
  showMenu.click()
  exitMenu.click()
  app.emit('second-instance', {}, [], '/memory', { action: 'show' })
  assert.deepEqual(events.slice(duringQuit), [], 'tray and second-instance routes are inert while quitting')

  desktop.stopped.resolve()
  await settle()
  assert.equal(desktop.quitEvents.length, 2)
  assert.equal(desktop.quitEvents[1].defaultPrevented, false)
  assert.equal(desktop.finalQuits, 1)
  assert.equal(events.filter((event) => event === 'backend.stop').length, 1)
  assert.equal(events.filter((event) => event === 'tray.destroy').length, 1)
  assert.equal(events.filter((event) => event === 'window.destroy').length, 1)
})

for (const userTasks of ['failure', 'throw']) {
  test(`legacy setUserTasks([]) ${userTasks} does not prevent startup`, async () => {
    const desktop = createDesktop({ userTasks })
    await desktop.initialize()
    assert.ok(desktop.events.includes('app.setUserTasks'))
    assert.ok(desktop.events.includes('backend.start'))
    assert.equal(desktop.trays.length, 1)
    assert.equal(desktop.trays[0].destroyed, false)
    assert.equal(desktop.windows.length, 1)
    assert.equal(desktop.windows[0].url, `${origin}/`)
    assert.equal(desktop.windows[0].visible, true)
    assert.deepEqual(desktop.dialogs, [])
    assert.equal(desktop.quitEvents.length, 0)
    const log = desktop.files.get('/memory/app-data/WorklogDesktop/desktop.log')
    assert.match(log, userTasks === 'throw' ? /policy denied/ : /无法清除旧版任务栏任务/)
  })
}

test('missing tray icon reports a fatal error and quits before starting the backend', async () => {
  const desktop = createDesktop({ missingIcon: true })
  await desktop.initialize()
  assert.equal(desktop.dialogs.length, 1)
  assert.equal(desktop.dialogs[0].type, 'error')
  assert.match(desktop.dialogs[0].message, /托盘图标/)
  assert.match(desktop.files.get('/memory/app-data/WorklogDesktop/desktop.log'), /托盘图标/)
  assert.equal(desktop.trays.length, 0)
  assert.equal(desktop.backends.length, 0)
  assert.equal(desktop.windows.length, 0)
  assert.equal(desktop.events.includes('config.prepared'), false)
  assert.equal(desktop.events.includes('backend.start'), false)
  assert.equal(desktop.events.includes('backend.stop'), false)
  assert.ok(desktop.events.indexOf('dialog.fatal') < desktop.events.indexOf('app.quit'))
  assert.equal(desktop.quitEvents.length, 2)
  assert.equal(desktop.quitEvents[0].defaultPrevented, true)
  assert.equal(desktop.finalQuits, 1)
})
