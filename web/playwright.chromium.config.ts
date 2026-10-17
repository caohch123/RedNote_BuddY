import { defineConfig } from '@playwright/test';
import base from './playwright.config';

export default defineConfig({
  ...base,
  use: { ...base.use, channel: undefined, launchOptions: { executablePath: process.env.XHS_TEST_CHROMIUM || undefined } },
});
