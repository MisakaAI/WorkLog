import { test } from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { ALL_PROJECTS, reorderProjects, parseDashboard, replaceProjects, validateProjects, filterTasks, projectLanes, moveTaskToLane, todayISO, dateToDay, dayToDate } from '../src/dashboard.js'

const taskDefaults = JSON.parse(readFileSync(new URL('./fixtures/dashboard.json', import.meta.url), 'utf8')).tasks[0]

function fixture() {
  return parseDashboard({
    projects: [{ id: 'a', name: '项目 A' }, { id: 'b', name: '项目 B' }],
    tasks: [
      { ...taskDefaults, id: 1, title: 'API 开发', projectId: 'a', status: 'planned', priority: '高' },
      { ...taskDefaults, id: 2, title: '页面设计', projectId: 'b', status: 'done', tag: '设计' },
      { ...taskDefaults, id: 3, title: '临时事项', projectId: '', priority: '极高', tag: '临时' },
    ], todos: [],
  })
}

test('读取项目和任务归属，拒绝无效、重复和保留项目 ID', () => {
  assert.deepEqual(fixture().tasks.map(task => task.projectId), ['a', 'b', ''])
  for (const projects of [[null], [{ id: '', name: '空 ID' }], [{ id: ALL_PROJECTS, name: '保留 ID' }], [{ id: 'a', name: 'A' }, { id: 'a', name: 'B' }]]) {
    assert.throws(() => parseDashboard({ projects, tasks: [], todos: [] }), /数据格式/)
  }
})

test('按项目构建非空泳道，筛选可与分类和优先级组合', () => {
  const { tasks, projects } = fixture()
  assert.deepEqual(projectLanes(tasks, projects).map(lane => [lane.name, lane.tasks.length]), [['项目 A', 1], ['项目 B', 1], ['未分配', 1]])
  const filtered = filterTasks(tasks, { projectId: 'a', search: 'api', priorityOnly: true })
  assert.deepEqual(filtered.map(task => task.id), [1])
  assert.deepEqual(projectLanes(filtered, projects, 'a').map(lane => lane.id), ['a'])
  assert.deepEqual(filterTasks(tasks, { projectId: '', tag: '临时' }).map(task => task.id), [3])
  assert.equal(projectLanes([], projects).length, 0)
  assert.equal(filterTasks(tasks, { projectId: 'a', tag: '设计' }).length, 0)
})

test('空项目和未分配分组均隐藏，筛选后无任务的项目也隐藏', () => {
  const { tasks, projects } = fixture()
  const onlyAssigned = tasks.filter(task => task.projectId === 'a')
  assert.deepEqual(projectLanes(onlyAssigned, [...projects, { id: 'c', name: '空项目' }]).map(lane => lane.id), ['a'])
  assert.deepEqual(projectLanes(onlyAssigned, projects, 'b'), [])
  assert.deepEqual(projectLanes(onlyAssigned, projects, ''), [])
  assert.deepEqual(projectLanes(tasks.filter(task => !task.projectId), projects).map(lane => lane.id), [''])
})

test('跨泳道拖拽同时修改项目和状态，拒绝无效数据，不影响其他任务', () => {
  const data = fixture()
  assert.equal(moveTaskToLane(data, '1', 'progress', 'b'), true)
  assert.equal(data.tasks[0].projectId, 'b')
  assert.equal(data.tasks[0].status, 'progress')
  assert.equal(data.tasks[1].status, 'done')
  assert.equal(moveTaskToLane(data, '1', 'progress', 'b'), false)
  assert.equal(moveTaskToLane(data, '1', 'done', ''), true)
  assert.equal(data.tasks[0].projectId, '')
  for (const args of [['1', 'invalid', 'a'], ['1', 'done', 'missing'], ['unknown', 'done', 'a']]) assert.equal(moveTaskToLane(data, ...args), false)
})

test('待开始拖至进行中补填开始日期，进行中拖至已完成补填截止日期，并可保存恢复', () => {
  const data = fixture()
  const task = data.tasks[0]
  task.startDate = ''
  task.date = ''
  assert.equal(moveTaskToLane(data, '1', 'progress', 'b'), true)
  assert.equal(task.startDate, todayISO())
  assert.equal(task.date, '')
  assert.equal(moveTaskToLane(data, '1', 'done', 'b'), true)
  assert.equal(task.date, todayISO())
  assert.deepEqual(parseDashboard(JSON.parse(JSON.stringify(data))), data)
})

test('拖拽补填不覆盖已有日期', () => {
  const data = fixture()
  const task = data.tasks[0]
  task.startDate = '2026-01-01'
  task.date = '2026-01-10'
  moveTaskToLane(data, '1', 'progress', 'a')
  moveTaskToLane(data, '1', 'done', 'a')
  assert.equal(task.startDate, '2026-01-01')
  assert.equal(task.date, '2026-01-10')
})

