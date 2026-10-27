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

test('配置读取完成之前不挂载；合法空JSON明确使用本地模式', async ({ page }) => {
  let release
  const gate = new Promise(resolve => { release = resolve })
  const apiRequests = []
  page.on('request', request => { if (new URL(request.url()).pathname.startsWith('/api/')) apiRequests.push(request.url()) })
  await page.route('**/config.json', async route => {
    await gate
    await route.fulfill({ json: {} })
  })
  await page.goto('/', { waitUntil: 'domcontentloaded' })
  await expect(page.locator('#app')).toHaveText('正在加载配置…')
  expect(apiRequests).toEqual([])
  release()
  await expect(page.getByRole('textbox', { name: '工作日志内容' })).toBeVisible()
  await expect(page.locator('.brand-word > span')).toHaveAttribute('data-backend-status', 'unconfigured')
  expect(apiRequests).toEqual([])
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
  await expect(page.locator('#app')).toHaveText('正在加载配置…')
  await page.clock.fastForward(6000)
  await expect(page.getByRole('alert')).toContainText('超时')
  await expect(page.getByRole('textbox', { name: '工作日志内容' })).toHaveCount(0)
})
