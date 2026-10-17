import { chromium } from 'playwright';
import assert from 'node:assert/strict';
import { mkdir, writeFile } from 'node:fs/promises';
import path from 'node:path';

const base = 'http://127.0.0.1:8080';
const project = '3e15dd3277739627464574280ad8981c';
const folder = path.resolve('../data/verification/media-library-ui-20261012');
await mkdir(folder, { recursive: true });
const browser = await chromium.launch({ channel: 'msedge', headless: true });
try {
  const context = await browser.newContext();
  await context.addInitScript(() => {
    localStorage.setItem('workbench.workspace', 'xiaohongshu');
    localStorage.setItem('workbench.xiaohongshu.page', 'rewrites');
    localStorage.setItem('workbench.xiaohongshu.sidebarCollapsed', 'true');
  });
  const detail = async () => {
    const res = await context.request.get(`${base}/api/v1/rewrites/${project}?platform=xiaohongshu`);
    assert(res.ok()); return res.json();
  };
  const before = await detail(); const writes = [], errors = [], checks = [];
  const page = await context.newPage(); page.on('pageerror', e => errors.push(e.message));
  await page.route('**/api/v1/**', route => {
    if (['GET', 'HEAD'].includes(route.request().method())) return route.continue();
    writes.push(new URL(route.request().url()).pathname); return route.abort();
  });
  await page.goto(base);
  const workspace = page.locator('[data-workspace="xiaohongshu"]');
  await workspace.locator('.rewrite-list-item').filter({ hasText: before.rewrite.title }).first().click();
  await workspace.getByRole('button', { name: 'AI 生成新版本', exact: true }).waitFor();
  for (const width of [1536, 825, 593, 570]) {
    await page.setViewportSize({ width, height: 751 });
    await workspace.getByRole('tab', { name: '文案编辑', exact: true }).click();
    const actions = workspace.locator('.copy-editor-actions'); await actions.scrollIntoViewIfNeeded();
    const buttons = await actions.locator('button').evaluateAll(elements => elements.map(el => {
      const r = el.getBoundingClientRect(); return { text: el.textContent, x: r.x, y: r.y, right: r.right, height: r.height };
    }));
    assert.equal(buttons.length, 1); assert.match(buttons[0].text, /AI.*生成新版本/);
    assert.equal(await workspace.getByRole('button', { name: '版本对比', exact: true }).count(), 0);
    assert(await workspace.getByRole('combobox', { name: '文案模型' }).isVisible());
    await page.screenshot({ path: path.join(folder, `edge-editor-${width}.png`) });
    await workspace.getByRole('tab', { name: '素材库', exact: true }).click();
    const first = workspace.locator('.library-media-card').first(); await first.waitFor(); await first.scrollIntoViewIfNeeded();
    const cards = await workspace.locator('.library-media-card').evaluateAll(elements => elements.map(el => {
      const box = node => { const r = node.getBoundingClientRect(); return { x: r.x, y: r.y, width: r.width, height: r.height, right: r.right, bottom: r.bottom }; };
      const media = el.querySelector('img, video');
      const naturalWidth = media instanceof HTMLVideoElement ? media.videoWidth : media.naturalWidth;
      const naturalHeight = media instanceof HTMLVideoElement ? media.videoHeight : media.naturalHeight;
      return { card: box(el), media: box(media), menu: box(el.querySelector('.library-media-menu-trigger')), ratio: naturalHeight ? naturalWidth / naturalHeight : null,
        hasMetadata: !!el.querySelector('.library-media-title, .library-media-tags, .library-media-meta, .library-media-actions') };
    }));
    assert(cards.length > 1);
    for (const { card, media, menu, ratio, hasMetadata } of cards) {
      assert(!hasMetadata, `metadata removed at ${width}`);
      assert(Math.abs(media.width - card.width) <= 1 && Math.abs(media.height - card.height) <= 1, `media fills its tile at ${width}`);
      if (ratio) assert(Math.abs(media.height - media.width / ratio) <= 1, `native aspect ratio preserved at ${width}`);
      assert.equal(menu.width, 30); assert.equal(menu.height, 30);
      assert(menu.x >= card.x && menu.right <= card.right && menu.y >= card.y && menu.bottom <= card.bottom, `menu stays inside media at ${width}`);
    }
    await first.getByRole('button', { name: /^素材操作：/ }).click();
    const menu = page.locator('.library-media-menu').getByRole('menu'); await menu.waitFor();
    assert.equal(await menu.getByRole('menuitem').count(), 3);
    const metrics = await workspace.evaluate(el => ({ width: el.getBoundingClientRect().width, scrollWidth: el.scrollWidth, rootScrollWidth: document.documentElement.scrollWidth }));
    assert(metrics.scrollWidth <= metrics.width + 2 && metrics.rootScrollWidth <= width + 2, `no horizontal overflow at ${width}`);
    await page.screenshot({ path: path.join(folder, `edge-library-${width}.png`) });
    await first.getByRole('button', { name: /^素材操作：/ }).click();
    checks.push({ width, editor_actions: buttons, library_cards: cards.length, native_aspect_ratios: true, menu_actions: 3, ...metrics });
  }
  assert.deepEqual((await detail()).rewrite, before.rewrite);
  assert.deepEqual(writes, []); assert.deepEqual(errors, []);
  const record = { checked_at: new Date().toISOString(), browser: 'Microsoft Edge', project_id: project, unchanged_project: true, writes, errors, checks };
  await writeFile(path.join(folder, 'checks.json'), JSON.stringify(record, null, 2)); process.stdout.write(JSON.stringify(record, null, 2));
} finally { await browser.close(); }
