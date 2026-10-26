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
export default defineConfig({
  base: './',
  // 开发时设置 VITE_API_BASE_URL=/api，保持与生产同源部署一致。
  server: {
    proxy: { '/api': { target: 'http://127.0.0.1:12450', changeOrigin: false } },
  },
  plugins: [vue(), { ...monacoHTMLWorkerBridge(), enforce: 'pre' }],
  // 开发预构建也必须使用相同桥接，避免 HTML 懒加载后重新引入全量功能。
  optimizeDeps: {
    rolldownOptions: { plugins: [monacoHTMLWorkerBridge()] },
  },
})
