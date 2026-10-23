import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { EditorContent } from '@tiptap/vue-3'
import WorklogEditor from '../../src/components/WorklogEditor.vue'
import { ElMessageBox } from 'element-plus'

vi.mock('element-plus', async importOriginal => ({
  ...await importOriginal(),
  ElMessage: { success: vi.fn(), error: vi.fn() },
  ElMessageBox: { confirm: vi.fn(async () => {}) },
}))

const empty = { type: 'doc', content: [{ type: 'paragraph' }] }
const doc = text => ({ type: 'doc', content: [{ type: 'paragraph', content: [{ type: 'text', text }] }] })
const entry = (date, version = 0, content = empty) => ({ date, version, content, updatedAt: version ? '2026-10-21T00:00:00.123Z' : null })
const response = (body, status = 200) => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
function deferred() { let resolve, reject; const promise = new Promise((a, b) => { resolve = a; reject = b }); return { promise, resolve, reject } }
let wrapper
function open(date) {
  wrapper = mount(WorklogEditor, { props: { date }, attachTo: document.body, global: { stubs: { ElColorPicker: true, ElDialog: true } } })
  return wrapper
}
async function edit(text) {
  wrapper.findComponent(EditorContent).props('editor').commands.setContent(doc(text), { emitUpdate: true })
  await nextTick()
}
const button = text => wrapper.findAll('button').find(button => button.text() === text)

beforeEach(() => {
  globalThis.WORKLOG_CONFIG = { apiBaseUrl: '/api' }
  vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout', 'setInterval', 'clearInterval', 'Date'] })
})
afterEach(() => {
  wrapper?.unmount(); wrapper = null
  vi.useRealTimers(); vi.unstubAllGlobals()
  delete globalThis.WORKLOG_CONFIG
  document.body.innerHTML = ''
})

describe('工作日志真实组件事件流', () => {
  it('首次 /today 失败后重试仍请求服务端今天，而非浏览器日期', async () => {
    const fetch = vi.fn().mockResolvedValueOnce(response({ error: { message: '暂不可用' } }, 503)).mockResolvedValueOnce(response(entry('2026-10-21')))
    vi.stubGlobal('fetch', fetch)
    open('2026-10-20')
    await flushPromises()
    await button('重试').trigger('click')
    await flushPromises()
    expect(fetch.mock.calls.map(call => call[0])).toEqual(['/api/today', '/api/today'])
    expect(wrapper.emitted('loaded').at(-1)).toEqual(['2026-10-21'])
  })

  it('旧日期保存失败不污染仍在加载的新日期，旧草稿仍保留', async () => {
    const put = deferred(), next = deferred()
    vi.stubGlobal('fetch', vi.fn((url, options) => {
      if (options.method === 'PUT') return put.promise
      if (url.endsWith('/2026-10-23')) return next.promise
      return Promise.resolve(response(entry('2026-10-22')))
    }))
    open('2026-10-22'); await flushPromises(); await edit('旧日期草稿')
    await wrapper.setProps({ date: '2026-10-23' })
    put.resolve(response({ error: { code: 'INTERNAL_ERROR', message: '旧日期保存错误' } }, 500))
    await flushPromises()
    next.resolve(response(entry('2026-10-23')))
    await flushPromises()
    expect(wrapper.text()).not.toContain('旧日期保存错误')
    await wrapper.setProps({ date: '2026-10-22' }); await flushPromises()
    expect(wrapper.text()).toContain('旧日期草稿')
    expect(wrapper.text()).toContain('旧日期保存错误')
  })

  it('409 暂停定时和失焦保存；确认放弃草稿后读取新版本才能恢复写入', async () => {
    let puts = 0, reads = 0
    const fetch = vi.fn(async (url, options) => {
      if (options.method === 'PUT') {
        puts++
        if (puts === 1) return response({ error: { code: 'VERSION_CONFLICT', message: '版本冲突' }, requestId: 'req_conflict' }, 409)
        expect(JSON.parse(options.body).version).toBe(2)
        return response(entry('2026-10-24', 3, doc('重新编辑')))
      }
      reads++
      if (reads === 2) throw new TypeError('重新读取失败')
      return response(entry('2026-10-24', reads === 1 ? 1 : 2, doc('服务器内容')))
    })
    vi.stubGlobal('fetch', fetch)
    open('2026-10-24'); await flushPromises(); await edit('必须保留的草稿')
    await vi.advanceTimersByTimeAsync(10_000); await flushPromises()
    expect(puts).toBe(1)
    expect(wrapper.text()).toContain('版本冲突')
    await edit('继续编辑也不能自动覆盖')
    const editor = wrapper.findComponent(EditorContent).props('editor')
    editor.emit('blur', { editor, event: new FocusEvent('blur') })
    await vi.advanceTimersByTimeAsync(30_000); await flushPromises()
    expect(puts).toBe(1)
    expect(button('保存').element.disabled).toBe(true)
    await button('重新加载（先复制草稿）').trigger('click'); await flushPromises()
    expect(ElMessageBox.confirm).toHaveBeenCalled()
    expect(wrapper.text()).toContain('重新读取失败')
    const leaving = new Event('beforeunload', { cancelable: true })
    window.dispatchEvent(leaving)
    expect(leaving.defaultPrevented).toBe(true) // failed reload has not discarded the draft
    await button('重试').trigger('click'); await flushPromises()
    expect(wrapper.text()).toContain('服务器内容')
    expect(wrapper.text()).not.toContain('继续编辑也不能自动覆盖')
    const reloaded = wrapper.findComponent(EditorContent).props('editor')
    expect(reloaded.can().undo()).toBe(false)
    expect(reloaded.can().redo()).toBe(false)
    await edit('重新编辑')
    await button('保存').trigger('click'); await flushPromises()
    expect(puts).toBe(2)
  })

  it('超时和其他失败也不自动重放写请求，允许用户手动重试', async () => {
    let puts = 0
    vi.stubGlobal('fetch', vi.fn(async (url, options) => {
      if (options.method === 'PUT') {
        puts++
        if (puts === 1) throw new TypeError('网络断开')
        return response(entry('2026-10-25', 1, doc('内容')))
      }
      return response(entry('2026-10-25'))
    }))
    open('2026-10-25'); await flushPromises(); await edit('内容')
    await button('保存').trigger('click'); await flushPromises()
    await vi.advanceTimersByTimeAsync(30_000); await flushPromises()
    expect(puts).toBe(1)
    expect(wrapper.text()).toContain('自动保存已暂停')
    await button('保存').trigger('click'); await flushPromises()
    expect(puts).toBe(2)
  })
})

