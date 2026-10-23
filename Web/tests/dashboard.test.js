import { test } from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { columns, tags, priorities, cardColors, parseDashboard, filterTasks, dateToDay, dayToDate, validateDates, taskSchedule, ganttPlacement } from '../src/dashboard.js'

const initial = JSON.parse(readFileSync(new URL('./fixtures/dashboard.json', import.meta.url), 'utf8'))

function fixture(task = {}) {
  return { projects: [], tasks: [{ ...initial.tasks[0], projectId: '', ...task }], todos: [] }
}

test('按当前结构读取数据，不改写字段；空看板可以加载', () => {
  assert.equal(parseDashboard(initial), initial)
  assert.deepEqual(parseDashboard({ projects: [], tasks: [], todos: [] }), { projects: [], tasks: [], todos: [] })
  for (const task of initial.tasks) assert.ok(columns.some(column => column.id === task.status))
})

test('拒绝不完整或无效的数据，不自动补值或转换字段', () => {
  for (const value of [null, {}, { tasks: [], todos: [] }, { projects: [], tasks: [null], todos: [] }]) assert.throws(() => parseDashboard(value), /数据格式/)
  for (const fields of [{ priority: '无效' }, { date: '06/18' }, { tag: undefined }, { id: null }, { projectId: 'missing' }, { color: '#ff0000' }, { status: 'unknown' }]) assert.throws(() => parseDashboard(fixture(fields)), /数据格式/)
  const duplicate = fixture()
  duplicate.tasks.push({ ...duplicate.tasks[0] })
  assert.throws(() => parseDashboard(duplicate), /数据格式/)
})

test('搜索、分类与高优先级筛选可以组合，英文搜索忽略大小写', () => {
  const tasks = [
    { ...initial.tasks[0], id: 1, title: 'WorkLog', description: '个人看板', tag: '开发', priority: '高' },
    { ...initial.tasks[0], id: 2, title: '设计', description: 'worklog', tag: '设计', priority: '中' },
  ]
  assert.equal(filterTasks(tasks, { search: ' WORKLOG ' }).length, 2)
  assert.equal(filterTasks(tasks, { search: '个人' })[0].id, 1)
  assert.deepEqual(filterTasks(tasks, { search: 'worklog', tag: '开发', priorityOnly: true }).map(task => task.id), [1])
  assert.equal(filterTasks(tasks, { tag: '设计', priorityOnly: true }).length, 0)
})

test('当前分类、四级优先级和固定色板有效', () => {
  assert.deepEqual(priorities, ['低', '中', '高', '极高'])
  assert.deepEqual(cardColors.map(color => color.id), ['white', 'red', 'orange', 'blue', 'green'])
  assert.equal(tags[0], '开发')
  assert.ok(tags.includes('临时'))
  for (const priority of priorities) assert.equal(parseDashboard(fixture({ priority })).tasks[0].priority, priority)
  assert.equal(filterTasks(priorities.map(priority => ({ title: priority, description: '', priority })), { priorityOnly: true }).length, 2)
})

test('日期校验支持闰年、同日任务和可选日期，禁止无效及倒序日期', () => {
  assert.equal(validateDates('2026-10-10', '2026-10-09'), '截止日期不能早于开始日期')
  assert.ok(validateDates('2026-02-29', ''))
  assert.ok(validateDates('', '2026-13-01'))
  assert.equal(validateDates('2024-02-29', '2024-02-29'), '')
  assert.equal(validateDates('', ''), '')
  assert.equal(validateDates('2026-10-10', ''), '')
  assert.equal(dateToDay('2026-02-30'), null)
  assert.equal(dayToDate(dateToDay('2026-10-19')), '2026-10-19')
  assert.equal(taskSchedule({ startDate: '2026-10-20', date: '2026-10-19' }), null)
})

test('甘特图忽略双空日期，单个日期按单日任务显示，不改写任务', () => {
  assert.equal(taskSchedule({ startDate: '', date: '' }), null)
  const day = dateToDay('2026-10-20')
  for (const task of [{ startDate: '2026-10-20', date: '' }, { startDate: '', date: '2026-10-20' }]) {
    const original = { ...task }
    assert.deepEqual(taskSchedule(task), { start: day, end: day })
    assert.deepEqual(ganttPlacement(task, day, 10), { left: 0, width: 10 })
    assert.deepEqual(task, original)
  }
  assert.equal(taskSchedule({ startDate: 'invalid', date: '2026-10-20' }), null)
})

test('甘特图首尾包含、跨月跨年和视窗裁剪正确', () => {
  const range = dateToDay('2026-12-25')
  assert.deepEqual(ganttPlacement({ startDate: '2026-12-30', date: '2027-01-02' }, range, 10), { left: 50, width: 40 })
  assert.deepEqual(ganttPlacement({ startDate: '2026-12-24', date: '2026-12-26' }, range, 10), { left: 0, width: 20 })
  assert.deepEqual(ganttPlacement({ startDate: '2027-01-03', date: '2027-01-09' }, range, 10), { left: 90, width: 10 })
  assert.deepEqual(ganttPlacement({ startDate: '2026-12-25', date: '2026-12-25' }, range, 10), { left: 0, width: 10 })
  assert.equal(ganttPlacement({ startDate: '2026-12-24', date: '2026-12-24' }, range, 10), null)
  assert.equal(ganttPlacement({ startDate: '', date: '' }, range, 10), null)
  assert.equal(dateToDay('2026-03-09') - dateToDay('2026-03-08'), 1)
})
