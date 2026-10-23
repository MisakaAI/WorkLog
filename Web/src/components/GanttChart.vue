<script setup>
import { computed, ref, watch } from 'vue'
import { ElButton, ElSelect, ElOption } from 'element-plus'
import { RiArrowLeftSLine, RiArrowRightSLine, RiCalendarScheduleLine, RiCheckLine, RiFileListLine } from '@remixicon/vue'
import { getCalendarDay } from '../workCalendar'
import { columns, dateToDay, dayToDate, todayISO, taskSchedule, ganttPlacement, ganttRange } from '../dashboard'

const props = defineProps({
  tasks: { type: Array, default: () => [] }, projects: { type: Array, default: () => [] },
  today: { type: String, default: todayISO }, calendar: { type: Object, default: null },
  calendarLoading: Boolean, calendarError: Boolean,
})
const projectName = id => props.projects.find(project => project.id === id)?.name || '未分配'
const emit = defineEmits(['edit', 'retry-calendar'])
const today = computed(() => dateToDay(props.today))
const view = ref('week')
const periodOffset = ref(0)
watch(view, () => { periodOffset.value = 0 })
const range = computed(() => ganttRange(view.value, today.value, periodOffset.value))
const rangeStart = computed(() => range.value.start)
const dayCount = computed(() => range.value.dayCount)
const calendar = computed(() => props.calendar)
const days = computed(() => Array.from({ length: dayCount.value }, (_, i) => {
  const day = rangeStart.value + i
  const date = dayToDate(day)
  const weekday = new Date(`${date}T00:00:00Z`).getUTCDay()
  const info = getCalendarDay(date, calendar.value)
  const type = info.isWorkday ? 'workday' : 'restday'
  const status = info.isWorkday ? '工作日' : '休息日'
  return { date, label: `${Number(date.slice(5, 7))}/${Number(date.slice(8))}`, weekday: '日一二三四五六'[weekday], type, title: `${date} 周${'日一二三四五六'[weekday]} · ${status}`, today: day === today.value }
}))
const scheduled = computed(() => props.tasks.flatMap(task => {
  const schedule = taskSchedule(task)
  return schedule ? [{ ...task, schedule, placement: ganttPlacement(task, rangeStart.value, dayCount.value) }] : []
}))
const period = computed(() => `${dayToDate(rangeStart.value)} — ${dayToDate(rangeStart.value + dayCount.value - 1)}`)
// 在日期格内留出呼吸空间，不改变排期的日期边界。
function barStyle(placement) { return { left: `calc(${placement.left}% + 4px)`, width: `calc(${placement.width}% - 8px)` } }
const statusLabel = status => columns.find(column => column.id === status)?.title || '待开始'
function scheduleLabel(task) {
  const start = dayToDate(task.schedule.start)
  const end = dayToDate(task.schedule.end)
  return start === end ? start : `${start} → ${end}`
}
</script>

