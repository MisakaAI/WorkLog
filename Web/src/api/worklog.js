import { todayISO, dateToDay } from '../dashboard.js'
import { getBackendURL, requestJSON } from './client.js'

export const STORAGE_PREFIX = 'worklog.v2.entry.'
const emptyContent = () => ({ type: 'doc', content: [{ type: 'paragraph' }] })
function validateEntry(entry, date) {
  if (!entry || dateToDay(entry.date) === null || (date && entry.date !== date) || entry.content?.type !== 'doc' ||
    !Number.isSafeInteger(entry.version) || entry.version < 0 ||
    !(entry.updatedAt === null || (typeof entry.updatedAt === 'string' && Number.isFinite(Date.parse(entry.updatedAt))))) throw new Error('工作日志数据格式不正确')
  return entry
}
function readLocal(date) {
  const cached = localStorage.getItem(`${STORAGE_PREFIX}${date}`)
  return cached === null ? { date, content: emptyContent(), updatedAt: null, version: 0 } : validateEntry(JSON.parse(cached), date)
}

export async function requestWorklog(path, { method = 'GET', body } = {}) {
  // 后端模式下 /today 的最终日期由服务端业务时区决定，以响应日期为准。
  const isToday = path === '/api/today'
  const date = isToday ? todayISO() : path.match(/^\/api\/worklog\/(\d{4}-\d{2}-\d{2})$/)?.[1]
  if (!date || dateToDay(date) === null) throw new Error('工作日志日期无效')
  if (method !== 'GET' && !(method === 'PUT' && !isToday)) throw new Error('不支持的工作日志操作')
  const backend = getBackendURL()
  let entry
  if (method === 'PUT') {
    if (body?.content?.type !== 'doc') throw new Error('工作日志内容无效')
    // 版本必须与调用方正在编辑的内容绑定；读取响应没有全局副作用。
    const version = body.version
    if (!Number.isSafeInteger(version) || version < 0 || version >= Number.MAX_SAFE_INTEGER) throw new Error('请提供草稿对应的日志版本')
    if (backend) entry = await requestJSON(path.slice(4), { method, body: { content: body.content, version } })
    else {
      if (readLocal(date).version !== version) {
        const error = new Error('日志已被其他页面修改，请备份草稿并重新加载')
        error.status = 409
        error.code = 'VERSION_CONFLICT'
        throw error
      }
      entry = { date, content: body.content, updatedAt: new Date().toISOString(), version: version + 1 }
      localStorage.setItem(`${STORAGE_PREFIX}${date}`, JSON.stringify(entry))
    }
  } else entry = backend ? await requestJSON(path.slice(4)) : readLocal(date)
  validateEntry(entry, isToday ? undefined : date)
  return entry
}

// 空段落不算有日志；分隔线等非文字内容也应显示标记。
export function hasWorklogContent(content) {
  if (!content || content.type !== 'doc') return false
  const hasContent = node => node.type === 'text'
    ? Boolean(node.text?.trim())
    : node.type === 'horizontalRule' || (node.content || []).some(hasContent)
  return hasContent(content)
}

export async function getWorklogDates() {
  if (getBackendURL()) {
    const result = await requestJSON('/worklog/dates')
    if (!Array.isArray(result?.dates) || result.dates.some(date => dateToDay(date) === null)) throw new Error('日志日期数据格式不正确')
    return [...new Set(result.dates)].sort()
  }
  const dates = []
  for (let i = 0; i < localStorage.length; i++) {
    const key = localStorage.key(i)
    if (!key?.startsWith(STORAGE_PREFIX)) continue
    const date = key.slice(STORAGE_PREFIX.length)
    if (dateToDay(date) === null) continue
    if (hasWorklogContent(readLocal(date).content)) dates.push(date)
  }
  return dates.sort()
}

export const getTodayWorklog = () => requestWorklog('/api/today')
export const getWorklog = date => requestWorklog(`/api/worklog/${date}`)
export const saveWorklog = (date, content, version) => requestWorklog(`/api/worklog/${date}`, { method: 'PUT', body: { content, version } })
