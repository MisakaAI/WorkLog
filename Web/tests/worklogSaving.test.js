import { test } from 'node:test'
import assert from 'node:assert/strict'
import { createWorklogDrafts } from '../src/worklogSaving.js'

function deferred() {
  let resolve, reject
  const promise = new Promise((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}

test('仅保存脏草稿，成功后清理，无修改时不重复请求', async () => {
  const drafts = createWorklogDrafts()
  drafts.load({ date: '2026-10-19', content: {}, version: 0 })
  drafts.load({ date: '2026-10-20', content: {}, version: 0 })
  const calls = []
  const persist = async (date, content) => { calls.push({ date, content }); return { updatedAt: 'saved', version: 1 } }
  assert.equal(await drafts.save('2026-10-19', persist), null)
  const content = { type: 'doc' }
  drafts.set('2026-10-19', content)
  assert.equal(drafts.size, 1)
  assert.deepEqual(await drafts.save('2026-10-19', persist), { updatedAt: 'saved', version: 1 })
  assert.equal(drafts.has('2026-10-19'), false)
  assert.equal(await drafts.save('2026-10-19', persist), null)
  assert.deepEqual(calls, [{ date: '2026-10-19', content }])
})

test('定时、失焦及手动保存并发时只发送一次；保存期间的新修改不被清除', async () => {
  const drafts = createWorklogDrafts()
  drafts.load({ date: '2026-10-19', content: {}, version: 0 })
  drafts.load({ date: '2026-10-20', content: {}, version: 0 })
  const request = deferred()
  const first = { version: 1 }
  const second = { version: 2 }
  drafts.set('2026-10-19', first)
  const pending = drafts.save('2026-10-19', async (date, content) => {
    assert.equal(content, first)
    return request.promise
  })
  const unexpected = () => { throw new Error('不应并发保存') }
  assert.equal(await drafts.save('2026-10-19', unexpected), null)
  drafts.set('2026-10-19', second)
  request.resolve({ updatedAt: 'first saved', version: 1 })
  await pending
  assert.equal(drafts.get('2026-10-19'), second)
  await drafts.save('2026-10-19', async (date, content, version) => {
    assert.equal(version, 1)
    assert.equal(content, second)
    return { updatedAt: 'second saved', version: 2 }
  })
  assert.equal(drafts.size, 0)
})

test('切换日期不会保存错日或清除另一日期的草稿，失败后仍可重试', async () => {
  const drafts = createWorklogDrafts()
  drafts.load({ date: '2026-10-19', content: {}, version: 0 })
  drafts.load({ date: '2026-10-20', content: {}, version: 0 })
  const request = deferred()
  drafts.set('2026-10-19', { day: 19 })
  const pending = drafts.save('2026-10-19', () => request.promise)
  drafts.set('2026-10-20', { day: 20 })
  request.resolve({ updatedAt: 'saved', version: 1 })
  await pending
  assert.equal(drafts.has('2026-10-20'), true)
  await assert.rejects(drafts.save('2026-10-20', async () => { throw new Error('保存失败') }), /保存失败/)
  assert.equal(drafts.has('2026-10-20'), true)
  await drafts.save('2026-10-20', async (date, content) => {
    assert.equal(date, '2026-10-20')
    assert.deepEqual(content, { day: 20 })
    return { updatedAt: 'saved', version: 1 }
  })
  assert.equal(drafts.size, 0)
})

test('重读不改变草稿版本，只有明确放弃草稿才能采纳新版本', async () => {
  const drafts = createWorklogDrafts()
  const date = '2026-11-01'
  drafts.load({ date, content: { text: 'v1' }, version: 1 })
  drafts.set(date, { text: 'my draft' })
  assert.deepEqual(drafts.load({ date, content: { text: 'v2' }, version: 2 }), { text: 'my draft' })
  await assert.rejects(drafts.save(date, async (_, content, version) => {
    assert.equal(version, 1)
    throw new Error('conflict')
  }))
  drafts.load({ date, content: { text: 'v2' }, version: 2 }, { discardDraft: true })
  assert.equal(drafts.size, 0)
  drafts.set(date, { text: 'edited v2' })
  await drafts.save(date, async (_, content, version) => {
    assert.equal(version, 2)
    return { content, version: 3 }
  })
  assert.deepEqual(drafts.load({ date, content: { text: 'stale read' }, version: 2 }), { text: 'edited v2' })
  drafts.set(date, { text: 'next edit' })
  await drafts.save(date, async (_, content, version) => {
    assert.equal(version, 3)
    return { content, version: 4 }
  })
})
