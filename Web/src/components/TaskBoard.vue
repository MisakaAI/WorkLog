<script setup>
import { computed, ref, watch } from 'vue'
import { RiArrowRightSLine, RiCheckLine, RiFileListLine } from '@remixicon/vue'
import { ElButton, ElCard, ElCollapse, ElCollapseItem, ElEmpty, ElTag } from 'element-plus'
import Icon from './Icon.vue'
import { columns } from '../dashboard'

const props = defineProps({ lanes: { type: Array, default: () => [] }, view: { type: String, default: '看板' } })
const emit = defineEmits(['edit', 'create', 'move'])
const expanded = ref([])
const draggedTask = ref(null)
const dropTarget = ref('')
const dropPlacement = ref(null)
function previewDrop(event, lane, status) {
  if (draggedTask.value === null) return
  event.dataTransfer.dropEffect = 'move'
  dropTarget.value = columnKey(lane, status)
  const card = event.target.closest('[data-task-id]')
  if (card && event.currentTarget.contains(card)) {
    const bounds = card.getBoundingClientRect()
    dropPlacement.value = { targetId: card.dataset.taskId, position: event.clientY < bounds.top + bounds.height / 2 ? 'before' : 'after' }
  } else {
    dropPlacement.value = { targetId: null }
  }
}
function leaveColumn(event) {
  if (!event.currentTarget.contains(event.relatedTarget)) {
    dropTarget.value = ''
    dropPlacement.value = null
  }
}
function insertionClass(task, lane, status) {
  if (dropTarget.value !== columnKey(lane, status) || draggedTask.value === task.id) return ''
  if (String(dropPlacement.value?.targetId) === String(task.id)) return `drop-${dropPlacement.value.position}`
  const tasks = lane.tasks.filter(item => item.status === status && item.id !== draggedTask.value)
  return dropPlacement.value?.targetId === null && tasks.at(-1)?.id === task.id ? 'drop-after' : ''
}
const columnKey = (lane, status) => `${laneKey(lane)}:${status}`
function startDrag(event, task) {
  draggedTask.value = task.id
  event.dataTransfer.setData('text/plain', String(task.id))
  event.dataTransfer.effectAllowed = 'move'
}
function endDrag() {
  draggedTask.value = null
  dropTarget.value = ''
  dropPlacement.value = null
}
const laneKey = lane => lane.id ? `project:${lane.id}` : 'unassigned'
// 筛选变化时保留已有分组的折叠选择，仅将新出现的分组默认展开。
watch(() => props.lanes.map(laneKey), (keys, previous = []) => {
  expanded.value = [...expanded.value.filter(key => keys.includes(key)), ...keys.filter(key => !previous.includes(key))]
}, { immediate: true })
const tagClass = value => ({ 产品: 'blue', 设计: 'purple', 开发: 'green', 调研: 'orange', 个人: 'neutral', 临时: 'neutral' }[value] || 'neutral')
const priorityClass = value => ({ 低: 'low', 中: 'medium', 高: 'high', 极高: 'urgent' }[value])
const statusTitle = status => columns.find(column => column.id === status)?.title
// 完成率仅统计当前筛选结果，不将“进行中”折算为虚构的完成百分比。
const laneProgress = computed(() => new Map(props.lanes.map(lane => {
  const completed = lane.tasks.filter(task => task.status === 'done').length
  return [laneKey(lane), { completed, percent: lane.tasks.length ? Math.round(completed / lane.tasks.length * 100) : 0 }]
})))
// 子组件只发出移动意图；父组件统一校验、修改数据并保存。
function drop(event, lane, status) {
  if (draggedTask.value === null) return
  previewDrop(event, lane, status)
  const id = event.dataTransfer.getData('text/plain')
  if (id === String(draggedTask.value)) emit('move', { id, projectId: lane.id, status, placement: dropPlacement.value })
  endDrag()
}
</script>

