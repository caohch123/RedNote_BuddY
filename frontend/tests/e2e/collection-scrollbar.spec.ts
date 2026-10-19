import { memberAuthResponse } from './member-auth';
import { expect, test, type Page } from '@playwright/test';
import { mkdirSync } from 'node:fs';
import path from 'node:path';

const proof = path.resolve('../data/verification/edge-bottom-scrollbar-20261010');
const allColumns = ['title', 'cover', 'author', 'likes', 'collects', 'comments', 'like_collect_ratio', 'like_comment_ratio', 'tags', 'collected_at', 'published_at', 'status', 'platform', 'shares'];

async function fixture(page: Page, wide = false) {
  if (wide) await page.addInitScript(columns => localStorage.setItem('workbench.xiaohongshu.columns', JSON.stringify(columns)), allColumns);
  await page.route('**/api/v1/**', async route => {
    const authResponse = memberAuthResponse(route); if (authResponse) return authResponse;
    const url = new URL(route.request().url()); const pathname = url.pathname.replace('/api/v1', '');
    const json = (value: unknown) => route.fulfill({ contentType: 'application/json', body: JSON.stringify(value) });
    if (pathname === '/workspaces') return json({ items: [{ id: 'xiaohongshu', platform: 'xiaohongshu' }], total: 1 });
    if (pathname === '/collections') {
      const platform = url.searchParams.get('platform');
      const items = Array.from({ length: 20 }, (_, index) => ({ id: `${platform}-${index}`, platform, title: `滚动测试笔记 ${index}`,
        author: { name: '测试作者' }, type: 'image', status: 'ready', metadata_status: 'succeeded', tags: [], topics: [], asset_ids: [] }));
      return json({ items, total: items.length });
    }
    if (pathname === '/publications') {
      const platform = url.searchParams.get('platform');
      const items = Array.from({ length: 20 }, (_, index) => ({ id: `publication-${platform}-${index}`, target_platform: platform,
        title: `发布滚动稿件 ${index}`, body: '正文', status: 'ready', asset_ids: [], topics: [], created_at: '2026-10-10T01:00:00Z' }));
      return json({ items, total: items.length });
    }
    if (pathname.endsWith('/account')) return json({ status: 'logged_out', dependency_ready: false });
    return json({ items: [], total: 0 });
  });
}

async function anchored(page: Page, platform = 'xiaohongshu', label = '采集表格横向滚动条') {
  const workspace = page.locator(`[data-workspace="${platform}"]`);
  if (label === '采集表格横向滚动条') {
    const tableView = workspace.getByRole('button', { name: '表格视图', exact: true });
    await expect(tableView).toBeVisible();
    if (await tableView.getAttribute('aria-pressed') !== 'true') await tableView.click();
  }
  const rail = workspace.getByRole('scrollbar', { name: label });
  await expect(rail).toBeVisible();
  const dock = (await workspace.locator('.workspace-scrollbar-dock').boundingBox())!;
  const control = (await rail.boundingBox())!;
  const thumb = (await workspace.locator('.workspace-scrollbar-thumb').boundingBox())!;
  const content = (await workspace.locator('.main-content').boundingBox())!;
  expect(Math.abs(dock.y + dock.height - page.viewportSize()!.height)).toBeLessThanOrEqual(1);
  expect(page.viewportSize()!.height - control.y - control.height).toBeLessThanOrEqual(6);
  expect(content.y + content.height).toBeLessThanOrEqual(dock.y + 1);
  expect(control.width).toBeLessThanOrEqual(580);
  expect(control.height).toBeLessThanOrEqual(10);
  expect(thumb.height).toBeLessThanOrEqual(6);
  expect(thumb.width).toBeLessThanOrEqual(control.width / 2 + 1);
  expect(Math.abs(control.x + control.width / 2 - content.x - content.width / 2)).toBeLessThanOrEqual(1);
  await expect(workspace.locator('.workspace-scrollbar-dock')).toHaveCSS('background-color', 'rgb(255, 255, 255)');
  await expect(workspace.locator('.workspace-horizontal-scrollbar')).toHaveCSS('background-color', 'rgb(240, 241, 244)');
  await expect(workspace.locator('.workspace-scrollbar-thumb')).toHaveCSS('background-color', 'rgb(200, 204, 211)');
  await expect(workspace.locator('.ant-table-sticky-scroll:visible')).toHaveCount(0);
}

