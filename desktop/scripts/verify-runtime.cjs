'use strict'

const assert = require('node:assert/strict')
const fs = require('node:fs')
const path = require('node:path')
const vm = require('node:vm')
const { execFileSync } = require('node:child_process')
const asar = require('@electron/asar')
const { path7za } = require('7zip-bin')

const runtimeFiles = ['main.cjs', 'runtime.cjs', 'window-behavior.cjs']

function smokeArchive(archive, sourcePackage) {
  const expectedId = sourcePackage.build?.appId
  assert.ok(typeof expectedId === 'string' && expectedId.length > 0,
    'source package.json must contain build.appId')
  const metadata = JSON.parse(asar.extractFile(archive, 'package.json').toString('utf8'))
  assert.equal(metadata.main, 'main.cjs', 'archive package.json must select main.cjs')
  const ids = []
  let readyCalls = 0
  const forbidden = (name) => () => { throw new Error(`unexpected smoke-test side effect: ${name}`) }
  const blocked = (name) => new Proxy({}, { get: (_target, key) => forbidden(`${name}.${String(key)}`) })
  const paths = { appData: 'C:\\Users\\smoke\\AppData\\Roaming' }
  const app = {
    isPackaged: true,
    setAppUserModelId: (id) => ids.push(id),
    getPath: (name) => {
      assert.ok(paths[name], `unexpected app path: ${name}`)
      return paths[name]
    },
    setPath: (name, value) => { paths[name] = value },
    on: () => {},
    requestSingleInstanceLock: () => true,
    // Never resolve: loading the packaged entry point must not start services.
    whenReady: () => { readyCalls++; return new Promise(() => {}) },
    quit: forbidden('app.quit'),
    exit: forbidden('app.exit'),
  }
  const modules = {
    electron: { app, BrowserWindow: forbidden('BrowserWindow'), dialog: blocked('dialog'), session: blocked('session') },
    'node:fs': blocked('fs'),
    'node:path': path.win32,
    'node:child_process': blocked('child_process'),
    'node:http': blocked('http'),
    'node:crypto': blocked('crypto'),
    'node:events': require('node:events'),
  }
  const context = vm.createContext({
    process: {
      platform: 'win32', arch: 'x64', argv: ['C:\\Worklog\\Worklog.exe'],
      execPath: 'C:\\Worklog\\Worklog.exe', resourcesPath: 'C:\\Worklog\\resources',
    },
    Buffer, URL,
  })
  const cache = new Map([['package.json', { exports: metadata }]])
  function load(name) {
    if (cache.has(name)) return cache.get(name).exports
    assert.ok(runtimeFiles.includes(name), `unexpected archive module: ${name}`)
    const module = { exports: {} }
    cache.set(name, module)
    const archiveRequire = (request) => {
      if (Object.hasOwn(modules, request)) return modules[request]
      if (request.startsWith('./')) return load(request.slice(2))
      throw new Error(`unexpected smoke-test require: ${request}`)
    }
    // Execute the actual archive bytes, including its local runtime helpers.
    context.module = module
    context.exports = module.exports
    context.require = archiveRequire
    context.__filename = path.win32.join('C:\\Worklog\\resources\\app.asar', name)
    context.__dirname = path.win32.dirname(context.__filename)
    const code = asar.extractFile(archive, name).toString('utf8')
    new vm.Script(`(function(require, module, exports, __filename, __dirname) {\n${code}\n})(require, module, exports, __filename, __dirname)`, {
      filename: `${archive}/${name}`,
    }).runInContext(context, { timeout: 1000 })
    return module.exports
  }
  load(metadata.main)
  assert.deepEqual(ids, [expectedId], 'packaged AppUserModelId must match source build.appId')
  assert.equal(readyCalls, 1, 'packaged main must reach app.whenReady() exactly once')
}

function verifyPackagedRuntime(archive, sourceRoot) {
  for (const name of runtimeFiles) {
    assert.ok(asar.extractFile(archive, name).equals(fs.readFileSync(path.join(sourceRoot, name))),
      `stale packaged runtime: ${name} differs from source`)
  }
  smokeArchive(archive, JSON.parse(fs.readFileSync(path.join(sourceRoot, 'package.json'), 'utf8')))
}

function verifyZipArchive(artifact, archive) {
  const expected = fs.readFileSync(archive)
  // No text encoding: ASAR is binary. Size the stdout buffer for the full archive.
  const actual = execFileSync(path7za, ['x', '-so', artifact, 'resources/app.asar'], {
    encoding: 'buffer', maxBuffer: Math.max(1024 * 1024, expected.length + 1024 * 1024),
    stdio: ['ignore', 'pipe', 'pipe'],
  })
  assert.ok(actual.equals(expected), 'ZIP resources/app.asar differs from win-unpacked/resources/app.asar')
}

module.exports = { verifyPackagedRuntime, smokeArchive, verifyZipArchive }
