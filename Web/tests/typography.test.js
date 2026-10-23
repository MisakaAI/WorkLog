import { test } from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const read = path => readFileSync(new URL(path, import.meta.url), 'utf8')
const css = read('../src/style.css')
const theme = read('../src/theme.css')
const editor = read('../src/components/WorklogEditor.vue')

function declarations(source, selector) {
  const start = source.indexOf(`${selector} {`)
  assert.notEqual(start, -1, `缺少样式：${selector}`)
  const block = source.slice(start + selector.length + 2).split('}')[0].replace(/\/\*[\s\S]*?\*\//g, '')
  return Object.fromEntries(block.split(';').filter(item => item.includes(':')).map(item => {
    const colon = item.indexOf(':')
    return [item.slice(0, colon).trim(), item.slice(colon + 1).trim()]
  }))
}
const tokens = declarations(css, ':root')

test('默认排版明确设置六项字体属性，使用系统无衬线字体且保留用户根字号', () => {
  const body = declarations(css, 'body')
  assert.equal(tokens['font-size'], '100%')
  assert.match(tokens['--font-sans'], /^system-ui,.*'PingFang SC'.*'Microsoft YaHei'.*sans-serif$/)
  assert.deepEqual(Object.fromEntries(
    ['font-family', 'font-size', 'line-height', 'font-weight', 'letter-spacing', 'font-style'].map(key => [key, body[key]]),
  ), {
    'font-family': 'var(--font-sans)',
    'font-size': 'var(--font-size-base)',
    'line-height': 'var(--line-height-body)',
    'font-weight': 'var(--font-weight-regular)',
    'letter-spacing': 'var(--letter-spacing-body)',
    'font-style': 'normal',
  })
})

test('字号、行高和字重形成清晰层级，中文标题不使用负字距', () => {
  const sizes = ['xs', 'sm', 'base', 'reading', 'lg', 'xl', '2xl', 'display'].map(size => {
    const value = tokens[`--font-size-${size}`]
    assert.match(value, /^[\d.]+rem$/)
    return parseFloat(value)
  })
  assert.ok(sizes.every((size, i) => i === 0 || size > sizes[i - 1]))
  assert.equal(tokens['--font-weight-regular'], '400')
  assert.equal(tokens['--font-weight-semibold'], '600')
  assert.equal(tokens['--font-weight-bold'], '700')
  assert.ok(Number(tokens['--line-height-tight']) < Number(tokens['--line-height-body']))
  assert.ok(Number(tokens['--line-height-body']) < Number(tokens['--line-height-reading']))
  assert.equal(tokens['--letter-spacing-heading'], '0')
})

test('原生控件、Element Plus 及浮层共享字体，代码保留等宽和零字距', () => {
  const controls = declarations(css, 'button, input, textarea, select')
  assert.equal(controls.font, 'inherit')
  assert.equal(controls['letter-spacing'], 'inherit')
  const sharedTheme = declarations(theme, ':root, :root.dark')
  assert.equal(sharedTheme['--el-font-family'], 'var(--font-sans)')
  assert.equal(sharedTheme['--el-font-size-base'], 'var(--font-size-base)')
  const code = declarations(css, 'code, pre, kbd, samp')
  assert.equal(code['font-family'], 'var(--font-mono)')
  assert.equal(code['letter-spacing'], '0')
})

test('日志正文使用阅读字号和行高，不覆盖已设置的加粗、斜体', () => {
  const content = declarations(editor, '.worklog-content :deep(.tiptap)')
  assert.equal(content['font-size'], 'var(--font-size-reading)')
  assert.equal(content['line-height'], 'var(--line-height-reading)')
  assert.equal(content['font-synthesis'], 'style')
  assert.equal(declarations(editor, '.worklog-content :deep(em)')['font-style'], 'italic')
  assert.equal(declarations(css, 'strong, b')['font-weight'], 'var(--font-weight-bold)')
  assert.doesNotMatch(css, /\*\s*\{[^}]*font-(?:style|weight):/)
})

test('全局、主题及编辑器引用的排版变量均已定义', () => {
  const references = [...`${css}\n${theme}\n${editor}`.matchAll(/var\((--(?:font-|line-height-|letter-spacing-)[\w-]+)\)/g)]
  for (const [, name] of references) assert.ok(tokens[name], `未定义排版变量：${name}`)
})
