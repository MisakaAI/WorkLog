import { test } from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync, existsSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { createServer as createHTTPServer } from 'node:http'
import { createServer } from 'vite'
import viteConfig from '../vite.config.js'
import { getBackendURL } from '../src/api/client.js'
import { loadRuntimeConfig, startApplication, validateRuntimeConfig } from '../src/runtimeConfig.js'

const configFile = fileURLToPath(new URL('../vite.config.js', import.meta.url))
const configURL = new URL('https://example.test/worklog/config.json')
const response = (value, status = 200) => ({ ok: status >= 200 && status < 300, status, json: async () => value })

function restoreConfig(t) {
  const original = Object.getOwnPropertyDescriptor(globalThis, 'WORKLOG_CONFIG')
  t.after(() => {
    if (original) Object.defineProperty(globalThis, 'WORKLOG_CONFIG', original)
    else delete globalThis.WORKLOG_CONFIG
  })
}

test('发布配置仅为 JSON，客户端只使用内部运行时载体，默认本地模式', t => {
  restoreConfig(t)
  delete globalThis.WORKLOG_CONFIG
  assert.equal(getBackendURL(), '')
  const config = JSON.parse(readFileSync(new URL('../public/config.json', import.meta.url), 'utf8'))
  assert.deepEqual(config, {})
  assert.equal(existsSync(new URL('../public/config.js', import.meta.url)), false)
  for (const value of [config, { businessTimezone: 'Asia/Shanghai' }, { apiBaseUrl: '' }]) {
    globalThis.WORKLOG_CONFIG = value
    assert.equal(getBackendURL(), '')
  }
  const client = readFileSync(new URL('../src/api/client.js', import.meta.url), 'utf8')
  assert.doesNotMatch(client, /import\.meta\.env|process\.env/)
})

test('开发模式默认本地运行且不代理配置；build 和 preview 不启用开发代理', () => {
  const dev = viteConfig({ command: 'serve', mode: 'development', isPreview: false })
  assert.equal(dev.server.proxy['/api'].target, 'http://127.0.0.1:12450')
  assert.equal(dev.server.proxy['/config.json'], undefined)
  for (const options of [
    { command: 'build', mode: 'production' },
    { command: 'build', mode: 'development' },
    { command: 'serve', mode: 'production', isPreview: true },
  ]) {
    const config = viteConfig(options)
    assert.equal(config.server.proxy, undefined)
  }
})

test('Vite 开发代理公开JSON与API，保留后端业务时区和禁缓存响应', async t => {
  const upstream = createHTTPServer((req, res) => {
    res.setHeader('Content-Type', 'application/json')
    res.setHeader('Cache-Control', 'private, no-store')
    res.end(JSON.stringify(req.url.startsWith('/config.json')
      ? { apiBaseUrl: '/api', businessTimezone: 'America/New_York' }
      : { path: req.url }))
  })
  await new Promise(resolve => upstream.listen(0, '127.0.0.1', resolve))
  t.after(() => new Promise(resolve => upstream.close(resolve)))
  const target = `http://127.0.0.1:${upstream.address().port}`
  const server = await createServer({ configFile, server: { host: '127.0.0.1', port: 0, open: false,
    proxy: { '/api': { target }, '/config.json': { target } },
  }, logLevel: 'error' })
  t.after(() => server.close())
  await server.listen()
  const port = server.httpServer.address().port
  for (const path of ['/config.json', '/config.json?t=123']) {
    const response = await fetch(`http://127.0.0.1:${port}${path}`)
    assert.equal(response.status, 200)
    assert.match(response.headers.get('content-type'), /application\/json/)
    assert.match(response.headers.get('cache-control'), /no-store/)
    assert.deepEqual(await response.json(), { apiBaseUrl: '/api', businessTimezone: 'America/New_York' })
  }
  const api = await fetch(`http://127.0.0.1:${port}/api/healthz`)
  assert.deepEqual(await api.json(), { path: '/api/healthz' })
})

test('公开配置严格校验，非法地址/时区/未知字段不能降级或注入', () => {
  for (const value of [null, [], '', { apiBaseUrl: null }, { apiBaseUrl: 1 }, { apiBaseUrl: 'relative' },
    { apiBaseUrl: '//host/api' }, { apiBaseUrl: 'javascript:alert(1)' }, { businessTimezone: '' },
    { businessTimezone: 1 }, { businessTimezone: 'Not/AZone' }, { write_token: 'secret' }]) {
    assert.throws(() => validateRuntimeConfig(value))
  }
  assert.deepEqual(validateRuntimeConfig({ apiBaseUrl: ' /api/ ', businessTimezone: 'Asia/Shanghai' }),
    { apiBaseUrl: '/api', businessTimezone: 'Asia/Shanghai' })
})

