import { test } from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { runInNewContext } from 'node:vm'

const css = readFileSync(new URL('../src/theme.css', import.meta.url), 'utf8')
const themeSource = readFileSync(new URL('../src/composables/useTheme.js', import.meta.url), 'utf8')
  .replace(/^import .*\n/, '')
  .replace('export function useTheme()', 'function useTheme()')

function luminance(hex) {
  return hex.slice(1).match(/../g).map(value => {
    const channel = parseInt(value, 16) / 255
    return channel <= 0.04045 ? channel / 12.92 : ((channel + 0.055) / 1.055) ** 2.4
  }).reduce((sum, channel, i) => sum + channel * [0.2126, 0.7152, 0.0722][i], 0)
}

for (const theme of [':root', ':root.dark']) {
  test(`${theme}: 关键文字与背景的对比度至少为 4.5:1`, () => {
    const block = css.slice(css.indexOf(`${theme} {`)).split('}')[0]
    const colors = Object.fromEntries([...block.matchAll(/--([\w-]+): (#[a-f\d]{6});/g)].map(m => [m[1], m[2]]))
    const pairs = [
      ...['page', 'surface', 'surface-soft', 'card-red', 'card-orange', 'card-blue', 'card-green'].flatMap(bg => ['text-main', 'text-secondary', 'text-muted'].map(fg => [fg, bg])),
      ['accent', 'accent-bg'], ['blue-fg', 'blue-bg'], ['green-fg', 'green-bg'],
      ['orange-fg', 'orange-bg'], ['red-fg', 'red-bg'], ['on-accent', 'accent-solid'],
      ['editor-selection-fg', 'editor-selection-bg'],
      ['editor-selection-fg', 'editor-selection-bg-inactive'],
      ['text-main', 'editor-selection-highlight-bg'],
    ]
    for (const [fg, bg] of pairs) {
      const [lighter, darker] = [luminance(colors[fg]), luminance(colors[bg])].sort((a, b) => b - a)
      const ratio = (lighter + 0.05) / (darker + 0.05)
      assert.ok(ratio >= 4.5, `${fg} / ${bg}: ${ratio.toFixed(2)}:1`)
    }
  })
}

// 用最小响应式桩测试主题状态逻辑，不依赖 DOM 或浏览器运行时。
function setup({ saved = null, systemDark = false, storageBlocked = false, startViewTransition, reducedMotion = false } = {}) {
  const root = { dataset: {}, style: {}, classList: { toggle(_, enabled) { root.dark = enabled } } }
  const storage = new Map(saved ? [['worklog-theme', saved]] : [])
  let systemListener, dispose
  const watchers = new Set()
  const system = {
    matches: systemDark,
    addEventListener(_, callback) { systemListener = callback },
    removeEventListener(_, callback) { assert.equal(callback, systemListener); systemListener = null },
  }
  const context = {
    window: { matchMedia: query => query === '(prefers-reduced-motion: reduce)' ? { matches: reducedMotion } : system },
    document: { documentElement: root, querySelector: () => null, startViewTransition },
    nextTick: () => Promise.resolve(),
    localStorage: {
      getItem(key) { if (storageBlocked) throw Error('blocked'); return storage.get(key) },
      setItem(key, value) { if (storageBlocked) throw Error('blocked'); storage.set(key, value) },
    },
    ref(initial) { let value = initial; return { get value() { return value }, set value(next) { const previous = value; value = next; watchers.forEach(fn => fn(next, previous)) } } },
    computed(getter) { return { get value() { return getter() } } },
    watch(source, callback) { watchers.add(callback); callback(source.value) },
    onScopeDispose(callback) { dispose = callback },
  }
  const api = runInNewContext(`${themeSource}\nuseTheme()`, context)
  return { api, root, storage, systemChange: dark => systemListener({ matches: dark }), dispose: () => { dispose(); assert.equal(systemListener, null) } }
}

test('默认跟随系统；手动切换后记住选择且不再被系统覆盖', () => {
  const { api, root, storage, systemChange, dispose } = setup({ systemDark: true })
  assert.equal(root.dataset.theme, 'dark')
  assert.equal(root.dark, true)
  systemChange(false)
  assert.equal(root.dataset.theme, 'light')
  api.toggleTheme()
  assert.equal(api.isDark.value, true)
  assert.equal(storage.get('worklog-theme'), 'dark')
  systemChange(false)
  assert.equal(root.dataset.theme, 'dark')
  dispose()
})

test('恢复已保存的主题；同步根节点和原生控件的颜色模式', () => {
  const { root, api } = setup({ saved: 'light', systemDark: true })
  assert.equal(root.dataset.theme, 'light')
  api.toggleTheme()
  assert.equal(root.style.colorScheme, 'dark')
  assert.equal(root.dark, true)
})

test('存储不可用时仍可切换主题', () => {
  const { root, api } = setup({ storageBlocked: true })
  assert.doesNotThrow(() => api.toggleTheme())
  assert.equal(root.dataset.theme, 'dark')
})

test('首次加载不播放过渡；切换时统一更新快照并清理临时状态', async () => {
  let calls = 0, finished
  const { root, api } = setup({ startViewTransition(update) {
    calls++
    finished = Promise.resolve().then(update)
    return { finished, skipTransition() {} }
  } })
  assert.equal(calls, 0)
  api.toggleTheme()
  assert.equal(calls, 1)
  await finished
  assert.equal(root.dataset.theme, 'dark')
  assert.equal('themeChanging' in root.dataset, false)
})

test('减少动态效果时直接切换，不创建动画快照', () => {
  const { root, api } = setup({ reducedMotion: true, startViewTransition() {
    assert.fail('不应创建动画快照')
  } })
  api.toggleTheme()
  assert.equal(root.dataset.theme, 'dark')
  assert.equal('themeChanging' in root.dataset, false)
})
