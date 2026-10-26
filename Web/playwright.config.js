import { defineConfig, devices } from '@playwright/test'

const port = process.env.WORKLOG_E2E_PORT || '22450'
export default defineConfig({
  testDir: './tests/e2e',
  workers: 1,
  fullyParallel: false,
  retries: process.env.CI ? 1 : 0,
  use: { baseURL: `http://127.0.0.1:${port}`, trace: 'retain-on-failure' },
  webServer: {
    command: 'node tests/start-backend.js',
    url: `http://127.0.0.1:${port}/healthz`,
    timeout: 90_000,
    reuseExistingServer: false,
    stdout: 'ignore', stderr: 'pipe',
  },
  projects: [
    { name: 'chromium', use: { ...devices['Desktop Chrome'], launchOptions: {
      ...(process.env.WORKLOG_CHROMIUM_EXECUTABLE ? { executablePath: process.env.WORKLOG_CHROMIUM_EXECUTABLE } : {}),
    } } },
    ...(process.env.WORKLOG_TEST_WEBKIT === '1' ? [{ name: 'webkit', use: { ...devices['Desktop Safari'] } }] : []),
  ],
})
