import { expect, test } from '@playwright/test';
import { mkdirSync, writeFileSync } from 'node:fs';
import path from 'node:path';

test('本地工作台真实素材在 Edge 展示序号与拖拽手柄（只读）', async ({ page, browser }) => {
  test.skip(process.env.XHS_MEDIA_LIVE !== '1', 'Opt-in read-only verification against the local workbench.');
  const writes: string[] = [];
  await page.route('**/api/v1/**', async route => {
    if (route.request().method() !== 'GET') {
      writes.push(route.request().method()); await route.abort(); return;
    }
    await route.continue();
  });
  await page.addInitScript(() => {
    localStorage.setItem('workbench.workspace', 'xiaohongshu');
    localStorage.setItem('workbench.xiaohongshu.page', 'rewrites');
    localStorage.setItem('workbench.xiaohongshu.sidebarCollapsed', 'false');
  });
  const detailResponse = page.waitForResponse(response => /\/api\/v1\/rewrites\/[^/?]+(?:\?|$)/.test(response.url()) && response.request().method() === 'GET');
  await page.goto('/');
  const detail = await (await detailResponse).json();
  expect(detail.assets.length).toBe(new Set(detail.assets.map((asset: { id: string }) => asset.id)).size);
  await page.getByRole('tab', { name: /发布素材/ }).click();
  const cards = page.locator('.publication-media-grid .selected-media');
  await expect(cards.first()).toBeVisible();
  await expect(cards.first().locator('.media-publish-position')).toHaveText('1');
  await expect(cards.first().locator('.media-sort-handle')).toBeVisible();
  await expect(page.locator('.selected-media-list')).toHaveCount(0);
  const image = cards.locator('img').first();
  await expect.poll(() => image.evaluate((node: HTMLImageElement) => node.naturalWidth)).toBeGreaterThan(0);
  const proof = path.resolve(process.env.XHS_MEDIA_PROOF || '../data/verification/edge-media-sort-20261010');
  mkdirSync(proof, { recursive: true });
  const selectedCount = await cards.count();
  const firstCard = page.locator('.publication-media-grid .media-selection-card').first();
  const box = firstCard.getByRole('checkbox');
  const before = (await firstCard.boundingBox())!;
  await box.uncheck();
  await expect(box).not.toBeChecked();
  await expect(cards).toHaveCount(selectedCount - 1);
  await expect(page.getByText('素材有未保存编辑', { exact: true })).toBeVisible();
  const after = (await firstCard.boundingBox())!;
  expect(Math.abs(after.x - before.x)).toBeLessThanOrEqual(1);
  expect(Math.abs(after.y - before.y)).toBeLessThanOrEqual(1);
  await page.locator('.publication-media-grid').screenshot({ path: path.join(proof, 'edge-live-canceled.png') });
  await box.check();
  await expect(cards).toHaveCount(selectedCount);
  await expect(page.getByText('素材有未保存编辑', { exact: true })).toHaveCount(0);
  await page.screenshot({ path: path.join(proof, 'edge-live-workbench.png') });
  await page.locator('.publication-media-grid').screenshot({ path: path.join(proof, 'edge-live-media-grid.png') });
  await page.setViewportSize({ width: 825, height: 751 });
  await expect(cards.first().locator('.media-sort-handle')).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(825);
  await page.locator('.publication-media-grid').screenshot({ path: path.join(proof, 'edge-live-media-grid-narrow.png') });
  expect(writes).toEqual([]);
  writeFileSync(path.join(proof, 'live-checks.json'), JSON.stringify({ browser: `Microsoft Edge ${browser.version()}`, selectedCards: await cards.count(), candidateAssets: detail.assets.length, duplicateAssetIDs: 0, canceledAndRestored: true, writes: 0, widths: [1536, 825] }, null, 2));
});
