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

test('启动脚本尚未返回时，HTML 已提前请求主应用 JS 和 CSS', async ({ page }) => {
  let release
  const gate = new Promise(resolve => { release = resolve })
  const appRequested = page.waitForRequest(/\/assets\/main-[^/]+\.js$/)
  const cssRequested = page.waitForRequest(/\/assets\/main-[^/]+\.css$/)
  await page.route('**/assets/index-*.js', async route => {
    await gate
    await route.continue()
  })
  await page.route('**/config.json', route => route.fulfill({ json: {} }))
  try {
    await page.goto('/', { waitUntil: 'commit' })
    await Promise.all([appRequested, cssRequested])
    await expect(page.getByRole('status')).toContainText('正在加载…')
    await expect(page.getByRole('textbox', { name: '工作日志内容' })).toHaveCount(0)
  } finally {
    release()
  }
  await expect(page.getByRole('textbox', { name: '工作日志内容' })).toBeVisible()
})

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
  await expect(page.getByRole('status')).toContainText('正在加载…')
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
      await expect(loader).toContainText('正在加载…')
      await expect(loader).toHaveCSS('background-color', scenario.background)
      const card = page.locator('.startup-loader__card')
      expect(await card.evaluate(el => getComputedStyle(el).boxShadow)).not.toBe('none')
      const bounds = await card.boundingBox()
      expect(bounds.x).toBeGreaterThanOrEqual(24)
      expect(bounds.x + bounds.width).toBeLessThanOrEqual(scenario.width - 24)
      expect(Math.abs(bounds.y + bounds.height / 2 - scenario.height / 2)).toBeLessThan(1)
      const ring = page.locator('.startup-loader__ring')
      await expect(ring).toHaveCSS('animation-name', 'startup-orbit')
      await expect(ring).toHaveCSS('animation-timing-function', 'ease-out')
      await expect(ring.locator('circle')).toHaveCount(1)
      const arc = ring.locator('.startup-loader__arc')
      await expect(arc).toHaveCSS('fill', 'none')
      await expect(arc).toHaveCSS('animation-name', 'startup-stretch')
      await expect(arc).toHaveCSS('animation-duration', scenario.reduced ? '4s' : '1.2s')
      await expect(arc).toHaveCSS('animation-iteration-count', 'infinite')
      const samples = await arc.evaluate(el => {
        const animation = el.getAnimations()[0]
        const duration = animation.effect.getTiming().duration
        const previousTime = animation.currentTime
        animation.pause()
        const values = [0, 0.5, 0.6, 0.7, 0.8, 0.9, 0.999, 1].map(progress => {
          animation.currentTime = duration * progress
          const style = getComputedStyle(el)
          const [length, gap] = style.strokeDasharray.split(/[,\s]+/).map(parseFloat)
          const tail = -parseFloat(style.strokeDashoffset)
          return { length, period: length + gap, tail, head: tail + length }
        })
        animation.currentTime = previousTime
        animation.play()
        return values
      })
      expect([samples[0].length, samples[1].length, samples.at(-1).length]).toEqual([12, 65, 12])
      for (const sample of samples) expect(sample.period).toBeCloseTo(100, 2)
      // 收短期间前后端都向前，不依靠外圈旋转掩盖前端回缩。
      for (let index = 2; index < samples.length - 1; index++) {
        expect(samples[index].length).toBeLessThan(samples[index - 1].length)
        expect(samples[index].tail).toBeGreaterThan(samples[index - 1].tail)
        expect(samples[index].head).toBeGreaterThan(samples[index - 1].head)
      }
      // 周期末与下圈起点只相差整圈，避免循环接缝跳动。
      expect(samples.at(-2).head - 100).toBeCloseTo(samples.at(-1).head, 2)
      expect(samples.at(-2).tail - 100).toBeCloseTo(samples.at(-1).tail, 2)
      await expect(ring).toHaveCSS('animation-duration', scenario.reduced ? '4s' : '1.2s')
      await expect(ring).toHaveCSS('animation-iteration-count', 'infinite')
      const transform = await ring.evaluate(el => getComputedStyle(el).transform)
      await expect.poll(() => ring.evaluate(el => getComputedStyle(el).transform)).not.toBe(transform)
      const logo = page.locator('.startup-loader__logo')
      await expect(logo).toHaveAttribute('src', './favicon.svg')
      await expect.poll(() => logo.evaluate(el => el.complete && el.naturalWidth > 0)).toBe(true)
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

test('减少动态效果时，应用全局样式加载后圆环仍持续慢速旋转', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'reduce' })
  let release
  const gate = new Promise(resolve => { release = resolve })
  await page.route('**/config.json', async route => {
    await gate
    await route.fulfill({ json: {} })
  })
  try {
    await page.goto('/', { waitUntil: 'domcontentloaded' })
    await expect.poll(() => page.evaluate(() => [...document.styleSheets].some(sheet => sheet.href))).toBe(true)
    const ring = page.locator('.startup-loader__ring')
    await expect(ring).toHaveCSS('animation-duration', '4s')
    await expect(ring).toHaveCSS('animation-iteration-count', 'infinite')
    await expect(ring.locator('.startup-loader__arc')).toHaveCSS('animation-duration', '4s')
    await expect(ring.locator('.startup-loader__arc')).toHaveCSS('animation-iteration-count', 'infinite')
    const transform = await ring.evaluate(el => getComputedStyle(el).transform)
    await expect.poll(() => ring.evaluate(el => getComputedStyle(el).transform)).not.toBe(transform)
  } finally {
    release()
  }
  await expect(page.locator('.startup-loader')).toHaveCount(0)
})

test('配置就绪但应用资源未完成时保持统一动画，且不访问业务API', async ({ page }) => {
  const configLoaded = page.waitForEvent('requestfinished', request => new URL(request.url()).pathname === '/config.json')
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
    await configLoaded
    await expect(page.getByRole('status')).toContainText('正在加载…')
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
  await expect(page.getByRole('status')).toContainText('正在加载…')
  await page.clock.fastForward(6000)
  await expect(page.getByRole('alert')).toContainText('超时')
  await expect(page.locator('.startup-loader')).toHaveCount(0)
  await expect(page.getByRole('textbox', { name: '工作日志内容' })).toHaveCount(0)
})
