'use strict'

const EXIT_ARGUMENT = '--worklog-desktop-quit'

function launchAction(argv) {
  return argv.includes(EXIT_ARGUMENT) ? 'quit' : 'show'
}

function registerTaskbarExit(app, executable) {
  // A Windows taskbar Jump List task launches a short-lived second instance.
  // The single-instance lock forwards its intent to the running main process.
  const installed = app.setUserTasks([{
    program: executable,
    arguments: EXIT_ARGUMENT,
    iconPath: executable,
    iconIndex: 0,
    title: '退出 Worklog',
    description: '退出 Worklog 并关闭后台服务；请先等待保存完成。',
  }])
  if (!installed) {
    throw new Error('无法添加任务栏的“退出 Worklog”命令。请检查 Windows 任务列表是否被系统策略禁用。')
  }
}

function minimizeOnClose(window, isQuitting) {
  window.on('close', (event) => {
    if (isQuitting()) return
    event.preventDefault()
    // Do not hide the window: keeping it on the taskbar lets Windows restore it
    // through a normal taskbar click, without a tray icon or renderer changes.
    window.minimize()
  })
}

function handleSecondInstance(app, window, action, isQuitting) {
  if (isQuitting()) return
  if (action === 'quit') {
    app.quit()
    return
  }
  if (!window || window.isDestroyed()) return
  if (window.isMinimized()) window.restore()
  window.show()
  window.focus()
}

module.exports = { EXIT_ARGUMENT, launchAction, registerTaskbarExit, minimizeOnClose, handleSecondInstance }
