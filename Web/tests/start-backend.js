// Disposable backend shared by browser smoke and the adapter integration test.
// Never opens ./data or inherits production database/credentials.
import { execFileSync, spawn } from 'node:child_process'
import { mkdtempSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { randomBytes } from 'node:crypto'

const root = fileURLToPath(new URL('../../', import.meta.url))
const dir = mkdtempSync(join(tmpdir(), 'worklog-smoke-'))
const bin = join(dir, 'worklog')
let child
function cleanup() { rmSync(dir, { recursive: true, force: true }) }
try {
  execFileSync('go', ['build', '-o', bin, './cmd/worklog'], { cwd: root, stdio: 'inherit' })
  child = spawn(bin, [], { cwd: root, stdio: 'inherit', env: {
    ...process.env,
    WORKLOG_ADDR: `127.0.0.1:${process.env.WORKLOG_E2E_PORT || '18080'}`,
    WORKLOG_DB_DRIVER: 'sqlite', WORKLOG_DB_DSN: join(dir, 'smoke.db'),
    WORKLOG_WRITE_TOKEN: randomBytes(32).toString('hex'), WORKLOG_READ_TOKEN: '',
    WORKLOG_BASIC_PASSWORD: '', WORKLOG_USER_ID: 'smoke', WORKLOG_TIMEZONE: 'Asia/Shanghai',
    WORKLOG_ALLOWED_HOSTS: 'localhost,127.0.0.1', WORKLOG_ALLOWED_ORIGINS: '',
    WORKLOG_STATIC_DIR: resolve(root, 'Web/dist'),
    WORKLOG_MCP_ALLOW_DESTRUCTIVE: 'false',
  } })
  for (const signal of ['SIGINT', 'SIGTERM']) process.on(signal, () => child.kill('SIGTERM'))
  child.on('error', error => { console.error(error); cleanup(); process.exit(1) })
  child.on('exit', code => { cleanup(); process.exit(code ?? 1) })
} catch (error) { cleanup(); throw error }
