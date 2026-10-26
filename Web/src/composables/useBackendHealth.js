import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { getBackendURL, requestJSON } from '../api/client.js'

// 仅展示连接状态，绝不据此切换数据来源、重试写入或访问本地业务缓存。
export function useBackendHealth() {
  const backendStatus = ref('unconfigured')
  const backendStatusLabel = computed(() => ({
    unconfigured: '未配置后端，业务数据保存在当前浏览器',
    checking: '正在检查后端连接',
    connected: '后端健康检查通过（不代表本次保存成功）',
    disconnected: '后端连接或健康检查失败（不会切换到浏览器存储）',
  })[backendStatus.value])
  let timer
  let stopped = false

  async function check() {
    try {
      const result = await requestJSON('/healthz')
      if (!stopped) backendStatus.value = result?.ok === true ? 'connected' : 'disconnected'
    } catch {
      if (!stopped) backendStatus.value = 'disconnected'
    } finally {
      // 完成后再计时，避免慢请求重叠；后续检查保留上次结果，圆点不闪烁。
      if (!stopped) timer = setTimeout(check, 30_000)
    }
  }

  onMounted(() => {
    try {
      if (!getBackendURL()) return
    } catch {
      backendStatus.value = 'disconnected'
      return
    }
    backendStatus.value = 'checking'
    void check()
  })
  onBeforeUnmount(() => {
    stopped = true
    clearTimeout(timer)
  })

  return { backendStatus, backendStatusLabel }
}
