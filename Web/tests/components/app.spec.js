import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { flushPromises, shallowMount } from '@vue/test-utils'
import App from '../../src/App.vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import GanttChart from '../../src/components/GanttChart.vue'
import WorklogEditor from '../../src/components/WorklogEditor.vue'
import { emptyCalendar, emptyDashboard, getDashboard, getWorkCalendar, saveDashboard } from '../../src/api/index.js'

vi.mock('../../src/api/index.js', async importOriginal => ({
  ...await importOriginal(), getDashboard: vi.fn(), getWorkCalendar: vi.fn(), saveDashboard: vi.fn(),
}))
vi.mock('../../src/api/worklog.js', async importOriginal => ({ ...await importOriginal(), getWorklogDates: vi.fn(async () => []) }))
vi.mock('element-plus', async importOriginal => ({ ...await importOriginal(), ElMessage: { error: vi.fn(), warning: vi.fn() }, ElMessageBox: { prompt: vi.fn(), confirm: vi.fn() } }))

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
  ElMessageBox.prompt.mockRejectedValue('cancel')
  window.matchMedia = vi.fn(() => ({ matches: false, addEventListener() {}, removeEventListener() {} }))
  globalThis.WORKLOG_CONFIG = { businessTimezone: 'Asia/Shanghai' }
  getDashboard.mockResolvedValue({ ...emptyDashboard(), revision: 0 })
  getWorkCalendar.mockResolvedValue(emptyCalendar())
  saveDashboard.mockImplementation(async data => ({ ...data, revision: data.revision + 1 }))
})
afterEach(() => { wrapper?.unmount(); wrapper = null; vi.useRealTimers(); delete globalThis.WORKLOG_CONFIG })

it('项目管理默认未归档，可归档、查看归档、取消归档及删除，保存完整快照', async () => {
  const active = { id: 'p', name: '当前项目' }
  const archived = { id: 'a', name: '旧项目', archived: true }
  const snapshot = { ...emptyDashboard(), revision: 0, projects: [active] }
  getDashboard.mockResolvedValueOnce(snapshot).mockResolvedValue({ ...snapshot, includeArchived: true, projects: [active, archived], tasks: [{ id: 'archived-task', projectId: 'a', status: 'done' }] })
  open(); await flushPromises()
  await wrapper.findAll('button').find(button => button.text() === '项目管理').trigger('click')
  await flushPromises()
  expect(wrapper.find('input[aria-label="项目名称：当前项目"]').exists()).toBe(true)
  expect(wrapper.find('input[aria-label="项目名称：旧项目"]').exists()).toBe(false)
  expect(wrapper.findAll('.project-row-actions button').map(button => button.attributes('title'))).toEqual(['归档', '导出项目', '删除项目'])
  expect(wrapper.find('.project-dialog-actions > button').text()).toBe('全部导出')
  await wrapper.find('button[aria-label="归档项目：当前项目"]').trigger('click')
  expect(wrapper.find('input[aria-label="项目名称：当前项目"]').exists()).toBe(false)
  expect(wrapper.findComponent({ name: 'ElTabs' }).props('modelValue')).toBe('active')
  wrapper.findComponent({ name: 'ElTabs' }).vm.$emit('tab-change', 'archived')
  await flushPromises()
  expect(wrapper.findAll('.project-form .project-edit-row')).toHaveLength(2)
  expect(wrapper.find('button[aria-label="删除项目：旧项目"]').exists()).toBe(true)
  await wrapper.find('button[aria-label="取消归档项目：当前项目"]').trigger('click')
  expect(wrapper.find('input[aria-label="项目名称：当前项目"]').exists()).toBe(false)
  ElMessageBox.prompt.mockResolvedValueOnce({ value: '旧项目' })
  await wrapper.find('button[aria-label="删除项目：旧项目"]').trigger('click')
  await flushPromises()
  expect(saveDashboard).toHaveBeenCalledTimes(1)
  expect(saveDashboard).toHaveBeenLastCalledWith(expect.objectContaining({ revision: 0, includeArchived: true, projects: [active], tasks: [] }))
  await wrapper.find('form.project-form').trigger('submit'); await flushPromises()
  expect(saveDashboard).toHaveBeenLastCalledWith(expect.objectContaining({ revision: 1, includeArchived: true, projects: [expect.objectContaining({ id: 'p', archived: false })] }))
})

