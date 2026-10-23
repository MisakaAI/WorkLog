import { editor } from 'monaco-editor/editor/editor.api.js'

// Monaco 0.57 的 internal/common/workers.js 附带全量功能注册。
// HTML 语言服务只需要 worker 桥接；功能由 monacoFeatures.js 统一按需注册。
export function createWebWorker({ label, createData, host, keepIdleModels }) {
  const worker = Promise.resolve(
    globalThis.MonacoEnvironment.getWorker('workerMain.js', label ?? 'monaco-editor-worker'),
  ).then(instance => {
    // 保持 Monaco SimpleWorker 的握手顺序：忽略首条消息，再传入语言服务配置。
    instance.postMessage('ignore')
    instance.postMessage(createData)
    return instance
  })
  return editor.createWebWorker({ worker, host, keepIdleModels })
}
