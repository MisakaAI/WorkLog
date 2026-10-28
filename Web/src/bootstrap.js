import { startApplication } from './runtimeConfig.js'

startApplication().catch(error => {
  const app = document.getElementById('app')
  app.setAttribute('role', 'alert')
  app.textContent = `启动失败：${error.message}。请检查 config.json 后刷新页面。`
})
