'use strict'
const fs = require('node:fs')
const path = require('node:path')
const { execFileSync } = require('node:child_process')
const { createHash } = require('node:crypto')
const root = path.resolve(__dirname, '../..')
const hash = (file) => createHash('sha256').update(fs.readFileSync(path.join(root, file))).digest('hex')
const pkg = require('../package.json')
let commit = 'unknown (source archive)'
try { commit = execFileSync('git', ['rev-parse', 'HEAD'], { cwd: root, encoding: 'utf8' }).trim() } catch {}
const info = {
  target: 'win-x64', version: pkg.version, commit,
  node: process.version,
  npm: execFileSync('npm', ['--version'], { encoding: 'utf8' }).trim(),
  go: execFileSync(process.env.WORKLOG_DESKTOP_GO_BIN, ['version'], { encoding: 'utf8' }).trim(),
  electron: pkg.devDependencies.electron,
  electronBuilder: pkg.devDependencies['electron-builder'],
  sevenZip: pkg.devDependencies['7zip-bin'],
  electronGet: pkg.devDependencies['@electron/get'],
  electronMirror: process.env.ELECTRON_MIRROR || 'https://github.com/electron/electron/releases/download/',
  electronChecksums: pkg.build.electronDownload.checksums,
  cgoEnabled: false,
  locks: Object.fromEntries(['go.mod', 'go.sum', 'Web/package-lock.json', 'desktop/package-lock.json'].map((f) => [f, hash(f)])),
}
fs.writeFileSync(path.join(root, 'desktop/build/BUILD-INFO.json'), JSON.stringify(info, null, 2) + '\n')
