// 局域网 HTTP 下可能没有 randomUUID，但通常仍可使用 getRandomValues。
// 持久化 ID 必须使用安全随机数，不能退回时间戳或 Math.random。
export function newID(crypto = globalThis.crypto) {
  if (typeof crypto?.randomUUID === 'function') return crypto.randomUUID()
  if (typeof crypto?.getRandomValues !== 'function') throw new Error('浏览器不支持安全 ID 生成，请使用 localhost、HTTPS 或更新浏览器')
  const bytes = crypto.getRandomValues(new Uint8Array(16))
  // 设置 UUID v4 的版本位和变体位，其余位保留随机性。
  bytes[6] = (bytes[6] & 0x0f) | 0x40
  bytes[8] = (bytes[8] & 0x3f) | 0x80
  const hex = [...bytes].map(value => value.toString(16).padStart(2, '0')).join('')
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`
}
