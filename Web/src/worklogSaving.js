// 内容和基准版本由同一个日期状态持有；只有采纳的读取或成功写入可以推进版本。
export function createWorklogDrafts() {
  const entries = new Map()
  let pending = false
  return {
    get size() { return [...entries.values()].filter(entry => entry.dirty).length },
    get: date => entries.get(date)?.dirty ? entries.get(date).content : undefined,
    has: date => Boolean(entries.get(date)?.dirty),
    load(entry, { discardDraft = false } = {}) {
      const current = entries.get(entry.date)
      // GET 可能早于刚完成的 PUT 取得快照，不能用它倒退本页面已确认的版本。
      if (discardDraft || (!current?.dirty && (!current || entry.version >= current.version))) {
        entries.set(entry.date, { content: entry.content, version: entry.version, dirty: false })
      }
      return entries.get(entry.date).content
    },
    set(date, content) {
      const entry = entries.get(date)
      if (!entry) throw new Error('请先读取日志后再编辑')
      entries.set(date, { ...entry, content, dirty: true })
    },
    async save(date, persist) {
      const draft = entries.get(date)
      if (pending || !draft?.dirty) return null
      pending = true
      try {
        const result = await persist(date, draft.content, draft.version)
        const current = entries.get(date)
        if (current === draft) {
          entries.set(date, { content: result.content, version: result.version, dirty: false })
        } else if (current?.version === draft.version) {
          // 保存期间的新编辑仍以本次提交为基础，保留正文并推进下一次保存版本。
          entries.set(date, { ...current, version: result.version })
        }
        return result
      } finally { pending = false }
    },
  }
}
