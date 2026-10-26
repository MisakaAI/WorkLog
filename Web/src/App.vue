<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import {
  ElButton, ElCheckbox, ElConfigProvider, ElDatePickerPanel, ElDialog, ElEmpty,
  ElForm, ElFormItem, ElInput, ElSelect, ElOption, ElMessage, ElMessageBox,
  ElRadioGroup, ElRadioButton, ElTabs, ElTabPane,
} from 'element-plus'
import { RiAddFill, RiFilterLine, RiFilterOffLine, RiInboxArchiveLine, RiInboxUnarchiveLine, RiUpload2Line, RiDownload2Line } from '@remixicon/vue'
import zhCn from 'element-plus/es/locale/lang/zh-cn'
import 'dayjs/locale/zh-cn'
import { getWorklogDates, hasWorklogContent } from './api/worklog'
import Icon from './components/Icon.vue'
import TaskBoard from './components/TaskBoard.vue'
import TaskDatePicker from './components/TaskDatePicker.vue'
import GanttChart from './components/GanttChart.vue'
import WorklogEditor from './components/WorklogEditor.vue'
import { getDashboard, saveDashboard, visibleDashboard } from './api'
import { getBackendURL } from './api/client'
import { exportProjects, importProjects, downloadProjects } from './api/projectTransfer'
import { getCalendarDay } from './workCalendar'
import { useWorkCalendar } from './composables/useWorkCalendar'
import { useToday } from './composables/useToday'
import { newID } from './id'
import {
  ALL_PROJECTS, columns, tags, priorities, cardColors, replaceProjects,
  filterTasks, todayISO, validateDates, validateProjects, projectLanes, moveTaskToLane,
  validateCategories, replaceCategories,
} from './dashboard'
import { useTheme } from './composables/useTheme'
import { useDraftSort } from './composables/useDraftSort'
import { useBackendHealth } from './composables/useBackendHealth'

