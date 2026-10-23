import { test } from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { dateToDay, dayToDate } from '../src/dashboard.js'
import { parseWorkCalendar, getCalendarDay } from '../src/workCalendar.js'
import { getWorkCalendar } from '../src/api/index.js'

const config = JSON.parse(readFileSync(new URL('./fixtures/work-calendar.json', import.meta.url), 'utf8'))
const calendar = parseWorkCalendar(config)

test('2026 年七个节日和六个调休上班日与通知一致', () => {
  assert.deepEqual(calendar.workingWeekdays, [1, 2, 3, 4, 5])
  assert.deepEqual(calendar.holidays.map(item => [item.name, item.startDate, item.endDate]), [
    ['元旦', '2026-01-01', '2026-01-03'],
    ['春节', '2026-02-15', '2026-02-23'],
    ['清明节', '2026-04-04', '2026-04-06'],
    ['劳动节', '2026-05-01', '2026-05-05'],
    ['端午节', '2026-06-19', '2026-06-21'],
    ['中秋节', '2026-09-25', '2026-09-27'],
    ['国庆节', '2026-10-01', '2026-10-07'],
  ])
  assert.deepEqual(calendar.adjustedWorkdays.map(item => item.date), ['2026-01-04', '2026-02-14', '2026-02-28', '2026-05-09', '2026-09-20', '2026-10-10'])
})

test('放假区间首尾都包含，33 天均识别为节假日，包括工作日和周末', () => {
  let count = 0
  for (const holiday of calendar.holidays) {
    for (let day = dateToDay(holiday.startDate); day <= dateToDay(holiday.endDate); day++) {
      assert.deepEqual(getCalendarDay(dayToDate(day), calendar), { type: 'holiday', name: holiday.name, isWorkday: false })
      count++
    }
  }
  assert.equal(count, 33)
  assert.equal(getCalendarDay('2026-02-24', calendar).type, 'workday')
  assert.equal(getCalendarDay('2026-10-08', calendar).type, 'workday')
})

test('六个调休日期覆盖周末规则，均标记为上班', () => {
  for (const adjusted of calendar.adjustedWorkdays) {
    assert.deepEqual(getCalendarDay(adjusted.date, calendar), { type: 'adjusted-workday', name: adjusted.name, isWorkday: true })
    assert.ok([0, 6].includes(new Date(`${adjusted.date}T00:00:00Z`).getUTCDay()))
  }
  assert.equal(getCalendarDay('2026-09-19', calendar).type, 'restday')
  assert.equal(getCalendarDay('2026-10-11', calendar).type, 'restday')
})

test('普通日期、未配置年份和加载失败时使用每周工作日规则', () => {
  assert.equal(getCalendarDay('2026-07-06', calendar).type, 'workday')
  assert.equal(getCalendarDay('2026-07-11', calendar).type, 'restday')
  assert.equal(getCalendarDay('2027-01-01', calendar).type, 'workday')
  assert.equal(getCalendarDay('2027-01-02', calendar).type, 'restday')
  assert.equal(getCalendarDay('2026-01-04').type, 'restday')
  assert.equal(getCalendarDay('2026-01-01').type, 'workday')
})

test('每周工作日可配置，明确指定的调休上班日优先于放假区间', () => {
  const custom = parseWorkCalendar({
    workingWeekdays: [0, 6],
    holidays: [{ name: '休息', startDate: '2026-07-01', endDate: '2026-07-10' }],
    adjustedWorkdays: [{ name: '上班覆盖', date: '2026-07-06' }],
  })
  assert.equal(getCalendarDay('2026-07-04', custom).type, 'holiday')
  assert.equal(getCalendarDay('2026-07-06', custom).type, 'adjusted-workday')
  assert.equal(getCalendarDay('2026-07-11', custom).type, 'workday')
  assert.equal(getCalendarDay('2026-07-13', custom).type, 'restday')
})

