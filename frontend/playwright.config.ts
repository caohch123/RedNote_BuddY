import { defineConfig } from '@playwright/test';

export default defineConfig({
  testDir: './tests/e2e', fullyParallel: false, workers: 1, timeout: 90_000,
  expect: { timeout: 15_000 },
  use: { baseURL: process.env.XHS_LIVE_URL || 'http://127.0.0.1:5173', viewport: { width: 1536, height: 1000 },
    // Browser acceptance and visual evidence use the installed Microsoft Edge.
    channel: 'msedge', trace: 'retain-on-failure', screenshot: 'only-on-failure' },
  webServer: process.env.XHS_LIVE_URL ? undefined : { command: 'npm run dev', url: 'http://127.0.0.1:5173', reuseExistingServer: !process.env.CI },
  reporter: [['list']],
});
