<script setup>
import { computed, defineAsyncComponent, onBeforeUnmount, ref, shallowRef, watch } from 'vue'
import { Editor, EditorContent } from '@tiptap/vue-3'
import { createWorklogExtensions, parseWorklogHTML } from '../worklogEditor'
import { createWorklogDrafts } from '../worklogSaving'
import { ElButton, ElColorPicker, ElDialog, ElMessage, ElMessageBox } from 'element-plus'
import {
  RiBold, RiItalic, RiStrikethrough, RiH2, RiListUnordered, RiListOrdered,
  RiDoubleQuotesL, RiCodeBoxLine, RiArrowGoBackLine, RiArrowGoForwardLine,
  RiSaveLine, RiUnderline, RiH3, RiCodeLine,
  RiSeparator, RiFormatClear, RiCodeSSlashLine, RiEraserLine,
} from '@remixicon/vue'
import { getTodayWorklog, getWorklog, saveWorklog } from '../api/worklog'

// 源码编辑器体积较大，仅在首次打开源码弹窗时加载。
const SourceCodeEditor = defineAsyncComponent(() => import('./SourceCodeEditor.vue'))
const props = defineProps({ date: { type: String, required: true } })
const emit = defineEmits(['loaded', 'saved'])
const dateLabel = computed(() => {
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(props.date)
  return match ? `${match[1]}年${match[2]}月${match[3]}日` : props.date
})
const loading = ref(true)
const saving = ref(false)
const error = ref('')
const dirty = ref(false)
const loadedDate = ref('')
const sourceVisible = ref(false)
const sourceHTML = ref('')
const originalSource = ref('')
const sourceChanged = computed(() => sourceVisible.value && sourceHTML.value !== originalSource.value)
const sourceError = ref('')
const colorWarning = ref(false)
const colorOptions = { onUnsupportedColor: () => { colorWarning.value = true } }
const savedAt = ref('')
// 保存错误归属于具体日期，切换日期不能清除它。任何写入失败都会暂停
// 该日期的自动重试；版本冲突还必须由用户明确重新加载后才能继续。
const saveFailures = ref(new Map())
const currentFailure = computed(() => !loading.value && loadedDate.value === props.date ? saveFailures.value.get(loadedDate.value) : null)
const saveError = computed(() => currentFailure.value?.message || '')
const saveStatus = computed(() => {
  if (saving.value) return '正在保存…'
  if (currentFailure.value?.conflict) return '版本冲突 · 自动保存已暂停，请先复制草稿，再重新加载'
  if (saveError.value) return `保存失败：${saveError.value} · 自动保存已暂停，请手动重试`
  if (dirty.value) return '有未保存的修改 · 切换日期会保留草稿'
  return savedAt.value ? `已保存 · ${new Date(savedAt.value).toLocaleTimeString('zh-CN')}` : '记录今天的进展与想法'
})
function showSource() {
  sourceHTML.value = editor.value?.getHTML() || ''
  originalSource.value = sourceHTML.value
  sourceError.value = ''
  sourceVisible.value = true
}
async function closeSource(done) {
  if (sourceChanged.value) {
    try {
      await ElMessageBox.confirm('源码修改尚未应用，确定放弃这些修改？', '未应用的修改', { type: 'warning', customClass: 'source-discard-confirm', confirmButtonText: '放弃修改', cancelButtonText: '继续编辑' })
    } catch { return }
  }
  if (done) done()
  else sourceVisible.value = false
}
async function applySource() {
  if (locked.value || saving.value) return
  if (currentFailure.value?.conflict) {
    sourceError.value = '日志有版本冲突，请先复制需要保留的源码和草稿，再重新加载日志'
    return
  }
  try {
    const content = parseWorklogHTML(sourceHTML.value, colorOptions)
    editor.value.commands.setContent(content, { emitUpdate: true })
    sourceVisible.value = false
    await save('manual')
  } catch (e) { sourceError.value = e.message || 'HTML 内容无法应用，请检查后重试' }
}
// 使用适合正文及重点标注的实色，避免浅色或透明预设影响可读性。
const predefineColors = [
  '#30333e', '#606266', '#b91c1c', '#c2410c', '#a16207',
  '#15803d', '#0f766e', '#0369a1', '#1d4ed8', '#6d28d9',
  '#a21caf', '#be185d',
]
// 与 Word 一样记住最近使用的颜色，点击 A 可重复应用到其他选区。
const selectedColor = ref('#b91c1c')
function setColor(color) {
  if (locked.value || !editor.value) return
  selectedColor.value = color || null
  const chain = editor.value.chain().focus()
  if (color) chain.setColor(color).run()
  else chain.unsetColor().run()
}
const drafts = createWorklogDrafts()
// 请求序号只控制界面采纳结果的顺序，避免快速切换日期后旧响应覆盖新内容。
let requestId = 0
let initial = true
let lastUseToday = false
let lastDiscardDraft = false
let disposed = false
const autoSaveTimer = window.setInterval(() => save('auto'), 10_000)
let blurTimer
function saveOnBlur() {
  const date = loadedDate.value
  window.clearTimeout(blurTimer)
  blurTimer = window.setTimeout(() => {
    if (disposed || date !== loadedDate.value || sourceVisible.value) return
    // 点击格式工具不打断编辑；真正离开编辑区域才触发失焦保存。
    if (document.activeElement?.closest('.worklog-toolbar')) return
    save('auto')
  }, 0)
}
const editor = shallowRef(null)
const editorGeneration = ref(0)
function replaceEditor(content, date) {
  // 加载/放弃草稿是新的编辑会话，不能进入旧文档的 undo/redo 或输入规则历史。
  const next = new Editor({
    extensions: createWorklogExtensions(colorOptions), content, editable: false,
    editorProps: { attributes: { role: 'textbox', 'aria-label': '工作日志内容', 'aria-multiline': 'true' } },
    onUpdate: ({ editor }) => {
      dirty.value = true
      drafts.set(date, editor.getJSON())
    },
    onBlur: saveOnBlur,
  })
  editor.value?.destroy()
  editor.value = next
  editorGeneration.value++
}
const tools = [
  { label: '加粗', icon: RiBold, active: 'bold', run: c => c.toggleBold() },
  { label: '斜体', icon: RiItalic, active: 'italic', run: c => c.toggleItalic() },
  { label: '下划线', icon: RiUnderline, active: 'underline', run: c => c.toggleUnderline() },
  { label: '删除线', icon: RiStrikethrough, active: 'strike', run: c => c.toggleStrike() },
  { label: '标题', icon: RiH2, active: 'heading', attrs: { level: 2 }, run: c => c.toggleHeading({ level: 2 }) },
  { label: '三级标题', icon: RiH3, active: 'heading', attrs: { level: 3 }, run: c => c.toggleHeading({ level: 3 }) },
  { label: '行内代码', icon: RiCodeLine, active: 'code', run: c => c.toggleCode() },
  { label: '无序列表', icon: RiListUnordered, active: 'bulletList', run: c => c.toggleBulletList() },
  { label: '有序列表', icon: RiListOrdered, active: 'orderedList', run: c => c.toggleOrderedList() },
  { label: '引用', icon: RiDoubleQuotesL, active: 'blockquote', run: c => c.toggleBlockquote() },
  { label: '代码块', icon: RiCodeBoxLine, active: 'codeBlock', run: c => c.toggleCodeBlock() },
  { label: '分隔线', icon: RiSeparator, run: c => c.setHorizontalRule() },
  { label: '清除格式', icon: RiFormatClear, run: c => c.unsetAllMarks().clearNodes() },
  { label: '撤销', icon: RiArrowGoBackLine, run: c => c.undo(), can: c => c.undo() },
  { label: '重做', icon: RiArrowGoForwardLine, run: c => c.redo(), can: c => c.redo() },
]
const locked = computed(() => loading.value || !!error.value)
function syncEditable() { editor.value?.setEditable(!locked.value && !sourceVisible.value, false) }
watch([locked, sourceVisible, editor], syncEditable)
async function load(useToday = false, { discardDraft = false } = {}) {
  lastUseToday = useToday
  lastDiscardDraft = discardDraft
  const id = ++requestId
  const date = props.date
  loading.value = true
  sourceVisible.value = false
  error.value = ''
  try {
    const entry = useToday ? await getTodayWorklog() : await getWorklog(date)
    if (id !== requestId) return
    colorWarning.value = false
    sourceVisible.value = false
    loadedDate.value = entry.date
    const content = drafts.load(entry, { discardDraft })
    if (!drafts.has(entry.date)) saveFailures.value.delete(entry.date)
    replaceEditor(content, entry.date)
    dirty.value = drafts.has(entry.date)
    savedAt.value = entry.updatedAt || ''
    emit('loaded', entry.date)
  } catch (e) {
    if (id === requestId) error.value = e.message || '加载工作日志失败'
  } finally {
    if (id === requestId) loading.value = false
  }
}
watch(() => props.date, date => {
  if (!initial && !loading.value && date === loadedDate.value) return
  const useToday = initial
  if (!initial) save('auto')
  initial = false
  load(useToday)
}, { immediate: true, flush: 'post' })
async function save(reason = 'auto') {
  if (disposed || !editor.value || locked.value || saving.value || sourceVisible.value || !dirty.value) return
  const date = loadedDate.value
  const failure = saveFailures.value.get(date)
  if (failure?.conflict || (reason === 'auto' && failure)) return
  saving.value = true
  try {
    const result = await drafts.save(date, saveWorklog)
    if (disposed || !result) return
    emit('saved', { date, content: result.content })
    saveFailures.value.delete(date)
    // 保存期间允许切换日期；旧日期的结果只能更新对应草稿，不能改变当前编辑状态。
    if (loadedDate.value === date && !loading.value) {
      dirty.value = drafts.has(date)
      savedAt.value = result.updatedAt || new Date().toISOString()
    }
    if (reason === 'manual') ElMessage.success(`${date} 的工作日志已保存`)
  } catch (e) {
    if (disposed) return
    saveFailures.value.set(date, {
      message: e.message || '请重试',
      conflict: e.status === 409 && (!e.code || e.code === 'VERSION_CONFLICT'),
      requestId: e.requestId,
    })
    if (reason === 'manual') ElMessage.error(e.message || '保存失败，请重试')
  } finally { saving.value = false }
}
async function reloadConflict() {
  const date = loadedDate.value
  if (saving.value || loading.value || props.date !== date) return
  try {
    await ElMessageBox.confirm('重新加载将放弃当前日期的未保存草稿。请先复制需要保留的内容，再确认读取服务器最新版本。', '重新加载日志', { type: 'warning', confirmButtonText: '放弃草稿并重新加载', cancelButtonText: '继续保留草稿' })
  } catch { return }
  if (disposed || props.date !== date || loadedDate.value !== date) return
  await load(false, { discardDraft: true })
}
// 草稿只保存在组件内存中，关闭页面会丢失；离开前提示，而不是假定自动保存已完成。
function beforeUnload(event) {
  if (!drafts.size && !sourceChanged.value) return
  event.preventDefault()
  event.returnValue = ''
}
window.addEventListener('beforeunload', beforeUnload)
onBeforeUnmount(() => {
  disposed = true
  requestId++
  editor.value?.destroy()
  window.clearInterval(autoSaveTimer)
  window.clearTimeout(blurTimer)
  window.removeEventListener('beforeunload', beforeUnload)
})
</script>

