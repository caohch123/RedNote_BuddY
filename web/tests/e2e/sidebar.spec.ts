import { expect, test, type Page } from '@playwright/test';

async function fixture(page: Page) {
  await page.route('**/api/v1/**', async route => {
    const pathname = new URL(route.request().url()).pathname;
    const body = pathname.endsWith('/workspaces')
      ? { items: [{ id: 'xiaohongshu', platform: 'xiaohongshu' }], total: 1 }
      : { items: [], total: 0 };
    await route.fulfill({ contentType: 'application/json', body: JSON.stringify(body) });
  });
}

test('侧边栏手动切换增加内容空间，并记住刷新后的选择', async ({ page }) => {
  await fixture(page); await page.setViewportSize({ width: 1440, height: 900 }); await page.goto('/');
  const xhs = page.locator('[data-workspace="xiaohongshu"]');
  const topbar = xhs.locator('.topbar');
  const brand = topbar.getByText('RedNote', { exact: true });
  await expect(brand).toBeVisible();
  await expect(topbar).toHaveCSS('background-color', 'rgb(255, 255, 255)');
  await expect(brand).toHaveCSS('background-color', 'rgba(0, 0, 0, 0)');
  const topbarBox = (await topbar.boundingBox())!;
  const brandBox = (await brand.boundingBox())!;
  expect(Math.abs(brandBox.x + brandBox.width / 2 - (topbarBox.x + topbarBox.width / 2))).toBeLessThan(1);
  await xhs.getByRole('button', { name: '收起侧边栏', exact: true }).waitFor();
  const originalWidth = (await xhs.locator('.main-content').boundingBox())!.width;
  await xhs.getByRole('button', { name: '收起侧边栏', exact: true }).click();
  await expect(xhs.getByRole('button', { name: '展开侧边栏', exact: true })).toHaveAttribute('aria-expanded', 'false');
  await expect.poll(async () => (await xhs.locator('.main-content').boundingBox())!.width).toBeGreaterThan(originalWidth + 150);
  await xhs.getByRole('menuitem', { name: /仿写库/ }).click();
  await expect(xhs.getByRole('button', { name: '展开侧边栏', exact: true })).toBeVisible();
  await page.reload();
  await expect(xhs.getByRole('button', { name: '展开侧边栏', exact: true })).toBeVisible();
  await xhs.getByRole('button', { name: '展开侧边栏', exact: true }).press('Enter');
  await expect(xhs.getByRole('button', { name: '收起侧边栏', exact: true })).toHaveAttribute('aria-expanded', 'true');
  await page.setViewportSize({ width: 825, height: 751 });
  await expect.poll(async () => (await xhs.locator('.sidebar').boundingBox())!.width).toBe(224);
  await expect(xhs.getByRole('button', { name: '收起侧边栏', exact: true })).toBeVisible();
});

test('调整仿写库项目列表与工作区分割线，并保存宽度', async ({ page }) => {
  await fixture(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/');
  const xhs = page.locator('[data-workspace="xiaohongshu"]');
  await xhs.getByRole('menuitem', { name: /仿写库/ }).click();
  await expect(xhs.getByPlaceholder('搜索仿写项目')).toBeVisible();

  const layout = xhs.locator('.rewrite-layout');
  const projectList = xhs.locator('.rewrite-list');
  const workspace = xhs.locator('.rewrite-main');
  const divider = xhs.getByRole('separator', { name: '调整项目列表与工作区宽度' });
  const initialList = (await projectList.boundingBox())!;
  const initialWorkspace = (await workspace.boundingBox())!;
  const dividerBox = (await divider.boundingBox())!;
  await expect.poll(() => divider.evaluate(element => getComputedStyle(element, '::after').left)).toBe('0px');
  const startX = dividerBox.x + dividerBox.width / 2;
  const startY = dividerBox.y + 100;

  await page.mouse.move(startX, startY);
  await page.mouse.down();
  await page.mouse.move(startX + 80, startY, { steps: 8 });
  await page.mouse.up();

  await expect.poll(async () => (await projectList.boundingBox())!.width).toBe(initialList.width + 80);
  await expect.poll(async () => (await workspace.boundingBox())!.x).toBe(initialWorkspace.x + 80);
  await expect(divider).toHaveAttribute('aria-valuenow', String(initialList.width + 80));
  await expect.poll(() => page.evaluate(() => localStorage.getItem('workbench.xiaohongshu.rewriteListWidth')))
    .toBe(String(initialList.width + 80));

  await page.reload();
  await xhs.getByRole('menuitem', { name: /仿写库/ }).click();
  await expect.poll(async () => (await projectList.boundingBox())!.width).toBe(initialList.width + 80);
  await divider.focus();
  await page.keyboard.press('ArrowLeft');
  await expect.poll(async () => (await projectList.boundingBox())!.width).toBe(initialList.width + 64);

  await page.setViewportSize({ width: 825, height: 751 });
  await expect(xhs.getByRole('separator', { name: '调整项目列表与工作区宽度' })).toBeHidden();
  const columns = await layout.evaluate(element => getComputedStyle(element).gridTemplateColumns);
  expect(columns.split(' ')).toHaveLength(1);
});

test('手机宽度展开导航不会挤窄内容，可点击遮罩收起并继续导航', async ({ page }) => {
  await fixture(page); await page.setViewportSize({ width: 360, height: 800 }); await page.goto('/');
  const xhs = page.locator('[data-workspace="xiaohongshu"]');
  await xhs.getByRole('button', { name: '展开侧边栏', exact: true }).waitFor();
  const originalWidth = (await xhs.locator('.main-content').boundingBox())!.width;
  await xhs.getByRole('button', { name: '展开侧边栏', exact: true }).click();
  await expect(xhs.getByRole('button', { name: '关闭侧边栏遮罩', exact: true })).toBeVisible();
  await expect.poll(async () => (await xhs.locator('.main-content').boundingBox())!.width).toBe(originalWidth);
  await xhs.getByRole('menuitem', { name: /仿写库/ }).click();
  await xhs.getByRole('button', { name: '关闭侧边栏遮罩', exact: true }).click();
  await expect(xhs.getByRole('button', { name: '展开侧边栏', exact: true })).toBeVisible();
  await expect(xhs.getByPlaceholder('搜索仿写项目')).toBeVisible();
  const dimensions = await page.evaluate(() => [document.documentElement.scrollWidth, innerWidth]);
  expect(dimensions[0]).toBeLessThanOrEqual(dimensions[1] + 1);
});