it('删除项目要求前端精确校验项目名称，取消或名称不匹配均不删除', async () => {
  const project = { id: 'p', name: 'Project [A].*' }
  getDashboard.mockResolvedValue({ ...emptyDashboard(), revision: 0, projects: [project] })
  open(); await flushPromises()
  await wrapper.findAll('button').find(button => button.text() === '项目管理').trigger('click')
  await flushPromises()
  const remove = () => wrapper.find('button[title="删除项目"]').trigger('click')
  await remove(); await flushPromises()
  expect(wrapper.find('button[title="删除项目"]').exists()).toBe(true)
  const [message, title, options] = ElMessageBox.prompt.mock.calls[0]
  expect(title).toBe('删除项目')
  expect(message).toContain('立即删除该项目及其所有任务')
  for (const name of ['', null, 'project [A].*', 'Project A', 'Project [A].* ']) {
    expect(options.inputValidator(name)).not.toBe(true)
  }
  expect(options.inputValidator(project.name)).toBe(true)
  ElMessageBox.prompt.mockResolvedValueOnce({ value: '错误名称' })
  await remove(); await flushPromises()
  expect(wrapper.find('button[title="删除项目"]').exists()).toBe(true)
  expect(saveDashboard).not.toHaveBeenCalled()
})

it('确认后立即删除所有状态的关联任务，保留其他项目和未分配任务，不顺带保存其他草稿', async () => {
  const projects = [{ id: 'p', name: '待删除' }, { id: 'q', name: '保留项目' }]
  const tasks = [
    ...['planned', 'progress', 'done'].map((status, index) => ({ id: `p${index}`, projectId: 'p', status })),
    { id: 'other', projectId: 'q', status: 'planned' },
    { id: 'unassigned', projectId: '', status: 'planned' },
  ]
  getDashboard.mockResolvedValue({ ...emptyDashboard(), revision: 0, projects, tasks })
  open(); await flushPromises()
  await wrapper.findAll('button').find(button => button.text() === '项目管理').trigger('click')
  await flushPromises()
  await wrapper.find('input[aria-label="项目名称：保留项目"]').setValue('尚未保存的新名称')
  ElMessageBox.prompt.mockResolvedValueOnce({ value: '待删除' })
  await wrapper.find('button[aria-label="删除项目：待删除"]').trigger('click')
  await flushPromises()
  expect(wrapper.find('button[aria-label="删除项目：待删除"]').exists()).toBe(false)
  expect(saveDashboard).toHaveBeenCalledTimes(1)
  expect(saveDashboard).toHaveBeenLastCalledWith(expect.objectContaining({ projects: [projects[1]], tasks: tasks.slice(3) }))
  expect(wrapper.find('input[aria-label="项目名称：尚未保存的新名称"]').exists()).toBe(true)
  await wrapper.find('form.project-form').trigger('submit'); await flushPromises()
  expect(saveDashboard).toHaveBeenLastCalledWith(expect.objectContaining({ revision: 1, projects: [{ id: 'q', name: '尚未保存的新名称' }], tasks: tasks.slice(3) }))
})

