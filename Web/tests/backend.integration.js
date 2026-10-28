// Explicit opt-in; run only against a disposable backend database.
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { randomUUID } from 'node:crypto'
import { JSDOM } from 'jsdom'
import { parseWorklogHTML } from '../src/worklogEditor.js'
import { getDashboard, manageCategories, saveDashboard } from '../src/api/index.js'
import { replaceProjects } from '../src/dashboard.js'
import { getTodayWorklog, getWorklog, saveWorklog, getWorklogDates } from '../src/api/worklog.js'
import { requestJSON } from '../src/api/client.js'

const url = process.env.WORKLOG_TEST_API_URL
if (!url) throw new Error('Set WORKLOG_TEST_API_URL to a disposable local backend /api URL')
globalThis.WORKLOG_CONFIG = { apiBaseUrl: url }
Object.defineProperty(globalThis, 'localStorage', { configurable: true, value: {
  getItem() { throw new Error('Remote mode must not read local storage') },
  setItem() { throw new Error('Remote mode must not write local storage') },
} })

test('real Vue API adapters: snapshot, granular concurrent write, log format/version/markers', async () => {
  const initial = await getDashboard()
  assert.equal(initial.tasks.length, 0, 'Use a fresh database, not personal data')
  const project = { id: randomUUID(), name: '真实接入项目' }
  const task = { id: randomUUID(), title: '真实接入任务', description: '', projectId: project.id, status: 'planned', tag: initial.categories[0], priority: '中', startDate: '', date: '', color: 'white' }
  const saved = await saveDashboard({ ...initial, projects: [project], tasks: [task], todos: [{ id: randomUUID(), title: '真实待办', done: false }] })
  assert.equal(saved.revision, initial.revision + 1)
  assert.deepEqual(await getDashboard(), saved)
  const created = await requestJSON('/tasks', { method: 'POST', body: { title: '并发 API 任务', idempotencyKey: randomUUID() } })
  assert.equal(created.version, 1)
  await assert.rejects(saveDashboard(saved), error => error.status === 409)
  const latest = await getDashboard()
  assert.equal(latest.tasks.length, 2, 'Conflict must not overwrite server changes')
  const deleted = await saveDashboard(replaceProjects(latest, []))
  assert.deepEqual(deleted.tasks.map(item => item.id), [created.id])
  assert.equal(deleted.tasks[0].projectId, '')
  const today = await getTodayWorklog()
  assert.match(today.date, /^\d{4}-\d{2}-\d{2}$/)
  const date = '2098-12-30'
  const empty = await getWorklog(date)
  assert.equal(empty.version, 0)
  assert.equal(empty.updatedAt, null)
  const content = { type: 'doc', content: [{ type: 'orderedList', attrs: { start: 3, type: null }, content: [{ type: 'listItem', content: [{ type: 'paragraph', content: [{ type: 'text', text: '格式往返', marks: [{ type: 'bold' }, { type: 'textStyle', attrs: { color: 'rgb(198, 40, 40)' } }] }] }] }] }] }
  const entry = await saveWorklog(date, content, empty.version)
  assert.equal(entry.version, 1)
  assert.deepEqual((await getWorklog(date)).content, content)
  assert.ok((await getWorklogDates()).includes(date))
  const cleared = await saveWorklog(date, { type: 'doc', content: [{ type: 'paragraph' }] }, entry.version)
  assert.equal(cleared.version, 2)
  assert.ok(!(await getWorklogDates()).includes(date))
})

test('HTML 导入结果可被真实后端保存和重新读取，颜色过滤不丢正文', async t => {
  const dom = new JSDOM('')
  for (const key of ['window', 'document', 'DOMParser']) {
    const original = Object.getOwnPropertyDescriptor(globalThis, key)
    Object.defineProperty(globalThis, key, { configurable: true, value: dom.window[key] })
    t.after(() => { if (original) Object.defineProperty(globalThis, key, original); else delete globalThis[key] })
  }
  t.after(() => dom.window.close())
  const date = '2098-12-29'
  const base = await getWorklog(date)
  let warned = false
  const content = parseWorklogHTML('<p><strong><span style="color: oklch(60% 0.2 25)">保留文字与粗体</span></strong><span style="color: #c62828">保留颜色</span></p>', { onUnsupportedColor: () => { warned = true } })
  assert.equal(warned, true)
  assert.ok(!JSON.stringify(content).includes('oklch'))
  const saved = await saveWorklog(date, content, base.version)
  // Tiptap attrs 使用无原型对象；HTTP JSON 往返只约束字段，不保留 JS 原型。
  const wireContent = JSON.parse(JSON.stringify(content))
  assert.deepEqual(saved.content, wireContent)
  assert.deepEqual((await getWorklog(date)).content, wireContent)
})

test('Go 接受的 Unicode 名称可经浏览器适配器保存和重新加载', async () => {
  const current = await getDashboard()
  const projects = ['ΟΣ', 'ος'].map(name => ({ id: randomUUID(), name }))
  const saved = await saveDashboard({ ...current, projects, categories: [...current.categories, 'ΟΣ', 'ος'] })
  assert.deepEqual(await getDashboard(), saved)
  await assert.rejects(requestJSON('/dashboard', { method: 'PUT', body: { ...saved, projects: [{ id: 'p1', name: 'İ' }, { id: 'p2', name: 'i' }] } }), error => error.status === 400)
})

test('真实分类适配器迁移归档任务，普通保存保留归档项目顺序和集合版本', async () => {
  const current = await getDashboard({ includeArchived: true })
  const project = { id: randomUUID(), name: '归档分类回归', archived: true }
  const task = { id: randomUUID(), title: '归档任务', description: '', projectId: project.id, status: 'planned', tag: current.categories[1], priority: '中', startDate: '', date: '', color: 'white' }
  await saveDashboard({ ...current, projects: [project, ...current.projects], tasks: [...current.tasks, task] })
  const before = await requestJSON('/projects')
  let snapshot = await saveDashboard({ ...await getDashboard(), personalLabel: '排序回归' })
  assert.equal((await requestJSON('/projects')).projectRevision, before.projectRevision)
  const rename = snapshot.categories.map(name => ({ originalName: name, name: name === task.tag ? '视觉设计' : name }))
  snapshot = await manageCategories(snapshot, rename)
  let full = await getDashboard({ includeArchived: true })
  assert.equal(full.projects[0].id, project.id)
  assert.equal(full.tasks.find(item => item.id === task.id).tag, '视觉设计')
  assert.ok(!snapshot.tasks.some(item => item.id === task.id))
  const first = snapshot.categories[0]
  snapshot = await manageCategories(snapshot, snapshot.categories.map(name => ({ originalName: name, name: name === first ? '视觉设计' : name === '视觉设计' ? first : name })))
  full = await getDashboard({ includeArchived: true })
  assert.equal(full.tasks.find(item => item.id === task.id).tag, first)
  assert.equal(full.projects[0].id, project.id)
  full.projects[0].archived = false
  const restored = await saveDashboard(full)
  assert.equal(restored.projects[0].id, project.id)
  assert.equal(restored.tasks.find(item => item.id === task.id).tag, first)
})