const { isDark, toggleTheme } = useTheme()
const { backendStatus, backendStatusLabel } = useBackendHealth()
const data = ref(null)
const error = ref('')
const persisting = ref(false)
// 记录服务端最后确认的快照；本地先更新界面，保存失败时整体回退。
let committed = null
function warnPendingSave(event) {
  if (!persisting.value) return
  event.preventDefault()
  event.returnValue = ''
}
window.addEventListener('beforeunload', warnPendingSave)
onBeforeUnmount(() => window.removeEventListener('beforeunload', warnPendingSave))
const editingPersonalLabel = ref(false)
const personalLabelDraft = ref('')
const personalLabelInput = ref(null)
const personalLabel = computed(() => data.value?.personalLabel ?? '个人工作台')
async function editPersonalLabel() {
  if (!data.value) return
  personalLabelDraft.value = personalLabel.value
  editingPersonalLabel.value = true
  await nextTick()
  personalLabelInput.value?.focus()
  personalLabelInput.value?.select()
}
async function savePersonalLabel(event) {
  if (event?.isComposing || !editingPersonalLabel.value || persisting.value) return
  if (personalLabelDraft.value.length > 255) return ElMessage.warning('工作台文字不能超过 255 个字')
  const previous = data.value.personalLabel
  data.value.personalLabel = personalLabelDraft.value.trim()
  if (!await persist()) {
    if (previous === undefined) delete data.value.personalLabel
    else data.value.personalLabel = previous
    return
  }
  editingPersonalLabel.value = false
}
function cancelPersonalLabel() {
  editingPersonalLabel.value = false
}
const search = ref('')
const filtersVisible = ref(false)
const activeTab = ref('看板')
const tag = ref('')
const selectedProject = ref(ALL_PROJECTS)
const priorityOnly = ref(false)
const todoFilter = ref('待完成')
const newTodo = ref('')
const today = useToday()
const selectedDate = ref(today.value)
const dayChanged = ref(false)
watch(today, () => { dayChanged.value = true })
const { workCalendar, calendarLoading, calendarError, loadCalendar } = useWorkCalendar()
const worklogDates = ref(new Set())
// 暂存本页面的日志保存结果，防止较晚返回的日期列表覆盖最新标记。
const savedWorklogDates = new Map()
function onWorklogSaved({ date, content }) {
  const exists = hasWorklogContent(content)
  savedWorklogDates.set(date, exists)
  const dates = new Set(worklogDates.value)
  if (exists) dates.add(date)
  else dates.delete(date)
  worklogDates.value = dates
}
onMounted(async () => {
  try {
    const dates = new Set(await getWorklogDates())
    // 请求期间的新保存优先于日期列表快照。
    for (const [date, exists] of savedWorklogDates) {
      if (exists) dates.add(date)
      else dates.delete(date)
    }
    worklogDates.value = dates
  } catch { /* 日期标记加载失败不影响编辑或选择日期 */ }
})
const dialog = ref(false)
const editingId = ref(null)
const projectDialog = ref(false)
const draftProjects = ref([])
const showArchivedProjects = ref(false)
const projectSnapshot = ref(null)
const hasBackend = Boolean(getBackendURL())
const transferringProjects = ref(false)
const projectImportInput = ref(null)
async function downloadProject(projectId) {
  if (transferringProjects.value || persisting.value) return
  transferringProjects.value = true
  try { downloadProjects(await exportProjects(projectId)) }
  catch (e) { ElMessage.error(`导出失败：${e.message}`) }
  finally { transferringProjects.value = false }
}
async function uploadProjects(event) {
  const file = event.target.files?.[0]
  event.target.value = ''
  if (!file || transferringProjects.value || persisting.value) return
  try {
    await ElMessageBox.confirm('导入会追加项目和关联任务，同名项目自动重命名，不会覆盖已有数据。当前项目管理中未保存的修改将被丢弃。是否继续？', '导入项目', { confirmButtonText: '导入', cancelButtonText: '取消' })
  } catch { return }
  if (persisting.value || transferringProjects.value) return
  transferringProjects.value = true
  persisting.value = true
  try {
    const saved = await importProjects(file)
    data.value = visibleDashboard(saved)
    committed = JSON.parse(JSON.stringify(data.value))
    projectSnapshot.value = saved
    draftProjects.value = saved.projects.map(project => ({ ...project }))
    newProjectName.value = ''
    resetProjectDrag()
    ElMessage.success('项目导入成功')
  } catch (e) { ElMessage.error(`导入失败：${e.message}`) }
  finally { transferringProjects.value = false; persisting.value = false }
}
const visibleDraftProjects = computed({
  get: () => draftProjects.value.filter(project => Boolean(project.archived) === showArchivedProjects.value),
  set: projects => {
    let index = 0
    draftProjects.value = draftProjects.value.map(project => Boolean(project.archived) === showArchivedProjects.value ? projects[index++] : project)
  },
})
function toggleProjectArchive(project) {
  resetProjectDrag()
  project.archived = !project.archived
}
const newProjectName = ref('')
const {
  draggedId: draggedProject, dropIndex: projectDropIndex, reset: resetProjectDrag,
  start: startProjectDrag, preview: previewProjectDrop, leave: leaveProjectList, drop: dropProject, move: moveProject,
} = useDraftSort(visibleDraftProjects, 'project')
const categoryDialog = ref(false)
const draftCategories = ref([])
const {
  draggedId: draggedCategory, dropIndex: categoryDropIndex, reset: resetCategoryDrag,
  start: startCategoryDrag, preview: previewCategoryDrop, leave: leaveCategoryList,
  drop: dropCategory, move: moveCategory,
} = useDraftSort(draftCategories, 'category')
const newCategoryName = ref('')
const categories = computed(() => data.value?.categories ?? tags)
const taskForm = reactive({ title: '', description: '', projectId: '', status: 'planned', tag: '开发', priority: '中', startDate: '', date: '', color: 'white' })
const hasActiveFilters = computed(() => Boolean(search.value || tag.value || priorityOnly.value || selectedProject.value !== ALL_PROJECTS))
const tasks = computed(() => filterTasks(data.value?.tasks || [], { search: search.value, tag: tag.value, priorityOnly: priorityOnly.value, projectId: selectedProject.value }))
const lanes = computed(() => projectLanes(tasks.value, data.value?.projects || [], selectedProject.value))
const visibleTodos = computed(() => (data.value?.todos || []).filter(todo => todoFilter.value === '全部' || (todoFilter.value === '待完成' ? !todo.done : todo.done)))
const taskCounts = computed(() => columns.map(column => ({ ...column, count: (data.value?.tasks || []).filter(task => task.status === column.id).length })))
const todayLabel = computed(() => new Intl.DateTimeFormat('zh-CN', { timeZone: 'UTC', year: 'numeric', month: 'long', day: 'numeric', weekday: 'long' }).format(new Date(`${today.value}T12:00:00Z`)).replace(/日/, '日 '))
let categoryDraftID = 0
function allocateID() {
  try { return newID() }
  catch (e) { ElMessage.error(e.message); return null }
}
const pendingTodos = computed(() => data.value?.todos.filter(todo => !todo.done).length || 0)

