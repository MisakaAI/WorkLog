import { expect, it } from 'vitest'
import { shallowMount } from '@vue/test-utils'
import TaskBoard from '../../src/components/TaskBoard.vue'
import GanttChart from '../../src/components/GanttChart.vue'

it('项目 __unassigned__ 与合成未分配泳道拥有独立展开状态', async () => {
  const wrapper = shallowMount(TaskBoard, { props: { lanes: [
    { id: '__unassigned__', name: '合法项目', tasks: [] },
    { id: '', name: '未分配', tasks: [] },
  ] }, global: { renderStubDefaultSlot: true } })
  try {
    const collapse = wrapper.findComponent({ name: 'ElCollapse' })
    expect(collapse.props('modelValue')).toEqual(['project:__unassigned__', 'unassigned'])
    collapse.vm.$emit('update:modelValue', ['unassigned'])
    await wrapper.setProps({ view: '列表' })
    expect(collapse.props('modelValue')).toEqual(['unassigned'])
  } finally { wrapper.unmount() }
})

it('甘特图使用父级日历、今天和错误状态，不自行加载独立副本', async () => {
  const wrapper = shallowMount(GanttChart, { props: { today: '2026-10-20', calendarError: true } })
  try {
    expect(wrapper.text()).toContain('节假日配置加载失败')
    wrapper.findAllComponents({ name: 'ElButton' }).find(button => button.props('link')).vm.$emit('click')
    expect(wrapper.emitted('retry-calendar')).toHaveLength(1)
    await wrapper.setProps({ today: '2026-10-21', calendarError: false })
    expect(wrapper.text()).not.toContain('节假日配置加载失败')
    // The reactive day range also moves across a week boundary.
    await wrapper.setProps({ today: '2026-10-26' })
    expect(wrapper.text()).toContain('2026-10-26 — 2026-11-01')
  } finally { wrapper.unmount() }
})