<template>
  <section class="gantt-panel" aria-label="项目进度">
    <div class="gantt-heading">
      <div class="gantt-period">
        <span class="gantt-period-icon" aria-hidden="true"><RiCalendarScheduleLine size="20" /></span>
        <div><strong>{{ period }}</strong><small>任务排期 · 已安排 {{ scheduled.length }} 项任务</small></div>
      </div>
      <div class="gantt-actions">
        <div class="gantt-legend" aria-label="工作日历图例"><span><i class="workday"></i>工作日</span><span><i class="restday"></i>休息日</span></div>
        <div class="gantt-controls">
          <div class="gantt-navigation" role="group" aria-label="切换时间段">
            <ElButton class="gantt-nav-button" size="small" aria-label="上一时间段" @click="periodOffset--"><RiArrowLeftSLine size="18" aria-hidden="true" /></ElButton>
            <ElButton class="gantt-today-button" size="small" @click="periodOffset = 0">现在</ElButton>
            <ElButton class="gantt-nav-button" size="small" aria-label="下一时间段" @click="periodOffset++"><RiArrowRightSLine size="18" aria-hidden="true" /></ElButton>
          </div>
          <ElSelect v-model="view" size="small" aria-label="甘特图时间范围"><ElOption value="week" label="周" /><ElOption value="month" label="月" /><ElOption value="15days" label="15天" /><ElOption value="30days" label="30天" /></ElSelect>
        </div>
      </div>
    </div>
    <p v-if="calendarLoading" class="gantt-calendar-status" role="status">正在加载工作日历…</p>
    <div v-else-if="calendarError" class="gantt-calendar-status" role="status">节假日配置加载失败，暂按周一至周五工作、周末休息显示。<ElButton link type="primary" @click="emit('retry-calendar')">重试</ElButton></div>
    <div v-if="scheduled.length" class="gantt-scroll" tabindex="0" aria-label="任务排期，可横向滚动">
      <div class="gantt-table" :style="{ minWidth: `calc(var(--gantt-task-width) + ${dayCount * 44}px)`, '--gantt-days': dayCount }" role="table" aria-label="任务日期安排">
        <div class="gantt-header gantt-table-row" role="row">
          <div class="gantt-task-name gantt-task-heading" role="columnheader"><span>任务 / 起止日期</span><span class="count-badge">{{ scheduled.length }}</span></div>
          <div class="gantt-days" role="columnheader" aria-label="日期">
            <span v-for="day in days" :key="day.date" :class="[day.type, { today: day.today }]" :data-date="day.date" :title="day.title" :aria-label="day.title" :aria-current="day.today ? 'date' : undefined"><small>{{ day.today ? '今天' : `周${day.weekday}` }}</small><span class="gantt-day-number">{{ day.label }}</span></span>
          </div>
        </div>
        <div v-for="task in scheduled" :key="task.id" class="gantt-table-row gantt-task-row" :class="`gantt-color-${task.color}`" role="row">
          <div class="gantt-task-name" role="cell">
            <ElButton link :title="`${projectName(task.projectId)} · ${task.title} · ${statusLabel(task.status)}`" :aria-label="`编辑任务：${task.title}，${statusLabel(task.status)}，${scheduleLabel(task)}`" @click="emit('edit', task)">
              <span class="gantt-task-title"><span class="gantt-task-icon" :class="{ 'is-complete': task.status === 'done' }" aria-hidden="true"><RiCheckLine v-if="task.status === 'done'" size="15" /><RiFileListLine v-else size="15" /></span><strong>{{ task.title }}</strong></span>
              <small class="gantt-task-meta"><span class="gantt-project-name">{{ projectName(task.projectId) }}</span><span class="gantt-task-status task-status-pill" :class="task.status"><i class="status-dot" :class="task.status" aria-hidden="true"></i>{{ statusLabel(task.status) }}</span></small>
              <small class="gantt-task-dates">{{ scheduleLabel(task) }}</small>
            </ElButton>
          </div>
          <div class="gantt-track" role="cell">
            <div class="gantt-cells" aria-hidden="true"><span v-for="day in days" :key="day.date" :class="[day.type, { today: day.today }]" :data-date="day.date"></span></div>
            <ElButton v-if="task.placement" class="gantt-bar" :class="`card-color-${task.color}`" :style="barStyle(task.placement)" :title="`${task.title}：${scheduleLabel(task)}`" :aria-label="`编辑排期：${task.title}，${scheduleLabel(task)}`" @click="emit('edit', task)">{{ task.title }}</ElButton>
          </div>
        </div>
      </div>
    </div>
    <div v-else class="gantt-empty">
      <span class="gantt-empty-icon" aria-hidden="true"><RiCalendarScheduleLine size="28" /></span>
      <strong>暂无排期任务</strong>
      <p>为任务设置开始或截止日期，即可在这里查看时间安排。</p>
    </div>
    <div v-if="scheduled.length" class="gantt-footer"><span>点击任务或排期条，查看与编辑任务</span><span>{{ dayCount }} 天视图 · 可横向滚动</span></div>
  </section>
</template>
