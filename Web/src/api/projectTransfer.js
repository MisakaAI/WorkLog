import JSZip from 'jszip'
import { getDashboard } from './index.js'
import { getBackendURL, requestJSON } from './client.js'

// 与后端共用版本化交换格式；只导出项目及其任务，不是数据库备份。
export function projectBundle(snapshot, projectId) {
  const projects = snapshot.projects.filter(project => projectId === undefined || project.id === projectId)
  if (projectId !== undefined && !projects.length) throw new Error('项目尚未保存或已被删除，请保存后再导出')
  const ids = new Set(projects.map(project => project.id))
  return {
    format: 'worklog-projects', version: 1,
    projects, categories: snapshot.categories,
    tasks: snapshot.tasks.filter(task => ids.has(task.projectId)),
  }
}

export async function exportProjects(projectId) {
  if (getBackendURL()) return requestJSON(`/projects/export${projectId === undefined ? '' : `?projectId=${encodeURIComponent(projectId)}`}`, { binary: true })
  const snapshot = await getDashboard({ includeArchived: true })
  const zip = new JSZip()
  zip.file('projects.json', JSON.stringify(projectBundle(snapshot, projectId)))
  return zip.generateAsync({ type: 'blob', compression: 'DEFLATE' })
}

export async function importProjects(file) {
  if (!getBackendURL()) throw new Error('本地模式不支持导入项目')
  if (file.size > 10 * 1024 * 1024) throw new Error('ZIP 文件不能超过 10 MiB')
  return requestJSON('/projects/import', { method: 'POST', body: file, zipBody: true })
}

export function downloadProjects(blob) {
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = 'worklog-projects.zip'
  document.body.append(link)
  link.click()
  link.remove()
  setTimeout(() => URL.revokeObjectURL(url), 1000)
}
