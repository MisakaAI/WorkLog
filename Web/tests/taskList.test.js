import { test } from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { compileScript, parse } from '@vue/compiler-sfc'
import { computed, reactive } from 'vue'

const component = readFileSync(new URL('../src/components/TaskBoard.vue', import.meta.url), 'utf8')
const css = readFileSync(new URL('../src/style.css', import.meta.url), 'utf8')

function progressFor(lanes) {
  const expression = component.match(/const laneProgress = computed\(([\s\S]*?)\nfunction drop/)[1].trim().slice(0, -1)
  const props = reactive({ lanes })
  const progress = new Function('computed', 'props', 'laneKey', `return computed(${expression})`)(computed, props, lane => lane.id || '__unassigned__')
  return { props, progress }
}

test('列表模板可编译，保留看板拖动、列表编辑、新建与折叠入口', () => {
  const { descriptor, errors } = parse(component)
  assert.deepEqual(errors, [])
  assert.doesNotThrow(() => compileScript(descriptor, { id: 'task-list-test', inlineTemplate: true }))
  assert.match(component, /<ElCollapse v-model="expanded"/)
  assert.match(component, /@dragstart="startDrag\(\$event, task\)"/)
  const list = component.slice(component.indexOf('<section v-else class="task-list"'))
  assert.match(list, /<ul class="list-rows">/)
  assert.match(list, /<li v-for="task in lane.tasks" :key="task.id">/)
  assert.match(list, /<ElButton class="list-task"[\s\S]*?@click="emit\('edit', task\)"/)
  assert.match(list, /emit\('create', \{ status: 'planned', projectId: lane.id \}\)/)
  assert.match(list, /<ElEmpty v-if="!lane.tasks.length"/)
})

test('完成进度只计算已完成任务，空组为 0%，未分配与满进度正确', () => {
  const { progress } = progressFor([
    { id: 'mixed', tasks: [{ status: 'planned' }, { status: 'progress' }, { status: 'done' }] },
    { id: 'empty', tasks: [] },
    { id: '', tasks: [{ status: 'done' }, { status: 'done' }] },
    { id: 'ongoing', tasks: [{ status: 'progress' }] },
  ])
  assert.deepEqual(progress.value.get('mixed'), { completed: 1, percent: 33 })
  assert.deepEqual(progress.value.get('empty'), { completed: 0, percent: 0 })
  assert.deepEqual(progress.value.get('__unassigned__'), { completed: 2, percent: 100 })
  assert.deepEqual(progress.value.get('ongoing'), { completed: 0, percent: 0 })
})

test('任务状态变更和筛选结果变化会更新完成率，不修改任务', () => {
  const lanes = [{ id: 'a', tasks: [{ id: 1, status: 'planned' }, { id: 2, status: 'done' }] }]
  const original = structuredClone(lanes)
  const { props, progress } = progressFor(lanes)
  assert.equal(progress.value.get('a').percent, 50)
  assert.deepEqual(lanes, original)
  props.lanes[0].tasks[0].status = 'done'
  assert.equal(progress.value.get('a').percent, 100)
  props.lanes = [{ id: 'b', tasks: [{ status: 'planned' }] }]
  assert.equal(progress.value.has('a'), false)
  assert.deepEqual(progress.value.get('b'), { completed: 0, percent: 0 })
})

test('列表展示真实日期、缺省提示与可访问的完成进度', () => {
  assert.match(component, /role="progressbar"[^>]*:aria-label=[^>]*:aria-valuenow=[^>]*:aria-valuetext=[^>]*:aria-valuemin="0" :aria-valuemax="100"/)
  assert.match(component, /当前筛选结果/)
  assert.match(component, /<time v-if="task.startDate" :datetime="task.startDate">/)
  assert.match(component, /<time v-if="task.date" :datetime="task.date">/)
  assert.match(component, /未设开始/)
  assert.match(component, /未设截止/)
  assert.match(component, /class="list-status task-status-pill" :class="task.status"/)
})

test('列表复用主题柔和投影、状态色及焦点，窄屏不隐藏任务信息', () => {
  assert.match(css, /\.task-list \{[^}]*box-shadow: var\(--shadow-inset\)/)
  assert.match(css, /\.list-task\.el-button \{[^}]*box-shadow: var\(--shadow-card\)/)
  assert.match(css, /\.task-list \.list-task\.el-button:focus-visible \{[^}]*var\(--shadow-focus-visible\)/)
  assert.match(css, /\.task-status-pill.done \{[^}]*var\(--green-fg\)/)
  const mobile = css.slice(css.indexOf('@media (max-width: 767px)'))
  assert.match(mobile, /\.list-task-content \{ grid-template-columns: minmax\(0, 1fr\) auto;/)
  assert.match(mobile, /\.list-date \{[^}]*flex-wrap: wrap/)
  assert.doesNotMatch(mobile, /\.list-(?:title|description|labels|status|date) \{[^}]*display: none/)
})
