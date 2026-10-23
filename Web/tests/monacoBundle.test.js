import { test } from 'node:test'
import assert from 'node:assert/strict'
import { fileURLToPath } from 'node:url'
import { build } from 'vite'

test('生产依赖图不会通过 HTML worker 隐式重新注册全量编辑功能', async () => {
  let checked = false
  await build({
    root: fileURLToPath(new URL('..', import.meta.url)),
    logLevel: 'silent',
    build: { write: false, minify: false, reportCompressedSize: false },
    plugins: [{
      name: 'assert-monaco-feature-graph',
      generateBundle(_, bundle) {
        const modules = Object.values(bundle).flatMap(chunk => chunk.type === 'chunk' ? Object.keys(chunk.modules) : [])
        assert.ok(modules.some(id => id.endsWith('/src/monacoWorkers.js')))
        for (const suffix of [
          '/features/register.all.js',
          '/internal/common/workers.js',
          '/diffEditor/diffEditor.contribution.js',
          '/codelens/browser/codelensController.js',
          '/inlayHints/browser/inlayHintsContribution.js',
          '/inlineCompletions/browser/inlineCompletions.contribution.js',
          '/rename/browser/rename.js',
        ]) {
          assert.ok(!modules.some(id => id.endsWith(suffix)), `不应打包：${suffix}`)
        }
        checked = true
      },
    }],
  })
  assert.ok(checked)
})
