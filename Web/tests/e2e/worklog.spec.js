import { test, expect } from '@playwright/test'

// Every run uses the disposable Go service configured in playwright.config.js.
test('真实浏览器保存待办和富文本，刷新后仍可读取', async ({ page }) => {
  const failures = []
  page.on('pageerror', error => failures.push(error.message))
  await page.goto('/')
  const editor = page.getByRole('textbox', { name: '工作日志内容' })
  await expect(editor).toBeVisible()
  await editor.fill('浏览器保存的工作日志')
  await page.getByRole('button', { name: '保存', exact: true }).click()
  await expect(page.getByText(/已保存 ·/)).toBeVisible()
  await page.getByRole('textbox', { name: '新待办' }).fill('浏览器待办')
  await page.getByRole('button', { name: '添加待办', exact: true }).click()
  await expect(page.locator('.todo-items')).toContainText('浏览器待办')
  await page.reload()
  await expect(editor).toContainText('浏览器保存的工作日志')
  await expect(page.locator('.todo-items')).toContainText('浏览器待办')
  expect(failures).toEqual([])
})

test('版本冲突不重复自动提交，确认重新加载后显示远端内容', async ({ page, request }) => {
  await page.clock.install()
  await page.goto('/')
  const editor = page.getByRole('textbox', { name: '工作日志内容' })
  await expect(editor).toBeVisible()
  const today = await (await request.get('/api/today')).json()
  const content = { type: 'doc', content: [{ type: 'paragraph', content: [{ type: 'text', text: '另一个客户端保存的内容' }] }] }
  const saved = await request.put(`/api/worklog/${today.date}`, { data: { version: today.version, content } })
  expect(saved.status()).toBe(200)
  let puts = 0
  page.on('request', req => { if (req.method() === 'PUT' && req.url().includes('/api/worklog/')) puts++ })
  await editor.fill('冲突时不能覆盖的本地草稿')
  await page.getByRole('button', { name: '保存', exact: true }).click()
  await expect(page.getByText('版本冲突 · 自动保存已暂停，请先复制草稿，再重新加载')).toBeVisible()
  await page.clock.fastForward(31_000)
  expect(puts).toBe(1)
  await page.getByRole('button', { name: '重新加载（先复制草稿）', exact: true }).click()
  await page.getByRole('button', { name: '放弃草稿并重新加载', exact: true }).click()
  await expect(editor).toContainText('另一个客户端保存的内容')
  expect(puts).toBe(1)
  await expect(page.getByRole('button', { name: '撤销', exact: true })).toBeDisabled()
})

test('加载隔离撤销历史，源码导入过滤颜色后仍可保存并重新打开', async ({ page }) => {
  await page.goto('/')
  const editor = page.getByRole('textbox', { name: '工作日志内容' })
  await expect(editor).toBeVisible()
  await expect(page.getByRole('button', { name: '撤销', exact: true })).toBeDisabled()
  for (let i = 0; i < 2; i++) {
    await page.getByRole('button', { name: '源代码', exact: true }).click()
    const source = page.getByRole('textbox', { name: '工作日志 HTML 源代码', exact: true })
    await expect(source).toBeVisible()
    await source.focus()
    // Monaco 根据 User-Agent 选择键位；WebKit 模拟 Safari 时与宿主 OS 不同。
    await page.keyboard.press(await page.evaluate(() => navigator.userAgent.includes('Macintosh') ? 'Meta+A' : 'Control+A'))
    await page.keyboard.insertText(`<h2>源码保存 ${i}</h2><p><strong><span style="color: oklch(60% 0.2 25)">保留正文</span></strong></p>`)
    await page.getByRole('dialog', { name: 'HTML 源代码' }).getByRole('button', { name: '保存', exact: true }).click()
    await expect(editor.locator('h2')).toHaveText(`源码保存 ${i}`)
    await expect(editor.locator('strong')).toHaveText('保留正文')
    await expect(page.getByText('已移除不支持的文字颜色，正文和其他格式已保留。')).toBeVisible()
    await expect(page.getByText(/已保存 ·/)).toBeVisible()
  }
  await page.reload()
  await expect(editor.locator('h2')).toHaveText('源码保存 1')
  await expect(editor.locator('strong')).toHaveText('保留正文')
  await expect(page.getByRole('button', { name: '撤销', exact: true })).toBeDisabled()
})
