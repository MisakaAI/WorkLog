import { test } from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const css = readFileSync(new URL('../src/style.css', import.meta.url), 'utf8')
const theme = readFileSync(new URL('../src/theme.css', import.meta.url), 'utf8')
const rule = selector => css.slice(css.indexOf(`${selector} {`)).split('}')[0]

test('看板与列表卡片使用柔和边框和投影，不再使用白色高光阴影', () => {
  for (const selector of ['.task-card.el-card', '.list-task.el-button']) {
    const block = rule(selector)
    assert.match(block, /border: 1px solid var\(--border-soft\)/)
    assert.match(block, /box-shadow: var\(--shadow-card\)/)
    assert.match(block, /background: var\(--card-background, var\(--surface\)\)/)
    assert.doesNotMatch(block, /--edge-light|--shadow-raised/)
  }
})

test('两种视图的悬停投影一致，键盘焦点可见，按下不恢复内凹高光', () => {
  for (const selector of ['.task-card.el-card:hover', '.task-list .list-task.el-button:not(.is-disabled):hover']) {
    assert.match(rule(selector), /box-shadow: var\(--shadow-card-hover\)/)
    assert.doesNotMatch(rule(selector), /--edge-light|--shadow-raised/)
  }
  for (const selector of ['.task-card.el-card:focus-visible', '.task-list .list-task.el-button:focus-visible']) {
    assert.match(rule(selector), /box-shadow: var\(--shadow-card-hover\), var\(--shadow-focus-visible\)/)
  }
  assert.match(rule('.task-list .list-task.el-button:active'), /box-shadow: var\(--shadow-card\)/)
})

test('明暗主题的卡片投影均向下，不包含左上角反向高光', () => {
  for (const selector of [':root', ':root.dark']) {
    const block = theme.slice(theme.indexOf(`${selector} {`)).split('}')[0]
    for (const token of ['--shadow-card', '--shadow-card-hover']) {
      const value = block.match(new RegExp(`${token}: ([^;]+);`))?.[1]
      assert.ok(value, `${selector} 定义 ${token}`)
      for (const shadow of value.split(',')) {
        assert.match(shadow.trim(), /^0 [1-9]\d*px \d+px #[a-f\d]{8}$/)
        assert.doesNotMatch(shadow, /#ffffff/i)
      }
    }
  }
})
