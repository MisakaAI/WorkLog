'use strict'

const { test } = require('node:test')
const assert = require('node:assert/strict')
const { EventEmitter } = require('node:events')
const fs = require('node:fs')
const path = require('node:path')
const vm = require('node:vm')
const {
  EXIT_ARGUMENT, launchAction, registerTaskbarExit, minimizeOnClose, handleSecondInstance,
} = require('../window-behavior.cjs')

function mockWindow() {
  const window = new EventEmitter()
  const calls = []
  window.isDestroyed = () => false
  window.isMinimized = () => true
  for (const name of ['minimize', 'restore', 'show', 'focus']) window[name] = () => calls.push(name)
  return { window, calls }
}

test('close minimizes instead of destroying or hiding the taskbar window', () => {
  const { window, calls } = mockWindow()
  let quitting = false
  let prevented = 0
  minimizeOnClose(window, () => quitting)
  const event = { preventDefault() { prevented++ } }
  window.emit('close', event)
  window.emit('close', event)
  assert.equal(prevented, 2)
  assert.deepEqual(calls, ['minimize', 'minimize'])
  quitting = true
  window.emit('close', event)
  assert.equal(prevented, 2, 'real quit must not be prevented')
  assert.equal(calls.length, 2, 'real quit must not minimize again')
})

test('taskbar exit task uses the packaged executable, including paths with spaces', () => {
  const executable = 'C:\\安装目录 with spaces\\Worklog.exe'
  let tasks
  registerTaskbarExit({ setUserTasks(value) { tasks = value; return true } }, executable)
  assert.equal(tasks.length, 1)
  assert.equal(tasks[0].title, '退出 Worklog')
  assert.equal(tasks[0].program, executable)
  assert.equal(tasks[0].arguments, EXIT_ARGUMENT)
  assert.equal(tasks[0].iconPath, executable)
  assert.equal(launchAction([executable, EXIT_ARGUMENT]), 'quit')
  assert.equal(launchAction([executable]), 'show')
  assert.equal(launchAction([executable, `${EXIT_ARGUMENT}=false`]), 'show')
})

test('failure to register the taskbar exit is reported instead of trapping the user', () => {
  assert.throws(() => registerTaskbarExit({ setUserTasks() { return false } }, 'Worklog.exe'), /任务列表.*系统策略/)
})

test('normal second launch restores and focuses the existing window', () => {
  const { window, calls } = mockWindow()
  const app = { quit() { assert.fail('normal launch must not quit') } }
  handleSecondInstance(app, window, 'show', () => false)
  assert.deepEqual(calls, ['restore', 'show', 'focus'])
  window.isMinimized = () => false
  calls.length = 0
  handleSecondInstance(app, window, undefined, () => false)
  assert.deepEqual(calls, ['show', 'focus'])
})

test('taskbar exit requests real quit, even before a window exists', () => {
  const { window, calls } = mockWindow()
  let quit = 0
  const app = { quit() { quit++ } }
  handleSecondInstance(app, window, 'quit', () => false)
  handleSecondInstance(app, undefined, 'quit', () => false)
  handleSecondInstance(app, window, 'quit', () => true)
  assert.equal(quit, 2)
  assert.deepEqual(calls, [])
})

test('restore requests are harmless for missing/destroyed windows or during cleanup', () => {
  const { window, calls } = mockWindow()
  const app = { quit() { assert.fail('restore must not quit') } }
  handleSecondInstance(app, undefined, 'show', () => false)
  window.isDestroyed = () => true
  handleSecondInstance(app, window, 'show', () => false)
  window.isDestroyed = () => false
  handleSecondInstance(app, window, 'show', () => true)
  assert.deepEqual(calls, [])
})

function loadMain({ action = 'show', primary = true, packaged = false } = {}) {
  const app = new EventEmitter()
  const exits = []
  let readyCalls = 0
  let quitCalls = 0
  let intent
  app.getPath = () => 'C:\\Users\\tester\\AppData\\Roaming'
  app.setPath = () => {}
  app.setAppUserModelId = (id) => assert.equal(id, require('../package.json').build.appId)
  app.requestSingleInstanceLock = (data) => { intent = data.action; return primary }
  app.exit = (code) => exits.push(code)
  app.quit = () => { quitCalls++ }
  // Pending ready ensures these routing tests cannot start services or write data.
  app.whenReady = () => { readyCalls++; return new Promise(() => {}) }
  vm.runInNewContext(fs.readFileSync(path.join(__dirname, '../main.cjs'), 'utf8'), {
    process: {
      platform: 'win32', arch: 'x64', execPath: 'C:\\Worklog.exe',
      argv: ['C:\\Worklog.exe', ...(action === 'quit' ? [EXIT_ARGUMENT] : [])],
    },
    require(name) {
      if (name === 'electron') return { app }
      if (name === './package.json' && packaged) {
        const { build, ...metadata } = require('../package.json')
        return metadata
      }
      if (name.startsWith('./')) return require(path.join(__dirname, '..', name))
      return require(name)
    },
  }, { filename: 'desktop/main.cjs' })
  return { app, exits, intent, readyCalls, quitCalls: () => quitCalls }
}

test('Windows startup does not depend on build metadata stripped by electron-builder', () => {
  const result = loadMain({ packaged: true })
  assert.equal(result.intent, 'show')
  assert.equal(result.readyCalls, 1)
  assert.deepEqual(result.exits, [])
})

test('exit launch forwards intent without starting a second backend', () => {
  for (const primary of [false, true]) {
    const result = loadMain({ action: 'quit', primary })
    assert.equal(result.intent, 'quit')
    assert.deepEqual(result.exits, [0])
    assert.equal(result.readyCalls, 0, 'even a stale exit task must not initialize Go or create a window')
  }
})

test('main process honors second-instance exit intent through the normal quit path', () => {
  const result = loadMain()
  assert.equal(result.intent, 'show')
  assert.equal(result.readyCalls, 1)
  result.app.emit('second-instance', {}, ['reordered-by-Chromium'], '', { action: 'quit' })
  assert.equal(result.quitCalls(), 1)
  assert.deepEqual(result.exits, [], 'primary must use app.quit so backend cleanup can run')
})

test('window-all-closed cannot skip the asynchronous cleanup of a real quit', async () => {
  const result = loadMain()
  let prevented = false
  result.app.emit('before-quit', { preventDefault() { prevented = true } })
  assert.equal(prevented, true)
  result.app.emit('window-all-closed')
  assert.equal(result.quitCalls(), 0, 'must wait for the cleanup continuation')
  await new Promise((done) => setImmediate(done))
  assert.equal(result.quitCalls(), 1)
})
