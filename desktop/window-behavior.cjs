'use strict'

const EXIT_ARGUMENT = '--worklog-desktop-quit'

function launchAction(argv) {
  return argv.includes(EXIT_ARGUMENT) ? 'quit' : 'show'
}

function showWindow(window, isQuitting) {
  if (isQuitting() || !window || window.isDestroyed()) return
  if (window.isMinimized()) window.restore()
  window.show()
  window.focus()
}

function createTray({ app, Tray, Menu, nativeImage, iconPath, getWindow, isQuitting }) {
  const icon = nativeImage.createFromPath(iconPath)
  if (icon.isEmpty()) throw new Error('无法加载 Worklog 托盘图标，请重新安装应用。')
  const tray = new Tray(icon)
  try {
    const show = () => showWindow(getWindow(), isQuitting)
    tray.setToolTip('Worklog')
    tray.setContextMenu(Menu.buildFromTemplate([
      { label: '显示主界面', click: show },
      { type: 'separator' },
      { label: '退出 Worklog', click: () => { if (!isQuitting()) app.quit() } },
    ]))
    tray.on('click', show)
    return tray
  } catch (error) {
    tray.destroy()
    throw error
  }
}

function hideOnClose(window, isQuitting) {
  window.on('close', (event) => {
    if (isQuitting()) return
    event.preventDefault()
    // Hiding removes the taskbar button; normal minimize remains native.
    window.hide()
  })
}

function handleSecondInstance(app, window, action, isQuitting) {
  if (isQuitting()) return
  if (action === 'quit') {
    app.quit()
    return
  }
  showWindow(window, isQuitting)
}

module.exports = { EXIT_ARGUMENT, launchAction, createTray, hideOnClose, handleSecondInstance }
