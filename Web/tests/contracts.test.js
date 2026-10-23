import { test } from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { nameKey, parseDashboard, validateCategories, validateProjects } from '../src/dashboard.js'
import { isWorklogColor } from '../src/worklogColor.js'

const fixtures = name => JSON.parse(readFileSync(new URL(`../../internal/domain/testdata/${name}.json`, import.meta.url), 'utf8'))

test('名称键与 Go 使用同一 Unicode 简单小写协议，后端合法快照可读取', () => {
  for (const { name, key } of fixtures('name_keys')) assert.equal(nameKey(name), key, name)
  for (const categories of [['ΟΣ', 'ος'], ['İ', 'i'], ['Project', 'project']]) {
    const duplicate = nameKey(categories[0]) === nameKey(categories[1])
    const projects = categories.map((name, i) => ({ id: String(i), name }))
    assert.equal(Boolean(validateCategories(categories)), duplicate)
    assert.equal(Boolean(validateProjects(projects)), duplicate)
    if (!duplicate) assert.doesNotThrow(() => parseDashboard({ projects, categories, tasks: [], todos: [], revision: 1 }))
  }
})

test('编辑器颜色白名单与后端文档校验共享契约', () => {
  for (const { color, valid } of fixtures('colors')) assert.equal(isWorklogColor(color), valid, color)
})
