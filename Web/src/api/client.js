// 运行时配置优先；空字符串显式启用本地模式。配置错误不能静默降级。
export function getBackendURL() {
  const value = globalThis.WORKLOG_CONFIG?.apiBaseUrl ?? import.meta.env?.VITE_API_BASE_URL ?? ''
  if (typeof value !== 'string') throw new Error('后端 URL 必须为字符串')
  const trimmed = value.trim()
  if (!trimmed) return ''
  if (!/^https?:\/\//i.test(trimmed) && !/^\/(?!\/)/.test(trimmed)) throw new Error('后端 URL 必须是 HTTP(S) 地址或以 / 开头的路径')
  let url
  try { url = new URL(trimmed, 'https://worklog.invalid') } catch { throw new Error('后端 URL 无效') }
  if (url.username || url.password || url.search || url.hash || trimmed.includes('\\')) throw new Error('后端 URL 不允许凭证、查询参数、片段或反斜杠')
  return trimmed === '/' ? '/' : trimmed.replace(/\/+$/, '')
}

// 统一请求超时与错误格式；仅同源请求携带浏览器凭证，不在前端存储 MCP Token。
export async function requestJSON(path, { method = 'GET', body, binary = false, zipBody = false } = {}) {
  const base = getBackendURL()
  if (!base) throw new Error('未配置后端')
  const controller = new AbortController()
  const timeout = setTimeout(() => controller.abort(), 15000)
  try {
    const response = await fetch(`${base === '/' ? '' : base}${path}`, {
      method, signal: controller.signal, credentials: 'same-origin', cache: 'no-store',
      headers: { Accept: binary ? 'application/zip' : 'application/json', ...(body === undefined ? {} : { 'Content-Type': zipBody ? 'application/zip' : 'application/json' }) },
      ...(body === undefined ? {} : { body: zipBody ? body : JSON.stringify(body) }),
    })
    if (!response.ok) {
      let detail
      try { detail = await response.json() } catch { /* 非 JSON 错误页 */ }
      const error = new Error(detail?.error?.message || `后端请求失败（HTTP ${response.status}）`)
      error.status = response.status
      error.code = detail?.error?.code
      error.requestId = detail?.requestId || response.headers?.get('X-Request-ID') || undefined
      throw error
    }
    return binary ? await response.blob() : await response.json()
  } catch (error) {
    // 超时只表示客户端没有收到结果，不代表服务端未保存，不能盲目重试写入。
    if (error.name === 'AbortError') throw new Error('后端请求超时，请重新读取确认保存结果')
    throw error
  } finally { clearTimeout(timeout) }
}
