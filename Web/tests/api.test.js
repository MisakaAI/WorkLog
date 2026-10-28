import { test } from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync, readdirSync } from 'node:fs'
import { getDashboard, manageCategories, saveDashboard, emptyDashboard, DASHBOARD_KEY } from '../src/api/index.js'
import { getBackendURL, requestJSON } from '../src/api/client.js'
import { replaceProjects } from '../src/dashboard.js'

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
  const saved = await saveDashboard({ ...initial, personalLabel: '自己的工作台' })
  assert.equal(saved.revision, 1)
  assert.deepEqual(await getDashboard(), saved)
  await assert.rejects(saveDashboard(initial), /其他页面/)
  assert.deepEqual(await getDashboard(), saved)
  storage.set(DASHBOARD_KEY, '{invalid')
  await assert.rejects(getDashboard())
  assert.equal(storage.get(DASHBOARD_KEY), '{invalid')
})

test('本地归档默认隐藏，普通保存不丢失归档任务，可恢复和删除', async t => {
  setup(t)
  const initial = { ...emptyDashboard(), revision: 0, projects: [{ id: 'p', name: '项目', archived: true }], tasks: [{ id: 't', title: '任务', description: '', projectId: 'p', status: 'planned', tag: '开发', priority: '中', startDate: '', date: '', color: 'white' }] }
  const archived = await saveDashboard(initial)
  assert.equal(archived.projects.length, 0)
  assert.equal(archived.tasks.length, 0)
  await saveDashboard({ ...archived, personalLabel: '新文字' })
  const full = await getDashboard({ includeArchived: true })
  assert.equal(full.projects[0].archived, true)
  assert.equal(full.tasks.length, 1)
  full.projects[0].archived = false
  await saveDashboard(full)
  assert.equal((await getDashboard()).tasks.length, 1)
  const restored = await getDashboard({ includeArchived: true })
  restored.projects[0].archived = true
  await saveDashboard(restored)
  const deleted = replaceProjects(await getDashboard({ includeArchived: true }), [])
  await saveDashboard(deleted)
  assert.equal((await getDashboard()).projects.length, 0)
  assert.deepEqual((await getDashboard()).tasks, [])
})

test('本地分类重命名、交换和删除覆盖归档任务，旧快照不能覆盖新数据', async t => {
  setup(t)
  const task = { title: '任务', description: '', status: 'planned', priority: '中', startDate: '', date: '', color: 'white' }
  let snapshot = await saveDashboard({ ...emptyDashboard(), revision: 0, categories: ['开发', '设计'], projects: [{ id: 'a', name: '归档', archived: true }], tasks: [
    { ...task, id: 'visible', projectId: '', tag: '开发' },
    { ...task, id: 'hidden', projectId: 'a', tag: '设计' },
  ] })
  const stale = snapshot
  snapshot = await manageCategories(snapshot, [{ originalName: '开发', name: '开发' }, { originalName: '设计', name: ' 视觉设计 ' }])
  assert.equal(snapshot.tasks.length, 1)
  assert.deepEqual((await getDashboard({ includeArchived: true })).tasks.map(task => task.tag), ['开发', '视觉设计'])
  snapshot = await manageCategories(snapshot, [{ originalName: '开发', name: '视觉设计' }, { originalName: '视觉设计', name: '开发' }])
  assert.deepEqual((await getDashboard({ includeArchived: true })).tasks.map(task => task.tag), ['视觉设计', '开发'])
  snapshot = await manageCategories(snapshot, [{ originalName: '视觉设计', name: '保留' }])
  assert.deepEqual((await getDashboard({ includeArchived: true })).tasks.map(task => task.tag), ['保留', '保留'])
  await assert.rejects(manageCategories(stale, [{ originalName: '开发', name: '旧修改' }]), /其他页面/)
  assert.deepEqual(await getDashboard(), snapshot)
})

