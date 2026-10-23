<script setup>
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
// 在创建主题／模型前按需注册核心服务及 HTML 编辑功能。
import '../monacoFeatures.js'
import * as monaco from 'monaco-editor/editor/editor.api.js'
import 'monaco-editor/languages/definitions/html/register.js'
import 'monaco-editor/language/html/monaco.contribution.js'
import EditorWorker from 'monaco-editor/editor/editor.worker.js?worker'
import HTMLWorker from 'monaco-editor/language/html/html.worker.js?worker'

const props = defineProps({ modelValue: { type: String, default: '' } })
const emit = defineEmits(['update:modelValue'])
const container = ref(null)
let editor
let model
let changeListener
let selectionListener
let selectionDecorations
let themeObserver

// Monaco 常规主题只能设置选区底色，选中文字仍沿用语法高亮色；
// 暗色下浅底色配近白文字对比不足，因此用行内装饰统一压深选中文字。
const SELECTED_TEXT_CLASS = 'editor-selected-text'

function syncSelectionTextColor() {
  if (!editor || !selectionDecorations) return
  if (!document.documentElement.classList.contains('dark')) {
    selectionDecorations.clear()
    return
  }
  selectionDecorations.set(editor.getSelections()
    .filter(selection => !selection.isEmpty())
    .map(range => ({
      range,
      options: { inlineClassName: SELECTED_TEXT_CLASS, description: 'worklog-selected-text' },
    })))
}

// Vite 单独打包 worker，避免依赖 CDN 或在主线程运行语言服务。
self.MonacoEnvironment = {
  ...self.MonacoEnvironment,
  getWorker(_moduleId, label) {
    return label === 'html' || label === 'handlebars' || label === 'razor'
      ? new HTMLWorker()
      : new EditorWorker()
  },
}

// Monaco 使用独立主题系统，从页面 CSS 变量取色以保持两种编辑界面一致。
function syncTheme() {
  const dark = document.documentElement.classList.contains('dark')
  const css = getComputedStyle(document.documentElement)
  monaco.editor.defineTheme('worklog', {
    base: dark ? 'vs-dark' : 'vs',
    inherit: true,
    rules: [],
    colors: {
      'editor.background': css.getPropertyValue('--surface-soft').trim(),
      'editor.foreground': css.getPropertyValue('--text-main').trim(),
      'editorLineNumber.foreground': css.getPropertyValue('--text-muted').trim(),
      'editor.lineHighlightBackground': css.getPropertyValue('--surface-hover').trim(),
      'editor.selectionBackground': css.getPropertyValue('--editor-selection-bg').trim(),
      'editor.inactiveSelectionBackground': css.getPropertyValue('--editor-selection-bg-inactive').trim(),
      'editor.selectionHighlightBackground': css.getPropertyValue('--editor-selection-highlight-bg').trim(),
      'editorCursor.foreground': css.getPropertyValue('--accent').trim(),
    },
  })
  monaco.editor.setTheme('worklog')
  syncSelectionTextColor()
}

onMounted(() => {
  syncTheme()
  model = monaco.editor.createModel(props.modelValue, 'html')
  editor = monaco.editor.create(container.value, {
    model,
    automaticLayout: true,
    ariaLabel: '工作日志 HTML 源代码',
    minimap: { enabled: false },
    fontFamily: '"Cascadia Code", "Source Code Pro", monospace',
    fontSize: 16,
    lineHeight: 32,
    tabSize: 2,
    insertSpaces: true,
    wordWrap: 'on',
    scrollBeyondLastLine: false,
    padding: { top: 12, bottom: 12 },
  })
  selectionDecorations = editor.createDecorationsCollection()
  changeListener = editor.onDidChangeModelContent(() => emit('update:modelValue', editor.getValue()))
  selectionListener = editor.onDidChangeCursorSelection(syncSelectionTextColor)
  themeObserver = new MutationObserver(syncTheme)
  themeObserver.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] })
})

watch(() => props.modelValue, value => {
  if (editor && editor.getValue() !== value) {
    editor.setValue(value)
    syncSelectionTextColor()
  }
})

// Monaco 的模型、监听器不会随 Vue 自动释放，关闭弹窗时必须主动清理。
onBeforeUnmount(() => {
  themeObserver?.disconnect()
  changeListener?.dispose()
  selectionListener?.dispose()
  selectionDecorations?.clear()
  editor?.dispose()
  model?.dispose()
})
</script>

<template>
  <div ref="container" class="source-code-editor"></div>
</template>

<style scoped>
.source-code-editor {
  height: min(440px, 55vh);
  min-height: 200px;
  width: 100%;
  overflow: hidden;
  border: 1px solid var(--border);
  border-radius: var(--radius-control);
}

/* 暗色下把选中文字压深，与浅色选区底色形成足够对比。 */
.source-code-editor :deep(.editor-selected-text) {
  color: var(--editor-selection-fg) !important;
}
</style>
