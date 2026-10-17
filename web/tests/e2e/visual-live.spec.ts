import { expect, test } from '@playwright/test';
import path from 'node:path';

test('deployed Xiaohongshu rewrite workspace exposes image and video tabs', async ({ page }) => {
  test.skip(process.env.XHS_LIVE_UI !== '1', 'read-only live UI verification is opt-in');
  await page.goto('/');
  const switcher = page.getByRole('button', { name: '切换到小红书工作区', exact: true });
  if (await switcher.count()) await switcher.click();
  await page.getByRole('menuitem', { name: /仿写库/ }).click();
  await page.getByRole('tab', { name: '图片仿写', exact: true }).click();
  await expect(page.getByRole('combobox', { name: '图片方案' })).toBeVisible();
  await expect(page.getByRole('button', { name: /新方案/ })).toBeVisible();
  await page.screenshot({ path: path.resolve('../data/verification/visual-image-workspace-20261009/workspace-ui.png'), fullPage: false });
  await page.getByRole('tab', { name: '视频仿写', exact: true }).click();
  await expect(page.getByRole('combobox', { name: '视频方案' })).toBeVisible();
  await expect(page.getByRole('button', { name: /图片配音与历史项目/ })).toHaveCount(0);
  await page.setViewportSize({ width: 900, height: 900 });
  const widths = await page.evaluate(() => [document.documentElement.scrollWidth, innerWidth]);
  expect(widths[0]).toBeLessThanOrEqual(widths[1] + 1);
});
