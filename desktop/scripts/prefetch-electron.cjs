'use strict'

const fs = require('node:fs/promises')
const { createHash } = require('node:crypto')
const { downloadArtifact } = require('@electron/get')
const pkg = require('../package.json')

async function prefetch() {
  const filename = `electron-v${pkg.devDependencies.electron}-win32-x64.zip`
  const expected = pkg.build.electronDownload.checksums[filename]
  if (!expected) throw new Error(`缺少 ${filename} 的固定 SHA-256，请同步更新打包配置。`)
  const upstream = require('electron/checksums.json')[filename]
  if (upstream !== expected) throw new Error('固定的 Electron SHA-256 与锁定的 npm 包不一致。')
  const localZip = process.env.WORKLOG_ELECTRON_ZIP
  if (localZip) {
    const hash = createHash('sha256').update(await fs.readFile(localZip)).digest('hex')
    if (hash !== expected) throw new Error('WORKLOG_ELECTRON_ZIP 校验失败：文件不是指定版本的官方 Windows x64 Electron ZIP。')
  }
  let milestone = -1
  const archive = await downloadArtifact({
    version: pkg.devDependencies.electron, platform: 'win32', arch: 'x64', artifactName: 'electron',
    checksums: pkg.build.electronDownload.checksums,
    // Populate @electron/get's standard cache before electron-builder's stricter
    // 10-minute request timeout applies. Never disable checksum/TLS validation.
    downloadOptions: {
      timeout: { request: 30 * 60 * 1000 },
      quiet: true,
      getProgressCallback: async ({ percent }) => {
        const current = Math.floor(percent * 10)
        if (current > milestone) { milestone = current; console.log(`Electron 下载：${current * 10}%`) }
      },
    },
    ...(localZip ? { downloader: { download: async (_url, destination) => fs.copyFile(localZip, destination) } } : {}),
  })
  console.log(`Electron 已校验并缓存：${archive}`)
}

if (require.main === module) prefetch().catch((error) => { console.error(error.message); process.exitCode = 1 })
module.exports = { prefetch }
