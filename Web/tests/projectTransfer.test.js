import { test } from 'node:test'
import assert from 'node:assert/strict'
import JSZip from 'jszip'
import { projectBundle, exportProjects, importProjects } from '../src/api/projectTransfer.js'
import { emptyDashboard, DASHBOARD_KEY } from '../src/api/index.js'

const snapshot = {
  ...emptyDashboard(), revision: 4,
  projects: [{ id: 'a', name: '进行中', archived: false }, { id: 'b', name: '历史', archived: true }],
  tasks: ['a', 'b', ''].map((projectId, i) => ({ id: `t${i}`, projectId, title: '任务', description: '', status: 'planned', tag: '开发', priority: '中', startDate: '', date: '', color: 'white' })),
}

test('交换格式包含所有项目或单个项目，归档不遗漏且不包含无关数据', () => {
  const all = projectBundle(snapshot)
  assert.equal(all.format, 'worklog-projects')
  assert.equal(all.version, 1)
  assert.equal(all.projects.length, 2)
  assert.equal(all.tasks.length, 2)
  assert.equal(all.projects[1].archived, true)
  assert.deepEqual(projectBundle(snapshot, 'b').tasks, [snapshot.tasks[1]])
  assert.throws(() => projectBundle(snapshot, 'missing'), /尚未保存/)
  for (const key of ['revision', 'personalLabel', 'todos']) assert.equal(key in all, false)
})

test('本地导出从完整浏览器数据生成可解压的 JSON ZIP，不请求后端', async t => {
  const original = Object.getOwnPropertyDescriptor(globalThis, 'localStorage')
  Object.defineProperty(globalThis, 'localStorage', { configurable: true, value: { getItem(key) { assert.equal(key, DASHBOARD_KEY); return JSON.stringify(snapshot) } } })
  t.after(() => { if (original) Object.defineProperty(globalThis, 'localStorage', original); else delete globalThis.localStorage })
  t.mock.method(globalThis, 'fetch', () => { throw new Error('不能请求网络') })
  const blob = await exportProjects()
  const zip = await JSZip.loadAsync(await blob.arrayBuffer())
  assert.deepEqual(Object.keys(zip.files), ['projects.json'])
  assert.deepEqual(JSON.parse(await zip.file('projects.json').async('string')), projectBundle(snapshot))
  await assert.rejects(importProjects(new Blob()), /本地模式不支持/)
})

test('后端模式直接下载服务端 ZIP、原样上传 ZIP，不访问浏览器缓存', async t => {
  globalThis.WORKLOG_CONFIG = { apiBaseUrl: '/api' }
  t.after(() => { delete globalThis.WORKLOG_CONFIG })
  const zip = new Blob(['zip'], { type: 'application/zip' })
  t.mock.method(globalThis, 'fetch', async (url, options) => {
    assert.equal(url, '/api/projects/export?projectId=a%2Fb')
    assert.equal(options.headers.Accept, 'application/zip')
    return { ok: true, blob: async () => zip }
  })
  assert.equal(await exportProjects('a/b'), zip)
  globalThis.fetch = async (url, options) => {
    assert.equal(url, '/api/projects/import')
    assert.equal(options.method, 'POST')
    assert.equal(options.body, zip)
    assert.equal(options.headers['Content-Type'], 'application/zip')
    return { ok: true, json: async () => snapshot }
  }
  assert.deepEqual(await importProjects(zip), snapshot)
  await assert.rejects(importProjects({ size: 11 * 1024 * 1024 }), /不能超过/)
})
