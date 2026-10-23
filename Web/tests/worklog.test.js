import { test } from 'node:test'
import assert from 'node:assert/strict'
import { todayISO } from '../src/dashboard.js'
import { getTodayWorklog, getWorklog, saveWorklog, requestWorklog, getWorklogDates, STORAGE_PREFIX } from '../src/api/worklog.js'

const empty = { type: 'doc', content: [{ type: 'paragraph' }] }
const content = { type: 'doc', content: [{ type: 'paragraph', content: [{ type: 'text', text: '自己的日志' }] }] }
function setup(t) {
  const storage = new Map()
  const original = Object.getOwnPropertyDescriptor(globalThis, 'localStorage')
  Object.defineProperty(globalThis, 'localStorage', { configurable: true, value: {
    get length() { return storage.size }, key: i => [...storage.keys()][i],
    getItem: key => storage.get(key) ?? null, setItem: (key, value) => storage.set(key, value),
  } })
  t.after(() => {
    if (original) Object.defineProperty(globalThis, 'localStorage', original)
    else delete globalThis.localStorage
    delete globalThis.WORKLOG_CONFIG
  })
  return storage
}

test('本地日志从空白开始，无网络依赖；保存、读取、清空标记与版本冲突', async t => {
  const storage = setup(t)
  t.mock.method(globalThis, 'fetch', () => { throw new Error('本地模式不应请求网络') })
  storage.set('worklog.entry.2025-01-01', JSON.stringify({ content }))
  const today = await getTodayWorklog()
  assert.equal(today.date, todayISO())
  assert.deepEqual(today.content, empty)
  assert.deepEqual(await getWorklogDates(), [])
  await getWorklog('2025-01-01')
  const saved = await saveWorklog('2025-01-01', content, 0)
  assert.equal(saved.version, 1)
  assert.ok(saved.updatedAt)
  assert.deepEqual(await getWorklog('2025-01-01'), saved)
  assert.deepEqual(await getWorklogDates(), ['2025-01-01'])
  await saveWorklog('2025-01-01', empty, saved.version)
  assert.deepEqual(await getWorklogDates(), [])
  storage.set(`${STORAGE_PREFIX}2025-01-01`, JSON.stringify({ ...saved, version: 9 }))
  await assert.rejects(saveWorklog('2025-01-01', content, 2), /其他页面/)
  await getWorklog('2025-01-01')
  await assert.rejects(saveWorklog('2025-01-01', content, 2), /其他页面/)
  await assert.rejects(getWorklog('2026-02-30'), /日期无效/)
  await assert.rejects(requestWorklog('/api/worklog/2025-01-01', { method: 'PUSH' }), /不支持/)
  await assert.rejects(saveWorklog('2025-01-01', {}), /内容无效/)
})

test('本地损坏记录和不可写存储不会回退示例或伪装保存成功', async t => {
  const storage = setup(t)
  storage.set(`${STORAGE_PREFIX}2025-02-01`, '{broken')
  await assert.rejects(getWorklog('2025-02-01'))
  await getWorklog('2025-02-02')
  await assert.rejects(saveWorklog('2025-02-02', content), /草稿对应的日志版本/)
  globalThis.localStorage.setItem = () => { throw new Error('空间不足') }
  await assert.rejects(saveWorklog('2025-02-02', content, 0), /空间不足/)
})

test('远端日志使用标准 PUT、提交版本，日期接口无静态文件依赖', async t => {
  setup(t)
  globalThis.WORKLOG_CONFIG = { apiBaseUrl: 'https://backend.test/api/' }
  let version = 0
  const calls = []
  t.mock.method(globalThis, 'fetch', async (url, options) => {
    calls.push([url, options.method])
    if (url.endsWith('/worklog/dates')) return { ok: true, json: async () => ({ dates: ['2025-03-01'] }) }
    if (options.method === 'PUT') {
      assert.deepEqual(JSON.parse(options.body), { content, version })
      version++
    }
    return { ok: true, json: async () => ({ date: '2025-03-01', content: version ? content : empty, updatedAt: version ? new Date().toISOString() : null, version }) }
  })
  await getWorklog('2025-03-01')
  assert.equal((await saveWorklog('2025-03-01', content, version)).version, 1)
  assert.deepEqual(await getWorklogDates(), ['2025-03-01'])
  assert.deepEqual(calls, [
    ['https://backend.test/api/worklog/2025-03-01', 'GET'],
    ['https://backend.test/api/worklog/2025-03-01', 'PUT'],
    ['https://backend.test/api/worklog/dates', 'GET'],
  ])
  globalThis.localStorage.getItem = () => { throw new Error('不应访问本地存储') }
  globalThis.fetch = async () => ({ ok: false, status: 409, json: async () => ({ error: { message: '版本冲突' } }) })
  await assert.rejects(saveWorklog('2025-03-01', content, version), /版本冲突/)
})
