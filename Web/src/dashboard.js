// 全部项目使用专用标记；空字符串留给“未分配”，两者不能混用。
export const ALL_PROJECTS = '__all__'
export const columns = [
  { id: 'planned', title: '待开始' },
  { id: 'progress', title: '进行中' },
  { id: 'done', title: '已完成' },
]
export const tags = ['开发', '产品', '设计', '调研', '临时', '个人']
export const priorities = ['低', '中', '高', '极高']
export const cardColors = [
  { id: 'white', label: '白' }, { id: 'red', label: '红' },
  { id: 'orange', label: '橙' }, { id: 'blue', label: '蓝' }, { id: 'green', label: '绿' },
]

export function todayISO(date = new Date()) {
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')}`
}

// 使用 UTC 日序号进行日期运算，避免夏令时导致一天不等于 24 小时。
export function dateToDay(value) {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(value)) return null
  const timestamp = Date.parse(`${value}T00:00:00Z`)
  if (!Number.isFinite(timestamp) || new Date(timestamp).toISOString().slice(0, 10) !== value) return null
  return timestamp / 86400000
}
export function dayToDate(day) { return new Date(day * 86400000).toISOString().slice(0, 10) }

export function validateDates(startDate, date) {
  if ((startDate && dateToDay(startDate) === null) || (date && dateToDay(date) === null)) return '请输入有效日期（YYYY-MM-DD）'
  if (startDate && date && date < startDate) return '截止日期不能早于开始日期'
  return ''
}

// 与 Go strings.ToLower / 数据库 name_key 一致：逐码点简单小写，
// 不使用 JS 的上下文 sigma 规则或 İ -> i + combining dot 的完整映射。
export function nameKey(name) {
  return Array.from(name, char => char === '\u0130' ? 'i' : char.toLowerCase()).join('')
}

export function validateProjects(projects) {
  const names = projects.map(project => project.name.trim())
  if (names.some(name => !name)) return '请输入项目名称'
  if (names.some(name => name.length > 40)) return '项目名称不能超过 40 个字'
  if (new Set(names.map(nameKey)).size !== names.length) return '项目名称不能重复'
  return ''
}

export function validateCategories(categories) {
  if (!Array.isArray(categories) || !categories.length) return '至少保留一个分类'
  if (categories.some(name => typeof name !== 'string' || !name.trim())) return '请输入分类名称'
  if (categories.some(name => name.trim().length > 40)) return '分类名称不能超过 40 个字'
  if (new Set(categories.map(name => nameKey(name.trim()))).size !== categories.length) return '分类名称不能重复'
  return ''
}

// 按原分类名迁移任务，支持重命名和名称交换；被删除分类的任务归入首个分类。
export function replaceCategories(data, drafts) {
  const categories = drafts.map(item => item.name.trim())
  const error = validateCategories(categories)
  if (error) throw new Error(error)
  const renamed = new Map(drafts.filter(item => item.originalName).map(item => [item.originalName, item.name.trim()]))
  return { ...data, categories, tasks: data.tasks.map(task => ({ ...task, tag: renamed.get(task.tag) || categories[0] })) }
}

export function reorderProjects(projects, sourceId, targetId) {
  const from = projects.findIndex(project => project.id === sourceId)
  const to = projects.findIndex(project => project.id === targetId)
  if (!sourceId || !targetId || from < 0 || to < 0 || from === to) return projects
  const reordered = [...projects]
  const [project] = reordered.splice(from, 1)
  reordered.splice(to, 0, project)
  return reordered
}

// 只校验当前数据结构，不迁移、补齐或改写旧格式。
export function parseDashboard(value) {
  const error = new Error('看板数据格式不正确')
  if (!value || !['projects', 'tasks', 'todos'].every(key => Array.isArray(value[key]))) throw error
  const { projects, tasks, todos, categories = tags } = value
  if (value.includeArchived !== undefined && typeof value.includeArchived !== 'boolean') throw error
  if (value.personalLabel !== undefined && (typeof value.personalLabel !== 'string' || value.personalLabel.length > 255)) throw error
  if (validateCategories(categories)) throw error
  if (!projects.every(project => project && typeof project.id === 'string' && project.id && project.id !== ALL_PROJECTS && typeof project.name === 'string' && (project.archived === undefined || typeof project.archived === 'boolean'))) throw error
  const projectIds = new Set(projects.map(project => project.id))
  if (projectIds.size !== projects.length || validateProjects(projects)) throw error
  const validId = id => (typeof id === 'string' && id.length > 0) || Number.isSafeInteger(id)
  if (!tasks.every(task => task && validId(task.id) &&
    ['title', 'description', 'projectId', 'startDate', 'date'].every(key => typeof task[key] === 'string') &&
    task.title.trim().length > 0 && task.title.length <= 100 &&
    (task.projectId === '' || projectIds.has(task.projectId)) &&
    columns.some(column => column.id === task.status) && categories.includes(task.tag) &&
    priorities.includes(task.priority) && cardColors.some(color => color.id === task.color) &&
    !validateDates(task.startDate, task.date))) throw error
  if (!todos.every(todo => todo && validId(todo.id) && typeof todo.title === 'string' && todo.title.trim().length > 0 && todo.title.length <= 100 && typeof todo.done === 'boolean')) throw error
  if ([tasks, todos].some(items => new Set(items.map(item => String(item.id))).size !== items.length)) throw error
  return value
}

