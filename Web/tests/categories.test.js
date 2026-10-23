import { test } from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { tags, parseDashboard, replaceCategories, validateCategories, filterTasks } from '../src/dashboard.js'

const fixture = () => JSON.parse(readFileSync(new URL('./fixtures/dashboard.json', import.meta.url), 'utf8'))

test('分类校验：禁止空集合、空名、超长、忽略大小写重名和非法类型', () => {
  for (const categories of [[], [' '], ['x'.repeat(41)], [' Work ', 'work'], [null], '开发']) assert.ok(validateCategories(categories))
  assert.equal(validateCategories(['研发', '文档']), '')
})

test('缺省分类不改写数据，自定义分类保存重读，非法集合和失效分类被拒绝', () => {
  const data = fixture()
  assert.equal(parseDashboard(data), data)
  assert.equal(data.categories, undefined)
  const saved = replaceCategories(data, tags.map(name => ({ originalName: name, name: name === '开发' ? '研发' : name })))
  assert.deepEqual(parseDashboard(JSON.parse(JSON.stringify(saved))), saved)
  assert.ok(saved.categories.includes('研发'))
  assert.throws(() => parseDashboard({ ...data, categories: [] }), /数据格式/)
  assert.throws(() => parseDashboard({ ...data, categories: ['不存在'] }), /数据格式/)
})

test('分类改名、删除同步任务，新增可选，原数据不变，分类筛选正常', () => {
  const data = fixture()
  data.tasks = [
    { ...data.tasks[0], id: '1', tag: '开发' },
    { ...data.tasks[0], id: '2', tag: '设计' },
    { ...data.tasks[0], id: '3', tag: '临时' },
  ]
  const original = structuredClone(data)
  const saved = replaceCategories(data, [{ originalName: '开发', name: ' 研发 ' }, { originalName: '临时', name: '临时' }, { name: '文档' }])
  assert.deepEqual(saved.categories, ['研发', '临时', '文档'])
  assert.deepEqual(saved.tasks.map(task => task.tag), ['研发', '研发', '临时'])
  assert.deepEqual(filterTasks(saved.tasks, { tag: '研发' }).map(task => task.id), ['1', '2'])
  assert.deepEqual(data, original)
  assert.throws(() => replaceCategories(data, []), /至少保留/)
})

test('分类名称交换使用原名称映射，不会连续替换导致任务误分类', () => {
  const data = fixture()
  data.tasks = [{ ...data.tasks[0], id: '1', tag: '开发' }, { ...data.tasks[0], id: '2', tag: '设计' }]
  const saved = replaceCategories(data, [{ originalName: '开发', name: '设计' }, { originalName: '设计', name: '开发' }])
  assert.deepEqual(saved.tasks.map(task => task.tag), ['设计', '开发'])
  assert.equal(parseDashboard(saved), saved)
})
