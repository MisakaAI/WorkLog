import { test } from 'node:test'
import assert from 'node:assert/strict'
import { moveTaskToLane, filterTasks, parseDashboard } from '../src/dashboard.js'
import { readFileSync } from 'node:fs'

const base = JSON.parse(readFileSync(new URL('./fixtures/dashboard.json', import.meta.url))).tasks[0]
function fixture(status = 'planned') {
  return { projects: [{ id: 'a', name: 'A' }], todos: [], tasks: [
    { ...base, id: 1, projectId: 'a', status, startDate: '', date: '' },
    { ...base, id: 'other', projectId: '', status },
    { ...base, id: '2', projectId: 'a', status },
    { ...base, id: 3, projectId: 'a', status },
  ] }
}
const order = data => data.tasks.filter(task => task.projectId === 'a').map(task => task.id)

for (const status of ['planned', 'progress', 'done']) {
  test(`${status} 列支持上下拖动、首尾插入，保存重读保留顺序且不改日期`, () => {
    const data = fixture(status)
    assert.equal(moveTaskToLane(data, '3', status, 'a', { targetId: 1, position: 'before' }), true)
    assert.deepEqual(order(data), [3, 1, '2'])
    assert.equal(moveTaskToLane(data, '3', status, 'a', { targetId: '2', position: 'after' }), true)
    assert.deepEqual(order(data), [1, '2', 3])
    assert.equal(moveTaskToLane(data, '1', status, 'a', { targetId: null }), true)
    assert.deepEqual(order(data), ['2', 3, 1])
    assert.equal(data.tasks.find(task => task.id === 1).startDate, '')
    assert.equal(data.tasks.find(task => task.id === 1).date, '')
    assert.equal(data.tasks.find(task => task.id === 'other').projectId, '')
    assert.deepEqual(order(parseDashboard(JSON.parse(JSON.stringify(data)))), ['2', 3, 1])
  })
}

test('相邻原位、自身、无效目标不修改数据', () => {
  const data = fixture()
  const original = structuredClone(data)
  for (const placement of [
    { targetId: 1, position: 'before' }, { targetId: '2', position: 'before' },
    { targetId: 'missing', position: 'after' }, { targetId: 'other', position: 'after' },
    { targetId: '2', position: 'invalid' },
  ]) assert.equal(moveTaskToLane(data, 1, 'planned', 'a', placement), false)
  assert.equal(moveTaskToLane(data, 3, 'planned', 'a', { targetId: null }), false)
  assert.deepEqual(data, original)
})

test('筛选后以目标 ID 排序，未显示任务不丢失且相对顺序不变', () => {
  const data = fixture()
  data.tasks[0].title = '匹配 A'
  data.tasks[3].title = '匹配 B'
  assert.deepEqual(filterTasks(data.tasks, { search: '匹配' }).map(task => task.id), [1, 3])
  moveTaskToLane(data, 3, 'planned', 'a', { targetId: 1, position: 'before' })
  assert.deepEqual(order(data), [3, 1, '2'])
  assert.deepEqual(data.tasks.filter(task => ![1, 3].includes(task.id)).map(task => task.id), ['other', '2'])
})

test('跨列可指定插入位置，空列可接收任务并保留日期流转规则', () => {
  const data = fixture()
  data.tasks[2].status = 'progress'
  assert.equal(moveTaskToLane(data, 1, 'progress', 'a', { targetId: '2', position: 'after' }), true)
  assert.ok(data.tasks.find(task => task.id === 1).startDate)
  assert.equal(moveTaskToLane(data, 1, 'done', '', { targetId: null }), true)
  assert.ok(data.tasks.find(task => task.id === 1).date)
})
