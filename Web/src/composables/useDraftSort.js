import { ref } from 'vue'

// 项目和分类共用排序交互；只修改草稿，保存后才写入正式数据。
export function useDraftSort(items, type) {
  const draggedId = ref('')
  const dropIndex = ref(null)
  const transferType = `application/x-worklog-${type}`

  function reset() {
    draggedId.value = ''
    dropIndex.value = null
  }
  function start(event, id) {
    reset()
    draggedId.value = id
    event.dataTransfer.setData(transferType, id)
    event.dataTransfer.effectAllowed = 'move'
  }
  function preview(event) {
    if (!draggedId.value) return
    event.preventDefault()
    event.dataTransfer.dropEffect = 'move'
    const rows = [...event.currentTarget.querySelectorAll('.project-sort-row')]
    const index = rows.findIndex(row => {
      const rect = row.getBoundingClientRect()
      return event.clientY < rect.top + rect.height / 2
    })
    dropIndex.value = index < 0 ? rows.length : index
  }
  function leave(event) {
    if (!event.currentTarget.contains(event.relatedTarget)) dropIndex.value = null
  }
  function drop(event) {
    const sourceId = event.dataTransfer.getData(transferType)
    const from = items.value.findIndex(item => item.id === sourceId)
    if (sourceId === draggedId.value && from >= 0 && dropIndex.value !== null) {
      // 插入位置以移除前的列表为准，向下移动时扣除源条目占据的位置。
      const to = dropIndex.value - (from < dropIndex.value ? 1 : 0)
      const reordered = [...items.value]
      const [item] = reordered.splice(from, 1)
      reordered.splice(to, 0, item)
      items.value = reordered
    }
    reset()
  }
  function move(id, offset) {
    const from = items.value.findIndex(item => item.id === id)
    const to = from + offset
    if (from < 0 || to < 0 || to >= items.value.length) return
    const reordered = [...items.value]
    const [item] = reordered.splice(from, 1)
    reordered.splice(to, 0, item)
    items.value = reordered
  }

  return { draggedId, dropIndex, reset, start, preview, leave, drop, move }
}
