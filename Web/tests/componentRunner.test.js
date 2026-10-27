import { test } from 'node:test'
import { execFile } from 'node:child_process'
import { promisify } from 'node:util'
import { fileURLToPath } from 'node:url'

const execute = promisify(execFile)
const webRoot = fileURLToPath(new URL('../', import.meta.url))
const runner = fileURLToPath(new URL('../scripts/test-components.js', import.meta.url))

test('组件测试入口覆盖继承的 production 模式，事件断言仍然生效', { timeout: 60_000 }, async () => {
  // Exercise the real drag/drop event assertions that previously failed when
  // Vue's production runtime prevented Test Utils from recording emits.
  await execute(process.execPath, [runner, 'run', 'tests/components/taskOrdering.spec.js'], {
    cwd: webRoot,
    env: { ...process.env, NODE_ENV: 'production' },
    timeout: 50_000,
  })
})
