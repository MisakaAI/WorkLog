import { test } from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { getCalendarDay, parseWorkCalendar } from '../src/workCalendar.js'

const app = readFileSync(new URL('../src/App.vue', import.meta.url), 'utf8')
const css = readFileSync(new URL('../src/style.css', import.meta.url), 'utf8')
const calendar = parseWorkCalendar(JSON.parse(readFileSync(new URL('./fixtures/work-calendar.json', import.meta.url), 'utf8')))

test('工作日历面板加载配置，按完整日期和 isWorkday 标记非工作日', () => {
  assert.match(app, /const \{ workCalendar, calendarLoading, calendarError, loadCalendar \} = useWorkCalendar\(\)/)
  assert.match(app, /:calendar="workCalendar"/)
  assert.match(app, /'is-restday': !getCalendarDay\(cell\.dayjs\.format\('YYYY-MM-DD'\), workCalendar\)\.isWorkday/)
  for (const date of ['2026-07-11', '2026-07-12', '2026-10-01']) {
    assert.equal(getCalendarDay(date, calendar).isWorkday, false)
  }
  for (const date of ['2026-07-13', ...calendar.adjustedWorkdays.map(item => item.date)]) {
    assert.equal(getCalendarDay(date, calendar).isWorkday, true)
  }
})

test('非工作日使用指定强调色，选中态保留对比度且样式仅作用于日期面板', () => {
  assert.ok(css.includes('.date-panel .el-date-table td .is-restday .el-date-table-cell__text { color: rgb(133 104 176 / 88%); background-color: rgb(133 104 176 / 10%); }'))
  assert.ok(css.includes('.date-panel .el-date-table td.current:not(.disabled) .is-restday .el-date-table-cell__text { color: #fff; background-color: #8568b0; }'))
})
