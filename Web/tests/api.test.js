import { test } from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync, readdirSync } from 'node:fs'
import { getDashboard, saveDashboard, getWorkCalendar, emptyDashboard, emptyCalendar, DASHBOARD_KEY } from '../src/api/index.js'
import { getBackendURL, requestJSON } from '../src/api/client.js'

function setup(t) {
  const storage = new Map()
  const original = Object.getOwnPropertyDescriptor(globalThis, 'localStorage')
  Object.defineProperty(globalThis, 'localStorage', { configurable: true, value: {
    getItem: key => storage.get(key) ?? null,
    setItem: (key, value) => storage.set(key, value),
  } })
  t.after(() => {
    if (original) Object.defineProperty(globalThis, 'localStorage', original)
    else delete globalThis.localStorage
    delete globalThis.WORKLOG_CONFIG
  })
  return storage
}

test('本地模式不调用 fetch，不加载旧示例缓存，保存后重新读取', async t => {
  const storage = setup(t)
  storage.set('worklog-dashboard', '{old demo}')
  t.mock.method(globalThis, 'fetch', () => { throw new Error('不应有网络请求') })
  const initial = await getDashboard()
  assert.deepEqual(initial, { ...emptyDashboard(), revision: 0 })
  assert.deepEqual(await getWorkCalendar(), emptyCalendar())
  const saved = await saveDashboard({ ...initial, personalLabel: '自己的工作台' })
  assert.equal(saved.revision, 1)
  assert.deepEqual(await getDashboard(), saved)
  await assert.rejects(saveDashboard(initial), /其他页面/)
  assert.deepEqual(await getDashboard(), saved)
  storage.set(DASHBOARD_KEY, '{invalid')
  await assert.rejects(getDashboard())
  assert.equal(storage.get(DASHBOARD_KEY), '{invalid')
})

test('远端模式不读取/写入本地数据，快照 GET/PUT 路径与版本一致', async t => {
  setup(t)
  globalThis.WORKLOG_CONFIG = { apiBaseUrl: '/services/api/' }
  globalThis.localStorage.getItem = globalThis.localStorage.setItem = () => { throw new Error('远端不应访问本地数据') }
  const snapshot = { ...emptyDashboard(), revision: 3 }
  t.mock.method(globalThis, 'fetch', async (url, options) => {
    assert.equal(url, '/services/api/dashboard')
    assert.equal(options.cache, 'no-store')
    if (options.method === 'PUT') {
      assert.deepEqual(JSON.parse(options.body), snapshot)
      return { ok: true, json: async () => ({ ...snapshot, revision: 4 }) }
    }
    return { ok: true, json: async () => snapshot }
  })
  assert.deepEqual(await getDashboard(), snapshot)
  assert.equal((await saveDashboard(snapshot)).revision, 4)
  globalThis.fetch = async () => ({ ok: false, status: 503, json: async () => ({ error: { message: '维护中' } }) })
  await assert.rejects(getDashboard(), /维护中/)
  await assert.rejects(saveDashboard(snapshot), /维护中/)
})

test('配置非法、后端响应非法、HTTP错误及超时均不降级本地', async t => {
  setup(t)
  for (const apiBaseUrl of ['ftp://host', '//host/api', 'relative', 123]) {
    globalThis.WORKLOG_CONFIG = { apiBaseUrl }
    assert.throws(getBackendURL, /后端 URL/)
  }
  globalThis.WORKLOG_CONFIG = { apiBaseUrl: '/api' }
  t.mock.method(globalThis, 'fetch', async () => ({ ok: true, json: async () => ({ ...emptyDashboard(), revision: -1 }) }))
  await assert.rejects(getDashboard(), /版本格式/)
  globalThis.fetch = async () => ({ ok: false, status: 500, json: async () => { throw new Error('HTML') } })
  await assert.rejects(requestJSON('/dashboard'), /HTTP 500/)
  globalThis.fetch = async () => { throw new DOMException('aborted', 'AbortError') }
  await assert.rejects(requestJSON('/dashboard'), /超时/)
})

test('HTTP 错误保留 code、status、requestId，供冲突处理与排障', async t => {
  setup(t)
  globalThis.WORKLOG_CONFIG = { apiBaseUrl: '/api' }
  t.mock.method(globalThis, 'fetch', async () => ({ ok: false, status: 409, json: async () => ({ error: { code: 'VERSION_CONFLICT', message: '冲突' }, requestId: 'req_123' }) }))
  await assert.rejects(requestJSON('/dashboard'), error => error.status === 409 && error.code === 'VERSION_CONFLICT' && error.requestId === 'req_123')
})

test('发布目录没有示例 API 文件，支持运行时配置与相对子路径构建', () => {
  assert.ok(!readdirSync(new URL('../public/', import.meta.url)).includes('api'))
  const html = readFileSync(new URL('../index.html', import.meta.url), 'utf8')
  assert.ok(html.indexOf('src="./config.js"') < html.indexOf('src="/src/main.js"'))
  assert.match(readFileSync(new URL('../vite.config.js', import.meta.url), 'utf8'), /base: '\.\/'/)
})
