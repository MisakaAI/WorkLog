import { dateToDay } from './dashboard.js'

const DEFAULT_WORKING_WEEKDAYS = [1, 2, 3, 4, 5]

export function parseWorkCalendar(value) {
  const error = new Error('工作日历配置格式不正确')
  if (!value || !['workingWeekdays', 'holidays', 'adjustedWorkdays'].every(key => Array.isArray(value[key]))) throw error
  const { workingWeekdays, holidays, adjustedWorkdays } = value
  if (new Set(workingWeekdays).size !== workingWeekdays.length || !workingWeekdays.every(day => Number.isInteger(day) && day >= 0 && day <= 6)) throw error
  const validName = item => item && typeof item.name === 'string' && item.name.trim()
  if (!holidays.every(item => validName(item) && dateToDay(item.startDate) !== null && dateToDay(item.endDate) !== null && item.startDate <= item.endDate)) throw error
  if (!adjustedWorkdays.every(item => validName(item) && dateToDay(item.date) !== null) || new Set(adjustedWorkdays.map(item => item.date)).size !== adjustedWorkdays.length) throw error
  const sorted = [...holidays].sort((a, b) => a.startDate.localeCompare(b.startDate))
  if (sorted.some((item, i) => i > 0 && item.startDate <= sorted[i - 1].endDate)) throw error
  return value
}

// 明确指定的调休上班日优先，其次是放假区间，最后使用每周工作日规则。
export function getCalendarDay(date, calendar = null) {
  const day = dateToDay(date)
  if (day === null) throw new Error('无效日期')
  const adjusted = calendar?.adjustedWorkdays.find(item => item.date === date)
  if (adjusted) return { type: 'adjusted-workday', name: adjusted.name, isWorkday: true }
  const holiday = calendar?.holidays.find(item => item.startDate <= date && date <= item.endDate)
  if (holiday) return { type: 'holiday', name: holiday.name, isWorkday: false }
  const weekday = new Date(day * 86400000).getUTCDay()
  const isWorkday = (calendar?.workingWeekdays ?? DEFAULT_WORKING_WEEKDAYS).includes(weekday)
  return { type: isWorkday ? 'workday' : 'restday', name: isWorkday ? '工作日' : '休息日', isWorkday }
}
