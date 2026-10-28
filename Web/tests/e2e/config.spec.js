import { test, expect } from '@playwright/test'
import { createServer } from 'node:http'
import { readFile } from 'node:fs/promises'

for (const scenario of [
  { name: '缺失', status: 404, json: {} },
  { name: '未认证', status: 401, json: {} },
  { name: '禁止访问', status: 403, json: {} },
  { name: '服务端错误', status: 503, json: {} },
  { name: '损坏JSON', body: '{broken', contentType: 'application/json' },
  { name: '非法URL', json: { apiBaseUrl: '//evil.test/api' } },
  { name: '非法时区', json: { businessTimezone: 'Bad/Timezone' } },
  { name: '网络失败', abort: true },
]) {
  test(`配置${scenario.name}阻止挂载，不请求业务API或写入本地数据`, async ({ page }) => {
    const apiRequests = []
    page.on('request', request => { if (new URL(request.url()).pathname.startsWith('/api/')) apiRequests.push(request.url()) })
    await page.route('**/config.json', route => {
      if (scenario.abort) return route.abort('failed')
      const { name, ...options } = scenario
      return route.fulfill(options)
    })
    await page.goto('/')
    await expect(page.getByRole('alert')).toContainText('启动失败')
    await expect(page.getByRole('alert')).toContainText('config.json')
    await expect(page.getByRole('textbox', { name: '工作日志内容' })).toHaveCount(0)
    expect(apiRequests).toEqual([])
    expect(await page.evaluate(() => Object.keys(localStorage).filter(key => key.startsWith('worklog.v2.')))).toEqual([])
  })
}

test('配置和应用资源并行加载，配置读取完成之前不挂载；合法空JSON使用本地模式', async ({ page }) => {
  const appLoaded = page.waitForEvent('requestfinished', request => /\/assets\/main-[^/]+\.js$/.test(new URL(request.url()).pathname))
  let release
  const gate = new Promise(resolve => { release = resolve })
  const apiRequests = []
  page.on('request', request => { if (new URL(request.url()).pathname.startsWith('/api/')) apiRequests.push(request.url()) })
  await page.route('**/config.json', async route => {
    await gate
    await route.fulfill({ json: {} })
  })
  await page.goto('/', { waitUntil: 'domcontentloaded' })
  await expect(page.getByRole('status')).toContainText('正在加载配置…')
  await expect(page.locator('.startup-loader__ring')).toBeVisible()
  // 配置仍被拦截时，主应用 JS 已下载，不再串行等待配置。
  await appLoaded
  await expect(page.getByRole('textbox', { name: '工作日志内容' })).toHaveCount(0)
  expect(await page.evaluate(() => Object.keys(localStorage).filter(key => key.startsWith('worklog.v2.')))).toEqual([])
  expect(apiRequests).toEqual([])
  release()
  await expect(page.getByRole('textbox', { name: '工作日志内容' })).toBeVisible()
  await expect(page.locator('.startup-loader')).toHaveCount(0)
  await expect(page.locator('.brand-word > span')).toHaveAttribute('data-backend-status', 'unconfigured')
  expect(apiRequests).toEqual([])
})

for (const scenario of [
  { name: '浅色桌面', saved: 'light', system: 'dark', width: 1280, height: 800, background: 'rgb(237, 240, 245)' },
  { name: '深色移动端', saved: 'dark', system: 'light', width: 375, height: 667, background: 'rgb(23, 26, 33)' },
  { name: '跟随系统深色', system: 'dark', width: 1280, height: 800, background: 'rgb(23, 26, 33)' },
  { name: '减少动态效果的小屏', system: 'light', width: 320, height: 568, reduced: true, background: 'rgb(237, 240, 245)' },
]) {
  test(`启动动画：${scenario.name}，应用样式加载前即可显示`, async ({ page }) => {
    await page.setViewportSize({ width: scenario.width, height: scenario.height })
    await page.emulateMedia({ colorScheme: scenario.system, reducedMotion: scenario.reduced ? 'reduce' : 'no-preference' })
    await page.addInitScript(saved => {
      if (saved) localStorage.setItem('worklog-theme', saved)
      else localStorage.removeItem('worklog-theme')
    }, scenario.saved)
    let release
    const gate = new Promise(resolve => { release = resolve })
    await page.route('**/config.json', async route => {
      await gate
      await route.fulfill({ json: {} })
    })
    // 主应用现在提前加载；主动延迟 CSS，验证内联动画仍独立于外部样式。
    await page.route('**/*.css', async route => {
      await gate
      await route.continue()
    })
    try {
      await page.goto('/', { waitUntil: 'domcontentloaded' })
      const loader = page.getByRole('status')
      await expect(loader).toBeVisible()
      await expect(loader).toContainText('正在加载配置…')
      await expect(loader).toHaveCSS('background-color', scenario.background)
      const card = page.locator('.startup-loader__card')
      expect(await card.evaluate(el => getComputedStyle(el).boxShadow)).not.toBe('none')
      const bounds = await card.boundingBox()
      expect(bounds.x).toBeGreaterThanOrEqual(24)
      expect(bounds.x + bounds.width).toBeLessThanOrEqual(scenario.width - 24)
      expect(Math.abs(bounds.y + bounds.height / 2 - scenario.height / 2)).toBeLessThan(1)
      const ring = page.locator('.startup-loader__ring')
      await expect(ring).toHaveCSS('animation-name', scenario.reduced ? 'none' : 'startup-orbit')
      if (!scenario.reduced) await expect(ring).toHaveCSS('animation-iteration-count', 'infinite')
      // 配置与外部样式尚未返回，启动动画仍可正常显示。
      expect(await page.evaluate(() => [...document.styleSheets].some(sheet => sheet.href))).toBe(false)
      await expect(page.getByRole('textbox', { name: '工作日志内容' })).toHaveCount(0)
    } finally {
      release()
    }
    await expect(page.getByRole('textbox', { name: '工作日志内容' })).toBeVisible()
    await expect(page.locator('.startup-loader')).toHaveCount(0)
  })
}

