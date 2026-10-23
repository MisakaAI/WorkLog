import { computed, nextTick, onScopeDispose, ref, watch } from 'vue'

const STORAGE_KEY = 'worklog-theme'

export function useTheme() {
  const system = window.matchMedia('(prefers-color-scheme: dark)')
  let saved
  try { saved = localStorage.getItem(STORAGE_KEY) } catch { /* 存储不可用时仍可切换 */ }
  const theme = ref(saved === 'dark' || saved === 'light' ? saved : system.matches ? 'dark' : 'light')
  // 用户明确选择后不再跟随系统变化；没有偏好时才自动跟随系统主题。
  let manuallySelected = saved === 'dark' || saved === 'light'
  const isDark = computed(() => theme.value === 'dark')

  function apply(value) {
    const root = document.documentElement
    root.dataset.theme = value
    // Element Plus 的暗色变量也作用于 teleport 到 body 的弹窗和下拉框。
    root.classList.toggle('dark', value === 'dark')
    root.style.colorScheme = value
    root.style.backgroundColor = value === 'dark' ? '#171a21' : '#edf0f5'
    document.querySelector('meta[name="theme-color"]')?.setAttribute('content', value === 'dark' ? '#1b1e26' : '#edf0f5')
  }
  let transition
  function applyWithoutTransitions(value) {
    const root = document.documentElement
    root.dataset.themeChanging = ''
    try {
      apply(value)
      // 先提交完整的新配色，避免组件 transition 混用新旧背景、边框和阴影。
      void root.offsetWidth
    } finally {
      delete root.dataset.themeChanging
    }
  }
  watch(theme, (value, previous) => {
    transition?.skipTransition()
    if (previous === undefined || !document.startViewTransition || window.matchMedia('(prefers-reduced-motion: reduce)').matches) {
      applyWithoutTransitions(value)
      return
    }
    const current = document.startViewTransition(async () => {
      if (theme.value !== value) return
      applyWithoutTransitions(value)
      // 将主题图标及依赖主题的编辑器更新一并纳入新画面。
      await nextTick()
    })
    transition = current
    current.finished.catch(() => {}).finally(() => {
      if (transition === current) transition = undefined
    })
  }, { immediate: true, flush: 'sync' })

  function toggleTheme() {
    manuallySelected = true
    theme.value = isDark.value ? 'light' : 'dark'
    try { localStorage.setItem(STORAGE_KEY, theme.value) } catch { /* 保留会话内选择 */ }
  }
  function onSystemChange(event) {
    if (!manuallySelected) theme.value = event.matches ? 'dark' : 'light'
  }
  system.addEventListener('change', onSystemChange)
  onScopeDispose(() => {
    transition?.skipTransition()
    system.removeEventListener('change', onSystemChange)
  })
  return { isDark, toggleTheme }
}
