'use strict'
const { test } = require('node:test')
const assert = require('node:assert/strict')
const fs = require('node:fs')
const os = require('node:os')
const path = require('node:path')
const http = require('node:http')
const { once, EventEmitter } = require('node:events')
const { PassThrough } = require('node:stream')
const { prepareConfig, isAppPage, isAppRequest, health, Backend } = require('../runtime.cjs')

const origin = 'http://127.0.0.1:51432'

test('desktop config is loopback :0, independent, stable, and read by --conf', (t) => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'worklog-desktop-test-'))
  t.after(() => fs.rmSync(dir, { recursive: true, force: true }))
  const data = path.join(dir, '用户数据')
  const web = path.join(dir, '安装路径', 'web')
  const file = prepareConfig(data, web)
  const first = JSON.parse(fs.readFileSync(file))
  const second = JSON.parse(fs.readFileSync(prepareConfig(data, web)))
  assert.deepEqual(first, second)
  assert.equal(first.addr, '127.0.0.1:0')
  assert.equal(first.db_driver, 'sqlite')
  assert.equal(first.db_dsn, path.join(data, 'worklog.db'))
  assert.equal(first.static_dir, web)
  assert.equal(first.write_token, fs.readFileSync(first.token_file, 'utf8'))
  assert.match(first.write_token, /^[a-f0-9]{64}$/)
  assert.deepEqual(first.allowed_origins, [])
  fs.writeFileSync(first.token_file, 'bad')
  assert.throws(() => prepareConfig(data, web), /令牌文件损坏/)
})

test('navigation is restricted to the app document at the exact dynamic origin', () => {
  for (const url of [`${origin}/`, `${origin}/#worklog`, `${origin}/index.html`]) assert.ok(isAppPage(url, origin))
  for (const url of ['https://example.com/', 'file:///C:/test', 'javascript:alert(1)',
    'http://127.0.0.1:51433/', `${origin}/api/dashboard`, 'http://user@127.0.0.1:51432/']) {
    assert.equal(isAppPage(url, origin), false, url)
  }
  assert.ok(isAppRequest(`${origin}/api/dashboard`, origin, 'xhr'))
  assert.ok(isAppRequest(`blob:${origin}/worker`, origin, 'script'))
  assert.ok(isAppRequest('data:image/png;base64,AA', origin, 'image'))
  for (const url of ['https://example.com/a.js', 'http://127.0.0.1:51433/api',
    'file:///C:/test', 'data:text/html,hello', 'blob:https://example.com/id']) {
    assert.equal(isAppRequest(url, origin, 'script'), false, url)
  }
})

async function healthServer(t, handler) {
  const server = http.createServer(handler)
  server.listen(0, '127.0.0.1')
  await once(server, 'listening')
  t.after(() => new Promise((done) => server.close(done)))
  return server.address().port
}

function fakeBackend(t) {
  const child = new EventEmitter()
  child.stdout = new PassThrough()
  child.stderr = new PassThrough()
  child.stdin = new PassThrough()
  child.stdin.on('close', () => setImmediate(() => child.emit('close', 0)))
  child.kill = () => child.emit('close', 1)
  let args
  const backend = new Backend({
    supervisor: 'supervisor.exe', executable: 'worklog.exe', configFile: 'backend.json',
    dataDir: 'data', log() {},
    spawnProcess(...value) { args = value; return child },
  })
  t.after(() => backend.stop())
  return { backend, child, get args() { return args } }
}

test('health requires successful JSON and does not follow redirects', async (t) => {
  let mode = 0
  const port = await healthServer(t, (_req, res) => {
    if (mode === 0) res.end('{"ok":true}')
    else if (mode === 1) { res.writeHead(503); res.end('{"ok":true}') }
    else if (mode === 2) res.end('not json')
    else { res.writeHead(302, { Location: 'http://example.com/' }); res.end() }
  })
  for (mode = 0; mode < 4; mode++) assert.equal(await health(`http://127.0.0.1:${port}`), mode === 0)
})

test('waits for health before ready; launches original backend via supervisor; cleans pipe', async (t) => {
  let healthy = false
  const port = await healthServer(t, (_req, res) => {
    res.writeHead(healthy ? 200 : 503)
    res.end(JSON.stringify({ ok: healthy }))
  })
  const mock = fakeBackend(t)
  const start = mock.backend.start()
  let resolved = false
  start.then(() => { resolved = true })
  mock.child.stdout.write(`{"type":"listening","pid":42,"port":${port}}\n`)
  await new Promise((done) => setTimeout(done, 50))
  assert.equal(resolved, false)
  healthy = true
  assert.equal(await start, `http://127.0.0.1:${port}`)
  assert.deepEqual(mock.args[1], ['worklog.exe', 'backend.json'])
  assert.equal(mock.args[2].windowsHide, true)
  await mock.backend.stop()
  assert.equal(mock.child.stdin.destroyed, true)
})

test('spawn errors and invalid port reports are understandable failures', async (t) => {
  for (const kind of ['error', 'port', 'exit']) {
    const { backend, child } = fakeBackend(t)
    const start = backend.start()
    if (kind === 'error') child.emit('error', new Error('ENOENT'))
    else if (kind === 'exit') child.emit('exit', 3)
    else child.stdout.write('{"type":"listening","pid":42,"port":0}\n')
    await assert.rejects(start, kind === 'error' ? /监管进程/ : kind === 'exit' ? /意外退出/ : /实际监听端口/)
  }
})

test('unexpected service exit after startup is reported', async (t) => {
  const port = await healthServer(t, (_req, res) => res.end('{"ok":true}'))
  const { backend, child } = fakeBackend(t)
  const start = backend.start()
  child.stdout.write(`{"type":"listening","pid":42,"port":${port}}\n`)
  await start
  const failed = once(backend, 'failure')
  child.emit('exit', 2)
  assert.match((await failed)[0].message, /意外退出/)
})

test('startup timeout rejects and shutdown still closes the supervisor pipe', async (t) => {
  t.mock.timers.enable({ apis: ['setTimeout'] })
  const { backend, child } = fakeBackend(t)
  const start = backend.start()
  t.mock.timers.tick(90000)
  await assert.rejects(start, /启动超时/)
  const stop = backend.stop()
  child.emit('close', 0)
  await stop
  assert.equal(child.stdin.destroyed, true)
})

test('shutdown forcibly terminates an unresponsive supervisor after five seconds', async (t) => {
  t.mock.timers.enable({ apis: ['setTimeout'] })
  const { backend, child } = fakeBackend(t)
  const start = backend.start()
  child.emit('error', new Error('startup failure'))
  await assert.rejects(start)
  child.stdin.removeAllListeners('close')
  let killed = false
  child.kill = () => { killed = true; child.emit('close', 1) }
  const stop = backend.stop()
  t.mock.timers.tick(5000)
  await stop
  assert.equal(killed, true)
})
