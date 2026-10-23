import { onMounted, ref } from 'vue'
import { getWorkCalendar } from '../api/index.js'
import { parseWorkCalendar } from '../workCalendar.js'

// 工作台只维护一份日历状态，日期面板与甘特图共用加载结果和重试状态。
export function useWorkCalendar() {
  const workCalendar = ref(null)
  const calendarLoading = ref(false)
  const calendarError = ref(false)
  let pending
  function loadCalendar() {
    // 合并尚未完成的加载，避免多个视图同时重试造成重复请求。
    if (pending) return pending
    calendarLoading.value = true
    calendarError.value = false
    pending = (async () => {
      try { workCalendar.value = parseWorkCalendar(await getWorkCalendar()) }
      catch { calendarError.value = true }
      finally { calendarLoading.value = false; pending = null }
    })()
    return pending
  }
  onMounted(loadCalendar)
  return { workCalendar, calendarLoading, calendarError, loadCalendar }
}