// 删除项目时同步删除其任务，保留其他项目及未分配任务。
export function replaceProjects(data, projects) {
  const projectIds = new Set(projects.map(project => project.id))
  return {
    ...data,
    projects: projects.map(project => ({ ...project, name: project.name.trim() })),
    tasks: data.tasks.filter(task => task.projectId === '' || projectIds.has(task.projectId)),
  }
}

export function filterTasks(tasks, { search = '', tag = '', priorityOnly = false, projectId = ALL_PROJECTS } = {}) {
  const query = search.trim().toLowerCase()
  return tasks.filter(task =>
    (!query || `${task.title} ${task.description}`.toLowerCase().includes(query)) &&
    (!tag || task.tag === tag) &&
    (projectId === ALL_PROJECTS || task.projectId === projectId) &&
    (!priorityOnly || ['高', '极高'].includes(task.priority)),
  )
}

export function projectLanes(tasks, projects, projectId = ALL_PROJECTS) {
  return [...projects, { id: '', name: '未分配' }]
    .filter(project => projectId === ALL_PROJECTS || project.id === projectId)
    .map(project => ({ ...project, tasks: tasks.filter(task => task.projectId === project.id) }))
    .filter(project => project.tasks.length > 0)
}

// 拖动时兼容数字和字符串 ID；仅在指定状态流转且日期为空时补入浏览器当天日期。
// 这是界面交互规则，后端细粒度 move_task 不会自动补日期。
// placement 指定卡片前/后的插入位置；targetId 为 null 表示目标列末尾。
// 顺序直接存入 tasks 数组，沿用快照与数据库 position，无需新增排序字段。
export function moveTaskToLane(data, id, status, projectId, placement) {
  if (!columns.some(column => column.id === status) || (projectId !== '' && !data.projects.some(project => project.id === projectId))) return false
  const task = data.tasks.find(task => String(task.id) === String(id))
  if (!task) return false
  const laneChanged = task.status !== status || task.projectId !== projectId
  let reordered = data.tasks
  if (placement) {
    const remaining = data.tasks.filter(item => item !== task)
    let index
    if (placement.targetId === null) {
      index = remaining.findLastIndex(item => item.status === status && item.projectId === projectId) + 1
    } else {
      index = remaining.findIndex(item => String(item.id) === String(placement.targetId) && item.status === status && item.projectId === projectId)
      if (index < 0 || !['before', 'after'].includes(placement.position)) return false
      if (placement.position === 'after') index++
    }
    reordered = [...remaining.slice(0, index), task, ...remaining.slice(index)]
  }
  const laneTasks = items => items.filter(item => item === task || (item.status === status && item.projectId === projectId))
  const previousOrder = laneTasks(data.tasks)
  const orderChanged = laneTasks(reordered).some((item, index) => item !== previousOrder[index])
  if (!laneChanged && !orderChanged) return false
  if (task.status === 'planned' && status === 'progress' && !task.startDate) task.startDate = todayISO()
  if (task.status === 'progress' && status === 'done' && !task.date) task.date = todayISO()
  task.status = status
  task.projectId = projectId
  if (orderChanged) data.tasks = reordered
  return true
}

// 只设置一个日期时按单日任务绘制；两个日期都为空时不参与排期。
export function taskSchedule(task) {
  const start = dateToDay(task.startDate || task.date)
  const end = dateToDay(task.date || task.startDate)
  return start !== null && end !== null && end >= start ? { start, end } : null
}

// 周、月按自然周期显示；固定天数视图从前天开始，翻页保持对应周期。
export function ganttRange(view, today, offset = 0) {
  const date = new Date(today * 86400000)
  if (view === 'month') {
    const year = date.getUTCFullYear()
    const month = date.getUTCMonth() + offset
    const start = Date.UTC(year, month, 1) / 86400000
    const end = Date.UTC(year, month + 1, 1) / 86400000
    return { start, dayCount: end - start }
  }
  if (view === 'week') {
    // 以周一为起点；周日归入前六天所在的周。
    return { start: today - (date.getUTCDay() + 6) % 7 + offset * 7, dayCount: 7 }
  }
  const dayCount = view === '15days' ? 15 : 30
  return { start: today - 2 + offset * dayCount, dayCount }
}

// 按首尾日期均包含在内的区间裁剪；区间外不绘制，单日任务仍占一个日期格。
export function ganttPlacement(task, rangeStart, dayCount) {
  const schedule = taskSchedule(task)
  if (!schedule) return null
  const start = Math.max(schedule.start, rangeStart)
  const end = Math.min(schedule.end, rangeStart + dayCount - 1)
  return end < start ? null : { left: (start - rangeStart) / dayCount * 100, width: (end - start + 1) / dayCount * 100 }
}
