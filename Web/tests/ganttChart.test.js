import { test } from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { compileScript, parse } from '@vue/compiler-sfc'
import { computed, nextTick, ref, watch } from 'vue'
import { dateToDay, dayToDate, ganttPlacement, ganttRange } from '../src/dashboard.js'
import { getCalendarDay, parseWorkCalendar } from '../src/workCalendar.js'

const component = readFileSync(new URL('../src/components/GanttChart.vue', import.meta.url), 'utf8')
const calendar = parseWorkCalendar(JSON.parse(readFileSync(new URL('./fixtures/work-calendar.json', import.meta.url), 'utf8')))

test('甘特图根据实际是否上班统一显示工作日和休息日', () => {
  const { descriptor } = parse(component)
  const daysExpression = descriptor.scriptSetup.content.match(/const days = computed\((\(\) => Array\.from\([\s\S]*?)\)\nconst scheduled/)[1]
  const days = new Function('dayCount', 'rangeStart', 'dayToDate', 'getCalendarDay', 'calendar', 'today', `return (${daysExpression})()`)
  const renderDay = date => days(
    { value: 1 }, { value: 0 }, () => date, getCalendarDay, { value: calendar }, { value: -1 },
  )[0]
  for (const [date, type, status] of [
    ['2026-01-01', 'restday', '休息日'],
    ['2026-01-04', 'workday', '工作日'],
    ['2026-07-06', 'workday', '工作日'],
    ['2026-07-11', 'restday', '休息日'],
  ]) {
    const day = renderDay(date)
    assert.equal(day.type, type)
    assert.ok(day.title.endsWith(` · ${status}`))
  }
})

test('时间范围依次支持周、月、15天和30天，默认选择周', () => {
  assert.match(component, /const view = ref\('week'\)/)
  assert.match(component, /<ElSelect v-model="view"/)
  assert.match(component, /<ElOption value="week" label="周" \/><ElOption value="month" label="月" \/><ElOption value="15days" label="15天" \/><ElOption value="30days" label="30天" \/>/)
  assert.doesNotMatch(component, /双周/)
})

test('移除任务定位入口和逻辑，保留时间段导航', () => {
  assert.doesNotMatch(component, /locateTasks|定位任务|点击定位|gantt-outside|rangeStart = task\.schedule/)
  assert.match(component, /@click="periodOffset--"/)
  assert.match(component, /@click="periodOffset = 0"/)
  assert.match(component, /@click="periodOffset\+\+"/)
  assert.match(component, /v-if="task\.placement"/)
})

test('周视图从本周一到周日，翻页及跨年日期归属正确', () => {
  const monday = dateToDay('2026-07-06')
  for (let day = 0; day < 7; day++) {
    for (const offset of [-1, 0, 1]) {
      assert.deepEqual(ganttRange('week', monday + day, offset), { start: monday + offset * 7, dayCount: 7 })
    }
  }
  assert.deepEqual(ganttRange('week', dateToDay('2027-01-01')), { start: dateToDay('2026-12-28'), dayCount: 7 })
})

test('月视图显示完整自然月，覆盖大小月、闰年和跨年翻页', () => {
  for (const [today, offset, start, count] of [
    ['2026-07-11', 0, '2026-07-01', 31],
    ['2026-04-30', 0, '2026-04-01', 30],
    ['2026-02-28', 0, '2026-02-01', 28],
    ['2024-02-29', 0, '2024-02-01', 29],
    ['2026-01-31', 1, '2026-02-01', 28],
    ['2026-03-31', -1, '2026-02-01', 28],
    ['2026-12-31', 1, '2027-01-01', 31],
    ['2027-01-01', -1, '2026-12-01', 31],
  ]) {
    assert.deepEqual(ganttRange('month', dateToDay(today), offset), { start: dateToDay(start), dayCount: count })
  }
})

test('15天和30天视图从前天开始，首尾包含且按固定天数翻页', () => {
  for (const today of ['2026-07-11', '2026-03-01', '2024-03-01', '2027-01-01']) {
    const day = dateToDay(today)
    for (const count of [15, 30]) {
      for (const offset of [-1, 0, 1]) {
        assert.deepEqual(ganttRange(`${count}days`, day, offset), { start: day - 2 + offset * count, dayCount: count })
      }
    }
  }
  const range = ganttRange('15days', dateToDay('2027-01-01'))
  assert.equal(dayToDate(range.start), '2026-12-30')
  assert.equal(dayToDate(range.start + range.dayCount - 1), '2027-01-13')
})

test('翻页后切换视图和返回今天均重置到当前视图的默认范围', async () => {
  const stateScript = component.match(/const view = ref\('week'\)[\s\S]*?(?=const calendar = computed)/)[0]
  const today = dateToDay('2026-07-11')
  const state = new Function('ref', 'computed', 'watch', 'ganttRange', 'today', `${stateScript}\nreturn { view, periodOffset, rangeStart, dayCount }`)(ref, computed, watch, ganttRange, { value: today })
  for (const view of ['month', '15days', '30days', 'week']) {
    state.periodOffset.value = 2
    state.view.value = view
    await nextTick()
    assert.equal(state.periodOffset.value, 0)
    assert.equal(state.rangeStart.value, ganttRange(view, today).start)
    assert.equal(state.dayCount.value, ganttRange(view, today).dayCount)
    state.periodOffset.value = -1
    assert.equal(state.rangeStart.value, ganttRange(view, today, -1).start)
    state.periodOffset.value = 0
    assert.equal(state.rangeStart.value, ganttRange(view, today).start)
  }
})

test('图例仅保留两类，不再展示节假日标记和今天描边说明', () => {
  const legend = component.match(/<div class="gantt-legend"[^>]*>(.*?)<\/div>/)[1]
  assert.equal((legend.match(/<i /g) || []).length, 2)
  assert.match(legend, /工作日/)
  assert.match(legend, /休息日/)
  assert.doesNotMatch(component, /描边为今天|ElTag|gantt-day-marker|adjusted-workday/)
  assert.match(component, /today: day\.today/)
})

test('美化后的模板可编译，保留日期语义、任务状态和编辑入口', () => {
  const { descriptor, errors } = parse(component)
  assert.deepEqual(errors, [])
  assert.doesNotThrow(() => compileScript(descriptor, { id: 'gantt-chart-test', inlineTemplate: true }))
  assert.match(component, /:aria-current="day\.today \? 'date' : undefined"/)
  assert.match(component, /statusLabel\(task\.status\)/)
  assert.equal((component.match(/@click="emit\('edit', task\)"/g) || []).length, 2)
  assert.match(component, /v-else class="gantt-empty"/)
})

test('任务条内缩保留首尾日期和裁剪边界，单日任务不消失', () => {
  const body = component.match(/function barStyle\(placement\) \{ return (.*) \}$/m)[1]
  const barStyle = new Function('placement', `return ${body}`)
  const start = dateToDay('2026-12-29')
  for (const count of [7, 15, 28, 29, 30, 31]) {
    for (const task of [
      { startDate: '2026-12-29', date: '2026-12-29' },
      { startDate: '2026-12-01', date: '2027-02-01' },
      { startDate: '2026-12-31', date: '2027-01-02' },
    ]) {
      const placement = ganttPlacement(task, start, count)
      assert.deepEqual(barStyle(placement), { left: `calc(${placement.left}% + 4px)`, width: `calc(${placement.width}% - 8px)` })
      // 最窄的日期格仍为 44px，留白后单日任务条至少有 36px。
      assert.ok(placement.width / 100 * count * 44 - 8 >= 35.99)
    }
  }
})

test('甘特图复用主题阴影并保留滚动、粘性表头和键盘焦点', () => {
  const css = readFileSync(new URL('../src/style.css', import.meta.url), 'utf8')
  assert.match(css, /\.gantt-scroll \{[^}]*overflow: auto;[^}]*box-shadow: var\(--shadow-inset\)/)
  assert.match(css, /\.gantt-header \{[^}]*position: sticky;[^}]*top: 0/)
  assert.match(css, /\.gantt-task-name \{[^}]*position: sticky;[^}]*left: 0/)
  assert.match(css, /\.gantt-bar\.el-button:focus-visible \{[^}]*var\(--shadow-focus-visible\)/)
  assert.match(css, /\.gantt-controls \.gantt-nav-button \{ width: 44px; min-width: 44px;/)
  assert.match(component, /minWidth: `calc\(var\(--gantt-task-width\)/)
})
