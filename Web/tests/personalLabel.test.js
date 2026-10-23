import { test } from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { runInNewContext } from 'node:vm'
import { parseDashboard } from '../src/dashboard.js'

const source = readFileSync(new URL('../src/App.vue', import.meta.url), 'utf8')
const fixture = () => JSON.parse(readFileSync(new URL('./fixtures/dashboard.json', import.meta.url), 'utf8'))
function setup(initial = {}, success = true) {
  const data = { value: initial }
  const warnings = []
  let saves = 0
  const script = source.slice(source.indexOf('const editingPersonalLabel'), source.indexOf("const search ="))
  const api = runInNewContext(`${script}\n;({ editPersonalLabel, savePersonalLabel, cancelPersonalLabel, editingPersonalLabel, personalLabelDraft, personalLabelInput, personalLabel })`, {
    data, persisting: { value: false }, ref: value => ({ value }), computed: fn => ({ get value() { return fn() } }), nextTick: async () => {},
    persist: () => { saves++; return success }, ElMessage: { warning: message => warnings.push(message) },
  })
  return { api, data, warnings, saves: () => saves }
}

test('工作台文字缺省兼容，允许空串和255码元，拒绝非法值', () => {
  const data = fixture()
  assert.equal(parseDashboard(data), data)
  for (const personalLabel of ['', '个人工作台', '字'.repeat(255)]) {
    assert.equal(parseDashboard({ ...data, personalLabel }).personalLabel, personalLabel)
  }
  for (const personalLabel of [null, 123, '字'.repeat(256)]) {
    assert.throws(() => parseDashboard({ ...data, personalLabel }), /数据格式/)
  }
})

test('双击聚焦并选择文字，回车组词不保存，保存trim后刷新可读', async () => {
  const { api, data, saves } = setup()
  let focused = false
  let selected = false
  api.personalLabelInput.value = { focus() { focused = true }, select() { selected = true } }
  await api.editPersonalLabel()
  assert.equal(api.personalLabelDraft.value, '个人工作台')
  assert.ok(focused && selected)
  api.personalLabelDraft.value = ' 我的工作台 '
  api.savePersonalLabel({ isComposing: true })
  assert.equal(saves(), 0)
  await api.savePersonalLabel()
  assert.equal(data.value.personalLabel, '我的工作台')
  assert.equal(JSON.parse(JSON.stringify(data.value)).personalLabel, '我的工作台')
  assert.equal(api.editingPersonalLabel.value, false)
  api.savePersonalLabel()
  assert.equal(saves(), 1)
})

test('取消不保存、空值可保存，超长保留编辑，存储失败回滚并保留草稿', async () => {
  const { api, data, warnings, saves } = setup({ personalLabel: '原值' })
  await api.editPersonalLabel()
  api.personalLabelDraft.value = '修改'
  api.cancelPersonalLabel()
  api.savePersonalLabel()
  assert.equal(data.value.personalLabel, '原值')
  assert.equal(saves(), 0)
  await api.editPersonalLabel()
  api.personalLabelDraft.value = '字'.repeat(256)
  api.savePersonalLabel()
  assert.equal(warnings.length, 1)
  assert.equal(api.editingPersonalLabel.value, true)
  api.personalLabelDraft.value = ' '
  await api.savePersonalLabel()
  assert.equal(data.value.personalLabel, '')

  const failed = setup({}, false)
  await failed.api.editPersonalLabel()
  failed.api.personalLabelDraft.value = '未保存'
  await failed.api.savePersonalLabel()
  assert.equal(failed.data.value.personalLabel, undefined)
  assert.equal(failed.api.editingPersonalLabel.value, true)
  assert.equal(failed.api.personalLabelDraft.value, '未保存')
})