test('全年共 248 个工作日，不将调休上班日当成周末', () => {
  let workdays = 0
  for (let day = dateToDay('2026-01-01'); day <= dateToDay('2026-12-31'); day++) {
    if (getCalendarDay(dayToDate(day), calendar).isWorkday) workdays++
  }
  assert.equal(workdays, 248)
})

test('拒绝非法星期、日期、倒序或重叠的放假区间和重复调休日期', () => {
  for (const value of [
    null, {}, { ...config, workingWeekdays: [1, 1] }, { ...config, workingWeekdays: [7] },
    { ...config, holidays: [{ name: '无效日期', startDate: '2026-02-29', endDate: '2026-03-01' }] },
    { ...config, holidays: [{ name: '倒序', startDate: '2026-10-07', endDate: '2026-10-01' }] },
    { ...config, holidays: [config.holidays[0], config.holidays[0]] },
    { ...config, adjustedWorkdays: [config.adjustedWorkdays[0], config.adjustedWorkdays[0]] },
    { ...config, adjustedWorkdays: [{ name: '', date: '2026-01-04' }] },
  ]) assert.throws(() => parseWorkCalendar(value), /配置格式/)
  assert.throws(() => getCalendarDay('invalid', calendar), /无效日期/)
})

test('本地和后端模式均从前端静态 JSON 读取，忽略旧 localStorage 配置', async t => {
  t.after(() => { delete globalThis.WORKLOG_CONFIG })
  t.mock.method(globalThis, 'fetch', async (url, options) => {
    assert.equal(url, './work-calendar.json')
    assert.equal(options.cache, 'no-store')
    assert.equal(options.credentials, 'same-origin')
    assert.ok(options.signal instanceof AbortSignal)
    return { ok: true, json: async () => config }
  })
  for (const apiBaseUrl of ['', 'https://backend.example.com/api']) {
    globalThis.WORKLOG_CONFIG = { apiBaseUrl }
    assert.deepEqual(await getWorkCalendar(), config)
  }
})

test('发布日历与 2026 年参考数据一致', () => {
  const published = JSON.parse(readFileSync(new URL('../public/work-calendar.json', import.meta.url), 'utf8'))
  assert.deepEqual(parseWorkCalendar(published), config)
})

test('单休和清空节日可自定义，所有日期设为工作日可取消休息日区分', () => {
  const singleRest = parseWorkCalendar({ workingWeekdays: [1, 2, 3, 4, 5, 6], holidays: [], adjustedWorkdays: [] })
  assert.equal(getCalendarDay('2026-07-11', singleRest).type, 'workday')
  assert.equal(getCalendarDay('2026-07-12', singleRest).type, 'restday')
  assert.equal(getCalendarDay('2026-01-01', singleRest).type, 'workday')
  const uniform = parseWorkCalendar({ ...singleRest, workingWeekdays: [0, 1, 2, 3, 4, 5, 6] })
  assert.equal(getCalendarDay('2026-07-12', uniform).type, 'workday')
})

test('配置接口失败时抛出错误，供甘特图降级和重试', async t => {
  globalThis.WORKLOG_CONFIG = { apiBaseUrl: '/api' }
  t.after(() => { delete globalThis.WORKLOG_CONFIG })
  t.mock.method(globalThis, 'fetch', async () => ({ ok: false }))
  await assert.rejects(getWorkCalendar(), /工作日历加载失败/)
})

test('静态日历网络错误、JSON 损坏和结构错误均交由界面降级与重试', async t => {
  t.mock.method(globalThis, 'fetch', async () => { throw new Error('network') })
  await assert.rejects(getWorkCalendar(), /network/)
  globalThis.fetch = async () => ({ ok: true, json: async () => { throw new SyntaxError('invalid JSON') } })
  await assert.rejects(getWorkCalendar(), /invalid JSON/)
  globalThis.fetch = async () => ({ ok: true, json: async () => ({}) })
  await assert.rejects(getWorkCalendar(), /配置格式/)
})
