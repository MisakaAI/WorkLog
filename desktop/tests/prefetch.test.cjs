'use strict'
const { test } = require('node:test')
const assert = require('node:assert/strict')
const fs = require('node:fs')
const os = require('node:os')
const path = require('node:path')
const pkg = require('../package.json')
const { prefetch } = require('../scripts/prefetch-electron.cjs')

test('pinned Windows runtime checksum matches the lockfile-installed Electron package', () => {
  const filename = `electron-v${pkg.devDependencies.electron}-win32-x64.zip`
  assert.match(pkg.build.electronDownload.checksums[filename], /^[a-f0-9]{64}$/)
  assert.equal(pkg.build.electronDownload.checksums[filename], require('electron/checksums.json')[filename])
})

test('pre-downloaded runtime is rejected before caching if its checksum is wrong', async (t) => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'worklog-electron-check-'))
  const file = path.join(dir, 'invalid.zip')
  fs.writeFileSync(file, 'not an official runtime')
  const previous = process.env.WORKLOG_ELECTRON_ZIP
  t.after(() => {
    if (previous === undefined) delete process.env.WORKLOG_ELECTRON_ZIP
    else process.env.WORKLOG_ELECTRON_ZIP = previous
    fs.rmSync(dir, { recursive: true, force: true })
  })
  process.env.WORKLOG_ELECTRON_ZIP = file
  await assert.rejects(prefetch(), /校验失败/)
})

test('an Electron version update cannot silently omit its fixed checksum', async (t) => {
  const previous = pkg.devDependencies.electron
  t.after(() => { pkg.devDependencies.electron = previous })
  pkg.devDependencies.electron = '0.0.0'
  await assert.rejects(prefetch(), /缺少.*SHA-256/)
})