test('本地普通保存保留归档位置，可见项目排序、增删和恢复后顺序一致', async t => {
  setup(t)
  const archived = id => ({ id, name: id, archived: true })
  const active = id => ({ id, name: id })
  let snapshot = await saveDashboard({ ...emptyDashboard(), revision: 0, includeArchived: true, projects: [archived('a'), active('b'), archived('c'), active('d'), archived('e')] })
  snapshot = await saveDashboard({ ...await getDashboard(), personalLabel: '新标题' })
  const ids = async () => (await getDashboard({ includeArchived: true })).projects.map(project => project.id)
  assert.deepEqual(await ids(), ['a', 'b', 'c', 'd', 'e'])
  snapshot = await saveDashboard({ ...snapshot, projects: [active('d'), active('b'), active('f')] })
  assert.deepEqual(await ids(), ['a', 'd', 'c', 'b', 'e', 'f'])
  await saveDashboard({ ...snapshot, projects: [active('b'), active('f')] })
  assert.deepEqual(await ids(), ['a', 'b', 'c', 'f', 'e'])
  const full = await getDashboard({ includeArchived: true })
  full.projects.forEach(project => { project.archived = false })
  const restored = await saveDashboard(full)
  assert.deepEqual(restored.projects.map(project => project.id), ['a', 'b', 'c', 'f', 'e'])
})

test('远端分类管理提交集合版本和旧名映射，成功后读取最新快照，不回写旧快照', async t => {
  setup(t)
  globalThis.WORKLOG_CONFIG = { apiBaseUrl: '/api' }
  const initial = { ...emptyDashboard(), revision: 3 }
  const latest = { ...initial, revision: 5, categories: ['设计', '开发'], todos: [{ id: 'new', title: '并发新增', done: false }] }
  const calls = []
  t.mock.method(globalThis, 'fetch', async (url, options) => {
    calls.push([url, options.method, options.body && JSON.parse(options.body)])
    if (url === '/api/categories') return { ok: true, json: async () => ({ revision: 3, categoryRevision: 2 }) }
    assert.equal(url, '/api/dashboard')
    return { ok: true, json: async () => latest }
  })
  const categories = [{ originalName: '开发', name: '设计' }, { originalName: '设计', name: '开发' }]
  assert.deepEqual(await manageCategories(initial, categories), latest)
  assert.deepEqual(calls, [
    ['/api/categories', 'GET', undefined],
    ['/api/categories', 'PUT', { categoryRevision: 2, categories }],
    ['/api/dashboard', 'GET', undefined],
  ])
})

test('远端分类管理拒绝过期快照和并发集合冲突，不降级或重试写入', async t => {
  setup(t)
  globalThis.WORKLOG_CONFIG = { apiBaseUrl: '/api' }
  const snapshot = { ...emptyDashboard(), revision: 3 }
  const categories = [{ originalName: '开发', name: '研发' }]
  const calls = []
  t.mock.method(globalThis, 'fetch', async (url, options) => {
    calls.push(options.method)
    return { ok: true, json: async () => ({ revision: 4, categoryRevision: 2 }) }
  })
  await assert.rejects(manageCategories(snapshot, categories), /已被修改/)
  assert.deepEqual(calls, ['GET'])
  globalThis.fetch = async (url, options) => {
    calls.push(options.method)
    if (options.method === 'GET') return { ok: true, json: async () => ({ revision: 3, categoryRevision: 2 }) }
    return { ok: false, status: 409, json: async () => ({ error: { code: 'VERSION_CONFLICT', message: '集合已变化' } }) }
  }
  await assert.rejects(manageCategories(snapshot, categories), error => error.status === 409)
  assert.deepEqual(calls, ['GET', 'GET', 'PUT'])
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
  assert.match(html, /src="\/src\/bootstrap\.js"/)
  assert.doesNotMatch(html, /src="\.\/config\.js"|src="\/src\/main\.js"/)
  assert.match(readFileSync(new URL('../vite.config.js', import.meta.url), 'utf8'), /base: '\.\/'/)
})