test('Edge：独立底部栏支持拖动、键盘、列表末尾、折叠导航和模块切换隔离', async ({ page }) => {
  const errors: string[] = []; page.on('pageerror', error => errors.push(error.message));
  await fixture(page); await page.setViewportSize({ width: 825, height: 751 }); await page.goto('/');
  const xhs = page.locator('[data-workspace="xiaohongshu"]');
  const content = xhs.locator('.main-content'); const body = xhs.locator('.ant-table-body');
  const rail = xhs.getByRole('scrollbar', { name: '采集表格横向滚动条' });
  await xhs.getByRole('button', { name: '表格视图', exact: true }).click();
  await expect(xhs.getByRole('button', { name: '滚动测试笔记 0', exact: false })).toBeVisible();
  await xhs.getByRole('button', { name: '展开侧边栏', exact: true }).click();
  await anchored(page);
  const thumb = (await xhs.locator('.workspace-scrollbar-thumb').boundingBox())!;
  await page.mouse.move(thumb.x + 20, thumb.y + thumb.height / 2); await page.mouse.down();
  await page.mouse.move(thumb.x + 180, thumb.y + thumb.height / 2, { steps: 8 }); await page.mouse.up();
  await expect.poll(() => body.evaluate(element => element.scrollLeft)).toBeGreaterThan(100);
  await expect.poll(async () => Math.abs(await body.evaluate(element => element.scrollLeft)
    - await xhs.locator('.ant-table-header').evaluate(element => element.scrollLeft))).toBeLessThanOrEqual(1);
  await rail.press('End');
  await expect.poll(() => body.evaluate(element => element.scrollWidth - element.clientWidth - element.scrollLeft)).toBeLessThanOrEqual(1);
  await rail.press('Home'); await expect.poll(() => body.evaluate(element => element.scrollLeft)).toBe(0);
  await rail.press('ArrowRight'); await expect.poll(() => body.evaluate(element => element.scrollLeft)).toBe(40);
  await content.hover(); await page.mouse.wheel(0, 500);
  await expect.poll(() => content.evaluate(element => element.scrollTop)).toBeGreaterThan(300); await anchored(page);
  await xhs.getByRole('button', { name: '收起侧边栏', exact: true }).click();
  await expect(xhs.getByRole('button', { name: '展开侧边栏', exact: true })).toBeVisible(); await anchored(page);
  await content.hover(); await page.mouse.wheel(0, 10000);
  await expect(xhs.locator('.ant-pagination')).toBeInViewport(); await anchored(page);
  const pagination = (await xhs.locator('.ant-pagination').boundingBox())!;
  const dock = (await xhs.locator('.workspace-scrollbar-dock').boundingBox())!;
  expect(pagination.y + pagination.height).toBeLessThanOrEqual(dock.y);
  mkdirSync(proof, { recursive: true }); await page.screenshot({ path: path.join(proof, 'edge-end-of-list.png') });
  await xhs.getByRole('menuitem', { name: /仿写库/ }).click();
  await expect(rail).not.toBeVisible();
  await expect(page.getByRole('scrollbar', { name: '采集表格横向滚动条' })).toHaveCount(0);
  expect(errors).toEqual([]);
});

test('Edge：宽窗口少量溢出时滑块仍短，滚到底部不回到数据行内', async ({ page }) => {
  await fixture(page, true); await page.setViewportSize({ width: 2000, height: 900 }); await page.goto('/');
  const workspace = page.locator('[data-workspace="xiaohongshu"]'); const content = workspace.locator('.main-content');
  const body = workspace.locator('.ant-table-body');
  await anchored(page);
  const ratio = await body.evaluate(element => element.clientWidth / element.scrollWidth);
  expect(ratio).toBeGreaterThan(.8);
  await content.hover(); await page.mouse.wheel(0, 700); await anchored(page);
  mkdirSync(proof, { recursive: true }); await page.screenshot({ path: path.join(proof, 'edge-wide-middle.png') });
  await page.mouse.wheel(0, 10000); await expect(workspace.locator('.ant-pagination')).toBeInViewport(); await anchored(page);
  await page.screenshot({ path: path.join(proof, 'edge-wide-bottom.png') });
  await page.setViewportSize({ width: 2560, height: 900 });
  await expect(workspace.getByRole('scrollbar', { name: '采集表格横向滚动条' })).toHaveCount(0);
  await page.setViewportSize({ width: 2000, height: 900 }); await anchored(page);
  await page.setViewportSize({ width: 825, height: 751 }); await anchored(page);
});

