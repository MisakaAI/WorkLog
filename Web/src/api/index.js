import { parseDashboard, tags } from '../dashboard.js'
import { parseWorkCalendar } from '../workCalendar.js'
import { getBackendURL, requestJSON } from './client.js'

// 独立命名空间，不将旧版本自带的示例缓存带入正式数据。
export const DASHBOARD_KEY = 'worklog.v2.dashboard'
export const CALENDAR_KEY = 'worklog.v2.calendar'
export const emptyDashboard = () => ({ projects: [], categories: [...tags], tasks: [], todos: [], personalLabel: '个人工作台' })
export const emptyCalendar = () => ({ workingWeekdays: [1, 2, 3, 4, 5], holidays: [], adjustedWorkdays: [] })

function validateSnapshot(value) {
  if (!Number.isSafeInteger(value?.revision) || value.revision < 0) throw new Error('工作台版本格式不正确')
  return { ...parseDashboard(value), revision: value.revision }
}

// 是否配置后端决定数据来源；网络错误不会自动切换本地存储，以免产生两份数据。
export async function getDashboard() {
  if (getBackendURL()) return validateSnapshot(await requestJSON('/dashboard'))
  const cached = localStorage.getItem(DASHBOARD_KEY)
  return cached === null ? { ...emptyDashboard(), revision: 0 } : validateSnapshot(JSON.parse(cached))
}

export async function saveDashboard(snapshot) {
  validateSnapshot(snapshot)
  if (getBackendURL()) return validateSnapshot(await requestJSON('/dashboard', { method: 'PUT', body: snapshot }))
  const cached = localStorage.getItem(DASHBOARD_KEY)
  // 本地模式也检查快照版本，尽量发现其他标签页的修改；
  // localStorage 不提供事务，这并不等同于服务端的原子并发保护。
  const revision = cached === null ? 0 : validateSnapshot(JSON.parse(cached)).revision
  if (revision !== snapshot.revision) throw new Error('工作台已被其他页面修改，请重新加载后再保存')
  const saved = { ...snapshot, revision: revision + 1 }
  localStorage.setItem(DASHBOARD_KEY, JSON.stringify(saved))
  return saved
}

export async function getWorkCalendar() {
  if (getBackendURL()) return parseWorkCalendar(await requestJSON('/work-calendar'))
  const cached = localStorage.getItem(CALENDAR_KEY)
  return cached === null ? emptyCalendar() : parseWorkCalendar(JSON.parse(cached))
}
