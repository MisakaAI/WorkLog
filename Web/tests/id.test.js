import { test } from 'node:test'
import assert from 'node:assert/strict'
import { newID } from '../src/id.js'
import { calendarToday } from '../src/composables/useToday.js'

test('LAN HTTP 使用 getRandomValues 生成 UUID v4，不降级到弱随机 ID', () => {
  assert.equal(newID({ randomUUID: () => 'native' }), 'native')
  const id = newID({ getRandomValues: bytes => { bytes.fill(0xff); return bytes } })
  assert.equal(id, 'ffffffff-ffff-4fff-bfff-ffffffffffff')
  assert.throws(() => newID({}), /localhost、HTTPS/)
})

test('今天按明确业务时区计算，不依赖浏览器时区', () => {
  const now = new Date('2026-10-20T16:30:00Z')
  assert.equal(calendarToday(now, 'Asia/Shanghai'), '2026-10-21')
  assert.equal(calendarToday(now, 'America/Los_Angeles'), '2026-10-20')
})