it('初次加载和切日重建编辑历史，同一天的新编辑仍可撤销重做', async () => {
  vi.stubGlobal('fetch', vi.fn(async url => response(entry(url.endsWith('/today') || url.endsWith('/2026-11-01') ? '2026-11-01' : '2026-11-02', 1, doc(url.endsWith('/today') || url.endsWith('/2026-11-01') ? 'DAY A' : 'DAY B')))))
  open('2026-11-01'); await flushPromises()
  const first = wrapper.findComponent(EditorContent).props('editor')
  expect(first.can().undo()).toBe(false)
  await wrapper.setProps({ date: '2026-11-02' }); await flushPromises()
  expect(first.isDestroyed).toBe(true)
  const second = wrapper.findComponent(EditorContent).props('editor')
  expect(second.getText()).toBe('DAY B')
  expect(second.commands.undo()).toBe(false)
  expect(second.commands.redo()).toBe(false)
  await edit('edited B')
  expect(second.commands.undo()).toBe(true)
  expect(second.getText()).toBe('DAY B')
  expect(second.commands.redo()).toBe(true)
  expect(second.getText()).toBe('edited B')
})

it.each([0, 2])('被丢弃的 B v%s 响应不能改变正在编辑的 B v1 基准', async staleVersion => {
  const oldRequest = deferred()
  let bReads = 0
  const fetch = vi.fn(async (url, options) => {
    if (options.method === 'PUT') return response({ error: { code: 'VERSION_CONFLICT', message: '版本冲突' } }, 409)
    if (url.endsWith('/2026-11-04')) {
      if (++bReads === 1) return oldRequest.promise
      return response(entry('2026-11-04', 1, doc('B v1')))
    }
    return response(entry('2026-11-03', 1, doc('A')))
  })
  vi.stubGlobal('fetch', fetch)
  open('2026-11-03'); await flushPromises()
  await wrapper.setProps({ date: '2026-11-04' }); await flushPromises()
  await wrapper.setProps({ date: '2026-11-03' }); await flushPromises()
  await wrapper.setProps({ date: '2026-11-04' }); await flushPromises()
  await edit('my edit of v1')
  oldRequest.resolve(response(entry('2026-11-04', staleVersion, doc('ignored content'))))
  await flushPromises()
  expect(wrapper.findComponent(EditorContent).props('editor').getText()).toBe('my edit of v1')
  await button('保存').trigger('click'); await flushPromises()
  const put = fetch.mock.calls.find(([, options]) => options.method === 'PUT')
  expect(JSON.parse(put[1].body).version).toBe(1)
  expect(wrapper.text()).toContain('版本冲突')
})

it('卸载后返回的加载不再创建编辑器或发出 loaded 事件', async () => {
  const pending = deferred()
  vi.stubGlobal('fetch', vi.fn(() => pending.promise))
  open('2026-11-05')
  const old = wrapper
  wrapper.unmount(); wrapper = null
  pending.resolve(response(entry('2026-11-05', 1, doc('late'))))
  await flushPromises()
  expect(old.emitted('loaded')).toBeUndefined()
})
