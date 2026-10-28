import { startApplication } from './runtimeConfig.js'

startApplication({
  onConfigLoaded() {
    const message = document.querySelector('.startup-loader__message')
    if (message) message.textContent = '正在准备应用…'
  },
}).catch(error => {
  const app = document.getElementById('app')
  app.setAttribute('role', 'alert')
  app.textContent = `启动失败：${error.message}。请检查 config.json 后刷新页面。`
})
