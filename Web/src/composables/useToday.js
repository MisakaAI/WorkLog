import { onBeforeUnmount, ref } from 'vue'

// 优先按服务端下发的业务时区取日期；未配置时才使用浏览器时区。
export function calendarToday(now = new Date(), timeZone = globalThis.WORKLOG_CONFIG?.businessTimezone) {
  const parts = new Intl.DateTimeFormat('en-US', { timeZone, year: 'numeric', month: '2-digit', day: '2-digit' }).formatToParts(now)
  const part = type => parts.find(item => item.type === type).value
  return `${part('year').padStart(4, '0')}-${part('month')}-${part('day')}`
}

export function useToday() {
  const today = ref(calendarToday())
  const refresh = () => { today.value = calendarToday() }
  // 后台标签页的定时器可能被节流，恢复可见时再刷新，及时发现跨日变化。
  const timer = window.setInterval(refresh, 30_000)
  document.addEventListener('visibilitychange', refresh)
  onBeforeUnmount(() => {
    window.clearInterval(timer)
    document.removeEventListener('visibilitychange', refresh)
  })
  return today
}
