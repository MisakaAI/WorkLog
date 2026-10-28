import { createApp } from 'vue'
import './style.css'
import './theme.css'
import App from './App.vue'

// 资源可提前下载和执行；配置校验完成前不能挂载、读取业务数据或启动保存。
export function mountApplication() {
  return createApp(App).mount('#app')
}