it('确认删除后取消项目管理不会撤销删除，重新打开项目不再出现', async () => {
  getDashboard.mockResolvedValue({ ...emptyDashboard(), revision: 0, projects: [{ id: 'p', name: '项目' }] })
  open(); await flushPromises()
  const manage = () => wrapper.findAll('button').find(button => button.text() === '项目管理').trigger('click')
  await manage(); await flushPromises()
  ElMessageBox.prompt.mockResolvedValueOnce({ value: '项目' })
  await wrapper.find('button[title="删除项目"]').trigger('click'); await flushPromises()
  expect(saveDashboard).toHaveBeenCalledTimes(1)
  getDashboard.mockResolvedValue(await saveDashboard.mock.results[0].value)
  await wrapper.find('.project-dialog-actions .cancel-button').trigger('click')
  await manage(); await flushPromises()
  expect(wrapper.find('button[aria-label="删除项目：项目"]').exists()).toBe(false)
  expect(saveDashboard).toHaveBeenCalledTimes(1)
})

it('删除失败保留项目和草稿，可重试且不会提前更新快照版本', async () => {
  getDashboard.mockResolvedValue({ ...emptyDashboard(), revision: 4, projects: [{ id: 'p', name: '项目' }] })
  saveDashboard.mockRejectedValueOnce(new Error('版本冲突'))
  open(); await flushPromises()
  await wrapper.findAll('button').find(button => button.text() === '项目管理').trigger('click'); await flushPromises()
  ElMessageBox.prompt.mockResolvedValue({ value: '项目' })
  await wrapper.find('button[title="删除项目"]').trigger('click'); await flushPromises()
  expect(wrapper.find('button[title="删除项目"]').exists()).toBe(true)
  expect(ElMessage.error).toHaveBeenCalledWith('删除失败：版本冲突')
  await wrapper.find('button[title="删除项目"]').trigger('click'); await flushPromises()
  expect(wrapper.find('button[title="删除项目"]').exists()).toBe(false)
  expect(saveDashboard).toHaveBeenNthCalledWith(2, expect.objectContaining({ revision: 4, projects: [], tasks: [] }))
})

it('删除尚未保存的新项目仅移除草稿，不提交其他修改', async () => {
  open(); await flushPromises()
  await wrapper.findAll('button').find(button => button.text() === '项目管理').trigger('click'); await flushPromises()
  await wrapper.find('input[aria-label="新项目名称"]').setValue('新项目')
  await wrapper.find('.project-add button').trigger('click')
  ElMessageBox.prompt.mockResolvedValueOnce({ value: '新项目' })
  await wrapper.find('button[title="删除项目"]').trigger('click'); await flushPromises()
  expect(wrapper.find('button[title="删除项目"]').exists()).toBe(false)
  expect(saveDashboard).not.toHaveBeenCalled()
})

it('删除请求未完成时阻止重复删除和保存，完成后立即更新工作台', async () => {
  getDashboard.mockResolvedValue({ ...emptyDashboard(), revision: 0, projects: [{ id: 'p', name: '项目' }] })
  let finish
  saveDashboard.mockImplementationOnce(data => new Promise(resolve => { finish = () => resolve({ ...data, revision: 1 }) }))
  open(); await flushPromises()
  await wrapper.findAll('button').find(button => button.text() === '项目管理').trigger('click'); await flushPromises()
  ElMessageBox.prompt.mockResolvedValueOnce({ value: '项目' })
  await wrapper.find('button[title="删除项目"]').trigger('click'); await flushPromises()
  expect(wrapper.find('button[title="删除项目"]').attributes('disabled')).toBeDefined()
  await wrapper.find('form.project-form').trigger('submit'); await flushPromises()
  expect(saveDashboard).toHaveBeenCalledTimes(1)
  finish(); await flushPromises()
  expect(wrapper.find('button[title="删除项目"]').exists()).toBe(false)
  // 后续普通写入使用删除成功后的版本和数据，不会恢复已删除项目。
  await wrapper.find('input[aria-label="新待办"]').setValue('后续待办')
  await wrapper.find('form.add-todo').trigger('submit'); await flushPromises()
  expect(saveDashboard).toHaveBeenLastCalledWith(expect.objectContaining({ revision: 1, projects: [] }))
})

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