test('并行加载 JSON 和应用资源，配置就绪后才挂载；子目录、凭证与无缓存选项正确', async t => {
  restoreConfig(t)
  const originalDocument = Object.getOwnPropertyDescriptor(globalThis, 'document')
  Object.defineProperty(globalThis, 'document', { configurable: true, value: { baseURI: 'https://example.test/worklog/' } })
  t.after(() => {
    if (originalDocument) Object.defineProperty(globalThis, 'document', originalDocument)
    else delete globalThis.document
  })
  const events = []
  globalThis.WORKLOG_CONFIG = { apiBaseUrl: 'https://stale.test/api' }
  await startApplication({
    fetchImpl: async (url, options) => {
      events.push('fetch')
      assert.equal(url.href, configURL.href)
      assert.equal(options.credentials, 'same-origin')
      assert.equal(options.cache, 'no-store')
      return response({ apiBaseUrl: '/api', businessTimezone: 'Asia/Shanghai' })
    },
    loadApp: async () => {
      events.push('app')
      return { mountApplication() {
        events.push('mount')
        assert.equal(getBackendURL(), '/api')
        assert.equal(globalThis.WORKLOG_CONFIG.businessTimezone, 'Asia/Shanghai')
      } }
    },
  })
  assert.deepEqual(events, ['fetch', 'app', 'mount'])
})

for (const first of ['config', 'app']) {
  test(`${first} 先就绪：两条加载链路并行启动，但两者完成之前不挂载`, async t => {
    restoreConfig(t)
    globalThis.WORKLOG_CONFIG = { apiBaseUrl: '/original/api' }
    let resolveConfig, resolveApp
    const configReady = new Promise(resolve => { resolveConfig = resolve })
    const appReady = new Promise(resolve => { resolveApp = resolve })
    const events = []
    const startup = startApplication({
      configURL,
      fetchImpl: () => { events.push('fetch'); return configReady },
      loadApp: () => { events.push('app'); return appReady },
      onConfigLoaded: () => events.push('config-ready'),
    })
    // 给微任务队列一次执行机会，不依赖耗时断言。
    await new Promise(resolve => setImmediate(resolve))
    assert.deepEqual(events, ['fetch', 'app'])
    const releaseConfig = () => resolveConfig(response({ apiBaseUrl: '/api' }))
    const releaseApp = () => resolveApp({ mountApplication() {
      assert.equal(getBackendURL(), '/api')
      events.push('mount')
    } })
    if (first === 'config') releaseConfig()
    else releaseApp()
    await new Promise(resolve => setImmediate(resolve))
    assert.equal(events.includes('mount'), false)
    assert.equal(getBackendURL(), '/original/api')
    assert.equal(events.includes('config-ready'), first === 'config')
    if (first === 'config') releaseApp()
    else releaseConfig()
    await startup
    assert.deepEqual(events, ['fetch', 'app', 'config-ready', 'mount'])
  })
}

test('应用资源失败立即结束启动；随后配置失败不会产生未处理拒绝或挂载', async t => {
  restoreConfig(t)
  globalThis.WORKLOG_CONFIG = { apiBaseUrl: '/original/api' }
  let rejectConfig
  const configReady = new Promise((_resolve, reject) => { rejectConfig = reject })
  await assert.rejects(startApplication({ configURL,
    fetchImpl: () => configReady,
    loadApp: () => { throw new Error('资源加载失败') },
  }), /资源加载失败/)
  rejectConfig(new TypeError('network'))
  await new Promise(resolve => setImmediate(resolve))
  assert.equal(getBackendURL(), '/original/api')
})

test('配置失败不等待应用资源就报错，资源随后就绪也不会挂载', async t => {
  restoreConfig(t)
  let resolveApp
  let mounted = false
  const appReady = new Promise(resolve => { resolveApp = resolve })
  await assert.rejects(startApplication({ configURL,
    fetchImpl: async () => response(null, 403),
    loadApp: () => appReady,
  }), /HTTP 403/)
  resolveApp({ mountApplication() { mounted = true } })
  await new Promise(resolve => setImmediate(resolve))
  assert.equal(mounted, false)
})

test('缺失、网络失败及超时阻止挂载，不切换为本地模式', async t => {
  restoreConfig(t)
  for (const fetchImpl of [
    async () => response(null, 404), async () => response(null, 410),
    async () => { throw new TypeError('network') },
    (_url, { signal }) => new Promise((_resolve, reject) => signal.addEventListener('abort',
      () => reject(new DOMException('timeout', 'AbortError')), { once: true })),
  ]) {
    globalThis.WORKLOG_CONFIG = { apiBaseUrl: '/original/api' }
    let started = false
    await assert.rejects(startApplication({ configURL, fetchImpl, timeoutMs: 5,
      loadApp: async () => ({ mountApplication() { started = true } }),
    }), /config.json/)
    assert.equal(started, false)
    assert.equal(getBackendURL(), '/original/api')
  }
})

test('认证/服务端错误、JSON损坏及字段非法阻止启动，不静默切换数据源', async () => {
  for (const fetchImpl of [
    async () => response(null, 401), async () => response(null, 403), async () => response(null, 500),
    async () => ({ ok: true, json: async () => { throw new SyntaxError('bad JSON') } }),
    async () => response({ apiBaseUrl: 'bad' }), async () => response(null),
  ]) {
    let started = false
    await assert.rejects(startApplication({ configURL, fetchImpl,
      loadApp: async () => ({ mountApplication() { started = true } }),
    }))
    assert.equal(started, false)
  }
  // 直接加载器也返回独立配置，不在取回前挂载应用。
  assert.deepEqual(await loadRuntimeConfig({ configURL, fetchImpl: async () => response({}) }), {})
})
