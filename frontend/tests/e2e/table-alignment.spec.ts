import { memberAuthResponse } from './member-auth';
import { expect, test, type Locator, type Page } from '@playwright/test';

async function fixture(page: Page) {
  await page.addInitScript(() => {
    localStorage.setItem('workbench.platform', 'xiaohongshu');
    localStorage.setItem('workbench.xiaohongshu.page', 'collections');
    localStorage.setItem('workbench.xiaohongshu.columns', JSON.stringify(['title', 'platform', 'cover', 'author', 'likes', 'collects', 'comments', 'tags', 'collected_at', 'status']));
  });
  await page.route('**/api/v1/**', async route => {
    const authResponse = memberAuthResponse(route); if (authResponse) return authResponse;
    const url = new URL(route.request().url());
    const path = url.pathname.replace('/api/v1', '');
    const json = (value: unknown) => route.fulfill({ contentType: 'application/json', body: JSON.stringify(value) });
    if (path === '/workspaces') return json({ items: [{ id: 'xiaohongshu', platform: 'xiaohongshu' }], total: 1 });
    if (path === '/collections') {
      const platform = url.searchParams.get('platform');
      return json({ items: Array.from({ length: 3 }, (_, index) => ({ id: `${platform}-${index}`, platform, title: `对齐测试 ${index}`, type: 'image', status: 'ready', metadata_status: 'succeeded', author: { name: '测试作者' }, tags: [], topics: [], asset_ids: [] })), total: 3 });
    }
    if (path === '/publications') {
      const platform = url.searchParams.get('platform');
      return json({ items: Array.from({ length: 20 }, (_, index) => ({ id: `publication-${platform}-${index}`, target_platform: platform, title: `发布对齐测试 ${index}`, body: '测试正文', status: 'ready', asset_ids: [], topics: [], created_at: '2026-10-16T00:00:00Z' })), total: 20 });
    }
    return json({ items: [], total: 0 });
  });
  await page.setViewportSize({ width: 825, height: 751 });
  await page.goto('/');
  const workspace = page.locator('[data-workspace="xiaohongshu"]');
  await workspace.getByRole('button', { name: '表格视图', exact: true }).click();
  const panel = workspace.locator('.collection-table-panel');
  await expect(panel.getByRole('button', { name: '对齐测试 0', exact: false })).toBeVisible();
  return { workspace, panel, header: panel.locator('.ant-table-header'), body: panel.locator('.ant-table-body'), rail: workspace.getByRole('scrollbar', { name: '采集表格横向滚动条' }) };
}

async function aligned(panel: Locator) {
  await expect.poll(() => panel.evaluate(node => {
    const header = node.querySelector<HTMLElement>('.ant-table-header')!;
    const body = node.querySelector<HTMLElement>('.ant-table-body')!;
    const headCells = [...header.querySelectorAll('thead tr:last-child th')];
    const cells = [...(body.querySelector('tbody tr.ant-table-row')?.querySelectorAll('td') || [])];
    if (headCells.length !== cells.length) return 1000;
    return Math.max(Math.abs(header.scrollLeft - body.scrollLeft), ...headCells.map((head, i) => {
      const a = head.getBoundingClientRect(), b = cells[i].getBoundingClientRect();
      return Math.max(Math.abs(a.left - b.left), Math.abs(a.width - b.width));
    }));
  })).toBeLessThanOrEqual(1);
}

test('表头横向滚动后立即操作底部栏，表头与数据保持同步', async ({ page }) => {
  const { panel, header, body, rail } = await fixture(page);
  await aligned(panel);
  await expect(rail).toBeVisible();
  await header.hover();
  await page.mouse.wheel(64, 0);
  await rail.press('Home');
  await expect.poll(() => body.evaluate(node => node.scrollLeft)).toBe(0);
  await aligned(panel);
  await rail.press('End'); await aligned(panel);
  await rail.press('Home'); await aligned(panel);
});

test('页面隐藏期间尺寸变化，返回后表头、选择列及数据仍对齐', async ({ page }) => {
  const { workspace, panel, body, rail } = await fixture(page);
  await rail.press('End'); await aligned(panel);
  const previousLeft = await body.evaluate(node => node.scrollLeft);
  await workspace.getByRole('menuitem', { name: /仿写库/ }).click();
  await page.setViewportSize({ width: 644, height: 751 });
  await workspace.getByRole('menuitem', { name: /采集库/ }).click();
  await aligned(panel);
  await expect.poll(() => body.evaluate(node => node.scrollLeft)).toBe(previousLeft);
  await workspace.getByRole('button', { name: '展开侧边栏', exact: true }).click();
  await aligned(panel);
  await workspace.getByRole('button', { name: '收起侧边栏', exact: true }).click();
  await aligned(panel);
  await rail.press('Home'); await aligned(panel);
  await workspace.getByRole('menuitem', { name: /仿写库/ }).click();
  await workspace.getByRole('menuitem', { name: /采集库/ }).click();
  await aligned(panel);
});

test('拖拽底部栏后反向滚动表头，列保持对齐且勾选状态保留', async ({ page }) => {
  const { panel, header, rail } = await fixture(page);
  const selected = panel.locator('tbody tr.ant-table-row').first().getByRole('checkbox');
  await selected.check();
  const thumb = (await rail.locator('.workspace-scrollbar-thumb').boundingBox())!;
  await page.mouse.move(thumb.x + thumb.width / 2, thumb.y + thumb.height / 2);
  await page.mouse.down();
  await page.mouse.move(thumb.x + thumb.width / 2 + 120, thumb.y + thumb.height / 2, { steps: 8 });
  await page.mouse.up();
  await aligned(panel);
  await header.hover(); await page.mouse.wheel(-80, 0);
  await aligned(panel);
  await expect(selected).toBeChecked();
  await rail.press('Home'); await aligned(panel);
  await page.screenshot({ path: '../data/verification/table-alignment-20261016/aligned-table.png' });
});

test('发布表格返回页面后保持分页和横向位置', async ({ page }) => {
  const { workspace } = await fixture(page);
  await workspace.getByRole('menuitem', { name: /发布管理/ }).click();
  const panel = workspace.locator('.publication-table-panel');
  await expect(panel.getByRole('button', { name: /^发布对齐测试 0/ })).toBeVisible();
  await panel.locator('.ant-pagination-item-2').click();
  await expect(panel.getByRole('button', { name: /^发布对齐测试 15/ })).toBeVisible();
  const rail = workspace.getByRole('scrollbar', { name: '发布表格横向滚动条' });
  await rail.press('End'); await aligned(panel);
  const previousLeft = await panel.locator('.ant-table-body').evaluate(node => node.scrollLeft);
  await workspace.getByRole('menuitem', { name: /采集库/ }).click();
  await workspace.getByRole('menuitem', { name: /发布管理/ }).click();
  await expect(panel.getByRole('button', { name: /发布对齐测试 15 / })).toBeVisible();
  await expect.poll(() => panel.locator('.ant-table-body').evaluate(node => node.scrollLeft)).toBe(previousLeft);
  await aligned(panel);
});
