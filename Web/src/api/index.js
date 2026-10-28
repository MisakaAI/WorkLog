import { parseDashboard, replaceCategories, tags } from '../dashboard.js'
import { parseWorkCalendar } from '../workCalendar.js'
import { getBackendURL, requestJSON } from './client.js'

// 独立命名空间，不将旧版本自带的示例缓存带入正式数据。
export const DASHBOARD_KEY = 'worklog.v2.dashboard'
export const emptyDashboard = () => ({ projects: [], categories: [...tags], tasks: [], todos: [], personalLabel: '个人工作台' })
export const emptyCalendar = () => ({ workingWeekdays: [1, 2, 3, 4, 5], holidays: [], adjustedWorkdays: [] })

function validateSnapshot(value) {
  if (!Number.isSafeInteger(value?.revision) || value.revision < 0) throw new Error('工作台版本格式不正确')
  return { ...parseDashboard(value), revision: value.revision }
}

// 是否配置后端决定数据来源；网络错误不会自动切换本地存储，以免产生两份数据。
export function visibleDashboard(snapshot) {
  const hidden = new Set(snapshot.projects.filter(project => project.archived).map(project => project.id))
  const visible = { ...snapshot, projects: snapshot.projects.filter(project => !project.archived), tasks: snapshot.tasks.filter(task => !hidden.has(task.projectId)) }
  delete visible.includeArchived
  return visible
}

export async function getDashboard({ includeArchived = false } = {}) {
  if (getBackendURL()) return validateSnapshot(await requestJSON(`/dashboard${includeArchived ? '?includeArchived=true' : ''}`))
  const cached = localStorage.getItem(DASHBOARD_KEY)
  const snapshot = cached === null ? { ...emptyDashboard(), revision: 0 } : validateSnapshot(JSON.parse(cached))
  return includeArchived ? { ...snapshot, includeArchived: true } : visibleDashboard(snapshot)
}

export async function saveDashboard(snapshot) {
  validateSnapshot(snapshot)
  if (getBackendURL()) return validateSnapshot(await requestJSON('/dashboard', { method: 'PUT', body: snapshot }))
  const cached = localStorage.getItem(DASHBOARD_KEY)
  // 本地模式也检查快照版本，尽量发现其他标签页的修改；
  // localStorage 不提供事务，这并不等同于服务端的原子并发保护。
  const revision = cached === null ? 0 : validateSnapshot(JSON.parse(cached)).revision
  if (revision !== snapshot.revision) throw new Error('工作台已被其他页面修改，请重新加载后再保存')
  let merged = snapshot
  if (!snapshot.includeArchived && cached !== null) {
    const old = validateSnapshot(JSON.parse(cached))
    const archived = old.projects.filter(project => project.archived)
    const hidden = new Set(archived.map(project => project.id))
    const visible = snapshot.projects.filter(project => !hidden.has(project.id))
    let index = 0
    // 按旧完整顺序保留归档位置，可见项目按提交顺序填入其余位置；新增项目追加。
    const projects = old.projects.flatMap(project => project.archived ? [project] : index < visible.length ? [visible[index++]] : [])
    merged = { ...snapshot, projects: [...projects, ...visible.slice(index)], tasks: [...snapshot.tasks, ...old.tasks.filter(task => hidden.has(task.projectId)).map(task => ({ ...task, tag: snapshot.categories.includes(task.tag) ? task.tag : snapshot.categories[0] }))] }
  }
  const saved = { ...merged, revision: revision + 1 }
  delete saved.includeArchived
  validateSnapshot(saved)
  localStorage.setItem(DASHBOARD_KEY, JSON.stringify(saved))
  return snapshot.includeArchived ? { ...saved, includeArchived: true } : visibleDashboard(saved)
}

export async function manageCategories(snapshot, drafts) {
  const categories = drafts.map(({ originalName, name }) => ({ originalName, name: name.trim() }))
  if (getBackendURL()) {
    const current = await requestJSON('/categories')
    if (current.revision !== snapshot.revision) throw new Error('数据已被修改，请刷新页面后重试')
    await requestJSON('/categories', { method: 'PUT', body: { categoryRevision: current.categoryRevision, categories } })
    // 集合响应只包含可见任务的变更；重新读取完整默认快照，避免遗漏并发新增的数据。
    return getDashboard()
  }
  const current = await getDashboard({ includeArchived: true })
  if (current.revision !== snapshot.revision) throw new Error('工作台已被其他页面修改，请重新加载后再保存')
  return visibleDashboard(await saveDashboard(replaceCategories(current, categories)))
}

export async function getWorkCalendar() {
  // 两种模式均读取前端静态资源；相对路径兼容子目录部署，不使用 API 地址或浏览器缓存配置。
  const response = await fetch('./work-calendar.json', {
    cache: 'no-store',
    credentials: 'same-origin',
    signal: AbortSignal.timeout(15000),
  })
  if (!response.ok) throw new Error(`工作日历加载失败（HTTP ${response.status}）`)
  return parseWorkCalendar(await response.json())
}
