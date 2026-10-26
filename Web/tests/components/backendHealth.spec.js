import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { useBackendHealth } from '../../src/composables/useBackendHealth.js'
import { getDashboard, saveDashboard, emptyDashboard } from '../../src/api/index.js'

let wrapper
let state
const Probe = {
  setup() { state = useBackendHealth(); return state },
  template: '<span :data-backend-status="backendStatus" :title="backendStatusLabel">.</span>',
}
const response = (body = { ok: true }) => ({ ok: true, json: async () => body })
const open = () => { wrapper = mount(Probe); return wrapper }
beforeEach(() => {
  vi.useFakeTimers()
  vi.stubGlobal('WORKLOG_CONFIG', { apiBaseUrl: '/api' })
  vi.stubGlobal('fetch', vi.fn(async () => response()))
})
afterEach(() => {
  wrapper?.unmount()
  wrapper = null
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

it('未配置后端时不发出健康请求、不启动轮询，保持原状态', async () => {
  globalThis.WORKLOG_CONFIG.apiBaseUrl = ''
  open(); await flushPromises()
  expect(state.backendStatus.value).toBe('unconfigured')
  expect(wrapper.attributes('title')).toContain('当前浏览器')
  await vi.advanceTimersByTimeAsync(90_000)
  expect(fetch).not.toHaveBeenCalled()
  expect(vi.getTimerCount()).toBe(0)
})

it.each([
  ['/api', '/api/healthz'],
  ['/services/api/', '/services/api/healthz'],
  ['https://backend.example.com/worklog/api/', 'https://backend.example.com/worklog/api/healthz'],
  ['/', '/healthz'],
])('使用业务 API 配置 %s 检查连接，检查期间保留原色', async (base, url) => {
  globalThis.WORKLOG_CONFIG.apiBaseUrl = base
  open()
  expect(state.backendStatus.value).toBe('checking')
  await flushPromises()
  expect(state.backendStatus.value).toBe('connected')
  expect(fetch).toHaveBeenCalledExactlyOnceWith(url, expect.objectContaining({
    method: 'GET', credentials: 'same-origin', cache: 'no-store',
  }))
  expect(wrapper.attributes('title')).toContain('不代表本次保存成功')
})

it.each([
  ['认证失败', () => ({ ok: false, status: 401, json: async () => ({}) })],
  ['数据库不可用', () => ({ ok: false, status: 503, json: async () => ({}) })],
  ['响应为 false', () => response({ ok: false })],
  ['缺少健康字段', () => response({})],
  ['错误字段类型', () => response({ ok: 'true' })],
  ['空响应', () => response(null)],
  ['HTML 响应', () => ({ ok: true, json: async () => { throw new SyntaxError('HTML') } })],
  ['网络错误', () => { throw new TypeError('Failed to fetch') }],
])('%s 显示失败；30 秒后恢复，后续轮询不闪回检查中', async (_name, fail) => {
  fetch.mockImplementationOnce(fail)
  open(); await flushPromises()
  expect(state.backendStatus.value).toBe('disconnected')
  expect(wrapper.attributes('title')).toContain('不会切换')
  await vi.advanceTimersByTimeAsync(29_999)
  expect(fetch).toHaveBeenCalledTimes(1)
  await vi.advanceTimersByTimeAsync(1)
  expect(state.backendStatus.value).toBe('connected')
  expect(fetch).toHaveBeenCalledTimes(2)
  let finish
  fetch.mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
  await vi.advanceTimersByTimeAsync(30_000)
  expect(state.backendStatus.value).toBe('connected')
  finish(response()); await flushPromises()
})

it('连接超时显示失败，慢请求不会重叠，卸载后停止轮询', async () => {
  fetch.mockImplementation((_url, { signal }) => new Promise((_resolve, reject) => {
    signal.addEventListener('abort', () => reject(new DOMException('aborted', 'AbortError')))
  }))
  open()
  await vi.advanceTimersByTimeAsync(14_999)
  expect(state.backendStatus.value).toBe('checking')
  expect(fetch).toHaveBeenCalledTimes(1)
  await vi.advanceTimersByTimeAsync(1)
  expect(state.backendStatus.value).toBe('disconnected')
  await vi.advanceTimersByTimeAsync(30_000)
  expect(fetch).toHaveBeenCalledTimes(2)
  wrapper.unmount(); wrapper = null
  await vi.advanceTimersByTimeAsync(90_000)
  expect(fetch).toHaveBeenCalledTimes(2)
  expect(vi.getTimerCount()).toBe(0)
})

it('卸载后忽略未完成请求的结果，不再安排检查', async () => {
  let finish
  fetch.mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
  open()
  wrapper.unmount(); wrapper = null
  finish(response()); await flushPromises()
  expect(state.backendStatus.value).toBe('checking')
  expect(vi.getTimerCount()).toBe(0)
})

it('检查成功后也能检测到断开，健康状态绝不改变数据源或操作本地业务缓存', async () => {
  const read = vi.spyOn(Storage.prototype, 'getItem')
  const write = vi.spyOn(Storage.prototype, 'setItem')
  open(); await flushPromises()
  expect(state.backendStatus.value).toBe('connected')
  fetch.mockImplementation(async () => ({ ok: false, status: 503, json: async () => ({}) }))
  await vi.advanceTimersByTimeAsync(30_000)
  expect(state.backendStatus.value).toBe('disconnected')
  await expect(getDashboard()).rejects.toThrow('HTTP 503')
  await expect(saveDashboard({ ...emptyDashboard(), revision: 0 })).rejects.toThrow('HTTP 503')
  expect(read).not.toHaveBeenCalled()
  expect(write).not.toHaveBeenCalled()
})