function openTask(task = null, status = 'planned', projectId = selectedProject.value === ALL_PROJECTS ? '' : selectedProject.value) {
  editingId.value = task?.id ?? null
  Object.assign(taskForm, {
    title: task?.title || '', description: task?.description || '', status: task?.status || status,
    projectId: task ? task.projectId : projectId, tag: task?.tag || (categories.value.includes('开发') ? '开发' : categories.value[0]), priority: task?.priority || '中',
    startDate: task ? task.startDate : todayISO(), date: task?.date || '', color: task?.color || 'white',
  })
  dialog.value = true
}
// 工作台使用完整快照保存，期间阻止再次修改，避免多个请求携带同一 revision。
// 不自动重试冲突：旧快照不能直接覆盖其他页面或 MCP 刚保存的数据。
async function persist() {
  if (persisting.value) return false
  persisting.value = true
  try {
    data.value = await saveDashboard(JSON.parse(JSON.stringify(data.value)))
    committed = JSON.parse(JSON.stringify(data.value))
    return true
  } catch (e) {
    data.value = JSON.parse(JSON.stringify(committed))
    ElMessage.error(`保存失败：${e.message}`)
    return false
  } finally { persisting.value = false }
}
async function saveTask() {
  if (persisting.value) return
  if (!taskForm.title.trim()) return ElMessage.warning('请输入任务名称')
  const dateError = validateDates(taskForm.startDate, taskForm.date)
  if (dateError) return ElMessage.warning(dateError)
  const fields = { ...taskForm, title: taskForm.title.trim(), description: taskForm.description.trim(), startDate: taskForm.startDate || '', date: taskForm.date || '' }
  if (editingId.value !== null) {
    const task = data.value.tasks.find(task => task.id === editingId.value)
    if (!task) return
    Object.assign(task, fields)
  } else {
    const id = allocateID()
    if (!id) return
    data.value.tasks.push({ ...fields, id })
  }
  if (await persist()) dialog.value = false
}
async function deleteTask() {
  try {
    await ElMessageBox.confirm('删除后无法恢复，确定删除这个任务？', '删除任务', {
      confirmButtonText: '删除', cancelButtonText: '取消', type: 'warning',
    })
  } catch { return }
  if (persisting.value) return
  data.value.tasks = data.value.tasks.filter(task => task.id !== editingId.value)
  if (await persist()) dialog.value = false
}
function openCategories() {
  resetCategoryDrag()
  draftCategories.value = categories.value.map(name => ({ id: `category:${++categoryDraftID}`, originalName: name, name }))
  newCategoryName.value = ''
  categoryDialog.value = true
}
function addDraftCategory() {
  const name = newCategoryName.value.trim()
  const message = validateCategories([...draftCategories.value.map(item => item.name), name])
  if (message) return ElMessage.warning(message)
  draftCategories.value.push({ id: `category:${++categoryDraftID}`, originalName: '', name })
  newCategoryName.value = ''
}
async function saveCategories() {
  if (persisting.value) return
  if (newCategoryName.value.trim()) addDraftCategory()
  if (newCategoryName.value.trim()) return
  const message = validateCategories(draftCategories.value.map(item => item.name))
  if (message) return ElMessage.warning(message)
  const selected = draftCategories.value.find(item => item.originalName === tag.value)
  data.value = replaceCategories(data.value, draftCategories.value)
  if (await persist()) {
    if (tag.value) tag.value = selected ? selected.name.trim() : ''
    categoryDialog.value = false
  }
}
async function openProjects() {
  try {
    const snapshot = await getDashboard({ includeArchived: true })
    if (snapshot.revision !== data.value.revision) return ElMessage.warning('数据已被修改，请刷新页面后重试')
    projectSnapshot.value = snapshot
    resetProjectDrag()
    showArchivedProjects.value = false
    draftProjects.value = snapshot.projects.map(project => ({ ...project }))
    newProjectName.value = ''
    projectDialog.value = true
  } catch (e) { ElMessage.error(`读取项目失败：${e.message}`) }
}
function addDraftProject() {
  const name = newProjectName.value.trim()
  if (!name) return ElMessage.warning('请输入项目名称')
  const id = allocateID()
  if (!id) return
  const projects = [...draftProjects.value, { id, name }]
  const message = validateProjects(projects)
  if (message) return ElMessage.warning(message)
  draftProjects.value = projects
  showArchivedProjects.value = false
  newProjectName.value = ''
}
async function deleteDraftProject(project) {
  if (persisting.value || transferringProjects.value) return
  const name = project.name.trim()
  if (!name) return ElMessage.warning('请先填写项目名称')
  try {
    const { value } = await ElMessageBox.prompt(`删除项目“${name}”将立即删除该项目及其所有任务，删除后无法恢复。请输入项目名称以确认删除：`, '删除项目', {
      confirmButtonText: '删除', cancelButtonText: '取消', type: 'warning',
      inputPlaceholder: name,
      inputValidator: value => value === name || '项目名称不匹配，请输入完整的项目名称',
    })
    if (value !== name || persisting.value || transferringProjects.value || !projectDialog.value) return
  } catch { return }
  // 只提交已保存快照中的删除，不顺带保存其他项目的草稿修改。
  const snapshot = projectSnapshot.value
  if (snapshot.projects.some(item => item.id === project.id)) {
    persisting.value = true
    try {
      const saved = await saveDashboard(replaceProjects(snapshot, snapshot.projects.filter(item => item.id !== project.id)))
      projectSnapshot.value = saved
      data.value = visibleDashboard(saved)
      committed = JSON.parse(JSON.stringify(data.value))
      if (selectedProject.value === project.id) selectedProject.value = ALL_PROJECTS
    } catch (e) {
      ElMessage.error(`删除失败：${e.message}`)
      return
    } finally { persisting.value = false }
  }
  resetProjectDrag()
  draftProjects.value = draftProjects.value.filter(item => item.id !== project.id)
}
async function saveProjects() {
  if (persisting.value || transferringProjects.value) return
  if (newProjectName.value.trim()) addDraftProject()
  if (newProjectName.value.trim()) return
  const message = validateProjects(draftProjects.value)
  if (message) return ElMessage.warning(message)
  persisting.value = true
  try {
    const saved = await saveDashboard(replaceProjects(projectSnapshot.value, draftProjects.value))
    data.value = visibleDashboard(saved)
    committed = JSON.parse(JSON.stringify(data.value))
    if (selectedProject.value !== ALL_PROJECTS && selectedProject.value !== '' && !data.value.projects.some(project => project.id === selectedProject.value)) selectedProject.value = ALL_PROJECTS
    projectDialog.value = false
  } catch (e) { ElMessage.error(`保存失败：${e.message}`) }
  finally { persisting.value = false }
}
async function addTodo() {
  if (persisting.value || !newTodo.value.trim()) return
  const id = allocateID()
  if (!id) return
  data.value.todos.push({ id, title: newTodo.value.trim(), done: false })
  if (await persist()) newTodo.value = ''
}
function deleteTodo(id) {
  if (persisting.value) return
  data.value.todos = data.value.todos.filter(todo => todo.id !== id)
  persist()
}
function onMove({ id, status, projectId, placement }) {
  if (persisting.value) return
  if (moveTaskToLane(data.value, id, status, projectId, placement)) persist()
}
function resetFilters() {
  search.value = ''
  tag.value = ''
  selectedProject.value = ALL_PROJECTS
  priorityOnly.value = false
}
async function load() {
  error.value = ''
  try {
    data.value = await getDashboard()
    committed = JSON.parse(JSON.stringify(data.value))
  } catch (e) { error.value = e.message }
}
onMounted(load)
</script>

