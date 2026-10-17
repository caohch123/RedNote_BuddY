import { chromium } from 'playwright';
import { mkdir, writeFile } from 'node:fs/promises';
import path from 'node:path';
import assert from 'node:assert/strict';

// Read-only acceptance against the local app: no AI submissions or data edits.
const base = process.env.XHS_LIVE_URL || 'http://127.0.0.1:8080';
const output = path.resolve(process.env.VERIFICATION_DIR || '../data/verification/edge-project-media-library-20261010');
await mkdir(output, { recursive: true });
const browser = await chromium.launch({ channel: 'msedge', headless: true });
try {
  const context = await browser.newContext({ viewport: { width: 1536, height: 1000 } });
  const page = await context.newPage();
  const errors = []; const writes = [];
  page.on('pageerror', error => errors.push(error.message));
  await page.route('**/api/v1/**', async route => {
    if (!['GET', 'HEAD'].includes(route.request().method())) { writes.push(new URL(route.request().url()).pathname); await route.abort(); }
    else await route.continue();
  });
  await context.addInitScript(() => {
    localStorage.setItem('workbench.workspace', 'xiaohongshu');
    localStorage.setItem('workbench.xiaohongshu.page', 'library');
    localStorage.setItem('workbench.xiaohongshu.sidebarCollapsed', 'true');
  });
  await page.goto(base);
  const workspace = page.locator('[data-workspace="xiaohongshu"]');
  await workspace.getByRole('tab', { name: '素材库', exact: true }).click();
  await workspace.getByRole('searchbox', { name: '搜索素材库' }).waitFor();
  assert.equal(await workspace.getByRole('menuitem', { name: /素材库/ }).count(), 0, 'Library still in sidebar');
  const projectID = await workspace.locator('.project-media-library').getAttribute('data-rewrite-id');
  assert(projectID, 'Library is not scoped to a project');
  const response = await context.request.get(`${base}/api/v1/media-library?platform=xiaohongshu&rewrite_id=${encodeURIComponent(projectID)}`);
  assert(response.ok(), 'Library endpoint unavailable');
  const library = await response.json(); const expected = library.total;
  assert(library.items.every(item => item.rewrite_id === projectID), 'Other project results leaked into library');
  await page.waitForFunction(total => document.querySelectorAll('[data-workspace="xiaohongshu"] .library-media-card').length === total, expected);
  const cards = await workspace.locator('.library-media-card').count();
  for (const width of [1536, 825, 597]) {
    await page.setViewportSize({ width, height: 751 });
    await page.screenshot({ path: path.join(output, `library-${width}.png`) });
    assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1), `Overflow at ${width}`);
  }
  await workspace.getByRole('tab', { name: '图片仿写', exact: true }).click();
  if (await workspace.getByRole('button', { name: '新方案', exact: true }).count()) await workspace.getByRole('button', { name: '新方案', exact: true }).click();
  await workspace.getByRole('button', { name: '选择来源图片', exact: true }).click();
  const source = page.getByRole('dialog', { name: '选择来源图片', exact: true });
  await source.waitFor();
  const sourceImages = await source.getByRole('checkbox').count();
  assert(sourceImages > 0, 'No real source images available');
  await page.screenshot({ path: path.join(output, 'source-picker-597.png') });
  assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1), 'Source picker overflow');
  await source.getByRole('button', { name: '取消', exact: true }).click();
  await workspace.getByRole('button', { name: '选择风格参考', exact: true }).click();
  const style = page.getByRole('dialog', { name: '官方风格参考库', exact: true });
  await style.getByText('官方风格参考图待上架', { exact: true }).waitFor();
  await style.getByRole('combobox', { name: '风格参考分类' }).click();
  await page.getByRole('option', { name: '教育知识', exact: true }).click();
  await page.screenshot({ path: path.join(output, 'style-library-empty-597.png') });
  await style.getByRole('button', { name: '取消', exact: true }).click();
  assert.equal(writes.length, 0, 'Acceptance attempted a write');
  assert.deepEqual(errors, [], 'Page errors');
  const checks = { browser: 'Microsoft Edge', projectID, cards, sourceImages, widths: [1536, 825, 597], sidebarLibraryRemoved: true, projectScoped: true, writes: writes.length, pageErrors: errors.length };
  await writeFile(path.join(output, 'live-checks.json'), JSON.stringify(checks, null, 2));
  console.log(JSON.stringify(checks));
  await context.close();
} finally { await browser.close(); }
