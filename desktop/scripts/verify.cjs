'use strict'
const assert = require('node:assert/strict')
const fs = require('node:fs')
const path = require('node:path')
const { execFileSync } = require('node:child_process')
const { path7za } = require('7zip-bin')
const { verifyPackagedRuntime, verifyZipArchive } = require('./verify-runtime.cjs')
const root = path.resolve(__dirname, '..')
const unpacked = path.join(root, 'dist/win-unpacked')
const archive = path.join(unpacked, 'resources/app.asar')
verifyPackagedRuntime(archive, root)

function assertPE64(file) {
  const bytes = fs.readFileSync(file)
  assert.equal(bytes.toString('ascii', 0, 2), 'MZ', `not a PE file: ${file}`)
  const pe = bytes.readUInt32LE(0x3c)
  assert.equal(bytes.toString('ascii', pe, pe + 4), 'PE\0\0')
  assert.equal(bytes.readUInt16LE(pe + 4), 0x8664, `not AMD64: ${file}`)
  assert.equal(bytes.readUInt16LE(pe + 24), 0x20b, `not PE32+: ${file}`)
}
for (const name of ['Worklog.exe', 'resources/backend/worklog.exe', 'resources/backend/worklog-supervisor.exe']) {
  assertPE64(path.join(unpacked, name))
}
for (const name of ['resources/app.asar', 'resources/tray.ico', 'resources/web/index.html', 'resources/web/config.js',
  'resources/BUILD-INFO.json', 'locales/en-US.pak', 'locales/zh-CN.pak', 'icudtl.dat', 'resources.pak',
  'electron_wer.dll', 'ffmpeg.dll', 'chrome_100_percent.pak', 'chrome_200_percent.pak',
  'v8_context_snapshot.bin', 'LICENSE.electron.txt', 'LICENSES.chromium.html']) {
  assert.ok(fs.statSync(path.join(unpacked, name)).size > 0, `missing resource: ${name}`)
}
assert.ok(fs.readFileSync(path.join(unpacked, 'resources/tray.ico')).equals(
  fs.readFileSync(path.resolve(root, '../Web/public/favicon.ico'))), 'packaged tray icon differs from source')
assert.ok(fs.readdirSync(path.join(unpacked, 'resources/web/assets')).some((f) => f.endsWith('.js')))
const info = JSON.parse(fs.readFileSync(path.join(unpacked, 'resources/BUILD-INFO.json')))
assert.equal(info.target, 'win-x64')
assert.equal(info.cgoEnabled, false)
const version = require('../package.json').version
const installer = process.argv[2] === 'installer'
const artifact = path.join(root, 'dist', `Worklog-${version}-win-x64${installer ? '-setup.exe' : '.zip'}`)
assert.ok(fs.statSync(artifact).size > 1024 * 1024)
if (installer) assertPE64Installer(artifact)
else {
  execFileSync(path7za, ['t', artifact], { stdio: 'pipe' })
  verifyZipArchive(artifact, archive)
  const list = execFileSync(path7za, ['l', artifact], { encoding: 'utf8' })
  for (const name of ['Worklog.exe', 'resources/backend/worklog.exe', 'resources/backend/worklog-supervisor.exe',
    'resources/web/index.html', 'resources/app.asar', 'resources/tray.ico', 'icudtl.dat']) assert.ok(list.includes(name), `archive missing ${name}`)
}
console.log(`已验证 AMD64 Go 后端、监管进程、Electron 和 Vue 资源：${artifact}`)

function assertPE64Installer(file) {
  // NSIS itself is a 32-bit bootstrapper, containing the x64 app checked above.
  const bytes = fs.readFileSync(file)
  assert.equal(bytes.toString('ascii', 0, 2), 'MZ')
}
