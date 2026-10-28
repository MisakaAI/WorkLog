import vue from '@vitejs/plugin-vue'
import { defineConfig } from 'vite'
import { fileURLToPath } from 'node:url'

// 仅替换 HTML 语言服务的 worker 桥接入口，避免其隐式注册全量编辑功能。
function monacoHTMLWorkerBridge() {
  const bridge = fileURLToPath(new URL('./src/monacoWorkers.js', import.meta.url))
  return {
    name: 'monaco-html-worker-bridge',
    resolveId(source, importer) {
      if (importer?.replaceAll('\\', '/').endsWith('/monaco-editor/esm/vs/languages/features/html/workerManager.js')
        && source === '../../../internal/common/workers.js') return bridge
    },
  }
}

// https://vite.dev/config/
export default defineConfig(({ command, isPreview }) => ({
  base: './',
  // 开发模式默认纯前端运行：config.json 由 Vite 从 public/ 提供（{}），数据保存到浏览器。
  // 仅显式配置了后端地址时才会请求后端；API 代理可供开发联调使用。
  // preview 不继承开发代理，独立静态发布默认本地模式。
  server: {
    proxy: command === 'serve' && !isPreview ? {
      '/api': { target: 'http://127.0.0.1:12450', changeOrigin: false },
    } : undefined,
  },
  plugins: [vue(), { ...monacoHTMLWorkerBridge(), enforce: 'pre' }],
  // 开发预构建也必须使用相同桥接，避免 HTML 懒加载后重新引入全量功能。
  optimizeDeps: {
    rolldownOptions: { plugins: [monacoHTMLWorkerBridge()] },
  },
}))
