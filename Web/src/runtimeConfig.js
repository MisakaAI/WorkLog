import { normalizeBackendURL } from './api/client.js'

export function validateRuntimeConfig(value) {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error('config.json 必须为 JSON 对象')
  for (const key of Object.keys(value)) {
    if (!['apiBaseUrl', 'businessTimezone'].includes(key)) throw new Error(`config.json 包含未知字段：${key}`)
  }
  const config = {}
  if (Object.hasOwn(value, 'apiBaseUrl')) config.apiBaseUrl = normalizeBackendURL(value.apiBaseUrl)
  if (Object.hasOwn(value, 'businessTimezone')) {
    const zone = value.businessTimezone
    if (typeof zone !== 'string' || !zone.trim()) throw new Error('businessTimezone 必须为有效时区字符串')
    try { new Intl.DateTimeFormat('en', { timeZone: zone }) } catch { throw new Error('businessTimezone 必须为有效时区字符串') }
    config.businessTimezone = zone
  }
  return config
}

// 地址相对于页面而非打包后的 assets/ 模块，支持 /worklog/ 子目录发布。
export async function loadRuntimeConfig({
  configURL = new URL('./config.json', document.baseURI),
  fetchImpl = globalThis.fetch,
  timeoutMs = 5000,
} = {}) {
  const controller = new AbortController()
  const timeout = setTimeout(() => controller.abort(), timeoutMs)
  let value
  try {
    let response
    try {
      response = await fetchImpl(configURL, { cache: 'no-store', credentials: 'same-origin', signal: controller.signal })
    } catch (error) {
      if (error.name === 'AbortError') throw new Error('读取 config.json 超时，已阻止应用启动')
      throw new Error('读取 config.json 网络失败，已阻止应用启动')
    }
    // 缺失、认证失败或服务端错误都不能误判为未配置后端。
    if (!response.ok) throw new Error(`读取 config.json 失败（HTTP ${response.status}），已阻止应用启动`)
    try { value = await response.json() } catch (error) {
      if (error.name === 'AbortError') throw new Error('读取 config.json 超时，已阻止应用启动')
      throw new Error('config.json 不是有效 JSON，已阻止应用启动')
    }
  } finally { clearTimeout(timeout) }
  return validateRuntimeConfig(value)
}

export async function startApplication({
  loadApp = () => import('./main.js'),
  onConfigLoaded = () => {},
  ...options
} = {}) {
  // 并行读取配置和加载应用资源，避免 config → JS/CSS 的串行瀑布。
  // main.js 仅导出挂载函数，模块求值不会读取业务数据。
  const [config, app] = await Promise.all([
    loadRuntimeConfig(options).then(config => {
      onConfigLoaded()
      return config
    }),
    Promise.resolve().then(loadApp),
  ])
  // 唯一用户配置源为公开 JSON；全局对象仅是兼容现有 API/日期组件的内部载体。
  globalThis.WORKLOG_CONFIG = config
  await app.mountApplication()
}
