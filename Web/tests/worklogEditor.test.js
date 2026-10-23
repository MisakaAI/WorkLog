import { test } from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { JSDOM } from 'jsdom'
import { Editor } from '@tiptap/vue-3'
import { createWorklogExtensions, parseWorklogHTML } from '../src/worklogEditor.js'

const component = readFileSync(new URL('../src/components/WorklogEditor.vue', import.meta.url), 'utf8')

test('编辑区局部允许合成中文斜体，工具栏鼠标操作保留文本选区', () => {
  assert.match(component, /\.worklog-content :deep\(\.tiptap\)\s*\{[^}]*font-synthesis:\s*style/)
  assert.match(component, /:deep\(em\)\s*\{\s*font-style:\s*italic/)
  assert.match(component, /@mousedown\.prevent @click="tool\.run/)
  assert.match(component, /title="HTML 源代码"/)
  assert.match(component, /sourceHTML\.value = editor\.value\?\.getHTML\(\)/)
  assert.match(component, /v-model="sourceHTML"/)
  assert.doesNotMatch(component, /:model-value="sourceHTML"[^>]*readonly/)
  assert.match(component, /@click="applySource"/)
  assert.match(component, /onBlur: saveOnBlur/)
  assert.match(component, /window\.setInterval\(\(\) => save\('auto'\)/)
  assert.match(component, /window\.clearInterval\(autoSaveTimer\)/)
  assert.match(component, /class="worklog-save-actions"[\s\S]*保存/) 
})

test('字体颜色使用 Word 式分体按钮，A 可直接应用颜色且横线显示最近所选颜色', () => {
  assert.doesNotMatch(component, /RiFontColor/)
  assert.match(component, /class="font-color-button"[^>]*@mousedown\.prevent @click="setColor\(selectedColor\)"/)
  assert.match(component, /<svg class="font-color-icon"[^>]*viewBox="0 0 24 24"/)
  assert.match(component, /<path class="font-color-underline"[^>]*:fill="selectedColor/)
  assert.doesNotMatch(component, />A<span/)
  assert.match(component, /<ElColorPicker[^>]*color-format="hex" :show-alpha="false"/)
  assert.doesNotMatch(component, /\.font-color-picker :deep\(\.el-color-picker__trigger\)\s*\{[^}]*border-left/)
  assert.match(component, /<ElColorPicker[^>]*:model-value="selectedColor"[^>]*aria-label="选择字体颜色"[^>]*@change="setColor"/)
  assert.match(component, /\.font-color-picker :deep\(\.el-color-picker__trigger > \*\)\s*\{ display: none !important;/)
  assert.match(component, /selectedColor\.value = color \|\| null/)
  assert.match(component, /if \(color\) chain\.setColor\(color\)\.run\(\)/)
  assert.match(component, /else chain\.unsetColor\(\)\.run\(\)/)
})

test('斜体、颜色及新增格式命令生效，JSON 保存和 HTML 源码均保留格式', t => {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { pretendToBeVisual: true })
  const globals = {
    window: dom.window, document: dom.window.document, navigator: dom.window.navigator,
    DOMParser: dom.window.DOMParser,
    requestAnimationFrame: dom.window.requestAnimationFrame.bind(dom.window),
    cancelAnimationFrame: dom.window.cancelAnimationFrame.bind(dom.window),
  }
  const originals = new Map()
  for (const [key, value] of Object.entries(globals)) {
    originals.set(key, Object.getOwnPropertyDescriptor(globalThis, key))
    Object.defineProperty(globalThis, key, { configurable: true, value })
  }
  const editors = []
  t.after(() => {
    editors.forEach(editor => editor.destroy())
    dom.window.close()
    for (const [key, descriptor] of originals) {
      if (descriptor) Object.defineProperty(globalThis, key, descriptor)
      else delete globalThis[key]
    }
  })
  function create(content) {
    const editor = new Editor({ extensions: createWorklogExtensions(), content })
    editors.push(editor)
    return editor
  }
  const editor = create('<p>中文工作日志</p>')
  editor.commands.selectAll()
  assert.equal(editor.commands.toggleItalic(), true)
  assert.match(editor.getHTML(), /<em>中文工作日志<\/em>/)
  assert.equal(editor.isActive('italic'), true)
  assert.equal(editor.commands.toggleUnderline(), true)
  assert.equal(editor.commands.setColor('#c62828'), true)
  assert.match(editor.getHTML(), /color: rgb\(198, 40, 40\)/)
  assert.match(editor.getHTML(), /<u>/)
  const json = editor.getJSON()
  const marks = json.content[0].content[0].marks
  assert.ok(marks.some(mark => mark.type === 'italic'))
  assert.ok(marks.some(mark => mark.type === 'textStyle' && mark.attrs.color === '#c62828'))
  const restored = create(JSON.parse(JSON.stringify(json)))
  assert.equal(restored.getHTML(), editor.getHTML())
  const parsed = create(editor.getHTML())
  assert.equal(parsed.getHTML(), editor.getHTML())
  editor.commands.unsetColor()
  assert.doesNotMatch(editor.getHTML(), /color:/)
  editor.chain().unsetAllMarks().clearNodes().run()
  assert.equal(editor.getHTML(), '<p>中文工作日志</p>')
  editor.commands.toggleHeading({ level: 3 })
  assert.match(editor.getHTML(), /<h3>/)
  editor.commands.setHorizontalRule()
  assert.match(editor.getHTML(), /<hr>/)
  const source = '<h2>源码修改</h2><p><em><span style="color: #c62828" onclick="alert(1)">工作记录</span></em></p><script>alert(1)</script><iframe src="https://example.com"></iframe>'
  const applied = create(parseWorklogHTML(source))
  assert.match(applied.getHTML(), /<h2>源码修改<\/h2>/)
  assert.match(applied.getHTML(), /<em>/)
  assert.match(applied.getHTML(), /color: rgb\(198, 40, 40\)/)
  assert.doesNotMatch(applied.getHTML(), /script|iframe|onclick|alert/)
  assert.equal(create(parseWorklogHTML('')).getHTML(), '<p></p>')
  let stripped = 0
  const modernHTML = '<p><strong><span style="color: oklch(60% 0.2 25)">保留正文和粗体</span></strong></p>'
  const imported = create(parseWorklogHTML(modernHTML, { onUnsupportedColor: () => stripped++ }))
  assert.equal(imported.getText(), '保留正文和粗体')
  assert.match(imported.getHTML(), /<strong>/)
  assert.doesNotMatch(imported.getHTML(), /oklch|color:/)
  assert.ok(stripped > 0)
  const pasted = new Editor({ extensions: createWorklogExtensions({ onUnsupportedColor: () => stripped++ }), content: '<p></p>' })
  editors.push(pasted)
  pasted.view.pasteHTML(modernHTML, new dom.window.Event('paste'))
  assert.equal(pasted.getText(), '保留正文和粗体')
  assert.doesNotMatch(pasted.getHTML(), /oklch|color:/)
  assert.ok(stripped > 1)

})
