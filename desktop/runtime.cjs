'use strict'

const { spawn } = require('node:child_process')
const { EventEmitter } = require('node:events')
const http = require('node:http')
const fs = require('node:fs')
const path = require('node:path')
const crypto = require('node:crypto')

function prepareConfig(dataDir, staticDir) {
  fs.mkdirSync(dataDir, { recursive: true, mode: 0o700 })
  const tokenFile = path.join(dataDir, 'mcp-token')
  try {
    fs.writeFileSync(tokenFile, crypto.randomBytes(32).toString('hex'), { flag: 'wx', mode: 0o600 })
  } catch (error) {
    if (error.code !== 'EEXIST') throw error
  }
  const token = fs.readFileSync(tokenFile, 'utf8').trim()
  if (!/^[a-f0-9]{64}$/.test(token)) throw new Error('桌面 MCP 令牌文件损坏，请从备份恢复或移走 mcp-token 后重试。')
  const configFile = path.join(dataDir, 'backend.json')
  // --conf ignores inherited WORKLOG_* variables. Supply the stable token
  // explicitly so the desktop and backend share the same persistent credential.
  const config = {
    addr: '127.0.0.1:0', db_driver: 'sqlite',
    db_dsn: path.join(dataDir, 'worklog.db'),
    token_file: tokenFile, write_token: token,
    static_dir: staticDir, user_id: 'owner', timezone: 'Asia/Shanghai',
    allowed_hosts: ['127.0.0.1'], allowed_origins: [],
    mcp_allow_destructive: false,
  }
  fs.writeFileSync(configFile, JSON.stringify(config, null, 2), { mode: 0o600 })
  return configFile
}

function isAppPage(value, origin) {
  try {
    const url = new URL(value)
    return url.origin === origin && !url.username && !url.password &&
      (url.pathname === '/' || url.pathname === '/index.html')
  } catch { return false }
}

function isAppRequest(value, origin, resourceType) {
  try {
    const url = new URL(value)
    if (url.username || url.password) return false
    if (url.protocol === 'http:') return url.origin === origin
    if (url.protocol === 'blob:') return url.origin === origin
    return url.protocol === 'data:' && ['image', 'font'].includes(resourceType)
  } catch { return false }
}

function health(url) {
  return new Promise((resolve) => {
    const req = http.get(`${url}/healthz`, { timeout: 2500, agent: false }, (res) => {
      let body = ''
      res.setEncoding('utf8')
      res.on('data', (part) => {
        body += part
        if (body.length > 4096) req.destroy(new Error('health response too large'))
      })
      res.on('error', () => resolve(false))
      res.on('end', () => {
        try { resolve(res.statusCode === 200 && JSON.parse(body).ok === true) }
        catch { resolve(false) }
      })
    })
    req.on('timeout', () => req.destroy(new Error('health timeout')))
    req.on('error', () => resolve(false))
  })
}

class Backend extends EventEmitter {
  constructor({ supervisor, executable, configFile, dataDir, log, spawnProcess = spawn }) {
    super()
    Object.assign(this, { supervisor, executable, configFile, dataDir, log, spawnProcess })
    this.stopping = false
    this.ready = false
  }

  start() {
    return new Promise((resolve, reject) => {
      this.rejectStart = reject
      this.startTimer = setTimeout(() => this.fail(new Error('后端启动超时（90 秒）。请检查数据目录权限和桌面日志。')), 90000)
      let child
      try {
        child = this.spawnProcess(this.supervisor, [this.executable, this.configFile], {
          cwd: this.dataDir, windowsHide: true, detached: false,
          stdio: ['pipe', 'pipe', 'pipe'],
        })
      } catch (error) { this.fail(error); return }
      this.child = child
      this.closed = new Promise((done) => child.once('close', done))
      child.stdin.on('error', () => {}) // EPIPE is reported through child exit.
      child.stderr.on('data', (part) => this.log(part.toString()))
      child.on('error', (error) => this.fail(new Error(`无法启动 Windows 后端监管进程：${error.message}`)))
      child.once('exit', (code, signal) => this.fail(new Error(`后端服务意外退出（代码 ${code}，信号 ${signal || '无'}）。请查看日志。`)))
      let buffer = ''
      child.stdout.setEncoding('utf8')
      child.stdout.on('data', (part) => {
        buffer += part
        if (buffer.length > 8192) { this.fail(new Error('后端端口通知异常。')); return }
        let end
        while ((end = buffer.indexOf('\n')) !== -1) {
          const line = buffer.slice(0, end)
          buffer = buffer.slice(end + 1)
          try {
            const message = JSON.parse(line)
            if (this.url || message.type !== 'listening' || !Number.isInteger(message.port) ||
                message.port < 1 || message.port > 65535 || !Number.isInteger(message.pid) || message.pid <= 0) {
              throw new Error('invalid listener message')
            }
            this.url = `http://127.0.0.1:${message.port}`
            void this.waitForHealth(resolve)
          } catch { this.fail(new Error('无法获取后端实际监听端口。')) }
        }
      })
    })
  }

  async waitForHealth(resolve) {
    while (!this.stopping && !this.failed) {
      if (await health(this.url)) {
        if (this.stopping || this.failed) return
        clearTimeout(this.startTimer)
        this.ready = true
        resolve(this.url)
        this.monitor()
        return
      }
      await new Promise((done) => setTimeout(done, 250))
    }
  }

  monitor() {
    let failures = 0
    let checking = false
    this.monitorTimer = setInterval(async () => {
      if (checking || this.stopping || this.failed) return
      checking = true
      const ok = await health(this.url)
      checking = false
      failures = ok ? 0 : failures + 1
      if (failures >= 3) this.fail(new Error('后端连续三次健康检查失败，数据库或本机服务可能不可用。请退出并检查日志。'))
    }, 10000)
  }

  fail(error) {
    if (this.stopping || this.failed) return
    this.failed = true
    clearTimeout(this.startTimer)
    clearInterval(this.monitorTimer)
    this.log(`${error.stack || error}\n`)
    if (!this.ready) this.rejectStart(error)
    else this.emit('failure', error)
  }

  async stop() {
    this.stopping = true
    clearTimeout(this.startTimer)
    clearInterval(this.monitorTimer)
    if (!this.child) return
    // Closing the control pipe makes the supervisor close its Job Object.
    this.child.stdin.destroy()
    let timer
    const exited = await Promise.race([
      this.closed.then(() => true),
      new Promise((done) => { timer = setTimeout(() => done(false), 5000) }),
    ])
    clearTimeout(timer)
    if (!exited) {
      // Killing the supervisor also closes its sole job handle in the kernel.
      this.child.kill()
      await this.closed
    }
  }
}

module.exports = { prepareConfig, isAppPage, isAppRequest, health, Backend }
