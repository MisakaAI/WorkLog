import { afterEach, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import WorklogEditor from '../../src/components/WorklogEditor.vue'

const sourceImport = vi.hoisted(() => {
  let resolve
  const promise = new Promise(done => { resolve = done })
  return { resolve, promise, requested: vi.fn() }
})
vi.mock('../../src/components/SourceCodeEditor.vue', async () => {
  sourceImport.requested()
  return sourceImport.promise
})

let wrapper

afterEach(() => {
  wrapper?.unmount()
  vi.unstubAllGlobals()
  delete globalThis.WORKLOG_CONFIG
})

it('仅打开源码弹窗时加载编辑器，等待期间立即显示启动动画，完成后替换为编辑器', async () => {
  globalThis.WORKLOG_CONFIG = { apiBaseUrl: '/api' }
  vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({
    date: '2026-11-05', version: 0,
    content: { type: 'doc', content: [{ type: 'paragraph' }] }, updatedAt: null,
  }), { status: 200, headers: { 'Content-Type': 'application/json' } })))
  wrapper = mount(WorklogEditor, {
    props: { date: '2026-11-05' },
    global: { stubs: {
      ElColorPicker: true,
      ElDialog: {
        props: ['modelValue'],
        template: '<div v-if="modelValue"><slot /></div>',
      },
    } },
  })
  await flushPromises()
  expect(sourceImport.requested).not.toHaveBeenCalled()
  expect(wrapper.find('.source-code-loading').exists()).toBe(false)

  await wrapper.findAll('button').find(button => button.text() === '源代码').trigger('click')
  const loading = wrapper.get('.source-code-loading')
  expect(loading.isVisible()).toBe(true)
  expect(loading.attributes('role')).toBe('status')
  expect(loading.text()).toContain('正在加载源码编辑器')
  expect(loading.find('.startup-loader__ring .startup-loader__arc').exists()).toBe(true)
  expect(loading.get('img').attributes('src')).toBe(`${import.meta.env.BASE_URL}favicon.svg`)
  await flushPromises()
  expect(sourceImport.requested).toHaveBeenCalledTimes(1)
  expect(wrapper.find('.source-code-loading').isVisible()).toBe(true)

  sourceImport.resolve({ __esModule: true, default: { template: '<div class="resolved-source-editor">HTML editor</div>' } })
  await flushPromises()
  expect(wrapper.find('.source-code-loading').exists()).toBe(false)
  expect(wrapper.get('.resolved-source-editor').isVisible()).toBe(true)
})
