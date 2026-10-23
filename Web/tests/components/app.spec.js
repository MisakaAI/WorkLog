import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { flushPromises, shallowMount } from '@vue/test-utils'
import App from '../../src/App.vue'
import GanttChart from '../../src/components/GanttChart.vue'
import WorklogEditor from '../../src/components/WorklogEditor.vue'
import { emptyCalendar, emptyDashboard, getDashboard, getWorkCalendar, saveDashboard } from '../../src/api/index.js'

vi.mock('../../src/api/index.js', async importOriginal => ({
  ...await importOriginal(), getDashboard: vi.fn(), getWorkCalendar: vi.fn(), saveDashboard: vi.fn(),
}))
vi.mock('../../src/api/worklog.js', async importOriginal => ({ ...await importOriginal(), getWorklogDates: vi.fn(async () => []) }))
vi.mock('element-plus', async importOriginal => ({ ...await importOriginal(), ElMessage: { error: vi.fn(), warning: vi.fn() } }))

let wrapper
const slot = { template: '<div><slot /></div>' }
function open() {
  wrapper = shallowMount(App, { global: {
    renderStubDefaultSlot: true,
    stubs: {
      ElConfigProvider: slot, ElDialog: slot,
      ElDatePickerPanel: { props: ['modelValue'], template: '<div class="date-panel-stub" />' },
      RiAddFill: true, RiFilterLine: true, RiFilterOffLine: true,
      ElButton: { props: ['disabled', 'nativeType'], template: '<button :type="nativeType || \'button\'" :disabled="disabled"><slot /></button>' },
      ElInput: { props: ['modelValue'], emits: ['update:modelValue'], template: '<input :value="modelValue" @input="$emit(\'update:modelValue\', $event.target.value)" />' },
      ElForm: { emits: ['submit'], template: '<form @submit.prevent="$emit(\'submit\', $event)"><slot /></form>' },
    },
  } })
  return wrapper
}
beforeEach(() => {
  window.matchMedia = vi.fn(() => ({ matches: false, addEventListener() {}, removeEventListener() {} }))
  globalThis.WORKLOG_CONFIG = { businessTimezone: 'Asia/Shanghai' }
  getDashboard.mockResolvedValue(emptyDashboard())
  getWorkCalendar.mockResolvedValue(emptyCalendar())
  saveDashboard.mockImplementation(async data => ({ ...data, revision: data.revision + 1 }))
})
afterEach(() => { wrapper?.unmount(); wrapper = null; vi.useRealTimers(); delete globalThis.WORKLOG_CONFIG })

it('工作台加载失败后真实点击重试恢复，不渲染空工作台代替失败', async () => {
  getDashboard.mockRejectedValueOnce(new Error('读取失败'))
  open(); await flushPromises()
  expect(wrapper.text()).toContain('读取失败')
  expect(wrapper.findComponent(WorklogEditor).exists()).toBe(false)
  await wrapper.findAll('button').find(button => button.text() === '重新加载').trigger('click')
  await flushPromises()
  expect(wrapper.findComponent(WorklogEditor).exists()).toBe(true)
})

it('待办保存失败回滚工作台，保留输入以便重试', async () => {
  saveDashboard.mockRejectedValueOnce(new Error('保存失败'))
  open(); await flushPromises()
  const input = wrapper.find('input[aria-label="新待办"]')
  await input.setValue('不能丢的待办')
  await wrapper.find('form.add-todo').trigger('submit'); await flushPromises()
  expect(wrapper.findAll('.todo-item')).toHaveLength(0)
  expect(input.element.value).toBe('不能丢的待办')
  await wrapper.find('form.add-todo').trigger('submit'); await flushPromises()
  expect(wrapper.findAll('.todo-item')).toHaveLength(1)
  expect(input.element.value).toBe('')
})

it('日历只加载一次，日期面板与甘特图共享失败状态和重试结果', async () => {
  getWorkCalendar.mockRejectedValueOnce(new Error('日历失败'))
  open(); await flushPromises()
  expect(getWorkCalendar).toHaveBeenCalledTimes(1)
  expect(wrapper.find('.date-panel').text()).toContain('节假日配置加载失败')
  expect(wrapper.findComponent(GanttChart).props('calendarError')).toBe(true)
  wrapper.findComponent(GanttChart).vm.$emit('retry-calendar')
  await flushPromises()
  expect(getWorkCalendar).toHaveBeenCalledTimes(2)
  expect(wrapper.findComponent(GanttChart).props('calendarError')).toBe(false)
  expect(wrapper.findComponent(GanttChart).props('calendar')).toEqual(emptyCalendar())
  expect(wrapper.find('.date-panel').text()).not.toContain('节假日配置加载失败')
})

it('跨业务时区午夜更新今天与甘特图，但不悄悄切换正在编辑的日期', async () => {
  vi.useFakeTimers({ toFake: ['Date', 'setInterval', 'clearInterval'] })
  vi.setSystemTime(new Date('2026-10-20T15:59:50Z'))
  open(); await flushPromises()
  expect(wrapper.findComponent(WorklogEditor).props('date')).toBe('2026-10-20')
  vi.setSystemTime(new Date('2026-10-20T16:00:10Z'))
  await vi.advanceTimersByTimeAsync(30_000)
  expect(wrapper.findComponent(GanttChart).props('today')).toBe('2026-10-21')
  expect(wrapper.findComponent(WorklogEditor).props('date')).toBe('2026-10-20')
  expect(wrapper.text()).toContain('当前仍在编辑 2026-10-20')
  await wrapper.findAll('button').find(button => button.text() === '切换到今天').trigger('click')
  expect(wrapper.findComponent(WorklogEditor).props('date')).toBe('2026-10-21')
})
