import { test } from 'node:test'
import assert from 'node:assert/strict'
import { ref } from 'vue'
import { useDraftSort } from '../src/composables/useDraftSort.js'
import { parseDashboard, replaceCategories } from '../src/dashboard.js'
import { readFileSync } from 'node:fs'

function setup(type = 'category') {
  const items = ref(['开发', '设计', '调研'].map((name, index) => ({ id: String(index), name, originalName: name })))
  const sort = useDraftSort(items, type)
  const transfer = new Map()
  const event = { dataTransfer: { setData: (key, value) => transfer.set(key, value), getData: key => transfer.get(key) || '' } }
  return { items, sort, event }
}

for (const type of ['category', 'project']) {
  test(`${type}：首尾拖动与指示位置一致，释放后清理状态`, () => {
    const { items, sort, event } = setup(type)
    sort.start(event, '0')
    const list = { querySelectorAll: () => items.value.map((_, index) => ({ getBoundingClientRect: () => ({ top: index * 42, height: 32 }) })) }
    let prevented = false
    sort.preview({ ...event, currentTarget: list, clientY: 130, preventDefault: () => { prevented = true } })
    assert.ok(prevented)
    assert.equal(sort.dropIndex.value, 3)
    sort.drop(event)
    assert.deepEqual(items.value.map(item => item.id), ['1', '2', '0'])
    assert.equal(sort.draggedId.value, '')
    assert.equal(sort.dropIndex.value, null)
    sort.start(event, '0')
    sort.dropIndex.value = 0
    sort.drop(event)
    assert.deepEqual(items.value.map(item => item.id), ['0', '1', '2'])
  })
}

test('分类：方向键排序有边界，保存重读后保留顺序且不改变关联分类', () => {
  const { items, sort } = setup()
  sort.move('0', -1)
  sort.move('2', 1)
  assert.deepEqual(items.value.map(item => item.id), ['0', '1', '2'])
  sort.move('2', -1)
  sort.move('2', -1)
  const data = JSON.parse(readFileSync(new URL('./fixtures/dashboard.json', import.meta.url), 'utf8'))
  data.tasks = [{ ...data.tasks[0], tag: '开发' }]
  const saved = replaceCategories(data, items.value)
  assert.deepEqual(saved.categories, ['调研', '开发', '设计'])
  assert.equal(saved.tasks[0].tag, '开发')
  assert.deepEqual(parseDashboard(JSON.parse(JSON.stringify(saved))).categories, saved.categories)
})

test('离开列表或取消拖动不会修改草稿', () => {
  const { items, sort, event } = setup()
  sort.start(event, '0')
  sort.dropIndex.value = 3
  sort.leave({ currentTarget: { contains: () => false }, relatedTarget: null })
  assert.equal(sort.dropIndex.value, null)
  sort.drop(event)
  assert.deepEqual(items.value.map(item => item.id), ['0', '1', '2'])
})
