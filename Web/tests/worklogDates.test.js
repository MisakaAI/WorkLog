import { test } from 'node:test'
import assert from 'node:assert/strict'
import { getWorklogDates, hasWorklogContent } from '../src/api/worklog.js'
import { STORAGE_PREFIX } from '../src/api/worklog.js'
import dayjs from 'dayjs'
import 'dayjs/locale/zh-cn.js'

const empty = { type: 'doc', content: [{ type: 'paragraph' }] }
const content = { type: 'doc', content: [{ type: 'paragraph', content: [{ type: 'text', text: '日志' }] }] }

test('日志标记忽略空段落和空白文字，支持文字及分隔线', () => {
  assert.equal(hasWorklogContent(empty), false)
  assert.equal(hasWorklogContent(null), false)
  assert.equal(hasWorklogContent({ type: 'doc', content: [{ type: 'text', text: '  ' }] }), false)
  assert.equal(hasWorklogContent(content), true)
  assert.equal(hasWorklogContent({ type: 'doc', content: [{ type: 'horizontalRule' }] }), true)
})

test('日期标记仅扫描新命名空间的本地数据，空日志不标记', async t => {
  t.mock.method(globalThis, 'fetch', () => { throw new Error('不应读取示例文件') })
  const entries = new Map([
    [`${STORAGE_PREFIX}2025-01-01`, JSON.stringify({ date: '2025-01-01', content: empty, version: 1, updatedAt: null })],
    [`${STORAGE_PREFIX}2025-01-02`, JSON.stringify({ date: '2025-01-02', content, version: 1, updatedAt: null })],
    ['worklog.entry.2025-01-03', 'invalid'],
    ['other', '{}'],
  ])
  const original = Object.getOwnPropertyDescriptor(globalThis, 'localStorage')
  Object.defineProperty(globalThis, 'localStorage', { configurable: true, value: {
    length: entries.size,
    key: i => [...entries.keys()][i],
    getItem: key => entries.get(key) ?? null,
  } })
  t.after(() => {
    if (original) Object.defineProperty(globalThis, 'localStorage', original)
    else delete globalThis.localStorage
  })
  assert.deepEqual(new Set(await getWorklogDates()), new Set(['2025-01-02']))
})

test('中文日期面板使用周一作为每周第一天', () => {
  assert.equal(dayjs().locale('zh-cn').$locale().weekStart, 1)
})
