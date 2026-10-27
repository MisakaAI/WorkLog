'use strict'

const assert = require('node:assert/strict')
const fs = require('node:fs')
const os = require('node:os')
const path = require('node:path')
const { execFileSync } = require('node:child_process')
const { test } = require('node:test')
const asar = require('@electron/asar')
const { path7za } = require('7zip-bin')
const { verifyPackagedRuntime, smokeArchive, verifyZipArchive } = require('../scripts/verify-runtime.cjs')

const root = path.resolve(__dirname, '..')
const sourcePackage = JSON.parse(fs.readFileSync(path.join(root, 'package.json'), 'utf8'))
const main = fs.readFileSync(path.join(root, 'main.cjs'), 'utf8')
const staleMain = main.replace("app.setAppUserModelId('org.worklog.desktop')",
  "app.setAppUserModelId(require('./package.json').build.appId)")

async function fixture(t, { code = main, metadata = { ...sourcePackage, build: undefined }, changes = {} } = {}) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'worklog-verify-'))
  t.after(() => { asar.uncacheAll(); fs.rmSync(dir, { recursive: true, force: true }) })
  const input = path.join(dir, 'input')
  fs.mkdirSync(input)
  for (const name of ['runtime.cjs', 'window-behavior.cjs']) {
    fs.writeFileSync(path.join(input, name), changes[name] ?? fs.readFileSync(path.join(root, name)))
  }
  fs.writeFileSync(path.join(input, 'main.cjs'), code)
  fs.writeFileSync(path.join(input, 'package.json'), JSON.stringify(metadata))
  const archive = path.join(dir, 'app.asar')
  await asar.createPackage(input, archive)
  return { dir, archive }
}

test('current archive boots to pending whenReady without build metadata or service startup', async (t) => {
  const { archive } = await fixture(t)
  verifyPackagedRuntime(archive, root)
})

test('stale main is rejected even if archive metadata masks the startup bug', async (t) => {
  assert.notEqual(staleMain, main)
  const { archive } = await fixture(t, { code: staleMain, metadata: sourcePackage })
  assert.throws(() => verifyPackagedRuntime(archive, root), /stale packaged runtime: main.cjs/)
})

for (const name of ['runtime.cjs', 'window-behavior.cjs']) {
  test(`stale ${name} bytes are rejected`, async (t) => {
    const { archive } = await fixture(t, { changes: { [name]: fs.readFileSync(path.join(root, name), 'utf8') + '\n// stale\n' } })
    assert.throws(() => verifyPackagedRuntime(archive, root), new RegExp(`stale packaged runtime: ${name}`))
  })
}

test('VM executes archived main against archived metadata, exposing missing build', async (t) => {
  const { archive } = await fixture(t, { code: staleMain })
  assert.throws(() => smokeArchive(archive, sourcePackage), /Cannot read properties of undefined.*appId/)
})

test('VM rejects incorrect AppUserModelId', async (t) => {
  const { archive } = await fixture(t, { code: main.replace("'org.worklog.desktop'", "'org.worklog.stale'") })
  assert.throws(() => smokeArchive(archive, sourcePackage), /AppUserModelId must match source/)
})

test('source build.appId is required even though archive build metadata is optional', async (t) => {
  const { archive } = await fixture(t)
  assert.throws(() => smokeArchive(archive, { ...sourcePackage, build: undefined }), /source package.json must contain build.appId/)
})

test('VM requires main to reach whenReady', async (t) => {
  const { archive } = await fixture(t, { code: "require('electron').app.setAppUserModelId('org.worklog.desktop')" })
  assert.throws(() => smokeArchive(archive, sourcePackage), /must reach app.whenReady/)
})

test('VM blocks startup side effects before readiness', async (t) => {
  const { archive } = await fixture(t, { code: "require('node:child_process').spawn('worklog.exe')" })
  assert.throws(() => smokeArchive(archive, sourcePackage), /unexpected smoke-test side effect: child_process.spawn/)
})

test('ZIP archive bytes must match unpacked archive, not merely exist', async (t) => {
  // npm may unpack 7zip-bin without its executable bit on Linux/macOS.
  if (process.platform !== 'win32') fs.chmodSync(path7za, fs.statSync(path7za).mode | 0o100)
  const { dir, archive } = await fixture(t)
  const resources = path.join(dir, 'resources')
  fs.mkdirSync(resources)
  fs.copyFileSync(archive, path.join(resources, 'app.asar'))
  const zip = path.join(dir, 'matching.zip')
  execFileSync(path7za, ['a', '-tzip', zip, 'resources/app.asar'], { cwd: dir, stdio: 'pipe' })
  verifyZipArchive(zip, archive)
  fs.appendFileSync(path.join(resources, 'app.asar'), '\nstale ZIP bytes\n')
  const staleZip = path.join(dir, 'stale.zip')
  execFileSync(path7za, ['a', '-tzip', staleZip, 'resources/app.asar'], { cwd: dir, stdio: 'pipe' })
  assert.throws(() => verifyZipArchive(staleZip, archive), /ZIP resources\/app.asar differs/)
})