<template>
  <section class="worklog-panel panel" aria-label="工作日志" :aria-busy="loading">
    <div class="worklog-toolbar" role="toolbar" aria-label="日志格式，窄屏可横向滚动" tabindex="0">
      <div class="worklog-tools">
        <ElButton v-for="tool in tools" :key="tool.label" text class="icon-button" :class="{ 'is-active': tool.active && editor?.isActive(tool.active, tool.attrs) }" :title="tool.label" :aria-label="tool.label" :aria-pressed="tool.active ? !!editor?.isActive(tool.active, tool.attrs) : undefined" :disabled="locked || !editor || (tool.can && !tool.can(editor.can().chain()).run())" @mousedown.prevent @click="tool.run(editor.chain().focus()).run()"><component :is="tool.icon" size="18" aria-hidden="true" /></ElButton>
        <span class="toolbar-divider" aria-hidden="true"></span>
        <div class="color-control" role="group" aria-label="字体颜色">
          <ElButton text class="font-color-button" title="应用字体颜色" aria-label="应用字体颜色" :disabled="locked || !editor" @mousedown.prevent @click="setColor(selectedColor)">
            <svg class="font-color-icon" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
              <path d="M15.2459 14H8.75407L7.15407 18H5L11 3H13L19 18H16.8459L15.2459 14ZM14.4459 12L12 5.88516L9.55407 12H14.4459Z" />
              <path class="font-color-underline" d="M3 20H21V22H3V20Z" :fill="selectedColor || 'var(--text-main)'" />
            </svg>
          </ElButton>
          <span class="font-color-picker">
            <ElColorPicker :model-value="selectedColor" :predefine="predefineColors" color-format="hex" :show-alpha="false" size="small" title="选择字体颜色" aria-label="选择字体颜色" :disabled="locked || !editor" @mousedown.prevent @change="setColor" />
          </span>
        </div>
        <ElButton text class="icon-button" title="清除颜色" aria-label="清除颜色" :disabled="locked || !editor" @mousedown.prevent @click="editor.chain().focus().unsetColor().run()"><RiEraserLine size="18" aria-hidden="true" /></ElButton>
        <span class="toolbar-divider" aria-hidden="true"></span>
        <ElButton text size="small" :disabled="locked || !editor" @click="showSource"><RiCodeSSlashLine size="18" aria-hidden="true" />源代码</ElButton>
      </div>
      <time class="worklog-date" :datetime="date">{{ dateLabel }}</time>
    </div>
    <div v-if="loading" class="worklog-notice" role="status">正在读取工作日志…</div>
    <div v-else-if="error" class="worklog-notice" role="alert">{{ error }}<ElButton link type="primary" @click="load(lastUseToday, { discardDraft: lastDiscardDraft })">重试</ElButton></div>
    <EditorContent :key="editorGeneration" v-show="!loading && !error" :editor="editor" class="worklog-content" />
    <p v-if="colorWarning" class="worklog-notice" role="status">已移除不支持的文字颜色，正文和其他格式已保留。</p>
    <div class="worklog-footer">
      <div class="worklog-save-info"><span role="status" :class="{ 'save-error': saveError }" :title="currentFailure?.requestId ? `请求 ID：${currentFailure.requestId}` : undefined">{{ saveStatus }}</span><ElButton v-if="currentFailure?.conflict" link type="primary" :disabled="saving" @click="reloadConflict">重新加载（先复制草稿）</ElButton></div>
      <div class="worklog-save-actions">
        <ElButton type="primary" size="default" class="management-button" :loading="saving" :disabled="locked || saving || !dirty || currentFailure?.conflict" @click="save('manual')"><RiSaveLine size="16" aria-hidden="true" />保存</ElButton>
      </div>
    </div>
    <ElDialog v-model="sourceVisible" title="HTML 源代码" width="min(800px, 92vw)" :close-on-click-modal="false" :before-close="closeSource">
      <p class="source-hint">编辑后点击“保存”同步到日志。仅保留编辑器支持的格式，不支持的标签及脚本将被过滤；应用前不会自动保存源码修改。</p>
      <SourceCodeEditor v-if="sourceVisible" v-model="sourceHTML" />
      <p v-if="sourceError" class="save-error" role="alert">{{ sourceError }}</p>
      <template #footer><ElButton @click="closeSource()">取消</ElButton><ElButton type="primary" size="default" class="management-button" :disabled="locked || saving" @click="applySource">保存</ElButton></template>
    </ElDialog>
  </section>
