import { test, expect } from '@playwright/test'

test('真实健康接口显示墨绿色，检查失败变深红，恢复后自动变绿且不切换数据源', async ({ page }) => {
  await page.clock.install()
  let unhealthy = false
  await page.route('**/api/healthz', route => unhealthy
    ? route.fulfill({ status: 503, json: { error: { message: '健康检查测试故障' } } })
    : route.continue())
  await page.goto('/')
  const dot = page.locator('.brand-word > span')
  await expect(dot).toHaveAttribute('data-backend-status', 'connected')
  await expect(dot).toHaveCSS('color', 'rgb(27, 94, 70)')
  unhealthy = true
  await page.clock.fastForward(31_000)
  await expect(dot).toHaveAttribute('data-backend-status', 'disconnected')
  await expect(dot).toHaveCSS('color', 'rgb(139, 38, 53)')
  await expect(page.getByRole('textbox', { name: '工作日志内容' })).toBeVisible()
  expect(await page.evaluate(() => Object.keys(localStorage).filter(key => key.startsWith('worklog.v2.')))).toEqual([])
  unhealthy = false
  await page.clock.fastForward(31_000)
  await expect(dot).toHaveAttribute('data-backend-status', 'connected')
  await expect(dot).toHaveCSS('color', 'rgb(27, 94, 70)')
})

test('未配置后端保留原色，页面可用且不发送健康检查或业务 API 请求', async ({ page }) => {
  const apiRequests = []
  page.on('request', request => { if (new URL(request.url()).pathname.startsWith('/api/')) apiRequests.push(request.url()) })
  await page.route('**/config.json', route => route.fulfill({ json: { apiBaseUrl: '' } }))
  await page.goto('/')
  const dot = page.locator('.brand-word > span')
  await expect(dot).toHaveAttribute('data-backend-status', 'unconfigured')
  await expect(dot).toHaveCSS('color', 'rgb(112, 91, 159)')
  await expect(page.getByRole('textbox', { name: '工作日志内容' })).toBeVisible()
  expect(apiRequests).toEqual([])
})