test('Edge：运行中的工作台底部栏只读验证', async ({ page, browser }) => {
  test.skip(!process.env.XHS_LIVE_URL, 'Set XHS_LIVE_URL to inspect the deployed workbench without mutations');
  await page.setViewportSize({ width: 1280, height: 900 }); await page.goto('/');
  await page.getByRole('menuitem', { name: /采集库/ }).click();
  await expect(page.getByRole('textbox', { name: '小红书链接', exact: true })).toBeVisible();
  await anchored(page);
  const content = page.locator('[data-workspace="xiaohongshu"] .main-content');
  await content.hover(); await page.mouse.wheel(0, 500); await anchored(page);
  mkdirSync(proof, { recursive: true }); await page.screenshot({ path: path.join(proof, 'edge-live-workbench.png') });
  await page.mouse.wheel(0, 10000); await anchored(page);
  await page.screenshot({ path: path.join(proof, 'edge-live-bottom.png') });
  console.log(`Microsoft Edge channel / browser version: ${browser.version()}`);
});

test('Edge：发布管理底部栏支持拖拽、键盘、分页和模块切换隔离', async ({ page }) => {
  await fixture(page); await page.setViewportSize({ width: 825, height: 751 }); await page.goto('/');
  const workspace = page.locator('[data-workspace="xiaohongshu"]');
  await workspace.getByRole('menuitem', { name: /发布管理/ }).click();
  await expect(workspace.getByRole('button', { name: /^发布滚动稿件 0/ })).toBeVisible();
  const panel = workspace.locator('.publication-table-panel'); const body = panel.locator('.ant-table-body');
  const rail = workspace.getByRole('scrollbar', { name: '发布表格横向滚动条' });
  await anchored(page, 'xiaohongshu', '发布表格横向滚动条');
  const thumb = (await workspace.locator('.workspace-scrollbar-thumb').boundingBox())!;
  await page.mouse.move(thumb.x + 15, thumb.y + thumb.height / 2); await page.mouse.down();
  await page.mouse.move(thumb.x + 150, thumb.y + thumb.height / 2, { steps: 8 }); await page.mouse.up();
  await expect.poll(() => body.evaluate(node => node.scrollLeft)).toBeGreaterThan(100);
  await expect.poll(async () => Math.abs(await body.evaluate(node => node.scrollLeft) - await panel.locator('.ant-table-header').evaluate(node => node.scrollLeft))).toBeLessThanOrEqual(1);
  await rail.press('End');
  await expect.poll(() => body.evaluate(node => node.scrollWidth - node.clientWidth - node.scrollLeft)).toBeLessThanOrEqual(1);
  await rail.press('Home'); await expect.poll(() => body.evaluate(node => node.scrollLeft)).toBe(0);
  await workspace.locator('.main-content').hover(); await page.mouse.wheel(0, 10000);
  await expect(panel.locator('.ant-pagination')).toBeInViewport();
  await anchored(page, 'xiaohongshu', '发布表格横向滚动条');
  const pagination = (await panel.locator('.ant-pagination').boundingBox())!;
  const dock = (await workspace.locator('.workspace-scrollbar-dock').boundingBox())!;
  expect(pagination.y + pagination.height).toBeLessThanOrEqual(dock.y);
  await panel.locator('.ant-pagination-item-2').click();
  await expect(workspace.getByRole('button', { name: /^发布滚动稿件 15/ })).toBeVisible();
  await anchored(page, 'xiaohongshu', '发布表格横向滚动条');
  await workspace.getByRole('menuitem', { name: /采集库/ }).click();
  await expect(rail).toHaveCount(0); await anchored(page);
  await workspace.getByRole('menuitem', { name: /仿写库/ }).click();
  await expect(workspace.getByRole('scrollbar')).toHaveCount(0);
});