</template>

<style scoped>
.worklog-toolbar { display: flex; align-items: center; gap: 8px; margin: 18px 20px 0; padding: 6px; border: 1px solid var(--border-soft); border-radius: var(--radius-control); background: var(--surface-soft); box-shadow: var(--shadow-inset); }
.worklog-tools { display: flex; flex: 1; flex-wrap: wrap; align-items: center; gap: 3px; min-width: 0; }
.worklog-toolbar:focus-visible { outline: none; box-shadow: var(--shadow-focus); }
.worklog-toolbar .is-active { color: var(--accent); background: var(--surface); box-shadow: var(--shadow-raised); }
.worklog-date { display: flex; align-items: center; align-self: flex-start; min-height: 32px; flex-shrink: 0; padding: 0 8px; color: var(--text-secondary); font-size: 12px; font-variant-numeric: tabular-nums; white-space: nowrap; }
.worklog-content { margin: 14px 20px 18px; border: 1px solid transparent; border-radius: var(--radius-control); transition: background var(--motion-fast), border-color var(--motion-fast), box-shadow var(--motion-fast); }
.worklog-content:focus-within { border-color: var(--border-soft); background: var(--surface-soft); box-shadow: var(--shadow-inset); }
.worklog-content :deep(.tiptap) { min-height: 180px; max-height: 460px; overflow-y: auto; padding: 18px 20px; font-size: var(--font-size-reading); line-height: var(--line-height-reading); outline: none; overflow-wrap: anywhere; font-synthesis: style; }
.worklog-content :deep(em) { font-style: italic; }
.worklog-content :deep(hr) { border: 0; border-top: 1px solid var(--border); margin: 18px 0; }
.worklog-content :deep(p) { margin: 5px 0; }
.worklog-content :deep(h2) { font-size: var(--font-size-2xl); margin: 14px 0 8px; }
.worklog-content :deep(h3) { font-size: var(--font-size-lg); margin: 12px 0 6px; }
.worklog-content :deep(ul), .worklog-content :deep(ol) { padding-left: 24px; margin: 8px 0; }
.worklog-content :deep(blockquote) { border-left: 3px solid var(--accent); margin: 12px 0; padding-left: 14px; color: var(--text-secondary); }
.worklog-content :deep(pre) { padding: 12px; border-radius: var(--radius-control); background: var(--surface-soft); white-space: pre-wrap; }
.worklog-content :deep(code) { background: var(--surface-soft); border-radius: 3px; padding: 2px 4px; }
.worklog-footer { display: flex; flex-wrap: wrap; justify-content: space-between; gap: 6px 16px; padding: 12px 24px; align-items: center; border-top: 1px solid var(--border-soft); color: var(--text-muted); font-size: 12px; }
.worklog-notice { min-height: 180px; display: flex; justify-content: center; align-items: center; gap: 12px; color: var(--text-secondary); }
.toolbar-divider { width: 1px; align-self: stretch; background: var(--border); margin: 3px 6px; }
.color-control { display: inline-flex; align-items: center; border-radius: 4px; }
.color-control:focus-within { background: var(--focus-surface); box-shadow: var(--shadow-focus-visible); }
.font-color-button { width: 30px; height: 32px; padding: 4px; border-radius: 4px 0 0 4px; }
.font-color-icon { display: block; width: 22px; height: 22px; }
.font-color-picker { display: inline-flex; width: 20px; height: 32px; }
.font-color-picker :deep(.el-color-picker) { width: 100%; height: 100%; }
.font-color-picker :deep(.el-color-picker__trigger) { display: flex; align-items: center; justify-content: center; width: 20px; height: 32px; padding: 0; border: 0; border-radius: 0 4px 4px 0; }
/* 隐藏组件自带的色块及内置图标，颜色仅显示在左侧 SVG 横线上。 */
.font-color-picker :deep(.el-color-picker__trigger > *) { display: none !important; }
.font-color-picker :deep(.el-color-picker__trigger)::after { content: ''; width: 0; height: 0; border: 4px solid transparent; border-bottom: 0; border-top-color: var(--text-secondary); }
.font-color-picker :deep(.el-color-picker:not(.is-disabled):hover) { background: var(--focus-surface); }
.font-color-picker :deep(.el-color-picker.is-disabled .el-color-picker__trigger)::after { opacity: 0.4; }
.source-hint { color: var(--text-secondary); font-size: 13px; margin-bottom: 12px; }
.worklog-save-info { display: flex; flex-direction: column; gap: 4px; }
.worklog-save-actions { display: flex; justify-content: flex-end; align-items: center; flex-wrap: wrap; gap: 12px; margin-left: auto; }
.save-error { color: var(--red-fg); }
@media (max-width: 767px) {
  .worklog-toolbar { margin: 14px 12px 0; gap: 4px; }
  .worklog-tools { flex-wrap: nowrap; overflow-x: auto; scrollbar-width: thin; }
  .worklog-tools > * { flex-shrink: 0; }
  .worklog-date { align-self: center; padding: 0 4px 0 8px; border-left: 1px solid var(--border); }
  .toolbar-divider { height: 24px; align-self: center; }
  .worklog-content { margin: 12px 12px 14px; }
  .worklog-content :deep(.tiptap) { padding: 16px; }
  .worklog-footer { padding: 12px 16px; }
  .font-color-button { width: 36px; height: 44px; }
  .font-color-picker { width: 28px; height: 44px; }
  .font-color-picker :deep(.el-color-picker__trigger) { width: 28px; height: 44px; }
}
</style>