test('配置就绪但应用资源未完成时提示准备应用，且不访问业务API', async ({ page }) => {
  let release
  const gate = new Promise(resolve => { release = resolve })
  const apiRequests = []
  page.on('request', request => { if (new URL(request.url()).pathname.startsWith('/api/')) apiRequests.push(request.url()) })
  await page.route('**/config.json', route => route.fulfill({ json: { apiBaseUrl: '/api' } }))
  await page.route('**/assets/main-*.js', async route => {
    await gate
    await route.continue()
  })
  try {
    await page.goto('/', { waitUntil: 'domcontentloaded' })
    await expect(page.getByRole('status')).toContainText('正在准备应用…')
    await expect(page.getByRole('textbox', { name: '工作日志内容' })).toHaveCount(0)
    expect(apiRequests).toEqual([])
  } finally {
    release()
  }
  await expect(page.getByRole('textbox', { name: '工作日志内容' })).toBeVisible()
  expect(apiRequests.length).toBeGreaterThan(0)
})

test('独立静态构建在子目录读取默认JSON，CSP同源连接下保持本地模式', async ({ page }) => {
  const server = createServer(async (req, res) => {
    const path = new URL(req.url, 'http://localhost').pathname
    if (!path.startsWith('/worklog/')) { res.writeHead(404).end(); return }
    const name = path.slice('/worklog/'.length) || 'index.html'
    const types = { html: 'text/html', js: 'text/javascript', css: 'text/css', json: 'application/json', png: 'image/png', svg: 'image/svg+xml' }
    try {
      const body = await readFile(new URL(`../../dist/${name}`, import.meta.url))
      res.setHeader('Content-Type', types[name.split('.').at(-1)] || 'application/octet-stream')
      res.setHeader('Content-Security-Policy', "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; connect-src 'self'; img-src 'self' data:; worker-src 'self' blob:")
      res.end(body)
    } catch { res.writeHead(404).end() }
  })
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve))
  const requests = []
  page.on('request', request => requests.push(new URL(request.url()).pathname))
  try {
    await page.goto(`http://127.0.0.1:${server.address().port}/worklog/`)
    await expect(page.getByRole('textbox', { name: '工作日志内容' })).toBeVisible()
    await expect(page.locator('.brand-word > span')).toHaveAttribute('data-backend-status', 'unconfigured')
    expect(requests).toContain('/worklog/config.json')
    expect(requests.some(path => path.includes('/api/') || path.endsWith('/config.js'))).toBe(false)
    expect(await page.evaluate(() => window.WORKLOG_CONFIG)).toEqual({})
  } finally {
    await new Promise(resolve => server.close(resolve))
  }
})

test('配置请求超时显示启动错误，不挂载', async ({ page }) => {
  await page.clock.install()
  await page.route('**/config.json', () => {})
  await page.goto('/', { waitUntil: 'domcontentloaded' })
  await expect(page.getByRole('status')).toContainText('正在加载配置…')
  await page.clock.fastForward(6000)
  await expect(page.getByRole('alert')).toContainText('超时')
  await expect(page.locator('.startup-loader')).toHaveCount(0)
  await expect(page.getByRole('textbox', { name: '工作日志内容' })).toHaveCount(0)
})
