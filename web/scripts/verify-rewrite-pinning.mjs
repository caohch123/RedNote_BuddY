import { chromium, expect } from '@playwright/test';
import assert from 'node:assert/strict';
import { mkdir, writeFile } from 'node:fs/promises';
import path from 'node:path';

const base = 'http://127.0.0.1:8080';
const folder = path.resolve('../data/verification/rewrite-pinning-20261013');
await mkdir(folder, { recursive: true });
const browser = await chromium.launch({ channel: 'msedge', headless: true, ignoreDefaultArgs: ['--hide-scrollbars'] });
const context = await browser.newContext();
let target, changed = false;
async function get(pathname) {
  const response = await context.request.get(`${base}/api/v1${pathname}`); assert(response.ok()); return response.json();
}
try {
  const before = await get('/rewrites?platform=xiaohongshu');
  target = before.items.slice(1).find(item => !item.pinned); assert(target, 'Requires an unpinned project below the first row');
  const beforeDetail = await get(`/rewrites/${target.id}?platform=xiaohongshu`);
  await context.addInitScript(() => {
    localStorage.setItem('workbench.workspace', 'xiaohongshu');
    localStorage.setItem('workbench.xiaohongshu.page', 'rewrites');
    localStorage.setItem('workbench.xiaohongshu.sidebarCollapsed', 'true');
  });
  const page = await context.newPage(); const writes = [], errors = [], checks = [];
  page.on('pageerror', e => errors.push(e.message));
  await page.route('**/api/v1/**', route => {
    const req = route.request(), pathname = new URL(req.url()).pathname;
    if (['GET', 'HEAD'].includes(req.method())) return route.continue();
    if (req.method() === 'PATCH' && pathname === `/api/v1/rewrites/${target.id}` && Object.keys(req.postDataJSON()).join() === 'pinned') {
      writes.push(req.postDataJSON()); return route.continue();
    }
    return route.abort();
  });
  await page.setViewportSize({ width: 474, height: 751 }); await page.goto(base);
  const row = page.locator(`[data-workspace="xiaohongshu"] [data-rewrite-id="${target.id}"]`);
  const rows = page.locator('[data-workspace="xiaohongshu"] .rewrite-list-row');
  await row.getByRole('button', { name: `置顶仿写项目 ${target.title}`, exact: true }).click(); changed = true;
  await expect(rows.first()).toHaveAttribute('data-rewrite-id', target.id);
  await expect(row.getByRole('button', { name: `取消置顶仿写项目 ${target.title}`, exact: true })).toHaveAttribute('aria-pressed', 'true');
  await page.reload();
  await expect(rows.first()).toHaveAttribute('data-rewrite-id', target.id);
  await expect(row.locator('.rewrite-pin')).toHaveAttribute('aria-pressed', 'true');
  await page.mouse.move(20, 20); await page.screenshot({ path: path.join(folder, 'edge-pinned-474.png') });
  await row.getByRole('button', { name: `取消置顶仿写项目 ${target.title}`, exact: true }).click();
  await expect.poll(() => rows.evaluateAll(elements => elements.map(el => el.dataset.rewriteId))).toEqual(before.items.map(item => item.id));
  changed = false;
  for (const width of [474, 825, 1536]) {
    await page.setViewportSize({ width, height: 751 });
    const trash = row.locator('.rewrite-delete'); await trash.scrollIntoViewIfNeeded();
    await page.mouse.move(20, 20);
    await expect.poll(() => trash.evaluate(el => getComputedStyle(el).color)).toBe('rgb(156, 166, 183)');
    await page.screenshot({ path: path.join(folder, `edge-idle-${width}.png`) });
    await trash.hover();
    await expect.poll(() => trash.evaluate(el => getComputedStyle(el).color)).toBe('rgb(255, 77, 79)');
    await page.screenshot({ path: path.join(folder, `edge-delete-hover-${width}.png`) });
    const sizes = await row.locator('.rewrite-row-actions button').evaluateAll(elements => elements.map(el => ({ width: el.getBoundingClientRect().width, height: el.getBoundingClientRect().height })));
    assert.deepEqual(sizes, [{ width: 24, height: 24 }, { width: 24, height: 24 }]);
    const rootWidth = await page.evaluate(() => document.documentElement.scrollWidth); assert(rootWidth <= width + 2);
    checks.push({ width, idle_color: '#9ca6b7', hover_color: '#ff4d4f', sizes, rootWidth });
  }
  assert.deepEqual((await get(`/rewrites/${target.id}?platform=xiaohongshu`)).rewrite, beforeDetail.rewrite);
  assert.deepEqual((await get('/rewrites?platform=xiaohongshu')).items, before.items);
  assert.deepEqual(writes, [{ pinned: true }, { pinned: false }]); assert.deepEqual(errors, []);
  const record = { checked_at: new Date().toISOString(), browser: 'Microsoft Edge', project_id: target.id, refresh_persistence: true, restored_original_pin_state: true, unchanged_project_content: true, writes, errors, checks };
  await writeFile(path.join(folder, 'checks.json'), JSON.stringify(record, null, 2)); process.stdout.write(JSON.stringify(record, null, 2));
} finally {
  if (changed && target) {
    const { csrf_token } = await get('/session');
    const restored = await context.request.patch(`${base}/api/v1/rewrites/${target.id}?platform=xiaohongshu`, { headers: { 'X-CSRF-Token': csrf_token }, data: { pinned: !!target.pinned } });
    assert(restored.ok(), 'Could not restore original pin state');
  }
  await browser.close();
}