<template>
  <ElConfigProvider :locale="zhCn">
    <div class="app-shell" :inert="persisting" :aria-busy="persisting">
      <header class="topbar">
        <a class="brand" href="./" aria-label="Worklog 首页"><span class="brand-mark"><img src="/logo.png" alt="" /></span><span class="brand-word">worklog<span :data-backend-status="backendStatus" :title="backendStatusLabel" role="img" :aria-label="backendStatusLabel">.</span></span></a>
        <div class="personal-label">
          <ElInput v-if="editingPersonalLabel" ref="personalLabelInput" v-model="personalLabelDraft" maxlength="255" aria-label="工作台文字" placeholder="输入工作台文字" @blur="savePersonalLabel" @keydown.enter.prevent="savePersonalLabel" @keydown.esc.prevent="cancelPersonalLabel" />
          <button v-else type="button" class="personal-label-button" :disabled="!data" :title="personalLabel || '双击输入工作台文字'" aria-label="双击编辑工作台文字" @dblclick="editPersonalLabel">{{ personalLabel || '双击输入工作台文字' }}</button>
        </div>
        <span v-if="persisting" role="status">正在保存工作台…</span>
        <span class="topbar-date">{{ todayLabel }}</span>
        <ElButton class="theme-toggle icon-button" :aria-pressed="isDark" :aria-label="isDark ? '切换到浅色模式' : '切换到暗色模式'" :title="isDark ? '切换到浅色模式' : '切换到暗色模式'" @click="toggleTheme">
          <Icon :name="isDark ? 'Sun' : 'Moon'" :size="18" />
        </ElButton>
      </header>

      <main v-if="data">
        <div class="dashboard-grid">
          <div class="task-workspace">
            <p v-if="dayChanged && selectedDate !== today" class="workspace-notice panel" role="status">今天已变为 {{ today }}，当前仍在编辑 {{ selectedDate }}。<ElButton link type="primary" @click="selectedDate = today">切换到今天</ElButton></p>
            <WorklogEditor :date="selectedDate" @loaded="selectedDate = $event" @saved="onWorklogSaved" />
            <section class="board-panel panel" aria-label="任务视图">
              <div class="board-toolbar" :class="{ 'filters-collapsed': !filtersVisible }">
                <div class="board-toolbar-heading">
                  <div class="board-title"><span class="board-title-icon"><Icon name="LayoutColumn" :size="18" /></span><h2>任务空间</h2></div>
                  <ElRadioGroup v-model="activeTab" aria-label="任务视图" size="small" class="view-tabs">
                    <ElRadioButton value="看板">看板</ElRadioButton><ElRadioButton value="进度">进度</ElRadioButton><ElRadioButton value="列表">列表</ElRadioButton>
                  </ElRadioGroup>
                </div>
                <div class="board-toolbar-actions">
                  <div class="search-controls">
                    <ElInput v-model="search" class="search-box" placeholder="搜索任务" aria-label="搜索任务" clearable><template #prefix><Icon name="Search" :size="17" /></template></ElInput>
                    <ElButton class="icon-button filter-toggle" :aria-expanded="filtersVisible" aria-controls="task-filters" :aria-label="filtersVisible ? '收起筛选' : '展开筛选'" @click="filtersVisible = !filtersVisible">
                      <RiFilterLine v-if="filtersVisible" size="18" aria-hidden="true" /><RiFilterOffLine v-else size="18" aria-hidden="true" />
                    </ElButton>
                  </div>
                  <ElButton class="management-button" @click="openProjects">项目管理</ElButton><ElButton @click="openCategories">分类管理</ElButton>
                  <ElButton type="primary" class="create-task" @click="openTask()"><RiAddFill size="18" aria-hidden="true" />新建任务</ElButton>
                </div>
              </div>
              <div v-show="filtersVisible" id="task-filters" class="board-filters">
                <span class="filter-label">筛选</span>
                <ElSelect v-model="selectedProject" aria-label="筛选项目" class="project-filter" :empty-values="[null, undefined]">
                  <ElOption label="全部项目" :value="ALL_PROJECTS" /><ElOption v-for="project in data.projects" :key="project.id" :label="project.name" :value="project.id" /><ElOption label="未分配" value="" />
                </ElSelect>
                <ElSelect v-model="tag" aria-label="任务分类" placeholder="全部分类" clearable class="tag-filter"><ElOption v-for="item in categories" :key="item" :value="item" /></ElSelect>
                <ElCheckbox v-model="priorityOnly" class="priority-filter">高优先级</ElCheckbox>
                <span class="filter-summary" role="status">{{ tasks.length }} 项任务</span>
                <ElButton v-if="hasActiveFilters" class="clear-filters" @click="resetFilters">清除筛选</ElButton>
              </div>
              <GanttChart v-show="activeTab === '进度'" :tasks="tasks" :projects="data.projects" :today="today" :calendar="workCalendar" :calendar-loading="calendarLoading" :calendar-error="calendarError" @retry-calendar="loadCalendar" @edit="openTask($event)" />
              <template v-if="activeTab !== '进度'">
                <ElEmpty v-if="!tasks.length" :image-size="64" :description="hasActiveFilters ? '没有找到匹配的任务，试试调整或清除筛选。' : '暂无任务，点击新建任务开始。'" />
                <TaskBoard v-else :lanes="lanes" :view="activeTab" @edit="openTask($event)" @create="openTask(null, $event.status, $event.projectId)" @move="onMove" />
              </template>
            </section>
          </div>

          <aside class="sidebar">
            <div class="calendar-workspace">
              <div class="workspace-overview" aria-label="全部任务概览">
                <div v-for="item in taskCounts" :key="item.id" class="overview-item"><span><i class="status-dot" :class="item.id"></i>{{ item.title }}</span><strong>{{ item.count }}</strong></div>
              </div>
            <section class="date-panel panel" aria-label="日期选择器面板">
              <p v-if="calendarError" class="workspace-notice" role="status">节假日配置加载失败，暂按周规则显示。<ElButton link type="primary" @click="loadCalendar">重试</ElButton></p>
              <ElDatePickerPanel
                v-model="selectedDate"
                type="date"
                value-format="YYYY-MM-DD"
                :border="false"
              >
                <template #default="cell">
                  <div class="el-date-table-cell worklog-date-cell" :class="{ 'has-worklog': worklogDates.has(cell.dayjs.format('YYYY-MM-DD')), 'is-restday': !getCalendarDay(cell.dayjs.format('YYYY-MM-DD'), workCalendar).isWorkday }" :title="worklogDates.has(cell.dayjs.format('YYYY-MM-DD')) ? '有工作日志' : undefined">
                    <span class="el-date-table-cell__text">{{ cell.text }}</span>
                    <span v-if="worklogDates.has(cell.dayjs.format('YYYY-MM-DD')) && cell.dayjs.format('YYYY-MM-DD') !== selectedDate" class="worklog-date-dot" aria-label="有工作日志"></span>
                  </div>
                </template>
              </ElDatePickerPanel>
            </section>
            </div>
          <section class="todo-panel panel" aria-labelledby="todo-heading">
            <div class="section-header"><div class="section-title"><span class="section-icon"><Icon name="ListCheck" :size="19" /></span><h2 id="todo-heading">个人待办</h2></div><span class="count-badge" :aria-label="`${pendingTodos} 项待完成`" :title="`${pendingTodos} 项待完成`">{{ pendingTodos }}</span></div>
            <ElForm class="add-todo" @submit.prevent="addTodo">
              <ElInput v-model="newTodo" placeholder="添加待办…" aria-label="新待办" maxlength="100" />
              <ElButton class="icon-button" native-type="submit" aria-label="添加待办" title="添加待办" :disabled="!newTodo.trim()"><Icon name="Add" /></ElButton>
            </ElForm>
            <ElRadioGroup v-model="todoFilter" class="todo-tabs" aria-label="待办筛选" size="small"><ElRadioButton v-for="filter in ['待完成', '已完成', '全部']" :key="filter" :value="filter">{{ filter }}</ElRadioButton></ElRadioGroup>
            <ul class="todo-items">
              <li v-for="todo in visibleTodos" :key="todo.id" class="todo-item" :class="{ checked: todo.done }">
                <ElCheckbox :model-value="todo.done" @update:model-value="!persisting && (todo.done = $event, persist())">{{ todo.title }}</ElCheckbox>
                <ElButton text class="icon-button delete-todo" :aria-label="`删除待办：${todo.title}`" title="删除待办" @click="deleteTodo(todo.id)"><Icon name="Delete" :size="16" /></ElButton>
              </li>
            </ul>
            <ElEmpty v-if="!visibleTodos.length" :image-size="48" :description="todoFilter === '待完成' ? '暂无待办，专注手头的事。' : '暂无相关待办'" />
          </section>
          </aside>
        </div>
      </main>
      <div v-else class="loading-state panel" role="status"><span class="section-icon"><Icon name="LayoutColumn" :size="24" /></span><h1>{{ error ? '暂时无法打开工作台' : '正在准备你的工作台' }}</h1><p>{{ error || '正在读取任务与个人记录…' }}</p><ElButton v-if="error" type="primary" @click="load">重新加载</ElButton></div>

      <ElDialog v-model="dialog" :title="editingId !== null ? '编辑任务' : '新建任务'" width="min(500px, 92vw)">
        <ElForm :model="taskForm" class="task-form" label-position="top" @submit.prevent="saveTask">
          <ElFormItem label="任务名称" for="task-title"><ElInput id="task-title" v-model="taskForm.title" placeholder="准备做什么？" maxlength="100" /></ElFormItem>
          <ElFormItem label="描述" for="task-description"><ElInput id="task-description" v-model="taskForm.description" type="textarea" :rows="3" placeholder="补充任务细节（选填）" /></ElFormItem>
          <ElFormItem label="所属项目"><ElSelect v-model="taskForm.projectId" aria-label="所属项目" :empty-values="[null, undefined]"><ElOption v-for="project in data?.projects" :key="project.id" :label="project.name" :value="project.id" /><ElOption label="未分配" value="" /></ElSelect></ElFormItem>
          <div class="form-row">
            <ElFormItem label="状态"><ElSelect v-model="taskForm.status" aria-label="任务状态"><ElOption v-for="column in columns" :key="column.id" :label="column.title" :value="column.id" /></ElSelect></ElFormItem>
            <ElFormItem label="分类"><ElSelect v-model="taskForm.tag" aria-label="任务分类"><ElOption v-for="item in categories" :key="item" :value="item" /></ElSelect></ElFormItem>
          </div>
          <div class="form-row">
            <ElFormItem label="开始日期（选填）"><TaskDatePicker v-model="taskForm.startDate" type="date" value-format="YYYY-MM-DD" format="YYYY-MM-DD" aria-label="开始日期" placeholder="选择开始日期" :disabled-date="date => Boolean(taskForm.date && todayISO(date) > taskForm.date)" /></ElFormItem>
            <ElFormItem label="截止日期（选填）"><TaskDatePicker v-model="taskForm.date" type="date" value-format="YYYY-MM-DD" format="YYYY-MM-DD" aria-label="截止日期" placeholder="选择截止日期" :disabled-date="date => Boolean(taskForm.startDate && todayISO(date) < taskForm.startDate)" /></ElFormItem>
          </div>
          <ElFormItem label="优先级"><ElSelect v-model="taskForm.priority" aria-label="优先级"><ElOption v-for="item in priorities" :key="item" :value="item" /></ElSelect></ElFormItem>
          <ElFormItem label="卡片背景"><ElRadioGroup v-model="taskForm.color" class="color-options" aria-label="卡片背景"><ElRadioButton v-for="color in cardColors" :key="color.id" :value="color.id"><span class="color-swatch" :class="`card-color-${color.id}`"></span>{{ color.label }}</ElRadioButton></ElRadioGroup></ElFormItem>
          <div class="dialog-actions">
            <ElButton v-if="editingId !== null" link type="danger" @click="deleteTask">删除任务</ElButton>
            <ElButton class="cancel-button" @click="dialog = false">取消</ElButton><ElButton type="primary" native-type="submit">保存任务</ElButton>
          </div>
        </ElForm>
      </ElDialog>

      <ElDialog v-model="projectDialog" title="项目管理" width="min(620px, 92vw)" @closed="resetProjectDrag">
        <p class="project-help">拖动排序手柄调整项目顺序。归档后项目及其任务不再展示，可在“已归档”中恢复。删除项目需输入项目名称确认，确认后立即删除项目及其所有任务，无法恢复，取消项目管理也不会撤销删除。其他修改点击保存后生效。导出仅包含已保存的项目、关联任务和分类，不包含未分配任务、待办或工作日志。</p>
        <div v-if="hasBackend" class="project-transfer-actions">
          <ElButton v-if="hasBackend" :disabled="transferringProjects || persisting" @click="projectImportInput?.click()"><RiDownload2Line :size="17" aria-hidden="true" />导入项目</ElButton>
          <input v-if="hasBackend" ref="projectImportInput" type="file" accept=".zip,application/zip" aria-label="选择项目 ZIP 文件" hidden @change="uploadProjects" />
        </div>
        <ElForm class="project-form" @submit.prevent="saveProjects">
          <div class="project-add"><ElInput v-model="newProjectName" placeholder="新项目名称" aria-label="新项目名称" maxlength="40" @keydown.enter.prevent="addDraftProject" /><ElButton @click="addDraftProject"><Icon name="Add" :size="16" />添加</ElButton></div>
          <ElTabs class="project-status-tabs" :model-value="showArchivedProjects ? 'archived' : 'active'" @tab-change="showArchivedProjects = $event === 'archived'; resetProjectDrag()">
            <ElTabPane label="进行中" name="active"><template #label><span class="project-tab-label">进行中</span></template></ElTabPane>
            <ElTabPane label="已归档" name="archived"><template #label><span class="project-tab-label">已归档</span></template></ElTabPane>
          </ElTabs>
          <div class="project-edit-list project-sort-list" @dragover="previewProjectDrop" @dragleave="leaveProjectList" @drop.prevent.stop="dropProject">
            <div v-for="(project, index) in visibleDraftProjects" :key="project.id" class="project-edit-row project-sort-row" :class="{ 'is-dragging': draggedProject === project.id, 'drop-before': projectDropIndex === index, 'drop-after': projectDropIndex === visibleDraftProjects.length && index === visibleDraftProjects.length - 1 }">
              <button type="button" class="project-drag-handle" draggable="true" :aria-label="`拖动排序：${project.name}；方向键上下移动`" title="拖动排序，也可使用上下方向键" @dragstart="startProjectDrag($event, project.id)" @dragend="resetProjectDrag" @keydown.up.prevent="moveProject(project.id, -1)" @keydown.down.prevent="moveProject(project.id, 1)">⠿</button>
              <ElInput v-model="project.name" :aria-label="`项目名称：${project.name}`" maxlength="40" />
              <div class="project-row-actions">
                <ElButton text :aria-label="`${project.archived ? '取消归档' : '归档'}项目：${project.name}`" :title="project.archived ? '取消归档' : '归档'" @click="toggleProjectArchive(project)"><RiInboxUnarchiveLine v-if="project.archived" size="17" aria-hidden="true" /><RiInboxArchiveLine v-else size="17" aria-hidden="true" /></ElButton>
                <ElButton text :disabled="transferringProjects || persisting || !projectSnapshot.projects.some(item => item.id === project.id)" :aria-label="`导出项目：${project.name}`" title="导出项目" @click="downloadProject(project.id)"><RiUpload2Line size="17" aria-hidden="true" /></ElButton>
                <ElButton text type="danger" :disabled="transferringProjects || persisting" :aria-label="`删除项目：${project.name}`" title="删除项目" @click="deleteDraftProject(project)"><Icon name="Delete" :size="17" /></ElButton>
              </div>
            </div>
            <ElEmpty v-if="!visibleDraftProjects.length" :image-size="48" :description="showArchivedProjects ? '暂无已归档项目。' : '暂无进行中的项目。'" />
          </div>
          <div class="dialog-actions project-dialog-actions">
            <ElButton :disabled="transferringProjects || persisting" @click="downloadProject()"><RiUpload2Line :size="17" aria-hidden="true" />全部导出</ElButton>
            <ElButton class="cancel-button" @click="projectDialog = false">取消</ElButton>
            <ElButton type="primary" native-type="submit">保存</ElButton>
          </div>
        </ElForm>
      </ElDialog>
      <ElDialog v-model="categoryDialog" title="分类管理" width="min(500px, 92vw)" @closed="resetCategoryDrag">
        <p class="project-help">拖动排序手柄调整分类顺序，下拉选项也将按此顺序显示。分类名称修改会同步更新关联任务。删除分类后，关联任务移至保存列表的第一个分类；至少保留一个分类。修改后点击保存生效。</p>
        <ElForm @submit.prevent="saveCategories">
          <div class="project-add"><ElInput v-model="newCategoryName" placeholder="新分类名称" aria-label="新分类名称" maxlength="40" @keydown.enter.prevent="addDraftCategory" /><ElButton @click="addDraftCategory"><RiAddFill size="16" aria-hidden="true" />添加</ElButton></div>
          <div class="project-edit-list project-sort-list" @dragover="previewCategoryDrop" @dragleave="leaveCategoryList" @drop.prevent.stop="dropCategory">
            <div v-for="(category, index) in draftCategories" :key="category.id" class="project-edit-row project-sort-row" :class="{ 'is-dragging': draggedCategory === category.id, 'drop-before': categoryDropIndex === index, 'drop-after': categoryDropIndex === draftCategories.length && index === draftCategories.length - 1 }">
              <button type="button" class="project-drag-handle" draggable="true" :aria-label="`拖动排序：${category.name}；方向键上下移动`" title="拖动排序，也可使用上下方向键" @dragstart="startCategoryDrag($event, category.id)" @dragend="resetCategoryDrag" @keydown.up.prevent="moveCategory(category.id, -1)" @keydown.down.prevent="moveCategory(category.id, 1)">⠿</button>
              <ElInput v-model="category.name" :aria-label="`分类名称：${category.name}`" maxlength="40" /><ElButton text type="danger" :aria-label="`删除分类：${category.name}`" @click="draftCategories = draftCategories.filter(item => item.id !== category.id)"><Icon name="Delete" :size="17" /></ElButton>
            </div>
          </div>
          <div class="dialog-actions"><ElButton class="cancel-button" @click="categoryDialog = false">取消</ElButton><ElButton type="primary" native-type="submit">保存分类</ElButton></div>
        </ElForm>
      </ElDialog>
    </div>
  </ElConfigProvider>
</template>