test('拖拽遇到过去截止日期或未来开始日期时保留空日期，状态和排序仍可保存', () => {
  const today = todayISO()
  const yesterday = dayToDate(dateToDay(today) - 1)
  const tomorrow = dayToDate(dateToDay(today) + 1)
  for (const [from, to, startDate, date, expectedStart, expectedDate] of [
    ['planned', 'progress', '', yesterday, '', yesterday],
    ['progress', 'done', tomorrow, '', tomorrow, ''],
    ['planned', 'progress', '', today, today, today],
    ['progress', 'done', today, '', today, today],
    ['planned', 'progress', '', tomorrow, today, tomorrow],
    ['progress', 'done', yesterday, '', yesterday, today],
  ]) {
    const data = fixture()
    Object.assign(data.tasks[0], { status: from, startDate, date })
    data.tasks[1].status = to
    assert.equal(moveTaskToLane(data, '1', to, 'b', { targetId: 2, position: 'after' }), true)
    const task = data.tasks.find(task => task.id === 1)
    assert.equal(task.status, to)
    assert.equal(task.startDate, expectedStart)
    assert.equal(task.date, expectedDate)
    assert.deepEqual(data.tasks.map(task => task.id), [2, 1, 3])
    assert.deepEqual(parseDashboard(JSON.parse(JSON.stringify(data))), data)
  }
})

test('其他状态转换、同状态跨项目以及无效拖拽不补填日期', () => {
  for (const [from, to, projectId] of [
    ['planned', 'done', 'a'], ['progress', 'planned', 'a'],
    ['done', 'progress', 'a'], ['done', 'planned', 'a'],
    ['planned', 'planned', 'b'], ['progress', 'progress', 'b'], ['done', 'done', 'b'],
    ['planned', 'progress', 'missing'], ['progress', 'done', 'missing'],
  ]) {
    const data = fixture()
    const task = data.tasks[0]
    Object.assign(task, { status: from, startDate: '', date: '' })
    moveTaskToLane(data, '1', to, projectId)
    assert.equal(task.startDate, '', `${from} → ${to} 不应补填开始日期`)
    assert.equal(task.date, '', `${from} → ${to} 不应补填截止日期`)
  }
})

test('重命名不改变任务归属，删除项目同步删除其任务且不影响其他项目和未分配任务', () => {
  const data = fixture()
  const renamed = replaceProjects(data, [{ id: 'a', name: '新名称' }, data.projects[1]])
  assert.equal(renamed.tasks[0].projectId, 'a')
  const deleted = replaceProjects(renamed, [renamed.projects[1]])
  assert.deepEqual(deleted.tasks, [data.tasks[1], data.tasks[2]])
  assert.deepEqual(replaceProjects(data, []).tasks, [data.tasks[2]])
  assert.equal(data.tasks.length, 3)
  assert.deepEqual(parseDashboard(JSON.parse(JSON.stringify(deleted))), deleted)
})

test('项目名称校验禁止空白、过长和重复名称', () => {
  assert.equal(validateProjects([]), '')
  assert.ok(validateProjects([{ name: ' ' }]))
  assert.ok(validateProjects([{ name: 'x'.repeat(41) }]))
  assert.ok(validateProjects([{ name: ' Worklog ' }, { name: 'worklog' }]))
  assert.equal(validateProjects([{ name: '项目 A' }, { name: '项目 B' }]), '')
})

test('项目排序不改变草稿源数据，保存重读保留顺序，未分配始终最后', () => {
  const data = fixture()
  const projects = [...data.projects, { id: 'c', name: '项目 C' }]
  const sorted = reorderProjects(projects, 'c', 'a')
  assert.deepEqual(sorted.map(project => project.id), ['c', 'a', 'b'])
  assert.deepEqual(projects.map(project => project.id), ['a', 'b', 'c'])
  assert.deepEqual(reorderProjects(sorted, 'c', 'b').map(project => project.id), ['a', 'b', 'c'])
  const saved = parseDashboard(JSON.parse(JSON.stringify(replaceProjects(data, sorted))))
  assert.deepEqual(projectLanes(saved.tasks, saved.projects).map(lane => lane.id), ['a', 'b', ''])
  assert.deepEqual(saved.tasks, data.tasks)
  for (const [source, target] of [['', 'a'], ['a', ''], ['unknown', 'a'], ['a', 'unknown'], ['a', 'a'], ['a', undefined]]) {
    assert.equal(reorderProjects(projects, source, target), projects)
  }
})

test('Element 日期组件使用本地日期判断禁用范围，不受 UTC 时差影响', () => {
  assert.equal(todayISO(new Date(2026, 0, 2, 0, 30)), '2026-01-02')
})
