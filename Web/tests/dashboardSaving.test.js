import { test } from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { runInNewContext } from 'node:vm'

const source = readFileSync(new URL('../src/App.vue', import.meta.url), 'utf8')
const persistSource = source.slice(source.indexOf('async function persist()'), source.indexOf('async function saveTask()'))
function setup(saveDashboard) {
  const errors = []
  const state = {
    data: { value: { revision: 0, tasks: [{ title: '新修改' }] } },
    persisting: { value: false }, committed: { revision: 0, tasks: [{ title: '原记录' }] },
    saveDashboard, ElMessage: { error: message => errors.push(message) },
  }
  const persist = runInNewContext(`${persistSource};persist`, state)
  return { persist, state, errors }
}

test('工作台异步保存去重，只有服务端确认后提交版本，确认前不覆盖快照', async () => {
  let resolve
  let calls = 0
  const { persist, state } = setup(() => { calls++; return new Promise(done => { resolve = done }) })
  const pending = persist()
  assert.equal(state.persisting.value, true)
  assert.equal(await persist(), false)
  assert.equal(calls, 1)
  assert.equal(state.committed.tasks[0].title, '原记录')
  resolve({ revision: 1, tasks: [{ title: '服务端确认' }] })
  assert.equal(await pending, true)
  assert.equal(state.data.value.revision, 1)
  assert.equal(state.committed.tasks[0].title, '服务端确认')
  assert.equal(state.persisting.value, false)
})

test('工作台写入失败恢复已保存数据，不变更提交快照，并反馈错误', async () => {
  const { persist, state, errors } = setup(async () => { throw new Error('版本冲突') })
  assert.equal(await persist(), false)
  assert.equal(state.data.value.tasks[0].title, '原记录')
  assert.equal(state.committed.revision, 0)
  assert.equal(state.persisting.value, false)
  assert.match(errors[0], /版本冲突/)
})

test('表单仅在保存成功后关闭，待办仅在保存成功后清空输入，写入中有离页保护', () => {
  assert.match(source, /if \(await persist\(\)\) dialog\.value = false/)
  assert.match(source, /if \(await persist\(\)\) newTodo\.value = ''/)
  assert.match(source, /:inert="persisting"/)
  assert.match(source, /window\.addEventListener\('beforeunload', warnPendingSave\)/)
})
