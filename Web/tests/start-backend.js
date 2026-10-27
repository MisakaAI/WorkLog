// Disposable backend shared by browser smoke and the adapter integration test.
// Never opens ./data or inherits production database/credentials.
import { execFileSync, spawn } from 'node:child_process'
import { mkdtempSync, rmSync, writeFileSync } from 'node:fs'
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
  const conf = join(dir, 'config.json')
  writeFileSync(conf, JSON.stringify({
    addr: `127.0.0.1:${process.env.WORKLOG_E2E_PORT || '22450'}`,
    db_driver: 'sqlite', db_dsn: join(dir, 'smoke.db'),
    write_token: randomBytes(32).toString('hex'), read_token: '', token_file: join(dir, 'mcp-token'),
    basic_password: '', user_id: 'smoke', timezone: 'Asia/Shanghai',
    allowed_hosts: ['localhost', '127.0.0.1'], allowed_origins: [],
    static_dir: resolve(root, 'Web/dist'),
    mcp_allow_destructive: false,
  }), { mode: 0o600 })
  // --conf ignores business environment variables inherited from the caller.
  child = spawn(bin, ['--conf', conf], { cwd: root, stdio: 'inherit' })
  for (const signal of ['SIGINT', 'SIGTERM']) process.on(signal, () => child.kill('SIGTERM'))
  child.on('error', error => { console.error(error); cleanup(); process.exit(1) })
  child.on('exit', code => { cleanup(); process.exit(code ?? 1) })
} catch (error) { cleanup(); throw error }
