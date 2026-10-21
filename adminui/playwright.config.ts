import { defineConfig, devices } from '@playwright/test';

const PORT = 4173;

/**
 * 冒烟用例：针对 `pnpm build` 的产物（vite preview）运行，接口全部在浏览器里拦截（见 e2e/support/api.ts），
 * 不依赖后端。`pnpm test:e2e` 会先构建；CI 里构建步骤已经跑过，直接执行 `pnpm exec playwright test`。
 * 本地没有下载 Playwright 浏览器时，用 PW_CHANNEL=chrome 或 msedge 复用系统浏览器。
 */
export default defineConfig({
  testDir: './e2e',
  testMatch: '**/*.e2e.ts',
  fullyParallel: true,
  workers: process.env.CI ? 2 : 3,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? [['github'], ['html', { open: 'never' }]] : 'list',
  expect: { timeout: 10_000 },
  use: {
    baseURL: `http://localhost:${PORT}`,
    trace: 'retain-on-failure',
    locale: 'zh-CN',
  },
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'], channel: process.env.PW_CHANNEL || undefined, viewport: { width: 1440, height: 900 } },
    },
  ],
  webServer: {
    command: `pnpm exec vite preview --port ${PORT} --strictPort`,
    url: `http://localhost:${PORT}`,
    reuseExistingServer: !process.env.CI,
    timeout: 120_000,
  },
});
