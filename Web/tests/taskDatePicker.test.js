import { test } from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { runInNewContext } from 'node:vm'

const source = readFileSync(new URL('../src/components/TaskDatePicker.vue', import.meta.url), 'utf8')

function setup() {
  const target = {}
  const model = { value: '2027-06-15' }
  const script = source.match(/<script setup>([\s\S]*?)<\/script>/)[1].replace(/^import .*$/gm, '')
  const api = runInNewContext(`${script}\n;({ onVisibleChange, returnToNow, footerTarget, defaultDate })`, {
    defineOptions() {}, defineModel: () => model,
    useId: () => 'v-0', ref: value => ({ value }), nextTick: async () => {},
    document: { querySelector(selector) {
      assert.equal(selector, '.task-date-picker-v-0 .el-picker-panel__body-wrapper')
      return target
    } },
  })
  return { api, model, target }
}

test('现在只更新面板默认日期，不改变所选值', () => {
  const { api, model } = setup()
  const previous = api.defaultDate.value
  api.returnToNow()
  assert.notEqual(api.defaultDate.value, previous)
  assert.ok(Math.abs(api.defaultDate.value.getTime() - Date.now()) < 1000)
  assert.equal(model.value, '2027-06-15')
  model.value = ''
  api.returnToNow()
  assert.equal(model.value, '')
})

test('打开后定位独立日历区域的父容器，关闭时清理传送目标', async () => {
  const { api, target } = setup()
  await api.onVisibleChange(true)
  assert.equal(api.footerTarget.value, target)
  await api.onVisibleChange(false)
  assert.equal(api.footerTarget.value, null)
})

test('今天使用圆圈标识且不覆盖已选中样式', () => {
  assert.match(source, /td\.today:not\(\.current\) \.el-date-table-cell__text/)
  assert.match(source, /border-radius: 50%/)
  assert.match(source, /outline: 2px solid var\(--el-color-primary\)/)
  assert.match(source, /<Teleport v-if="footerTarget" :to="footerTarget">/)
  assert.match(source, /class="task-date-picker-footer"/)
  assert.match(source, /justify-content: flex-end/)
  assert.match(source, /type="button"[^>]*@click\.stop="returnToNow"/)
})
