import { expect, it } from 'vitest'
import { shallowMount } from '@vue/test-utils'
import TaskBoard from '../../src/components/TaskBoard.vue'

function mountBoard() {
  return shallowMount(TaskBoard, { props: { lanes: [{ id: 'a', name: 'A', tasks: [
    { id: 1, title: '一', status: 'planned' },
    { id: '2', title: '二', status: 'planned' },
  ] }] }, global: { renderStubDefaultSlot: true } })
}
function transfer() {
  let value = ''
  return { setData: (_, data) => { value = data }, getData: () => value }
}

it('卡片上下半区显示插入提示，投放只发出一次排序意图并清理拖拽状态', async () => {
  const wrapper = mountBoard()
  try {
    const dataTransfer = transfer()
    const source = wrapper.get('[data-task-id="1"]')
    const target = wrapper.get('[data-task-id="2"]')
    target.element.getBoundingClientRect = () => ({ top: 100, height: 80 })
    await source.trigger('dragstart', { dataTransfer })
    await target.trigger('dragover', { dataTransfer, clientY: 110 })
    expect(target.classes()).toContain('drop-before')
    await target.trigger('dragover', { dataTransfer, clientY: 170 })
    expect(target.classes()).toContain('drop-after')
    await target.trigger('drop', { dataTransfer, clientY: 170 })
    expect(wrapper.emitted('move')).toEqual([[{ id: '1', projectId: 'a', status: 'planned', placement: { targetId: '2', position: 'after' } }]])
    expect(source.classes()).not.toContain('is-dragging')
    expect(target.classes()).not.toContain('drop-after')
    expect(wrapper.emitted('edit')).toBeUndefined()
  } finally { wrapper.unmount() }
})

it('空列投放为追加，取消拖拽清理提示，外部文本拖入不发出移动', async () => {
  const wrapper = mountBoard()
  try {
    const dataTransfer = transfer()
    const source = wrapper.get('[data-task-id="1"]')
    const column = wrapper.get('[data-status="done"]')
    await column.trigger('drop', { dataTransfer })
    expect(wrapper.emitted('move')).toBeUndefined()
    await source.trigger('dragstart', { dataTransfer })
    await column.trigger('dragover', { dataTransfer })
    expect(column.classes()).toContain('is-drop-target')
    await source.trigger('dragend')
    expect(column.classes()).not.toContain('is-drop-target')
    await source.trigger('dragstart', { dataTransfer })
    await column.trigger('drop', { dataTransfer })
    expect(wrapper.emitted('move')[0][0]).toEqual({ id: '1', projectId: 'a', status: 'done', placement: { targetId: null } })
  } finally { wrapper.unmount() }
})
