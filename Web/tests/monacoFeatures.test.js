import { test } from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync, existsSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { runInNewContext } from 'node:vm'
import viteConfig from '../vite.config.js'

const config = viteConfig({ command: 'build', mode: 'production' })

const read = path => readFileSync(new URL(path, import.meta.url), 'utf8')
const features = read('../src/monacoFeatures.js')
const source = read('../src/components/SourceCodeEditor.vue')
const worklog = read('../src/components/WorklogEditor.vue')
const imports = [...features.matchAll(/^import '([^']+)'/gm)].map(match => match[1])

test('按需注册入口先加载核心服务，且所有 Monaco 注册路径均可解析', () => {
  assert.equal(imports[0], 'monaco-editor/features/codeEditor/register.js')
  assert.match(source, /import '\.\.\/monacoFeatures\.js'[\s\S]*import \* as monaco/)
  assert.doesNotMatch(`${features}\n${source}`, /import\s+['"][^'"]*register\.all\.js/)
  for (const specifier of imports) {
    assert.ok(existsSync(fileURLToPath(import.meta.resolve(specifier))), specifier)
  }
})

test('保留 HTML 编辑必需功能及弹出补全控制器，不注册高级编辑功能', () => {
  for (const feature of [
    'codicon', 'tokenization', 'bracketMatching', 'clipboard', 'comment', 'contextmenu',
    'find', 'folding', 'format', 'gotoLine', 'hover', 'indentation', 'linesOperations',
    'snippet', 'toggleTabFocusMode', 'wordOperations',
  ]) {
    assert.ok(imports.includes(`monaco-editor/features/${feature}/register.js`), feature)
  }
  assert.ok(imports.includes('monaco-editor/editor/contrib/suggest/browser/suggestController.js'))
  for (const feature of ['diffEditor', 'codelens', 'inlayHints', 'inlineCompletions', 'rename', 'suggest']) {
    assert.ok(!imports.includes(`monaco-editor/features/${feature}/register.js`), feature)
  }
})

test('源码组件仍为按需加载，HTML 语言及两个独立 worker 不受影响', () => {
  assert.match(worklog, /defineAsyncComponent\(\(\) => import\('\.\/SourceCodeEditor\.vue'\)\)/)
  assert.match(worklog, /<SourceCodeEditor v-if="sourceVisible"/)
  assert.match(source, /import 'monaco-editor\/languages\/definitions\/html\/register\.js'/)
  assert.match(source, /import 'monaco-editor\/language\/html\/monaco\.contribution\.js'/)
  assert.match(source, /editor\/editor\.worker\.js\?worker/)
  assert.match(source, /html\/html\.worker\.js\?worker/)
  assert.match(source, /\? new HTMLWorker\(\)\s*: new EditorWorker\(\)/)
})

test('生产构建和开发预构建只替换 Monaco HTML 管理器的内部 worker 入口', () => {
  const plugins = [
    config.plugins.find(plugin => plugin.name === 'monaco-html-worker-bridge'),
    config.optimizeDeps.rolldownOptions.plugins.find(plugin => plugin.name === 'monaco-html-worker-bridge'),
  ]
  const specifier = '../../../internal/common/workers.js'
  const importer = '/project/node_modules/monaco-editor/esm/vs/languages/features/html/workerManager.js'
  const expected = fileURLToPath(new URL('../src/monacoWorkers.js', import.meta.url))
  for (const plugin of plugins) {
    assert.equal(plugin.resolveId(specifier, importer), expected)
    assert.equal(plugin.resolveId(specifier, importer.replaceAll('/', '\\')), expected)
    assert.equal(plugin.resolveId(specifier, '/project/src/workerManager.js'), undefined)
    assert.equal(plugin.resolveId('./other.js', importer), undefined)
    assert.equal(plugin.resolveId(specifier), undefined)
  }
})

test('worker 桥接保留标签、异步 worker、握手顺序及模型同步配置', async () => {
  const bridge = read('../src/monacoWorkers.js')
    .replace(/^import .*\n/, '')
    .replace('export function createWebWorker', 'function createWebWorker')
  const requests = []
  const messages = []
  const createData = { languageId: 'html' }
  const host = {}
  let options
  const context = {
    MonacoEnvironment: {
      getWorker(moduleId, label) {
        requests.push([moduleId, label])
        return Promise.resolve({ postMessage: message => messages.push(message) })
      },
    },
    editor: { createWebWorker(value) { options = value; return 'client' } },
  }
  const createWebWorker = runInNewContext(`${bridge}\ncreateWebWorker`, context)
  assert.equal(createWebWorker({ label: 'html', createData, host, keepIdleModels: true }), 'client')
  await options.worker
  assert.deepEqual(requests, [['workerMain.js', 'html']])
  assert.deepEqual(messages, ['ignore', createData])
  assert.equal(options.host, host)
  assert.equal(options.keepIdleModels, true)
  createWebWorker({})
  await options.worker
  assert.equal(requests[1][1], 'monaco-editor-worker')
})