<template>
  <ElCollapse v-model="expanded" class="project-lanes">
    <ElCollapseItem v-for="lane in lanes" :key="laneKey(lane)" :name="laneKey(lane)" class="project-lane" :data-project-id="lane.id">
      <template #title>
        <div class="lane-heading"><span class="lane-monogram" aria-hidden="true">{{ lane.name.slice(0, 1).toUpperCase() }}</span><h2>{{ lane.name }}</h2><span class="column-count">{{ lane.tasks.length }} 项任务</span></div>
      </template>
      <div v-if="view === '看板'" class="kanban-columns">
        <section v-for="column in columns" :key="column.id" class="kanban-column" :class="{ 'is-drop-target': dropTarget === columnKey(lane, column.id) }" :aria-label="`${lane.name} · ${column.title}`" :data-status="column.id" :data-project-id="lane.id" @dragover.prevent="previewDrop($event, lane, column.id)" @dragleave="leaveColumn" @drop.prevent="drop($event, lane, column.id)">
          <div class="column-heading">
            <span class="status-dot" :class="column.id"></span><h3>{{ column.title }}</h3>
            <span class="column-count">{{ lane.tasks.filter(task => task.status === column.id).length }}</span>
            <ElButton text class="icon-button" :aria-label="`添加${lane.name}的${column.title}任务`" :title="`添加${column.title}任务`" @click="emit('create', { status: column.id, projectId: lane.id })"><Icon name="Add" :size="18" /></ElButton>
          </div>
          <div class="column-cards">
            <ElCard v-for="task in lane.tasks.filter(task => task.status === column.id)" :key="task.id" class="task-card" :class="[{ 'completed-card': task.status === 'done', 'is-dragging': draggedTask === task.id }, `card-color-${task.color}`, insertionClass(task, lane, column.id)]" shadow="never" body-class="task-card-body" role="button" tabindex="0" :aria-label="`编辑任务：${task.title}`" :data-task-id="task.id" draggable="true" @dragstart="startDrag($event, task)" @dragend="endDrag" @click="emit('edit', task)" @keydown.enter.prevent="emit('edit', task)" @keydown.space.prevent="emit('edit', task)">
              <div class="card-labels"><ElTag class="tag" :class="tagClass(task.tag)" size="small">{{ task.tag }}</ElTag><ElTag class="priority" :class="priorityClass(task.priority)" size="small"><Icon v-if="['高', '极高'].includes(task.priority)" name="Flag" :size="12" />{{ task.priority }}</ElTag></div>
              <h4 class="task-title">{{ task.title }}</h4>
              <p v-if="task.description" class="task-description">{{ task.description }}</p>
              <div v-if="task.startDate || task.date" class="card-date"><Icon name="CalendarEvent" :size="14" />{{ task.startDate || '未设开始' }} → {{ task.date || '未设截止' }}</div>
            </ElCard>
            <p v-if="!lane.tasks.some(task => task.status === column.id)" class="column-empty">暂无任务</p>
            <ElButton text class="add-card" @click="emit('create', { status: column.id, projectId: lane.id })"><Icon name="Add" :size="16" />添加任务</ElButton>
          </div>
        </section>
      </div>
      <section v-else class="task-list" :aria-label="`${lane.name}任务列表`">
        <div class="list-summary">
          <span class="list-summary-label"><RiFileListLine size="16" aria-hidden="true" />任务清单<small>当前筛选结果</small></span>
          <div class="list-completion">
            <span>已完成 <strong>{{ laneProgress.get(laneKey(lane)).completed }}</strong> / {{ lane.tasks.length }}</span>
            <span class="list-completion-track" role="progressbar" :aria-label="`${lane.name}当前任务完成率`" :aria-valuenow="laneProgress.get(laneKey(lane)).percent" :aria-valuetext="`当前 ${lane.tasks.length} 项任务中已完成 ${laneProgress.get(laneKey(lane)).completed} 项`" :aria-valuemin="0" :aria-valuemax="100"><span :style="{ width: `${laneProgress.get(laneKey(lane)).percent}%` }"></span></span>
            <strong>{{ laneProgress.get(laneKey(lane)).percent }}%</strong>
          </div>
        </div>
        <div v-if="lane.tasks.length" class="list-columns" aria-hidden="true"><span>任务详情</span><span>分类 / 优先级</span><span>状态</span><span>起止日期</span><span></span></div>
        <ul class="list-rows">
          <li v-for="task in lane.tasks" :key="task.id">
            <ElButton class="list-task" :class="[`card-color-${task.color}`, { 'is-complete': task.status === 'done' }]" :aria-label="`编辑任务：${task.title}，${statusTitle(task.status)}`" :data-task-id="task.id" @click="emit('edit', task)">
              <span class="list-task-content">
                <span class="list-task-main">
                  <span class="list-task-icon" :class="task.status" aria-hidden="true"><RiCheckLine v-if="task.status === 'done'" size="18" /><RiFileListLine v-else size="18" /></span>
                  <span class="list-task-copy"><span class="list-title">{{ task.title }}</span><span v-if="task.description" class="list-description">{{ task.description }}</span></span>
                </span>
                <span class="list-labels"><ElTag class="tag" :class="tagClass(task.tag)" size="small">{{ task.tag }}</ElTag><ElTag class="priority" :class="priorityClass(task.priority)" size="small"><Icon v-if="['高', '极高'].includes(task.priority)" name="Flag" :size="12" />{{ task.priority }}</ElTag></span>
                <span class="list-status task-status-pill" :class="task.status"><span class="status-dot" :class="task.status" aria-hidden="true"></span>{{ statusTitle(task.status) }}</span>
                <span class="list-date"><span><small>起</small><time v-if="task.startDate" :datetime="task.startDate">{{ task.startDate }}</time><span v-else class="list-date-unset">未设开始</span></span><span><small>止</small><time v-if="task.date" :datetime="task.date">{{ task.date }}</time><span v-else class="list-date-unset">未设截止</span></span></span>
                <span class="list-open" aria-hidden="true"><RiArrowRightSLine size="18" /></span>
              </span>
            </ElButton>
          </li>
        </ul>
        <ElEmpty v-if="!lane.tasks.length" :image-size="48" description="暂无任务，试试新建任务或调整筛选。" />
        <div class="list-footer"><span>点击任务查看详情与编辑</span><ElButton text class="list-add-task" @click="emit('create', { status: 'planned', projectId: lane.id })"><Icon name="Add" :size="16" />添加任务</ElButton></div>
      </section>
    </ElCollapseItem>
  </ElCollapse>
</template>
